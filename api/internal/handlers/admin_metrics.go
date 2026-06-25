package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/jackc/pgx/v5/pgxpool"
)

type AdminMetricsStore struct {
	pool *pgxpool.Pool
}

type AdminMetricsHandler struct {
	store *AdminMetricsStore
}

func NewAdminMetricsStore(pool *pgxpool.Pool) *AdminMetricsStore {
	return &AdminMetricsStore{pool: pool}
}

func NewAdminMetricsHandler(store *AdminMetricsStore) *AdminMetricsHandler {
	return &AdminMetricsHandler{store: store}
}

func (s *AdminMetricsStore) Get(ctx context.Context) (models.AdminMetricsResponse, error) {
	hasArchived, err := s.itemArchiveColumnExists(ctx)
	if err != nil {
		return models.AdminMetricsResponse{}, fmt.Errorf("itemArchiveColumnExists: %w", err)
	}

	summary, err := s.loadSummary(ctx, hasArchived)
	if err != nil {
		return models.AdminMetricsResponse{}, fmt.Errorf("loadSummary: %w", err)
	}

	topValueItems, err := s.loadTopValueItems(ctx, hasArchived)
	if err != nil {
		return models.AdminMetricsResponse{}, fmt.Errorf("loadTopValueItems: %w", err)
	}

	duplicateGroups, err := s.loadDuplicateTitleGroups(ctx, hasArchived)
	if err != nil {
		return models.AdminMetricsResponse{}, fmt.Errorf("loadDuplicateTitleGroups: %w", err)
	}

	return models.AdminMetricsResponse{
		Summary:              summary,
		TopValueItems:        topValueItems,
		DuplicateTitleGroups: duplicateGroups,
		GeneratedDatetime:    time.Now().UTC(),
	}, nil
}

