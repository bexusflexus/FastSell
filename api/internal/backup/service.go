package backup

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Root            string
	DataRoot        string
	FastSellVersion string
	DatabaseURL     string
}

type Service struct {
	cfg           Config
	database      Database
	settings      SettingsStore
	runner        CommandRunner
	jobs          *JobStore
	lock          *OperationLock
	gate          *MaintenanceGate
	pgEnv         []string
	now           func() time.Time
	applySettings func(Settings) error
}

func NewService(cfg Config, database Database, settings SettingsStore, runner CommandRunner, gate *MaintenanceGate) (*Service, error) {
	if filepath.Clean(cfg.Root) == "." || !filepath.IsAbs(cfg.Root) {
		return nil, errors.New("backup root must be an absolute path")
	}
	if runner == nil {
		runner = ExecRunner{}
	}
	if gate == nil {
		gate = NewMaintenanceGate()
	}
	if err := ensureSecureDirectory(cfg.Root); err != nil {
		return nil, err
	}
	for _, dir := range []string{
		filepath.Join(cfg.Root, "database"),
		filepath.Join(cfg.Root, "media"),
		filepath.Join(cfg.Root, "jobs"),
		filepath.Join(cfg.Root, "restore-staging"),
	} {
		if err := ensureSecureDirectory(dir); err != nil {
			return nil, err
		}
	}
	pgEnv, err := postgresEnvironment(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	s := &Service{
		cfg: cfg, database: database, settings: settings, runner: runner,
		jobs: NewJobStore(filepath.Join(cfg.Root, "jobs")),
		lock: NewOperationLock(filepath.Join(cfg.Root, "jobs", "operation.lock")),
		gate: gate, pgEnv: pgEnv, now: func() time.Time { return time.Now().UTC() },
	}
	startupRelease, lockErr := s.lock.TryAcquire("startup recovery")
	if lockErr == nil {
		uncertainRestore, recoverErr := s.jobs.RecoverInterrupted(s.now())
		if recoverErr != nil {
			startupRelease()
			return nil, errors.New("failed to recover persisted backup job state")
		}
		if uncertainRestore {
			if err := s.gate.EnterAndWait(context.Background()); err != nil {
				startupRelease()
				return nil, errors.New("failed to restore maintenance mode")
			}
		}
		if err := s.CleanupPartials(); err != nil {
			log.Printf("backup startup partial cleanup warning: %s", sanitizeError(err.Error()))
		}
		startupRelease()
	} else if !errors.Is(lockErr, ErrOperationConflict) {
		return nil, lockErr
	}
	return s, nil
}

func ensureSecureDirectory(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("backup path is not a safe directory")
	}
	return os.Chmod(path, 0700)
}

func (s *Service) Gate() *MaintenanceGate { return s.gate }

func (s *Service) SetSettingsApplyHook(hook func(Settings) error) { s.applySettings = hook }

func (s *Service) GetSettings(ctx context.Context) (Settings, error) {
	return s.settings.Get(ctx)
}

func (s *Service) UpdateSettings(ctx context.Context, input Settings) (Settings, error) {
	settings, err := ValidateSettings(input)
	if err != nil {
		return Settings{}, err
	}
	updated, err := s.settings.Update(ctx, settings)
	if err != nil {
		return Settings{}, errors.New("failed to save backup settings")
	}
	if s.applySettings != nil {
		if err := s.applySettings(updated); err != nil {
			return Settings{}, errors.New("backup settings were saved but the scheduler could not be updated")
		}
	}
	return updated, nil
}

func (s *Service) reapplySettings(ctx context.Context) error {
	settings, err := s.settings.Get(ctx)
	if err != nil {
		return errors.New("failed to reload backup settings")
	}
	settings, err = ValidateSettings(settings)
	if err != nil {
		return errors.New("restored backup settings are invalid")
	}
	if s.applySettings != nil {
		if err := s.applySettings(settings); err != nil {
			return errors.New("failed to reschedule automatic backups")
		}
	}
	return nil
}

