package handlers

import (
	"path/filepath"
	"strings"
)

func isSafeManagedPath(path string, roots []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}

	for _, root := range roots {
		if pathIsUnderRoot(path, root) {
			return true
		}
	}

	return false
}

func pathIsUnderRoot(path string, root string) bool {
	if root == "" || !filepath.IsAbs(root) {
		return false
	}

	cleanRoot := filepath.Clean(root)
	cleanPath := filepath.Clean(path)
	rel, err := filepath.Rel(cleanRoot, cleanPath)
	if err != nil {
		return false
	}

	return rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}
