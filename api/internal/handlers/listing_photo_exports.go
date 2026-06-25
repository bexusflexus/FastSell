package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fastsell-api/internal/models"
)

type ListingPhotoExportConfig struct {
	ExportRoot      string
	ExportHostRoot  string
	TTL             time.Duration
	SourceSafeRoots []string
}

type listingPhotoSource struct {
	ImageAssetID     string
	FilePath         string
	MimeType         *string
	OriginalFilename *string
	StoredFilename   *string
	UploadOrder      int
	CreatedDatetime  time.Time
}

func (s *ListingDraftStore) PreparePhotoExport(ctx context.Context, draftID string) (models.ListingDraft, error) {
	record, err := s.getStoredByID(ctx, draftID)
	if err != nil {
		return models.ListingDraft{}, err
	}

	if err := s.cleanupExpiredPhotoExports(); err != nil {
		log.Printf("listing photo export cleanup before prepare failed: %v", err)
	}

	export, err := s.preparePhotoExportForDraft(ctx, record)
	if err != nil {
		return models.ListingDraft{}, err
	}

	draft := record.toPublic(true)
	draft.PhotoExport = &export
	return draft, nil
}

func (s *ListingDraftStore) RunPhotoExportCleanupWorker(ctx context.Context) {
	if strings.TrimSpace(s.exportConfig.ExportRoot) == "" || s.exportConfig.TTL <= 0 {
		return
	}

	if err := s.cleanupExpiredPhotoExports(); err != nil {
		log.Printf("listing photo export startup cleanup failed: %v", err)
	}

	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.cleanupExpiredPhotoExports(); err != nil {
				log.Printf("listing photo export periodic cleanup failed: %v", err)
			}
		}
	}
}

func (s *ListingDraftStore) cleanupExpiredPhotoExports() error {
	root := filepath.Clean(strings.TrimSpace(s.exportConfig.ExportRoot))
	if root == "." || root == "" {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	expirationCutoff := time.Now().UTC().Add(-s.exportConfig.TTL)
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 || !entry.IsDir() {
			continue
		}

		childPath := filepath.Join(root, entry.Name())
		if !isSafeManagedPath(childPath, []string{root}) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			log.Printf("listing photo export cleanup failed to stat %s: %v", childPath, err)
			continue
		}

		if info.ModTime().UTC().After(expirationCutoff) {
			continue
		}

		if err := os.RemoveAll(childPath); err != nil {
			log.Printf("listing photo export cleanup failed path=%s error=%v", childPath, err)
		}
	}

	return nil
}

func (s *ListingDraftStore) preparePhotoExportForDraft(ctx context.Context, record storedListingDraft) (models.ListingPhotoExport, error) {
	root := filepath.Clean(strings.TrimSpace(s.exportConfig.ExportRoot))
	if root == "." || root == "" {
		return models.ListingPhotoExport{}, errors.New("listing photo export root is not configured")
	}

	if err := os.MkdirAll(root, 0750); err != nil {
		return models.ListingPhotoExport{}, err
	}

	exportID := record.ID
	exportDir := filepath.Join(root, exportID)
	if !isSafeManagedPath(exportDir, []string{root}) {
		return models.ListingPhotoExport{}, errors.New("listing photo export path is outside approved export root")
	}

	if err := os.RemoveAll(exportDir); err != nil {
		return models.ListingPhotoExport{}, err
	}
	if err := os.MkdirAll(exportDir, 0750); err != nil {
		return models.ListingPhotoExport{}, err
	}

	sources, err := s.loadListingPhotoSources(ctx, record.ItemID)
	if err != nil {
		return models.ListingPhotoExport{}, err
	}

	export := models.ListingPhotoExport{
		ExportID:   exportID,
		ExportPath: s.hostFacingExportPath(exportID),
		ExpiresAt:  time.Now().UTC().Add(s.exportConfig.TTL),
		Files:      make([]models.ListingPhotoExportFile, 0, len(sources)),
		Warnings:   make([]string, 0),
	}

	baseName := sanitizeListingPhotoBaseName(record.Title)
	width := maxInt(len(strconv.Itoa(maxInt(len(sources), 1))), 2)
	copiedCount := 0
	for index, source := range sources {
		if err := validateListingPhotoSource(source.FilePath, s.exportConfig.SourceSafeRoots); err != nil {
			export.Warnings = append(export.Warnings, fmt.Sprintf("Skipped unsafe original image path for image asset %s.", source.ImageAssetID))
			continue
		}

		extension := listingPhotoExtension(source)
		filename := fmt.Sprintf("%0*d_%s%s", width, index+1, baseName, extension)
		destination := filepath.Join(exportDir, filename)
		if err := copyRegularFile(source.FilePath, destination); err != nil {
			if os.IsNotExist(err) {
				export.Warnings = append(export.Warnings, fmt.Sprintf("Original image was missing for image asset %s.", source.ImageAssetID))
				continue
			}
			export.Warnings = append(export.Warnings, fmt.Sprintf("Failed to copy original image for image asset %s.", source.ImageAssetID))
			continue
		}

		stat, err := os.Stat(destination)
		if err != nil {
			export.Warnings = append(export.Warnings, fmt.Sprintf("Copied file could not be inspected for image asset %s.", source.ImageAssetID))
			continue
		}

		export.Files = append(export.Files, models.ListingPhotoExportFile{
			Filename:  filename,
			SizeBytes: stat.Size(),
		})
		copiedCount++
	}

	export.ImageCount = copiedCount
	now := time.Now()
	_ = os.Chtimes(exportDir, now, now)

	if len(sources) > 0 && copiedCount == 0 {
		return models.ListingPhotoExport{}, errors.New("failed to copy any original item images for listing photo export")
	}

	if len(sources) == 0 {
		export.Warnings = append(export.Warnings, "No original item images were available to export.")
	}

	return export, nil
}

