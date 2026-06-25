package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errSellProviderTypeConflict = errors.New("provider_type already exists")

type SellProviderStore struct {
	pool *pgxpool.Pool
}

type SellAdminHandler struct {
	store *SellProviderStore
}

type SellPublicHandler struct {
	store *SellProviderStore
}

type storedSellProviderConfig struct {
	ID               string
	ProviderType     string
	DisplayName      string
	Enabled          bool
	SortOrder        int
	IconKey          string
	BaseURL          *string
	SellerProfileURL *string
	Notes            *string
	CreatedDatetime  time.Time
	UpdatedDatetime  *time.Time
}

type normalizedSellProviderInput struct {
	ProviderType     string
	DisplayName      string
	Enabled          bool
	SortOrder        int
	IconKey          string
	BaseURL          *string
	SellerProfileURL *string
	Notes            *string
}

func NewSellProviderStore(pool *pgxpool.Pool) *SellProviderStore {
	return &SellProviderStore{pool: pool}
}

func NewSellAdminHandler(store *SellProviderStore) *SellAdminHandler {
	return &SellAdminHandler{store: store}
}

func NewSellPublicHandler(store *SellProviderStore) *SellPublicHandler {
	return &SellPublicHandler{store: store}
}

func (s *SellProviderStore) List(ctx context.Context) ([]models.SellProviderConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes,
			created_datetime,
			updated_datetime
		FROM sell_provider_configs
		ORDER BY sort_order ASC, display_name ASC, created_datetime ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]models.SellProviderConfig, 0)
	for rows.Next() {
		record, err := scanStoredSellProviderConfig(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, record.toPublic())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return providers, nil
}

func (s *SellProviderStore) Get(ctx context.Context, id string) (models.SellProviderConfig, error) {
	record, err := s.getStoredByID(ctx, s.pool, id)
	if err != nil {
		return models.SellProviderConfig{}, err
	}
	return record.toPublic(), nil
}

