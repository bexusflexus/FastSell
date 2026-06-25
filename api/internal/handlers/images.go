package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ImageHandler struct {
	pool      *pgxpool.Pool
	safeRoots []string
}

func NewImageHandler(pool *pgxpool.Pool, safeRoots []string) *ImageHandler {
	roots := make([]string, 0, len(safeRoots))
	for _, root := range safeRoots {
		root = strings.TrimSpace(root)
		if root != "" {
			roots = append(roots, root)
		}
	}

	return &ImageHandler{pool: pool, safeRoots: roots}
}

func (h *ImageHandler) ServeImage(w http.ResponseWriter, r *http.Request) {
	imageID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(imageID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "image id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	var filePath string
	var thumbnailPath *string
	var normalizedPath *string
	var mimeType *string
	if err := h.pool.QueryRow(ctx, `
		SELECT file_path, thumbnail_path, normalized_path, mime_type
		FROM image_assets
		WHERE id = $1
	`, imageID).Scan(&filePath, &thumbnailPath, &normalizedPath, &mimeType); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "image was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load image")
		return
	}

	requestedVariant := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("variant")))
	if requestedVariant == "" {
		requestedVariant = "original"
	}
	switch requestedVariant {
	case "original":
	case "thumbnail":
		if thumbnailPath != nil && strings.TrimSpace(*thumbnailPath) != "" {
			filePath = *thumbnailPath
		}
	case "normalized":
		if normalizedPath != nil && strings.TrimSpace(*normalizedPath) != "" {
			filePath = *normalizedPath
		}
	default:
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "variant must be original, thumbnail, or normalized")
		return
	}

	cleanPath := filepath.Clean(filePath)
	if !isSafeManagedPath(cleanPath, h.safeRoots) {
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "image was not found")
		return
	}

	file, err := os.Open(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "image was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_error", "failed to open image")
		return
	}
	defer file.Close()

	contentType := strings.TrimSpace(derefString(mimeType))
	if contentType == "" {
		sniff := make([]byte, 512)
		n, readErr := file.Read(sniff)
		if readErr != nil && !errors.Is(readErr, io.EOF) {
			respond.ErrorCode(w, http.StatusInternalServerError, "storage_error", "failed to read image")
			return
		}
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			respond.ErrorCode(w, http.StatusInternalServerError, "storage_error", "failed to read image")
			return
		}
		if n > 0 {
			contentType = http.DetectContentType(sniff[:n])
		}
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	if stat, err := file.Stat(); err == nil {
		w.Header().Set("Content-Length", strconv.FormatInt(stat.Size(), 10))
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, file); err != nil {
		return
	}
}
