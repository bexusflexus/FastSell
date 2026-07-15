package backup

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type SettingsStore interface {
	Get(context.Context) (Settings, error)
	Update(context.Context, Settings) (Settings, error)
	RecordAttempt(context.Context, time.Time) error
	RecordSuccess(context.Context, time.Time) error
	RecordFailure(context.Context, time.Time, string) error
}

type PostgresSettingsStore struct{ pool *pgxpool.Pool }

func NewPostgresSettingsStore(pool *pgxpool.Pool) *PostgresSettingsStore {
	return &PostgresSettingsStore{pool: pool}
}

func (s *PostgresSettingsStore) Get(ctx context.Context) (Settings, error) {
	settings := DefaultSettings()
	err := s.pool.QueryRow(ctx, `
		SELECT automatic_enabled, schedule_preset, cron_expression, timezone,
			retention_count, last_attempt_datetime, last_success_datetime,
			last_failure_datetime, last_failure_message
		FROM backup_settings WHERE singleton = true
	`).Scan(
		&settings.AutomaticEnabled, &settings.SchedulePreset, &settings.CronExpression,
		&settings.Timezone, &settings.RetentionCount, &settings.LastAttempt,
		&settings.LastSuccess, &settings.LastFailure, &settings.LastFailureMessage,
	)
	settings.HostLocation = HostDatabasePath
	return settings, err
}

func (s *PostgresSettingsStore) Update(ctx context.Context, settings Settings) (Settings, error) {
	err := s.pool.QueryRow(ctx, `
		UPDATE backup_settings SET automatic_enabled=$1, schedule_preset=$2,
			cron_expression=$3, timezone=$4, retention_count=$5, updated_datetime=now()
		WHERE singleton=true
		RETURNING last_attempt_datetime, last_success_datetime,
			last_failure_datetime, last_failure_message
	`, settings.AutomaticEnabled, settings.SchedulePreset, settings.CronExpression,
		settings.Timezone, settings.RetentionCount).Scan(
		&settings.LastAttempt, &settings.LastSuccess, &settings.LastFailure,
		&settings.LastFailureMessage,
	)
	settings.HostLocation = HostDatabasePath
	return settings, err
}

func (s *PostgresSettingsStore) RecordAttempt(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE backup_settings SET last_attempt_datetime=$1, updated_datetime=now() WHERE singleton=true`, at)
	return err
}

func (s *PostgresSettingsStore) RecordSuccess(ctx context.Context, at time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE backup_settings SET last_success_datetime=$1, last_failure_message=NULL, updated_datetime=now() WHERE singleton=true`, at)
	return err
}

func (s *PostgresSettingsStore) RecordFailure(ctx context.Context, at time.Time, message string) error {
	_, err := s.pool.Exec(ctx, `UPDATE backup_settings SET last_failure_datetime=$1, last_failure_message=$2, updated_datetime=now() WHERE singleton=true`, at, sanitizeError(message))
	return err
}

func ValidateSettings(settings Settings) (Settings, error) {
	settings.SchedulePreset = strings.ToLower(strings.TrimSpace(settings.SchedulePreset))
	settings.Timezone = strings.TrimSpace(settings.Timezone)
	settings.CronExpression = strings.TrimSpace(settings.CronExpression)
	if settings.RetentionCount < 1 || settings.RetentionCount > 365 {
		return Settings{}, errors.New("retention_count must be between 1 and 365")
	}
	if settings.Timezone == "" {
		settings.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		return Settings{}, errors.New("timezone must be a valid IANA timezone")
	}
	switch settings.SchedulePreset {
	case "daily":
		settings.CronExpression = "0 2 * * *"
	case "weekly":
		settings.CronExpression = "0 2 * * 0"
	case "advanced":
		if settings.CronExpression == "" {
			return Settings{}, errors.New("cron_expression is required for the advanced schedule")
		}
	default:
		return Settings{}, errors.New("schedule_preset must be daily, weekly, or advanced")
	}
	if err := ValidateCron(settings.CronExpression, settings.Timezone); err != nil {
		return Settings{}, err
	}
	settings.HostLocation = HostDatabasePath
	return settings, nil
}
