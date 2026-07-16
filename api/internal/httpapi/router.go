package httpapi

import (
	"net/http"
	"time"

	"fastsell-api/internal/handlers"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func NewRouter(containerStore *handlers.ContainerStore, locationHandler *handlers.LocationHandler, containerTypeHandler *handlers.ContainerTypeHandler, inventoryGroupHandler *handlers.InventoryGroupHandler, uploadHandler *handlers.UploadHandler, reviewHandler *handlers.ReviewHandler, imageHandler *handlers.ImageHandler, itemHandler *handlers.ItemHandler, inventoryHandler *handlers.InventoryHandler, aiAdminHandler *handlers.AIAdminHandler, sellAdminHandler *handlers.SellAdminHandler, sellPublicHandler *handlers.SellPublicHandler, adminMetricsHandler *handlers.AdminMetricsHandler, adminSystemHandler *handlers.AdminSystemHandler, adminBackupHandler *handlers.AdminBackupHandler, versionHandler *handlers.VersionHandler, listingDraftHandler *handlers.ListingDraftHandler, wholeSceneHandler *handlers.WholeSceneHandler, pool *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(devCORSMiddleware)

	healthHandler := handlers.NewHealthHandler(pool)
	containerHandler := handlers.NewContainerHandler(containerStore)

	r.Get("/health", healthHandler.Health)
	r.Get("/health/db", healthHandler.Database)

	r.Route("/api", func(r chi.Router) {
		r.Use(adminBackupHandler.MaintenanceMiddleware)
		r.Get("/containers", containerHandler.List)
		r.Post("/containers", containerHandler.Create)
		r.Patch("/containers/{id}", containerHandler.Update)
		r.Get("/containers/{id}/delete-preview", containerHandler.DeletePreview)
		r.Delete("/containers/{id}", containerHandler.Delete)
		r.Get("/containers/{id}/summary", containerHandler.Summary)
		r.Get("/uploads/next-item-number", uploadHandler.GetNextItemNumber)
		r.Get("/uploads/{id}", uploadHandler.GetSession)
		r.Post("/uploads/images", uploadHandler.CreateImages)
		r.Get("/review/upload-groups", reviewHandler.ListUploadGroups)
		r.Get("/review/whole-scene-scans", wholeSceneHandler.ListReviewScans)
		r.Get("/review/upload-groups/{id}", reviewHandler.GetUploadGroup)
		r.Get("/review/upload-groups/{id}/delete-preview", reviewHandler.DeletePreview)
		r.Post("/review/upload-groups/{id}/ai-assist", reviewHandler.QueueAIAssist)
		r.Post("/review/upload-groups/{id}/images", reviewHandler.UploadImages)
		r.Delete("/review/upload-groups/{id}/images/{image}", reviewHandler.DeleteImage)
		r.Post("/review/upload-groups/{id}/approve", reviewHandler.ApproveUploadGroup)
		r.Post("/review/upload-groups/{id}/approve-all", reviewHandler.ApproveUploadGroup)
		r.Delete("/review/upload-groups/{id}", reviewHandler.DeleteUploadGroup)
		r.Post("/whole-scene/scans", wholeSceneHandler.CreateScan)
		r.Get("/whole-scene/scans/{id}", wholeSceneHandler.GetScan)
		r.Delete("/whole-scene/scans/{id}", wholeSceneHandler.DeleteScan)
		r.Post("/whole-scene/scans/{id}/analyze", wholeSceneHandler.QueueAnalysis)
		r.Post("/whole-scene/scans/{id}/candidates", wholeSceneHandler.AddCandidate)
		r.Patch("/whole-scene/scans/{id}/candidates/{candidateID}", wholeSceneHandler.PatchCandidate)
		r.Post("/whole-scene/scans/{id}/candidates/{candidateID}/ai-assist", wholeSceneHandler.AssistCandidate)
		r.Post("/whole-scene/scans/{id}/candidates/{candidateID}/images", wholeSceneHandler.UploadCandidateImages)
		r.Delete("/whole-scene/scans/{id}/candidates/{candidateID}/images/{cropID}", wholeSceneHandler.DeleteCandidateImage)
		r.Post("/whole-scene/scans/{id}/candidates/{candidateID}/reject", wholeSceneHandler.RejectCandidate)
		r.Post("/whole-scene/scans/{id}/candidates/{candidateID}/approve", wholeSceneHandler.ApproveCandidate)
		r.Get("/images/{id}", imageHandler.ServeImage)
		r.Get("/inventory/containers", inventoryHandler.ListContainers)
		r.Get("/inventory-groups", inventoryGroupHandler.List)
		r.Post("/inventory-groups", inventoryGroupHandler.Create)
		r.Patch("/inventory-groups/{id}", inventoryGroupHandler.Patch)
		r.Delete("/inventory-groups/{id}", inventoryGroupHandler.Delete)
		r.Get("/item-dispositions", itemHandler.ListDispositions)
		r.Get("/items", itemHandler.List)
		r.Route("/items/{id}", func(r chi.Router) {
			r.Get("/", itemHandler.Get)
			r.Patch("/", itemHandler.Patch)
			r.Delete("/", itemHandler.Delete)
			r.Get("/delete-preview", itemHandler.DeletePreview)
			r.Post("/archive", itemHandler.Archive)
			r.Post("/unarchive", itemHandler.Unarchive)
			r.Get("/disposition-history", itemHandler.ListDispositionHistory)
			r.Post("/images", itemHandler.UploadImages)
			r.Delete("/images/{image}", itemHandler.DeleteImage)
			r.Get("/listing-drafts", listingDraftHandler.ListByItem)
			r.Post("/listing-drafts", listingDraftHandler.CreateForItem)
		})
		r.Get("/listing-drafts/{id}", listingDraftHandler.Get)
		r.Post("/listing-drafts/{id}/prepare-photos", listingDraftHandler.PreparePhotos)
		r.Patch("/listing-drafts/{id}", listingDraftHandler.Patch)
		r.Delete("/listing-drafts/{id}", listingDraftHandler.Delete)
		r.Get("/sell/providers", sellPublicHandler.ListProviders)
		r.Get("/admin/locations", locationHandler.List)
		r.Post("/admin/locations", locationHandler.Create)
		r.Get("/admin/locations/{id}", locationHandler.Get)
		r.Get("/admin/locations/{id}/delete-preview", locationHandler.DeletePreview)
		r.Patch("/admin/locations/{id}", locationHandler.Patch)
		r.Delete("/admin/locations/{id}", locationHandler.Delete)
		r.Post("/admin/locations/{id}/archive", locationHandler.Archive)
		r.Post("/admin/locations/{id}/unarchive", locationHandler.Unarchive)
		r.Get("/admin/container-types", containerTypeHandler.List)
		r.Post("/admin/container-types", containerTypeHandler.Create)
		r.Get("/admin/container-types/{id}", containerTypeHandler.Get)
		r.Get("/admin/container-types/{id}/delete-preview", containerTypeHandler.DeletePreview)
		r.Patch("/admin/container-types/{id}", containerTypeHandler.Patch)
		r.Delete("/admin/container-types/{id}", containerTypeHandler.Delete)
		r.Post("/admin/container-types/{id}/archive", containerTypeHandler.Archive)
		r.Post("/admin/container-types/{id}/unarchive", containerTypeHandler.Unarchive)
		r.Get("/admin/ai/providers", aiAdminHandler.ListProviders)
		r.Post("/admin/ai/providers", aiAdminHandler.CreateProvider)
		r.Get("/admin/ai/providers/{id}", aiAdminHandler.GetProvider)
		r.Patch("/admin/ai/providers/{id}", aiAdminHandler.PatchProvider)
		r.Delete("/admin/ai/providers/{id}", aiAdminHandler.DeleteProvider)
		r.Post("/admin/ai/providers/{id}/set-active", aiAdminHandler.SetActiveProvider)
		r.Post("/admin/ai/providers/{id}/test", aiAdminHandler.TestProvider)
		r.Get("/admin/ai/settings", aiAdminHandler.GetSettings)
		r.Patch("/admin/ai/settings", aiAdminHandler.PatchSettings)
		r.Get("/admin/sell/providers", sellAdminHandler.ListProviders)
		r.Post("/admin/sell/providers", sellAdminHandler.CreateProvider)
		r.Get("/admin/sell/providers/{id}", sellAdminHandler.GetProvider)
		r.Patch("/admin/sell/providers/{id}", sellAdminHandler.PatchProvider)
		r.Delete("/admin/sell/providers/{id}", sellAdminHandler.DeleteProvider)
		r.Post("/admin/sell/providers/{id}/enable", sellAdminHandler.EnableProvider)
		r.Post("/admin/sell/providers/{id}/disable", sellAdminHandler.DisableProvider)
		r.Get("/admin/metrics", adminMetricsHandler.Get)
		r.Get("/admin/system/health", adminSystemHandler.GetHealth)
		r.Get("/admin/backup-settings", adminBackupHandler.GetSettings)
		r.Put("/admin/backup-settings", adminBackupHandler.PutSettings)
		r.Get("/admin/backups", adminBackupHandler.List)
		r.Post("/admin/backups", adminBackupHandler.Create)
		r.Post("/admin/backups/media", adminBackupHandler.CreateMedia)
		r.Get("/admin/backups/jobs/{jobID}", adminBackupHandler.GetJob)
		r.Post("/admin/backups/{backupID}/validate", adminBackupHandler.Validate)
		r.Delete("/admin/backups/{backupID}", adminBackupHandler.Delete)
		r.Post("/admin/backups/{backupID}/restore", adminBackupHandler.Restore)
		r.Get("/admin/restores/jobs/{jobID}", adminBackupHandler.GetJob)
		r.Get("/system/version", versionHandler.Get)
	})

	return r
}

func devCORSMiddleware(next http.Handler) http.Handler {
	// LAN/dev-only CORS for the first API baseline. Tighten this before exposing beyond local development.
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "600")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
