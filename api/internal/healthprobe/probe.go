package healthprobe

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const probePrefix = ".write-check-"

var probeMu sync.Mutex

// WritableDirectory verifies write access using a short-lived, restrictive
// file. Callers must pass a dedicated probe directory, not application data.
func WritableDirectory(dir string) error {
	return writableDirectory(dir, nil)
}

func writableDirectory(dir string, inspect func(string) error) (err error) {
	probeMu.Lock()
	defer probeMu.Unlock()

	if err := removeStaleProbes(dir); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, probePrefix+"*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer func() {
		closeErr := file.Close()
		removeErr := os.Remove(name)
		if errors.Is(removeErr, os.ErrNotExist) {
			removeErr = nil
		}
		err = errors.Join(err, closeErr, removeErr)
	}()

	if err := file.Chmod(0600); err != nil {
		return err
	}
	if inspect != nil {
		return inspect(name)
	}
	return nil
}

func removeStaleProbes(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), probePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
