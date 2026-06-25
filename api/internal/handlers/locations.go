package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	errLocationNameConflict = errors.New("location name already exists")
	errLocationInUse        = errors.New("location is referenced by one or more containers")
)

type LocationStore struct {
	pool *pgxpool.Pool
}

type locationDeleteState struct {
	id         string
	name       string
	usageCount int
}

type LocationHandler struct {
	store *LocationStore
}

func NewLocationStore(pool *pgxpool.Pool) *LocationStore {
	return &LocationStore{pool: pool}
}

func NewLocationHandler(store *LocationStore) *LocationHandler {
	return &LocationHandler{store: store}
}

func (s *LocationStore) List(ctx context.Context, includeArchived bool) ([]models.Location, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
		FROM locations
		WHERE ($1::boolean = true OR archived = false)
		ORDER BY archived ASC, name ASC, created_datetime ASC
	`, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	locations := make([]models.Location, 0)
	for rows.Next() {
		var location models.Location
		if err := rows.Scan(
			&location.ID,
			&location.Name,
			&location.Description,
			&location.Archived,
			&location.ArchivedDatetime,
			&location.CreatedDatetime,
			&location.UpdatedDatetime,
		); err != nil {
			return nil, err
		}
		locations = append(locations, location)
	}

	return locations, rows.Err()
}

func (s *LocationStore) Get(ctx context.Context, id string) (models.Location, error) {
	var location models.Location
	err := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
		FROM locations
		WHERE id = $1::uuid
	`, id).Scan(
		&location.ID,
		&location.Name,
		&location.Description,
		&location.Archived,
		&location.ArchivedDatetime,
		&location.CreatedDatetime,
		&location.UpdatedDatetime,
	)
	if err != nil {
		return models.Location{}, err
	}
	return location, nil
}

func (s *LocationStore) Create(ctx context.Context, req models.CreateLocationRequest) (models.Location, error) {
	var location models.Location
	err := s.pool.QueryRow(ctx, `
		INSERT INTO locations (name, description, archived, archived_datetime)
		VALUES ($1, $2, false, NULL)
		RETURNING
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
	`, req.Name, nullableStringArg(req.Description)).Scan(
		&location.ID,
		&location.Name,
		&location.Description,
		&location.Archived,
		&location.ArchivedDatetime,
		&location.CreatedDatetime,
		&location.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Location{}, errLocationNameConflict
		}
		return models.Location{}, err
	}
	return location, nil
}

func (s *LocationStore) Patch(ctx context.Context, id string, req models.UpdateLocationRequest) (models.Location, error) {
	var archivedValue bool
	if req.Archived != nil {
		archivedValue = *req.Archived
	}

	var location models.Location
	err := s.pool.QueryRow(ctx, `
		UPDATE locations
		SET
			name = CASE WHEN $2 THEN $3 ELSE name END,
			description = CASE WHEN $4 THEN $5 ELSE description END,
			archived = CASE WHEN $6 THEN $7 ELSE archived END,
			archived_datetime = CASE
				WHEN $6 AND $7 AND archived = false THEN now()
				WHEN $6 AND $7 AND archived = true THEN archived_datetime
				WHEN $6 AND $7 = false THEN NULL
				ELSE archived_datetime
			END,
			updated_datetime = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
	`,
		id,
		req.NameSupplied,
		req.Name,
		req.DescriptionSupplied,
		nullableStringArg(req.Description),
		req.ArchivedSupplied,
		archivedValue,
	).Scan(
		&location.ID,
		&location.Name,
		&location.Description,
		&location.Archived,
		&location.ArchivedDatetime,
		&location.CreatedDatetime,
		&location.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.Location{}, errLocationNameConflict
		}
		return models.Location{}, err
	}
	return location, nil
}

func (s *LocationStore) Delete(ctx context.Context, id string) error {
	_, err := s.DeleteWithPreview(ctx, id)
	return err
}

func (s *LocationStore) DeletePreview(ctx context.Context, id string) (models.LocationDeletePreview, error) {
	state, err := s.loadDeleteState(ctx, id)
	if err != nil {
		return models.LocationDeletePreview{}, err
	}

	return locationDeletePreviewFromState(state), nil
}

