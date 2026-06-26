package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"
	"fastsell-api/internal/upload"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

const (
	itemImageMultipartField     = "images"
	defaultThumbnailMaxEdge     = 320
	defaultNormalizedMaxEdge    = 1600
	itemImageUploadFormOverhead = 4 << 20
)

type itemImageUploadRecord struct {
	ID             string
	FilePath       string
	StoredFilename string
	MimeType       *string
}

type writtenItemImageFile struct {
	path string
}

type savedItemImageFile struct {
	originalFilename string
	storedFilename   string
	filePath         string
	mimeType         string
	sizeBytes        int64
	hashHex          string
}

type itemImageVariantUpdate struct {
	ImageAssetID   string
	ThumbnailPath  string
	NormalizedPath string
}

type ItemImageStorageConfig struct {
	OriginalsDir     string
	ThumbnailsDir    string
	NormalizedDir    string
	MaxUploadBytes   int64
	MaxImagesPerItem int
}

func NewItemImageStorageConfig(originalsDir string, thumbnailsDir string, normalizedDir string, maxUploadMB int64, maxImagesPerItem int) ItemImageStorageConfig {
	return ItemImageStorageConfig{
		OriginalsDir:     originalsDir,
		ThumbnailsDir:    thumbnailsDir,
		NormalizedDir:    normalizedDir,
		MaxUploadBytes:   maxUploadMB * 1024 * 1024,
		MaxImagesPerItem: maxImagesPerItem,
	}
}

func (h *ItemHandler) UploadImages(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("item image upload panic item_id=%s panic=%v", itemID, recovered)
			panic(recovered)
		}
	}()

	if err := h.store.ensureImageDirectories(); err != nil {
		log.Printf("item image upload storage prepare failed item_id=%s error=%v", itemID, err)
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_unavailable", "failed to prepare image directories")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.store.maxItemImageBodyBytes())
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
		saved, err := h.store.saveItemImageUpload(header, &writtenFiles)
		if err != nil {
			cleanupWrittenItemFiles(writtenFiles)
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		savedFiles = append(savedFiles, saved)
	}

	item, err := h.store.AppendImages(ctx, itemID, savedFiles)
	if err != nil {
		log.Printf("item image upload failed item_id=%s error=%v", itemID, err)
		cleanupWrittenItemFiles(writtenFiles)
		h.store.cleanupSavedItemVariants(savedFiles)
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
			return
		}
		if errors.Is(err, errInvalidUploadRequest) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "upload_failed", "failed to upload item images")
		return
	}

	respond.JSON(w, http.StatusCreated, models.GetItemResponse{Item: item})
}

