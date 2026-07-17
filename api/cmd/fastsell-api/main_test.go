package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestVerifyAccessibleDirectoryDoesNotModifyDirectory(t *testing.T) {
	dir := t.TempDir()
	fixture := filepath.Join(dir, "fixture")
	if err := os.WriteFile(fixture, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}

	for range 100 {
		if err := verifyAccessibleDirectory(dir); err != nil {
			t.Fatal(err)
		}
	}

	after, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entryNames(before), entryNames(after)) || !beforeInfo.ModTime().Equal(afterInfo.ModTime()) {
		t.Fatalf("read-only directory check modified contents: before=%v after=%v", entryNames(before), entryNames(after))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for index, entry := range entries {
		names[index] = entry.Name()
	}
	return names
}
