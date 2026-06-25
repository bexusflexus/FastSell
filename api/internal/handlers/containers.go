package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

var (
	errContainerTypeIDInvalid = errors.New("container_type_id must be a valid UUID")
	errContainerTypeNotFound  = errors.New("container type was not found")
	errContainerTypeArchived  = errors.New("cannot assign archived container type")
	errLocationIDInvalid      = errors.New("location_id must be a valid UUID")
	errLocationNotFound       = errors.New("location was not found")
	errLocationArchived       = errors.New("cannot assign archived location")
	errContainerNameConflict  = errors.New("container name already exists")
)

type ContainerStore struct {
	pool       *pgxpool.Pool
	deleteDirs ContainerDeleteDirs
}

type ContainerDeleteDirs struct {
	ImageRoot           string
	ImageOriginalsDir   string
	IntakeDir           string
	IntakeProcessingDir string
	IntakeFailedDir     string
}

func NewContainerStore(pool *pgxpool.Pool, deleteDirs ContainerDeleteDirs) *ContainerStore {
	return &ContainerStore{pool: pool, deleteDirs: deleteDirs}
}

func (s *ContainerStore) List(ctx context.Context, includeArchived bool) ([]models.Container, error) {
	query := `
		SELECT
			c.id::text,
			c.name,
			c.type,
			c.container_type_id::text,
			ct.name AS container_type_name,
			c.location_id::text,
			l.name AS location_name,
			c.location_description,
			c.notes,
			c.created_datetime,
			c.updated_datetime,
			c.archived,
			c.archived_datetime
		FROM containers c
		LEFT JOIN container_types ct ON ct.id = c.container_type_id
		LEFT JOIN locations l ON l.id = c.location_id
		WHERE ($1::boolean = true OR c.archived = false)
		ORDER BY c.created_datetime DESC, c.name ASC
	`

	rows, err := s.pool.Query(ctx, query, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	containers := make([]models.Container, 0)
	for rows.Next() {
		var container models.Container
		if err := rows.Scan(
			&container.ID,
			&container.Name,
			&container.Type,
			&container.ContainerTypeID,
			&container.ContainerTypeName,
			&container.LocationID,
			&container.LocationName,
			&container.LocationDescription,
			&container.Notes,
			&container.CreatedDatetime,
			&container.UpdatedDatetime,
			&container.Archived,
			&container.ArchivedDatetime,
		); err != nil {
			return nil, err
		}
		containers = append(containers, container)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return containers, nil
}

func (s *ContainerStore) Create(ctx context.Context, req models.CreateContainerRequest) (models.Container, error) {
	if err := s.validateContainerType(ctx, req.ContainerTypeID); err != nil {
		return models.Container{}, err
	}
	if err := s.validateLocation(ctx, req.LocationID); err != nil {
		return models.Container{}, err
	}

	query := `
		WITH inserted AS (
			INSERT INTO containers (name, type, container_type_id, location_id, location_description, notes, archived, archived_datetime)
			VALUES ($1, $2, $3, $4, $5, $6, false, NULL)
			RETURNING id, name, type, container_type_id, location_id, location_description, notes, created_datetime, updated_datetime, archived, archived_datetime
		)
		SELECT
			i.id::text,
			i.name,
			i.type,
			i.container_type_id::text,
			ct.name AS container_type_name,
			i.location_id::text,
			l.name AS location_name,
			i.location_description,
			i.notes,
			i.created_datetime,
			i.updated_datetime,
			i.archived,
			i.archived_datetime
		FROM inserted i
		LEFT JOIN container_types ct ON ct.id = i.container_type_id
		LEFT JOIN locations l ON l.id = i.location_id
	`

	row := s.pool.QueryRow(ctx, query, req.Name, req.Type, req.ContainerTypeID, req.LocationID, req.LocationDescription, req.Notes)

	var container models.Container
	err := row.Scan(
		&container.ID,
		&container.Name,
		&container.Type,
		&container.ContainerTypeID,
		&container.ContainerTypeName,
		&container.LocationID,
		&container.LocationName,
		&container.LocationDescription,
		&container.Notes,
		&container.CreatedDatetime,
		&container.UpdatedDatetime,
		&container.Archived,
		&container.ArchivedDatetime,
	)
	if err != nil && isUniqueViolation(err) {
		return models.Container{}, errContainerNameConflict
	}
	return container, err
}

func (s *ContainerStore) Update(ctx context.Context, id string, req models.UpdateContainerRequest) (models.Container, error) {
	containerTypeID := req.ContainerTypeID
	if !req.ContainerTypeIDSupplied {
		containerTypeID = nil
	}
	if req.ContainerTypeIDSupplied {
		if err := s.validateContainerType(ctx, req.ContainerTypeID); err != nil {
			return models.Container{}, err
		}
	}
	locationID := req.LocationID
	if !req.LocationIDSupplied {
		locationID = nil
	}
	if req.LocationIDSupplied {
		if err := s.validateLocation(ctx, req.LocationID); err != nil {
			return models.Container{}, err
		}
	}

	query := `
		WITH updated AS (
			UPDATE containers
			SET
				name = CASE WHEN $2 THEN $3 ELSE name END,
				type = CASE WHEN $4 THEN $5 ELSE type END,
				container_type_id = CASE WHEN $6 THEN $7::uuid ELSE container_type_id END,
				location_id = CASE WHEN $8 THEN $9::uuid ELSE location_id END,
				location_description = CASE WHEN $10 THEN $11 ELSE location_description END,
				notes = CASE WHEN $12 THEN $13 ELSE notes END,
				archived = CASE WHEN $14 THEN $15 ELSE archived END,
				archived_datetime = CASE
					WHEN $14 AND $15 AND archived = false THEN now()
					WHEN $14 AND $15 AND archived = true THEN archived_datetime
					WHEN $14 AND $15 = false THEN NULL
					ELSE archived_datetime
				END,
				updated_datetime = now()
			WHERE id = $1
			RETURNING id, name, type, container_type_id, location_id, location_description, notes, created_datetime, updated_datetime, archived, archived_datetime
		)
		SELECT
			u.id::text,
			u.name,
			u.type,
			u.container_type_id::text,
			ct.name AS container_type_name,
			u.location_id::text,
			l.name AS location_name,
			u.location_description,
			u.notes,
			u.created_datetime,
			u.updated_datetime,
			u.archived,
			u.archived_datetime
		FROM updated u
		LEFT JOIN container_types ct ON ct.id = u.container_type_id
		LEFT JOIN locations l ON l.id = u.location_id
	`

	var archivedValue bool
	if req.Archived != nil {
		archivedValue = *req.Archived
	}

	row := s.pool.QueryRow(
		ctx,
		query,
		id,
		req.NameSupplied,
		req.Name,
		req.TypeSupplied,
		req.Type,
		req.ContainerTypeIDSupplied,
		containerTypeID,
		req.LocationIDSupplied,
		locationID,
		req.LocationDescriptionSupplied,
		req.LocationDescription,
		req.NotesSupplied,
		req.Notes,
		req.ArchivedSupplied,
		archivedValue,
	)

	var container models.Container
	err := row.Scan(
		&container.ID,
		&container.Name,
		&container.Type,
		&container.ContainerTypeID,
		&container.ContainerTypeName,
		&container.LocationID,
		&container.LocationName,
		&container.LocationDescription,
		&container.Notes,
		&container.CreatedDatetime,
		&container.UpdatedDatetime,
		&container.Archived,
		&container.ArchivedDatetime,
	)
	if err != nil && isUniqueViolation(err) {
		return models.Container{}, errContainerNameConflict
	}
	return container, err
}

func (s *ContainerStore) Summary(ctx context.Context, id string) (models.ContainerSummary, error) {
	query := `
		WITH selected_container AS (
			SELECT
				c.id,
				c.name,
				c.type,
				c.container_type_id,
				ct.name AS container_type_name,
				c.location_id,
				l.name AS location_name,
				c.location_description,
				c.notes,
				c.created_datetime,
				c.updated_datetime,
				c.archived,
				c.archived_datetime
			FROM containers c
			LEFT JOIN container_types ct ON ct.id = c.container_type_id
			LEFT JOIN locations l ON l.id = c.location_id
			WHERE c.id = $1
		),
		session_counts AS (
			SELECT
				count(*)::int AS upload_session_count,
				max(created_datetime) AS latest_upload_datetime
			FROM upload_sessions
			WHERE container_id = $1
		),
		group_counts AS (
			SELECT count(*)::int AS upload_group_count
			FROM upload_groups
			WHERE container_id = $1
		),
		image_counts AS (
			SELECT
				count(ia.id)::int AS image_count,
				count(ia.id) FILTER (WHERE ia.status = 'pending')::int AS pending_image_count,
				count(ia.id) FILTER (WHERE ia.status = 'uploaded')::int AS uploaded_image_count,
				count(ia.id) FILTER (WHERE ia.status = 'processing')::int AS processing_image_count,
				count(ia.id) FILTER (WHERE ia.status = 'processed')::int AS processed_image_count,
				count(ia.id) FILTER (WHERE ia.status = 'failed')::int AS failed_image_count
			FROM upload_groups ug
			LEFT JOIN image_assets ia ON ia.upload_group_id = ug.id
			WHERE ug.container_id = $1
		)
		SELECT
			sc.id::text,
			sc.name,
			sc.type,
			sc.container_type_id::text,
			sc.container_type_name,
			sc.location_id::text,
			sc.location_name,
			sc.location_description,
			sc.notes,
			sc.created_datetime,
			sc.updated_datetime,
			sc.archived,
			sc.archived_datetime,
			COALESCE(session_counts.upload_session_count, 0),
			COALESCE(group_counts.upload_group_count, 0),
			COALESCE(image_counts.image_count, 0),
			COALESCE(image_counts.pending_image_count, 0),
			COALESCE(image_counts.uploaded_image_count, 0),
			COALESCE(image_counts.processing_image_count, 0),
			COALESCE(image_counts.processed_image_count, 0),
			COALESCE(image_counts.failed_image_count, 0),
			session_counts.latest_upload_datetime
		FROM selected_container sc
		CROSS JOIN session_counts
		CROSS JOIN group_counts
		CROSS JOIN image_counts
	`

	row := s.pool.QueryRow(ctx, query, id)

	var summary models.ContainerSummary
	err := row.Scan(
		&summary.Container.ID,
		&summary.Container.Name,
		&summary.Container.Type,
		&summary.Container.ContainerTypeID,
		&summary.Container.ContainerTypeName,
		&summary.Container.LocationID,
		&summary.Container.LocationName,
		&summary.Container.LocationDescription,
		&summary.Container.Notes,
		&summary.Container.CreatedDatetime,
		&summary.Container.UpdatedDatetime,
		&summary.Container.Archived,
		&summary.Container.ArchivedDatetime,
		&summary.Summary.UploadSessionCount,
		&summary.Summary.UploadGroupCount,
		&summary.Summary.ImageCount,
		&summary.Summary.PendingImageCount,
		&summary.Summary.UploadedImageCount,
		&summary.Summary.ProcessingImageCount,
		&summary.Summary.ProcessedImageCount,
		&summary.Summary.FailedImageCount,
		&summary.Summary.LatestUploadDatetime,
	)
	return summary, err
}

func (s *ContainerStore) DeletePreview(ctx context.Context, id string) (models.ContainerDeletePreview, error) {
	preview, _, err := s.deletePreview(ctx, s.pool, id)
	return preview, err
}

func (s *ContainerStore) Delete(ctx context.Context, id string) (models.ContainerDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ContainerDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	preview, filePaths, err := s.deletePreview(ctx, tx, id)
	if err != nil {
		return models.ContainerDeleteResponse{}, err
	}

	counts, err := s.deleteContainerRows(ctx, tx, id)
	if err != nil {
		return models.ContainerDeleteResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ContainerDeleteResponse{}, err
	}

	counts.FilesDeleted, counts.FilesMissing, counts.FileDeleteErrors = s.deleteReferencedFiles(filePaths)

	warnings := make([]string, 0)
	if counts.FileDeleteErrors > 0 {
		warnings = append(warnings, "some referenced files could not be deleted; check API logs for details")
	}

	if counts.ImageAssets != preview.Counts.ImageAssets {
		warnings = append(warnings, "deleted image asset count differed from preview count")
	}

	return models.ContainerDeleteResponse{
		ContainerID: id,
		Deleted:     counts,
		Warnings:    warnings,
	}, nil
}

type containerQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *ContainerStore) deletePreview(ctx context.Context, q containerQuerier, id string) (models.ContainerDeletePreview, []string, error) {
	query := `
		WITH selected_container AS (
			SELECT
				c.id,
				c.name,
				c.type,
				c.container_type_id,
				ct.name AS container_type_name,
				c.location_id,
				l.name AS location_name,
				c.location_description,
				c.notes,
				c.created_datetime,
				c.updated_datetime,
				c.archived,
				c.archived_datetime
			FROM containers c
			LEFT JOIN container_types ct ON ct.id = c.container_type_id
			LEFT JOIN locations l ON l.id = c.location_id
			WHERE c.id = $1
		),
		item_ids AS (
			SELECT id
			FROM items
			WHERE container_id = $1
		),
		group_ids AS (
			SELECT id
			FROM upload_groups
			WHERE container_id = $1
		),
		deletable_session_ids AS (
			SELECT us.id
			FROM upload_sessions us
			WHERE us.container_id = $1
				AND NOT EXISTS (
					SELECT 1
					FROM upload_groups ug
					WHERE ug.session_id = us.id
						AND ug.container_id IS DISTINCT FROM $1
				)
		),
		image_asset_ids AS (
			SELECT DISTINCT ia.id
			FROM image_assets ia
			WHERE ia.item_id IN (SELECT id FROM item_ids)
				OR ia.upload_group_id IN (SELECT id FROM group_ids)
				OR ia.session_id IN (SELECT id FROM deletable_session_ids)
		),
		file_paths AS (
			SELECT DISTINCT path
			FROM image_assets ia
			CROSS JOIN LATERAL (
				VALUES (ia.file_path), (ia.thumbnail_path), (ia.normalized_path)
			) AS paths(path)
			WHERE ia.id IN (SELECT id FROM image_asset_ids)
				AND paths.path IS NOT NULL
				AND paths.path <> ''
		)
		SELECT
			sc.id::text,
			sc.name,
			sc.type,
			sc.container_type_id::text,
			sc.container_type_name,
			sc.location_id::text,
			sc.location_name,
			sc.location_description,
			sc.notes,
			sc.created_datetime,
			sc.updated_datetime,
			sc.archived,
			sc.archived_datetime,
			(SELECT count(*)::int FROM item_ids),
			(SELECT count(*)::int FROM deletable_session_ids),
			(SELECT count(*)::int FROM group_ids),
			(SELECT count(*)::int FROM image_asset_ids),
			(SELECT count(*)::int FROM file_paths)
		FROM selected_container sc
	`

	var preview models.ContainerDeletePreview
	err := q.QueryRow(ctx, query, id).Scan(
		&preview.Container.ID,
		&preview.Container.Name,
		&preview.Container.Type,
		&preview.Container.ContainerTypeID,
		&preview.Container.ContainerTypeName,
		&preview.Container.LocationID,
		&preview.Container.LocationName,
		&preview.Container.LocationDescription,
		&preview.Container.Notes,
		&preview.Container.CreatedDatetime,
		&preview.Container.UpdatedDatetime,
		&preview.Container.Archived,
		&preview.Container.ArchivedDatetime,
		&preview.Counts.Items,
		&preview.Counts.UploadSessions,
		&preview.Counts.UploadGroups,
		&preview.Counts.ImageAssets,
		&preview.Counts.FilePaths,
	)
	if err != nil {
		return models.ContainerDeletePreview{}, nil, err
	}

	fileRows, err := q.Query(ctx, `
		WITH item_ids AS (
			SELECT id FROM items WHERE container_id = $1
		),
		group_ids AS (
			SELECT id FROM upload_groups WHERE container_id = $1
		),
		deletable_session_ids AS (
			SELECT us.id
			FROM upload_sessions us
			WHERE us.container_id = $1
				AND NOT EXISTS (
					SELECT 1
					FROM upload_groups ug
					WHERE ug.session_id = us.id
						AND ug.container_id IS DISTINCT FROM $1
				)
		),
		image_asset_ids AS (
			SELECT DISTINCT ia.id
			FROM image_assets ia
			WHERE ia.item_id IN (SELECT id FROM item_ids)
				OR ia.upload_group_id IN (SELECT id FROM group_ids)
				OR ia.session_id IN (SELECT id FROM deletable_session_ids)
		)
		SELECT DISTINCT path
		FROM image_assets ia
		CROSS JOIN LATERAL (
			VALUES (ia.file_path), (ia.thumbnail_path), (ia.normalized_path)
		) AS paths(path)
		WHERE ia.id IN (SELECT id FROM image_asset_ids)
			AND paths.path IS NOT NULL
			AND paths.path <> ''
	`, id)
	if err != nil {
		return models.ContainerDeletePreview{}, nil, err
	}
	defer fileRows.Close()

	filePaths := make([]string, 0)
	for fileRows.Next() {
		var path string
		if err := fileRows.Scan(&path); err != nil {
			return models.ContainerDeletePreview{}, nil, err
		}
		filePaths = append(filePaths, path)
	}
	if err := fileRows.Err(); err != nil {
		return models.ContainerDeletePreview{}, nil, err
	}

	return preview, filePaths, nil
}

func (s *ContainerStore) deleteContainerRows(ctx context.Context, tx pgx.Tx, id string) (models.ContainerDeletedCounts, error) {
	query := `
		WITH item_ids AS (
			SELECT id FROM items WHERE container_id = $1
		),
		group_ids AS (
			SELECT id FROM upload_groups WHERE container_id = $1
		),
		deletable_session_ids AS (
			SELECT us.id
			FROM upload_sessions us
			WHERE us.container_id = $1
				AND NOT EXISTS (
					SELECT 1
					FROM upload_groups ug
					WHERE ug.session_id = us.id
						AND ug.container_id IS DISTINCT FROM $1
				)
		),
		image_asset_ids AS (
			SELECT DISTINCT ia.id
			FROM image_assets ia
			WHERE ia.item_id IN (SELECT id FROM item_ids)
				OR ia.upload_group_id IN (SELECT id FROM group_ids)
				OR ia.session_id IN (SELECT id FROM deletable_session_ids)
		),
		whole_scene_scan_ids AS (
			SELECT id
			FROM whole_scene_scans
			WHERE container_id = $1
				OR upload_session_id IN (SELECT id FROM deletable_session_ids)
		),
		deleted_whole_scene_scans AS (
			DELETE FROM whole_scene_scans
			WHERE id IN (SELECT id FROM whole_scene_scan_ids)
			RETURNING id
		),
		deleted_image_assets AS (
			DELETE FROM image_assets
			WHERE id IN (SELECT id FROM image_asset_ids)
				AND (SELECT count(*) FROM deleted_whole_scene_scans) >= 0
			RETURNING id
		),
		deleted_upload_groups AS (
			DELETE FROM upload_groups
			WHERE id IN (SELECT id FROM group_ids)
			RETURNING id
		),
		deleted_items AS (
			DELETE FROM items
			WHERE id IN (SELECT id FROM item_ids)
			RETURNING id
		),
		deleted_upload_sessions AS (
			DELETE FROM upload_sessions
			WHERE id IN (SELECT id FROM deletable_session_ids)
			RETURNING id
		),
		deleted_container AS (
			DELETE FROM containers
			WHERE id = $1
			RETURNING id
		)
		SELECT
			(SELECT count(*)::int FROM deleted_container),
			(SELECT count(*)::int FROM deleted_items),
			(SELECT count(*)::int FROM deleted_upload_sessions),
			(SELECT count(*)::int FROM deleted_upload_groups),
			(SELECT count(*)::int FROM deleted_image_assets)
	`

	var counts models.ContainerDeletedCounts
	err := tx.QueryRow(ctx, query, id).Scan(
		&counts.Containers,
		&counts.Items,
		&counts.UploadSessions,
		&counts.UploadGroups,
		&counts.ImageAssets,
	)
	return counts, err
}

func (s *ContainerStore) deleteReferencedFiles(paths []string) (int, int, int) {
	deleted := 0
	missing := 0
	deleteErrors := 0
	seen := make(map[string]struct{})

	for _, rawPath := range paths {
		cleanPath := filepath.Clean(rawPath)
		if _, ok := seen[cleanPath]; ok {
			continue
		}
		seen[cleanPath] = struct{}{}

		if !s.isSafeDeletePath(cleanPath) {
			deleteErrors++
			log.Printf("skipping unsafe referenced file path during container delete: %s", cleanPath)
			continue
		}

		if err := os.Remove(cleanPath); err != nil {
			if os.IsNotExist(err) {
				missing++
				continue
			}
			deleteErrors++
			log.Printf("failed to delete referenced file during container delete path=%s error=%v", cleanPath, err)
			continue
		}
		deleted++
	}

	return deleted, missing, deleteErrors
}

func (s *ContainerStore) isSafeDeletePath(path string) bool {
	allowedRoots := []string{
		s.deleteDirs.ImageRoot,
		s.deleteDirs.ImageOriginalsDir,
		s.deleteDirs.IntakeDir,
		s.deleteDirs.IntakeProcessingDir,
		s.deleteDirs.IntakeFailedDir,
	}

	return isSafeManagedPath(path, allowedRoots)
}

type ContainerHandler struct {
	store *ContainerStore
}

func NewContainerHandler(store *ContainerStore) *ContainerHandler {
	return &ContainerHandler{store: store}
}

func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	includeArchived := r.URL.Query().Get("include_archived") == "true"
	containers, err := h.store.List(ctx, includeArchived)
	if err != nil {
		respond.Error(w, http.StatusInternalServerError, "failed to list containers")
		return
	}

	respond.JSON(w, http.StatusOK, containers)
}

