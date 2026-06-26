package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fastsell-api/internal/upload"

	"github.com/jackc/pgx/v5"
)

const wholeSceneCropGeneratorVersion = "whole-scene-crop-v1"

type wholeSceneGeneratedCropInfo struct {
	AppearanceID     string
	CropImageAssetID string
	CropPath         string
	DiagnosticIndex  *int
}

func (w *WholeSceneAnalysisWorker) generatePreferredWholeSceneCrop(ctx context.Context, tx pgx.Tx, candidateID string, appearances []persistedWholeSceneAppearance) ([]string, *wholeSceneGeneratedCropInfo) {
	if len(appearances) == 0 {
		return nil, nil
	}
	if strings.TrimSpace(w.cfg.OriginalsDir) == "" || strings.TrimSpace(w.cfg.ThumbnailsDir) == "" || strings.TrimSpace(w.cfg.NormalizedDir) == "" {
		return []string{fmt.Sprintf("candidate %s crop skipped because image output directories are not configured", candidateID)}, nil
	}

	warnings := make([]string, 0)
	for _, appearance := range appearances {
		if !wholeSceneBBoxComplete(appearance.BoundingBox) {
			continue
		}

		cropInfo, err := w.generateWholeSceneCrop(ctx, tx, candidateID, appearance, true)
		if err == nil {
			return warnings, cropInfo
		}

		message := truncateReviewAssistText(err.Error(), 500)
		warnings = append(warnings, fmt.Sprintf("candidate %s crop failed from source_image_index %d: %s", candidateID, appearance.SourceImageIndex, message))
		if insertErr := insertFailedWholeSceneCrop(ctx, tx, candidateID, appearance, message); insertErr != nil {
			log.Printf("failed to record whole scene crop failure candidate_id=%s appearance_id=%s: %v", candidateID, appearance.ID, insertErr)
			return warnings, nil
		}
	}

	return warnings, nil
}