func (s *Service) StartBackup(source string) (Job, error) {
	if s.gate.Active() {
		return Job{}, ErrOperationConflict
	}
	if source != "scheduled" && source != "manual" {
		source = "manual"
	}
	release, err := s.lock.TryAcquire("database backup")
	if err != nil {
		return Job{}, err
	}
	job := newJob("database_backup", source, s.now())
	if err := s.jobs.Save(job); err != nil {
		release()
		return Job{}, errors.New("failed to persist backup job state")
	}
	go func() {
		defer release()
		s.runBackupJob(context.Background(), &job, true)
	}()
	return job, nil
}

func (s *Service) GetJob(id string) (Job, error) { return s.jobs.Get(id) }

func (s *Service) runBackupJob(ctx context.Context, job *Job, retention bool) {
	now := s.now()
	job.State = "running"
	job.Phase = "preparing"
	job.StartedAt = &now
	_ = s.jobs.Save(*job)
	_ = s.settings.RecordAttempt(ctx, now)

	backupID, err := s.createDatabaseBackup(ctx, job, retention)
	completed := s.now()
	job.CompletedAt = &completed
	if err != nil {
		job.State = "failed"
		job.ErrorMessage = sanitizeError(err.Error())
		_ = s.settings.RecordFailure(ctx, completed, job.ErrorMessage)
	} else {
		job.State = "succeeded"
		job.Phase = "complete"
		job.BackupID = backupID
		_ = s.settings.RecordSuccess(ctx, completed)
	}
	_ = s.jobs.Save(*job)
}

func (s *Service) createDatabaseBackup(ctx context.Context, job *Job, applyRetention bool) (string, error) {
	if err := s.CleanupPartials(); err != nil {
		return "", errors.New("failed to clean incomplete backup files")
	}
	job.Phase = "reading database metadata"
	_ = s.jobs.Save(*job)
	info, err := s.database.Info(ctx)
	if err != nil {
		return "", errors.New("failed to read database metadata")
	}

	created := s.now()
	version := safeFilenamePart(s.cfg.FastSellVersion)
	dir := filepath.Join(s.cfg.Root, "database")
	filename := uniqueBackupFilename(dir, fmt.Sprintf("fastsell-db-backup-%s-%s-pg%d", created.Format("20060102T150405"), version, info.PostgreSQLMajor))
	dumpFinal := filepath.Join(dir, filename)
	dumpPartial := dumpFinal + ".partial"
	checksumFinal := dumpFinal + ".sha256"
	checksumPartial := checksumFinal + ".partial"
	metadataFinal := dumpFinal + ".json"
	metadataPartial := metadataFinal + ".partial"
	cleanup := func() {
		_ = os.Remove(dumpPartial)
		_ = os.Remove(checksumPartial)
		_ = os.Remove(metadataPartial)
	}
	defer cleanup()

	job.Phase = "creating database dump"
	_ = s.jobs.Save(*job)
	args := []string{"--file", dumpPartial, "--format=custom", "--no-owner", "--no-acl"}
	if err := s.runner.Run(ctx, "pg_dump", args, s.pgEnv); err != nil {
		return "", errors.New("database dump command failed")
	}
	if err := os.Chmod(dumpPartial, 0600); err != nil {
		return "", errors.New("failed to secure database dump permissions")
	}
	if err := syncExistingFile(dumpPartial); err != nil {
		return "", errors.New("failed to flush database dump")
	}

	job.Phase = "validating database dump"
	_ = s.jobs.Save(*job)
	if err := s.runner.Run(ctx, "pg_restore", []string{"--list", dumpPartial}, s.pgEnv); err != nil {
		return "", errors.New("database dump validation failed")
	}

	checksum, size, err := fileSHA256(dumpPartial)
	if err != nil {
		return "", errors.New("failed to checksum database dump")
	}
	checksumData := []byte(fmt.Sprintf("%s  %s\n", checksum, filename))
	if err := writeSyncedFile(checksumPartial, checksumData, 0600); err != nil {
		return "", errors.New("failed to write database checksum")
	}
	metadata := Metadata{
		BackupFormatVersion: FormatVersion, CreatedAt: created, FastSellVersion: s.cfg.FastSellVersion,
		DatabaseType: "postgresql", DatabaseName: info.Name, PostgreSQLMajor: info.PostgreSQLMajor,
		SchemaVersion: info.SchemaVersion, DumpFormat: "custom", DumpByteSize: size, Source: job.Source,
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return "", errors.New("failed to encode database backup metadata")
	}
	if err := writeSyncedFile(metadataPartial, append(metadataBytes, '\n'), 0600); err != nil {
		return "", errors.New("failed to write database backup metadata")
	}

	job.Phase = "publishing database backup"
	_ = s.jobs.Save(*job)
	// Publish the dump last. Inventory only recognizes sets whose final dump exists.
	if err := os.Rename(checksumPartial, checksumFinal); err != nil {
		return "", errors.New("failed to publish database checksum")
	}
	if err := os.Rename(metadataPartial, metadataFinal); err != nil {
		_ = os.Remove(checksumFinal)
		return "", errors.New("failed to publish database metadata")
	}
	if err := os.Rename(dumpPartial, dumpFinal); err != nil {
		_ = os.Remove(checksumFinal)
		_ = os.Remove(metadataFinal)
		return "", errors.New("failed to publish database dump")
	}
	if err := syncDirectory(dir); err != nil {
		_ = os.Remove(dumpFinal)
		_ = os.Remove(checksumFinal)
		_ = os.Remove(metadataFinal)
		return "", errors.New("failed to flush database backup directory")
	}
	job.BackupID = filename
	_ = s.jobs.Save(*job)

	if applyRetention {
		job.Phase = "applying retention"
		_ = s.jobs.Save(*job)
		settings, settingsErr := s.settings.Get(ctx)
		if settingsErr != nil {
			log.Printf("backup retention settings warning: %s", sanitizeError(settingsErr.Error()))
		} else if cleanupErr := s.ApplyRetention(settings.RetentionCount, map[string]bool{filename: true}); cleanupErr != nil {
			log.Printf("backup retention cleanup warning: %s", sanitizeError(cleanupErr.Error()))
		}
	}
	return filename, nil
}

