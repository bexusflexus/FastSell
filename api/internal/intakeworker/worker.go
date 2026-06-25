package intakeworker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fastsell-api/internal/uploadstatus"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Config struct {
	IntakeDir      string
	ProcessingDir  string
	FailedDir      string
	OriginalsDir   string
	ScanInterval   time.Duration
	StableDuration time.Duration
	MaxUploadBytes int64
	MaxRowsPerScan int
}

type Worker struct {
	pool *pgxpool.Pool
	cfg  Config
}

type imageAsset struct {
	ID             string
	SessionID      string
	StoredFilename string
	FilePath       string
	MimeType       *string
	FileSizeBytes  *int64
}

type detectedImage struct {
	mimeType string
	size     int64
	hashHex  string
}

func New(pool *pgxpool.Pool, cfg Config) *Worker {
	if cfg.MaxRowsPerScan <= 0 {
		cfg.MaxRowsPerScan = 25
	}
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 5 * time.Second
	}
	if cfg.StableDuration <= 0 {
		cfg.StableDuration = 3 * time.Second
	}

	return &Worker{pool: pool, cfg: cfg}
}

func (w *Worker) Run(ctx context.Context) {
	log.Printf(
		"intake worker started intake_dir=%s processing_dir=%s failed_dir=%s originals_dir=%s scan_interval=%s stable_duration=%s",
		filepath.Clean(w.cfg.IntakeDir),
		filepath.Clean(w.cfg.ProcessingDir),
		filepath.Clean(w.cfg.FailedDir),
		filepath.Clean(w.cfg.OriginalsDir),
		w.cfg.ScanInterval,
		w.cfg.StableDuration,
	)

	w.scanOnce(ctx)

	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Print("intake worker stopped")
			return
		case <-ticker.C:
			w.scanOnce(ctx)
		}
	}
}

func (w *Worker) scanOnce(ctx context.Context) {
	scanCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	assets, err := w.listPending(scanCtx)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			log.Printf("intake worker scan failed: %v", err)
		}
		return
	}

	for _, asset := range assets {
		if err := w.processAsset(ctx, asset); err != nil {
			log.Printf("intake worker asset %s failed: %v", asset.ID, err)
		}
	}
}

