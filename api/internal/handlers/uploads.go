package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fastsell-api/internal/respond"
	"fastsell-api/internal/upload"
	"fastsell-api/internal/uploadstatus"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const metadataFieldName = "metadata"

type UploadHandler struct {
	pool             *pgxpool.Pool
	intakeDir        string
	maxUploadBytes   int64
	parseMemoryBytes int64
}

func NewUploadHandler(pool *pgxpool.Pool, intakeDir string, maxUploadMB int64) *UploadHandler {
	return &UploadHandler{
		pool:             pool,
		intakeDir:        intakeDir,
		maxUploadBytes:   maxUploadMB * 1024 * 1024,
		parseMemoryBytes: 32 << 20,
	}
}

type groupedUploadRequest struct {
	IntakeContext intakeContext      `json:"intake_context"`
	SessionNotes  *string            `json:"session_notes"`
	Groups        []uploadGroupInput `json:"groups"`
}

type intakeContext struct {
	ContainerID    string `json:"container_id"`
	ContainerName  string `json:"container_name"`
	NoContainer    bool   `json:"no_container"`
	LocationID     string `json:"location_id"`
	LocationDetail string `json:"location_detail"`
}

type uploadGroupInput struct {
	ClientGroupID    string            `json:"client_group_id"`
	InventoryGroupID *string           `json:"inventory_group_id"`
	Title            *string           `json:"title"`
	Notes            *string           `json:"notes"`
	Files            []uploadFileInput `json:"files"`
}

type uploadFileInput struct {
	ClientFileID     string `json:"client_file_id"`
	OriginalFilename string `json:"original_filename"`
	MimeType         string `json:"mime_type"`
	SizeBytes        int64  `json:"size_bytes"`
}

type uploadResponse struct {
	UploadSessionID string                `json:"upload_session_id"`
	Status          string                `json:"status"`
	Groups          []uploadGroupResponse `json:"groups"`
}

type uploadGroupResponse struct {
	UploadGroupID    string               `json:"upload_group_id"`
	ClientGroupID    string               `json:"client_group_id"`
	InventoryGroupID string               `json:"inventory_group_id"`
	Title            *string              `json:"title"`
	Files            []uploadFileResponse `json:"files"`
}

type uploadFileResponse struct {
	ImageAssetID     string `json:"image_asset_id"`
	ClientFileID     string `json:"client_file_id"`
	OriginalFilename string `json:"original_filename"`
	StoredFilename   string `json:"stored_filename"`
	Status           string `json:"status"`
}

type uploadSessionStatusResponse struct {
	UploadSessionID string                      `json:"upload_session_id"`
	Status          string                      `json:"status"`
	Groups          []uploadGroupStatusResponse `json:"groups"`
}

type nextUploadItemNumberResponse struct {
	NextNumber     int    `json:"next_number"`
	Step           int    `json:"step"`
	TitlePrefix    string `json:"title_prefix"`
	SuggestedTitle string `json:"suggested_title"`
	Scope          string `json:"scope"`
}

type uploadGroupStatusResponse struct {
	UploadGroupID      string                     `json:"upload_group_id"`
	ClientGroupID      *string                    `json:"client_group_id"`
	InventoryGroupID   *string                    `json:"inventory_group_id"`
	InventoryGroupCode *string                    `json:"inventory_group_code"`
	InventoryGroupName *string                    `json:"inventory_group_name"`
	Title              *string                    `json:"title"`
	Notes              *string                    `json:"notes"`
	SortOrder          int                        `json:"sort_order"`
	Files              []uploadFileStatusResponse `json:"files"`
}

type uploadFileStatusResponse struct {
	ImageAssetID     string  `json:"image_asset_id"`
	ClientFileID     *string `json:"client_file_id"`
	OriginalFilename *string `json:"original_filename"`
	StoredFilename   *string `json:"stored_filename"`
	FilePath         string  `json:"file_path"`
	MimeType         *string `json:"mime_type"`
	FileSizeBytes    *int64  `json:"file_size_bytes"`
	UploadOrder      int     `json:"upload_order"`
	Status           string  `json:"status"`
	ErrorMessage     *string `json:"error_message"`
}

