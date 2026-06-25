package upload

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"strings"
)

var allowedExtensions = map[string]string{
	".jpg":  ".jpg",
	".jpeg": ".jpg",
	".png":  ".png",
	".heic": ".heic",
	".heif": ".heif",
}

var mimeExtensions = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/heic": ".heic",
	"image/heif": ".heif",
}

func SafeImageExtension(originalFilename string, mimeType string) (string, bool) {
	filename := strings.ReplaceAll(originalFilename, "\\", "/")
	ext := strings.ToLower(filepath.Ext(filepath.Base(filename)))
	if normalized, ok := allowedExtensions[ext]; ok {
		return normalized, true
	}

	normalized, ok := mimeExtensions[strings.ToLower(strings.TrimSpace(mimeType))]
	return normalized, ok
}

func IsAllowedImage(originalFilename string, mimeType string) bool {
	if _, ok := mimeExtensions[strings.ToLower(strings.TrimSpace(mimeType))]; ok {
		return true
	}

	_, ok := SafeImageExtension(originalFilename, mimeType)
	return ok
}

func NewStoredFilename(originalFilename string, mimeType string) (string, error) {
	id, err := newUUIDString()
	if err != nil {
		return "", err
	}

	ext, ok := SafeImageExtension(originalFilename, mimeType)
	if !ok {
		ext = ".bin"
	}

	return id + ext, nil
}

func newUUIDString() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80

	return hex.EncodeToString(b[0:4]) + "-" +
		hex.EncodeToString(b[4:6]) + "-" +
		hex.EncodeToString(b[6:8]) + "-" +
		hex.EncodeToString(b[8:10]) + "-" +
		hex.EncodeToString(b[10:16]), nil
}
