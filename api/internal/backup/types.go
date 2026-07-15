package backup

import "time"

const (
	FormatVersion       = 1
	RestoreConfirmation = "RESTORE FASTSELL"
	HostDatabasePath    = "/srv/fastsell/backups/database"
)

type Settings struct {
	AutomaticEnabled   bool       `json:"automatic_enabled"`
	SchedulePreset     string     `json:"schedule_preset"`
	CronExpression     string     `json:"cron_expression"`
	Timezone           string     `json:"timezone"`
	RetentionCount     int        `json:"retention_count"`
	HostLocation       string     `json:"host_location"`
	LastAttempt        *time.Time `json:"last_attempt"`
	LastSuccess        *time.Time `json:"last_success"`
	LastFailure        *time.Time `json:"last_failure"`
	LastFailureMessage *string    `json:"last_failure_message"`
}

func DefaultSettings() Settings {
	return Settings{
		AutomaticEnabled: true,
		SchedulePreset:   "daily",
		CronExpression:   "0 2 * * *",
		Timezone:         "UTC",
		RetentionCount:   14,
		HostLocation:     HostDatabasePath,
	}
}

type Metadata struct {
	BackupFormatVersion int       `json:"backup_format_version"`
	CreatedAt           time.Time `json:"creation_timestamp"`
	FastSellVersion     string    `json:"fastsell_version"`
	DatabaseType        string    `json:"database_type"`
	DatabaseName        string    `json:"database_name"`
	PostgreSQLMajor     int       `json:"postgresql_major_version"`
	SchemaVersion       int64     `json:"schema_migration_version"`
	DumpFormat          string    `json:"dump_format"`
	DumpByteSize        int64     `json:"dump_byte_size"`
	Source              string    `json:"source"`
}

type Backup struct {
	ID               string    `json:"backup_id"`
	Filename         string    `json:"filename"`
	CreatedAt        time.Time `json:"created_time"`
	FastSellVersion  string    `json:"fastsell_version"`
	PostgreSQLMajor  int       `json:"postgresql_version"`
	SchemaVersion    int64     `json:"schema_version"`
	Size             int64     `json:"size"`
	ValidationStatus string    `json:"validation_status"`
	Source           string    `json:"source"`
}

type Job struct {
	ID              string     `json:"job_id"`
	Kind            string     `json:"kind"`
	State           string     `json:"state"`
	Phase           string     `json:"phase"`
	Source          string     `json:"source,omitempty"`
	BackupID        string     `json:"backup_id,omitempty"`
	PreRestoreID    string     `json:"pre_restore_backup_id,omitempty"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	RecoveryMessage string     `json:"recovery_message,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type DatabaseInfo struct {
	Name            string
	PostgreSQLMajor int
	SchemaVersion   int64
	MigrationDirty  bool
}

type OperationConflict struct {
	Operation string `json:"operation"`
}

type RestoreRequest struct {
	Confirmation string `json:"confirmation"`
}
