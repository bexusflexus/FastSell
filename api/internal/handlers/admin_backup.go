package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"

	backupsvc "fastsell-api/internal/backup"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
)

type AdminBackupHandler struct {
	service      *backupsvc.Service
	timezoneFile string
}

func NewAdminBackupHandler(service *backupsvc.Service) *AdminBackupHandler {
	return &AdminBackupHandler{service: service, timezoneFile: backupsvc.TimezoneDataFile}
}

func (h *AdminBackupHandler) GetTimezones(w http.ResponseWriter, _ *http.Request) {
	timezones, err := backupsvc.LoadTimezones(h.timezoneFile)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "server timezone data is unavailable")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"timezones": timezones})
}

func (h *AdminBackupHandler) MaintenanceMiddleware(next http.Handler) http.Handler {
	return h.service.Gate().Middleware(next)
}

func (h *AdminBackupHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := h.service.GetSettings(r.Context())
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load backup settings")
		return
	}
	respond.JSON(w, http.StatusOK, settings)
}

func (h *AdminBackupHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	var input backupsvc.Settings
	if err := decodeBackupJSON(r, &input); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	settings, err := h.service.UpdateSettings(r.Context(), input)
	if err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	respond.JSON(w, http.StatusOK, settings)
}

func (h *AdminBackupHandler) List(w http.ResponseWriter, _ *http.Request) {
	backups, err := h.service.ListBackups()
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load backup inventory")
		return
	}
	respond.JSON(w, http.StatusOK, map[string]any{"backups": backups})
}

func (h *AdminBackupHandler) Create(w http.ResponseWriter, _ *http.Request) {
	job, err := h.service.StartBackup("manual")
	if handleBackupOperationError(w, err) {
		return
	}
	respond.JSON(w, http.StatusAccepted, job)
}

func (h *AdminBackupHandler) CreateMedia(w http.ResponseWriter, _ *http.Request) {
	job, err := h.service.StartMediaArchive()
	if handleBackupOperationError(w, err) {
		return
	}
	respond.JSON(w, http.StatusAccepted, job)
}

func (h *AdminBackupHandler) GetJob(w http.ResponseWriter, r *http.Request) {
	job, err := h.service.GetJob(chi.URLParam(r, "jobID"))
	if errors.Is(err, os.ErrNotExist) {
		respond.Error(w, http.StatusNotFound, "backup job not found")
		return
	}
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to load backup job")
		return
	}
	respond.JSON(w, http.StatusOK, job)
}

func (h *AdminBackupHandler) Validate(w http.ResponseWriter, r *http.Request) {
	result, err := h.service.ValidateBackup(r.Context(), chi.URLParam(r, "backupID"))
	if handleBackupOperationError(w, err) {
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

func (h *AdminBackupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	err := h.service.DeleteBackup(chi.URLParam(r, "backupID"))
	if handleBackupOperationError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *AdminBackupHandler) Restore(w http.ResponseWriter, r *http.Request) {
	var input backupsvc.RestoreRequest
	if err := decodeBackupJSON(r, &input); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	job, err := h.service.StartRestore(chi.URLParam(r, "backupID"), input.Confirmation)
	if handleBackupOperationError(w, err) {
		return
	}
	respond.JSON(w, http.StatusAccepted, job)
}

func handleBackupOperationError(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, backupsvc.ErrOperationConflict):
		respond.ErrorCode(w, http.StatusConflict, "backup_operation_conflict", "Another database backup, restore, validation, deletion, or media archive is already running.")
	case errors.Is(err, os.ErrNotExist):
		respond.Error(w, http.StatusNotFound, "backup not found in the FastSell backup inventory")
	default:
		respond.Error(w, http.StatusBadRequest, err.Error())
	}
	return true
}

func decodeBackupJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errors.New("request body must be valid JSON")
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
