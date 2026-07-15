package config

import (
	"os"
	"path/filepath"
)

type Config struct {
	DatabaseURL                   string
	FastSellVersion               string
	Port                          string
	DataRoot                      string
	BackupRoot                    string
	MigrationRoot                 string
	FrontendHostingMode           string
	FrontendPublicURL             string
	SystemAgentURL                string
	ListingPhotoExportRoot        string
	ListingPhotoExportHostRoot    string
	ListingPhotoExportTTLHours    int64
	IntakeDir                     string
	IntakeProcessingDir           string
	IntakeFailedDir               string
	ImageRoot                     string
	ImageOriginalsDir             string
	ImageThumbnailsDir            string
	ImageNormalizedDir            string
	IntakeWorkerEnabled           bool
	IntakeScanIntervalSeconds     int64
	IntakeStableSeconds           int64
	MaxUploadMB                   int64
	ItemImageMaxUploadMB          int64
	ItemImageMaxCount             int64
	AIAssistWorkerEnabled         bool
	AIAssistScanIntervalSeconds   int64
	AIAssistMaxImages             int64
	AIAssistMaxImageBytes         int64
	WholeSceneWorkerEnabled       bool
	WholeSceneScanIntervalSeconds int64
	WholeSceneMaxImages           int64
	WholeSceneMaxImageBytes       int64
}

func Load() Config {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}

	dataRoot := os.Getenv("DATA_ROOT")
	if dataRoot == "" {
		dataRoot = "/app/data"
	}

	backupRoot := os.Getenv("FASTSELL_BACKUP_ROOT")
	if backupRoot == "" {
		backupRoot = "/app/backups"
	}

	migrationRoot := os.Getenv("FASTSELL_MIGRATION_ROOT")
	if migrationRoot == "" {
		migrationRoot = "/app/migrations"
	}

	intakeDir := os.Getenv("INTAKE_DIR")
	if intakeDir == "" {
		intakeDir = filepath.Join(dataRoot, "intake", "incoming")
	}

	intakeProcessingDir := os.Getenv("INTAKE_PROCESSING_DIR")
	if intakeProcessingDir == "" {
		intakeProcessingDir = filepath.Join(dataRoot, "intake", "processing")
	}

	intakeFailedDir := os.Getenv("INTAKE_FAILED_DIR")
	if intakeFailedDir == "" {
		intakeFailedDir = filepath.Join(dataRoot, "intake", "failed")
	}

	imageRoot := os.Getenv("IMAGE_ROOT")
	if imageRoot == "" {
		imageRoot = filepath.Join(dataRoot, "images")
	}

	imageOriginalsDir := os.Getenv("IMAGE_ORIGINALS_DIR")
	if imageOriginalsDir == "" {
		imageOriginalsDir = imageRoot + "/originals"
	}
	imageThumbnailsDir := os.Getenv("IMAGE_THUMBNAILS_DIR")
	if imageThumbnailsDir == "" {
		imageThumbnailsDir = imageRoot + "/thumbnails"
	}
	imageNormalizedDir := os.Getenv("IMAGE_NORMALIZED_DIR")
	if imageNormalizedDir == "" {
		imageNormalizedDir = imageRoot + "/normalized"
	}

	listingPhotoExportRoot := os.Getenv("LISTING_PHOTO_EXPORT_ROOT")
	if listingPhotoExportRoot == "" {
		listingPhotoExportRoot = filepath.Join(dataRoot, "exports", "listing-photos")
	}

	listingPhotoExportHostRoot := os.Getenv("LISTING_PHOTO_EXPORT_HOST_ROOT")
	if listingPhotoExportHostRoot == "" {
		listingPhotoExportHostRoot = "/srv/fastsell/data/exports/listing-photos"
	}

	return Config{
		DatabaseURL:                   os.Getenv("DATABASE_URL"),
		FastSellVersion:               envOrDefault("FASTSELL_VERSION", "candidate-development"),
		Port:                          port,
		DataRoot:                      dataRoot,
		BackupRoot:                    backupRoot,
		MigrationRoot:                 migrationRoot,
		FrontendHostingMode:           os.Getenv("FRONTEND_HOSTING_MODE"),
		FrontendPublicURL:             os.Getenv("FRONTEND_PUBLIC_URL"),
		SystemAgentURL:                os.Getenv("SYSTEM_AGENT_URL"),
		ListingPhotoExportRoot:        listingPhotoExportRoot,
		ListingPhotoExportHostRoot:    listingPhotoExportHostRoot,
		ListingPhotoExportTTLHours:    parseInt64Env("LISTING_PHOTO_EXPORT_TTL_HOURS", 24),
		IntakeDir:                     intakeDir,
		IntakeProcessingDir:           intakeProcessingDir,
		IntakeFailedDir:               intakeFailedDir,
		ImageRoot:                     imageRoot,
		ImageOriginalsDir:             imageOriginalsDir,
		ImageThumbnailsDir:            imageThumbnailsDir,
		ImageNormalizedDir:            imageNormalizedDir,
		IntakeWorkerEnabled:           parseBoolEnv("INTAKE_WORKER_ENABLED", true),
		IntakeScanIntervalSeconds:     parseInt64Env("INTAKE_SCAN_INTERVAL_SECONDS", 5),
		IntakeStableSeconds:           parseInt64Env("INTAKE_STABLE_SECONDS", 3),
		MaxUploadMB:                   parseInt64Env("MAX_UPLOAD_MB", 25),
		ItemImageMaxUploadMB:          parseInt64Env("ITEM_IMAGE_MAX_UPLOAD_MB", 10),
		ItemImageMaxCount:             parseInt64Env("ITEM_IMAGE_MAX_COUNT", 50),
		AIAssistWorkerEnabled:         parseBoolEnv("AI_ASSIST_WORKER_ENABLED", true),
		AIAssistScanIntervalSeconds:   parseInt64Env("AI_ASSIST_SCAN_INTERVAL_SECONDS", 2),
		AIAssistMaxImages:             parseInt64Env("AI_ASSIST_MAX_IMAGES", 6),
		AIAssistMaxImageBytes:         parseInt64Env("AI_ASSIST_MAX_IMAGE_BYTES", 10*1024*1024),
		WholeSceneWorkerEnabled:       parseBoolEnv("WHOLE_SCENE_WORKER_ENABLED", true),
		WholeSceneScanIntervalSeconds: parseInt64Env("WHOLE_SCENE_SCAN_INTERVAL_SECONDS", 2),
		WholeSceneMaxImages:           parseInt64Env("WHOLE_SCENE_MAX_IMAGES", 6),
		WholeSceneMaxImageBytes:       parseInt64Env("WHOLE_SCENE_MAX_IMAGE_BYTES", 10*1024*1024),
	}
}

func envOrDefault(name string, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

func parseInt64Env(name string, fallback int64) int64 {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	var parsed int64
	for _, ch := range value {
		if ch < '0' || ch > '9' {
			return fallback
		}
		parsed = parsed*10 + int64(ch-'0')
	}
	if parsed <= 0 {
		return fallback
	}

	return parsed
}

func parseBoolEnv(name string, fallback bool) bool {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	switch value {
	case "1", "true", "TRUE", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "no", "NO", "off", "OFF":
		return false
	default:
		return fallback
	}
}
