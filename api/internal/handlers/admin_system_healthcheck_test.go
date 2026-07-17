package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	backupsvc "fastsell-api/internal/backup"
	"fastsell-api/internal/config"
)

type filesystemSnapshot struct {
	Mode    os.FileMode
	Size    int64
	ModTime int64
}

func TestRepeatedPathHealthChecksDoNotModifyArchivedMedia(t *testing.T) {
	cfg := newPathHealthTestConfig(t)
	store := &AdminSystemStore{cfg: cfg}
	before := snapshotArchivedMedia(t, cfg.DataRoot)

	oldTime := time.Unix(946684800, 0)
	probeDir := filepath.Join(cfg.BackupRoot, ".healthcheck")
	if err := os.Chtimes(probeDir, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		health := store.buildPathsHealth()
		if health.Status != "ok" {
			t.Fatalf("path health failed: %#v", health)
		}
	}

	after := snapshotArchivedMedia(t, cfg.DataRoot)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("health checks modified archived media\nbefore: %#v\nafter: %#v", before, after)
	}
	probeInfo, err := os.Stat(probeDir)
	if err != nil {
		t.Fatal(err)
	}
	if probeInfo.ModTime().Equal(oldTime) {
		t.Fatal("dedicated write-probe directory was not exercised")
	}
	assertNoWriteChecks(t, cfg.DataRoot)
	assertNoWriteChecks(t, cfg.BackupRoot)
}

func TestMediaBackupSucceedsDuringRepeatedPathHealthChecks(t *testing.T) {
	cfg := newPathHealthTestConfig(t)
	store := &AdminSystemStore{cfg: cfg}
	runner := &healthRaceMediaRunner{
		dataRoot:       cfg.DataRoot,
		archiveStarted: make(chan struct{}),
		healthDone:     make(chan struct{}),
	}
	service, err := backupsvc.NewService(backupsvc.Config{
		Root:            cfg.BackupRoot,
		DataRoot:        cfg.DataRoot,
		FastSellVersion: "test",
		DatabaseURL:     "postgres://fastsell:test@postgres:5432/fastsell?sslmode=disable",
	}, nil, nil, runner, backupsvc.NewMaintenanceGate())
	if err != nil {
		t.Fatal(err)
	}

	job, err := service.StartMediaArchive()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-runner.archiveStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("media archive did not start")
	}
	var healthErr error
	for range 100 {
		if health := store.buildPathsHealth(); health.Status != "ok" {
			healthErr = fmt.Errorf("path health failed: %#v", health)
			break
		}
	}
	close(runner.healthDone)
	if healthErr != nil {
		t.Fatal(healthErr)
	}

	completed := waitForBackupJob(t, service, job.ID)
	if completed.State != "succeeded" {
		t.Fatalf("media backup failed while health checks ran: %#v", completed)
	}
	assertNoWriteChecks(t, cfg.DataRoot)
	assertNoWriteChecks(t, cfg.BackupRoot)
}

type healthRaceMediaRunner struct {
	dataRoot       string
	archiveStarted chan struct{}
	healthDone     chan struct{}
}

func (r *healthRaceMediaRunner) Run(_ context.Context, name string, args []string, _ []string) error {
	if name != "tar" {
		return nil
	}
	if containsArgument(args, "--create") {
		before, err := archivedMediaSnapshot(r.dataRoot)
		if err != nil {
			return err
		}
		close(r.archiveStarted)
		<-r.healthDone
		after, err := archivedMediaSnapshot(r.dataRoot)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			return errors.New("archived media changed while tar was running")
		}
		return os.WriteFile(argumentValue(args, "--file"), []byte("media archive fixture"), 0600)
	}
	return nil
}

func newPathHealthTestConfig(t *testing.T) config.Config {
	t.Helper()
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	backupRoot := filepath.Join(root, "backups")
	cfg := config.Config{
		DataRoot:               dataRoot,
		BackupRoot:             backupRoot,
		IntakeDir:              filepath.Join(dataRoot, "intake", "incoming"),
		IntakeProcessingDir:    filepath.Join(dataRoot, "intake", "processing"),
		IntakeFailedDir:        filepath.Join(dataRoot, "intake", "failed"),
		ImageOriginalsDir:      filepath.Join(dataRoot, "images", "originals"),
		ImageThumbnailsDir:     filepath.Join(dataRoot, "images", "thumbnails"),
		ImageNormalizedDir:     filepath.Join(dataRoot, "images", "normalized"),
		ListingPhotoExportRoot: filepath.Join(dataRoot, "exports", "listing-photos"),
	}
	for _, dir := range []string{
		cfg.IntakeDir,
		cfg.IntakeProcessingDir,
		cfg.IntakeFailedDir,
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.ListingPhotoExportRoot,
		filepath.Join(dataRoot, "videos"),
		filepath.Join(backupRoot, ".healthcheck"),
	} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{
		filepath.Join(cfg.ImageOriginalsDir, "original.jpg"),
		filepath.Join(cfg.ImageThumbnailsDir, "thumbnail.jpg"),
		filepath.Join(cfg.ImageNormalizedDir, "normalized.jpg"),
		filepath.Join(cfg.ListingPhotoExportRoot, "export.jpg"),
		filepath.Join(dataRoot, "videos", "video.mp4"),
	} {
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return cfg
}

func snapshotArchivedMedia(t *testing.T, dataRoot string) map[string]filesystemSnapshot {
	t.Helper()
	snapshot, err := archivedMediaSnapshot(dataRoot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func archivedMediaSnapshot(dataRoot string) (map[string]filesystemSnapshot, error) {
	result := make(map[string]filesystemSnapshot)
	for _, name := range []string{"images", "exports", "videos"} {
		root := filepath.Join(dataRoot, name)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(dataRoot, path)
			if err != nil {
				return err
			}
			result[relative] = filesystemSnapshot{Mode: info.Mode(), Size: info.Size(), ModTime: info.ModTime().UnixNano()}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func assertNoWriteChecks(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if strings.HasPrefix(info.Name(), ".write-check-") {
			return fmt.Errorf("write probe remained at %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func waitForBackupJob(t *testing.T, service *backupsvc.Service, id string) backupsvc.Job {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		job, err := service.GetJob(id)
		if err == nil && (job.State == "succeeded" || job.State == "failed") {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for media backup job %s", id)
	return backupsvc.Job{}
}

func containsArgument(args []string, target string) bool {
	for _, arg := range args {
		if arg == target {
			return true
		}
	}
	return false
}

func argumentValue(args []string, target string) string {
	for index, arg := range args {
		if arg == target && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}
