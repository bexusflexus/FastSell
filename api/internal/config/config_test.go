package config

import "testing"

func TestLoadDefaultsPort(t *testing.T) {
	t.Setenv("PORT", "")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FASTSELL_VERSION", "")
	t.Setenv("DATA_ROOT", "")
	t.Setenv("FRONTEND_HOSTING_MODE", "")
	t.Setenv("FRONTEND_PUBLIC_URL", "")
	t.Setenv("SYSTEM_AGENT_URL", "")
	t.Setenv("LISTING_PHOTO_EXPORT_ROOT", "")
	t.Setenv("LISTING_PHOTO_EXPORT_HOST_ROOT", "")
	t.Setenv("LISTING_PHOTO_EXPORT_TTL_HOURS", "")
	t.Setenv("INTAKE_DIR", "")
	t.Setenv("INTAKE_PROCESSING_DIR", "")
	t.Setenv("INTAKE_FAILED_DIR", "")
	t.Setenv("IMAGE_ROOT", "")
	t.Setenv("IMAGE_ORIGINALS_DIR", "")
	t.Setenv("IMAGE_THUMBNAILS_DIR", "")
	t.Setenv("IMAGE_NORMALIZED_DIR", "")
	t.Setenv("INTAKE_WORKER_ENABLED", "")
	t.Setenv("INTAKE_SCAN_INTERVAL_SECONDS", "")
	t.Setenv("INTAKE_STABLE_SECONDS", "")
	t.Setenv("MAX_UPLOAD_MB", "")
	t.Setenv("ITEM_IMAGE_MAX_UPLOAD_MB", "")
	t.Setenv("ITEM_IMAGE_MAX_COUNT", "")
	t.Setenv("WHOLE_SCENE_WORKER_ENABLED", "")
	t.Setenv("WHOLE_SCENE_SCAN_INTERVAL_SECONDS", "")
	t.Setenv("WHOLE_SCENE_MAX_IMAGES", "")
	t.Setenv("WHOLE_SCENE_MAX_IMAGE_BYTES", "")

	cfg := Load()
	if cfg.Port != "8888" {
		t.Fatalf("expected default port 8888, got %q", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://example" {
		t.Fatalf("expected database URL from environment")
	}
	if cfg.FastSellVersion != "candidate-development" {
		t.Fatalf("expected candidate version default, got %q", cfg.FastSellVersion)
	}
	if cfg.DataRoot != "/app/data" {
		t.Fatalf("expected default data root, got %q", cfg.DataRoot)
	}
	if cfg.FrontendHostingMode != "" {
		t.Fatalf("expected empty frontend hosting mode by default, got %q", cfg.FrontendHostingMode)
	}
	if cfg.FrontendPublicURL != "" {
		t.Fatalf("expected empty frontend public URL by default, got %q", cfg.FrontendPublicURL)
	}
	if cfg.SystemAgentURL != "" {
		t.Fatalf("expected empty system agent URL by default, got %q", cfg.SystemAgentURL)
	}
	if cfg.ListingPhotoExportRoot != "/app/data/exports/listing-photos" {
		t.Fatalf("expected default listing photo export root, got %q", cfg.ListingPhotoExportRoot)
	}
	if cfg.ListingPhotoExportHostRoot != "/srv/fastsell/data/exports/listing-photos" {
		t.Fatalf("expected default listing photo export host root, got %q", cfg.ListingPhotoExportHostRoot)
	}
	if cfg.ListingPhotoExportTTLHours != 24 {
		t.Fatalf("expected default listing photo export TTL hours 24, got %d", cfg.ListingPhotoExportTTLHours)
	}
	if cfg.IntakeDir != "/app/data/intake/incoming" {
		t.Fatalf("expected default intake dir, got %q", cfg.IntakeDir)
	}
	if cfg.IntakeProcessingDir != "/app/data/intake/processing" {
		t.Fatalf("expected default processing dir, got %q", cfg.IntakeProcessingDir)
	}
	if cfg.IntakeFailedDir != "/app/data/intake/failed" {
		t.Fatalf("expected default failed dir, got %q", cfg.IntakeFailedDir)
	}
	if cfg.ImageRoot != "/app/data/images" {
		t.Fatalf("expected default image root, got %q", cfg.ImageRoot)
	}
	if cfg.ImageOriginalsDir != "/app/data/images/originals" {
		t.Fatalf("expected default originals dir, got %q", cfg.ImageOriginalsDir)
	}
	if cfg.ImageThumbnailsDir != "/app/data/images/thumbnails" {
		t.Fatalf("expected default thumbnails dir, got %q", cfg.ImageThumbnailsDir)
	}
	if cfg.ImageNormalizedDir != "/app/data/images/normalized" {
		t.Fatalf("expected default normalized dir, got %q", cfg.ImageNormalizedDir)
	}
	if !cfg.IntakeWorkerEnabled {
		t.Fatal("expected worker enabled by default")
	}
	if cfg.IntakeScanIntervalSeconds != 5 {
		t.Fatalf("expected default scan interval 5, got %d", cfg.IntakeScanIntervalSeconds)
	}
	if cfg.IntakeStableSeconds != 3 {
		t.Fatalf("expected default stable seconds 3, got %d", cfg.IntakeStableSeconds)
	}
	if cfg.MaxUploadMB != 25 {
		t.Fatalf("expected default max upload MB 25, got %d", cfg.MaxUploadMB)
	}
	if cfg.ItemImageMaxUploadMB != 10 {
		t.Fatalf("expected default item image max upload MB 10, got %d", cfg.ItemImageMaxUploadMB)
	}
	if cfg.ItemImageMaxCount != 50 {
		t.Fatalf("expected default item image max count 50, got %d", cfg.ItemImageMaxCount)
	}
	if !cfg.WholeSceneWorkerEnabled {
		t.Fatal("expected Whole Scene worker enabled by default")
	}
	if cfg.WholeSceneScanIntervalSeconds != 2 {
		t.Fatalf("expected default Whole Scene scan interval 2, got %d", cfg.WholeSceneScanIntervalSeconds)
	}
	if cfg.WholeSceneMaxImages != 6 {
		t.Fatalf("expected default Whole Scene max images 6, got %d", cfg.WholeSceneMaxImages)
	}
	if cfg.WholeSceneMaxImageBytes != 10*1024*1024 {
		t.Fatalf("expected default Whole Scene max image bytes 10485760, got %d", cfg.WholeSceneMaxImageBytes)
	}
}

func TestLoadUploadConfigFromEnvironment(t *testing.T) {
	t.Setenv("PORT", "9999")
	t.Setenv("DATABASE_URL", "postgres://example")
	t.Setenv("FASTSELL_VERSION", "v0.1.3")
	t.Setenv("DATA_ROOT", "/tmp/data")
	t.Setenv("FRONTEND_HOSTING_MODE", "nginx")
	t.Setenv("FRONTEND_PUBLIC_URL", "http://localhost:8888")
	t.Setenv("SYSTEM_AGENT_URL", "http://system-agent:8081")
	t.Setenv("LISTING_PHOTO_EXPORT_ROOT", "/tmp/exports")
	t.Setenv("LISTING_PHOTO_EXPORT_HOST_ROOT", "/srv/fastsell/data/exports/listing-photos")
	t.Setenv("LISTING_PHOTO_EXPORT_TTL_HOURS", "36")
	t.Setenv("INTAKE_DIR", "/tmp/intake")
	t.Setenv("INTAKE_PROCESSING_DIR", "/tmp/processing")
	t.Setenv("INTAKE_FAILED_DIR", "/tmp/failed")
	t.Setenv("IMAGE_ROOT", "/tmp/images")
	t.Setenv("IMAGE_ORIGINALS_DIR", "/tmp/originals")
	t.Setenv("IMAGE_THUMBNAILS_DIR", "/tmp/thumbnails")
	t.Setenv("IMAGE_NORMALIZED_DIR", "/tmp/normalized")
	t.Setenv("INTAKE_WORKER_ENABLED", "false")
	t.Setenv("INTAKE_SCAN_INTERVAL_SECONDS", "9")
	t.Setenv("INTAKE_STABLE_SECONDS", "2")
	t.Setenv("MAX_UPLOAD_MB", "7")
	t.Setenv("ITEM_IMAGE_MAX_UPLOAD_MB", "11")
	t.Setenv("ITEM_IMAGE_MAX_COUNT", "12")
	t.Setenv("WHOLE_SCENE_WORKER_ENABLED", "false")
	t.Setenv("WHOLE_SCENE_SCAN_INTERVAL_SECONDS", "4")
	t.Setenv("WHOLE_SCENE_MAX_IMAGES", "5")
	t.Setenv("WHOLE_SCENE_MAX_IMAGE_BYTES", "12345")

	cfg := Load()
	if cfg.FastSellVersion != "v0.1.3" {
		t.Fatalf("expected FastSell version from environment, got %q", cfg.FastSellVersion)
	}
	if cfg.DataRoot != "/tmp/data" {
		t.Fatalf("expected data root from environment, got %q", cfg.DataRoot)
	}
	if cfg.FrontendHostingMode != "nginx" {
		t.Fatalf("expected frontend hosting mode from environment, got %q", cfg.FrontendHostingMode)
	}
	if cfg.FrontendPublicURL != "http://localhost:8888" {
		t.Fatalf("expected frontend public URL from environment, got %q", cfg.FrontendPublicURL)
	}
	if cfg.SystemAgentURL != "http://system-agent:8081" {
		t.Fatalf("expected system agent URL from environment, got %q", cfg.SystemAgentURL)
	}
	if cfg.ListingPhotoExportRoot != "/tmp/exports" {
		t.Fatalf("expected listing photo export root from environment, got %q", cfg.ListingPhotoExportRoot)
	}
	if cfg.ListingPhotoExportHostRoot != "/srv/fastsell/data/exports/listing-photos" {
		t.Fatalf("expected listing photo export host root from environment, got %q", cfg.ListingPhotoExportHostRoot)
	}
	if cfg.ListingPhotoExportTTLHours != 36 {
		t.Fatalf("expected listing photo export TTL hours from environment, got %d", cfg.ListingPhotoExportTTLHours)
	}
	if cfg.IntakeDir != "/tmp/intake" {
		t.Fatalf("expected intake dir from environment, got %q", cfg.IntakeDir)
	}
	if cfg.IntakeProcessingDir != "/tmp/processing" {
		t.Fatalf("expected processing dir from environment, got %q", cfg.IntakeProcessingDir)
	}
	if cfg.IntakeFailedDir != "/tmp/failed" {
		t.Fatalf("expected failed dir from environment, got %q", cfg.IntakeFailedDir)
	}
	if cfg.ImageRoot != "/tmp/images" {
		t.Fatalf("expected image root from environment, got %q", cfg.ImageRoot)
	}
	if cfg.ImageOriginalsDir != "/tmp/originals" {
		t.Fatalf("expected originals dir from environment, got %q", cfg.ImageOriginalsDir)
	}
	if cfg.ImageThumbnailsDir != "/tmp/thumbnails" {
		t.Fatalf("expected thumbnails dir from environment, got %q", cfg.ImageThumbnailsDir)
	}
	if cfg.ImageNormalizedDir != "/tmp/normalized" {
		t.Fatalf("expected normalized dir from environment, got %q", cfg.ImageNormalizedDir)
	}
	if cfg.IntakeWorkerEnabled {
		t.Fatal("expected worker disabled from environment")
	}
	if cfg.IntakeScanIntervalSeconds != 9 {
		t.Fatalf("expected scan interval from environment, got %d", cfg.IntakeScanIntervalSeconds)
	}
	if cfg.IntakeStableSeconds != 2 {
		t.Fatalf("expected stable seconds from environment, got %d", cfg.IntakeStableSeconds)
	}
	if cfg.MaxUploadMB != 7 {
		t.Fatalf("expected max upload MB from environment, got %d", cfg.MaxUploadMB)
	}
	if cfg.ItemImageMaxUploadMB != 11 {
		t.Fatalf("expected item image max upload MB from environment, got %d", cfg.ItemImageMaxUploadMB)
	}
	if cfg.ItemImageMaxCount != 12 {
		t.Fatalf("expected item image max count from environment, got %d", cfg.ItemImageMaxCount)
	}
	if cfg.WholeSceneWorkerEnabled {
		t.Fatal("expected Whole Scene worker disabled from environment")
	}
	if cfg.WholeSceneScanIntervalSeconds != 4 {
		t.Fatalf("expected Whole Scene scan interval from environment, got %d", cfg.WholeSceneScanIntervalSeconds)
	}
	if cfg.WholeSceneMaxImages != 5 {
		t.Fatalf("expected Whole Scene max images from environment, got %d", cfg.WholeSceneMaxImages)
	}
	if cfg.WholeSceneMaxImageBytes != 12345 {
		t.Fatalf("expected Whole Scene max image bytes from environment, got %d", cfg.WholeSceneMaxImageBytes)
	}
}