func (h *ItemHandler) DeleteImage(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	imageID := strings.TrimSpace(chi.URLParam(r, "image"))
	if !isUUID(imageID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "image id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.store.DeleteImage(ctx, itemID, imageID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item image was not found")
		case errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "delete_failed", "failed to delete item image")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (s *ItemStore) ensureImageDirectories() error {
	for _, dir := range []string{s.imageConfig.OriginalsDir, s.imageConfig.ThumbnailsDir, s.imageConfig.NormalizedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (s *ItemStore) cleanupSavedItemVariants(savedFiles []savedItemImageFile) {
	for _, file := range savedFiles {
		_ = os.Remove(filepath.Join(s.imageConfig.ThumbnailsDir, file.storedFilename))
		_ = os.Remove(filepath.Join(s.imageConfig.NormalizedDir, file.storedFilename))
	}
}

func (s *ItemStore) maxItemImageBodyBytes() int64 {
	const maxExpectedFiles = 20
	return int64(maxExpectedFiles)*s.imageConfig.MaxUploadBytes + itemImageUploadFormOverhead
}

func (s *ItemStore) saveItemImageUpload(header *multipart.FileHeader, writtenFiles *[]writtenItemImageFile) (savedItemImageFile, error) {
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
	dst, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
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

func (s *ItemStore) AppendImages(ctx context.Context, itemID string, savedFiles []savedItemImageFile) (models.InventoryItemDetail, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := s.loadDeleteRecord(ctx, tx, itemID, true); err != nil {
		return models.InventoryItemDetail{}, err
	}

	existingImages, err := s.loadItemImageUploadRecords(ctx, tx, itemID, true)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}

	if s.imageConfig.MaxImagesPerItem > 0 && len(existingImages)+len(savedFiles) > s.imageConfig.MaxImagesPerItem {
		return models.InventoryItemDetail{}, fmt.Errorf("%w: item cannot have more than %d images", errInvalidUploadRequest, s.imageConfig.MaxImagesPerItem)
	}

	nextUploadOrder := len(existingImages)
	for index, file := range savedFiles {
		if _, err := tx.Exec(ctx, `
			INSERT INTO image_assets (
				item_id,
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
			VALUES ($1::uuid, $2, $3, $4, $5, $6, $7, $8, true, 'processed')
		`,
			itemID,
			file.originalFilename,
			file.storedFilename,
			file.filePath,
			file.hashHex,
			file.mimeType,
			file.sizeBytes,
			nextUploadOrder+index,
		); err != nil {
			return models.InventoryItemDetail{}, err
		}
	}

	allImages, err := s.loadItemImageUploadRecords(ctx, tx, itemID, true)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}

	variantUpdates := make([]itemImageVariantUpdate, 0, len(allImages))
	for _, imageRecord := range allImages {
		thumbnailPath, normalizedPath, err := s.regenerateItemImageVariants(imageRecord)
		if err != nil {
			return models.InventoryItemDetail{}, err
		}
		variantUpdates = append(variantUpdates, itemImageVariantUpdate{
			ImageAssetID:   imageRecord.ID,
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
			return models.InventoryItemDetail{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.InventoryItemDetail{}, err
	}

	return s.GetByID(ctx, itemID)
}

func (s *ItemStore) DeleteImage(ctx context.Context, itemID string, imageID string) (models.ItemImageDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ItemImageDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	if _, err := s.loadDeleteRecord(ctx, tx, itemID, true); err != nil {
		return models.ItemImageDeleteResponse{}, err
	}

	image, err := s.loadItemDeleteImageRecord(ctx, tx, itemID, imageID, true)
	if err != nil {
		return models.ItemImageDeleteResponse{}, err
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
		return models.ItemImageDeleteResponse{}, err
	}

	deleteTag, err := tx.Exec(ctx, `
		DELETE FROM image_assets
		WHERE id = $1
			AND item_id = $2
	`, imageID, itemID)
	if err != nil {
		return models.ItemImageDeleteResponse{}, err
	}
	if deleteTag.RowsAffected() == 0 {
		return models.ItemImageDeleteResponse{}, pgx.ErrNoRows
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ItemImageDeleteResponse{}, err
	}

	item, err := s.GetByID(ctx, itemID)
	if err != nil {
		return models.ItemImageDeleteResponse{}, err
	}

	deletedFiles, missingFiles, warnings := s.files.DeleteFiles(inspection.DeletePaths, "item image delete")

	return models.ItemImageDeleteResponse{
		Item:                item,
		DeletedImageAssetID: imageID,
		DeletedFileCount:    deletedFiles,
		MissingFileCount:    missingFiles,
		Warnings:            warnings,
	}, nil
}

func (s *ItemStore) loadItemImageUploadRecords(ctx context.Context, q itemQuerier, itemID string, lock bool) ([]itemImageUploadRecord, error) {
	query := `
		SELECT
			id::text,
			file_path,
			stored_filename,
			mime_type
		FROM image_assets
		WHERE item_id = $1
		ORDER BY upload_order ASC, created_datetime ASC
	`
	if lock {
		query += ` FOR UPDATE`
	}

	rows, err := q.Query(ctx, query, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := make([]itemImageUploadRecord, 0)
	for rows.Next() {
		var record itemImageUploadRecord
		if err := rows.Scan(&record.ID, &record.FilePath, &record.StoredFilename, &record.MimeType); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func (s *ItemStore) loadItemDeleteImageRecord(ctx context.Context, q itemQuerier, itemID string, imageID string, lock bool) (itemDeleteImageRow, error) {
	query := `
		SELECT
			id::text,
			upload_group_id::text,
			session_id::text,
			file_path,
			thumbnail_path,
			normalized_path,
			original_filename,
			stored_filename
		FROM image_assets
		WHERE id = $1
			AND item_id = $2
	`
	if lock {
		query += ` FOR UPDATE`
	}

	var image itemDeleteImageRow
	if err := q.QueryRow(ctx, query, imageID, itemID).Scan(
		&image.ImageAssetID,
		&image.UploadGroupID,
		&image.SessionID,
		&image.FilePath,
		&image.ThumbnailPath,
		&image.NormalizedPath,
		&image.OriginalFilename,
		&image.StoredFilename,
	); err != nil {
		return itemDeleteImageRow{}, err
	}

	return image, nil
}

func (s *ItemStore) regenerateItemImageVariants(record itemImageUploadRecord) (string, string, error) {
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
		log.Printf("item image upload using original file as fallback variants image_asset_id=%s error=%v", record.ID, err)
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

func writeVariantImage(src image.Image, imageFormat string, destinationPath string, maxEdge int) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
		return err
	}

	scaled := resizeImageToMaxEdge(src, maxEdge)
	tempPath := destinationPath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	encodeErr := encodeImageVariant(file, scaled, imageFormat)
	closeErr := file.Close()
	if encodeErr != nil {
		_ = os.Remove(tempPath)
		return encodeErr
	}
	if closeErr != nil {
		_ = os.Remove(tempPath)
		return closeErr
	}

	return os.Rename(tempPath, destinationPath)
}

func resizeImageToMaxEdge(src image.Image, maxEdge int) image.Image {
	bounds := src.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width <= 0 || height <= 0 {
		return src
	}
	if width <= maxEdge && height <= maxEdge {
		return src
	}

	targetWidth := width
	targetHeight := height
	if width >= height {
		targetWidth = maxEdge
		targetHeight = max(1, (height*maxEdge)/width)
	} else {
		targetHeight = maxEdge
		targetWidth = max(1, (width*maxEdge)/height)
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetWidth, targetHeight))
	for y := 0; y < targetHeight; y++ {
		srcY := bounds.Min.Y + (y*height)/targetHeight
		for x := 0; x < targetWidth; x++ {
			srcX := bounds.Min.X + (x*width)/targetWidth
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}

	return dst
}

func encodeImageVariant(w io.Writer, img image.Image, imageFormat string) error {
	switch strings.ToLower(strings.TrimSpace(imageFormat)) {
	case "jpeg", "jpg":
		return jpeg.Encode(w, img, &jpeg.Options{Quality: 85})
	case "png":
		return png.Encode(w, img)
	default:
		fallback := image.NewRGBA(img.Bounds())
		for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
			for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
				fallback.Set(x, y, img.At(x, y))
			}
		}
		if fallback.Bounds().Empty() {
			fallback = image.NewRGBA(image.Rect(0, 0, 1, 1))
			fallback.Set(0, 0, color.RGBA{0, 0, 0, 255})
		}
		return png.Encode(w, fallback)
	}
}

func detectSupportedImageMime(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(path))
	mimeType, err := detectSupportedImageMimeFromReader(file, ext)
	if err != nil {
		return "", err
	}
	if _, _, err := image.DecodeConfig(file); err != nil {
		return "", errors.New("image file is not a valid JPEG or PNG image")
	}
	return mimeType, nil
}

func detectSupportedImageMimeFromReader(file *os.File, ext string) (string, error) {
	var header [12]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", err
	}

	switch ext {
	case ".jpg", ".jpeg":
		if n >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff {
			return "image/jpeg", nil
		}
		return "", errors.New("only JPEG and PNG images are supported")
	case ".png":
		pngMagic := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
		if n >= len(pngMagic) && string(header[:len(pngMagic)]) == string(pngMagic) {
			return "image/png", nil
		}
		return "", errors.New("only JPEG and PNG images are supported")
	default:
		return "", errors.New("only JPEG and PNG images are supported")
	}
}

func cleanupWrittenItemFiles(files []writtenItemImageFile) {
	for _, file := range files {
		_ = os.Remove(file.path)
	}
}

func copyImageVariantFile(sourcePath string, destinationPath string) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
		return err
	}

	src, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	tempPath := destinationPath + ".tmp"
	dst, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dst, src); err != nil {
		_ = dst.Close()
		_ = os.Remove(tempPath)
		return err
	}
	if err := dst.Close(); err != nil {
		_ = os.Remove(tempPath)
		return err
	}

	return os.Rename(tempPath, destinationPath)
}
