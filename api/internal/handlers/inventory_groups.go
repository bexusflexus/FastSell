package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultInventoryGroupCode = "household_items"

var (
	inventoryGroupCodePattern        = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	errInventoryGroupConflict        = errors.New("inventory group code already exists")
	errInventoryGroupInUse           = errors.New("inventory group is referenced by inventory data")
	errDefaultInventoryGroupModified = errors.New("default inventory group code cannot be changed, archived, or deleted")
)

type InventoryGroupStore struct {
	pool *pgxpool.Pool
}

type InventoryGroupHandler struct {
	store *InventoryGroupStore
}

func NewInventoryGroupStore(pool *pgxpool.Pool) *InventoryGroupStore {
	return &InventoryGroupStore{pool: pool}
}

func NewInventoryGroupHandler(store *InventoryGroupStore) *InventoryGroupHandler {
	return &InventoryGroupHandler{store: store}
}

func (s *InventoryGroupStore) List(ctx context.Context, includeArchived bool) ([]models.InventoryGroup, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id::text, code, name, description, archived, created_datetime, updated_datetime
		FROM inventory_groups
		WHERE ($1::boolean = true OR archived = false)
		ORDER BY archived ASC, name ASC, code ASC
	`, includeArchived)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]models.InventoryGroup, 0)
	for rows.Next() {
		var group models.InventoryGroup
		if err := rows.Scan(
			&group.ID,
			&group.Code,
			&group.Name,
			&group.Description,
			&group.Archived,
			&group.CreatedDatetime,
			&group.UpdatedDatetime,
		); err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (s *InventoryGroupStore) Get(ctx context.Context, id string) (models.InventoryGroup, error) {
	var group models.InventoryGroup
	err := s.pool.QueryRow(ctx, `
		SELECT id::text, code, name, description, archived, created_datetime, updated_datetime
		FROM inventory_groups
		WHERE id = $1::uuid
	`, id).Scan(
		&group.ID,
		&group.Code,
		&group.Name,
		&group.Description,
		&group.Archived,
		&group.CreatedDatetime,
		&group.UpdatedDatetime,
	)
	return group, err
}

func (s *InventoryGroupStore) DefaultID(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `
		SELECT id::text
		FROM inventory_groups
		WHERE code = $1
	`, defaultInventoryGroupCode).Scan(&id)
	return id, err
}

func (s *InventoryGroupStore) Create(ctx context.Context, req models.CreateInventoryGroupRequest) (models.InventoryGroup, error) {
	var group models.InventoryGroup
	err := s.pool.QueryRow(ctx, `
		INSERT INTO inventory_groups (code, name, description, archived)
		VALUES ($1, $2, $3, false)
		RETURNING id::text, code, name, description, archived, created_datetime, updated_datetime
	`, req.Code, req.Name, strings.TrimSpace(derefString(req.Description))).Scan(
		&group.ID,
		&group.Code,
		&group.Name,
		&group.Description,
		&group.Archived,
		&group.CreatedDatetime,
		&group.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.InventoryGroup{}, errInventoryGroupConflict
		}
		return models.InventoryGroup{}, err
	}
	return group, nil
}

func (s *InventoryGroupStore) Patch(ctx context.Context, id string, req models.UpdateInventoryGroupRequest) (models.InventoryGroup, error) {
	if req.CodeSupplied || req.ArchivedSupplied {
		current, err := s.Get(ctx, id)
		if err != nil {
			return models.InventoryGroup{}, err
		}
		if current.Code == defaultInventoryGroupCode {
			if req.CodeSupplied && req.Code != nil && *req.Code != defaultInventoryGroupCode {
				return models.InventoryGroup{}, errDefaultInventoryGroupModified
			}
			if req.ArchivedSupplied && req.Archived != nil && *req.Archived {
				return models.InventoryGroup{}, errDefaultInventoryGroupModified
			}
		}
	}

	var archivedValue bool
	if req.Archived != nil {
		archivedValue = *req.Archived
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}

	var group models.InventoryGroup
	err := s.pool.QueryRow(ctx, `
		UPDATE inventory_groups
		SET
			code = CASE WHEN $2 THEN $3 ELSE code END,
			name = CASE WHEN $4 THEN $5 ELSE name END,
			description = CASE WHEN $6 THEN $7 ELSE description END,
			archived = CASE WHEN $8 THEN $9 ELSE archived END,
			updated_datetime = now()
		WHERE id = $1::uuid
		RETURNING id::text, code, name, description, archived, created_datetime, updated_datetime
	`,
		id,
		req.CodeSupplied,
		req.Code,
		req.NameSupplied,
		req.Name,
		req.DescriptionSupplied,
		description,
		req.ArchivedSupplied,
		archivedValue,
	).Scan(
		&group.ID,
		&group.Code,
		&group.Name,
		&group.Description,
		&group.Archived,
		&group.CreatedDatetime,
		&group.UpdatedDatetime,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return models.InventoryGroup{}, errInventoryGroupConflict
		}
		return models.InventoryGroup{}, err
	}
	return group, nil
}

func (s *InventoryGroupStore) Delete(ctx context.Context, id string) (models.InventoryGroupReferenceCounts, error) {
	group, err := s.Get(ctx, id)
	if err != nil {
		return models.InventoryGroupReferenceCounts{}, err
	}
	if group.Code == defaultInventoryGroupCode {
		return models.InventoryGroupReferenceCounts{}, errDefaultInventoryGroupModified
	}

	counts, err := s.ReferenceCounts(ctx, id)
	if err != nil {
		return models.InventoryGroupReferenceCounts{}, err
	}
	if counts.Items > 0 || counts.UploadGroups > 0 {
		return counts, errInventoryGroupInUse
	}

	tag, err := s.pool.Exec(ctx, `DELETE FROM inventory_groups WHERE id = $1::uuid`, id)
	if err != nil {
		return counts, err
	}
	if tag.RowsAffected() == 0 {
		return counts, pgx.ErrNoRows
	}
	return counts, nil
}

func (s *InventoryGroupStore) ReferenceCounts(ctx context.Context, id string) (models.InventoryGroupReferenceCounts, error) {
	var counts models.InventoryGroupReferenceCounts
	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*)::int FROM items WHERE inventory_group_id = $1::uuid),
			(SELECT count(*)::int FROM upload_groups WHERE inventory_group_id = $1::uuid)
	`, id).Scan(&counts.Items, &counts.UploadGroups)
	return counts, err
}

