package upload

import (
	"strings"
	"testing"
)

func TestSafeImageExtensionNormalizesJpeg(t *testing.T) {
	ext, ok := SafeImageExtension(`C:\fakepath\IMG_1.JPEG`, "")
	if !ok {
		t.Fatal("expected extension to be accepted")
	}
	if ext != ".jpg" {
		t.Fatalf("expected .jpg, got %q", ext)
	}
}

func TestIsAllowedImageAllowsKnownMimeWithoutExtension(t *testing.T) {
	if !IsAllowedImage("upload", "image/png") {
		t.Fatal("expected image/png to be accepted")
	}
}

func TestNewStoredFilenameDoesNotUseOriginalBaseName(t *testing.T) {
	name, err := NewStoredFilename("IMG_7585.jpg", "image/jpeg")
	if err != nil {
		t.Fatalf("expected generated filename: %v", err)
	}
	if strings.Contains(name, "IMG_7585") {
		t.Fatalf("stored filename should not include original base name: %q", name)
	}
	if !strings.HasSuffix(name, ".jpg") {
		t.Fatalf("expected .jpg extension, got %q", name)
	}
}
