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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var errActiveListingDraftConflict = errors.New("active listing draft already exists")

type ListingDraftStore struct {
	pool         *pgxpool.Pool
	exportConfig ListingPhotoExportConfig
}

type ListingDraftHandler struct {
	store *ListingDraftStore
}

type listingDraftProvider struct {
	ID               string
	ProviderType     string
	DisplayName      string
	Enabled          bool
	SortOrder        int
	IconKey          string
	BaseURL          *string
	SellerProfileURL *string
}

type listingDraftItemSeed struct {
	ID              string
	Title           *string
	Description     *string
	ApproxValue     *string
	DispositionCode *string
	ContainerID     *string
	ContainerName   *string
}

type storedListingDraft struct {
	ID                   string
	ItemID               string
	SellProviderConfigID *string
	ProviderType         string
	ProviderDisplayName  *string
	ProviderIconKey      *string
	Status               string
	Title                string
	Description          *string
	AskingPrice          *string
	Currency             string
	ListingURL           *string
	Notes                *string
	CreatedDatetime      time.Time
	UpdatedDatetime      *time.Time
	ItemTitle            *string
	ItemApproxValue      *string
	ItemDispositionCode  *string
	ItemContainerID      *string
	ItemContainerName    *string
}

type normalizedListingDraftCreateInput struct {
	ProviderID   *string
	ProviderType *string
}

type normalizedListingDraftPatch struct {
	Title       *string
	Description *string
	AskingPrice *string
	Currency    *string
	Status      *string
	ListingURL  *string
	Notes       *string
}

func NewListingDraftStore(pool *pgxpool.Pool, exportConfig ListingPhotoExportConfig) *ListingDraftStore {
	return &ListingDraftStore{
		pool:         pool,
		exportConfig: exportConfig,
	}
}

func NewListingDraftHandler(store *ListingDraftStore) *ListingDraftHandler {
	return &ListingDraftHandler{store: store}
}

func (s *SellProviderStore) ListEnabledPublic(ctx context.Context) ([]models.PublicSellProvider, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url
		FROM sell_provider_configs
		WHERE enabled = true
		ORDER BY sort_order ASC, display_name ASC, created_datetime ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]models.PublicSellProvider, 0)
	for rows.Next() {
		var provider models.PublicSellProvider
		if err := rows.Scan(
			&provider.ID,
			&provider.ProviderType,
			&provider.DisplayName,
			&provider.Enabled,
			&provider.SortOrder,
			&provider.IconKey,
			&provider.BaseURL,
			&provider.SellerProfileURL,
		); err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return providers, nil
}

func (h *SellPublicHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	providers, err := h.store.ListEnabledPublic(ctx)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load sell providers")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListPublicSellProvidersResponse{Providers: providers})
}

