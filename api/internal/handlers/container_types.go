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
	errContainerTypeNameConflict = errors.New("container type name already exists")
	errContainerTypeInUse        = errors.New("container type is referenced by one or more containers")
)

type ContainerTypeStore struct {
	pool *pgxpool.Pool
}

type containerTypeDeleteState struct {
	id         string
	name       string
	usageCount int
}

type ContainerTypeHandler struct {
	store *ContainerTypeStore
}

func NewContainerTypeStore(pool *pgxpool.Pool) *ContainerTypeStore {
	return &ContainerTypeStore{pool: pool}
}

func NewContainerTypeHandler(store *ContainerTypeStore) *ContainerTypeHandler {
	return &ContainerTypeHandler{store: store}
}

func (s *ContainerTypeStore) List(ctx context.Context, includeArchived bool) ([]models.ContainerType, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
		FROM container_types
		WHERE ($1::boolean = true OR archived = false)
		ORDER BY archived ASC, name ASC, created_datetime ASC
	`, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	containerTypes := make([]models.ContainerType, 0)
	for rows.Next() {
		var containerType models.ContainerType
		if err := rows.Scan(
			&containerType.ID,
			&containerType.Name,
			&containerType.Description,
			&containerType.Archived,
			&containerType.ArchivedDatetime,
			&containerType.CreatedDatetime,
			&containerType.UpdatedDatetime,
		); err != nil {
			return nil, err
		}
		containerTypes = append(containerTypes, containerType)
	}

	return containerTypes, rows.Err()
}

func (s *ContainerTypeStore) Get(ctx context.Context, id string) (models.ContainerType, error) {
	var containerType models.ContainerType
	err := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			name,
			description,
			archived,
			archived_datetime,
			created_datetime,
			updated_datetime
		FROM container_types
		WHERE id = $1::uuid
	`, id).Scan(
		&containerType.ID,
		&containerType.Name,
		&containerType.Description,
		&containerType.Archived,
		&containerType.ArchivedDatetime,
		&containerType.CreatedDatetime,
		&containerType.UpdatedDatetime,
	)
	if err != nil {
		return models.ContainerType{}, err
	}
	return containerType, nil
}

func (s *ContainerTypeStore) Create(ctx context.Context, req models.CreateContainerTypeRequest) (models.ContainerType, error) {
	var containerType models.ContainerType
	err := s.pool.QueryRow(ctx, `
		INSERT INTO container_types (name, description, archived, archived_datetime)
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
		&containerType.ID,
		&containerType.Name,
		&containerType.Description,
		&containerType.Archived,
		&containerType.ArchivedDatetime,
		&containerType.CreatedDatetime,
		&containerType.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.ContainerType{}, errContainerTypeNameConflict
		}
		return models.ContainerType{}, err
	}
	return containerType, nil
}

func (s *ContainerTypeStore) Patch(ctx context.Context, id string, req models.UpdateContainerTypeRequest) (models.ContainerType, error) {
	var archivedValue bool
	if req.Archived != nil {
		archivedValue = *req.Archived
	}

	var containerType models.ContainerType
	err := s.pool.QueryRow(ctx, `
		UPDATE container_types
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
		&containerType.ID,
		&containerType.Name,
		&containerType.Description,
		&containerType.Archived,
		&containerType.ArchivedDatetime,
		&containerType.CreatedDatetime,
		&containerType.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.ContainerType{}, errContainerTypeNameConflict
		}
		return models.ContainerType{}, err
	}
	return containerType, nil
}

func (s *ContainerTypeStore) Delete(ctx context.Context, id string) error {
	_, err := s.DeleteWithPreview(ctx, id)
	return err
}

func (s *ContainerTypeStore) DeletePreview(ctx context.Context, id string) (models.ContainerTypeDeletePreview, error) {
	state, err := s.loadDeleteState(ctx, id)
	if err != nil {
		return models.ContainerTypeDeletePreview{}, err
	}

	return containerTypeDeletePreviewFromState(state), nil
}