func (s *AdminMetricsStore) itemArchiveColumnExists(ctx context.Context) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
				AND c.relname = 'items'
				AND a.attname = 'archived'
				AND NOT a.attisdropped
		)
	`).Scan(&exists)
	return exists, err
}

func (s *AdminMetricsStore) loadSummary(ctx context.Context, hasArchived bool) (models.AdminMetricsSummary, error) {
	currentFilter := currentInventoryFilter("i", hasArchived)
	archivedFilter := archivedInventoryFilter("i", hasArchived)

	query := fmt.Sprintf(`
		SELECT
			count(*) FILTER (WHERE %s)::int,
			COALESCE(sum(i.approx_value) FILTER (WHERE %s), 0)::float8,
			count(*) FILTER (WHERE %s AND i.disposition_code = 'for_sale')::int,
			count(*) FILTER (WHERE %s AND i.disposition_code = 'in_use')::int,
			count(*) FILTER (WHERE %s AND i.disposition_code = 'sale_pending')::int,
			count(*) FILTER (WHERE i.disposition_code = 'sold')::int,
			count(*) FILTER (WHERE i.disposition_code = 'donated')::int,
			count(*) FILTER (WHERE i.disposition_code = 'disposed')::int,
			count(*) FILTER (WHERE %s)::int,
			count(*) FILTER (WHERE %s AND i.ai_enriched = true)::int,
			count(*) FILTER (WHERE %s AND i.approx_value IS NULL)::int
		FROM items i
	`, currentFilter, currentFilter, currentFilter, currentFilter, currentFilter, archivedFilter, currentFilter, currentFilter)

	var summary models.AdminMetricsSummary
	err := s.pool.QueryRow(ctx, query).Scan(
		&summary.TotalCurrentInventoryItems,
		&summary.TotalCurrentApproxValue,
		&summary.ForSaleCount,
		&summary.InUseCount,
		&summary.SalePendingCount,
		&summary.SoldCount,
		&summary.DonatedCount,
		&summary.DisposedCount,
		&summary.ArchivedCount,
		&summary.AIEnrichedCount,
		&summary.MissingApproxValueCount,
	)
	return summary, err
}

func (s *AdminMetricsStore) loadTopValueItems(ctx context.Context, hasArchived bool) ([]models.AdminMetricsTopValueItem, error) {
	currentFilter := currentInventoryFilter("i", hasArchived)
	query := fmt.Sprintf(`
		SELECT
			i.id::text,
			i.title,
			i.approx_value::float8,
			i.disposition_code,
			c.id::text,
			c.name,
			primary_image.id::text
		FROM items i
		LEFT JOIN containers c ON c.id = i.container_id
		LEFT JOIN LATERAL (
			SELECT ia.id
			FROM image_assets ia
			WHERE ia.item_id = i.id
			ORDER BY ia.upload_order ASC, ia.created_datetime ASC
			LIMIT 1
		) primary_image ON true
		WHERE %s
		ORDER BY i.approx_value DESC NULLS LAST, i.created_datetime DESC, i.title ASC NULLS LAST
		LIMIT 10
	`, currentFilter)

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]models.AdminMetricsTopValueItem, 0)
	for rows.Next() {
		var item models.AdminMetricsTopValueItem
		if err := rows.Scan(
			&item.ID,
			&item.Title,
			&item.ApproxValue,
			&item.DispositionCode,
			&item.ContainerID,
			&item.ContainerName,
			&item.PrimaryImageID,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func (s *AdminMetricsStore) loadDuplicateTitleGroups(ctx context.Context, hasArchived bool) ([]models.AdminMetricsDuplicateTitleGroup, error) {
	currentFilter := currentInventoryFilter("i", hasArchived)
	normalizedTitleExpr := normalizedTitleSQL("i")

	query := fmt.Sprintf(`
		WITH candidate_items AS (
			SELECT
				i.id::text AS id,
				i.title,
				i.approx_value::float8 AS approx_value,
				i.disposition_code,
				c.id::text AS container_id,
				c.name AS container_name,
				%s AS normalized_title
			FROM items i
			LEFT JOIN containers c ON c.id = i.container_id
			WHERE %s
				AND btrim(COALESCE(i.title, '')) <> ''
		),
		top_groups AS (
			SELECT
				normalized_title,
				count(*)::int AS item_count,
				COALESCE(sum(approx_value), 0)::float8 AS total_approx_value
			FROM candidate_items
			WHERE normalized_title <> ''
			GROUP BY normalized_title
			HAVING count(*) > 1
			ORDER BY item_count DESC, total_approx_value DESC, normalized_title ASC
			LIMIT 25
		)
		SELECT
			tg.normalized_title,
			tg.item_count,
			tg.total_approx_value,
			COALESCE(
				json_agg(
					json_build_object(
						'id', ci.id,
						'title', ci.title,
						'approx_value', ci.approx_value,
						'disposition_code', ci.disposition_code,
						'container_id', ci.container_id,
						'container_name', ci.container_name
					)
					ORDER BY ci.approx_value DESC NULLS LAST, ci.title ASC NULLS LAST, ci.id ASC
				) FILTER (WHERE ci.id IS NOT NULL),
				'[]'::json
			)
		FROM top_groups tg
		JOIN candidate_items ci ON ci.normalized_title = tg.normalized_title
		GROUP BY tg.normalized_title, tg.item_count, tg.total_approx_value
		ORDER BY tg.item_count DESC, tg.total_approx_value DESC, tg.normalized_title ASC
	`, normalizedTitleExpr, currentFilter)

	rows, err := s.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	groups := make([]models.AdminMetricsDuplicateTitleGroup, 0)
	for rows.Next() {
		var (
			group     models.AdminMetricsDuplicateTitleGroup
			itemsJSON []byte
		)

		if err := rows.Scan(
			&group.NormalizedTitle,
			&group.Count,
			&group.TotalApproxValue,
			&itemsJSON,
		); err != nil {
			return nil, err
		}

		if err := json.Unmarshal(itemsJSON, &group.Items); err != nil {
			return nil, err
		}

		groups = append(groups, group)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return groups, nil
}

func (h *AdminMetricsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	response, err := h.store.Get(ctx)
	if err != nil {
		log.Printf("failed to load admin metrics: %v", err)
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load admin metrics")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func activeInventoryFilter(alias string, hasArchived bool) string {
	if hasArchived {
		return fmt.Sprintf("COALESCE(%s.archived, false) = false", alias)
	}
	return "true"
}

func archivedInventoryFilter(alias string, hasArchived bool) string {
	if hasArchived {
		return fmt.Sprintf("COALESCE(%s.archived, false) = true", alias)
	}
	return "false"
}

func currentInventoryFilter(alias string, hasArchived bool) string {
	return fmt.Sprintf("(%s AND COALESCE(%s.current_inventory, true) = true)", activeInventoryFilter(alias, hasArchived), alias)
}

func normalizedTitleSQL(alias string) string {
	return fmt.Sprintf(
		"btrim(regexp_replace(lower(regexp_replace(COALESCE(%s.title, ''), '\\s+', ' ', 'g')), '\\s+[0-9]+$', '', 'g'))",
		alias,
	)
}