func (h *InventoryGroupHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	includeArchived := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("include_archived")), "true")
	groups, err := h.store.List(ctx, includeArchived)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load inventory groups")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListInventoryGroupsResponse{InventoryGroups: groups})
}

func (h *InventoryGroupHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateInventoryGroupRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	normalizeCreateInventoryGroupRequest(&req)
	if err := validateCreateInventoryGroupRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	group, err := h.store.Create(ctx, req)
	if err != nil {
		if errors.Is(err, errInventoryGroupConflict) {
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to create inventory group")
		return
	}

	respond.JSON(w, http.StatusCreated, group)
}

func (h *InventoryGroupHandler) Patch(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "inventory group id must be a valid UUID")
		return
	}

	req, err := decodeUpdateInventoryGroupRequest(w, r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}
	if err := validateUpdateInventoryGroupRequest(req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	group, err := h.store.Patch(ctx, id, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
		case errors.Is(err, errInventoryGroupConflict):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, errDefaultInventoryGroupModified):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update inventory group")
		}
		return
	}

	respond.JSON(w, http.StatusOK, group)
}

func (h *InventoryGroupHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "inventory group id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	counts, err := h.store.Delete(ctx, id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
		case errors.Is(err, errInventoryGroupInUse):
			respond.JSON(w, http.StatusConflict, models.DeleteInventoryGroupBlockedResponse{
				Message: "inventory group is referenced by inventory data",
				Counts:  counts,
			})
		case errors.Is(err, errDefaultInventoryGroupModified):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete inventory group")
		}
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func normalizeCreateInventoryGroupRequest(req *models.CreateInventoryGroupRequest) {
	req.Code = strings.TrimSpace(req.Code)
	req.Name = strings.TrimSpace(req.Name)
	if req.Description != nil {
		description := strings.TrimSpace(*req.Description)
		req.Description = &description
	}
}

func decodeUpdateInventoryGroupRequest(w http.ResponseWriter, r *http.Request) (models.UpdateInventoryGroupRequest, error) {
	var raw map[string]json.RawMessage
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return models.UpdateInventoryGroupRequest{}, errors.New("invalid JSON request body")
	}

	var req models.UpdateInventoryGroupRequest
	for key, value := range raw {
		switch key {
		case "code":
			req.CodeSupplied = true
			var code *string
			if err := json.Unmarshal(value, &code); err != nil {
				return models.UpdateInventoryGroupRequest{}, errors.New("code must be a string")
			}
			if code != nil {
				trimmed := strings.TrimSpace(*code)
				req.Code = &trimmed
			}
		case "name":
			req.NameSupplied = true
			var name *string
			if err := json.Unmarshal(value, &name); err != nil {
				return models.UpdateInventoryGroupRequest{}, errors.New("name must be a string")
			}
			if name != nil {
				trimmed := strings.TrimSpace(*name)
				req.Name = &trimmed
			}
		case "description":
			req.DescriptionSupplied = true
			var description *string
			if err := json.Unmarshal(value, &description); err != nil {
				return models.UpdateInventoryGroupRequest{}, errors.New("description must be a string or null")
			}
			if description == nil {
				empty := ""
				req.Description = &empty
			} else {
				trimmed := strings.TrimSpace(*description)
				req.Description = &trimmed
			}
		case "archived":
			req.ArchivedSupplied = true
			var archived *bool
			if err := json.Unmarshal(value, &archived); err != nil {
				return models.UpdateInventoryGroupRequest{}, errors.New("archived must be a boolean")
			}
			if archived == nil {
				return models.UpdateInventoryGroupRequest{}, errors.New("archived must be a boolean")
			}
			req.Archived = archived
		default:
			return models.UpdateInventoryGroupRequest{}, errors.New("unknown field: " + key)
		}
	}

	return req, nil
}

func validateCreateInventoryGroupRequest(req models.CreateInventoryGroupRequest) error {
	if req.Code == "" {
		return errors.New("code is required")
	}
	if !inventoryGroupCodePattern.MatchString(req.Code) {
		return errors.New("code must be lowercase snake_case")
	}
	if req.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func validateUpdateInventoryGroupRequest(req models.UpdateInventoryGroupRequest) error {
	if !req.CodeSupplied && !req.NameSupplied && !req.DescriptionSupplied && !req.ArchivedSupplied {
		return errors.New("at least one inventory group field is required")
	}
	if req.CodeSupplied {
		if req.Code == nil || *req.Code == "" {
			return errors.New("code must not be blank")
		}
		if !inventoryGroupCodePattern.MatchString(*req.Code) {
			return errors.New("code must be lowercase snake_case")
		}
	}
	if req.NameSupplied && (req.Name == nil || *req.Name == "") {
		return errors.New("name must not be blank")
	}
	return nil
}
