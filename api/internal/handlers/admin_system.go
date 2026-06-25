package handlers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"fastsell-api/internal/config"
	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminSystemStore struct {
	pool      *pgxpool.Pool
	cfg       config.Config
	startTime time.Time
	agent     *systemAgentClient
}

type AdminSystemHandler struct {
	store *AdminSystemStore
}

func NewAdminSystemStore(pool *pgxpool.Pool, cfg config.Config, startTime time.Time) *AdminSystemStore {
	return &AdminSystemStore{
		pool:      pool,
		cfg:       cfg,
		startTime: startTime.UTC(),
		agent:     newSystemAgentClient(cfg.SystemAgentURL),
	}
}

func NewAdminSystemHandler(store *AdminSystemStore) *AdminSystemHandler {
	return &AdminSystemHandler{store: store}
}

func (h *AdminSystemHandler) GetHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	health, err := h.store.Get(ctx)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load system health")
		return
	}

	respond.JSON(w, http.StatusOK, health)
}

func (s *AdminSystemStore) Get(ctx context.Context) (models.AdminSystemHealthResponse, error) {
	apiHealth := s.buildAPIHealth()
	storageHealth := s.buildStorageHealth()
	pathsHealth := s.buildPathsHealth()
	databaseHealth, databaseErr := s.buildDatabaseHealth(ctx)
	intakeHealth, intakeErr := s.buildIntakeHealth(ctx)
	aiHealth, aiErr := s.buildAIHealth(ctx)
	dockerHealth := s.buildDockerHealth(ctx)

	alerts := make([]models.SystemHealthAlert, 0)
	alerts = append(alerts, buildAPIAlerts(apiHealth)...)
	alerts = append(alerts, buildDatabaseAlerts(databaseHealth, databaseErr)...)
	alerts = append(alerts, buildStorageAlerts(storageHealth)...)
	alerts = append(alerts, buildPathAlerts(pathsHealth)...)
	alerts = append(alerts, buildIntakeAlerts(intakeHealth, intakeErr)...)
	alerts = append(alerts, buildAIAlerts(aiHealth, aiErr)...)
	alerts = append(alerts, dockerHealth.Alerts...)

	frontendHealth := models.SystemSimpleHealth{
		Status:      models.SystemStatusUnknown,
		HostingMode: "dev_or_not_configured",
		Message:     "Frontend hosting is not configured for the container deployment.",
	}
	if s.cfg.FrontendHostingMode == "nginx" && s.cfg.FrontendPublicURL != "" {
		frontendHealth = models.SystemSimpleHealth{
			Status:      models.SystemStatusOK,
			HostingMode: "nginx",
			PublicURL:   s.cfg.FrontendPublicURL,
			Message:     "Frontend is expected to be served by fastsell-web nginx.",
		}
	}
	overall := deriveOverallStatus(apiHealth.Status, databaseHealth.Status, storageHealth.Status, pathsHealth.Status)

	return models.AdminSystemHealthResponse{
		OverallStatus:     overall,
		GeneratedDatetime: time.Now().UTC(),
		API:               apiHealth,
		Database:          databaseHealth,
		Storage:           storageHealth,
		Paths:             pathsHealth,
		Intake:            intakeHealth,
		AI:                aiHealth,
		Frontend:          frontendHealth,
		Docker:            dockerHealth,
		Alerts:            alerts,
	}, nil
}

func (s *AdminSystemStore) buildAPIHealth() models.SystemAPIHealth {
	status := models.SystemStatusOK
	if s.cfg.DataRoot == "" || s.cfg.ImageRoot == "" || s.cfg.IntakeDir == "" {
		status = models.SystemStatusWarning
	}

	return models.SystemAPIHealth{
		Status:        status,
		UptimeSeconds: int64(time.Since(s.startTime).Seconds()),
		ServerTime:    time.Now().UTC(),
		DataRoot:      filepath.Clean(s.cfg.DataRoot),
		ImageRoot:     filepath.Clean(s.cfg.ImageRoot),
		IntakeDir:     filepath.Clean(s.cfg.IntakeDir),
	}
}