type writtenUploadFile struct {
	path string
}

type savedUploadFile struct {
	originalFilename string
	storedFilename   string
	filePath         string
	mimeType         string
	sizeBytes        int64
	hashHex          string
}

func (h *UploadHandler) CreateImages(w http.ResponseWriter, r *http.Request) {
	if err := os.MkdirAll(h.intakeDir, 0755); err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_unavailable", "failed to prepare intake directory")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.maxBodyBytes())
	if err := r.ParseMultipartForm(h.parseMemoryBytes); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid multipart form data")
		return
	}

	req, err := parseUploadMetadata(r.MultipartForm)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	normalizeGroupedUploadRequest(&req)
	if err := validateGroupedUploadRequest(req, h.maxUploadBytes, r.MultipartForm); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	tx, err := h.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to start upload transaction")
		return
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	writtenFiles := make([]writtenUploadFile, 0)
	response, err := h.insertGroupedUpload(ctx, tx, req, r.MultipartForm, &writtenFiles)
	if err != nil {
		log.Printf("grouped image upload failed: %v", err)
		cleanupWrittenFiles(writtenFiles)
		if errors.Is(err, errInvalidUploadRequest) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		if errors.Is(err, errArchivedLocation) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign upload session to archived location.")
			return
		}
		if errors.Is(err, errItemGroupNotFound) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
			return
		}
		if errors.Is(err, errArchivedItemGroup) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign upload group to archived inventory group.")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "upload_failed", "failed to store upload")
		return
	}

	if err := tx.Commit(ctx); err != nil {
		cleanupWrittenFiles(writtenFiles)
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to commit upload")
		return
	}

	respond.JSON(w, http.StatusCreated, response)
}

func (h *UploadHandler) GetSession(w http.ResponseWriter, r *http.Request) {
	sessionID := strings.TrimSpace(chi.URLParam(r, "id"))
	if sessionID == "" {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "upload session id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response, err := h.fetchUploadSessionStatus(ctx, sessionID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "upload session was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load upload session")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *UploadHandler) GetNextItemNumber(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(r.URL.Query().Get("container_id"))
	if containerID != "" && !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container_id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	scope := "global"
	if containerID != "" {
		scope = "container"
		exists, err := h.containerExists(ctx, containerID)
		if err != nil {
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to validate container")
			return
		}
		if !exists {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			return
		}
	}

	maxNumber, err := h.maxExistingItemNumber(ctx, containerID)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to calculate next item number")
		return
	}

	const step = 10
	const titlePrefix = "Item"
	nextNumber := ((maxNumber / step) + 1) * step
	suggestedTitle := fmt.Sprintf("%s %d", titlePrefix, nextNumber)

	respond.JSON(w, http.StatusOK, nextUploadItemNumberResponse{
		NextNumber:     nextNumber,
		Step:           step,
		TitlePrefix:    titlePrefix,
		SuggestedTitle: suggestedTitle,
		Scope:          scope,
	})
}

var errInvalidUploadRequest = errors.New("invalid upload request")

func parseUploadMetadata(form *multipart.Form) (groupedUploadRequest, error) {
	var req groupedUploadRequest
	if form == nil {
		return req, errors.New("metadata is required")
	}

	values := form.Value[metadataFieldName]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return req, errors.New("metadata is required")
	}

	decoder := json.NewDecoder(strings.NewReader(values[0]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, errors.New("metadata must be valid JSON")
	}

	return req, nil
}