func (w *WholeSceneAnalysisWorker) generateWholeSceneCrop(ctx context.Context, tx pgx.Tx, candidateID string, appearance persistedWholeSceneAppearance, preferred bool) (*wholeSceneGeneratedCropInfo, error) {
	sourcePath := filepath.Clean(strings.TrimSpace(appearance.Source.FilePath))
	if sourcePath == "" {
		return nil, errors.New("source image path was empty")
	}
	if !isSafeManagedPath(sourcePath, w.cfg.SafeRoots) {
		return nil, fmt.Errorf("source image path was outside managed storage: %s", sourcePath)
	}
	if strings.TrimSpace(appearance.Source.UploadSessionID) == "" {
		return nil, errors.New("source image was not linked to an upload session")
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return nil, err
	}
	defer sourceFile.Close()

	srcImage, _, err := image.Decode(sourceFile)
	if err != nil {
		return nil, fmt.Errorf("source image could not be decoded for reference crop: %w", err)
	}

	cropRect, err := wholeScenePixelCropRect(srcImage.Bounds(), appearance.BoundingBox)
	if err != nil {
		return nil, err
	}
	cropImage := copyImageRegion(srcImage, cropRect)

	originalFilename := fmt.Sprintf("whole-scene-crop-%s.jpg", candidateID)
	storedFilename, err := upload.NewStoredFilename(originalFilename, "image/jpeg")
	if err != nil {
		return nil, err
	}

	originalPath := filepath.Join(w.cfg.OriginalsDir, storedFilename)
	thumbnailPath := filepath.Join(w.cfg.ThumbnailsDir, storedFilename)
	normalizedPath := filepath.Join(w.cfg.NormalizedDir, storedFilename)

	if err := writeWholeSceneCropJPEG(cropImage, originalPath); err != nil {
		return nil, err
	}
	writtenPaths := []string{originalPath}
	cleanupOnError := true
	defer func() {
		if cleanupOnError {
			for _, path := range writtenPaths {
				_ = os.Remove(path)
			}
		}
	}()

	if err := writeVariantImage(cropImage, "jpeg", thumbnailPath, defaultThumbnailMaxEdge); err != nil {
		return nil, err
	}
	writtenPaths = append(writtenPaths, thumbnailPath)
	if err := writeVariantImage(cropImage, "jpeg", normalizedPath, defaultNormalizedMaxEdge); err != nil {
		return nil, err
	}
	writtenPaths = append(writtenPaths, normalizedPath)

	fileHash, fileSize, err := hashWholeSceneCropFile(originalPath)
	if err != nil {
		return nil, err
	}

	metadata, err := json.Marshal(map[string]any{
		"generator":             wholeSceneCropGeneratorVersion,
		"source_image_asset_id": appearance.Source.ImageAssetID,
		"source_scan_image_id":  appearance.ScanImageID,
		"source_image_index":    appearance.SourceImageIndex,
		"normalized_bbox": map[string]any{
			"x":      *appearance.BoundingBox.X,
			"y":      *appearance.BoundingBox.Y,
			"width":  *appearance.BoundingBox.Width,
			"height": *appearance.BoundingBox.Height,
		},
		"pixel_rect": map[string]any{
			"x":      cropRect.Min.X,
			"y":      cropRect.Min.Y,
			"width":  cropRect.Dx(),
			"height": cropRect.Dy(),
		},
	})
	if err != nil {
		return nil, err
	}

	nextUploadOrder, err := nextWholeSceneCropUploadOrder(ctx, tx, appearance.Source.UploadSessionID)
	if err != nil {
		return nil, err
	}

	var cropImageAssetID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO image_assets (
			session_id,
			upload_group_id,
			item_id,
			original_filename,
			stored_filename,
			file_path,
			thumbnail_path,
			normalized_path,
			file_hash,
			mime_type,
			file_size_bytes,
			upload_order,
			is_original,
			status
		)
		VALUES ($1::uuid, NULL, NULL, $2, $3, $4, $5, $6, $7, 'image/jpeg', $8, $9, true, 'processed')
		RETURNING id::text
	`,
		appearance.Source.UploadSessionID,
		originalFilename,
		storedFilename,
		originalPath,
		thumbnailPath,
		normalizedPath,
		fileHash,
		fileSize,
		nextUploadOrder,
	).Scan(&cropImageAssetID); err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO whole_scene_candidate_crops (
			candidate_id,
			appearance_id,
			scan_image_id,
			crop_image_asset_id,
			status,
			is_preferred,
			bounding_box_x,
			bounding_box_y,
			bounding_box_width,
			bounding_box_height,
			crop_metadata
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, 'generated', $5, $6, $7, $8, $9, $10::jsonb)
	`,
		candidateID,
		appearance.ID,
		appearance.ScanImageID,
		cropImageAssetID,
		preferred,
		appearance.BoundingBox.X,
		appearance.BoundingBox.Y,
		appearance.BoundingBox.Width,
		appearance.BoundingBox.Height,
		string(metadata),
	); err != nil {
		return nil, err
	}

	cleanupOnError = false
	return &wholeSceneGeneratedCropInfo{
		AppearanceID:     appearance.ID,
		CropImageAssetID: cropImageAssetID,
		CropPath:         originalPath,
		DiagnosticIndex:  appearance.DiagnosticIndex,
	}, nil
}