func (s *AdminSystemStore) buildDatabaseHealth(ctx context.Context) (models.SystemDatabaseHealth, error) {
	health := models.SystemDatabaseHealth{
		Status:    models.SystemStatusUnknown,
		Reachable: false,
	}

	if err := s.pool.QueryRow(ctx, `SELECT 1`).Scan(new(int)); err != nil {
		health.Status = models.SystemStatusFailed
		return health, err
	}
	health.Reachable = true
	health.Status = models.SystemStatusOK

	migrationVersion, migrationDirty, migrationErr := loadMigrationState(ctx, s.pool)
	if migrationErr == nil {
		health.MigrationVersion = migrationVersion
		health.MigrationDirty = migrationDirty
		if migrationDirty != nil && *migrationDirty {
			health.Status = models.SystemStatusFailed
		}
	} else {
		health.Status = models.SystemStatusWarning
	}

	var databaseSize int64
	if err := s.pool.QueryRow(ctx, `SELECT pg_database_size(current_database())::bigint`).Scan(&databaseSize); err != nil {
		health.DatabaseSizeBytes = nil
	} else {
		health.DatabaseSizeBytes = &databaseSize
	}

	if err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*)::int FROM containers),
			(SELECT count(*)::int FROM items),
			(SELECT count(*)::int FROM image_assets),
			(SELECT count(*)::int FROM upload_sessions),
			(SELECT count(*)::int FROM upload_groups)
	`).Scan(
		&health.ContainerCount,
		&health.ItemCount,
		&health.ImageAssetCount,
		&health.UploadSessionCount,
		&health.UploadGroupCount,
	); err != nil {
		health.Status = models.SystemStatusFailed
		return health, err
	}

	return health, nil
}

func (s *AdminSystemStore) buildStorageHealth() models.SystemStorageHealth {
	health := models.SystemStorageHealth{
		Status: models.SystemStatusUnknown,
		Path:   filepath.Clean(s.cfg.DataRoot),
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(s.cfg.DataRoot, &stat); err != nil {
		health.Status = models.SystemStatusFailed
		return health
	}

	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	used := total - free
	usedPercent := 0.0
	if total > 0 {
		usedPercent = (float64(used) / float64(total)) * 100
	}

	health.TotalBytes = total
	health.FreeBytes = free
	health.UsedBytes = used
	health.UsedPercent = usedPercent
	switch {
	case usedPercent >= 90:
		health.Status = models.SystemStatusFailed
	case usedPercent >= 75:
		health.Status = models.SystemStatusWarning
	default:
		health.Status = models.SystemStatusOK
	}

	return health
}

func (s *AdminSystemStore) buildPathsHealth() models.SystemPathsHealth {
	requiredPaths := []string{
		s.cfg.DataRoot,
		s.cfg.IntakeDir,
		s.cfg.IntakeProcessingDir,
		s.cfg.IntakeFailedDir,
		s.cfg.ImageOriginalsDir,
		s.cfg.ImageThumbnailsDir,
		s.cfg.ImageNormalizedDir,
	}

	results := make([]models.SystemPathHealth, 0, len(requiredPaths))
	overall := models.SystemStatusOK
	for _, path := range requiredPaths {
		entry := inspectRequiredPath(path)
		if entry.Status == models.SystemStatusFailed {
			overall = models.SystemStatusFailed
		}
		results = append(results, entry)
	}

	return models.SystemPathsHealth{
		Status: overall,
		Paths:  results,
	}
}

func (s *AdminSystemStore) buildIntakeHealth(ctx context.Context) (models.SystemIntakeHealth, error) {
	health := models.SystemIntakeHealth{
		Status: models.SystemStatusOK,
	}

	err := s.pool.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE status IN ('pending', 'uploaded'))::int,
			count(*) FILTER (WHERE status = 'processing')::int,
			count(*) FILTER (WHERE status = 'processed')::int,
			count(*) FILTER (WHERE status = 'failed')::int,
			count(*) FILTER (
				WHERE status = 'processing'
					AND COALESCE(updated_datetime, created_datetime) < now() - interval '10 minutes'
			)::int,
			min(created_datetime) FILTER (WHERE status IN ('pending', 'uploaded')),
			max(COALESCE(updated_datetime, created_datetime)) FILTER (WHERE status = 'processed')
		FROM image_assets
	`).Scan(
		&health.PendingOrUploadedCount,
		&health.ProcessingImageCount,
		&health.ProcessedImageCount,
		&health.FailedImageCount,
		&health.StuckProcessingImageCount,
		&health.OldestPendingDatetime,
		&health.LatestProcessedDatetime,
	)
	if err != nil {
		health.Status = models.SystemStatusUnknown
		return health, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT status, count(*)::int
		FROM upload_sessions
		GROUP BY status
	`)
	if err == nil {
		defer rows.Close()
		counts := make(map[string]int)
		for rows.Next() {
			var status string
			var count int
			if scanErr := rows.Scan(&status, &count); scanErr != nil {
				err = scanErr
				break
			}
			counts[status] = count
		}
		if rows.Err() == nil && err == nil {
			health.UploadSessionStatusCounts = counts
		}
	}

	if health.FailedImageCount > 0 || health.StuckProcessingImageCount > 0 {
		health.Status = models.SystemStatusWarning
	}

	return health, nil
}

func (s *AdminSystemStore) buildAIHealth(ctx context.Context) (models.SystemAIHealth, error) {
	health := models.SystemAIHealth{
		Status: models.SystemStatusUnknown,
	}

	providerExists, err := tableExists(ctx, s.pool, "ai_provider_configs")
	if err != nil {
		return health, err
	}
	if !providerExists {
		health.Status = models.SystemStatusUnknown
		return health, nil
	}

	err = s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			name,
			provider_type,
			model_name,
			vision_enabled,
			last_test_status,
			last_test_datetime,
			last_error_message
		FROM ai_provider_configs
		WHERE active = true
		ORDER BY updated_datetime DESC NULLS LAST, created_datetime DESC
		LIMIT 1
	`).Scan(
		&health.ActiveProviderID,
		&health.ActiveProviderName,
		&health.ActiveProviderType,
		&health.ActiveModelName,
		&health.VisionEnabled,
		&health.LastTestStatus,
		&health.LastTestDatetime,
		&health.LastErrorMessage,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		health.Status = models.SystemStatusWarning
		health.AIAssistEnabled = false
		return health, nil
	}
	if err != nil {
		health.Status = models.SystemStatusUnknown
		return health, err
	}

	health.AIAssistEnabled = true
	switch {
	case health.LastTestStatus == nil:
		health.Status = models.SystemStatusUnknown
	case *health.LastTestStatus == "success":
		health.Status = models.SystemStatusOK
	case *health.LastTestStatus == "failed":
		health.Status = models.SystemStatusWarning
	default:
		health.Status = models.SystemStatusUnknown
	}

	return health, nil
}