func normalizeGroupedUploadRequest(req *groupedUploadRequest) {
	req.IntakeContext.ContainerID = strings.TrimSpace(req.IntakeContext.ContainerID)
	req.IntakeContext.ContainerName = strings.TrimSpace(req.IntakeContext.ContainerName)
	req.IntakeContext.LocationID = strings.TrimSpace(req.IntakeContext.LocationID)
	req.IntakeContext.LocationDetail = strings.TrimSpace(req.IntakeContext.LocationDetail)
	req.SessionNotes = trimOptionalString(req.SessionNotes)

	for groupIndex := range req.Groups {
		group := &req.Groups[groupIndex]
		group.ClientGroupID = strings.TrimSpace(group.ClientGroupID)
		group.InventoryGroupID = trimOptionalString(group.InventoryGroupID)
		group.Title = trimOptionalString(group.Title)
		group.Notes = trimOptionalString(group.Notes)

		for fileIndex := range group.Files {
			file := &group.Files[fileIndex]
			file.ClientFileID = strings.TrimSpace(file.ClientFileID)
			file.OriginalFilename = cleanOriginalFilename(file.OriginalFilename)
			file.MimeType = strings.ToLower(strings.TrimSpace(file.MimeType))
		}
	}
}

func validateGroupedUploadRequest(req groupedUploadRequest, maxUploadBytes int64, form *multipart.Form) error {
	if len(req.Groups) == 0 {
		return errors.New("at least one group is required")
	}
	if req.IntakeContext.LocationID != "" && !isUUID(req.IntakeContext.LocationID) {
		return errors.New("intake_context.location_id must be a valid UUID")
	}

	fileCount := 0
	seenGroups := make(map[string]struct{})
	seenFiles := make(map[string]struct{})
	for groupIndex, group := range req.Groups {
		if group.ClientGroupID == "" {
			return fmt.Errorf("groups[%d].client_group_id is required", groupIndex)
		}
		if _, exists := seenGroups[group.ClientGroupID]; exists {
			return fmt.Errorf("duplicate client_group_id %q", group.ClientGroupID)
		}
		seenGroups[group.ClientGroupID] = struct{}{}
		if group.InventoryGroupID != nil && !isUUID(*group.InventoryGroupID) {
			return fmt.Errorf("groups[%d].inventory_group_id must be a valid UUID", groupIndex)
		}

		for fileIndex, file := range group.Files {
			fileCount++
			if file.ClientFileID == "" {
				return fmt.Errorf("groups[%d].files[%d].client_file_id is required", groupIndex, fileIndex)
			}
			if _, exists := seenFiles[file.ClientFileID]; exists {
				return fmt.Errorf("duplicate client_file_id %q", file.ClientFileID)
			}
			seenFiles[file.ClientFileID] = struct{}{}

			if file.SizeBytes > maxUploadBytes {
				return fmt.Errorf("file %q exceeds max upload size", file.ClientFileID)
			}
			if !upload.IsAllowedImage(file.OriginalFilename, file.MimeType) {
				return fmt.Errorf("file %q must be jpeg, png, heic, or heif", file.ClientFileID)
			}

			partName := multipartFileFieldName(file.ClientFileID)
			parts := form.File[partName]
			if len(parts) == 0 {
				return fmt.Errorf("missing multipart file part %q", partName)
			}
			if len(parts) > 1 {
				return fmt.Errorf("multipart file part %q must appear once", partName)
			}
			if parts[0].Size > maxUploadBytes {
				return fmt.Errorf("file %q exceeds max upload size", file.ClientFileID)
			}
		}
	}

	if fileCount == 0 {
		return errors.New("at least one file is required")
	}

	return nil
}

