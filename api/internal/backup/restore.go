package backup

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
)

func (s *Service) StartRestore(backupID, confirmation string) (Job, error) {
	if confirmation != RestoreConfirmation {
		return Job{}, errors.New("confirmation must exactly match RESTORE FASTSELL")
	}
	release, err := s.lock.TryAcquire("database restore")
	if err != nil {
		return Job{}, err
	}
	if _, _, _, err := s.readBackupSet(backupID); err != nil {
		release()
		return Job{}, os.ErrNotExist
	}
	job := newJob("database_restore", "manual", s.now())
	job.BackupID = backupID
	if err := s.jobs.Save(job); err != nil {
		release()
		return Job{}, errors.New("failed to persist restore job state")
	}
	snapshot := snapshotJob(job)
	jobID := snapshot.ID
	go func() {
		defer release()
		s.runRestoreJob(context.Background(), jobID)
	}()
	return snapshot, nil
}

// RunRestore performs the same restore workflow synchronously for the recovery CLI.
func (s *Service) RunRestore(ctx context.Context, backupID, confirmation string) (Job, error) {
	if confirmation != RestoreConfirmation {
		return Job{}, errors.New("confirmation must exactly match RESTORE FASTSELL")
	}
	release, err := s.lock.TryAcquire("database restore")
	if err != nil {
		return Job{}, err
	}
	defer release()
	job := newJob("database_restore", "recovery", s.now())
	job.BackupID = backupID
	if err := s.jobs.Save(job); err != nil {
		return Job{}, errors.New("failed to persist restore job state")
	}
	s.runRestoreJob(ctx, job.ID)
	job, err = s.jobs.Get(job.ID)
	if err != nil {
		return Job{}, errors.New("failed to load completed restore job state")
	}
	if job.State != "succeeded" {
		return job, errors.New(job.ErrorMessage)
	}
	return job, nil
}

