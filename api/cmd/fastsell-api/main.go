package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	backupsvc "fastsell-api/internal/backup"
	"fastsell-api/internal/config"
	"fastsell-api/internal/db"
	"fastsell-api/internal/handlers"
	"fastsell-api/internal/httpapi"
	"fastsell-api/internal/intakeworker"
)

func main() {
	cfg := config.Load()

	log.Printf(
		"starting fastsell-api on 0.0.0.0:%s database_url_configured=%t intake_dir=%s processing_dir=%s failed_dir=%s originals_dir=%s backup_root=%s worker_enabled=%t ai_assist_worker_enabled=%t whole_scene_worker_enabled=%t max_upload_mb=%d",
		cfg.Port,
		cfg.DatabaseURL != "",
		filepath.Clean(cfg.IntakeDir),
		filepath.Clean(cfg.IntakeProcessingDir),
		filepath.Clean(cfg.IntakeFailedDir),
		filepath.Clean(cfg.ImageOriginalsDir),
		filepath.Clean(cfg.BackupRoot),
		cfg.IntakeWorkerEnabled,
		cfg.AIAssistWorkerEnabled,
		cfg.WholeSceneWorkerEnabled,
		cfg.MaxUploadMB,
	)

	requiredDirs := []string{
		cfg.IntakeDir,
		cfg.IntakeProcessingDir,
		cfg.IntakeFailedDir,
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.ListingPhotoExportRoot,
	}
	for _, dir := range requiredDirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			log.Fatalf("failed to prepare directory %s: %v", filepath.Clean(dir), err)
		}
		if err := verifyWritableDirectory(dir); err != nil {
			log.Fatalf("directory is not writable %s: %v", filepath.Clean(dir), err)
		}
	}

	pool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		log.Fatal("database pool setup failed")
	}
	defer pool.Close()

	pingCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		cancel()
		log.Fatal("database ping failed")
	}
	cancel()
	log.Print("database connection verified")
	maintenanceGate := backupsvc.NewMaintenanceGate()

	containerStore := handlers.NewContainerStore(pool, handlers.ContainerDeleteDirs{
		ImageRoot:           cfg.ImageRoot,
		ImageOriginalsDir:   cfg.ImageOriginalsDir,
		IntakeDir:           cfg.IntakeDir,
		IntakeProcessingDir: cfg.IntakeProcessingDir,
		IntakeFailedDir:     cfg.IntakeFailedDir,
	})
	locationStore := handlers.NewLocationStore(pool)
	locationHandler := handlers.NewLocationHandler(locationStore)
	containerTypeStore := handlers.NewContainerTypeStore(pool)
	containerTypeHandler := handlers.NewContainerTypeHandler(containerTypeStore)
	inventoryGroupStore := handlers.NewInventoryGroupStore(pool)
	inventoryGroupHandler := handlers.NewInventoryGroupHandler(inventoryGroupStore)
	managedFiles := handlers.NewManagedFileService([]string{
		cfg.ImageRoot,
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.IntakeDir,
		cfg.IntakeProcessingDir,
		cfg.IntakeFailedDir,
	})
	uploadHandler := handlers.NewUploadHandler(pool, cfg.IntakeDir, cfg.MaxUploadMB)
	reviewStore := handlers.NewReviewStore(pool, managedFiles, handlers.NewItemImageStorageConfig(
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.ItemImageMaxUploadMB,
		int(cfg.ItemImageMaxCount),
	))
	reviewHandler := handlers.NewReviewHandler(reviewStore)
	imageHandler := handlers.NewImageHandler(pool, []string{
		cfg.ImageRoot,
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.IntakeDir,
		cfg.IntakeProcessingDir,
		cfg.IntakeFailedDir,
	})
	itemStore := handlers.NewItemStore(pool, managedFiles, handlers.NewItemImageStorageConfig(
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.ItemImageMaxUploadMB,
		int(cfg.ItemImageMaxCount),
	))
	itemHandler := handlers.NewItemHandler(itemStore)
	inventoryStore := handlers.NewInventoryStore(pool)
	inventoryHandler := handlers.NewInventoryHandler(inventoryStore)
	aiProviderStore := handlers.NewAIProviderStore(pool)
	aiAdminHandler := handlers.NewAIAdminHandler(aiProviderStore)
	sellProviderStore := handlers.NewSellProviderStore(pool)
	sellAdminHandler := handlers.NewSellAdminHandler(sellProviderStore)
	sellPublicHandler := handlers.NewSellPublicHandler(sellProviderStore)
	listingDraftStore := handlers.NewListingDraftStore(pool, handlers.ListingPhotoExportConfig{
		ExportRoot:      cfg.ListingPhotoExportRoot,
		ExportHostRoot:  cfg.ListingPhotoExportHostRoot,
		TTL:             time.Duration(cfg.ListingPhotoExportTTLHours) * time.Hour,
		SourceSafeRoots: []string{cfg.ImageRoot, cfg.ImageOriginalsDir},
		BeginWrite:      maintenanceGate.BeginWrite,
	})
	listingDraftHandler := handlers.NewListingDraftHandler(listingDraftStore)
	adminMetricsStore := handlers.NewAdminMetricsStore(pool)
	adminMetricsHandler := handlers.NewAdminMetricsHandler(adminMetricsStore)
	adminSystemStore := handlers.NewAdminSystemStore(pool, cfg, time.Now().UTC())
	adminSystemHandler := handlers.NewAdminSystemHandler(adminSystemStore)
	backupService, err := backupsvc.NewService(backupsvc.Config{
		Root: cfg.BackupRoot, DataRoot: cfg.DataRoot, FastSellVersion: cfg.FastSellVersion,
		DatabaseURL: cfg.DatabaseURL,
	}, backupsvc.NewPostgresDatabase(pool, cfg.MigrationRoot), backupsvc.NewPostgresSettingsStore(pool), backupsvc.ExecRunner{}, maintenanceGate)
	if err != nil {
		log.Fatalf("backup service setup failed: %v", err)
	}
	backupScheduler := backupsvc.NewScheduler(func() {
		if _, err := backupService.StartBackup("scheduled"); err != nil && !errors.Is(err, backupsvc.ErrOperationConflict) {
			log.Printf("scheduled database backup could not be queued: %v", err)
		}
	})
	backupService.SetSettingsApplyHook(backupScheduler.Start)
	backupSettings, err := backupService.GetSettings(context.Background())
	if err != nil {
		log.Fatalf("backup settings load failed: %v", err)
	}
	if err := backupScheduler.Start(backupSettings); err != nil {
		log.Fatalf("backup scheduler setup failed: %v", err)
	}
	defer backupScheduler.Stop()
	adminBackupHandler := handlers.NewAdminBackupHandler(backupService)
	versionHandler := handlers.NewVersionHandler(cfg.FastSellVersion, handlers.NewGitHubReleaseLookup())
	wholeSceneStore := handlers.NewWholeSceneStore(pool, cfg.IntakeDir, cfg.MaxUploadMB, managedFiles, handlers.NewItemImageStorageConfig(
		cfg.ImageOriginalsDir,
		cfg.ImageThumbnailsDir,
		cfg.ImageNormalizedDir,
		cfg.ItemImageMaxUploadMB,
		int(cfg.ItemImageMaxCount),
	))
	wholeSceneHandler := handlers.NewWholeSceneHandler(wholeSceneStore)
	router := httpapi.NewRouter(containerStore, locationHandler, containerTypeHandler, inventoryGroupHandler, uploadHandler, reviewHandler, imageHandler, itemHandler, inventoryHandler, aiAdminHandler, sellAdminHandler, sellPublicHandler, adminMetricsHandler, adminSystemHandler, adminBackupHandler, versionHandler, listingDraftHandler, wholeSceneHandler, pool)

	workerCtx, stopWorker := context.WithCancel(context.Background())
	defer stopWorker()
	if cfg.IntakeWorkerEnabled {
		worker := intakeworker.New(pool, intakeworker.Config{
			IntakeDir:      cfg.IntakeDir,
			ProcessingDir:  cfg.IntakeProcessingDir,
			FailedDir:      cfg.IntakeFailedDir,
			OriginalsDir:   cfg.ImageOriginalsDir,
			ScanInterval:   time.Duration(cfg.IntakeScanIntervalSeconds) * time.Second,
			StableDuration: time.Duration(cfg.IntakeStableSeconds) * time.Second,
			MaxUploadBytes: cfg.MaxUploadMB * 1024 * 1024,
			MaxRowsPerScan: 25,
			BeginWrite:     maintenanceGate.BeginWrite,
		})
		go worker.Run(workerCtx)
	} else {
		log.Print("intake worker disabled by configuration")
	}

	if cfg.AIAssistWorkerEnabled {
		worker := handlers.NewReviewAIAssistWorker(pool, handlers.ReviewAIAssistWorkerConfig{
			ScanInterval:  time.Duration(cfg.AIAssistScanIntervalSeconds) * time.Second,
			MaxImages:     int(cfg.AIAssistMaxImages),
			MaxImageBytes: cfg.AIAssistMaxImageBytes,
			SafeRoots: []string{
				cfg.ImageRoot,
				cfg.ImageOriginalsDir,
				cfg.IntakeDir,
				cfg.IntakeProcessingDir,
				cfg.IntakeFailedDir,
			},
			BeginWrite: maintenanceGate.BeginWrite,
		})
		go worker.Run(workerCtx)
	} else {
		log.Print("review AI assist worker disabled by configuration")
	}

	if cfg.WholeSceneWorkerEnabled {
		worker := handlers.NewWholeSceneAnalysisWorker(pool, handlers.WholeSceneAnalysisWorkerConfig{
			ScanInterval:  time.Duration(cfg.WholeSceneScanIntervalSeconds) * time.Second,
			MaxImages:     int(cfg.WholeSceneMaxImages),
			MaxImageBytes: cfg.WholeSceneMaxImageBytes,
			OriginalsDir:  cfg.ImageOriginalsDir,
			ThumbnailsDir: cfg.ImageThumbnailsDir,
			NormalizedDir: cfg.ImageNormalizedDir,
			SafeRoots: []string{
				cfg.ImageRoot,
				cfg.ImageOriginalsDir,
				cfg.IntakeDir,
				cfg.IntakeProcessingDir,
				cfg.IntakeFailedDir,
			},
			BeginWrite: maintenanceGate.BeginWrite,
		})
		go worker.Run(workerCtx)
	} else {
		log.Print("whole scene analysis worker disabled by configuration")
	}

	go listingDraftStore.RunPhotoExportCleanupWorker(workerCtx)

	server := &http.Server{
		Addr:              "0.0.0.0:" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	stopCh := make(chan os.Signal, 1)
	signal.Notify(stopCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case sig := <-stopCh:
		log.Printf("shutdown signal received: %s", sig)
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server failed: %v", err)
		}
	}

	stopWorker()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Printf("server shutdown failed: %v", err)
	}
}

func verifyWritableDirectory(dir string) error {
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