func (h *UploadHandler) insertGroupedUpload(ctx context.Context, tx pgx.Tx, req groupedUploadRequest, form *multipart.Form, writtenFiles *[]writtenUploadFile) (uploadResponse, error) {
	var containerID *string
	if !req.IntakeContext.NoContainer && req.IntakeContext.ContainerID != "" {
		containerID = &req.IntakeContext.ContainerID
	}

	var locationID *string
	var locationDetail *string
	if req.IntakeContext.NoContainer {
		if req.IntakeContext.LocationID != "" {
			locationID = &req.IntakeContext.LocationID
			if err := h.ensureUploadLocation(ctx, tx, *locationID); err != nil {
				return uploadResponse{}, err
			}
		}
		if req.IntakeContext.LocationDetail != "" {
			locationDetail = &req.IntakeContext.LocationDetail
		}
	}

	var sessionID string
	err := tx.QueryRow(ctx, `
		INSERT INTO upload_sessions (container_id, location_id, location_detail, source, notes, status)
		VALUES ($1, $2::uuid, $3, 'web_upload', $4, 'pending')
		RETURNING id::text
	`, containerID, locationID, locationDetail, req.SessionNotes).Scan(&sessionID)
	if err != nil {
		return uploadResponse{}, err
	}

	response := uploadResponse{
		UploadSessionID: sessionID,
		Status:          "pending",
		Groups:          make([]uploadGroupResponse, 0, len(req.Groups)),
	}

	for groupIndex, group := range req.Groups {
		inventoryGroupID, err := resolveUploadInventoryGroup(ctx, tx, group.InventoryGroupID)
		if err != nil {
			return uploadResponse{}, err
		}

		var groupID string
		err = tx.QueryRow(ctx, `
			INSERT INTO upload_groups (session_id, container_id, inventory_group_id, client_group_id, title, notes, sort_order, status)
			VALUES ($1, $2, $3::uuid, $4, $5, $6, $7, 'pending')
			RETURNING id::text
		`, sessionID, containerID, inventoryGroupID, group.ClientGroupID, group.Title, group.Notes, groupIndex).Scan(&groupID)
		if err != nil {
			return uploadResponse{}, err
		}

		groupResponse := uploadGroupResponse{
			UploadGroupID:    groupID,
			ClientGroupID:    group.ClientGroupID,
			InventoryGroupID: inventoryGroupID,
			Title:            group.Title,
			Files:            make([]uploadFileResponse, 0, len(group.Files)),
		}

		for fileIndex, file := range group.Files {
			saved, err := h.saveMultipartFile(file, form.File[multipartFileFieldName(file.ClientFileID)][0], writtenFiles)
			if err != nil {
				return uploadResponse{}, err
			}

			var imageAssetID string
			err = tx.QueryRow(ctx, `
				INSERT INTO image_assets (
					session_id,
					upload_group_id,
					client_file_id,
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
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, true, 'pending')
				RETURNING id::text
			`,
				sessionID,
				groupID,
				file.ClientFileID,
				saved.originalFilename,
				saved.storedFilename,
				saved.filePath,
				saved.hashHex,
				saved.mimeType,
				saved.sizeBytes,
				fileIndex,
			).Scan(&imageAssetID)
			if err != nil {
				return uploadResponse{}, err
			}

			groupResponse.Files = append(groupResponse.Files, uploadFileResponse{
				ImageAssetID:     imageAssetID,
				ClientFileID:     file.ClientFileID,
				OriginalFilename: saved.originalFilename,
				StoredFilename:   saved.storedFilename,
				Status:           "pending",
			})
		}

		response.Groups = append(response.Groups, groupResponse)
	}

	status, err := uploadstatus.Recalculate(ctx, tx, sessionID)
	if err != nil {
		return uploadResponse{}, err
	}
	response.Status = status

	return response, nil
}

func (h *UploadHandler) ensureUploadLocation(ctx context.Context, q reviewQuerier, locationID string) error {
	var archived bool
	if err := q.QueryRow(ctx, `
		SELECT archived
		FROM locations
		WHERE id = $1::uuid
	`, locationID).Scan(&archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: location was not found", errInvalidUploadRequest)
		}
		return err
	}
	if archived {
		return errArchivedLocation
	}
	return nil
}

func resolveUploadInventoryGroup(ctx context.Context, q reviewQuerier, requestedID *string) (string, error) {
	if requestedID == nil {
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

	var id string
	var archived bool
	if err := q.QueryRow(ctx, `
		SELECT id::text, archived
		FROM inventory_groups
		WHERE id = $1::uuid
	`, *requestedID).Scan(&id, &archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errItemGroupNotFound
		}
		return "", err
	}
	if archived {
		return "", errArchivedItemGroup
	}
	return id, nil
}