func (s *Service) ListBackups() ([]Backup, error) {
	dir := filepath.Join(s.cfg.Root, "database")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	sources := s.jobs.Sources()
	backups := make([]Backup, 0)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, "fastsell-db-backup-") || !strings.HasSuffix(name, ".dump") || !safeIDPattern.MatchString(name) {
			continue
		}
		backup, loadErr := s.loadBackup(name, sources[name])
		if loadErr != nil {
			continue
		}
		backups = append(backups, backup)
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].CreatedAt.After(backups[j].CreatedAt) })
	return backups, nil
}

func (s *Service) loadBackup(id, source string) (Backup, error) {
	if !safeIDPattern.MatchString(id) || filepath.Base(id) != id || !strings.HasSuffix(id, ".dump") {
		return Backup{}, os.ErrNotExist
	}
	path := filepath.Join(s.cfg.Root, "database", id)
	if err := requireRegularFile(path); err != nil {
		return Backup{}, err
	}
	if err := requireRegularFile(path + ".sha256"); err != nil {
		return Backup{}, err
	}
	if err := requireRegularFile(path + ".json"); err != nil {
		return Backup{}, err
	}
	metadataBytes, err := os.ReadFile(path + ".json")
	if err != nil {
		return Backup{}, err
	}
	var metadata Metadata
	if err := json.Unmarshal(metadataBytes, &metadata); err != nil {
		return Backup{}, err
	}
	stat, err := os.Stat(path)
	if err != nil {
		return Backup{}, err
	}
	status := "checksum_valid"
	if err := verifyChecksum(path); err != nil {
		status = "invalid"
	}
	if metadata.Source == "manual" || metadata.Source == "scheduled" || metadata.Source == "pre_restore" {
		source = metadata.Source
	} else if source != "scheduled" && source != "pre_restore" {
		source = "manual"
	}
	return Backup{
		ID: id, Filename: id, CreatedAt: metadata.CreatedAt, FastSellVersion: metadata.FastSellVersion,
		PostgreSQLMajor: metadata.PostgreSQLMajor, SchemaVersion: metadata.SchemaVersion,
		Size: stat.Size(), ValidationStatus: status, Source: source,
	}, nil
}

