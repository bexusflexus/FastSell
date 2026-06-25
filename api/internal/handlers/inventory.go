package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/jackc/pgx/v5/pgxpool"
)

const looseInventoryBucketID = "loose"

type InventoryStore struct {
	pool *pgxpool.Pool
}

type InventoryHandler struct {
	store *InventoryStore
}

type listInventoryContainersOptions struct {
	DispositionCode string
	AIEnriched      *bool
	MissingApprox   *bool
	Archived        *bool
	IncludeArchived bool
}

func NewInventoryStore(pool *pgxpool.Pool) *InventoryStore {
	return &InventoryStore{pool: pool}
}

func NewInventoryHandler(store *InventoryStore) *InventoryHandler {
	return &InventoryHandler{store: store}
}

func (s *InventoryStore) ListContainerSummaries(ctx context.Context, opts listInventoryContainersOptions) ([]models.InventoryContainerSummary, error) {
	rows, err := s.pool.Query(ctx, `
		WITH container_summaries AS (
			SELECT
				c.id::text AS container_id,
				c.name AS container_name,
				false AS is_synthetic,
				c.type AS container_type,
				c.container_type_id::text AS container_type_id,
				ct.name AS container_type_name,
				c.location_id::text AS location_id,
				l.name AS location_name,
				c.location_description,
				c.notes,
				COALESCE(displayed.item_count, 0) AS item_count,
				COALESCE(aggregates.active_item_count, 0) AS active_item_count,
				COALESCE(aggregates.archived_item_count, 0) AS archived_item_count,
				COALESCE(aggregates.for_sale_count, 0) AS for_sale_count,
				COALESCE(aggregates.in_use_count, 0) AS in_use_count,
				COALESCE(aggregates.sale_pending_count, 0) AS sale_pending_count,
				COALESCE(aggregates.sold_count, 0) AS sold_count,
				COALESCE(aggregates.donated_count, 0) AS donated_count,
				COALESCE(aggregates.disposed_count, 0) AS disposed_count,
				COALESCE(aggregates.total_approx_value, 0)::float8 AS total_approx_value,
				displayed.latest_item_datetime
			FROM containers c
			LEFT JOIN container_types ct ON ct.id = c.container_type_id
			LEFT JOIN locations l ON l.id = c.location_id
			LEFT JOIN LATERAL (
				SELECT
					count(*) FILTER (WHERE i.archived = false)::int AS active_item_count,
					count(*) FILTER (WHERE i.archived = true)::int AS archived_item_count,
					count(*) FILTER (WHERE i.disposition_code = 'for_sale')::int AS for_sale_count,
					count(*) FILTER (WHERE i.disposition_code = 'in_use')::int AS in_use_count,
					count(*) FILTER (WHERE i.disposition_code = 'sale_pending')::int AS sale_pending_count,
					count(*) FILTER (WHERE i.disposition_code = 'sold')::int AS sold_count,
					count(*) FILTER (WHERE i.disposition_code = 'donated')::int AS donated_count,
					count(*) FILTER (WHERE i.disposition_code = 'disposed')::int AS disposed_count,
					COALESCE(sum(i.approx_value) FILTER (WHERE i.archived = false), 0) AS total_approx_value
				FROM items i
				WHERE i.container_id = c.id
					AND i.current_inventory = true
					AND ($1 = '' OR i.disposition_code = $1)
					AND ($6::boolean = false OR i.ai_enriched = $7)
					AND ($8::boolean = false OR ($9::boolean = true AND i.approx_value IS NULL) OR ($9::boolean = false AND i.approx_value IS NOT NULL))
			) aggregates ON true
			LEFT JOIN LATERAL (
				SELECT
					count(*)::int AS item_count,
					max(i.created_datetime) AS latest_item_datetime
				FROM items i
				WHERE i.container_id = c.id
					AND i.current_inventory = true
					AND ($1 = '' OR i.disposition_code = $1)
					AND ($6::boolean = false OR i.ai_enriched = $7)
					AND ($8::boolean = false OR ($9::boolean = true AND i.approx_value IS NULL) OR ($9::boolean = false AND i.approx_value IS NOT NULL))
					AND (
						($2::boolean = true AND i.archived = $3)
						OR ($2::boolean = false AND ($4::boolean = true OR i.archived = false))
					)
			) displayed ON true
		),
		loose_summary AS (
			SELECT
				$5::text AS container_id,
				'No Container / Loose Items'::text AS container_name,
				true AS is_synthetic,
				NULL::text AS container_type,
				NULL::text AS container_type_id,
				NULL::text AS container_type_name,
				NULL::text AS location_id,
				NULL::text AS location_name,
				NULL::text AS location_description,
				NULL::text AS notes,
				count(*)::int AS item_count,
				count(*) FILTER (WHERE i.archived = false)::int AS active_item_count,
				count(*) FILTER (WHERE i.archived = true)::int AS archived_item_count,
				count(*) FILTER (WHERE i.disposition_code = 'for_sale')::int AS for_sale_count,
				count(*) FILTER (WHERE i.disposition_code = 'in_use')::int AS in_use_count,
				count(*) FILTER (WHERE i.disposition_code = 'sale_pending')::int AS sale_pending_count,
				count(*) FILTER (WHERE i.disposition_code = 'sold')::int AS sold_count,
				count(*) FILTER (WHERE i.disposition_code = 'donated')::int AS donated_count,
				count(*) FILTER (WHERE i.disposition_code = 'disposed')::int AS disposed_count,
				COALESCE(sum(i.approx_value) FILTER (WHERE i.archived = false), 0)::float8 AS total_approx_value,
				max(i.created_datetime) AS latest_item_datetime
			FROM items i
			WHERE i.container_id IS NULL
				AND i.current_inventory = true
				AND ($1 = '' OR i.disposition_code = $1)
				AND ($6::boolean = false OR i.ai_enriched = $7)
				AND ($8::boolean = false OR ($9::boolean = true AND i.approx_value IS NULL) OR ($9::boolean = false AND i.approx_value IS NOT NULL))
				AND (
					($2::boolean = true AND i.archived = $3)
					OR ($2::boolean = false AND ($4::boolean = true OR i.archived = false))
				)
		)
		SELECT *
		FROM (
			SELECT *
			FROM container_summaries
			UNION ALL
			SELECT *
			FROM loose_summary
			WHERE item_count > 0
		) AS summaries
		ORDER BY is_synthetic ASC, COALESCE(location_name, location_description, '') ASC, container_name ASC
	`,
		opts.DispositionCode,
		opts.Archived != nil,
		boolValue(opts.Archived),
		opts.IncludeArchived,
		looseInventoryBucketID,
		opts.AIEnriched != nil,
		boolValue(opts.AIEnriched),
		opts.MissingApprox != nil,
		boolValue(opts.MissingApprox),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]models.InventoryContainerSummary, 0)
	for rows.Next() {
		var summary models.InventoryContainerSummary
		if err := rows.Scan(
			&summary.ContainerID,
			&summary.ContainerName,
			&summary.IsSynthetic,
			&summary.ContainerType,
			&summary.ContainerTypeID,
			&summary.ContainerTypeName,
			&summary.LocationID,
			&summary.LocationName,
			&summary.LocationDescription,
			&summary.Notes,
			&summary.ItemCount,
			&summary.ActiveItemCount,
			&summary.ArchivedItemCount,
			&summary.ForSaleCount,
			&summary.InUseCount,
			&summary.SalePendingCount,
			&summary.SoldCount,
			&summary.DonatedCount,
			&summary.DisposedCount,
			&summary.TotalApproxValue,
			&summary.LatestItemDatetime,
		); err != nil {
			return nil, err
		}
		summaries = append(summaries, summary)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return summaries, nil
}

