package handlers

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestTinyVerifierPNGDecodesAndVariantsWrite(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "tiny.png")
	thumbnailPath := filepath.Join(dir, "thumb.png")
	normalizedPath := filepath.Join(dir, "normalized.png")

	srcImage := image.NewRGBA(image.Rect(0, 0, 1, 1))
	srcImage.Set(0, 0, color.RGBA{R: 255, A: 255})
	file, err := os.Create(originalPath)
	if err != nil {
		t.Fatalf("create original: %v", err)
	}
	if err := png.Encode(file, srcImage); err != nil {
		_ = file.Close()
		t.Fatalf("encode original: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close original: %v", err)
	}

	file, err = os.Open(originalPath)
	if err != nil {
		t.Fatalf("open original: %v", err)
	}
	defer file.Close()

	img, format, err := image.Decode(file)
	if err != nil {
		t.Fatalf("decode original: %v", err)
	}
	if format != "png" {
		t.Fatalf("expected png format, got %q", format)
	}

	if err := writeVariantImage(img, format, thumbnailPath, defaultThumbnailMaxEdge); err != nil {
		t.Fatalf("write thumbnail: %v", err)
	}
	if err := writeVariantImage(img, format, normalizedPath, defaultNormalizedMaxEdge); err != nil {
		t.Fatalf("write normalized: %v", err)
	}

	if _, err := os.Stat(thumbnailPath); err != nil {
		t.Fatalf("stat thumbnail: %v", err)
	}
	if _, err := os.Stat(normalizedPath); err != nil {
		t.Fatalf("stat normalized: %v", err)
	}
}
