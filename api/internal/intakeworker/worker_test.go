package intakeworker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateAndHashImageAcceptsPngMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.png")
	png := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0x00, 0x00, 0x00, 0x0d}
	if err := os.WriteFile(path, png, 0640); err != nil {
		t.Fatalf("write png fixture: %v", err)
	}

	detected, err := validateAndHashImage(path, 1024)
	if err != nil {
		t.Fatalf("expected png to validate: %v", err)
	}
	if detected.mimeType != "image/png" {
		t.Fatalf("expected image/png, got %q", detected.mimeType)
	}
	if detected.size != int64(len(png)) {
		t.Fatalf("expected size %d, got %d", len(png), detected.size)
	}
}

func TestValidateAndHashImageRejectsInvalidJpegMagic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.jpg")
	if err := os.WriteFile(path, []byte("not really a jpg"), 0640); err != nil {
		t.Fatalf("write jpg fixture: %v", err)
	}

	if _, err := validateAndHashImage(path, 1024); err == nil {
		t.Fatal("expected invalid jpeg to fail")
	}
}

func TestMoveFileFallsBackWithinFilesystem(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source.png")
	destination := filepath.Join(dir, "nested", "destination.png")

	if err := os.WriteFile(source, []byte("data"), 0640); err != nil {
		t.Fatalf("write source: %v", err)
	}

	if err := moveFile(source, destination); err != nil {
		t.Fatalf("move file: %v", err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("expected destination: %v", err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("expected source removed, got %v", err)
	}
}