func (s *Service) runRestoreJob(ctx context.Context, jobID string) {
	job, err := s.jobs.Get(jobID)
	if err != nil {
		return
	}
	backupID := job.BackupID
	started := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "running"
		job.Phase = "entering maintenance mode"
		job.StartedAt = &started
	})

	if err := s.gate.EnterAndWait(ctx); err != nil {
		s.failRestore(jobID, "entering maintenance mode", "failed to quiesce application writes", false)
		return
	}
	maintenanceMayExit := true
	defer func() {
		if maintenanceMayExit {
			s.gate.Exit()
		}
	}()

	phase := "validating selected backup"
	s.setJobPhase(jobID, phase)
	_, _, selectedPath, err := s.preRestoreValidation(ctx, backupID)
	if err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}
	targetInfo, err := s.database.Info(ctx)
	if err != nil {
		s.failRestore(jobID, "reading installed schema", "failed to read installed database schema", false)
		return
	}

	phase = "creating pre-restore backup"
	s.setJobPhase(jobID, phase)
	preJob := newJob("database_backup", "pre_restore", s.now())
	preStarted := s.now()
	preJob.State = "running"
	preJob.StartedAt = &preStarted
	_ = s.jobs.Save(preJob)
	_ = s.settings.RecordAttempt(ctx, preStarted)
	preID, err := s.createDatabaseBackup(ctx, preJob.ID, false)
	preCompleted := s.now()
	if err != nil {
		message := sanitizeError(err.Error())
		s.updateJob(preJob.ID, func(job *Job) {
			job.State = "failed"
			job.ErrorMessage = message
			job.CompletedAt = &preCompleted
		})
		_ = s.settings.RecordFailure(ctx, preCompleted, message)
		s.failRestore(jobID, phase, "pre-restore backup failed; the database was not modified", false)
		return
	}
	s.updateJob(preJob.ID, func(job *Job) {
		job.State = "succeeded"
		job.Phase = "complete"
		job.BackupID = preID
		job.CompletedAt = &preCompleted
	})
	_ = s.settings.RecordSuccess(ctx, preCompleted)
	s.updateJob(jobID, func(job *Job) { job.PreRestoreID = preID })

	suffix := randomHex(6)
	stagingName := "fastsell_restore_stage_" + suffix
	oldName := "fastsell_restore_old_" + suffix
	failedName := "fastsell_restore_failed_" + suffix
	stagingExists := false
	swapped := false
	defer func() {
		if stagingExists && !swapped {
			if dropErr := s.database.DropDatabase(context.Background(), stagingName); dropErr != nil {
				log.Printf("restore staging database cleanup warning: %v", dropErr)
			}
		}
	}()

	phase = "creating restore staging database"
	s.setJobPhase(jobID, phase)
	if err := s.database.CreateDatabase(ctx, stagingName); err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}
	stagingExists = true

	phase = "restoring into staging database"
	s.setJobPhase(jobID, phase)
	if err := s.restoreDump(ctx, selectedPath, stagingName); err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}

	phase = "applying database migrations to staging"
	s.setJobPhase(jobID, phase)
	if err := s.database.MigrateDatabase(ctx, stagingName); err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}

	phase = "validating staged database health"
	s.setJobPhase(jobID, phase)
	if err := s.database.VerifyDatabase(ctx, stagingName, targetInfo.SchemaVersion); err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}

	phase = "swapping restored database"
	s.setJobPhase(jobID, phase)
	if err := s.database.SwapDatabases(ctx, targetInfo.Name, stagingName, oldName); err != nil {
		s.failRestore(jobID, phase, err.Error(), false)
		return
	}
	stagingExists = false
	swapped = true

	phase = "validating active restored database"
	s.setJobPhase(jobID, phase)
	restoreErr := s.database.VerifyDatabase(ctx, targetInfo.Name, targetInfo.SchemaVersion)
	if restoreErr == nil {
		phase = "rescheduling automatic backups"
		s.setJobPhase(jobID, phase)
		_ = s.settings.RecordAttempt(ctx, preStarted)
		_ = s.settings.RecordSuccess(ctx, preCompleted)
		restoreErr = s.reapplySettings(ctx)
	}
	if restoreErr != nil {
		failedPhase := phase
		s.setJobPhase(jobID, "rolling back database swap")
		rollbackErr := s.database.RollbackSwap(ctx, targetInfo.Name, oldName, failedName)
		if rollbackErr == nil {
			s.database.Reset()
			rollbackErr = s.database.VerifyDatabase(ctx, targetInfo.Name, targetInfo.SchemaVersion)
		}
		if rollbackErr == nil {
			rollbackErr = s.reapplySettings(ctx)
		}
		if rollbackErr != nil {
			maintenanceMayExit = false
			s.updateJob(jobID, func(job *Job) {
				job.RecoveryMessage = "Automatic database-swap rollback failed. Maintenance mode remains active. Preserve the pre-restore backup and retained databases and use the documented recovery command."
			})
			s.failRestore(jobID, failedPhase, "restore failed and database health is uncertain", true)
			return
		}
		if dropErr := s.database.DropDatabase(ctx, failedName); dropErr != nil {
			log.Printf("failed restored database cleanup warning: %v", dropErr)
		}
		s.updateJob(jobID, func(job *Job) {
			job.RecoveryMessage = "Restore failed, but the original database was swapped back and validated successfully. The pre-restore backup was preserved."
		})
		s.failRestore(jobID, failedPhase, "restore failed; automatic rollback succeeded", false)
		return
	}

	s.setJobPhase(jobID, "removing retained rollback database")
	if err := s.database.DropDatabase(ctx, oldName); err != nil {
		log.Printf("retained rollback database cleanup warning: %v", err)
	}

	completed := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "succeeded"
		job.Phase = "complete"
		job.CompletedAt = &completed
	})
}

func (s *Service) restoreDump(ctx context.Context, path, databaseName string) error {
	args := []string{
		"--dbname", databaseName, "--clean", "--if-exists", "--no-owner", "--no-acl", "--exit-on-error", path,
	}
	if err := s.runner.Run(ctx, "pg_restore", args, s.pgEnv); err != nil {
		return fmt.Errorf("database restore command failed: %w", err)
	}
	return nil
}

func (s *Service) failRestore(jobID, phase, message string, maintenanceActive bool) {
	completed := s.now()
	s.updateJob(jobID, func(job *Job) {
		job.State = "failed"
		job.Phase = phase
		job.ErrorMessage = sanitizeError(message)
		job.CompletedAt = &completed
		if maintenanceActive && job.RecoveryMessage == "" {
			job.RecoveryMessage = "Maintenance mode remains active because database health is uncertain. Use the documented recovery command."
		}
	})
}
