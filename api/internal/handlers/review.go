package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"
	"fastsell-api/internal/upload"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReviewStore struct {
	pool        *pgxpool.Pool
	files       *ManagedFileService
	imageConfig ItemImageStorageConfig
}

type ReviewHandler struct {
	store *ReviewStore
}

type reviewImageRow struct {
	ImageAssetID     string
	OriginalFilename *string
	StoredFilename   *string
	FilePath         string
	ThumbnailPath    *string
	NormalizedPath   *string
	MimeType         *string
	FileSizeBytes    *int64
	Status           string
	UploadOrder      int
	ItemID           *string
}

type reviewGroupRow struct {
	UploadSessionID       string
	ContainerID           *string
	SessionLocationID     *string
	SessionLocationName   *string
	SessionLocationDetail *string
	InventoryGroupID      *string
	ClientGroupID         *string
	Title                 *string
	Notes                 *string
	Status                string
	AIAssistStatus        string
}

type reviewQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

var (
	errAlreadyApproved         = errors.New("upload group is already approved")
	errAIAssistAlreadyRunning  = errors.New("AI Assist is already queued or processing for this group")
	errAIAssistNoProvider      = errors.New("AI Assist requires an active provider")
	errAIAssistNoImages        = errors.New("upload group must have at least one processed image")
	errAIAssistImagesInFlight  = errors.New("upload group still has images in progress")
	errAIAssistApprovalBlocked = errors.New("upload group cannot be approved while AI Assist is queued or processing")
	errReviewDiscardApproved   = errors.New("upload group has approved item-linked images and cannot be discarded")
	errReviewDiscardInFlight   = errors.New("upload group still has images in progress and cannot be discarded")
)

var approxValuePattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

type reviewDeleteContext struct {
	Preview         models.ReviewUploadGroupDeletePreview
	FilePaths       []string
	UploadSessionID string
}

func NewReviewStore(pool *pgxpool.Pool, files *ManagedFileService, imageConfig ItemImageStorageConfig) *ReviewStore {
	return &ReviewStore{pool: pool, files: files, imageConfig: imageConfig}
}

func NewReviewHandler(store *ReviewStore) *ReviewHandler {
	return &ReviewHandler{store: store}
}

func (h *ReviewHandler) UploadImages(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "upload group id must be a valid UUID")
		return
	}

	if err := h.store.ensureImageDirectories(); err != nil {
		log.Printf("review image upload storage prepare failed upload_group_id=%s error=%v", groupID, err)
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_unavailable", "failed to prepare image directories")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.store.maxReviewImageBodyBytes())
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid multipart form data")
		return
	}

	headers := r.MultipartForm.File[itemImageMultipartField]
	if len(headers) == 0 {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "at least one image file is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	savedFiles := make([]savedItemImageFile, 0, len(headers))
	writtenFiles := make([]writtenItemImageFile, 0, len(headers))
	for _, header := range headers {
		saved, err := h.store.saveReviewImageUpload(header, &writtenFiles)
		if err != nil {
			cleanupWrittenItemFiles(writtenFiles)
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		savedFiles = append(savedFiles, saved)
	}

	group, err := h.store.AppendImages(ctx, groupID, savedFiles)
	if err != nil {
		log.Printf("review image upload failed upload_group_id=%s error=%v", groupID, err)
		cleanupWrittenItemFiles(writtenFiles)
		h.store.cleanupSavedReviewVariants(savedFiles)
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
			return
		}
		if errors.Is(err, errAlreadyApproved) {
			respond.ErrorCode(w, http.StatusConflict, "conflict", "upload group is already approved")
			return
		}
		if errors.Is(err, errInvalidUploadRequest) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "upload_failed", "failed to upload review images")
		return
	}

	respond.JSON(w, http.StatusCreated, models.ReviewUploadGroupImageMutationResponse{Group: group})
}

func (h *ReviewHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "upload group id must be a valid UUID")
		return
	}

	imageID := strings.TrimSpace(chi.URLParam(r, "image"))
	if !isUUID(imageID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "image id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.store.DeleteImage(ctx, groupID, imageID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "review image was not found")
		case errors.Is(err, errAlreadyApproved):
			respond.ErrorCode(w, http.StatusConflict, "conflict", "upload group is already approved")
		case errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "delete_failed", "failed to delete review image")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (s *ReviewStore) ListUploadGroups(ctx context.Context, containerID *string) (models.ReviewUploadGroupList, error) {
	groups, err := s.queryReviewUploadGroups(ctx, s.pool, containerID, nil)
	if err != nil {
		return models.ReviewUploadGroupList{}, err
	}
	return models.ReviewUploadGroupList{Groups: groups}, nil
}

func (s *ReviewStore) GetUploadGroup(ctx context.Context, groupID string) (models.ReviewUploadGroup, error) {
	groups, err := s.queryReviewUploadGroups(ctx, s.pool, nil, &groupID)
	if err != nil {
		return models.ReviewUploadGroup{}, err
	}
	if len(groups) == 0 {
		return models.ReviewUploadGroup{}, pgx.ErrNoRows
	}
	return groups[0], nil
}

func (s *ReviewStore) ensureImageDirectories() error {
	for _, dir := range []string{s.imageConfig.OriginalsDir, s.imageConfig.ThumbnailsDir, s.imageConfig.NormalizedDir} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}
	return nil
}

func (s *ReviewStore) cleanupSavedReviewVariants(savedFiles []savedItemImageFile) {
	for _, file := range savedFiles {
		_ = os.Remove(filepath.Join(s.imageConfig.ThumbnailsDir, file.storedFilename))
		_ = os.Remove(filepath.Join(s.imageConfig.NormalizedDir, file.storedFilename))
	}
}