func (h *UploadHandler) fetchUploadSessionStatus(ctx context.Context, sessionID string) (uploadSessionStatusResponse, error) {
	var response uploadSessionStatusResponse
	err := h.pool.QueryRow(ctx, `
		SELECT id::text, status
		FROM upload_sessions
		WHERE id = $1
	`, sessionID).Scan(&response.UploadSessionID, &response.Status)
	if err != nil {
		return uploadSessionStatusResponse{}, err
	}

	groupRows, err := h.pool.Query(ctx, `
		SELECT ug.id::text, ug.client_group_id, ug.inventory_group_id::text, ig.code, ig.name, ug.title, ug.notes, ug.sort_order
		FROM upload_groups ug
		LEFT JOIN inventory_groups ig ON ig.id = ug.inventory_group_id
		WHERE ug.session_id = $1
		ORDER BY ug.sort_order ASC, ug.created_datetime ASC
	`, sessionID)
	if err != nil {
		return uploadSessionStatusResponse{}, err
	}
	defer groupRows.Close()

	groups := make([]uploadGroupStatusResponse, 0)
	groupIndexes := make(map[string]int)
	for groupRows.Next() {
		var group uploadGroupStatusResponse
		if err := groupRows.Scan(&group.UploadGroupID, &group.ClientGroupID, &group.InventoryGroupID, &group.InventoryGroupCode, &group.InventoryGroupName, &group.Title, &group.Notes, &group.SortOrder); err != nil {
			return uploadSessionStatusResponse{}, err
		}
		group.Files = make([]uploadFileStatusResponse, 0)
		groupIndexes[group.UploadGroupID] = len(groups)
		groups = append(groups, group)
	}
	if err := groupRows.Err(); err != nil {
		return uploadSessionStatusResponse{}, err
	}

	fileRows, err := h.pool.Query(ctx, `
		SELECT
			id::text,
			upload_group_id::text,
			client_file_id,
			original_filename,
			stored_filename,
			file_path,
			mime_type,
			file_size_bytes,
			upload_order,
			status,
			error_message
		FROM image_assets
		WHERE session_id = $1
		ORDER BY upload_group_id, upload_order ASC, created_datetime ASC
	`, sessionID)
	if err != nil {
		return uploadSessionStatusResponse{}, err
	}
	defer fileRows.Close()

	for fileRows.Next() {
		var groupID *string
		var file uploadFileStatusResponse
		if err := fileRows.Scan(
			&file.ImageAssetID,
			&groupID,
			&file.ClientFileID,
			&file.OriginalFilename,
			&file.StoredFilename,
			&file.FilePath,
			&file.MimeType,
			&file.FileSizeBytes,
			&file.UploadOrder,
			&file.Status,
			&file.ErrorMessage,
		); err != nil {
			return uploadSessionStatusResponse{}, err
		}

		if groupID == nil {
			continue
		}
		if groupIndex, ok := groupIndexes[*groupID]; ok {
			groups[groupIndex].Files = append(groups[groupIndex].Files, file)
		}
	}
	if err := fileRows.Err(); err != nil {
		return uploadSessionStatusResponse{}, err
	}

	response.Groups = groups
	return response, nil
}

func (h *UploadHandler) saveMultipartFile(file uploadFileInput, header *multipart.FileHeader, writtenFiles *[]writtenUploadFile) (savedUploadFile, error) {
	if header.Size > h.maxUploadBytes {
		return savedUploadFile{}, fmt.Errorf("%w: file %q exceeds max upload size", errInvalidUploadRequest, file.ClientFileID)
	}

	return saveUploadMultipartFile(h.intakeDir, h.maxUploadBytes, file, header, writtenFiles)
}

