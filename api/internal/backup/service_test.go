package backup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

type fakeSettingsStore struct {
	mu       sync.Mutex
	settings Settings
	attempts int
	success  int
	failures int
}

func (s *fakeSettingsStore) Get(context.Context) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.settings, nil
}
func (s *fakeSettingsStore) Update(_ context.Context, value Settings) (Settings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settings = value
	return value, nil
}
func (s *fakeSettingsStore) RecordAttempt(context.Context, time.Time) error {
	s.mu.Lock()
	s.attempts++
	s.mu.Unlock()
	return nil
}
func (s *fakeSettingsStore) RecordSuccess(context.Context, time.Time) error {
	s.mu.Lock()
	s.success++
	s.mu.Unlock()
	return nil
}
func (s *fakeSettingsStore) RecordFailure(context.Context, time.Time, string) error {
	s.mu.Lock()
	s.failures++
	s.mu.Unlock()
	return nil
}

type fakeDatabase struct {
	mu             sync.Mutex
	info           DatabaseInfo
	createCalls    int
	dropCalls      int
	migrateCalls   int
	verifyCalls    int
	swapCalls      int
	rollbackCalls  int
	migrateErr     error
	verifyErr      error
	failActiveOnce int
}

func (d *fakeDatabase) Info(context.Context) (DatabaseInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.info, nil
}
func (d *fakeDatabase) Reset() {}
func (d *fakeDatabase) CreateDatabase(context.Context, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.createCalls++
	return nil
}
func (d *fakeDatabase) DropDatabase(context.Context, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.dropCalls++
	return nil
}
func (d *fakeDatabase) MigrateDatabase(context.Context, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.migrateCalls++
	return d.migrateErr
}
func (d *fakeDatabase) VerifyDatabase(_ context.Context, name string, _ int64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.verifyCalls++
	if name == d.info.Name && d.failActiveOnce > 0 {
		d.failActiveOnce--
		return errors.New("active database validation failed")
	}
	return d.verifyErr
}
func (d *fakeDatabase) SwapDatabases(context.Context, string, string, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.swapCalls++
	return nil
}
func (d *fakeDatabase) RollbackSwap(context.Context, string, string, string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rollbackCalls++
	return nil
}

type fakeRunner struct {
	mu              sync.Mutex
	calls           [][]string
	environments    [][]string
	failDump        bool
	failList        bool
	failTarCreate   error
	failTarList     error
	failRestorePath string
	blockDump       chan struct{}
	blockRestore    chan struct{}
}

func (r *fakeRunner) Run(_ context.Context, name string, args []string, env []string) error {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	r.environments = append(r.environments, append([]string(nil), env...))
	failDump, failList := r.failDump, r.failList
	failTarCreate, failTarList := r.failTarCreate, r.failTarList
	failRestorePath := r.failRestorePath
	blockDump, blockRestore := r.blockDump, r.blockRestore
	r.mu.Unlock()
	if name == "pg_dump" {
		if blockDump != nil {
			<-blockDump
		}
		if failDump {
			return errors.New("dump failed")
		}
		path := argumentAfter(args, "--file")
		return os.WriteFile(path, []byte("custom postgres dump fixture"), 0600)
	}
	if name == "pg_restore" && contains(args, "--list") {
		if failList {
			return errors.New("list failed")
		}
		return nil
	}
	if name == "pg_restore" {
		if blockRestore != nil {
			<-blockRestore
		}
		if len(args) > 0 && args[len(args)-1] == failRestorePath {
			return errors.New("restore failed")
		}
		return nil
	}
	if name == "tar" && contains(args, "--create") {
		if failTarCreate != nil {
			path := argumentAfter(args, "--file")
			_ = os.WriteFile(path, []byte("partial zstd media fixture"), 0600)
			return failTarCreate
		}
		return os.WriteFile(argumentAfter(args, "--file"), []byte("zstd media fixture"), 0600)
	}
	if name == "tar" && contains(args, "--list") && failTarList != nil {
		return failTarList
	}
	return nil
}