func (s *ReviewStore) maxReviewImageBodyBytes() int64 {
	const maxExpectedFiles = 20
	return int64(maxExpectedFiles)*s.imageConfig.MaxUploadBytes + itemImageUploadFormOverhead
}

func (s *ReviewStore) saveReviewImageUpload(header *multipart.FileHeader, writtenFiles *[]writtenItemImageFile) (savedItemImageFile, error) {
	if header.Size <= 0 {
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q is empty", errInvalidUploadRequest, header.Filename)
	}
	if header.Size > s.imageConfig.MaxUploadBytes {
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q exceeds max upload size", errInvalidUploadRequest, header.Filename)
	}

	src, err := header.Open()
	if err != nil {
		return savedItemImageFile{}, err
	}
	defer src.Close()

	originalFilename := cleanOriginalFilename(header.Filename)
	if originalFilename == "" {
		return savedItemImageFile{}, fmt.Errorf("%w: original filename is required", errInvalidUploadRequest)
	}

	declaredMimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	storedFilename, err := upload.NewStoredFilename(originalFilename, declaredMimeType)
	if err != nil {
		return savedItemImageFile{}, fmt.Errorf("%w: %v", errInvalidUploadRequest, err)
	}

	destinationPath := filepath.Join(s.imageConfig.OriginalsDir, storedFilename)
	dst, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		return savedItemImageFile{}, err
	}

	hasher := sha256.New()
	limitedReader := io.LimitReader(src, s.imageConfig.MaxUploadBytes+1)
	bytesWritten, copyErr := io.Copy(dst, io.TeeReader(limitedReader, hasher))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, closeErr
	}
	if bytesWritten > s.imageConfig.MaxUploadBytes {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q exceeds max upload size", errInvalidUploadRequest, originalFilename)
	}

	detectedMimeType, err := detectSupportedImageMime(destinationPath)
	if err != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, fmt.Errorf("%w: %v", errInvalidUploadRequest, err)
	}

	*writtenFiles = append(*writtenFiles, writtenItemImageFile{path: destinationPath})

	return savedItemImageFile{
		originalFilename: originalFilename,
		storedFilename:   storedFilename,
		filePath:         destinationPath,
		mimeType:         detectedMimeType,
		sizeBytes:        bytesWritten,
		hashHex:          hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (s *ReviewStore) AppendImages(ctx context.Context, groupID string, savedFiles []savedItemImageFile) (models.ReviewUploadGroup, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ReviewUploadGroup{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := s.lockReviewGroup(ctx, tx, groupID); err != nil {
		return models.ReviewUploadGroup{}, err
	}

	existingImages, err := s.loadReviewImagesForDelete(ctx, tx, groupID, true)
	if err != nil {
		return models.ReviewUploadGroup{}, err
	}

	for _, image := range existingImages {
		if image.ItemID != nil {
			return models.ReviewUploadGroup{}, errAlreadyApproved
		}
	}

	if s.imageConfig.MaxImagesPerItem > 0 && len(existingImages)+len(savedFiles) > s.imageConfig.MaxImagesPerItem {
		return models.ReviewUploadGroup{}, fmt.Errorf("%w: upload group cannot have more than %d images", errInvalidUploadRequest, s.imageConfig.MaxImagesPerItem)
	}

	nextUploadOrder := len(existingImages)
	variantUpdates := make([]itemImageVariantUpdate, 0, len(savedFiles))
	for index, file := range savedFiles {
		var imageAssetID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO image_assets (
				upload_group_id,
				session_id,
				original_filename,
				stored_filename,
				file_path,
				file_hash,
				mime_type,
				file_size_bytes,
				upload_order,
				is_original,
				status
			)
			SELECT
				$1::uuid,
				ug.session_id,
				$2,
				$3,
				$4,
				$5,
				$6,
				$7,
				$8,
				true,
				'processed'
			FROM upload_groups ug
			WHERE ug.id = $1::uuid
			RETURNING id::text
		`,
			groupID,
			file.originalFilename,
			file.storedFilename,
			file.filePath,
			file.hashHex,
			file.mimeType,
			file.sizeBytes,
			nextUploadOrder+index,
		).Scan(&imageAssetID); err != nil {
			return models.ReviewUploadGroup{}, err
		}

		thumbnailPath, normalizedPath, err := s.regenerateReviewImageVariants(itemImageUploadRecord{
			ID:             imageAssetID,
			FilePath:       file.filePath,
			StoredFilename: file.storedFilename,
			MimeType:       &file.mimeType,
		})
		if err != nil {
			return models.ReviewUploadGroup{}, err
		}
		variantUpdates = append(variantUpdates, itemImageVariantUpdate{
			ImageAssetID:   imageAssetID,
			ThumbnailPath:  thumbnailPath,
			NormalizedPath: normalizedPath,
		})
	}

	for _, update := range variantUpdates {
		if _, err := tx.Exec(ctx, `
			UPDATE image_assets
			SET thumbnail_path = $2,
				normalized_path = $3,
				updated_datetime = now()
			WHERE id = $1::uuid
		`, update.ImageAssetID, update.ThumbnailPath, update.NormalizedPath); err != nil {
			return models.ReviewUploadGroup{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ReviewUploadGroup{}, err
	}

	return s.GetUploadGroup(ctx, groupID)
}

func (s *ReviewStore) DeleteImage(ctx context.Context, groupID string, imageID string) (models.ReviewImageDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := s.lockReviewGroup(ctx, tx, groupID); err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}

	image, err := s.loadReviewDeleteImageRecord(ctx, tx, groupID, imageID, true)
	if err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}
	if image.ItemID != nil {
		return models.ReviewImageDeleteResponse{}, errAlreadyApproved
	}

	refs := []managedFileReference{{
		ImageAssetID: image.ImageAssetID,
		Kind:         "original",
		Path:         image.FilePath,
	}}
	if image.ThumbnailPath != nil && strings.TrimSpace(*image.ThumbnailPath) != "" {
		refs = append(refs, managedFileReference{
			ImageAssetID: image.ImageAssetID,
			Kind:         "thumbnail",
			Path:         *image.ThumbnailPath,
		})
	}
	if image.NormalizedPath != nil && strings.TrimSpace(*image.NormalizedPath) != "" {
		refs = append(refs, managedFileReference{
			ImageAssetID: image.ImageAssetID,
			Kind:         "normalized",
			Path:         *image.NormalizedPath,
		})
	}

	inspection, err := s.files.InspectReferences(refs)
	if err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}

	deleteTag, err := tx.Exec(ctx, `
		DELETE FROM image_assets
		WHERE id = $1::uuid
			AND upload_group_id = $2::uuid
			AND item_id IS NULL
	`, imageID, groupID)
	if err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}
	if deleteTag.RowsAffected() == 0 {
		return models.ReviewImageDeleteResponse{}, pgx.ErrNoRows
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ReviewImageDeleteResponse{}, err
	}

	var updatedGroup *models.ReviewUploadGroup
	group, err := s.GetUploadGroup(ctx, groupID)
	if err == nil {
		updatedGroup = &group
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return models.ReviewImageDeleteResponse{}, err
	}

	deletedFiles, missingFiles, warnings := s.files.DeleteFiles(inspection.DeletePaths, "review image delete")
	return models.ReviewImageDeleteResponse{
		Group:               updatedGroup,
		DeletedImageAssetID: imageID,
		DeletedFileCount:    deletedFiles,
		MissingFileCount:    missingFiles,
		Warnings:            warnings,
	}, nil
}

func (s *ReviewStore) QueueAIAssist(ctx context.Context, groupID string, req models.QueueReviewAIAssistRequest) (models.QueueReviewAIAssistResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.QueueReviewAIAssistResponse{}, err
	}
	defer tx.Rollback(ctx)

	group, err := s.lockReviewGroup(ctx, tx, groupID)
	if err != nil {
		return models.QueueReviewAIAssistResponse{}, err
	}
	if group.Status == "approved" {
		return models.QueueReviewAIAssistResponse{}, errAlreadyApproved
	}
	if group.AIAssistStatus == "queued" || group.AIAssistStatus == "processing" {
		return models.QueueReviewAIAssistResponse{}, errAIAssistAlreadyRunning
	}

	images, err := s.loadReviewImagesForTx(ctx, tx, groupID)
	if err != nil {
		return models.QueueReviewAIAssistResponse{}, err
	}

	var processedCount int
	var inFlightCount int
	for _, image := range images {
		if image.ItemID != nil {
			return models.QueueReviewAIAssistResponse{}, errAlreadyApproved
		}
		switch image.Status {
		case "processed":
			processedCount++
		case "pending", "uploaded", "processing":
			inFlightCount++
		}
	}
	if processedCount == 0 {
		return models.QueueReviewAIAssistResponse{}, errAIAssistNoImages
	}
	if inFlightCount > 0 {
		return models.QueueReviewAIAssistResponse{}, errAIAssistImagesInFlight
	}

	providerStore := NewAIProviderStore(s.pool)
	provider, err := providerStore.getActiveProvider(ctx, tx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.QueueReviewAIAssistResponse{}, errAIAssistNoProvider
		}
		return models.QueueReviewAIAssistResponse{}, err
	}
	if err := validateReviewAssistProvider(provider); err != nil {
		return models.QueueReviewAIAssistResponse{}, err
	}

	rememberReviewAssistHint(groupID, req.UserHint)

	var response models.QueueReviewAIAssistResponse
	err = tx.QueryRow(ctx, `
		UPDATE upload_groups
		SET
			ai_assist_status = 'queued',
			ai_assist_error_message = NULL,
			ai_assist_requested_datetime = now(),
			ai_assist_started_datetime = NULL,
			ai_assist_completed_datetime = NULL,
			ai_assist_provider_config_id = $2,
			ai_suggested_title = NULL,
			ai_suggested_description = NULL,
			ai_suggested_approx_value = NULL,
			updated_datetime = now()
		WHERE id = $1
		RETURNING
			id::text,
			ai_assist_status,
			ai_assist_error_message,
			ai_suggested_title,
			ai_suggested_description,
			ai_suggested_approx_value::text,
			ai_assist_requested_datetime,
			ai_assist_started_datetime,
			ai_assist_completed_datetime
	`, groupID, provider.ID).Scan(
		&response.UploadGroupID,
		&response.AIAssistStatus,
		&response.AIAssistErrorMessage,
		&response.AISuggestedTitle,
		&response.AISuggestedDescription,
		&response.AISuggestedApproxValue,
		&response.AIAssistRequestedDatetime,
		&response.AIAssistStartedDatetime,
		&response.AIAssistCompletedDatetime,
	)
	if err != nil {
		forgetReviewAssistHint(groupID)
		return models.QueueReviewAIAssistResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		forgetReviewAssistHint(groupID)
		return models.QueueReviewAIAssistResponse{}, err
	}

	return response, nil
}

func (s *ReviewStore) Approve(ctx context.Context, groupID string, req models.ApproveUploadGroupRequest) (models.ApproveUploadGroupResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}
	defer tx.Rollback(ctx)

	group, err := s.lockReviewGroup(ctx, tx, groupID)
	if err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}
	if group.AIAssistStatus == "queued" || group.AIAssistStatus == "processing" {
		return models.ApproveUploadGroupResponse{}, errAIAssistApprovalBlocked
	}

	images, err := s.loadReviewImagesForTx(ctx, tx, groupID)
	if err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}
	if len(images) == 0 {
		return models.ApproveUploadGroupResponse{}, errors.New("upload group has no images")
	}

	var processedCount int
	var inFlightCount int
	for _, image := range images {
		if image.ItemID != nil {
			return models.ApproveUploadGroupResponse{}, errAlreadyApproved
		}
		switch image.Status {
		case "processed":
			processedCount++
		case "pending", "uploaded", "processing":
			inFlightCount++
		}
	}

	if processedCount == 0 {
		return models.ApproveUploadGroupResponse{}, errors.New("upload group must have at least one processed image")
	}
	if inFlightCount > 0 {
		return models.ApproveUploadGroupResponse{}, errors.New("upload group still has images in progress")
	}
	inventoryGroupID, inventoryGroupCode, inventoryGroupName, err := s.resolveReviewInventoryGroup(ctx, tx, req.InventoryGroupID, group.InventoryGroupID)
	if err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}

	title := trimOptionalString(req.Title)
	if title == nil {
		title = group.Title
	}
	description := trimOptionalString(req.Description)
	if description == nil {
		description = group.Notes
	}

	var containerArg any = nil
	if group.ContainerID != nil {
		containerArg = *group.ContainerID
	}
	var locationArg any = nil
	var locationName *string
	var locationDetailArg any = nil
	if group.ContainerID == nil {
		if group.SessionLocationID != nil {
			locationArg = *group.SessionLocationID
			locationName = group.SessionLocationName
		}
		if group.SessionLocationDetail != nil {
			locationDetailArg = *group.SessionLocationDetail
		}
	}

	var approxValueArg any = nil
	if req.ApproxValue != nil {
		approxValue := strings.TrimSpace(*req.ApproxValue)
		if approxValue != "" {
			if !approxValuePattern.MatchString(approxValue) {
				return models.ApproveUploadGroupResponse{}, errors.New("approx_value must be a non-negative decimal with up to two decimal places")
			}
			approxValueArg = approxValue
		}
	}
	var soldDateArg any = nil
	var soldDate *string
	if req.SoldDate != nil {
		value := strings.TrimSpace(*req.SoldDate)
		if value != "" {
			if _, err := time.Parse("2006-01-02", value); err != nil {
				return models.ApproveUploadGroupResponse{}, errors.New("sold_date must be a YYYY-MM-DD date")
			}
			soldDate = &value
			soldDateArg = value
		}
	}
	notes := ""
	if req.Notes != nil {
		notes = strings.TrimSpace(*req.Notes)
	}

	aiEnriched := group.AIAssistStatus == "succeeded"

	var itemID string
	var approxValue *string
	var createdDatetime time.Time
	if err := tx.QueryRow(ctx, `
		INSERT INTO items (container_id, title, description, ai_enriched, approx_value, inventory_group_id, location_id, location_detail, sold_date, notes)
		VALUES ($1, $2, $3, $4, CASE WHEN $5::text IS NULL THEN NULL ELSE $5::numeric(12,2) END, $6::uuid, $7::uuid, $8, CASE WHEN $9::text IS NULL THEN NULL ELSE $9::date END, $10)
		RETURNING id::text, approx_value::text, created_datetime
	`, containerArg, title, description, aiEnriched, approxValueArg, inventoryGroupID, locationArg, locationDetailArg, soldDateArg, notes).Scan(&itemID, &approxValue, &createdDatetime); err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}

	result, err := tx.Exec(ctx, `
		UPDATE image_assets
		SET item_id = $2,
			updated_datetime = now()
		WHERE upload_group_id = $1
	`, groupID, itemID)
	if err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE upload_groups
		SET inventory_group_id = $2::uuid,
			status = 'approved',
			updated_datetime = now()
		WHERE id = $1
	`, groupID, inventoryGroupID); err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ApproveUploadGroupResponse{}, err
	}

	return models.ApproveUploadGroupResponse{
		Item: models.ReviewItem{
			ID:                 itemID,
			ContainerID:        group.ContainerID,
			LocationID:         group.SessionLocationID,
			LocationName:       locationName,
			LocationDetail:     group.SessionLocationDetail,
			InventoryGroupID:   &inventoryGroupID,
			InventoryGroupCode: &inventoryGroupCode,
			InventoryGroupName: &inventoryGroupName,
			Title:              title,
			Description:        description,
			ApproxValue:        approxValue,
			SoldDate:           soldDate,
			Notes:              notes,
			AiEnriched:         aiEnriched,
			CreatedDatetime:    createdDatetime,
		},
		LinkedImageCount: int(result.RowsAffected()),
	}, nil
}

func (s *ReviewStore) DeletePreview(ctx context.Context, groupID string) (models.ReviewUploadGroupDeletePreview, error) {
	deleteCtx, err := s.buildDeleteContext(ctx, s.pool, groupID, false)
	if err != nil {
		return models.ReviewUploadGroupDeletePreview{}, err
	}
	return deleteCtx.Preview, nil
}

func (s *ReviewStore) Delete(ctx context.Context, groupID string) (models.ReviewUploadGroupDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	deleteCtx, err := s.buildDeleteContext(ctx, tx, groupID, true)
	if err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}

	imageDeleteTag, err := tx.Exec(ctx, `
		DELETE FROM image_assets
		WHERE upload_group_id = $1
	`, groupID)
	if err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}

	groupDeleteTag, err := tx.Exec(ctx, `
		DELETE FROM upload_groups
		WHERE id = $1
	`, groupID)
	if err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}
	if groupDeleteTag.RowsAffected() == 0 {
		return models.ReviewUploadGroupDeleteResponse{}, pgx.ErrNoRows
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM upload_sessions us
		WHERE us.id::text = $1
			AND NOT EXISTS (
				SELECT 1
				FROM upload_groups ug
				WHERE ug.session_id = us.id
			)
			AND NOT EXISTS (
				SELECT 1
				FROM image_assets ia
				WHERE ia.session_id = us.id
			)
	`, deleteCtx.UploadSessionID); err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ReviewUploadGroupDeleteResponse{}, err
	}

	deletedFiles, missingFiles, warnings := s.files.DeleteFiles(deleteCtx.FilePaths, "review discard")

	return models.ReviewUploadGroupDeleteResponse{
		DeletedUploadGroupID:   groupID,
		DeletedImageAssetCount: int(imageDeleteTag.RowsAffected()),
		DeletedFileCount:       deletedFiles,
		MissingFileCount:       missingFiles,
		Warnings:               warnings,
	}, nil
}

func (s *ReviewStore) buildDeleteContext(ctx context.Context, q reviewQuerier, groupID string, lock bool) (reviewDeleteContext, error) {
	group, err := s.loadReviewGroupForDelete(ctx, q, groupID, lock)
	if err != nil {
		return reviewDeleteContext{}, err
	}

	images, err := s.loadReviewImagesForDelete(ctx, q, groupID, lock)
	if err != nil {
		return reviewDeleteContext{}, err
	}

	refs := make([]managedFileReference, 0, len(images)*3)
	for _, image := range images {
		if image.ItemID != nil {
			return reviewDeleteContext{}, errReviewDiscardApproved
		}
		switch image.Status {
		case "pending", "uploaded", "processing":
			return reviewDeleteContext{}, errReviewDiscardInFlight
		}
		refs = append(refs, managedFileReference{
			ImageAssetID: image.ImageAssetID,
			Kind:         "original",
			Path:         image.FilePath,
		})
		if image.ThumbnailPath != nil && strings.TrimSpace(*image.ThumbnailPath) != "" {
			refs = append(refs, managedFileReference{
				ImageAssetID: image.ImageAssetID,
				Kind:         "thumbnail",
				Path:         *image.ThumbnailPath,
			})
		}
		if image.NormalizedPath != nil && strings.TrimSpace(*image.NormalizedPath) != "" {
			refs = append(refs, managedFileReference{
				ImageAssetID: image.ImageAssetID,
				Kind:         "normalized",
				Path:         *image.NormalizedPath,
			})
		}
	}

	inspection, err := s.files.InspectReferences(refs)
	if err != nil {
		return reviewDeleteContext{}, err
	}

	return reviewDeleteContext{
		Preview: models.ReviewUploadGroupDeletePreview{
			UploadGroupID:      groupID,
			UploadSessionID:    group.UploadSessionID,
			ClientGroupID:      group.ClientGroupID,
			Title:              group.Title,
			ImageCount:         len(images),
			FileCount:          len(inspection.Files),
			TotalFileSizeBytes: inspection.TotalFileSizeBytes,
			Warnings: []string{
				"This permanently deletes the review upload group and DB-referenced image files.",
			},
			Files: inspection.Files,
		},
		FilePaths:       inspection.DeletePaths,
		UploadSessionID: group.UploadSessionID,
	}, nil
}

func (s *ReviewStore) queryReviewUploadGroups(ctx context.Context, q reviewQuerier, containerID *string, groupID *string) ([]models.ReviewUploadGroup, error) {
	var containerFilter any
	var groupFilter any
	if containerID != nil {
		containerFilter = *containerID
	}
	if groupID != nil {
		groupFilter = *groupID
	}

	rows, err := q.Query(ctx, `
		WITH reviewable_groups AS (
			SELECT
				ug.id::text,
				ug.session_id::text,
				ug.container_id::text AS container_id,
				us.location_id::text AS location_id,
				loc.name AS location_name,
				us.location_detail,
				ug.inventory_group_id::text AS inventory_group_id,
				ig.code AS inventory_group_code,
				ig.name AS inventory_group_name,
				ug.client_group_id,
				ug.title,
				ug.notes,
				ug.sort_order,
				ug.status,
				ug.created_datetime,
				ug.updated_datetime,
				ug.ai_assist_status,
				ug.ai_assist_error_message,
				ug.ai_assist_requested_datetime,
				ug.ai_assist_started_datetime,
				ug.ai_assist_completed_datetime,
				ug.ai_suggested_title,
				ug.ai_suggested_description,
				ug.ai_suggested_approx_value::text,
				count(ia.id)::int AS image_count,
				count(ia.id) FILTER (WHERE ia.status = 'processed')::int AS processed_image_count,
				count(ia.id) FILTER (WHERE ia.status = 'failed')::int AS failed_image_count
			FROM upload_groups ug
			JOIN upload_sessions us ON us.id = ug.session_id
			LEFT JOIN inventory_groups ig ON ig.id = ug.inventory_group_id
			LEFT JOIN locations loc ON loc.id = us.location_id
			JOIN image_assets ia ON ia.upload_group_id = ug.id
			WHERE ($1::uuid IS NULL OR ug.container_id = $1::uuid)
				AND ($2::uuid IS NULL OR ug.id = $2::uuid)
			GROUP BY
				ug.id,
				ug.session_id,
				ug.container_id,
				us.location_id,
				loc.name,
				us.location_detail,
				ug.inventory_group_id,
				ig.code,
				ig.name,
				ug.client_group_id,
				ug.title,
				ug.notes,
				ug.sort_order,
				ug.status,
				ug.created_datetime,
				ug.updated_datetime,
				ug.ai_assist_status,
				ug.ai_assist_error_message,
				ug.ai_assist_requested_datetime,
				ug.ai_assist_started_datetime,
				ug.ai_assist_completed_datetime,
				ug.ai_suggested_title,
				ug.ai_suggested_description,
				ug.ai_suggested_approx_value
			HAVING count(ia.id) FILTER (WHERE ia.status = 'processed') > 0
				AND count(ia.id) FILTER (WHERE ia.status IN ('pending', 'uploaded', 'processing')) = 0
				AND coalesce(bool_or(ia.item_id IS NOT NULL), false) = false
		)
		SELECT
			rg.id,
			rg.session_id,
			rg.container_id,
			rg.location_id,
			rg.location_name,
			rg.location_detail,
			rg.inventory_group_id,
			rg.inventory_group_code,
			rg.inventory_group_name,
			rg.client_group_id,
			rg.title,
			rg.notes,
			rg.sort_order,
			rg.status,
			rg.created_datetime,
			rg.updated_datetime,
			rg.ai_assist_status,
			rg.ai_assist_error_message,
			rg.ai_assist_requested_datetime,
			rg.ai_assist_started_datetime,
			rg.ai_assist_completed_datetime,
			rg.ai_suggested_title,
			rg.ai_suggested_description,
			rg.ai_suggested_approx_value,
			rg.image_count,
			rg.processed_image_count,
			rg.failed_image_count,
			c.id::text,
			c.name,
			c.type,
			c.location_description
		FROM reviewable_groups rg
		LEFT JOIN containers c ON c.id::text = rg.container_id
		ORDER BY rg.created_datetime DESC, rg.sort_order ASC
	`, containerFilter, groupFilter)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]models.ReviewUploadGroup, 0)
	for rows.Next() {
		var group models.ReviewUploadGroup
		var containerIDValue *string
		var locationID *string
		var locationName *string
		var locationDetail *string
		var containerName *string
		var containerType *string
		var containerLocation *string

		if err := rows.Scan(
			&group.UploadGroupID,
			&group.UploadSessionID,
			&containerIDValue,
			&locationID,
			&locationName,
			&locationDetail,
			&group.InventoryGroupID,
			&group.InventoryGroupCode,
			&group.InventoryGroupName,
			&group.ClientGroupID,
			&group.Title,
			&group.Notes,
			&group.SortOrder,
			&group.Status,
			&group.CreatedDatetime,
			&group.UpdatedDatetime,
			&group.AIAssistStatus,
			&group.AIAssistErrorMessage,
			&group.AIAssistRequestedDatetime,
			&group.AIAssistStartedDatetime,
			&group.AIAssistCompletedDatetime,
			&group.AISuggestedTitle,
			&group.AISuggestedDescription,
			&group.AISuggestedApproxValue,
			&group.ImageCount,
			&group.ProcessedImageCount,
			&group.FailedImageCount,
			&containerIDValue,
			&containerName,
			&containerType,
			&containerLocation,
		); err != nil {
			return nil, err
		}
		group.LocationID = locationID
		group.LocationName = locationName
		group.LocationDetail = locationDetail

		if containerIDValue != nil || containerName != nil || containerType != nil || containerLocation != nil {
			group.Container = &models.ReviewContainer{
				ID:                  derefString(containerIDValue),
				Name:                derefString(containerName),
				Type:                containerType,
				LocationDescription: containerLocation,
			}
		}

		images, err := s.loadReviewImages(ctx, group.UploadGroupID)
		if err != nil {
			return nil, err
		}
		group.Images = images
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

func (s *ReviewStore) lockReviewGroup(ctx context.Context, tx pgx.Tx, groupID string) (reviewGroupRow, error) {
	var group reviewGroupRow
	err := tx.QueryRow(ctx, `
		SELECT ug.session_id::text, ug.container_id::text, us.location_id::text, loc.name, us.location_detail, ug.inventory_group_id::text, ug.client_group_id, ug.title, ug.notes, ug.status, ug.ai_assist_status
		FROM upload_groups ug
		JOIN upload_sessions us ON us.id = ug.session_id
		LEFT JOIN locations loc ON loc.id = us.location_id
		WHERE ug.id = $1
		FOR UPDATE OF ug, us
	`, groupID).Scan(&group.UploadSessionID, &group.ContainerID, &group.SessionLocationID, &group.SessionLocationName, &group.SessionLocationDetail, &group.InventoryGroupID, &group.ClientGroupID, &group.Title, &group.Notes, &group.Status, &group.AIAssistStatus)
	if err != nil {
		return reviewGroupRow{}, err
	}
	return group, nil
}

func (s *ReviewStore) resolveReviewInventoryGroup(ctx context.Context, q reviewQuerier, requestedID *string, currentID *string) (string, string, string, error) {
	inventoryGroupID := currentID
	if requestedID != nil {
		inventoryGroupID = requestedID
	}
	if inventoryGroupID == nil {
		defaultID, err := s.defaultInventoryGroupID(ctx, q)
		if err != nil {
			return "", "", "", err
		}
		inventoryGroupID = &defaultID
	}

	var id string
	var code string
	var name string
	var archived bool
	if err := q.QueryRow(ctx, `
		SELECT id::text, code, name, archived
		FROM inventory_groups
		WHERE id = $1::uuid
	`, *inventoryGroupID).Scan(&id, &code, &name, &archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", "", errItemGroupNotFound
		}
		return "", "", "", err
	}
	if archived {
		return "", "", "", errArchivedItemGroup
	}
	return id, code, name, nil
}

func (s *ReviewStore) defaultInventoryGroupID(ctx context.Context, q reviewQuerier) (string, error) {
	var id string
	if err := q.QueryRow(ctx, `
		SELECT id::text
		FROM inventory_groups
		WHERE code = $1
	`, defaultInventoryGroupCode).Scan(&id); err != nil {
		return "", err
	}
	return id, nil
}

func (s *ReviewStore) loadReviewGroupForDelete(ctx context.Context, q reviewQuerier, groupID string, lock bool) (reviewGroupRow, error) {
	query := `
		SELECT ug.session_id::text, ug.container_id::text, us.location_id::text, loc.name, us.location_detail, ug.inventory_group_id::text, ug.client_group_id, ug.title, ug.notes, ug.status, ug.ai_assist_status
		FROM upload_groups ug
		JOIN upload_sessions us ON us.id = ug.session_id
		LEFT JOIN locations loc ON loc.id = us.location_id
		WHERE ug.id = $1
	`
	if lock {
		query += ` FOR UPDATE OF ug, us`
	}

	var group reviewGroupRow
	if err := q.QueryRow(ctx, query, groupID).Scan(
		&group.UploadSessionID,
		&group.ContainerID,
		&group.SessionLocationID,
		&group.SessionLocationName,
		&group.SessionLocationDetail,
		&group.InventoryGroupID,
		&group.ClientGroupID,
		&group.Title,
		&group.Notes,
		&group.Status,
		&group.AIAssistStatus,
	); err != nil {
		return reviewGroupRow{}, err
	}
	return group, nil
}

func (s *ReviewStore) loadReviewImages(ctx context.Context, groupID string) ([]models.ReviewImageAsset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			original_filename,
			stored_filename,
			file_path,
			thumbnail_path,
			normalized_path,
			mime_type,
			file_size_bytes,
			status,
			upload_order
		FROM image_assets
		WHERE upload_group_id = $1
		ORDER BY upload_order ASC, created_datetime ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]models.ReviewImageAsset, 0)
	for rows.Next() {
		var image models.ReviewImageAsset
		if err := rows.Scan(
			&image.ImageAssetID,
			&image.OriginalFilename,
			&image.StoredFilename,
			&image.FilePath,
			&image.ThumbnailPath,
			&image.NormalizedPath,
			&image.MimeType,
			&image.FileSizeBytes,
			&image.Status,
			&image.UploadOrder,
		); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func (s *ReviewStore) loadReviewImagesForTx(ctx context.Context, tx pgx.Tx, groupID string) ([]reviewImageRow, error) {
	return s.loadReviewImagesForDelete(ctx, tx, groupID, true)
}

func (s *ReviewStore) loadReviewImagesForDelete(ctx context.Context, q reviewQuerier, groupID string, lock bool) ([]reviewImageRow, error) {
	query := `
		SELECT
			id::text,
			original_filename,
			stored_filename,
			file_path,
			thumbnail_path,
			normalized_path,
			mime_type,
			file_size_bytes,
			status,
			upload_order,
			item_id::text
		FROM image_assets
		WHERE upload_group_id = $1
		ORDER BY upload_order ASC, created_datetime ASC
	`
	if lock {
		query += ` FOR UPDATE`
	}

	rows, err := q.Query(ctx, query, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]reviewImageRow, 0)
	for rows.Next() {
		var image reviewImageRow
		if err := rows.Scan(
			&image.ImageAssetID,
			&image.OriginalFilename,
			&image.StoredFilename,
			&image.FilePath,
			&image.ThumbnailPath,
			&image.NormalizedPath,
			&image.MimeType,
			&image.FileSizeBytes,
			&image.Status,
			&image.UploadOrder,
			&image.ItemID,
		); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func (s *ReviewStore) loadReviewDeleteImageRecord(ctx context.Context, q reviewQuerier, groupID string, imageID string, lock bool) (reviewImageRow, error) {
	query := `
		SELECT
			id::text,
			original_filename,
			stored_filename,
			file_path,
			thumbnail_path,
			normalized_path,
			mime_type,
			file_size_bytes,
			status,
			upload_order,
			item_id::text
		FROM image_assets
		WHERE id = $1::uuid
			AND upload_group_id = $2::uuid
	`
	if lock {
		query += ` FOR UPDATE`
	}

	var image reviewImageRow
	if err := q.QueryRow(ctx, query, imageID, groupID).Scan(
		&image.ImageAssetID,
		&image.OriginalFilename,
		&image.StoredFilename,
		&image.FilePath,
		&image.ThumbnailPath,
		&image.NormalizedPath,
		&image.MimeType,
		&image.FileSizeBytes,
		&image.Status,
		&image.UploadOrder,
		&image.ItemID,
	); err != nil {
		return reviewImageRow{}, err
	}

	return image, nil
}

func (s *ReviewStore) regenerateReviewImageVariants(record itemImageUploadRecord) (string, string, error) {
	cleanOriginalPath := filepath.Clean(strings.TrimSpace(record.FilePath))
	if !isSafeManagedPath(cleanOriginalPath, s.files.safeRoots) {
		return "", "", fmt.Errorf("%w: %s", errUnsafeManagedFilePath, cleanOriginalPath)
	}

	sourceFile, err := os.Open(cleanOriginalPath)
	if err != nil {
		return "", "", err
	}
	defer sourceFile.Close()

	thumbnailPath := filepath.Join(s.imageConfig.ThumbnailsDir, record.StoredFilename)
	normalizedPath := filepath.Join(s.imageConfig.NormalizedDir, record.StoredFilename)

	srcImage, imageFormat, err := image.Decode(sourceFile)
	if err != nil {
		log.Printf("review image upload using original file as fallback variants image_asset_id=%s error=%v", record.ID, err)
		if err := copyImageVariantFile(cleanOriginalPath, thumbnailPath); err != nil {
			return "", "", err
		}
		if err := copyImageVariantFile(cleanOriginalPath, normalizedPath); err != nil {
			return "", "", err
		}
		return thumbnailPath, normalizedPath, nil
	}

	if err := writeVariantImage(srcImage, imageFormat, thumbnailPath, defaultThumbnailMaxEdge); err != nil {
		return "", "", err
	}

	if err := writeVariantImage(srcImage, imageFormat, normalizedPath, defaultNormalizedMaxEdge); err != nil {
		return "", "", err
	}

	return thumbnailPath, normalizedPath, nil
}

func (h *ReviewHandler) ListUploadGroups(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(r.URL.Query().Get("container_id"))
	if containerID != "" && !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container_id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var containerFilter *string
	if containerID != "" {
		containerFilter = &containerID
	}

	groups, err := h.store.ListUploadGroups(ctx, containerFilter)
	if err != nil {
		log.Printf("failed to load review queue: %v", err)
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load review queue")
		return
	}

	respond.JSON(w, http.StatusOK, groups)
}

func (h *ReviewHandler) GetUploadGroup(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "group id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	group, err := h.store.GetUploadGroup(ctx, groupID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
			return
		}
		log.Printf("failed to load review upload group %s: %v", groupID, err)
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load review upload group")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetReviewUploadGroupResponse{Group: group})
}

func (h *ReviewHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "group id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	preview, err := h.store.DeletePreview(ctx, groupID)
	if err != nil {
		log.Printf("failed to load review upload group delete preview %s: %v", groupID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
		case errors.Is(err, errReviewDiscardApproved), errors.Is(err, errReviewDiscardInFlight), errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load review upload group delete preview")
		}
		return
	}

	respond.JSON(w, http.StatusOK, preview)
}

func (h *ReviewHandler) QueueAIAssist(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "group id must be a valid UUID")
		return
	}

	var req models.QueueReviewAIAssistRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}
	normalizeQueueReviewAIAssistRequest(&req)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.QueueAIAssist(ctx, groupID, req)
	if err != nil {
		log.Printf("failed to queue AI assist for upload group %s: %v", groupID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
		case errors.Is(err, errAlreadyApproved):
			respond.ErrorCode(w, http.StatusConflict, "conflict", "upload group is already approved")
		case errors.Is(err, errAIAssistAlreadyRunning):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		}
		return
	}

	respond.JSON(w, http.StatusAccepted, response)
}

func (h *ReviewHandler) ApproveUploadGroup(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "group id must be a valid UUID")
		return
	}

	var req models.ApproveUploadGroupRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}
	normalizeApproveUploadGroupRequest(&req)
	if err := validateApproveUploadGroupRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.Approve(ctx, groupID, req)
	if err != nil {
		log.Printf("failed to approve review upload group %s: %v", groupID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
		case errors.Is(err, errAlreadyApproved):
			respond.ErrorCode(w, http.StatusConflict, "conflict", "upload group is already approved")
		case errors.Is(err, errAIAssistApprovalBlocked):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, errItemGroupNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
		case errors.Is(err, errArchivedItemGroup):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign item to archived inventory group.")
		default:
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *ReviewHandler) DeleteUploadGroup(w http.ResponseWriter, r *http.Request) {
	groupID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(groupID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "group id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.store.Delete(ctx, groupID)
	if err != nil {
		log.Printf("failed to discard review upload group %s: %v", groupID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload group was not found")
		case errors.Is(err, errReviewDiscardApproved), errors.Is(err, errReviewDiscardInFlight), errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to discard review upload group")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func normalizeApproveUploadGroupRequest(req *models.ApproveUploadGroupRequest) {
	req.Title = trimOptionalString(req.Title)
	req.Description = trimOptionalString(req.Description)
	req.ApproxValue = trimOptionalString(req.ApproxValue)
	req.InventoryGroupID = trimOptionalString(req.InventoryGroupID)
}

func normalizeQueueReviewAIAssistRequest(req *models.QueueReviewAIAssistRequest) {
	req.UserHint = trimOptionalString(req.UserHint)
}

func validateApproveUploadGroupRequest(req models.ApproveUploadGroupRequest) error {
	if req.ApproxValue != nil && !approxValuePattern.MatchString(*req.ApproxValue) {
		return errors.New("approx_value must be a non-negative decimal with up to two decimal places")
	}
	if req.InventoryGroupID != nil && !isUUID(*req.InventoryGroupID) {
		return errors.New("inventory_group_id must be a valid UUID")
	}

	return nil
}