func (s *AdminSystemStore) buildDockerHealth(ctx context.Context) models.SystemDockerHealth {
	if s.agent == nil {
		return models.SystemDockerHealth{
			Status:  models.SystemStatusUnknown,
			Message: "Docker system agent is not configured.",
		}
	}

	health, err := s.agent.GetDockerHealth(ctx)
	if err != nil {
		return models.SystemDockerHealth{
			Status:  models.SystemStatusWarning,
			Message: "Docker system agent is not reachable.",
			Alerts: []models.SystemHealthAlert{{
				Severity: models.SystemStatusWarning,
				Area:     "docker",
				Message:  "Docker system agent is not reachable.",
			}},
		}
	}

	if health.Message == "" {
		health.Message = "Docker service health was returned by fastsell-system-agent."
	}
	return health
}

func loadMigrationState(ctx context.Context, pool *pgxpool.Pool) (*int64, *bool, error) {
	exists, err := tableExists(ctx, pool, "schema_migrations")
	if err != nil {
		return nil, nil, err
	}
	if !exists {
		return nil, nil, errors.New("schema_migrations table not found")
	}

	var version int64
	var dirty bool
	if err := pool.QueryRow(ctx, `SELECT version, dirty FROM schema_migrations LIMIT 1`).Scan(&version, &dirty); err != nil {
		return nil, nil, err
	}

	return &version, &dirty, nil
}