func TestDefaultBackupSettings(t *testing.T) {
	got := DefaultSettings()
	if !got.AutomaticEnabled || got.SchedulePreset != "daily" || got.CronExpression != "0 2 * * *" || got.RetentionCount != 14 || got.Timezone != "UTC" {
		t.Fatalf("unexpected defaults: %#v", got)
	}
}

func TestValidateSettingsAndInvalidCron(t *testing.T) {
	settings := DefaultSettings()
	settings.SchedulePreset = "advanced"
	settings.CronExpression = "not cron"
	if _, err := ValidateSettings(settings); err == nil {
		t.Fatal("expected invalid cron error")
	}
	settings.CronExpression = "15 3 * * 1-5"
	settings.Timezone = "America/Chicago"
	if _, err := ValidateSettings(settings); err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
}

func TestSchedulerDisabledAndScheduleUpdates(t *testing.T) {
	var runs atomic.Int64
	scheduler := NewScheduler(func() { runs.Add(1) })
	disabled := DefaultSettings()
	disabled.AutomaticEnabled = false
	if err := scheduler.Start(disabled); err != nil {
		t.Fatal(err)
	}
	if scheduler.entryID != 0 {
		t.Fatal("disabled scheduler registered an entry")
	}
	if err := scheduler.Start(DefaultSettings()); err != nil {
		t.Fatal(err)
	}
	first := scheduler.entryID
	weekly := DefaultSettings()
	weekly.SchedulePreset = "weekly"
	weekly.CronExpression = "0 2 * * 0"
	weekly.Timezone = "America/Chicago"
	if err := scheduler.Start(weekly); err != nil {
		t.Fatal(err)
	}
	if scheduler.entryID == 0 || scheduler.entryID == first {
		t.Fatal("schedule update did not replace cron entry")
	}
	scheduler.Stop()
	if runs.Load() != 0 {
		t.Fatal("scheduler launched an unexpected catch-up backup")
	}
}

func TestManualBackupWorksWhenSchedulingDisabled(t *testing.T) {
	service, settings, _, _ := newTestService(t)
	settings.settings.AutomaticEnabled = false
	job, err := service.StartBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "succeeded" {
		t.Fatalf("manual backup failed: %#v", job)
	}
	if settings.attempts != 1 || settings.success != 1 {
		t.Fatalf("attempt state not recorded: %#v", settings)
	}
}

func TestSingleFlightReturnsConflict(t *testing.T) {
	service, _, _, runner := newTestService(t)
	runner.blockDump = make(chan struct{})
	first, err := service.StartBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	waitForCall(t, runner, "pg_dump")
	if _, err := service.StartMediaArchive(); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	close(runner.blockDump)
	_ = waitJob(t, service, first.ID)
}

func TestSharedFilesystemLockConflictsAcrossServices(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	makeService := func() *Service {
		settings := &fakeSettingsStore{settings: DefaultSettings()}
		database := &fakeDatabase{info: DatabaseInfo{Name: "fastsell", PostgreSQLMajor: 16, SchemaVersion: 3}}
		service, err := NewService(Config{
			Root: backupRoot, DataRoot: filepath.Join(root, "data"), FastSellVersion: "v0.1.4",
			DatabaseURL: "postgres://fastsell:password@postgres:5432/fastsell?sslmode=disable",
		}, database, settings, &fakeRunner{}, NewMaintenanceGate())
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	first := makeService()
	release, err := first.lock.TryAcquire("api database backup")
	if err != nil {
		t.Fatal(err)
	}
	second := makeService()
	if _, err := second.StartBackup("manual"); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("recovery service did not honor API filesystem lock: %v", err)
	}
	release()
	job, err := second.StartBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	if job = waitJob(t, second, job.ID); job.State != "succeeded" {
		t.Fatalf("operation did not proceed after shared lock release: %#v", job)
	}
}

func TestSharedFilesystemLockRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside-lock")
	if err := os.WriteFile(outside, []byte("do not modify"), 0600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(root, "operation.lock")
	if err := os.Symlink(outside, lockPath); err != nil {
		t.Fatal(err)
	}
	if _, err := NewOperationLock(lockPath).TryAcquire("restore"); err == nil {
		t.Fatal("shared operation lock followed a symlink")
	}
	data, err := os.ReadFile(outside)
	if err != nil || string(data) != "do not modify" {
		t.Fatalf("symlink target was modified: %q %v", data, err)
	}
}

func TestServiceRejectsSymlinkedBackupSubdirectory(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	if err := os.MkdirAll(backupRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), filepath.Join(backupRoot, "database")); err != nil {
		t.Fatal(err)
	}
	_, err := NewService(Config{
		Root: backupRoot, DataRoot: filepath.Join(root, "data"), FastSellVersion: "v0.1.4",
		DatabaseURL: "postgres://fastsell:password@postgres:5432/fastsell?sslmode=disable",
	}, &fakeDatabase{}, &fakeSettingsStore{settings: DefaultSettings()}, &fakeRunner{}, NewMaintenanceGate())
	if err == nil {
		t.Fatal("service accepted a symlinked database backup directory")
	}
}

func TestAtomicDumpCreationValidationAndMetadataSafety(t *testing.T) {
	service, _, _, _ := newTestService(t)
	job, _ := service.StartBackup("manual")
	job = waitJob(t, service, job.ID)
	base := filepath.Join(service.cfg.Root, "database", job.BackupID)
	for _, path := range []string{base, base + ".sha256", base + ".json"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("missing artifact %s: %v", path, err)
		}
	}
	if err := verifyChecksum(base); err != nil {
		t.Fatalf("checksum failed: %v", err)
	}
	data, _ := os.ReadFile(base + ".json")
	for _, forbidden := range []string{"password", "postgres:5432", "PGPASSWORD", "token", "secret"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatalf("metadata leaked %q: %s", forbidden, data)
		}
	}
	var metadata map[string]any
	if err := json.Unmarshal(data, &metadata); err != nil {
		t.Fatal(err)
	}
	if len(metadata) != 10 || metadata["source"] != "manual" {
		t.Fatalf("metadata contains unexpected fields: %#v", metadata)
	}
	backups, err := service.ListBackups()
	if err != nil || len(backups) != 1 || backups[0].Source != "manual" {
		t.Fatalf("inventory mismatch: %#v %v", backups, err)
	}
}

func TestFailedDumpAndListValidationDoNotPublish(t *testing.T) {
	for _, mode := range []string{"dump", "list"} {
		t.Run(mode, func(t *testing.T) {
			service, _, _, runner := newTestService(t)
			runner.failDump = mode == "dump"
			runner.failList = mode == "list"
			job, _ := service.StartBackup("manual")
			job = waitJob(t, service, job.ID)
			if job.State != "failed" {
				t.Fatalf("expected failed job: %#v", job)
			}
			backups, _ := service.ListBackups()
			if len(backups) != 0 {
				t.Fatalf("failed dump was published: %#v", backups)
			}
			entries, _ := os.ReadDir(filepath.Join(service.cfg.Root, "database"))
			for _, entry := range entries {
				if strings.HasSuffix(entry.Name(), ".partial") {
					t.Fatalf("partial remained: %s", entry.Name())
				}
			}
		})
	}
}