func (s *Service) ValidateBackup(ctx context.Context, id string) (Backup, error) {
	release, err := s.lock.TryAcquire("database backup validation")
	if err != nil {
		return Backup{}, err
	}
	defer release()
	backup, _, _, err := s.preRestoreValidation(ctx, id)
	if err != nil {
		return Backup{}, err
	}
	backup.ValidationStatus = "valid"
	return backup, nil
}

func (s *Service) DeleteBackup(id string) error {
	release, err := s.lock.TryAcquire("database backup deletion")
	if err != nil {
		return err
	}
	defer release()
	if _, _, _, err := s.readBackupSet(id); err != nil {
		return err
	}
	base := filepath.Join(s.cfg.Root, "database", id)
	for _, path := range []string{base, base + ".sha256", base + ".json"} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("failed to delete complete backup set")
		}
	}
	if err := syncDirectory(filepath.Dir(base)); err != nil {
		return errors.New("backup set was removed but the backup directory could not be flushed")
	}
	return nil
}

func (s *Service) ApplyRetention(keep int, protected map[string]bool) error {
	if keep < 1 {
		return errors.New("retention count must be positive")
	}
	backups, err := s.ListBackups()
	if err != nil {
		return err
	}
	kept := 0
	var cleanupErrors []string
	for _, backup := range backups {
		if kept < keep || protected[backup.ID] {
			kept++
			continue
		}
		base := filepath.Join(s.cfg.Root, "database", backup.ID)
		failed := false
		for _, path := range []string{base, base + ".sha256", base + ".json"} {
			if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
				failed = true
			}
		}
		if failed {
			cleanupErrors = append(cleanupErrors, backup.ID)
		}
	}
	if len(cleanupErrors) > 0 {
		return errors.New("one or more expired backup sets could not be removed")
	}
	return nil
}

func (s *Service) CleanupPartials() error {
	var failures int
	for _, subdir := range []string{"database", "media", "jobs", "restore-staging"} {
		dir := filepath.Join(s.cfg.Root, subdir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			failures++
			continue
		}
		for _, entry := range entries {
			if entry.Type()&os.ModeSymlink != 0 || entry.IsDir() || !strings.HasSuffix(entry.Name(), ".partial") {
				continue
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				failures++
			}
		}
	}
	if failures > 0 {
		return errors.New("some partial files could not be removed")
	}
	return nil
}

func (s *Service) readBackupSet(id string) (Backup, Metadata, string, error) {
	backup, err := s.loadBackup(id, s.jobs.Sources()[id])
	if err != nil {
		return Backup{}, Metadata{}, "", os.ErrNotExist
	}
	path := filepath.Join(s.cfg.Root, "database", id)
	data, err := os.ReadFile(path + ".json")
	if err != nil {
		return Backup{}, Metadata{}, "", err
	}
	var metadata Metadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return Backup{}, Metadata{}, "", errors.New("backup metadata is malformed")
	}
	return backup, metadata, path, nil
}

func (s *Service) preRestoreValidation(ctx context.Context, id string) (Backup, Metadata, string, error) {
	backup, metadata, path, err := s.readBackupSet(id)
	if err != nil {
		return Backup{}, Metadata{}, "", err
	}
	if err := verifyChecksum(path); err != nil {
		return Backup{}, Metadata{}, "", errors.New("backup checksum verification failed")
	}
	if metadata.BackupFormatVersion != FormatVersion {
		return Backup{}, Metadata{}, "", errors.New("backup format version is unsupported")
	}
	if metadata.DatabaseType != "postgresql" || metadata.DumpFormat != "custom" || metadata.DumpByteSize != backup.Size {
		return Backup{}, Metadata{}, "", errors.New("backup metadata is inconsistent")
	}
	current, err := s.database.Info(ctx)
	if err != nil {
		return Backup{}, Metadata{}, "", errors.New("failed to read current database compatibility")
	}
	if metadata.PostgreSQLMajor != current.PostgreSQLMajor {
		return Backup{}, Metadata{}, "", errors.New("backup PostgreSQL major version does not match the running server")
	}
	if metadata.SchemaVersion > current.SchemaVersion {
		return Backup{}, Metadata{}, "", errors.New("backup schema is newer than this FastSell installation")
	}
	if err := s.runner.Run(ctx, "pg_restore", []string{"--list", path}, s.pgEnv); err != nil {
		return Backup{}, Metadata{}, "", errors.New("database dump validation failed")
	}
	return backup, metadata, path, nil
}