func (s *ListingDraftStore) ListByItem(ctx context.Context, itemID string) ([]models.ListingDraft, error) {
	if _, err := s.loadItemSeed(ctx, itemID); err != nil {
		return nil, err
	}

	rows, err := s.pool.Query(ctx, listingDraftSelectQuery(`
		WHERE ld.item_id = $1::uuid
		ORDER BY ld.created_datetime DESC, ld.id DESC
	`), itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	drafts := make([]models.ListingDraft, 0)
	for rows.Next() {
		record, err := scanStoredListingDraft(rows)
		if err != nil {
			return nil, err
		}
		drafts = append(drafts, record.toPublic(true))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return drafts, nil
}

func (s *ListingDraftStore) Get(ctx context.Context, draftID string) (models.ListingDraft, error) {
	record, err := s.getStoredByID(ctx, draftID)
	if err != nil {
		return models.ListingDraft{}, err
	}
	return record.toPublic(true), nil
}

func (s *ListingDraftStore) GetOrCreate(ctx context.Context, itemID string, req models.CreateListingDraftRequest) (models.ListingDraft, error) {
	input, err := normalizeListingDraftCreateRequest(req)
	if err != nil {
		return models.ListingDraft{}, err
	}

	itemSeed, err := s.loadItemSeed(ctx, itemID)
	if err != nil {
		return models.ListingDraft{}, err
	}

	provider, err := s.resolveEnabledProvider(ctx, input)
	if err != nil {
		return models.ListingDraft{}, err
	}

	existing, err := s.getActiveByItemProvider(ctx, itemID, provider.ProviderType)
	if err == nil {
		return existing.toPublic(true), nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return models.ListingDraft{}, err
	}

	title := normalizeSeedDraftTitle(itemSeed.Title)
	var draftID string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO sale_listing_drafts (
			item_id,
			sell_provider_config_id,
			provider_type,
			status,
			title,
			description,
			asking_price,
			currency
		)
		VALUES ($1::uuid, $2::uuid, $3, 'draft', $4, $5, CASE WHEN $6::text IS NULL THEN NULL ELSE $6::numeric(12,2) END, 'USD')
		RETURNING id::text
	`,
		itemID,
		provider.ID,
		provider.ProviderType,
		title,
		nullableStringArg(itemSeed.Description),
		nullableStringArg(itemSeed.ApproxValue),
	).Scan(&draftID)
	if err != nil {
		if isActiveListingDraftConflict(err) {
			existing, existingErr := s.getActiveByItemProvider(ctx, itemID, provider.ProviderType)
			if existingErr != nil {
				return models.ListingDraft{}, existingErr
			}
			return existing.toPublic(true), nil
		}
		return models.ListingDraft{}, err
	}

	record, err := s.getStoredByID(ctx, draftID)
	if err != nil {
		return models.ListingDraft{}, err
	}
	return record.toPublic(true), nil
}

func (s *ListingDraftStore) Patch(ctx context.Context, draftID string, req models.PatchListingDraftRequest) (models.ListingDraft, error) {
	if !listingDraftPatchRequested(req) {
		return models.ListingDraft{}, errors.New("at least one draft field is required")
	}

	current, err := s.getStoredByID(ctx, draftID)
	if err != nil {
		return models.ListingDraft{}, err
	}

	updated, err := applyListingDraftPatch(current, req)
	if err != nil {
		return models.ListingDraft{}, err
	}

	commandTag, err := s.pool.Exec(ctx, `
		UPDATE sale_listing_drafts
		SET
			title = $2,
			description = $3,
			asking_price = CASE WHEN $4::text IS NULL THEN NULL ELSE $4::numeric(12,2) END,
			currency = $5,
			status = $6,
			listing_url = $7,
			notes = $8,
			updated_datetime = now()
		WHERE id = $1::uuid
	`,
		draftID,
		updated.Title,
		nullableStringArg(updated.Description),
		nullableStringArg(updated.AskingPrice),
		updated.Currency,
		updated.Status,
		nullableStringArg(updated.ListingURL),
		nullableStringArg(updated.Notes),
	)
	if err != nil {
		return models.ListingDraft{}, err
	}
	if commandTag.RowsAffected() == 0 {
		return models.ListingDraft{}, pgx.ErrNoRows
	}

	record, err := s.getStoredByID(ctx, draftID)
	if err != nil {
		return models.ListingDraft{}, err
	}
	return record.toPublic(true), nil
}

func (s *ListingDraftStore) Delete(ctx context.Context, draftID string) error {
	commandTag, err := s.pool.Exec(ctx, `DELETE FROM sale_listing_drafts WHERE id = $1::uuid`, draftID)
	if err != nil {
		return err
	}
	if commandTag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *ListingDraftStore) loadItemSeed(ctx context.Context, itemID string) (listingDraftItemSeed, error) {
	var seed listingDraftItemSeed
	err := s.pool.QueryRow(ctx, `
		SELECT
			i.id::text,
			i.title,
			i.description,
			i.approx_value::text,
			i.disposition_code,
			c.id::text,
			c.name
		FROM items i
		LEFT JOIN containers c ON c.id = i.container_id
		WHERE i.id = $1::uuid
	`, itemID).Scan(
		&seed.ID,
		&seed.Title,
		&seed.Description,
		&seed.ApproxValue,
		&seed.DispositionCode,
		&seed.ContainerID,
		&seed.ContainerName,
	)
	return seed, err
}

func (s *ListingDraftStore) resolveEnabledProvider(ctx context.Context, input normalizedListingDraftCreateInput) (listingDraftProvider, error) {
	if input.ProviderID != nil {
		provider, err := s.getProviderByID(ctx, *input.ProviderID)
		if err != nil {
			return listingDraftProvider{}, err
		}
		if !provider.Enabled {
			return listingDraftProvider{}, errors.New("provider must be enabled")
		}
		if input.ProviderType != nil && provider.ProviderType != *input.ProviderType {
			return listingDraftProvider{}, errors.New("provider_type must match sell_provider_config_id")
		}
		return provider, nil
	}

	if input.ProviderType == nil {
		return listingDraftProvider{}, errors.New("provider selection is required")
	}

	provider, err := s.getEnabledProviderByType(ctx, *input.ProviderType)
	if err != nil {
		return listingDraftProvider{}, err
	}
	return provider, nil
}

func (s *ListingDraftStore) getProviderByID(ctx context.Context, providerID string) (listingDraftProvider, error) {
	var provider listingDraftProvider
	err := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url
		FROM sell_provider_configs
		WHERE id = $1::uuid
	`, providerID).Scan(
		&provider.ID,
		&provider.ProviderType,
		&provider.DisplayName,
		&provider.Enabled,
		&provider.SortOrder,
		&provider.IconKey,
		&provider.BaseURL,
		&provider.SellerProfileURL,
	)
	return provider, err
}

func (s *ListingDraftStore) getEnabledProviderByType(ctx context.Context, providerType string) (listingDraftProvider, error) {
	var provider listingDraftProvider
	err := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			provider_type,
			display_name,
			enabled,
			sort_order,
			icon_key,
			base_url,
			seller_profile_url
		FROM sell_provider_configs
		WHERE provider_type = $1
			AND enabled = true
		ORDER BY sort_order ASC, display_name ASC, created_datetime ASC
		LIMIT 1
	`, providerType).Scan(
		&provider.ID,
		&provider.ProviderType,
		&provider.DisplayName,
		&provider.Enabled,
		&provider.SortOrder,
		&provider.IconKey,
		&provider.BaseURL,
		&provider.SellerProfileURL,
	)
	return provider, err
}

func (s *ListingDraftStore) getActiveByItemProvider(ctx context.Context, itemID string, providerType string) (storedListingDraft, error) {
	row := s.pool.QueryRow(ctx, listingDraftSelectQuery(`
		WHERE ld.item_id = $1::uuid
			AND ld.provider_type = $2
			AND ld.status IN ('draft', 'ready')
		ORDER BY ld.created_datetime DESC, ld.id DESC
		LIMIT 1
	`), itemID, providerType)
	return scanStoredListingDraft(row)
}

func (s *ListingDraftStore) getStoredByID(ctx context.Context, draftID string) (storedListingDraft, error) {
	row := s.pool.QueryRow(ctx, listingDraftSelectQuery(`
		WHERE ld.id = $1::uuid
	`), draftID)
	return scanStoredListingDraft(row)
}

func scanStoredListingDraft(row interface {
	Scan(...any) error
}) (storedListingDraft, error) {
	var record storedListingDraft
	err := row.Scan(
		&record.ID,
		&record.ItemID,
		&record.SellProviderConfigID,
		&record.ProviderType,
		&record.ProviderDisplayName,
		&record.ProviderIconKey,
		&record.Status,
		&record.Title,
		&record.Description,
		&record.AskingPrice,
		&record.Currency,
		&record.ListingURL,
		&record.Notes,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
		&record.ItemTitle,
		&record.ItemApproxValue,
		&record.ItemDispositionCode,
		&record.ItemContainerID,
		&record.ItemContainerName,
	)
	return record, err
}

func (r storedListingDraft) toPublic(includeItem bool) models.ListingDraft {
	draft := models.ListingDraft{
		ID:                   r.ID,
		ItemID:               r.ItemID,
		SellProviderConfigID: r.SellProviderConfigID,
		ProviderType:         r.ProviderType,
		ProviderDisplayName:  r.ProviderDisplayName,
		ProviderIconKey:      r.ProviderIconKey,
		Status:               r.Status,
		Title:                r.Title,
		Description:          r.Description,
		AskingPrice:          r.AskingPrice,
		Currency:             r.Currency,
		ListingURL:           r.ListingURL,
		Notes:                r.Notes,
		CreatedDatetime:      r.CreatedDatetime,
		UpdatedDatetime:      r.UpdatedDatetime,
	}
	if includeItem {
		draft.Item = &models.ListingDraftItemSummary{
			ID:              r.ItemID,
			Title:           r.ItemTitle,
			ApproxValue:     r.ItemApproxValue,
			DispositionCode: r.ItemDispositionCode,
			ContainerID:     r.ItemContainerID,
			ContainerName:   r.ItemContainerName,
		}
	}
	return draft
}

func listingDraftSelectQuery(suffix string) string {
	return `
		SELECT
			ld.id::text,
			ld.item_id::text,
			ld.sell_provider_config_id::text,
			ld.provider_type,
			spc.display_name,
			spc.icon_key,
			ld.status,
			ld.title,
			ld.description,
			ld.asking_price::text,
			ld.currency,
			ld.listing_url,
			ld.notes,
			ld.created_datetime,
			ld.updated_datetime,
			i.title,
			i.approx_value::text,
			i.disposition_code,
			c.id::text,
			c.name
		FROM sale_listing_drafts ld
		LEFT JOIN sell_provider_configs spc ON spc.id = ld.sell_provider_config_id
		JOIN items i ON i.id = ld.item_id
		LEFT JOIN containers c ON c.id = i.container_id
	` + suffix
}

func normalizeSeedDraftTitle(title *string) string {
	if title == nil || strings.TrimSpace(*title) == "" {
		return "Untitled item"
	}
	return strings.TrimSpace(*title)
}

func normalizeListingDraftCreateRequest(req models.CreateListingDraftRequest) (normalizedListingDraftCreateInput, error) {
	input := normalizedListingDraftCreateInput{}
	if req.SellProviderConfigID != nil {
		value := strings.TrimSpace(*req.SellProviderConfigID)
		if value == "" {
			return normalizedListingDraftCreateInput{}, errors.New("sell_provider_config_id must not be blank")
		}
		input.ProviderID = &value
	}
	if req.ProviderType != nil {
		value := normalizeSellProviderType(*req.ProviderType)
		if value == "" {
			return normalizedListingDraftCreateInput{}, errors.New("provider_type must not be blank")
		}
		if !isSupportedSellProviderType(value) {
			return normalizedListingDraftCreateInput{}, errors.New("provider_type must be one of facebook_marketplace, ebay, craigslist, etsy")
		}
		input.ProviderType = &value
	}
	if input.ProviderID == nil && input.ProviderType == nil {
		return normalizedListingDraftCreateInput{}, errors.New("sell_provider_config_id or provider_type is required")
	}
	return input, nil
}

func applyListingDraftPatch(current storedListingDraft, req models.PatchListingDraftRequest) (normalizedListingDraftPatch, error) {
	updated := normalizedListingDraftPatch{
		Title:       &current.Title,
		Description: current.Description,
		AskingPrice: current.AskingPrice,
		Currency:    &current.Currency,
		Status:      &current.Status,
		ListingURL:  current.ListingURL,
		Notes:       current.Notes,
	}

	if req.Title.Set {
		title, err := normalizeRequiredOptionalString(req.Title, "title")
		if err != nil {
			return normalizedListingDraftPatch{}, err
		}
		if title == nil {
			return normalizedListingDraftPatch{}, errors.New("title is required")
		}
		updated.Title = title
	}
	if req.Description.Set {
		updated.Description = normalizeNullableOptionalString(req.Description)
	}
	if req.AskingPrice.Set {
		askingPrice, err := normalizeMoneyOptionalString(req.AskingPrice, "asking_price")
		if err != nil {
			return normalizedListingDraftPatch{}, err
		}
		updated.AskingPrice = askingPrice
	}
	if req.Currency.Set {
		currency, err := normalizeRequiredOptionalString(req.Currency, "currency")
		if err != nil {
			return normalizedListingDraftPatch{}, err
		}
		normalized := strings.ToUpper(strings.TrimSpace(*currency))
		updated.Currency = &normalized
	}
	if req.Status.Set {
		status, err := normalizeRequiredOptionalString(req.Status, "status")
		if err != nil {
			return normalizedListingDraftPatch{}, err
		}
		normalized := strings.ToLower(strings.TrimSpace(*status))
		if !isSupportedListingDraftStatus(normalized) {
			return normalizedListingDraftPatch{}, errors.New("status must be one of draft, ready, listed, archived")
		}
		updated.Status = &normalized
	}
	if req.ListingURL.Set {
		listingURL, err := normalizeOptionalHTTPURL(req.ListingURL.Value, "listing_url")
		if err != nil {
			return normalizedListingDraftPatch{}, err
		}
		updated.ListingURL = listingURL
	}
	if req.Notes.Set {
		updated.Notes = normalizeOptionalString(req.Notes.Value)
	}

	return updated, nil
}

func listingDraftPatchRequested(req models.PatchListingDraftRequest) bool {
	return req.Title.Set ||
		req.Description.Set ||
		req.AskingPrice.Set ||
		req.Currency.Set ||
		req.Status.Set ||
		req.ListingURL.Set ||
		req.Notes.Set
}

func isSupportedListingDraftStatus(value string) bool {
	switch value {
	case "draft", "ready", "listed", "archived":
		return true
	default:
		return false
	}
}

func isActiveListingDraftConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (h *ListingDraftHandler) ListByItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	drafts, err := h.store.ListByItem(ctx, itemID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load listing drafts")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListListingDraftsResponse{Drafts: drafts})
}

func (h *ListingDraftHandler) CreateForItem(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")

	var req models.CreateListingDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	draft, err := h.store.GetOrCreate(ctx, itemID, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item or provider was not found")
		default:
			status := http.StatusBadRequest
			code := "validation_failed"
			message := err.Error()
			if errors.Is(err, errActiveListingDraftConflict) {
				message = "listing draft already exists"
			}
			if strings.Contains(message, "provider was not found") || strings.Contains(message, "item was not found") {
				status = http.StatusNotFound
				code = "not_found"
			}
			if isPgInvalidTextRepresentation(err) {
				status = http.StatusBadRequest
				code = "validation_failed"
				message = "provider id must be a valid UUID"
			}
			if message == "" {
				message = "failed to create listing draft"
			}
			respond.ErrorCode(w, status, code, message)
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetListingDraftResponse{Draft: draft})
}

func (h *ListingDraftHandler) Get(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	draft, err := h.store.Get(ctx, draftID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "listing draft was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load listing draft")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetListingDraftResponse{Draft: draft})
}

func (h *ListingDraftHandler) PreparePhotos(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	draft, err := h.store.PreparePhotoExport(ctx, draftID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "listing draft was not found")
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "photo_export_failed", "failed to prepare listing photo export")
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetListingDraftResponse{Draft: draft})
}

func (h *ListingDraftHandler) Patch(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")

	var req models.PatchListingDraftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	draft, err := h.store.Patch(ctx, draftID, req)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "listing draft was not found")
			return
		}
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	respond.JSON(w, http.StatusOK, models.GetListingDraftResponse{Draft: draft})
}

func (h *ListingDraftHandler) Delete(w http.ResponseWriter, r *http.Request) {
	draftID := chi.URLParam(r, "id")
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.store.Delete(ctx, draftID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "listing draft was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete listing draft")
		return
	}

	respond.JSON(w, http.StatusOK, models.DeleteListingDraftResponse{
		DraftID: draftID,
		Deleted: true,
	})
}

func isPgInvalidTextRepresentation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "22P02"
}
