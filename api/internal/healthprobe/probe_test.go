package healthprobe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWritableDirectoryUsesRestrictiveTemporaryFileAndCleansProbes(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, probePrefix+"stale")
	if err := os.WriteFile(stale, []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(dir, "keep")
	if err := os.WriteFile(preserved, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}

	err := writableDirectory(dir, func(path string) error {
		info, err := os.Stat(path)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("probe mode = %#o, want 0600", info.Mode().Perm())
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), probePrefix) {
			t.Fatalf("probe file accumulated: %s", entry.Name())
		}
	}
	if data, err := os.ReadFile(preserved); err != nil || string(data) != "keep" {
		t.Fatalf("unrelated file changed: %q %v", data, err)
	}
}
