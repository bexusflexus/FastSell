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
	go func() {
		defer release()
		s.runRestoreJob(context.Background(), &job)
	}()
	return job, nil
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
	s.runRestoreJob(ctx, &job)
	if job.State != "succeeded" {
		return job, errors.New(job.ErrorMessage)
	}
	return job, nil
}

func (s *Service) runRestoreJob(ctx context.Context, job *Job) {
	started := s.now()
	job.State = "running"
	job.Phase = "entering maintenance mode"
	job.StartedAt = &started
	_ = s.jobs.Save(*job)

	if err := s.gate.EnterAndWait(ctx); err != nil {
		s.failRestore(job, "entering maintenance mode", "failed to quiesce application writes", false)
		return
	}
	maintenanceMayExit := true
	defer func() {
		if maintenanceMayExit {
			s.gate.Exit()
		}
	}()

	job.Phase = "validating selected backup"
	_ = s.jobs.Save(*job)
	_, _, selectedPath, err := s.preRestoreValidation(ctx, job.BackupID)
	if err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}
	targetInfo, err := s.database.Info(ctx)
	if err != nil {
		s.failRestore(job, "reading installed schema", "failed to read installed database schema", false)
		return
	}

	job.Phase = "creating pre-restore backup"
	_ = s.jobs.Save(*job)
	preJob := newJob("database_backup", "pre_restore", s.now())
	preStarted := s.now()
	preJob.State = "running"
	preJob.StartedAt = &preStarted
	_ = s.jobs.Save(preJob)
	_ = s.settings.RecordAttempt(ctx, preStarted)
	preID, err := s.createDatabaseBackup(ctx, &preJob, false)
	preCompleted := s.now()
	preJob.CompletedAt = &preCompleted
	if err != nil {
		preJob.State = "failed"
		preJob.ErrorMessage = sanitizeError(err.Error())
		_ = s.jobs.Save(preJob)
		_ = s.settings.RecordFailure(ctx, preCompleted, preJob.ErrorMessage)
		s.failRestore(job, "creating pre-restore backup", "pre-restore backup failed; the database was not modified", false)
		return
	}
	preJob.State = "succeeded"
	preJob.Phase = "complete"
	preJob.BackupID = preID
	_ = s.jobs.Save(preJob)
	_ = s.settings.RecordSuccess(ctx, preCompleted)
	job.PreRestoreID = preID
	_ = s.jobs.Save(*job)

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

	job.Phase = "creating restore staging database"
	_ = s.jobs.Save(*job)
	if err := s.database.CreateDatabase(ctx, stagingName); err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}
	stagingExists = true

	job.Phase = "restoring into staging database"
	_ = s.jobs.Save(*job)
	if err := s.restoreDump(ctx, selectedPath, stagingName); err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}

	job.Phase = "applying database migrations to staging"
	_ = s.jobs.Save(*job)
	if err := s.database.MigrateDatabase(ctx, stagingName); err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}

	job.Phase = "validating staged database health"
	_ = s.jobs.Save(*job)
	if err := s.database.VerifyDatabase(ctx, stagingName, targetInfo.SchemaVersion); err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}

	job.Phase = "swapping restored database"
	_ = s.jobs.Save(*job)
	if err := s.database.SwapDatabases(ctx, targetInfo.Name, stagingName, oldName); err != nil {
		s.failRestore(job, job.Phase, err.Error(), false)
		return
	}
	stagingExists = false
	swapped = true

	job.Phase = "validating active restored database"
	_ = s.jobs.Save(*job)
	restoreErr := s.database.VerifyDatabase(ctx, targetInfo.Name, targetInfo.SchemaVersion)
	if restoreErr == nil {
		job.Phase = "rescheduling automatic backups"
		_ = s.jobs.Save(*job)
		_ = s.settings.RecordAttempt(ctx, preStarted)
		_ = s.settings.RecordSuccess(ctx, preCompleted)
		restoreErr = s.reapplySettings(ctx)
	}
	if restoreErr != nil {
		failedPhase := job.Phase
		job.Phase = "rolling back database swap"
		_ = s.jobs.Save(*job)
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
			job.RecoveryMessage = "Automatic database-swap rollback failed. Maintenance mode remains active. Preserve the pre-restore backup and retained databases and use the documented recovery command."
			s.failRestore(job, failedPhase, "restore failed and database health is uncertain", true)
			return
		}
		if dropErr := s.database.DropDatabase(ctx, failedName); dropErr != nil {
			log.Printf("failed restored database cleanup warning: %v", dropErr)
		}
		job.RecoveryMessage = "Restore failed, but the original database was swapped back and validated successfully. The pre-restore backup was preserved."
		s.failRestore(job, failedPhase, "restore failed; automatic rollback succeeded", false)
		return
	}

	job.Phase = "removing retained rollback database"
	_ = s.jobs.Save(*job)
	if err := s.database.DropDatabase(ctx, oldName); err != nil {
		log.Printf("retained rollback database cleanup warning: %v", err)
	}

	completed := s.now()
	job.State = "succeeded"
	job.Phase = "complete"
	job.CompletedAt = &completed
	_ = s.jobs.Save(*job)
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

func (s *Service) failRestore(job *Job, phase, message string, maintenanceActive bool) {
	completed := s.now()
	job.State = "failed"
	job.Phase = phase
	job.ErrorMessage = sanitizeError(message)
	job.CompletedAt = &completed
	if maintenanceActive && job.RecoveryMessage == "" {
		job.RecoveryMessage = "Maintenance mode remains active because database health is uncertain. Use the documented recovery command."
	}
	_ = s.jobs.Save(*job)
}