func (w *Worker) listPending(ctx context.Context) ([]imageAsset, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT id::text, session_id::text, stored_filename, file_path, mime_type, file_size_bytes
		FROM image_assets
		WHERE status IN ('pending', 'uploaded')
			AND stored_filename IS NOT NULL
			AND stored_filename <> ''
			AND file_path IS NOT NULL
			AND file_path <> ''
		ORDER BY created_datetime ASC
		LIMIT $1
	`, w.cfg.MaxRowsPerScan)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := make([]imageAsset, 0)
	for rows.Next() {
		var asset imageAsset
		if err := rows.Scan(&asset.ID, &asset.SessionID, &asset.StoredFilename, &asset.FilePath, &asset.MimeType, &asset.FileSizeBytes); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}

	return assets, rows.Err()
}

func (w *Worker) processAsset(ctx context.Context, asset imageAsset) error {
	sourcePath := w.resolveSourcePath(asset)
	stable, err := w.isStable(ctx, sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if claimed, claimErr := w.claim(ctx, asset.ID); claimErr != nil || !claimed {
				return claimErr
			}
			return w.failAsset(ctx, asset, sourcePath, fmt.Sprintf("source file missing: %s", filepath.Base(sourcePath)))
		}
		return err
	}
	if !stable {
		return nil
	}

	claimed, err := w.claim(ctx, asset.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return nil
	}

	processingPath := filepath.Join(w.cfg.ProcessingDir, asset.StoredFilename)
	finalPath := filepath.Join(w.cfg.OriginalsDir, asset.StoredFilename)

	if err := moveFile(sourcePath, processingPath); err != nil {
		return w.failAsset(ctx, asset, sourcePath, fmt.Sprintf("failed to move file to processing: %v", err))
	}

	detected, err := validateAndHashImage(processingPath, w.cfg.MaxUploadBytes)
	if err != nil {
		return w.failAsset(ctx, asset, processingPath, err.Error())
	}

	if err := moveFile(processingPath, finalPath); err != nil {
		return w.failAsset(ctx, asset, processingPath, fmt.Sprintf("failed to move file to originals: %v", err))
	}

	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err = w.pool.Exec(updateCtx, `
		UPDATE image_assets
		SET status = 'processed',
			file_path = $2,
			file_hash = $3,
			mime_type = $4,
			file_size_bytes = $5,
			error_message = NULL,
			updated_datetime = now()
		WHERE id = $1
	`, asset.ID, finalPath, detected.hashHex, detected.mimeType, detected.size)
	if err != nil {
		return err
	}

	if _, err := uploadstatus.Recalculate(updateCtx, w.pool, asset.SessionID); err != nil {
		return err
	}

	log.Printf("intake worker processed image_asset=%s stored_filename=%s", asset.ID, asset.StoredFilename)
	return nil
}

func (w *Worker) claim(ctx context.Context, assetID string) (bool, error) {
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tag, err := w.pool.Exec(claimCtx, `
		UPDATE image_assets
		SET status = 'processing',
			updated_datetime = now()
		WHERE id = $1
			AND status IN ('pending', 'uploaded')
	`, assetID)
	if err != nil {
		return false, err
	}

	return tag.RowsAffected() == 1, nil
}

func (w *Worker) failAsset(ctx context.Context, asset imageAsset, currentPath string, reason string) error {
	failedPath := filepath.Join(w.cfg.FailedDir, asset.StoredFilename)
	if currentPath != "" {
		if err := moveFile(currentPath, failedPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			log.Printf("intake worker failed to move image_asset=%s to failed dir: %v", asset.ID, err)
		}
	}

	updatePath := failedPath
	if _, err := os.Stat(failedPath); err != nil {
		updatePath = currentPath
	}

	updateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := w.pool.Exec(updateCtx, `
		UPDATE image_assets
		SET status = 'failed',
			file_path = COALESCE(NULLIF($2, ''), file_path),
			error_message = $3,
			updated_datetime = now()
		WHERE id = $1
	`, asset.ID, updatePath, reason)
	if err != nil {
		return err
	}

	if _, err := uploadstatus.Recalculate(updateCtx, w.pool, asset.SessionID); err != nil {
		return err
	}

	log.Printf("intake worker marked image_asset=%s failed reason=%s", asset.ID, reason)
	return fmt.Errorf("%s", reason)
}

func (w *Worker) resolveSourcePath(asset imageAsset) string {
	if asset.FilePath != "" {
		if _, err := os.Stat(asset.FilePath); err == nil {
			return asset.FilePath
		}
	}

	return filepath.Join(w.cfg.IntakeDir, asset.StoredFilename)
}

func (w *Worker) isStable(ctx context.Context, path string) (bool, error) {
	first, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	if first.Size() <= 0 {
		return true, nil
	}

	timer := time.NewTimer(w.cfg.StableDuration)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case <-timer.C:
	}

	second, err := os.Stat(path)
	if err != nil {
		return false, err
	}

	return first.Size() == second.Size(), nil
}

func validateAndHashImage(path string, maxBytes int64) (detectedImage, error) {
	file, err := os.Open(path)
	if err != nil {
		return detectedImage{}, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return detectedImage{}, err
	}
	if info.Size() <= 0 {
		return detectedImage{}, errors.New("image file is empty")
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return detectedImage{}, errors.New("image file exceeds max upload size")
	}

	ext := strings.ToLower(filepath.Ext(path))
	mimeType, err := detectMimeType(file, ext)
	if err != nil {
		return detectedImage{}, err
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return detectedImage{}, err
	}

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return detectedImage{}, err
	}

	return detectedImage{
		mimeType: mimeType,
		size:     info.Size(),
		hashHex:  hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func detectMimeType(file *os.File, ext string) (string, error) {
	var header [12]byte
	n, err := io.ReadFull(file, header[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}

	switch ext {
	case ".jpg", ".jpeg":
		if n >= 3 && header[0] == 0xff && header[1] == 0xd8 && header[2] == 0xff {
			return "image/jpeg", nil
		}
		return "", errors.New("invalid JPEG magic bytes")
	case ".png":
		pngMagic := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
		if n >= len(pngMagic) && string(header[:len(pngMagic)]) == string(pngMagic) {
			return "image/png", nil
		}
		return "", errors.New("invalid PNG magic bytes")
	case ".heic":
		return "image/heic", nil
	case ".heif":
		return "image/heif", nil
	default:
		return "", fmt.Errorf("unsupported image extension %q", ext)
	}
}

func moveFile(source string, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0750); err != nil {
		return err
	}

	if err := os.Rename(source, destination); err == nil {
		return nil
	} else if errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := copyFile(source, destination); err != nil {
		return err
	}

	return os.Remove(source)
}

func copyFile(source string, destination string) error {
	src, err := os.Open(source)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0640)
	if err != nil {
		return err
	}

	_, copyErr := io.Copy(dst, src)
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}

	return nil
}