func insertFailedWholeSceneCrop(ctx context.Context, tx pgx.Tx, candidateID string, appearance persistedWholeSceneAppearance, message string) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO whole_scene_candidate_crops (
			candidate_id,
			appearance_id,
			scan_image_id,
			status,
			is_preferred,
			bounding_box_x,
			bounding_box_y,
			bounding_box_width,
			bounding_box_height,
			error_message
		)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'failed', false, $4, $5, $6, $7, $8)
	`,
		candidateID,
		appearance.ID,
		appearance.ScanImageID,
		appearance.BoundingBox.X,
		appearance.BoundingBox.Y,
		appearance.BoundingBox.Width,
		appearance.BoundingBox.Height,
		message,
	)
	return err
}

func nextWholeSceneCropUploadOrder(ctx context.Context, tx pgx.Tx, sessionID string) (int, error) {
	var nextUploadOrder int
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(max(upload_order), -1) + 1
		FROM image_assets
		WHERE session_id = $1::uuid
	`, sessionID).Scan(&nextUploadOrder); err != nil {
		return 0, err
	}
	return nextUploadOrder, nil
}

func wholeSceneBBoxComplete(bbox wholeSceneParsedBoundingBox) bool {
	return bbox.X != nil && bbox.Y != nil && bbox.Width != nil && bbox.Height != nil
}

func wholeScenePixelCropRect(bounds image.Rectangle, bbox wholeSceneParsedBoundingBox) (image.Rectangle, error) {
	normalized, err := normalizeWholeSceneBoundingBox(bounds, bbox)
	if err != nil {
		return image.Rectangle{}, err
	}
	return wholeScenePixelCropRectFromNormalized(bounds, normalized)
}

func wholeScenePixelCropRectFromNormalized(bounds image.Rectangle, bbox wholeSceneParsedBoundingBox) (image.Rectangle, error) {
	if !wholeSceneBBoxComplete(bbox) {
		return image.Rectangle{}, errors.New("bounding box was incomplete")
	}
	if *bbox.X < 0 || *bbox.Y < 0 || *bbox.Width <= 0 || *bbox.Height <= 0 || *bbox.X > 1 || *bbox.Y > 1 || *bbox.Width > 1 || *bbox.Height > 1 || *bbox.X+*bbox.Width > 1 || *bbox.Y+*bbox.Height > 1 {
		return image.Rectangle{}, errors.New("bounding box was outside normalized 0..1 coordinates")
	}

	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return image.Rectangle{}, errors.New("source image had invalid dimensions")
	}

	x1 := bounds.Min.X + int(math.Floor(*bbox.X*float64(sourceWidth)))
	y1 := bounds.Min.Y + int(math.Floor(*bbox.Y*float64(sourceHeight)))
	x2 := bounds.Min.X + int(math.Ceil(((*bbox.X+*bbox.Width)*float64(sourceWidth))-1e-9))
	y2 := bounds.Min.Y + int(math.Ceil(((*bbox.Y+*bbox.Height)*float64(sourceHeight))-1e-9))

	if x1 < bounds.Min.X {
		x1 = bounds.Min.X
	}
	if y1 < bounds.Min.Y {
		y1 = bounds.Min.Y
	}
	if x2 > bounds.Max.X {
		x2 = bounds.Max.X
	}
	if y2 > bounds.Max.Y {
		y2 = bounds.Max.Y
	}
	if x2 <= x1 || y2 <= y1 {
		return image.Rectangle{}, errors.New("bounding box produced an empty crop")
	}

	return image.Rect(x1, y1, x2, y2), nil
}

