package handlers

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"fastsell-api/internal/models"
)

var (
	errUnsafeManagedFilePath  = errors.New("referenced file path is outside approved FastSell data roots")
	errManagedFileIsDirectory = errors.New("referenced file path resolves to a directory")
)

type ManagedFileService struct {
	safeRoots []string
}

type managedFileReference struct {
	ImageAssetID string
	Kind         string
	Path         string
}

type managedFileInspection struct {
	Files              []models.DeletePreviewFile `json:"files"`
	DeletePaths        []string
	TotalFileSizeBytes int64
}

func NewManagedFileService(safeRoots []string) *ManagedFileService {
	roots := make([]string, 0, len(safeRoots))
	for _, root := range safeRoots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		roots = append(roots, filepath.Clean(root))
	}

	return &ManagedFileService{safeRoots: roots}
}

func (s *ManagedFileService) InspectReferences(refs []managedFileReference) (managedFileInspection, error) {
	inspection := managedFileInspection{
		Files:       make([]models.DeletePreviewFile, 0, len(refs)),
		DeletePaths: make([]string, 0, len(refs)),
	}
	seen := make(map[string]struct{})

	for _, ref := range refs {
		cleanPath := filepath.Clean(strings.TrimSpace(ref.Path))
		if cleanPath == "." || cleanPath == "" {
			continue
		}
		if !isSafeManagedPath(cleanPath, s.safeRoots) {
			return managedFileInspection{}, fmt.Errorf("%w: %s", errUnsafeManagedFilePath, cleanPath)
		}
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}

		entry := models.DeletePreviewFile{
			ImageAssetID: ref.ImageAssetID,
			Kind:         ref.Kind,
			Path:         cleanPath,
		}

		stat, err := os.Stat(cleanPath)
		switch {
		case err == nil:
			if stat.IsDir() {
				return managedFileInspection{}, fmt.Errorf("%w: %s", errManagedFileIsDirectory, cleanPath)
			}
			entry.Exists = true
			entry.SizeBytes = stat.Size()
			inspection.TotalFileSizeBytes += stat.Size()
		case os.IsNotExist(err):
			entry.Exists = false
			entry.SizeBytes = 0
		default:
			return managedFileInspection{}, err
		}

		inspection.Files = append(inspection.Files, entry)
		inspection.DeletePaths = append(inspection.DeletePaths, cleanPath)
	}

	return inspection, nil
}

func (s *ManagedFileService) DeleteFiles(paths []string, logContext string) (int, int, []string) {
	deleted := 0
	missing := 0
	warnings := make([]string, 0)
	seen := make(map[string]struct{})

	for _, rawPath := range paths {
		cleanPath := filepath.Clean(strings.TrimSpace(rawPath))
		if cleanPath == "." || cleanPath == "" {
			continue
		}
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}

		if !isSafeManagedPath(cleanPath, s.safeRoots) {
			warnings = append(warnings, fmt.Sprintf("skipped unsafe referenced file path: %s", cleanPath))
			log.Printf("%s skipped unsafe referenced file path: %s", logContext, cleanPath)
			continue
		}

		stat, err := os.Stat(cleanPath)
		switch {
		case err == nil:
			if stat.IsDir() {
				warnings = append(warnings, fmt.Sprintf("skipped directory path referenced as a file: %s", cleanPath))
				log.Printf("%s skipped directory path referenced as a file: %s", logContext, cleanPath)
				continue
			}
		case os.IsNotExist(err):
			missing++
			warnings = append(warnings, fmt.Sprintf("referenced file was already missing: %s", cleanPath))
			continue
		default:
			warnings = append(warnings, fmt.Sprintf("failed to inspect referenced file before deletion: %s", cleanPath))
			log.Printf("%s failed to inspect referenced file path=%s error=%v", logContext, cleanPath, err)
			continue
		}

		if err := os.Remove(cleanPath); err != nil {
			if os.IsNotExist(err) {
				missing++
				warnings = append(warnings, fmt.Sprintf("referenced file was already missing: %s", cleanPath))
				continue
			}
			warnings = append(warnings, fmt.Sprintf("failed to delete referenced file: %s", cleanPath))
			log.Printf("%s failed to delete referenced file path=%s error=%v", logContext, cleanPath, err)
			continue
		}

		deleted++
	}

	return deleted, missing, warnings
}
