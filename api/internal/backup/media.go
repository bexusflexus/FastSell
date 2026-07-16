package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type mediaMetadata struct {
	ArchiveFormatVersion int      `json:"archive_format_version"`
	CreatedAt            string   `json:"creation_timestamp"`
	FastSellVersion      string   `json:"fastsell_version"`
	ArchiveFormat        string   `json:"archive_format"`
	ArchiveByteSize      int64    `json:"archive_byte_size"`
	IncludedDirectories  []string `json:"included_directories"`
}

func (s *Service) StartMediaArchive() (Job, error) {
	release, err := s.lock.TryAcquire("media archive")
	if err != nil {
		return Job{}, err
	}
	job := newJob("media_archive", "manual", s.now())
	if err := s.jobs.Save(job); err != nil {
		release()
		return Job{}, errors.New("failed to persist media archive job state")
	}
	snapshot := snapshotJob(job)
	jobID := snapshot.ID
	go func() {
		defer release()
		s.runMediaJob(context.Background(), jobID)
	}()
	return snapshot, nil
}

func (s *Service) runMediaJob(ctx context.Context, jobID string) {
	started := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "running"
		job.Phase = "collecting media directories"
		job.StartedAt = &started
	})

	dataRootInfo, err := os.Lstat(s.cfg.DataRoot)
	if err != nil || dataRootInfo.Mode()&os.ModeSymlink != 0 || !dataRootInfo.IsDir() {
		s.failMedia(jobID, "media data root is not a safe directory")
		return
	}
	included := make([]string, 0, 3)
	for _, name := range []string{"images", "exports", "videos"} {
		root := filepath.Join(s.cfg.DataRoot, name)
		info, err := os.Lstat(root)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			s.failMedia(jobID, "durable media root is not a safe directory")
			return
		}
		if err := rejectMediaSymlinks(root); err != nil {
			s.failMedia(jobID, "media archive refused a symbolic link in a durable media root")
			return
		}
		included = append(included, name)
	}
	if len(included) == 0 {
		s.failMedia(jobID, "no durable media directories exist")
		return
	}

	created := s.now()
	dir := filepath.Join(s.cfg.Root, "media")
	filename := uniqueMediaFilename(dir, fmt.Sprintf("fastsell-media-%s", created.Format("20060102T150405")))
	final := filepath.Join(dir, filename)
	partial := final + ".partial"
	checksumFinal := final + ".sha256"
	checksumPartial := checksumFinal + ".partial"
	metadataFinal := final + ".json"
	metadataPartial := metadataFinal + ".partial"
	defer func() {
		_ = os.Remove(partial)
		_ = os.Remove(checksumPartial)
		_ = os.Remove(metadataPartial)
	}()

	s.setJobPhase(jobID, "creating media archive")
	args := []string{"--zstd", "--create", "--file", partial, "--directory", s.cfg.DataRoot}
	args = append(args, included...)
	if err := s.runner.Run(ctx, "tar", args, nil); err != nil {
		s.failMedia(jobID, fmt.Sprintf("media archive creation failed: %v", err))
		return
	}
	if err := os.Chmod(partial, 0600); err != nil {
		s.failMedia(jobID, "failed to secure media archive permissions")
		return
	}
	if err := syncExistingFile(partial); err != nil {
		s.failMedia(jobID, "failed to flush media archive")
		return
	}
	s.setJobPhase(jobID, "validating media archive")
	if err := s.runner.Run(ctx, "tar", []string{"--zstd", "--list", "--file", partial}, nil); err != nil {
		s.failMedia(jobID, fmt.Sprintf("media archive validation failed: %v", err))
		return
	}
	checksum, size, err := fileSHA256(partial)
	if err != nil {
		s.failMedia(jobID, "failed to checksum media archive")
		return
	}
	if err := writeSyncedFile(checksumPartial, []byte(fmt.Sprintf("%s  %s\n", checksum, filename)), 0600); err != nil {
		s.failMedia(jobID, "failed to write media checksum")
		return
	}
	metadata := mediaMetadata{1, created.Format("2006-01-02T15:04:05Z07:00"), s.cfg.FastSellVersion, "tar.zst", size, included}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		s.failMedia(jobID, "failed to encode media metadata")
		return
	}
	if err := writeSyncedFile(metadataPartial, append(data, '\n'), 0600); err != nil {
		s.failMedia(jobID, "failed to write media metadata")
		return
	}
	if err := os.Rename(checksumPartial, checksumFinal); err != nil {
		s.failMedia(jobID, "failed to publish media checksum")
		return
	}
	if err := os.Rename(metadataPartial, metadataFinal); err != nil {
		_ = os.Remove(checksumFinal)
		s.failMedia(jobID, "failed to publish media metadata")
		return
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(checksumFinal)
		_ = os.Remove(metadataFinal)
		s.failMedia(jobID, "failed to publish media archive")
		return
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Remove(final)
		_ = os.Remove(checksumFinal)
		_ = os.Remove(metadataFinal)
		s.failMedia(jobID, "failed to flush media archive directory")
		return
	}
	completed := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "succeeded"
		job.Phase = "complete"
		job.BackupID = filename
		job.CompletedAt = &completed
	})
}

func rejectMediaSymlinks(root string) error {
	return filepath.WalkDir(root, func(_ string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("symbolic link is not permitted in media archives")
		}
		return nil
	})
}

func uniqueMediaFilename(dir, stem string) string {
	for suffix := 0; suffix < 10000; suffix++ {
		filename := stem + ".tar.zst"
		if suffix > 0 {
			filename = fmt.Sprintf("%s-%d.tar.zst", stem, suffix)
		}
		if _, err := os.Lstat(filepath.Join(dir, filename)); errors.Is(err, os.ErrNotExist) {
			return filename
		}
	}
	return fmt.Sprintf("%s-%s.tar.zst", stem, randomHex(4))
}

func (s *Service) failMedia(jobID, message string) {
	completed := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "failed"
		job.ErrorMessage = sanitizeError(message)
		job.CompletedAt = &completed
	})
}
