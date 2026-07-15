package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPostgresLogicalRestoreIntegration is opt-in because it destructively swaps
// the database named by FASTSELL_BACKUP_INTEGRATION_DATABASE_URL. It must only be
// pointed at a dedicated PostgreSQL 16 test database. The test runs the same
// golang-migrate path and staged database-swap restore used in production.
func TestPostgresLogicalRestoreIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_BACKUP_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_BACKUP_INTEGRATION_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	databaseName := pool.Config().ConnConfig.Database
	if !internalDatabaseNamePattern.MatchString(databaseName) {
		t.Fatalf("integration database name %q is not safe for destructive testing", databaseName)
	}
	migrationRoot := os.Getenv("FASTSELL_BACKUP_INTEGRATION_MIGRATION_ROOT")
	if migrationRoot == "" {
		migrationRoot = filepath.Clean("../../../db/migrations")
	}
	database := NewPostgresDatabase(pool, migrationRoot)
	if err := database.MigrateDatabase(ctx, databaseName); err != nil {
		t.Fatalf("production migration path failed: %v", err)
	}
	var markerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO locations(name, description)
		VALUES ('backup-integration-marker', 'before-backup')
		RETURNING id::text
	`).Scan(&markerID); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM locations WHERE id::text = $1`, markerID)
	})

	root := t.TempDir()
	service, err := NewService(Config{
		Root: filepath.Join(root, "backups"), DataRoot: filepath.Join(root, "data"),
		FastSellVersion: "integration", DatabaseURL: databaseURL,
	}, database, NewPostgresSettingsStore(pool), ExecRunner{}, NewMaintenanceGate())
	if err != nil {
		t.Fatal(err)
	}
	backupJob, err := service.StartBackup("manual")
	if err != nil {
		t.Fatal(err)
	}
	backupJob = waitJobWithin(t, service, backupJob.ID, 2*time.Minute)
	if backupJob.State != "succeeded" {
		t.Fatalf("logical backup failed: %#v", backupJob)
	}
	if _, err := pool.Exec(ctx, `UPDATE locations SET description = 'after-backup' WHERE id::text = $1`, markerID); err != nil {
		t.Fatal(err)
	}
	restoreJob, err := service.StartRestore(backupJob.BackupID, RestoreConfirmation)
	if err != nil {
		t.Fatal(err)
	}
	restoreJob = waitJobWithin(t, service, restoreJob.ID, 2*time.Minute)
	if restoreJob.State != "succeeded" {
		t.Fatalf("staged logical restore failed: %#v", restoreJob)
	}
	var description string
	if err := pool.QueryRow(ctx, `SELECT description FROM locations WHERE id::text = $1`, markerID).Scan(&description); err != nil {
		t.Fatal(err)
	}
	if description != "before-backup" {
		t.Fatalf("restore did not activate the selected logical backup: %q", description)
	}
	info, err := database.Info(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := database.VerifyDatabase(ctx, databaseName, info.SchemaVersion); err != nil {
		t.Fatalf("post-restore production health validation failed: %v", err)
	}
}