func (s *SellProviderStore) Create(ctx context.Context, req models.CreateSellProviderRequest) (models.SellProviderConfig, error) {
	input, err := normalizeCreateSellProviderRequest(req)
	if err != nil {
		return models.SellProviderConfig{}, err
	}

	var record storedSellProviderConfig
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sell_provider_configs (
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes,
			created_datetime,
			updated_datetime
	`,
		input.ProviderType,
		input.DisplayName,
		input.Enabled,
		input.SortOrder,
		input.IconKey,
		nullableStringArg(input.BaseURL),
		nullableStringArg(input.SellerProfileURL),
		nullableStringArg(input.Notes),
	).Scan(
		&record.ID,
		&record.ProviderType,
		&record.DisplayName,
		&record.Enabled,
		&record.SortOrder,
		&record.IconKey,
		&record.BaseURL,
		&record.SellerProfileURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	if err != nil {
		if isSellProviderTypeConflict(err) {
			return models.SellProviderConfig{}, errSellProviderTypeConflict
		}
		return models.SellProviderConfig{}, err
	}

	return record.toPublic(), nil
}

func (s *SellProviderStore) Patch(ctx context.Context, id string, req models.PatchSellProviderRequest) (models.SellProviderConfig, error) {
	if !sellProviderPatchRequested(req) {
		return models.SellProviderConfig{}, errors.New("at least one provider field is required")
	}

	current, err := s.getStoredByID(ctx, s.pool, id)
	if err != nil {
		return models.SellProviderConfig{}, err
	}

	updated, err := applySellProviderPatch(current, req)
	if err != nil {
		return models.SellProviderConfig{}, err
	}

	var record storedSellProviderConfig
	err = s.pool.QueryRow(ctx, `
		UPDATE sell_provider_configs
		SET
			display_name = $2,
			enabled = $3,
			sort_order = $4,
			icon_key = $5,
			base_url = $6,
			seller_profile_url = $7,
			notes = $8,
			updated_datetime = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes,
			created_datetime,
			updated_datetime
	`,
		id,
		updated.DisplayName,
		updated.Enabled,
		updated.SortOrder,
		updated.IconKey,
		nullableStringArg(updated.BaseURL),
		nullableStringArg(updated.SellerProfileURL),
		nullableStringArg(updated.Notes),
	).Scan(
		&record.ID,
		&record.ProviderType,
		&record.DisplayName,
		&record.Enabled,
		&record.SortOrder,
		&record.IconKey,
		&record.BaseURL,
		&record.SellerProfileURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	if err != nil {
		return models.SellProviderConfig{}, err
	}

	return record.toPublic(), nil
}

func (s *SellProviderStore) SetEnabled(ctx context.Context, id string, enabled bool) (models.SellProviderConfig, error) {
	var record storedSellProviderConfig
	err := s.pool.QueryRow(ctx, `
		UPDATE sell_provider_configs
		SET
			enabled = $2,
			updated_datetime = now()
		WHERE id = $1::uuid
		RETURNING
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes,
			created_datetime,
			updated_datetime
	`, id, enabled).Scan(
		&record.ID,
		&record.ProviderType,
		&record.DisplayName,
		&record.Enabled,
		&record.SortOrder,
		&record.IconKey,
		&record.BaseURL,
		&record.SellerProfileURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	if err != nil {
		return models.SellProviderConfig{}, err
	}

	return record.toPublic(), nil
}

func (s *SellProviderStore) Delete(ctx context.Context, id string) error {
	commandTag, err := s.pool.Exec(ctx, `DELETE FROM sell_provider_configs WHERE id = $1::uuid`, id)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *SellProviderStore) getStoredByID(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, id string) (storedSellProviderConfig, error) {
	var record storedSellProviderConfig
	err := q.QueryRow(ctx, `
		SELECT
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url,
			notes,
			created_datetime,
			updated_datetime
		FROM sell_provider_configs
		WHERE id = $1::uuid
	`, id).Scan(
		&record.ID,
		&record.ProviderType,
		&record.DisplayName,
		&record.Enabled,
		&record.SortOrder,
		&record.IconKey,
		&record.BaseURL,
		&record.SellerProfileURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	return record, err
}

func scanStoredSellProviderConfig(row interface {
	Scan(...any) error
}) (storedSellProviderConfig, error) {
	var record storedSellProviderConfig
	err := row.Scan(
		&record.ID,
		&record.ProviderType,
		&record.DisplayName,
		&record.Enabled,
		&record.SortOrder,
		&record.IconKey,
		&record.BaseURL,
		&record.SellerProfileURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	return record, err
}

func (r storedSellProviderConfig) toPublic() models.SellProviderConfig {
	return models.SellProviderConfig{
		ID:               r.ID,
		ProviderType:     r.ProviderType,
		DisplayName:      r.DisplayName,
		Enabled:          r.Enabled,
		SortOrder:        r.SortOrder,
		IconKey:          r.IconKey,
		BaseURL:          r.BaseURL,
		SellerProfileURL: r.SellerProfileURL,
		Notes:            r.Notes,
		CreatedDatetime:  r.CreatedDatetime,
		UpdatedDatetime:  r.UpdatedDatetime,
	}
}

func normalizeCreateSellProviderRequest(req models.CreateSellProviderRequest) (normalizedSellProviderInput, error) {
	providerType := normalizeSellProviderType(req.ProviderType)
	if providerType == "" {
		return normalizedSellProviderInput{}, errors.New("provider_type is required")
	}
	if !isSupportedSellProviderType(providerType) {
		return normalizedSellProviderInput{}, errors.New("provider_type must be one of facebook_marketplace, ebay, craigslist, etsy")
	}

	displayName := strings.TrimSpace(req.DisplayName)
	if displayName == "" {
		return normalizedSellProviderInput{}, errors.New("display_name is required")
	}

	iconKey := strings.TrimSpace(req.IconKey)
	if iconKey == "" {
		return normalizedSellProviderInput{}, errors.New("icon_key is required")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	sortOrder := 100
	if req.SortOrder != nil {
		sortOrder = *req.SortOrder
	}
	if sortOrder < 0 {
		return normalizedSellProviderInput{}, errors.New("sort_order must be greater than or equal to 0")
	}

	baseURL, err := normalizeOptionalHTTPURL(req.BaseURL, "base_url")
	if err != nil {
		return normalizedSellProviderInput{}, err
	}
	sellerProfileURL, err := normalizeOptionalHTTPURL(req.SellerProfileURL, "seller_profile_url")
	if err != nil {
		return normalizedSellProviderInput{}, err
	}

	return normalizedSellProviderInput{
		ProviderType:     providerType,
		DisplayName:      displayName,
		Enabled:          enabled,
		SortOrder:        sortOrder,
		IconKey:          iconKey,
		BaseURL:          baseURL,
		SellerProfileURL: sellerProfileURL,
		Notes:            normalizeOptionalString(req.Notes),
	}, nil
}

func applySellProviderPatch(current storedSellProviderConfig, req models.PatchSellProviderRequest) (normalizedSellProviderInput, error) {
	updated := normalizedSellProviderInput{
		ProviderType:     current.ProviderType,
		DisplayName:      current.DisplayName,
		Enabled:          current.Enabled,
		SortOrder:        current.SortOrder,
		IconKey:          current.IconKey,
		BaseURL:          current.BaseURL,
		SellerProfileURL: current.SellerProfileURL,
		Notes:            current.Notes,
	}

	if req.DisplayName.Set {
		if req.DisplayName.Value == nil || strings.TrimSpace(*req.DisplayName.Value) == "" {
			return normalizedSellProviderInput{}, errors.New("display_name is required")
		}
		updated.DisplayName = strings.TrimSpace(*req.DisplayName.Value)
	}
	if req.Enabled.Set {
		if req.Enabled.Value == nil {
			return normalizedSellProviderInput{}, errors.New("enabled cannot be null")
		}
		updated.Enabled = *req.Enabled.Value
	}
	if req.SortOrder.Set {
		if req.SortOrder.Value == nil {
			return normalizedSellProviderInput{}, errors.New("sort_order cannot be null")
		}
		if *req.SortOrder.Value < 0 {
			return normalizedSellProviderInput{}, errors.New("sort_order must be greater than or equal to 0")
		}
		updated.SortOrder = *req.SortOrder.Value
	}
	if req.IconKey.Set {
		if req.IconKey.Value == nil || strings.TrimSpace(*req.IconKey.Value) == "" {
			return normalizedSellProviderInput{}, errors.New("icon_key is required")
		}
		updated.IconKey = strings.TrimSpace(*req.IconKey.Value)
	}
	if req.BaseURL.Set {
		baseURL, err := normalizeOptionalHTTPURL(req.BaseURL.Value, "base_url")
		if err != nil {
			return normalizedSellProviderInput{}, err
		}
		updated.BaseURL = baseURL
	}
	if req.SellerProfileURL.Set {
		sellerProfileURL, err := normalizeOptionalHTTPURL(req.SellerProfileURL.Value, "seller_profile_url")
		if err != nil {
			return normalizedSellProviderInput{}, err
		}
		updated.SellerProfileURL = sellerProfileURL
	}
	if req.Notes.Set {
		updated.Notes = normalizeOptionalString(req.Notes.Value)
	}

	return updated, nil
}

func normalizeSellProviderType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedSellProviderType(value string) bool {
	switch value {
	case "facebook_marketplace", "ebay", "craigslist", "etsy":
		return true
	default:
		return false
	}
}

func normalizeOptionalHTTPURL(value *string, field string) (*string, error) {
	normalized := normalizeOptionalString(value)
	if normalized == nil {
		return nil, nil
	}

	parsed, err := url.Parse(*normalized)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New(field + " must be a valid absolute URL")
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		return normalized, nil
	default:
		return nil, errors.New(field + " must use http or https")
	}
}

func sellProviderPatchRequested(req models.PatchSellProviderRequest) bool {
	return req.DisplayName.Set ||
		req.Enabled.Set ||
		req.SortOrder.Set ||
		req.IconKey.Set ||
		req.BaseURL.Set ||
		req.SellerProfileURL.Set ||
		req.Notes.Set
}

func isSellProviderTypeConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (h *SellAdminHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	providers, err := h.store.List(ctx)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load sell providers")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListSellProvidersResponse{Providers: providers})
}

func (h *SellAdminHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req models.CreateSellProviderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Create(ctx, req)
	if err != nil {
		h.respondSellProviderError(w, err)
		return
	}

	respond.JSON(w, http.StatusCreated, models.GetSellProviderResponse{Provider: provider})
}

func (h *SellAdminHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "sell provider was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load sell provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetSellProviderResponse{Provider: provider})
}

func (h *SellAdminHandler) PatchProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	var req models.PatchSellProviderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Patch(ctx, id, req)
	if err != nil {
		h.respondSellProviderError(w, err)
		return
	}

	respond.JSON(w, http.StatusOK, models.GetSellProviderResponse{Provider: provider})
}

func (h *SellAdminHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.store.Delete(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "sell provider was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete sell provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.DeleteSellProviderResponse{ProviderID: id, Deleted: true})
}

func (h *SellAdminHandler) EnableProvider(w http.ResponseWriter, r *http.Request) {
	h.setProviderEnabled(w, r, true)
}

func (h *SellAdminHandler) DisableProvider(w http.ResponseWriter, r *http.Request) {
	h.setProviderEnabled(w, r, false)
}

func (h *SellAdminHandler) setProviderEnabled(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.SetEnabled(ctx, id, enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "sell provider was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update sell provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetSellProviderResponse{Provider: provider})
}

func (h *SellAdminHandler) respondSellProviderError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "sell provider was not found")
	case errors.Is(err, errSellProviderTypeConflict):
		respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
	default:
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
	}
}