func normalizeWholeSceneBoundingBox(bounds image.Rectangle, bbox wholeSceneParsedBoundingBox) (wholeSceneParsedBoundingBox, error) {
	if !wholeSceneBBoxComplete(bbox) {
		return wholeSceneParsedBoundingBox{}, errors.New("bounding box was incomplete")
	}
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return wholeSceneParsedBoundingBox{}, errors.New("source image had invalid dimensions")
	}

	x1 := *bbox.X
	y1 := *bbox.Y
	x2 := *bbox.Width
	y2 := *bbox.Height
	mode := strings.ToLower(strings.TrimSpace(bbox.CoordinateMode))
	if mode == "yxyx" {
		y1 = *bbox.X
		x1 = *bbox.Y
		y2 = *bbox.Width
		x2 = *bbox.Height
	} else if mode != "xyxy" {
		x2 = x1 + *bbox.Width
		y2 = y1 + *bbox.Height
	}
	if !validWholeSceneBBoxNumbers(x1, y1, x2, y2) {
		return wholeSceneParsedBoundingBox{}, errors.New("bounding box contained invalid coordinates")
	}
	if x2 <= x1 || y2 <= y1 {
		return wholeSceneParsedBoundingBox{}, errors.New("bounding box produced an empty crop")
	}

	unit := inferWholeSceneBBoxUnit(bbox, x1, y1, x2, y2, sourceWidth, sourceHeight)
	switch unit {
	case "pixel":
		x1 = x1 / float64(sourceWidth)
		x2 = x2 / float64(sourceWidth)
		y1 = y1 / float64(sourceHeight)
		y2 = y2 / float64(sourceHeight)
	case "percent":
		x1 = x1 / 100
		x2 = x2 / 100
		y1 = y1 / 100
		y2 = y2 / 100
	case "thousand":
		x1 = x1 / 1000
		x2 = x2 / 1000
		y1 = y1 / 1000
		y2 = y2 / 1000
	case "normalized":
	default:
		return wholeSceneParsedBoundingBox{}, errors.New("bounding box coordinate units were unsupported")
	}

	x1 = clampFloat64(x1, 0, 1)
	y1 = clampFloat64(y1, 0, 1)
	x2 = clampFloat64(x2, 0, 1)
	y2 = clampFloat64(y2, 0, 1)
	width := x2 - x1
	height := y2 - y1
	if width <= 0 || height <= 0 {
		return wholeSceneParsedBoundingBox{}, errors.New("bounding box produced an empty crop")
	}

	return newWholeSceneParsedBoundingBox(x1, y1, width, height, "xywh", "normalized"), nil
}

func inferWholeSceneBBoxUnit(bbox wholeSceneParsedBoundingBox, x1 float64, y1 float64, x2 float64, y2 float64, sourceWidth int, sourceHeight int) string {
	unit := strings.ToLower(strings.TrimSpace(bbox.CoordinateUnit))
	if unit != "" {
		return unit
	}

	maxCoordinate := 0.0
	for _, value := range []float64{x1, y1, x2, y2} {
		maxCoordinate = math.Max(maxCoordinate, math.Abs(value))
	}

	switch {
	case maxCoordinate <= 1.2:
		return "normalized"
	case maxCoordinate <= 100:
		return "percent"
	case (bbox.CoordinateMode == "yxyx" || bbox.CoordinateMode == "xyxy") && maxCoordinate <= 1000:
		return "thousand"
	case sourceWidth > 0 && sourceHeight > 0 && sourceWidth <= 1000 && sourceHeight <= 1000 && x2 <= float64(sourceWidth)+5 && y2 <= float64(sourceHeight)+5:
		return "pixel"
	case maxCoordinate <= 1000:
		return "thousand"
	default:
		return "pixel"
	}
}

func newWholeSceneParsedBoundingBox(x float64, y float64, width float64, height float64, mode string, unit string) wholeSceneParsedBoundingBox {
	return wholeSceneParsedBoundingBox{
		X:              &x,
		Y:              &y,
		Width:          &width,
		Height:         &height,
		CoordinateMode: mode,
		CoordinateUnit: unit,
	}
}

func clampFloat64(value float64, minimum float64, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func copyImageRegion(src image.Image, rect image.Rectangle) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst
}

func writeWholeSceneCropJPEG(img image.Image, destinationPath string) error {
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0755); err != nil {
		return err
	}

	tempPath := destinationPath + ".tmp"
	file, err := os.OpenFile(tempPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	encodeErr := jpeg.Encode(file, img, &jpeg.Options{Quality: 88})
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

func hashWholeSceneCropFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	hasher := sha256.New()
	size, err := io.Copy(hasher, file)
	if err != nil {
		return "", 0, err
	}

	return hex.EncodeToString(hasher.Sum(nil)), size, nil
}
