package backup

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var safeIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,254}$`)

type JobStore struct{ root string }

func NewJobStore(root string) *JobStore { return &JobStore{root: root} }

func (s *JobStore) Save(job Job) error {
	if !safeIDPattern.MatchString(job.ID) {
		return errors.New("invalid job ID")
	}
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return err
	}
	partial := filepath.Join(s.root, job.ID+".json.partial")
	final := filepath.Join(s.root, job.ID+".json")
	if err := writeSyncedFile(partial, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Rename(partial, final); err != nil {
		_ = os.Remove(partial)
		return err
	}
	return syncDirectory(s.root)
}

func (s *JobStore) Get(id string) (Job, error) {
	if !safeIDPattern.MatchString(id) {
		return Job{}, os.ErrNotExist
	}
	path := filepath.Join(s.root, id+".json")
	if err := requireRegularFile(path); err != nil {
		return Job{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Job{}, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil || job.ID != id {
		return Job{}, errors.New("job state is malformed")
	}
	return job, nil
}

func (s *JobStore) Sources() map[string]string {
	result := make(map[string]string)
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return result
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() > entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, readErr := os.ReadFile(filepath.Join(s.root, entry.Name()))
		if readErr != nil {
			continue
		}
		var job Job
		if json.Unmarshal(data, &job) == nil && job.Kind == "database_backup" && job.State == "succeeded" && job.BackupID != "" {
			if _, exists := result[job.BackupID]; !exists {
				result[job.BackupID] = job.Source
			}
		}
	}
	return result
}

func (s *JobStore) RecoverInterrupted(now time.Time) (bool, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return false, err
	}
	databaseUncertain := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".json")
		job, getErr := s.Get(id)
		if getErr != nil || job.State != "queued" && job.State != "running" {
			continue
		}
		if job.Kind == "database_restore" && restorePhaseMayHaveModifiedDatabase(job.Phase) {
			databaseUncertain = true
			job.RecoveryMessage = "The application restarted during a destructive restore phase. Maintenance mode remains active; preserve all backup sets and use the documented recovery command."
		}
		job.State = "failed"
		job.ErrorMessage = "operation was interrupted by an application restart"
		job.CompletedAt = &now
		if saveErr := s.Save(job); saveErr != nil {
			return databaseUncertain, saveErr
		}
	}
	return databaseUncertain, nil
}

func restorePhaseMayHaveModifiedDatabase(phase string) bool {
	switch phase {
	case "swapping restored database", "validating active restored database", "rescheduling automatic backups", "rolling back database swap":
		return true
	default:
		return false
	}
}