func (s *ContainerTypeStore) DeleteWithPreview(ctx context.Context, id string) (models.DeleteContainerTypeResponse, error) {
	state, err := s.loadDeleteState(ctx, id)
	if err != nil {
		return models.DeleteContainerTypeResponse{}, err
	}
	if state.usageCount > 0 {
		return models.DeleteContainerTypeResponse{}, errContainerTypeInUse
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM container_types WHERE id = $1::uuid`, id)
	if err != nil {
		if isForeignKeyViolation(err) {
			return models.DeleteContainerTypeResponse{}, errContainerTypeInUse
		}
		return models.DeleteContainerTypeResponse{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.DeleteContainerTypeResponse{}, pgx.ErrNoRows
	}

	return models.DeleteContainerTypeResponse{
		ID:         state.id,
		Name:       state.name,
		Deleted:    true,
		UsageCount: state.usageCount,
	}, nil
}

func (s *ContainerTypeStore) loadDeleteState(ctx context.Context, id string) (containerTypeDeleteState, error) {
	var state containerTypeDeleteState
	err := s.pool.QueryRow(ctx, `
		SELECT
			ct.id::text,
			ct.name,
			(SELECT count(*)::int FROM containers c WHERE c.container_type_id = ct.id) AS usage_count
		FROM container_types ct
		WHERE ct.id = $1::uuid
	`, id).Scan(&state.id, &state.name, &state.usageCount)
	return state, err
}

func containerTypeDeletePreviewFromState(state containerTypeDeleteState) models.ContainerTypeDeletePreview {
	var blockingReason *string
	canDelete := state.usageCount == 0
	if !canDelete {
		message := "Container type is in use by one or more containers and cannot be deleted."
		blockingReason = &message
	}

	return models.ContainerTypeDeletePreview{
		ID:             state.id,
		Name:           state.name,
		CanDelete:      canDelete,
		UsageCount:     state.usageCount,
		BlockingReason: blockingReason,
	}
}

func (h *ContainerTypeHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	containerTypes, err := h.store.List(ctx, includeArchived)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load container types")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListContainerTypesResponse{ContainerTypes: containerTypes})
}

func (h *ContainerTypeHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container type id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containerType, err := h.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container type was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load container type")
		return
	}

	respond.JSON(w, http.StatusOK, containerType)
}

func (h *ContainerTypeHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateContainerTypeRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	normalizeCreateContainerTypeRequest(&req)
	if err := validateCreateContainerTypeRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containerType, err := h.store.Create(ctx, req)
	if err != nil {
		if errors.Is(err, errContainerTypeNameConflict) {
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to create container type")
		return
	}

	respond.JSON(w, http.StatusCreated, containerType)
}

func (h *ContainerTypeHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container type id must be a valid UUID")
		return
	}

	req, err := decodeUpdateContainerTypeRequest(w, r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := validateUpdateContainerTypeRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containerType, err := h.store.Patch(ctx, id, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container type was not found")
			return
		case errors.Is(err, errContainerTypeNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update container type")
			return
		}
	}

	respond.JSON(w, http.StatusOK, containerType)
}

func (h *ContainerTypeHandler) Archive(w http.ResponseWriter, r *http.Request) {
	h.patchArchiveState(w, r, true)
}

func (h *ContainerTypeHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.patchArchiveState(w, r, false)
}

func (h *ContainerTypeHandler) patchArchiveState(w http.ResponseWriter, r *http.Request, archived bool) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container type id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	containerType, err := h.store.Patch(ctx, id, models.UpdateContainerTypeRequest{
		ArchivedSupplied: true,
		Archived:         &archived,
	})
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container type was not found")
			return
		case errors.Is(err, errContainerTypeNameConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update container type archive state")
			return
		}
	}

	respond.JSON(w, http.StatusOK, containerType)
}

func (h *ContainerTypeHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container type id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	response, err := h.store.DeleteWithPreview(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container type was not found")
			return
		case errors.Is(err, errContainerTypeInUse):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete container type")
			return
		}
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *ContainerTypeHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container type id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	preview, err := h.store.DeletePreview(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container type was not found")
			return
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load container type delete preview")
			return
		}
	}

	respond.JSON(w, http.StatusOK, preview)
}

func normalizeCreateContainerTypeRequest(req *models.CreateContainerTypeRequest) {
	req.Name = strings.TrimSpace(req.Name)
	req.Description = trimOptionalString(req.Description)
}

func decodeUpdateContainerTypeRequest(w http.ResponseWriter, r *http.Request) (models.UpdateContainerTypeRequest, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return models.UpdateContainerTypeRequest{}, errors.New("invalid JSON request body")
	}

	var req models.UpdateContainerTypeRequest
	for key, value := range raw {
		switch key {
		case "name":
			req.NameSupplied = true
			var name *string
			if err := json.Unmarshal(value, &name); err != nil {
				return models.UpdateContainerTypeRequest{}, errors.New("name must be a string")
			}
			if name != nil {
				trimmed := strings.TrimSpace(*name)
				req.Name = &trimmed
			}
		case "description":
			req.DescriptionSupplied = true
			var description *string
			if err := json.Unmarshal(value, &description); err != nil {
				return models.UpdateContainerTypeRequest{}, errors.New("description must be a string or null")
			}
			req.Description = trimOptionalString(description)
		case "archived":
			req.ArchivedSupplied = true
			var archived *bool
			if err := json.Unmarshal(value, &archived); err != nil {
				return models.UpdateContainerTypeRequest{}, errors.New("archived must be a boolean")
			}
			if archived == nil {
				return models.UpdateContainerTypeRequest{}, errors.New("archived must be a boolean")
			}
			req.Archived = archived
		default:
			return models.UpdateContainerTypeRequest{}, errors.New("unknown field: " + key)
		}
	}

	return req, nil
}

func validateCreateContainerTypeRequest(req models.CreateContainerTypeRequest) error {
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func validateUpdateContainerTypeRequest(req models.UpdateContainerTypeRequest) error {
	if !req.NameSupplied && !req.DescriptionSupplied && !req.ArchivedSupplied {
		return errors.New("at least one container type field is required")
	}
	if req.NameSupplied && (req.Name == nil || *req.Name == "") {
		return errors.New("name must not be blank")
	}
	return nil
}