func TestRetentionPartialCleanupAndTraversalRejection(t *testing.T) {
	service, _, _, _ := newTestService(t)
	for i := 0; i < 3; i++ {
		service.now = func() time.Time { return time.Date(2026, 7, 14, 2, 0, i, 0, time.UTC) }
		job, _ := service.StartBackup("manual")
		_ = waitJob(t, service, job.ID)
	}
	if err := service.ApplyRetention(2, nil); err != nil {
		t.Fatal(err)
	}
	backups, _ := service.ListBackups()
	if len(backups) != 2 {
		t.Fatalf("expected two retained backups, got %d", len(backups))
	}
	partial := filepath.Join(service.cfg.Root, "database", "abandoned.dump.partial")
	if err := os.WriteFile(partial, []byte("partial"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := service.CleanupPartials(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(partial); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial was not removed")
	}
	if _, err := service.ValidateBackup(context.Background(), "../outside.dump"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("traversal was not rejected: %v", err)
	}
}

func TestInventoryRejectsSymlinksNonregularFilesAndPartialSets(t *testing.T) {
	for _, artifact := range []string{"dump", "checksum", "metadata"} {
		t.Run("symlink_"+artifact, func(t *testing.T) {
			service, _, _, _ := newTestService(t)
			job, _ := service.StartBackup("manual")
			job = waitJob(t, service, job.ID)
			base := filepath.Join(service.cfg.Root, "database", job.BackupID)
			target := base
			if artifact == "checksum" {
				target += ".sha256"
			} else if artifact == "metadata" {
				target += ".json"
			}
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Remove(target); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, target); err != nil {
				t.Fatal(err)
			}
			backups, err := service.ListBackups()
			if err != nil || len(backups) != 0 {
				t.Fatalf("inventory accepted symlinked %s: %#v %v", artifact, backups, err)
			}
		})
	}

	service, _, _, _ := newTestService(t)
	dir := filepath.Join(service.cfg.Root, "database")
	fifo := filepath.Join(dir, "fastsell-db-backup-fifo.dump")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	partialSet := filepath.Join(dir, "fastsell-db-backup-incomplete.dump")
	if err := os.WriteFile(partialSet, []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partialSet+".sha256", []byte("incomplete"), 0600); err != nil {
		t.Fatal(err)
	}
	backups, err := service.ListBackups()
	if err != nil || len(backups) != 0 {
		t.Fatalf("inventory accepted a nonregular or partial set: %#v %v", backups, err)
	}
}

func TestRestoreConfirmationChecksumAndDumpValidation(t *testing.T) {
	service, _, _, runner := newTestService(t)
	backupJob, _ := service.StartBackup("manual")
	backupJob = waitJob(t, service, backupJob.ID)
	if _, err := service.StartRestore(backupJob.BackupID, "RESTORE"); err == nil {
		t.Fatal("invalid confirmation accepted")
	}
	base := filepath.Join(service.cfg.Root, "database", backupJob.BackupID)
	if err := os.WriteFile(base, []byte("corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	restoreJob, err := service.StartRestore(backupJob.BackupID, RestoreConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	restoreJob = waitJob(t, service, restoreJob.ID)
	if restoreJob.State != "failed" || !strings.Contains(restoreJob.ErrorMessage, "checksum") {
		t.Fatalf("checksum failure not reported: %#v", restoreJob)
	}

	service2, _, _, runner2 := newTestService(t)
	backupJob, _ = service2.StartBackup("manual")
	backupJob = waitJob(t, service2, backupJob.ID)
	runner2.failList = true
	restoreJob, _ = service2.StartRestore(backupJob.BackupID, RestoreConfirmation)
	restoreJob = waitJob(t, service2, restoreJob.ID)
	if restoreJob.State != "failed" || !strings.Contains(restoreJob.ErrorMessage, "validation") {
		t.Fatalf("dump validation failure not reported: %#v", restoreJob)
	}
	_ = runner
}

func TestRestoreCreatesPreBackupUsesMaintenanceAndValidatesHealth(t *testing.T) {
	service, _, database, runner := newTestService(t)
	backupJob, _ := service.StartBackup("manual")
	backupJob = waitJob(t, service, backupJob.ID)
	runner.blockRestore = make(chan struct{})
	restoreJob, err := service.StartRestore(backupJob.BackupID, RestoreConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool {
		current, getErr := service.GetJob(restoreJob.ID)
		if getErr == nil {
			restoreJob = current
		}
		return service.Gate().Active() && restoreJob.PreRestoreID != "" && callCount(runner, "pg_restore") >= 3
	})
	if done, ok := service.Gate().BeginWrite(); ok {
		done()
		t.Fatal("write admitted during maintenance")
	}
	if err := service.DeleteBackup(restoreJob.PreRestoreID); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("pre-restore backup deletion did not conflict during restore: %v", err)
	}
	close(runner.blockRestore)
	restoreJob = waitJob(t, service, restoreJob.ID)
	if restoreJob.State != "succeeded" || restoreJob.PreRestoreID == "" {
		t.Fatalf("restore did not complete with pre-backup: %#v", restoreJob)
	}
	if service.Gate().Active() {
		t.Fatal("maintenance mode remained active after verified restore")
	}
	if database.createCalls == 0 || database.migrateCalls == 0 || database.verifyCalls < 2 || database.swapCalls == 0 {
		t.Fatalf("migration/health not invoked: %#v", database)
	}
	if _, err := os.Stat(filepath.Join(service.cfg.Root, "database", restoreJob.PreRestoreID)); err != nil {
		t.Fatalf("pre-restore backup was not preserved: %v", err)
	}
	preMetadataBytes, err := os.ReadFile(filepath.Join(service.cfg.Root, "database", restoreJob.PreRestoreID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var preMetadata Metadata
	if err := json.Unmarshal(preMetadataBytes, &preMetadata); err != nil || preMetadata.Source != "pre_restore" {
		t.Fatalf("pre-restore origin is not portable: %#v %v", preMetadata, err)
	}
}

func TestRestoreFailureRollsBackAndReportsRecovery(t *testing.T) {
	service, _, database, _ := newTestService(t)
	backupJob, _ := service.StartBackup("manual")
	backupJob = waitJob(t, service, backupJob.ID)
	database.failActiveOnce = 1
	restoreJob, _ := service.StartRestore(backupJob.BackupID, RestoreConfirmation)
	restoreJob = waitJob(t, service, restoreJob.ID)
	if restoreJob.State != "failed" || !strings.Contains(restoreJob.RecoveryMessage, "swapped back") || database.rollbackCalls != 1 {
		t.Fatalf("rollback recovery not reported: %#v", restoreJob)
	}
	if service.Gate().Active() {
		t.Fatal("maintenance remained active after successful rollback")
	}
	if _, err := os.Stat(filepath.Join(service.cfg.Root, "database", restoreJob.PreRestoreID)); err != nil {
		t.Fatalf("rollback did not preserve pre-restore backup: %v", err)
	}
}

func TestMediaArchiveContainsOnlyDurableMediaRoots(t *testing.T) {
	service, _, _, runner := newTestService(t)
	for _, dir := range []string{"images", "exports"} {
		if err := os.MkdirAll(filepath.Join(service.cfg.DataRoot, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	job, err := service.StartMediaArchive()
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "succeeded" {
		t.Fatalf("media archive failed: %#v", job)
	}
	runner.mu.Lock()
	defer runner.mu.Unlock()
	joined := ""
	for _, call := range runner.calls {
		if call[0] == "tar" && contains(call, "--create") {
			for index, arg := range call {
				if arg == "--directory" && index+2 < len(call) {
					joined = strings.Join(call[index+2:], " ")
				}
			}
		}
	}
	if !strings.Contains(joined, "images") || !strings.Contains(joined, "exports") {
		t.Fatalf("media roots missing: %s", joined)
	}
	for _, forbidden := range []string{"postgres", "intake", "backups", ".env"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("media command included %q: %s", forbidden, joined)
		}
	}
}

func TestMediaArchiveRejectsSymlinksOutsideDurableRoots(t *testing.T) {
	service, _, _, _ := newTestService(t)
	images := filepath.Join(service.cfg.DataRoot, "images")
	if err := os.MkdirAll(images, 0700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "private.env")
	if err := os.WriteFile(outside, []byte("not archive data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(images, "escape")); err != nil {
		t.Fatal(err)
	}
	job, err := service.StartMediaArchive()
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "failed" || !strings.Contains(job.ErrorMessage, "symbolic link") {
		t.Fatalf("unsafe media symlink was not rejected: %#v", job)
	}
	entries, err := os.ReadDir(filepath.Join(service.cfg.Root, "media"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsafe media archive was published: %v %#v", err, entries)
	}
}

func TestMediaArchiveValidationReportsTarReasonAndPublishesNothing(t *testing.T) {
	service, _, _, runner := newTestService(t)
	if err := os.MkdirAll(filepath.Join(service.cfg.DataRoot, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	runner.failTarList = &CommandError{
		Executable: "tar",
		Status:     2,
		Stderr:     "zstd: premature end",
		Err:        errors.New("exit status 2"),
	}
	job, err := service.StartMediaArchive()
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "failed" || !strings.Contains(job.ErrorMessage, "media archive validation failed: tar exited with status 2: zstd: premature end") {
		t.Fatalf("underlying tar validation error was not retained: %#v", job)
	}
	entries, readErr := os.ReadDir(filepath.Join(service.cfg.Root, "media"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed archive published partial or completed artifacts: %#v", entries)
	}
}

func TestMediaArchiveCreationFailureRemovesPartialFiles(t *testing.T) {
	service, _, _, runner := newTestService(t)
	if err := os.MkdirAll(filepath.Join(service.cfg.DataRoot, "images"), 0700); err != nil {
		t.Fatal(err)
	}
	runner.failTarCreate = &CommandError{
		Executable: "tar",
		Status:     1,
		Stderr:     "images/file.jpg: file changed as we read it",
		Err:        errors.New("exit status 1"),
	}
	job, err := service.StartMediaArchive()
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "failed" || !strings.Contains(job.ErrorMessage, "file changed as we read it") {
		t.Fatalf("underlying tar creation error was not retained: %#v", job)
	}
	entries, readErr := os.ReadDir(filepath.Join(service.cfg.Root, "media"))
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed archive published partial or completed artifacts: %#v", entries)
	}
}

func TestDatabaseCredentialsNeverEnterArgumentsOrPortableState(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://leak-user:leak-password@private-host/secret-db")
	t.Setenv("PGPASSWORD", "inherited-leak-password")
	service, _, _, runner := newTestService(t)
	job, err := service.StartBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	job = waitJob(t, service, job.ID)
	if job.State != "succeeded" {
		t.Fatalf("backup failed: %#v", job)
	}
	runner.mu.Lock()
	for _, call := range runner.calls {
		joined := strings.Join(call, " ")
		for _, forbidden := range []string{"leak-password", "inherited-leak-password", "postgres://", "PGPASSWORD"} {
			if strings.Contains(joined, forbidden) {
				runner.mu.Unlock()
				t.Fatalf("process argument exposed %q: %s", forbidden, joined)
			}
		}
	}
	for _, env := range runner.environments {
		for _, value := range env {
			if strings.HasPrefix(value, "DATABASE_URL=") || value == "PGPASSWORD=inherited-leak-password" {
				runner.mu.Unlock()
				t.Fatalf("unsafe inherited database environment reached child: %s", value)
			}
		}
	}
	runner.mu.Unlock()
	for _, path := range []string{
		filepath.Join(service.cfg.Root, "database", job.BackupID+".json"),
		filepath.Join(service.cfg.Root, "jobs", job.ID+".json"),
	} {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		for _, forbidden := range []string{"leak-password", "inherited-leak-password", "postgres://", "PGPASSWORD"} {
			if strings.Contains(string(data), forbidden) {
				t.Fatalf("portable state exposed %q in %s", forbidden, path)
			}
		}
	}
	if got := sanitizeError("postgres://user:leak-password@private-host/db"); strings.Contains(got, "leak-password") {
		t.Fatalf("sanitized error retained a database URL: %q", got)
	}
}

func TestInterruptedDestructiveRestoreReentersMaintenance(t *testing.T) {
	root := t.TempDir()
	backupRoot := filepath.Join(root, "backups")
	jobsRoot := filepath.Join(backupRoot, "jobs")
	if err := os.MkdirAll(jobsRoot, 0700); err != nil {
		t.Fatal(err)
	}
	store := NewJobStore(jobsRoot)
	job := newJob("database_restore", "manual", time.Now().UTC())
	job.State = "running"
	job.Phase = "swapping restored database"
	if err := store.Save(job); err != nil {
		t.Fatal(err)
	}
	gate := NewMaintenanceGate()
	settings := &fakeSettingsStore{settings: DefaultSettings()}
	database := &fakeDatabase{info: DatabaseInfo{Name: "fastsell", PostgreSQLMajor: 16, SchemaVersion: 3}}
	service, err := NewService(Config{Root: backupRoot, DataRoot: filepath.Join(root, "data"), FastSellVersion: "v0.1.4", DatabaseURL: "postgres://fastsell:password@postgres:5432/fastsell?sslmode=disable"}, database, settings, &fakeRunner{}, gate)
	if err != nil {
		t.Fatal(err)
	}
	if !service.Gate().Active() {
		t.Fatal("maintenance mode was not restored after interrupted destructive restore")
	}
	recovered, err := service.GetJob(job.ID)
	if err != nil || recovered.State != "failed" || recovered.RecoveryMessage == "" {
		t.Fatalf("interrupted job was not recovered safely: %#v %v", recovered, err)
	}
	if _, err := service.StartBackup("scheduled"); !errors.Is(err, ErrOperationConflict) {
		t.Fatalf("scheduled backup was allowed during uncertain maintenance: %v", err)
	}
}

func newTestService(t *testing.T) (*Service, *fakeSettingsStore, *fakeDatabase, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	dataRoot := filepath.Join(root, "data")
	if err := os.MkdirAll(dataRoot, 0700); err != nil {
		t.Fatal(err)
	}
	settings := &fakeSettingsStore{settings: DefaultSettings()}
	database := &fakeDatabase{info: DatabaseInfo{Name: "fastsell", PostgreSQLMajor: 16, SchemaVersion: 3}}
	runner := &fakeRunner{}
	service, err := NewService(Config{Root: filepath.Join(root, "backups"), DataRoot: dataRoot, FastSellVersion: "v0.1.4", DatabaseURL: "postgres://fastsell:password@postgres:5432/fastsell?sslmode=disable"}, database, settings, runner, NewMaintenanceGate())
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 7, 14, 2, 0, 0, 0, time.UTC) }
	info, statErr := os.Stat(service.cfg.Root)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("backup root permissions are not 0700: %#o", info.Mode().Perm())
	}
	return service, settings, database, runner
}

func waitJob(t *testing.T, service *Service, id string) Job {
	return waitJobWithin(t, service, id, 3*time.Second)
}

func waitJobWithin(t *testing.T, service *Service, id string, timeout time.Duration) Job {
	t.Helper()
	var job Job
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var err error
		job, err = service.GetJob(id)
		if err == nil && (job.State == "succeeded" || job.State == "failed") {
			return job
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for job %s", id)
	return Job{}
}

func waitUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

func waitForCall(t *testing.T, runner *fakeRunner, name string) {
	t.Helper()
	waitUntil(t, func() bool { return callCount(runner, name) > 0 })
}
func callCount(runner *fakeRunner, name string) int {
	runner.mu.Lock()
	defer runner.mu.Unlock()
	count := 0
	for _, call := range runner.calls {
		if call[0] == name {
			count++
		}
	}
	return count
}
func argumentAfter(args []string, key string) string {
	for i := range args {
		if args[i] == key && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
func contains(args []string, value string) bool {
	for _, arg := range args {
		if arg == value {
			return true
		}
	}
	return false
}