func saveUploadMultipartFile(intakeDir string, maxUploadBytes int64, file uploadFileInput, header *multipart.FileHeader, writtenFiles *[]writtenUploadFile) (savedUploadFile, error) {
	src, err := header.Open()
	if err != nil {
		return savedUploadFile{}, err
	}
	defer src.Close()

	storedFilename, err := upload.NewStoredFilename(file.OriginalFilename, file.MimeType)
	if err != nil {
		return savedUploadFile{}, err
	}

	destinationPath := filepath.Join(intakeDir, storedFilename)
	dst, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return savedUploadFile{}, err
	}

	hasher := sha256.New()
	limitedReader := io.LimitReader(src, maxUploadBytes+1)
	bytesWritten, copyErr := io.Copy(dst, io.TeeReader(limitedReader, hasher))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destinationPath)
		return savedUploadFile{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destinationPath)
		return savedUploadFile{}, closeErr
	}
	if bytesWritten > maxUploadBytes {
		_ = os.Remove(destinationPath)
		return savedUploadFile{}, fmt.Errorf("%w: file %q exceeds max upload size", errInvalidUploadRequest, file.ClientFileID)
	}

	*writtenFiles = append(*writtenFiles, writtenUploadFile{path: destinationPath})

	mimeType := file.MimeType
	if mimeType == "" {
		mimeType = header.Header.Get("Content-Type")
	}

	return savedUploadFile{
		originalFilename: file.OriginalFilename,
		storedFilename:   storedFilename,
		filePath:         destinationPath,
		mimeType:         mimeType,
		sizeBytes:        bytesWritten,
		hashHex:          hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func multipartFileFieldName(clientFileID string) string {
	return "file_" + clientFileID
}

func (h *UploadHandler) containerExists(ctx context.Context, containerID string) (bool, error) {
	var exists bool
	err := h.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM containers
			WHERE id = $1::uuid
		)
	`, containerID).Scan(&exists)
	return exists, err
}

func (h *UploadHandler) maxExistingItemNumber(ctx context.Context, containerID string) (int, error) {
	args := []any{}
	uploadGroupsWhere := ""
	itemsWhere := ""
	if containerID != "" {
		args = append(args, containerID)
		uploadGroupsWhere = "WHERE us.container_id = $1::uuid"
		itemsWhere = "WHERE i.container_id = $1::uuid"
	}

	query := fmt.Sprintf(`
		WITH numbered_titles AS (
			SELECT ((regexp_match(ug.title, '(?i)^item[[:space:]]+([0-9]+)$'))[1])::int AS item_number
			FROM upload_groups ug
			JOIN upload_sessions us ON us.id = ug.session_id
			%s
			AND ug.title ~* '^item[[:space:]]+[0-9]+$'

			UNION ALL

			SELECT ((regexp_match(i.title, '(?i)^item[[:space:]]+([0-9]+)$'))[1])::int AS item_number
			FROM items i
			%s
			AND i.title ~* '^item[[:space:]]+[0-9]+$'
		)
		SELECT COALESCE(MAX(item_number), 0)
		FROM numbered_titles
	`, withOptionalWhere(uploadGroupsWhere), withOptionalWhere(itemsWhere))

	var maxNumber int
	if err := h.pool.QueryRow(ctx, query, args...).Scan(&maxNumber); err != nil {
		return 0, err
	}

	return maxNumber, nil
}

func withOptionalWhere(where string) string {
	if strings.TrimSpace(where) == "" {
		return "WHERE true"
	}
	return where
}

func cleanOriginalFilename(value string) string {
	filename := strings.TrimSpace(value)
	filename = strings.ReplaceAll(filename, "\\", "/")
	filename = filepath.Base(filename)
	if filename == "." || filename == "/" {
		return ""
	}
	return filename
}

func cleanupWrittenFiles(files []writtenUploadFile) {
	for _, file := range files {
		_ = os.Remove(file.path)
	}
}

func (h *UploadHandler) maxBodyBytes() int64 {
	const maxFormOverheadBytes = 10 << 20
	const maxExpectedFiles = 100
	return h.maxUploadBytes*maxExpectedFiles + maxFormOverheadBytes
}