func (h *ContainerHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateContainerRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, "invalid JSON request body")
		return
	}

	normalizeCreateContainerRequest(&req)
	if err := validateCreateContainerRequest(req); err != nil {
		respond.Error(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	container, err := h.store.Create(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, errContainerTypeIDInvalid):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		case errors.Is(err, errContainerTypeNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", err.Error())
			return
		case errors.Is(err, errContainerTypeArchived):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign archived container type.")
			return
		case errors.Is(err, errLocationIDInvalid):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		case errors.Is(err, errLocationNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", err.Error())
			return
		case errors.Is(err, errLocationArchived):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign archived location.")
			return
		case errors.Is(err, errContainerNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.Error(w, http.StatusInternalServerError, "failed to create container")
		return
	}

	respond.JSON(w, http.StatusCreated, container)
}

func (h *ContainerHandler) Update(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container id must be a valid UUID")
		return
	}

	req, err := decodeUpdateContainerRequest(w, r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	if err := validateUpdateContainerRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	container, err := h.store.Update(ctx, containerID, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			return
		case errors.Is(err, errContainerTypeIDInvalid):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		case errors.Is(err, errContainerTypeNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", err.Error())
			return
		case errors.Is(err, errContainerTypeArchived):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign archived container type.")
			return
		case errors.Is(err, errLocationIDInvalid):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		case errors.Is(err, errLocationNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", err.Error())
			return
		case errors.Is(err, errLocationArchived):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign archived location.")
			return
		case errors.Is(err, errContainerNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update container")
		return
	}

	respond.JSON(w, http.StatusOK, container)
}

func (h *ContainerHandler) Summary(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(chi.URLParam(r, "id"))
	if containerID == "" {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container id is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	summary, err := h.store.Summary(ctx, containerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load container summary")
		return
	}

	respond.JSON(w, http.StatusOK, summary)
}

func (h *ContainerHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := h.store.DeletePreview(ctx, containerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load container delete preview")
		return
	}

	respond.JSON(w, http.StatusOK, preview)
}

func (h *ContainerHandler) Delete(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	response, err := h.store.Delete(ctx, containerID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete container")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func normalizeCreateContainerRequest(req *models.CreateContainerRequest) {
	req.Name = strings.TrimSpace(req.Name)
	req.Type = trimOptionalString(req.Type)
	req.ContainerTypeID = trimOptionalString(req.ContainerTypeID)
	req.LocationID = trimOptionalString(req.LocationID)
	req.LocationDescription = trimOptionalString(req.LocationDescription)
	req.Notes = trimOptionalString(req.Notes)
}

func decodeUpdateContainerRequest(w http.ResponseWriter, r *http.Request) (models.UpdateContainerRequest, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return models.UpdateContainerRequest{}, errors.New("invalid JSON request body")
	}

	var req models.UpdateContainerRequest
	for key, value := range raw {
		switch key {
		case "name":
			req.NameSupplied = true
			var name *string
			if err := json.Unmarshal(value, &name); err != nil {
				return models.UpdateContainerRequest{}, errors.New("name must be a string")
			}
			if name != nil {
				trimmed := strings.TrimSpace(*name)
				req.Name = &trimmed
			}
		case "type":
			req.TypeSupplied = true
			var containerType *string
			if err := json.Unmarshal(value, &containerType); err != nil {
				return models.UpdateContainerRequest{}, errors.New("type must be a string or null")
			}
			req.Type = trimOptionalString(containerType)
		case "container_type_id":
			req.ContainerTypeIDSupplied = true
			var containerTypeID *string
			if err := json.Unmarshal(value, &containerTypeID); err != nil {
				return models.UpdateContainerRequest{}, errors.New("container_type_id must be a string or null")
			}
			req.ContainerTypeID = trimOptionalString(containerTypeID)
		case "location_id":
			req.LocationIDSupplied = true
			var locationID *string
			if err := json.Unmarshal(value, &locationID); err != nil {
				return models.UpdateContainerRequest{}, errors.New("location_id must be a string or null")
			}
			req.LocationID = trimOptionalString(locationID)
		case "location_description":
			req.LocationDescriptionSupplied = true
			var location *string
			if err := json.Unmarshal(value, &location); err != nil {
				return models.UpdateContainerRequest{}, errors.New("location_description must be a string or null")
			}
			req.LocationDescription = trimOptionalString(location)
		case "notes":
			req.NotesSupplied = true
			var notes *string
			if err := json.Unmarshal(value, &notes); err != nil {
				return models.UpdateContainerRequest{}, errors.New("notes must be a string or null")
			}
			req.Notes = trimOptionalString(notes)
		case "archived":
			req.ArchivedSupplied = true
			var archived *bool
			if err := json.Unmarshal(value, &archived); err != nil {
				return models.UpdateContainerRequest{}, errors.New("archived must be a boolean")
			}
			if archived == nil {
				return models.UpdateContainerRequest{}, errors.New("archived must be a boolean")
			}
			req.Archived = archived
		default:
			return models.UpdateContainerRequest{}, errors.New("unknown field: " + key)
		}
	}

	return req, nil
}

func validateCreateContainerRequest(req models.CreateContainerRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func validateUpdateContainerRequest(req models.UpdateContainerRequest) error {
	if req.NameSupplied {
		if req.Name == nil || *req.Name == "" {
			return errors.New("name must not be blank")
		}
	}
	return nil
}

func isUUID(value string) bool {
	return uuidPattern.MatchString(value)
}

func trimOptionalString(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}

	return &trimmed
}

func (s *ContainerStore) validateLocation(ctx context.Context, locationID *string) error {
	if locationID == nil {
		return nil
	}
	if !isUUID(*locationID) {
		return errLocationIDInvalid
	}

	var archived bool
	err := s.pool.QueryRow(ctx, `
		SELECT archived
		FROM locations
		WHERE id = $1::uuid
	`, *locationID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errLocationNotFound
		}
		return err
	}
	if archived {
		return errLocationArchived
	}
	return nil
}

func (s *ContainerStore) validateContainerType(ctx context.Context, containerTypeID *string) error {
	if containerTypeID == nil {
		return nil
	}
	if !isUUID(*containerTypeID) {
		return errContainerTypeIDInvalid
	}

	var archived bool
	err := s.pool.QueryRow(ctx, `
		SELECT archived
		FROM container_types
		WHERE id = $1::uuid
	`, *containerTypeID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errContainerTypeNotFound
		}
		return err
	}
	if archived {
		return errContainerTypeArchived
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}