func tableExists(ctx context.Context, pool *pgxpool.Pool, tableName string) (bool, error) {
	var exists bool
	err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
				AND table_name = $1
		)
	`, tableName).Scan(&exists)
	return exists, err
}

func inspectRequiredPath(path string) models.SystemPathHealth {
	entry := models.SystemPathHealth{
		Path:   filepath.Clean(path),
		Status: models.SystemStatusFailed,
	}

	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			entry.Message = "Path is missing."
			return entry
		}
		entry.Message = "Path could not be inspected."
		return entry
	}

	entry.Exists = true
	entry.IsDirectory = info.IsDir()
	if !entry.IsDirectory {
		entry.Message = "Path is not a directory."
		return entry
	}

	if readErr := verifyReadableDirectory(path); readErr == nil {
		entry.Readable = true
	}
	if writeErr := verifyWritableDirectoryLocal(path); writeErr == nil {
		entry.Writable = true
	}

	switch {
	case !entry.Readable:
		entry.Message = "Directory is not readable."
	case !entry.Writable:
		entry.Message = "Directory is not writable."
	default:
		entry.Status = models.SystemStatusOK
		entry.Message = "Directory is available."
	}

	return entry
}

func verifyReadableDirectory(dir string) error {
	handle, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer handle.Close()

	_, err = handle.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func verifyWritableDirectoryLocal(dir string) error {
	file, err := os.CreateTemp(dir, ".write-check-*")
	if err != nil {
		return err
	}

	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}

	return os.Remove(name)
}

func deriveOverallStatus(statuses ...models.SystemHealthStatus) models.SystemHealthStatus {
	overall := models.SystemStatusOK
	for _, status := range statuses {
		switch status {
		case models.SystemStatusFailed:
			return models.SystemStatusFailed
		case models.SystemStatusWarning:
			overall = models.SystemStatusWarning
		case models.SystemStatusUnknown:
			if overall == models.SystemStatusOK {
				overall = models.SystemStatusUnknown
			}
		}
	}
	return overall
}

func buildAPIAlerts(api models.SystemAPIHealth) []models.SystemHealthAlert {
	if api.Status != models.SystemStatusWarning {
		return nil
	}
	return []models.SystemHealthAlert{{
		Severity: models.SystemStatusWarning,
		Area:     "api",
		Message:  "API configuration is incomplete but the service is running.",
	}}
}

func buildDatabaseAlerts(database models.SystemDatabaseHealth, err error) []models.SystemHealthAlert {
	alerts := make([]models.SystemHealthAlert, 0)
	if err != nil || !database.Reachable {
		alerts = append(alerts, models.SystemHealthAlert{
			Severity: models.SystemStatusFailed,
			Area:     "database",
			Message:  "Database is unreachable.",
		})
		return alerts
	}
	if database.MigrationDirty != nil && *database.MigrationDirty {
		alerts = append(alerts, models.SystemHealthAlert{
			Severity: models.SystemStatusFailed,
			Area:     "database",
			Message:  "Database migration state is dirty.",
		})
	}
	return alerts
}

func buildStorageAlerts(storage models.SystemStorageHealth) []models.SystemHealthAlert {
	switch storage.Status {
	case models.SystemStatusWarning:
		return []models.SystemHealthAlert{{
			Severity: models.SystemStatusWarning,
			Area:     "storage",
			Message:  fmt.Sprintf("Disk usage is high at %.1f%%.", storage.UsedPercent),
		}}
	case models.SystemStatusFailed:
		return []models.SystemHealthAlert{{
			Severity: models.SystemStatusFailed,
			Area:     "storage",
			Message:  fmt.Sprintf("Disk usage is critically high at %.1f%%.", storage.UsedPercent),
		}}
	default:
		return nil
	}
}

func buildPathAlerts(paths models.SystemPathsHealth) []models.SystemHealthAlert {
	alerts := make([]models.SystemHealthAlert, 0)
	for _, path := range paths.Paths {
		if path.Status == models.SystemStatusFailed {
			alerts = append(alerts, models.SystemHealthAlert{
				Severity: models.SystemStatusFailed,
				Area:     "paths",
				Message:  fmt.Sprintf("%s: %s", path.Path, path.Message),
			})
		}
	}
	return alerts
}

func buildIntakeAlerts(intake models.SystemIntakeHealth, err error) []models.SystemHealthAlert {
	if err != nil {
		return nil
	}

	alerts := make([]models.SystemHealthAlert, 0)
	if intake.FailedImageCount > 0 {
		alerts = append(alerts, models.SystemHealthAlert{
			Severity: models.SystemStatusWarning,
			Area:     "intake",
			Message:  fmt.Sprintf("%d image assets are failed.", intake.FailedImageCount),
		})
	}
	if intake.StuckProcessingImageCount > 0 {
		alerts = append(alerts, models.SystemHealthAlert{
			Severity: models.SystemStatusWarning,
			Area:     "intake",
			Message:  fmt.Sprintf("%d image assets appear stuck in processing.", intake.StuckProcessingImageCount),
		})
	}
	return alerts
}

func buildAIAlerts(ai models.SystemAIHealth, err error) []models.SystemHealthAlert {
	if err != nil {
		return nil
	}
	if !ai.AIAssistEnabled {
		return []models.SystemHealthAlert{{
			Severity: models.SystemStatusWarning,
			Area:     "ai",
			Message:  "No active AI provider is configured.",
		}}
	}
	if ai.LastTestStatus != nil && *ai.LastTestStatus == "failed" {
		return []models.SystemHealthAlert{{
			Severity: models.SystemStatusWarning,
			Area:     "ai",
			Message:  "Active AI provider last test failed.",
		}}
	}
	return nil
}