func (h *InventoryHandler) ListContainers(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListInventoryContainersOptions(r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.ListContainerSummaries(ctx, opts)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load inventory container summaries")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func parseListInventoryContainersOptions(r *http.Request) (listInventoryContainersOptions, error) {
	query := r.URL.Query()

	dispositionCode := strings.TrimSpace(query.Get("disposition_code"))
	if strings.EqualFold(dispositionCode, "all") {
		dispositionCode = ""
	}
	aiEnrichedFilter, err := parseOptionalBool(query.Get("ai_enriched"), "ai_enriched")
	if err != nil {
		return listInventoryContainersOptions{}, err
	}
	missingApproxFilter, err := parseOptionalBool(query.Get("missing_approx_value"), "missing_approx_value")
	if err != nil {
		return listInventoryContainersOptions{}, err
	}

	archivedFilter, includeArchived, err := parseArchivedFilters(query.Get("archived"), query.Get("include_archived"))
	if err != nil {
		return listInventoryContainersOptions{}, err
	}

	return listInventoryContainersOptions{
		DispositionCode: dispositionCode,
		AIEnriched:      aiEnrichedFilter,
		MissingApprox:   missingApproxFilter,
		Archived:        archivedFilter,
		IncludeArchived: includeArchived,
	}, nil
}