func (s *ListingDraftStore) loadListingPhotoSources(ctx context.Context, itemID string) ([]listingPhotoSource, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			file_path,
			mime_type,
			original_filename,
			stored_filename,
			upload_order,
			created_datetime
		FROM image_assets
		WHERE item_id = $1::uuid
		ORDER BY upload_order ASC, created_datetime ASC, id ASC
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sources := make([]listingPhotoSource, 0)
	for rows.Next() {
		var source listingPhotoSource
		if err := rows.Scan(
			&source.ImageAssetID,
			&source.FilePath,
			&source.MimeType,
			&source.OriginalFilename,
			&source.StoredFilename,
			&source.UploadOrder,
			&source.CreatedDatetime,
		); err != nil {
			return nil, err
		}
		sources = append(sources, source)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return sources, nil
}

func (s *ListingDraftStore) hostFacingExportPath(exportID string) string {
	hostRoot := filepath.Clean(strings.TrimSpace(s.exportConfig.ExportHostRoot))
	if hostRoot == "." || hostRoot == "" {
		return filepath.Join(filepath.Clean(s.exportConfig.ExportRoot), exportID)
	}
	return filepath.Join(hostRoot, exportID)
}

func validateListingPhotoSource(path string, safeRoots []string) error {
	cleanPath := filepath.Clean(strings.TrimSpace(path))
	if cleanPath == "." || cleanPath == "" {
		return errors.New("original image path is empty")
	}
	if !isSafeManagedPath(cleanPath, safeRoots) {
		return errors.New("original image path is outside approved roots")
	}
	info, err := os.Lstat(cleanPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("original image path is a symlink")
	}
	if info.IsDir() {
		return errors.New("original image path is a directory")
	}
	return nil
}

func copyRegularFile(source string, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	srcInfo, err := src.Stat()
	if err != nil {
		return err
	}
	if !srcInfo.Mode().IsRegular() {
		return errors.New("source file is not a regular file")
	}

	dst, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return err
	}

	return nil
}

func listingPhotoExtension(source listingPhotoSource) string {
	candidates := []*string{source.OriginalFilename, source.StoredFilename}
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		extension := strings.ToLower(filepath.Ext(strings.TrimSpace(*candidate)))
		if isSafeImageExtension(extension) {
			return extension
		}
	}

	if source.MimeType != nil {
		switch strings.ToLower(strings.TrimSpace(*source.MimeType)) {
		case "image/png":
			return ".png"
		case "image/jpeg", "image/jpg":
			return ".jpg"
		default:
			if extensions, _ := mime.ExtensionsByType(strings.TrimSpace(*source.MimeType)); len(extensions) > 0 && isSafeImageExtension(strings.ToLower(extensions[0])) {
				return strings.ToLower(extensions[0])
			}
		}
	}

	return ".jpg"
}

func sanitizeListingPhotoBaseName(title string) string {
	trimmed := strings.TrimSpace(title)
	if trimmed == "" {
		trimmed = "listing_photo"
	}

	var builder strings.Builder
	lastUnderscore := false
	for _, ch := range trimmed {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9'):
			builder.WriteRune(ch)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	result := strings.Trim(builder.String(), "_")
	if result == "" {
		result = "listing_photo"
	}
	if len(result) > 80 {
		result = strings.Trim(result[:80], "_")
	}
	if result == "" {
		result = "listing_photo"
	}
	if strings.HasPrefix(result, ".") {
		result = "listing_photo"
	}
	return result
}

func isSafeImageExtension(extension string) bool {
	switch extension {
	case ".jpg", ".jpeg", ".png":
		return true
	default:
		return false
	}
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}
	return b
}