func (s *LocationStore) DeleteWithPreview(ctx context.Context, id string) (models.DeleteLocationResponse, error) {
	state, err := s.loadDeleteState(ctx, id)
	if err != nil {
		return models.DeleteLocationResponse{}, err
	}
	if state.usageCount > 0 {
		return models.DeleteLocationResponse{}, errLocationInUse
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM locations WHERE id = $1::uuid`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return models.DeleteLocationResponse{}, errLocationInUse
		}
		return models.DeleteLocationResponse{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.DeleteLocationResponse{}, pgx.ErrNoRows
	}

	return models.DeleteLocationResponse{
		ID:         state.id,
		Name:       state.name,
		Deleted:    true,
		UsageCount: state.usageCount,
	}, nil
}

func (s *LocationStore) loadDeleteState(ctx context.Context, id string) (locationDeleteState, error) {
	var state locationDeleteState
	err := s.pool.QueryRow(ctx, `
		SELECT
			l.id::text,
			l.name,
			(SELECT count(*)::int FROM containers c WHERE c.location_id = l.id) AS usage_count
		FROM locations l
		WHERE l.id = $1::uuid
	`, id).Scan(&state.id, &state.name, &state.usageCount)
	return state, err
}

func locationDeletePreviewFromState(state locationDeleteState) models.LocationDeletePreview {
	var blockingReason *string
	canDelete := state.usageCount == 0
	if !canDelete {
		message := "Location is in use by one or more containers and cannot be deleted."
		blockingReason = &message
	}

	return models.LocationDeletePreview{
		ID:             state.id,
		Name:           state.name,
		CanDelete:      canDelete,
		UsageCount:     state.usageCount,
		BlockingReason: blockingReason,
	}
}

func (h *LocationHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	locations, err := h.store.List(ctx, includeArchived)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load locations")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListLocationsResponse{Locations: locations})
}

func (h *LocationHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "location id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	location, err := h.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load location")
		return
	}

	respond.JSON(w, http.StatusOK, location)
}

func (h *LocationHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateLocationRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	normalizeCreateLocationRequest(&req)
	if err := validateCreateLocationRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	location, err := h.store.Create(ctx, req)
	if err != nil {
		if errors.Is(err, errLocationNameConflict) {
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to create location")
		return
	}

	respond.JSON(w, http.StatusCreated, location)
}

func (h *LocationHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "location id must be a valid UUID")
		return
	}

	req, err := decodeUpdateLocationRequest(w, r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := validateUpdateLocationRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	location, err := h.store.Patch(ctx, id, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			return
		case errors.Is(err, errLocationNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update location")
			return
		}
	}

	respond.JSON(w, http.StatusOK, location)
}

func (h *LocationHandler) Archive(w http.ResponseWriter, r *http.Request) {
	h.patchArchiveState(w, r, true)
}

func (h *LocationHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.patchArchiveState(w, r, false)
}

func (h *LocationHandler) patchArchiveState(w http.ResponseWriter, r *http.Request, archived bool) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "location id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	location, err := h.store.Patch(ctx, id, models.UpdateLocationRequest{
		ArchivedSupplied: true,
		Archived:         &archived,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			return
		case errors.Is(err, errLocationNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update location archive state")
			return
		}
	}

	respond.JSON(w, http.StatusOK, location)
}

func (h *LocationHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "location id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response, err := h.store.DeleteWithPreview(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			return
		case errors.Is(err, errLocationInUse):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete location")
			return
		}
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *LocationHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "location id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := h.store.DeletePreview(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load location delete preview")
			return
		}
	}

	respond.JSON(w, http.StatusOK, preview)
}

func normalizeCreateLocationRequest(req *models.CreateLocationRequest) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = trimOptionalString(req.Description)
}

func decodeUpdateLocationRequest(w http.ResponseWriter, r *http.Request) (models.UpdateLocationRequest, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return models.UpdateLocationRequest{}, errors.New("invalid JSON request body")
	}

	var req models.UpdateLocationRequest
	for key, value := range raw {
		switch key {
		case "name":
			req.NameSupplied = true
			var name *string
			if err := json.Unmarshal(value, &name); err != nil {
				return models.UpdateLocationRequest{}, errors.New("name must be a string")
			}
			if name != nil {
				trimmed := strings.TrimSpace(*name)
				req.Name = &trimmed
			}
		case "description":
			req.DescriptionSupplied = true
			var description *string
			if err := json.Unmarshal(value, &description); err != nil {
				return models.UpdateLocationRequest{}, errors.New("description must be a string or null")
			}
			req.Description = trimOptionalString(description)
		case "archived":
			req.ArchivedSupplied = true
			var archived *bool
			if err := json.Unmarshal(value, &archived); err != nil {
				return models.UpdateLocationRequest{}, errors.New("archived must be a boolean")
			}
			if archived == nil {
				return models.UpdateLocationRequest{}, errors.New("archived must be a boolean")
			}
			req.Archived = archived
		default:
			return models.UpdateLocationRequest{}, errors.New("unknown field: " + key)
		}
	}

	return req, nil
}

func validateCreateLocationRequest(req models.CreateLocationRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func validateUpdateLocationRequest(req models.UpdateLocationRequest) error {
	if !req.NameSupplied && !req.DescriptionSupplied && !req.ArchivedSupplied {
		return errors.New("at least one location field is required")
	}
	if req.NameSupplied && (req.Name == nil || *req.Name == "") {
		return errors.New("name must not be blank")
	}
	return nil
}