func newJob(kind, source string, now time.Time) Job {
	return Job{ID: fmt.Sprintf("%s-%s", now.Format("20060102T150405.000000000"), randomHex(6)), Kind: kind, State: "queued", Phase: "queued", Source: source, CreatedAt: now}
}

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	if _, err := rand.Read(value); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(value)
}

func safeFilenamePart(value string) string {
	value = strings.TrimSpace(value)
	var out strings.Builder
	for _, char := range value {
		if char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' {
			out.WriteRune(char)
		} else {
			out.WriteByte('-')
		}
	}
	result := strings.Trim(out.String(), "-.")
	if result == "" {
		return "unknown"
	}
	if len(result) > 64 {
		return result[:64]
	}
	return result
}

func uniqueBackupFilename(dir, stem string) string {
	for suffix := 0; suffix < 10000; suffix++ {
		filename := stem + ".dump"
		if suffix > 0 {
			filename = fmt.Sprintf("%s-%d.dump", stem, suffix)
		}
		if _, err := os.Lstat(filepath.Join(dir, filename)); errors.Is(err, os.ErrNotExist) {
			return filename
		}
	}
	return fmt.Sprintf("%s-%s.dump", stem, randomHex(4))
}

func postgresEnvironment(databaseURL string) ([]string, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.User == nil {
		return nil, errors.New("DATABASE_URL is invalid for backup operations")
	}
	password, _ := parsed.User.Password()
	databaseName, err := url.PathUnescape(strings.TrimPrefix(parsed.Path, "/"))
	if err != nil || databaseName == "" {
		return nil, errors.New("DATABASE_URL database name is invalid")
	}
	port := parsed.Port()
	if port == "" {
		port = "5432"
	}
	values := map[string]string{
		"PGHOST": parsed.Hostname(), "PGPORT": port, "PGUSER": parsed.User.Username(),
		"PGPASSWORD": password, "PGDATABASE": databaseName, "PGSSLMODE": parsed.Query().Get("sslmode"),
	}
	if values["PGSSLMODE"] == "" {
		values["PGSSLMODE"] = "prefer"
	}
	env := make([]string, 0, len(os.Environ())+len(values))
	for _, existing := range os.Environ() {
		key := strings.SplitN(existing, "=", 2)[0]
		_, replaced := values[key]
		if !replaced && key != "DATABASE_URL" && key != "PGPASSWORD" {
			env = append(env, existing)
		}
	}
	for key, value := range values {
		env = append(env, key+"="+value)
	}
	return env, nil
}

func fileSHA256(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), size, nil
}

func verifyChecksum(path string) error {
	data, err := os.ReadFile(path + ".sha256")
	if err != nil {
		return err
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 || len(fields[0]) != sha256.Size*2 || fields[1] != filepath.Base(path) {
		return errors.New("checksum sidecar is malformed")
	}
	actual, _, err := fileSHA256(path)
	if err != nil || !strings.EqualFold(fields[0], actual) {
		return errors.New("checksum mismatch")
	}
	return nil
}

func requireRegularFile(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("backup artifact is not a regular file")
	}
	return nil
}

func writeSyncedFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err = file.Write(data); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(path)
		return err
	}
	return closeErr
}

func syncExistingFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	err = dir.Sync()
	closeErr := dir.Close()
	if err != nil {
		return err
	}
	return closeErr
}

func sanitizeError(message string) string {
	lower := strings.ToLower(message)
	for _, marker := range []string{"password=", "token=", "secret=", "api_key=", "pgpassword", "postgres://", "postgresql://"} {
		if strings.Contains(lower, marker) {
			return "operation failed; review sanitized server logs"
		}
	}
	message = strings.TrimSpace(message)
	if len(message) > 240 {
		message = message[:240]
	}
	if message == "" {
		return "operation failed"
	}
	return message
}
