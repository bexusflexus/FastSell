package models

import "time"

type SystemVersionResponse struct {
	InstalledVersion string  `json:"installed_version"`
	LatestVersion    *string `json:"latest_version"`
	UpdateAvailable  bool    `json:"update_available"`
}

type SystemHealthStatus string

const (
	SystemStatusOK      SystemHealthStatus = "ok"
	SystemStatusWarning SystemHealthStatus = "warning"
	SystemStatusFailed  SystemHealthStatus = "failed"
	SystemStatusUnknown SystemHealthStatus = "unknown"
)

type AdminSystemHealthResponse struct {
	OverallStatus     SystemHealthStatus   `json:"overall_status"`
	GeneratedDatetime time.Time            `json:"generated_datetime"`
	API               SystemAPIHealth      `json:"api"`
	Database          SystemDatabaseHealth `json:"database"`
	Storage           SystemStorageHealth  `json:"storage"`
	Paths             SystemPathsHealth    `json:"paths"`
	Intake            SystemIntakeHealth   `json:"intake"`
	AI                SystemAIHealth       `json:"ai"`
	Frontend          SystemSimpleHealth   `json:"frontend"`
	Docker            SystemDockerHealth   `json:"docker"`
	Alerts            []SystemHealthAlert  `json:"alerts"`
}

type SystemAPIHealth struct {
	Status        SystemHealthStatus `json:"status"`
	UptimeSeconds int64              `json:"uptime_seconds"`
	ServerTime    time.Time          `json:"server_time"`
	DataRoot      string             `json:"data_root"`
	ImageRoot     string             `json:"image_root"`
	IntakeDir     string             `json:"intake_dir"`
}

type SystemDatabaseHealth struct {
	Status             SystemHealthStatus `json:"status"`
	Reachable          bool               `json:"reachable"`
	MigrationVersion   *int64             `json:"migration_version"`
	MigrationDirty     *bool              `json:"migration_dirty"`
	DatabaseSizeBytes  *int64             `json:"database_size_bytes"`
	ContainerCount     int                `json:"container_count"`
	ItemCount          int                `json:"item_count"`
	ImageAssetCount    int                `json:"image_asset_count"`
	UploadSessionCount int                `json:"upload_session_count"`
	UploadGroupCount   int                `json:"upload_group_count"`
}

type SystemStorageHealth struct {
	Status      SystemHealthStatus `json:"status"`
	Path        string             `json:"path"`
	TotalBytes  uint64             `json:"total_bytes"`
	FreeBytes   uint64             `json:"free_bytes"`
	UsedBytes   uint64             `json:"used_bytes"`
	UsedPercent float64            `json:"used_percent"`
}

type SystemPathsHealth struct {
	Status SystemHealthStatus `json:"status"`
	Paths  []SystemPathHealth `json:"paths"`
}

type SystemPathHealth struct {
	Path        string             `json:"path"`
	Status      SystemHealthStatus `json:"status"`
	Exists      bool               `json:"exists"`
	IsDirectory bool               `json:"is_directory"`
	Readable    bool               `json:"readable"`
	Writable    bool               `json:"writable"`
	Message     string             `json:"message"`
}

type SystemIntakeHealth struct {
	Status                    SystemHealthStatus `json:"status"`
	PendingOrUploadedCount    int                `json:"pending_or_uploaded_image_count"`
	ProcessingImageCount      int                `json:"processing_image_count"`
	ProcessedImageCount       int                `json:"processed_image_count"`
	FailedImageCount          int                `json:"failed_image_count"`
	StuckProcessingImageCount int                `json:"stuck_processing_image_count"`
	OldestPendingDatetime     *time.Time         `json:"oldest_pending_datetime"`
	LatestProcessedDatetime   *time.Time         `json:"latest_processed_datetime"`
	UploadSessionStatusCounts map[string]int     `json:"upload_session_status_counts,omitempty"`
}

type SystemAIHealth struct {
	Status             SystemHealthStatus `json:"status"`
	AIAssistEnabled    bool               `json:"ai_assist_enabled"`
	ActiveProviderID   *string            `json:"active_provider_id"`
	ActiveProviderName *string            `json:"active_provider_name"`
	ActiveProviderType *string            `json:"active_provider_type"`
	ActiveModelName    *string            `json:"active_model_name"`
	VisionEnabled      *bool              `json:"vision_enabled"`
	LastTestStatus     *string            `json:"last_test_status"`
	LastTestDatetime   *time.Time         `json:"last_test_datetime"`
	LastErrorMessage   *string            `json:"last_error_message"`
}

type SystemSimpleHealth struct {
	Status      SystemHealthStatus `json:"status"`
	HostingMode string             `json:"hosting_mode,omitempty"`
	PublicURL   string             `json:"public_url,omitempty"`
	Message     string             `json:"message"`
}

type SystemDockerHealth struct {
	Status            SystemHealthStatus    `json:"status"`
	Message           string                `json:"message,omitempty"`
	GeneratedDatetime *time.Time            `json:"generated_datetime,omitempty"`
	Services          []SystemDockerService `json:"services,omitempty"`
	Alerts            []SystemHealthAlert   `json:"alerts,omitempty"`
}

type SystemDockerService struct {
	ServiceName   string             `json:"service_name"`
	ContainerName string             `json:"container_name"`
	Image         string             `json:"image"`
	State         string             `json:"state"`
	Health        string             `json:"health"`
	RestartCount  int64              `json:"restart_count"`
	StartedAt     *time.Time         `json:"started_at"`
	FinishedAt    *time.Time         `json:"finished_at"`
	Ports         []string           `json:"ports"`
	Status        SystemHealthStatus `json:"status"`
}

type SystemHealthAlert struct {
	Severity SystemHealthStatus `json:"severity"`
	Area     string             `json:"area"`
	Message  string             `json:"message"`
}
