package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	backupsvc "fastsell-api/internal/backup"
	"fastsell-api/internal/config"
	"fastsell-api/internal/db"
)

func main() {
	if len(os.Args) < 2 || os.Args[1] != "restore" {
		fmt.Fprintln(os.Stderr, "usage: fastsell-backup restore --backup-id <inventory filename> --confirm 'RESTORE FASTSELL'")
		os.Exit(2)
	}
	flags := flag.NewFlagSet("restore", flag.ExitOnError)
	backupID := flags.String("backup-id", "", "backup inventory filename under the fixed FastSell backup root")
	confirmation := flags.String("confirm", "", "required destructive confirmation")
	_ = flags.Parse(os.Args[2:])
	if *backupID == "" || *confirmation != backupsvc.RestoreConfirmation {
		fmt.Fprintln(os.Stderr, "backup ID and exact confirmation RESTORE FASTSELL are required")
		os.Exit(2)
	}

	cfg := config.Load()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	defer cancel()
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "database connection setup failed")
		os.Exit(1)
	}
	defer pool.Close()
	service, err := backupsvc.NewService(backupsvc.Config{
		Root: cfg.BackupRoot, DataRoot: cfg.DataRoot, FastSellVersion: cfg.FastSellVersion,
		DatabaseURL: cfg.DatabaseURL,
	}, backupsvc.NewPostgresDatabase(pool, cfg.MigrationRoot), backupsvc.NewPostgresSettingsStore(pool), backupsvc.ExecRunner{}, backupsvc.NewMaintenanceGate())
	if err != nil {
		fmt.Fprintln(os.Stderr, "backup recovery service setup failed")
		os.Exit(1)
	}
	job, err := service.RunRestore(ctx, *backupID, *confirmation)
	if err != nil {
		if errors.Is(err, backupsvc.ErrOperationConflict) {
			fmt.Fprintln(os.Stderr, "restore not started: another backup, restore, validation, deletion, or media operation is running")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "restore failed in phase %s: %s\n", job.Phase, job.ErrorMessage)
		if job.RecoveryMessage != "" {
			fmt.Fprintln(os.Stderr, job.RecoveryMessage)
		}
		os.Exit(1)
	}
	fmt.Printf("restore job %s completed successfully\n", job.ID)
}
