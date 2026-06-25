package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultItemsLimit = 50
	maxItemsLimit     = 100
	dispositionStored = "stored"
)

var (
	moneyValuePattern     = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)
	errArchivedMoveTarget = errors.New("cannot move item to archived container")
	errArchivedLocation   = errors.New("cannot assign item to archived location")
	errArchivedItemGroup  = errors.New("cannot assign item to archived inventory group")
	errItemGroupNotFound  = errors.New("inventory group was not found")
)

type ItemStore struct {
	pool        *pgxpool.Pool
	files       *ManagedFileService
	imageConfig ItemImageStorageConfig
}

type ItemHandler struct {
	store *ItemStore
}

type listItemsOptions struct {
	Search           string
	SearchPattern    string
	ContainerID      *string
	InventoryGroupID *string
	LooseOnly        bool
	DispositionCode  string
	AIEnriched       *bool
	MissingApprox    *bool
	Archived         *bool
	IncludeArchived  bool
	InventoryState   string
	Limit            int
	Offset           int
	Sort             string
}

type itemQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type itemDeleteRecord struct {
	ID                    string
	Title                 *string
	ContainerID           *string
	ContainerName         *string
	ContainerType         *string
	ContainerTypeID       *string
	ContainerTypeName     *string
	ContainerLocationID   *string
	ContainerLocationName *string
	ContainerLocation     *string
}

type itemDeleteImageRow struct {
	ImageAssetID     string
	UploadGroupID    *string
	SessionID        *string
	FilePath         string
	ThumbnailPath    *string
	NormalizedPath   *string
	OriginalFilename *string
	StoredFilename   *string
}

type itemDeleteContext struct {
	Preview          models.ItemDeletePreview
	FilePaths        []string
	LinkedGroupIDs   []string
	LinkedSessionIDs []string
}

func NewItemStore(pool *pgxpool.Pool, files *ManagedFileService, imageConfig ItemImageStorageConfig) *ItemStore {
	return &ItemStore{pool: pool, files: files, imageConfig: imageConfig}
}

func NewItemHandler(store *ItemStore) *ItemHandler {
	return &ItemHandler{store: store}
}

func (s *ItemStore) List(ctx context.Context, opts listItemsOptions) (models.ListItemsResponse, error) {
	orderClause := buildItemOrderClause(opts.Search != "", opts.Sort)
	query := fmt.Sprintf(`
		WITH ranked_items AS (
			SELECT
				i.id,
				i.title,
				i.description,
				i.approx_value::text,
				i.sold_price::text,
				i.sold_date::text,
				i.notes,
				i.disposition_code,
				d.label AS disposition_label,
				i.current_inventory,
				i.ai_enriched,
				i.archived,
				i.archived_datetime,
				i.created_datetime,
				i.updated_datetime,
				i.inventory_group_id::text AS inventory_group_id,
				ig.code AS inventory_group_code,
				ig.name AS inventory_group_name,
				c.id::text AS container_id,
				c.name AS container_name,
				c.type AS container_type,
				c.container_type_id::text AS container_type_id,
				ct.name AS container_type_name,
				c.location_id::text AS container_location_id,
				l.name AS container_location_name,
				c.location_description AS container_location_description,
				i.location_id::text AS item_location_id,
				il.name AS item_location_name,
				i.location_detail,
				CASE
					WHEN $1 = '' THEN 0::real
					ELSE GREATEST(
						similarity(COALESCE(i.title, ''), $1),
						similarity(COALESCE(i.description, ''), $1),
						similarity(COALESCE(i.notes, ''), $1),
						similarity(COALESCE(c.name, ''), $1),
						similarity(COALESCE(ct.name, ''), $1),
						similarity(COALESCE(ig.name, ''), $1),
						similarity(COALESCE(ig.code, ''), $1),
						similarity(COALESCE(l.name, ''), $1),
						similarity(COALESCE(c.location_description, ''), $1),
						similarity(COALESCE(il.name, ''), $1),
						similarity(COALESCE(i.location_detail, ''), $1)
					)
				END AS search_rank,
				i.approx_value AS approx_value_numeric,
				i.sold_price AS sold_price_numeric
			FROM items i
			LEFT JOIN inventory_groups ig ON ig.id = i.inventory_group_id
			LEFT JOIN containers c ON c.id = i.container_id
			LEFT JOIN container_types ct ON ct.id = c.container_type_id
			LEFT JOIN locations l ON l.id = c.location_id
			LEFT JOIN locations il ON il.id = i.location_id
			LEFT JOIN item_dispositions d ON d.code = i.disposition_code
			WHERE (
					($10::boolean = true AND i.container_id IS NULL)
					OR ($10::boolean = false AND ($2::uuid IS NULL OR i.container_id = $2::uuid))
				)
				AND ($3 = '' OR i.disposition_code = $3)
				AND ($11::boolean = false OR i.ai_enriched = $12)
				AND ($13::boolean = false OR ($14::boolean = true AND i.approx_value IS NULL) OR ($14::boolean = false AND i.approx_value IS NOT NULL))
				AND (
					$15 = 'all'
					OR ($15 = 'current' AND i.current_inventory = true)
					OR ($15 = 'former' AND i.current_inventory = false)
				)
				AND ($16::uuid IS NULL OR i.inventory_group_id = $16::uuid)
				AND (
					($5::boolean = true AND i.archived = $6)
					OR ($5::boolean = false AND ($7::boolean = true OR i.archived = false))
				)
				AND (
					$1 = '' OR
					COALESCE(i.title, '') %% $1 OR
					COALESCE(i.description, '') %% $1 OR
					COALESCE(i.notes, '') %% $1 OR
					COALESCE(c.name, '') %% $1 OR
					COALESCE(ct.name, '') %% $1 OR
					COALESCE(ig.name, '') %% $1 OR
					COALESCE(ig.code, '') %% $1 OR
					COALESCE(l.name, '') %% $1 OR
					COALESCE(c.location_description, '') %% $1 OR
					COALESCE(il.name, '') %% $1 OR
					COALESCE(i.location_detail, '') %% $1 OR
					COALESCE(i.title, '') ILIKE $4 OR
					COALESCE(i.description, '') ILIKE $4 OR
					COALESCE(i.notes, '') ILIKE $4 OR
					COALESCE(c.name, '') ILIKE $4 OR
					COALESCE(ct.name, '') ILIKE $4 OR
					COALESCE(ig.name, '') ILIKE $4 OR
					COALESCE(ig.code, '') ILIKE $4 OR
					COALESCE(l.name, '') ILIKE $4 OR
					COALESCE(c.location_description, '') ILIKE $4 OR
					COALESCE(il.name, '') ILIKE $4 OR
					COALESCE(i.location_detail, '') ILIKE $4
				)
		),
		numbered_items AS (
			SELECT
				*,
				count(*) OVER() AS total_count,
				row_number() OVER (ORDER BY %s) AS row_num
			FROM ranked_items
		),
		paged_items AS (
			SELECT *
			FROM numbered_items
			WHERE row_num > $8
				AND row_num <= ($8 + $9)
		)
		SELECT
			pi.id::text,
			pi.title,
			pi.description,
			pi.approx_value,
			pi.sold_price,
			pi.sold_date,
			pi.notes,
			pi.disposition_code,
			pi.disposition_label,
			pi.current_inventory,
			pi.ai_enriched,
			pi.archived,
			pi.archived_datetime,
			pi.created_datetime,
			pi.updated_datetime,
			pi.inventory_group_id,
			pi.inventory_group_code,
			pi.inventory_group_name,
			pi.container_id,
			pi.container_name,
			pi.container_type,
			pi.container_type_id,
			pi.container_type_name,
			pi.container_location_id,
			pi.container_location_name,
			pi.container_location_description,
			pi.item_location_id,
			pi.item_location_name,
			pi.location_detail,
			COALESCE(image_counts.image_count, 0),
			primary_image.id::text,
			primary_image.original_filename,
			primary_image.stored_filename,
			primary_image.mime_type,
			primary_image.file_size_bytes,
			primary_image.status,
			primary_image.upload_order,
			pi.total_count
		FROM paged_items pi
		LEFT JOIN LATERAL (
			SELECT count(*)::int AS image_count
			FROM image_assets ia
			WHERE ia.item_id = pi.id
		) image_counts ON true
		LEFT JOIN LATERAL (
			SELECT
				ia.id,
				ia.original_filename,
				ia.stored_filename,
				ia.mime_type,
				ia.file_size_bytes,
				ia.status,
				ia.upload_order
			FROM image_assets ia
			WHERE ia.item_id = pi.id
			ORDER BY ia.upload_order ASC, ia.created_datetime ASC
			LIMIT 1
		) primary_image ON true
		ORDER BY pi.row_num
	`, orderClause)

	rows, err := s.pool.Query(
		ctx,
		query,
		opts.Search,
		uuidFilter(opts.ContainerID),
		opts.DispositionCode,
		opts.SearchPattern,
		opts.Archived != nil,
		boolValue(opts.Archived),
		opts.IncludeArchived,
		opts.Offset,
		opts.Limit,
		opts.LooseOnly,
		opts.AIEnriched != nil,
		boolValue(opts.AIEnriched),
		opts.MissingApprox != nil,
		boolValue(opts.MissingApprox),
		opts.InventoryState,
		uuidFilter(opts.InventoryGroupID),
	)
	if err != nil {
		return models.ListItemsResponse{}, err
	}
	defer rows.Close()

	items := make([]models.InventoryItemSummary, 0)
	totalCount := 0
	for rows.Next() {
		item, rowTotal, err := scanInventoryItemSummary(rows)
		if err != nil {
			return models.ListItemsResponse{}, err
		}

		totalCount = rowTotal
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return models.ListItemsResponse{}, err
	}

	return models.ListItemsResponse{
		Items:      items,
		TotalCount: totalCount,
		Limit:      opts.Limit,
		Offset:     opts.Offset,
	}, nil
}

func (s *ItemStore) GetByID(ctx context.Context, itemID string) (models.InventoryItemDetail, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			i.id::text,
			i.title,
			i.description,
			i.approx_value::text,
			i.sold_price::text,
			i.sold_date::text,
			i.notes,
			i.disposition_code,
			d.label AS disposition_label,
			i.current_inventory,
			i.ai_enriched,
			i.archived,
			i.archived_datetime,
			i.created_datetime,
			i.updated_datetime,
			i.inventory_group_id::text,
			ig.code,
			ig.name,
			c.id::text,
			c.name,
			c.type,
			c.container_type_id::text,
			ct.name,
			c.location_id::text,
			l.name,
			c.location_description,
			i.location_id::text,
			il.name,
			i.location_detail
		FROM items i
		LEFT JOIN inventory_groups ig ON ig.id = i.inventory_group_id
		LEFT JOIN containers c ON c.id = i.container_id
		LEFT JOIN container_types ct ON ct.id = c.container_type_id
		LEFT JOIN locations l ON l.id = c.location_id
		LEFT JOIN locations il ON il.id = i.location_id
		LEFT JOIN item_dispositions d ON d.code = i.disposition_code
		WHERE i.id = $1
	`, itemID)

	var item models.InventoryItemDetail
	var containerID *string
	var containerName *string
	var containerType *string
	var containerTypeID *string
	var containerTypeName *string
	var containerLocationID *string
	var containerLocationName *string
	var containerLocation *string
	var itemLocationID *string
	var itemLocationName *string
	var itemLocationDetail *string
	if err := row.Scan(
		&item.ID,
		&item.Title,
		&item.Description,
		&item.ApproxValue,
		&item.SoldPrice,
		&item.SoldDate,
		&item.Notes,
		&item.DispositionCode,
		&item.DispositionLabel,
		&item.CurrentInventory,
		&item.AiEnriched,
		&item.Archived,
		&item.ArchivedDatetime,
		&item.CreatedDatetime,
		&item.UpdatedDatetime,
		&item.InventoryGroupID,
		&item.InventoryGroupCode,
		&item.InventoryGroupName,
		&containerID,
		&containerName,
		&containerType,
		&containerTypeID,
		&containerTypeName,
		&containerLocationID,
		&containerLocationName,
		&containerLocation,
		&itemLocationID,
		&itemLocationName,
		&itemLocationDetail,
	); err != nil {
		return models.InventoryItemDetail{}, err
	}

	if containerID != nil && containerName != nil {
		item.Container = &models.InventoryContainer{
			ID:                  *containerID,
			Name:                *containerName,
			Type:                containerType,
			ContainerTypeID:     containerTypeID,
			ContainerTypeName:   containerTypeName,
			LocationID:          containerLocationID,
			LocationName:        containerLocationName,
			LocationDescription: containerLocation,
		}
	}
	item.LocationID = itemLocationID
	item.LocationName = itemLocationName
	item.LocationDetail = itemLocationDetail

	images, err := s.loadItemImages(ctx, itemID)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	item.Images = images
	item.ImageCount = len(images)

	return item, nil
}

func (s *ItemStore) Patch(ctx context.Context, itemID string, req models.PatchItemRequest) (models.InventoryItemDetail, error) {
	title, err := normalizeRequiredOptionalString(req.Title, "title")
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	description := normalizeNullableOptionalString(req.Description)
	approxValue, err := normalizeMoneyOptionalString(req.ApproxValue, "approx_value")
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	soldPrice, err := normalizeMoneyOptionalString(req.SoldPrice, "sold_price")
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	soldDate, err := normalizeDateOptionalString(req.SoldDate, "sold_date")
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	notes := normalizePlainTextOptionalString(req.Notes)
	dispositionCode, err := normalizeDispositionOptionalString(req.DispositionCode)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}

	if dispositionCode != nil {
		exists, err := s.dispositionExists(ctx, *dispositionCode)
		if err != nil {
			return models.InventoryItemDetail{}, err
		}
		if !exists {
			return models.InventoryItemDetail{}, errors.New("disposition_code must be a valid item disposition")
		}
	}

	containerID, updateContainer, err := normalizeContainerPatchField(req.ContainerID)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	if updateContainer && containerID != nil {
		currentContainerID, err := s.itemContainerID(ctx, itemID)
		if err != nil {
			return models.InventoryItemDetail{}, err
		}
		if currentContainerID != nil && *currentContainerID == *containerID {
			containerID = currentContainerID
		} else {
			exists, archived, err := s.containerStatus(ctx, *containerID)
			if err != nil {
				return models.InventoryItemDetail{}, err
			}
			if !exists {
				return models.InventoryItemDetail{}, pgx.ErrNoRows
			}
			if archived {
				return models.InventoryItemDetail{}, errArchivedMoveTarget
			}
		}
	}
	locationID, updateLocation, err := normalizeLocationPatchField(req.LocationID)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	locationDetail := normalizeNullableOptionalString(req.LocationDetail)
	if updateLocation && locationID != nil {
		exists, archived, err := s.locationStatus(ctx, *locationID)
		if err != nil {
			return models.InventoryItemDetail{}, err
		}
		if !exists {
			return models.InventoryItemDetail{}, pgx.ErrNoRows
		}
		if archived {
			return models.InventoryItemDetail{}, errArchivedLocation
		}
	}
	inventoryGroupID, updateInventoryGroup, err := normalizeInventoryGroupPatchField(req.InventoryGroupID)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	if updateInventoryGroup {
		exists, archived, err := s.inventoryGroupStatus(ctx, *inventoryGroupID)
		if err != nil {
			return models.InventoryItemDetail{}, err
		}
		if !exists {
			return models.InventoryItemDetail{}, errItemGroupNotFound
		}
		if archived {
			return models.InventoryItemDetail{}, errArchivedItemGroup
		}
	}

	if !req.Title.Set && !req.Description.Set && !req.ApproxValue.Set && !req.SoldPrice.Set && !req.SoldDate.Set && !req.Notes.Set && !req.DispositionCode.Set && !updateContainer && !updateLocation && !req.LocationDetail.Set && !updateInventoryGroup {
		return models.InventoryItemDetail{}, errors.New("at least one editable field is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	defer tx.Rollback(ctx)

	var previousDisposition *string
	var previousCurrentInventory bool
	if err := tx.QueryRow(ctx, `
		SELECT disposition_code, current_inventory
		FROM items
		WHERE id = $1
		FOR UPDATE
	`, itemID).Scan(&previousDisposition, &previousCurrentInventory); err != nil {
		return models.InventoryItemDetail{}, err
	}

	nextDisposition := previousDisposition
	if req.DispositionCode.Set {
		nextDisposition = dispositionCode
	}
	if nextDisposition == nil {
		stored := dispositionStored
		nextDisposition = &stored
	}
	nextCurrentInventory := isCurrentInventoryDisposition(*nextDisposition)
	if !nextCurrentInventory {
		containerID = nil
		updateContainer = true
	}

	tag, err := tx.Exec(ctx, `
		UPDATE items
		SET
			title = CASE WHEN $2 THEN $3 ELSE title END,
			description = CASE WHEN $4 THEN $5 ELSE description END,
			approx_value = CASE
				WHEN $6 THEN CASE WHEN $7::text IS NULL THEN NULL ELSE $7::numeric(12,2) END
				ELSE approx_value
			END,
			sold_price = CASE
				WHEN $8 THEN CASE WHEN $9::text IS NULL THEN NULL ELSE $9::numeric(12,2) END
				ELSE sold_price
			END,
			sold_date = CASE
				WHEN $21 THEN CASE WHEN $22::text IS NULL THEN NULL ELSE $22::date END
				ELSE sold_date
			END,
			notes = CASE
				WHEN $23 THEN COALESCE($24::text, '')
				ELSE notes
			END,
			disposition_code = CASE WHEN $10 THEN $11 ELSE disposition_code END,
			current_inventory = $20,
			container_id = CASE
				WHEN $12 THEN CASE WHEN $13::text IS NULL THEN NULL ELSE $13::uuid END
				ELSE container_id
			END,
			location_id = CASE
				WHEN $14 THEN CASE WHEN $15::text IS NULL THEN NULL ELSE $15::uuid END
				ELSE location_id
			END,
			location_detail = CASE
				WHEN $16 THEN $17
				ELSE location_detail
			END,
			inventory_group_id = CASE
				WHEN $18 THEN $19::uuid
				ELSE inventory_group_id
			END,
			updated_datetime = now()
		WHERE id = $1
	`,
		itemID,
		req.Title.Set, nullableStringArg(title),
		req.Description.Set, nullableStringArg(description),
		req.ApproxValue.Set, nullableStringArg(approxValue),
		req.SoldPrice.Set, nullableStringArg(soldPrice),
		req.DispositionCode.Set, nullableStringArg(dispositionCode),
		updateContainer, nullableStringArg(containerID),
		updateLocation, nullableStringArg(locationID),
		req.LocationDetail.Set, nullableStringArg(locationDetail),
		updateInventoryGroup, nullableStringArg(inventoryGroupID),
		nextCurrentInventory,
		req.SoldDate.Set, nullableStringArg(soldDate),
		req.Notes.Set, nullableStringArg(notes),
	)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.InventoryItemDetail{}, pgx.ErrNoRows
	}

	if req.DispositionCode.Set && !sameNullableString(previousDisposition, nextDisposition) {
		if _, err := tx.Exec(ctx, `
			INSERT INTO item_disposition_history (
				item_id,
				previous_disposition_code,
				new_disposition_code,
				previous_current_inventory,
				new_current_inventory
			)
			VALUES ($1, $2, $3, $4, $5)
		`, itemID, nullableStringArg(previousDisposition), *nextDisposition, previousCurrentInventory, nextCurrentInventory); err != nil {
			return models.InventoryItemDetail{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.InventoryItemDetail{}, err
	}

	return s.GetByID(ctx, itemID)
}

func (s *ItemStore) SetArchived(ctx context.Context, itemID string, archived bool) (models.InventoryItemDetail, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE items
		SET
			archived = $2,
			archived_datetime = CASE
				WHEN $2 AND archived = false THEN now()
				WHEN $2 AND archived = true THEN archived_datetime
				ELSE NULL
			END,
			updated_datetime = now()
		WHERE id = $1
	`, itemID, archived)
	if err != nil {
		return models.InventoryItemDetail{}, err
	}
	if tag.RowsAffected() == 0 {
		return models.InventoryItemDetail{}, pgx.ErrNoRows
	}

	return s.GetByID(ctx, itemID)
}

func (s *ItemStore) DeletePreview(ctx context.Context, itemID string) (models.ItemDeletePreview, error) {
	deleteCtx, err := s.buildDeleteContext(ctx, s.pool, itemID, false)
	if err != nil {
		return models.ItemDeletePreview{}, err
	}
	return deleteCtx.Preview, nil
}

func (s *ItemStore) Delete(ctx context.Context, itemID string) (models.ItemDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.ItemDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	deleteCtx, err := s.buildDeleteContext(ctx, tx, itemID, true)
	if err != nil {
		return models.ItemDeleteResponse{}, err
	}

	imageDeleteTag, err := tx.Exec(ctx, `
		DELETE FROM image_assets
		WHERE item_id = $1
	`, itemID)
	if err != nil {
		return models.ItemDeleteResponse{}, err
	}

	itemDeleteTag, err := tx.Exec(ctx, `
		DELETE FROM items
		WHERE id = $1
	`, itemID)
	if err != nil {
		return models.ItemDeleteResponse{}, err
	}
	if itemDeleteTag.RowsAffected() == 0 {
		return models.ItemDeleteResponse{}, pgx.ErrNoRows
	}

	if len(deleteCtx.LinkedGroupIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM upload_groups ug
			WHERE ug.id::text = ANY($1::text[])
				AND NOT EXISTS (
					SELECT 1
					FROM image_assets ia
					WHERE ia.upload_group_id = ug.id
				)
		`, deleteCtx.LinkedGroupIDs); err != nil {
			return models.ItemDeleteResponse{}, err
		}
	}

	if len(deleteCtx.LinkedSessionIDs) > 0 {
		if _, err := tx.Exec(ctx, `
			DELETE FROM upload_sessions us
			WHERE us.id::text = ANY($1::text[])
				AND NOT EXISTS (
					SELECT 1
					FROM upload_groups ug
					WHERE ug.session_id = us.id
				)
				AND NOT EXISTS (
					SELECT 1
					FROM image_assets ia
					WHERE ia.session_id = us.id
				)
		`, deleteCtx.LinkedSessionIDs); err != nil {
			return models.ItemDeleteResponse{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.ItemDeleteResponse{}, err
	}

	deletedFiles, missingFiles, warnings := s.files.DeleteFiles(deleteCtx.FilePaths, "item delete")

	return models.ItemDeleteResponse{
		DeletedItemID:          itemID,
		DeletedImageAssetCount: int(imageDeleteTag.RowsAffected()),
		DeletedFileCount:       deletedFiles,
		MissingFileCount:       missingFiles,
		Warnings:               warnings,
	}, nil
}

func (s *ItemStore) ListDispositions(ctx context.Context) (models.ListItemDispositionsResponse, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT code, label, sort_order, is_active
		FROM item_dispositions
		WHERE is_active = true
		ORDER BY sort_order ASC, label ASC
	`)
	if err != nil {
		return models.ListItemDispositionsResponse{}, err
	}
	defer rows.Close()

	dispositions := make([]models.ItemDisposition, 0)
	for rows.Next() {
		var disposition models.ItemDisposition
		if err := rows.Scan(&disposition.Code, &disposition.Label, &disposition.SortOrder, &disposition.IsActive); err != nil {
			return models.ListItemDispositionsResponse{}, err
		}
		dispositions = append(dispositions, disposition)
	}
	if err := rows.Err(); err != nil {
		return models.ListItemDispositionsResponse{}, err
	}

	return models.ListItemDispositionsResponse{Dispositions: dispositions}, nil
}

func (s *ItemStore) ListDispositionHistory(ctx context.Context, itemID string) (models.ListItemDispositionHistoryResponse, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			h.id::text,
			h.item_id::text,
			h.previous_disposition_code,
			p.label,
			h.new_disposition_code,
			n.label,
			h.previous_current_inventory,
			h.new_current_inventory,
			h.changed_datetime,
			h.changed_by
		FROM item_disposition_history h
		LEFT JOIN item_dispositions p ON p.code = h.previous_disposition_code
		LEFT JOIN item_dispositions n ON n.code = h.new_disposition_code
		WHERE h.item_id = $1
		ORDER BY h.changed_datetime DESC, h.id DESC
	`, itemID)
	if err != nil {
		return models.ListItemDispositionHistoryResponse{}, err
	}
	defer rows.Close()

	history := make([]models.ItemDispositionHistoryEntry, 0)
	for rows.Next() {
		var entry models.ItemDispositionHistoryEntry
		if err := rows.Scan(
			&entry.ID,
			&entry.ItemID,
			&entry.PreviousDispositionCode,
			&entry.PreviousDispositionLabel,
			&entry.NewDispositionCode,
			&entry.NewDispositionLabel,
			&entry.PreviousCurrentInventory,
			&entry.NewCurrentInventory,
			&entry.ChangedDatetime,
			&entry.ChangedBy,
		); err != nil {
			return models.ListItemDispositionHistoryResponse{}, err
		}
		history = append(history, entry)
	}
	if err := rows.Err(); err != nil {
		return models.ListItemDispositionHistoryResponse{}, err
	}

	return models.ListItemDispositionHistoryResponse{History: history}, nil
}

func (s *ItemStore) buildDeleteContext(ctx context.Context, q itemQuerier, itemID string, lock bool) (itemDeleteContext, error) {
	record, err := s.loadDeleteRecord(ctx, q, itemID, lock)
	if err != nil {
		return itemDeleteContext{}, err
	}

	images, err := s.loadDeleteImages(ctx, q, itemID, lock)
	if err != nil {
		return itemDeleteContext{}, err
	}

	refs := make([]managedFileReference, 0, len(images)*3)
	groupSet := make(map[string]struct{})
	sessionSet := make(map[string]struct{})
	for _, image := range images {
		refs = append(refs, managedFileReference{
			ImageAssetID: image.ImageAssetID,
			Kind:         "original",
			Path:         image.FilePath,
		})
		if image.ThumbnailPath != nil && strings.TrimSpace(*image.ThumbnailPath) != "" {
			refs = append(refs, managedFileReference{
				ImageAssetID: image.ImageAssetID,
				Kind:         "thumbnail",
				Path:         *image.ThumbnailPath,
			})
		}
		if image.NormalizedPath != nil && strings.TrimSpace(*image.NormalizedPath) != "" {
			refs = append(refs, managedFileReference{
				ImageAssetID: image.ImageAssetID,
				Kind:         "normalized",
				Path:         *image.NormalizedPath,
			})
		}
		if image.UploadGroupID != nil {
			groupSet[*image.UploadGroupID] = struct{}{}
		}
		if image.SessionID != nil {
			sessionSet[*image.SessionID] = struct{}{}
		}
	}

	inspection, err := s.files.InspectReferences(refs)
	if err != nil {
		return itemDeleteContext{}, err
	}

	preview := models.ItemDeletePreview{
		ItemID:                   record.ID,
		Title:                    record.Title,
		ImageCount:               len(images),
		FileCount:                len(inspection.Files),
		TotalFileSizeBytes:       inspection.TotalFileSizeBytes,
		LinkedUploadGroupCount:   len(groupSet),
		LinkedUploadSessionCount: len(sessionSet),
		Warnings: []string{
			"This permanently deletes the inventory item and DB-referenced image files.",
		},
		Files: inspection.Files,
	}
	if record.ContainerID != nil && record.ContainerName != nil {
		preview.Container = &models.InventoryContainer{
			ID:                  *record.ContainerID,
			Name:                *record.ContainerName,
			Type:                record.ContainerType,
			ContainerTypeID:     record.ContainerTypeID,
			ContainerTypeName:   record.ContainerTypeName,
			LocationID:          record.ContainerLocationID,
			LocationName:        record.ContainerLocationName,
			LocationDescription: record.ContainerLocation,
		}
	}

	return itemDeleteContext{
		Preview:          preview,
		FilePaths:        inspection.DeletePaths,
		LinkedGroupIDs:   mapKeys(groupSet),
		LinkedSessionIDs: mapKeys(sessionSet),
	}, nil
}

func (s *ItemStore) loadDeleteRecord(ctx context.Context, q itemQuerier, itemID string, lock bool) (itemDeleteRecord, error) {
	query := `
		SELECT
			i.id::text,
			i.title,
			c.id::text,
			c.name,
			c.type,
			c.container_type_id::text,
			ct.name,
			c.location_id::text,
			l.name,
			c.location_description
		FROM items i
		LEFT JOIN containers c ON c.id = i.container_id
		LEFT JOIN container_types ct ON ct.id = c.container_type_id
		LEFT JOIN locations l ON l.id = c.location_id
		WHERE i.id = $1
	`
	if lock {
		query += ` FOR UPDATE OF i`
	}

	var record itemDeleteRecord
	if err := q.QueryRow(ctx, query, itemID).Scan(
		&record.ID,
		&record.Title,
		&record.ContainerID,
		&record.ContainerName,
		&record.ContainerType,
		&record.ContainerTypeID,
		&record.ContainerTypeName,
		&record.ContainerLocationID,
		&record.ContainerLocationName,
		&record.ContainerLocation,
	); err != nil {
		return itemDeleteRecord{}, err
	}
	return record, nil
}

func (s *ItemStore) loadDeleteImages(ctx context.Context, q itemQuerier, itemID string, lock bool) ([]itemDeleteImageRow, error) {
	query := `
		SELECT
			id::text,
			upload_group_id::text,
			session_id::text,
			file_path,
			thumbnail_path,
			normalized_path,
			original_filename,
			stored_filename
		FROM image_assets
		WHERE item_id = $1
		ORDER BY upload_order ASC, created_datetime ASC
	`
	if lock {
		query += ` FOR UPDATE`
	}

	rows, err := q.Query(ctx, query, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]itemDeleteImageRow, 0)
	for rows.Next() {
		var image itemDeleteImageRow
		if err := rows.Scan(
			&image.ImageAssetID,
			&image.UploadGroupID,
			&image.SessionID,
			&image.FilePath,
			&image.ThumbnailPath,
			&image.NormalizedPath,
			&image.OriginalFilename,
			&image.StoredFilename,
		); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func (s *ItemStore) loadItemImages(ctx context.Context, itemID string) ([]models.InventoryImage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			original_filename,
			stored_filename,
			mime_type,
			file_size_bytes,
			status,
			upload_order,
			thumbnail_path IS NOT NULL AND thumbnail_path <> '',
			normalized_path IS NOT NULL AND normalized_path <> ''
		FROM image_assets
		WHERE item_id = $1
		ORDER BY upload_order ASC, created_datetime ASC
	`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]models.InventoryImage, 0)
	for rows.Next() {
		var image models.InventoryImage
		if err := rows.Scan(
			&image.ImageAssetID,
			&image.OriginalFilename,
			&image.StoredFilename,
			&image.MimeType,
			&image.FileSizeBytes,
			&image.Status,
			&image.UploadOrder,
			&image.ThumbnailAvailable,
			&image.NormalizedAvailable,
		); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func (s *ItemStore) dispositionExists(ctx context.Context, code string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM item_dispositions
			WHERE code = $1
				AND is_active = true
		)
	`, code).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *ItemStore) containerStatus(ctx context.Context, containerID string) (exists bool, archived bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT archived
		FROM containers
		WHERE id = $1::uuid
	`, containerID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, archived, nil
}

func (s *ItemStore) itemExists(ctx context.Context, itemID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM items
			WHERE id = $1::uuid
		)
	`, itemID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *ItemStore) itemContainerID(ctx context.Context, itemID string) (*string, error) {
	var containerID *string
	if err := s.pool.QueryRow(ctx, `
		SELECT container_id::text
		FROM items
		WHERE id = $1::uuid
	`, itemID).Scan(&containerID); err != nil {
		return nil, err
	}
	return containerID, nil
}

func (s *ItemStore) locationStatus(ctx context.Context, locationID string) (exists bool, archived bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT archived
		FROM locations
		WHERE id = $1::uuid
	`, locationID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, archived, nil
}

func (s *ItemStore) inventoryGroupStatus(ctx context.Context, inventoryGroupID string) (exists bool, archived bool, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT archived
		FROM inventory_groups
		WHERE id = $1::uuid
	`, inventoryGroupID).Scan(&archived)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, false, nil
		}
		return false, false, err
	}
	return true, archived, nil
}

func (h *ItemHandler) List(w http.ResponseWriter, r *http.Request) {
	opts, err := parseListItemsOptions(r)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.List(ctx, opts)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load inventory items")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *ItemHandler) Get(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	item, err := h.store.GetByID(ctx, itemID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load inventory item")
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetItemResponse{Item: item})
}

func (h *ItemHandler) Patch(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	var req models.PatchItemRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	item, err := h.store.Patch(ctx, itemID, req)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			exists, existsErr := h.store.itemExists(ctx, itemID)
			if existsErr != nil {
				respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to validate item update")
			} else if exists && req.LocationID.Set && !req.ContainerID.Set {
				respond.ErrorCode(w, http.StatusNotFound, "not_found", "location was not found")
			} else if exists {
				respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
			} else {
				respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
			}
		case errors.Is(err, errArchivedMoveTarget):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot move item to archived container.")
		case errors.Is(err, errArchivedLocation):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign item to archived location.")
		case errors.Is(err, errItemGroupNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
		case errors.Is(err, errArchivedItemGroup):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign item to archived inventory group.")
		default:
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetItemResponse{Item: item})
}

func (h *ItemHandler) Archive(w http.ResponseWriter, r *http.Request) {
	h.setArchivedState(w, r, true)
}

func (h *ItemHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.setArchivedState(w, r, false)
}

func (h *ItemHandler) setArchivedState(w http.ResponseWriter, r *http.Request, archived bool) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	item, err := h.store.SetArchived(ctx, itemID, archived)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to update item archive state")
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetItemResponse{Item: item})
}

func (h *ItemHandler) DeletePreview(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	preview, err := h.store.DeletePreview(ctx, itemID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
		case errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load item delete preview")
		}
		return
	}

	respond.JSON(w, http.StatusOK, preview)
}

func (h *ItemHandler) Delete(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.store.Delete(ctx, itemID)
	if err != nil {
		log.Printf("failed to delete inventory item %s: %v", itemID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
		case errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete item")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *ItemHandler) ListDispositions(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.ListDispositions(ctx)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load item dispositions")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *ItemHandler) ListDispositionHistory(w http.ResponseWriter, r *http.Request) {
	itemID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(itemID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "item id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	exists, err := h.store.itemExists(ctx, itemID)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to validate item")
		return
	}
	if !exists {
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "item was not found")
		return
	}

	response, err := h.store.ListDispositionHistory(ctx, itemID)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load item disposition history")
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func parseListItemsOptions(r *http.Request) (listItemsOptions, error) {
	query := r.URL.Query()

	containerID := strings.TrimSpace(query.Get("container_id"))
	looseOnly := strings.EqualFold(containerID, "none") || strings.EqualFold(containerID, "null")
	if containerID != "" && !looseOnly && !isUUID(containerID) {
		return listItemsOptions{}, errors.New("container_id must be a valid UUID")
	}
	inventoryGroupID := strings.TrimSpace(query.Get("inventory_group_id"))
	if strings.EqualFold(inventoryGroupID, "all") {
		inventoryGroupID = ""
	}
	if inventoryGroupID != "" && !isUUID(inventoryGroupID) {
		return listItemsOptions{}, errors.New("inventory_group_id must be a valid UUID")
	}

	dispositionCode := strings.TrimSpace(query.Get("disposition_code"))
	if strings.EqualFold(dispositionCode, "all") {
		dispositionCode = ""
	}
	aiEnrichedFilter, err := parseOptionalBool(query.Get("ai_enriched"), "ai_enriched")
	if err != nil {
		return listItemsOptions{}, err
	}
	missingApproxFilter, err := parseOptionalBool(query.Get("missing_approx_value"), "missing_approx_value")
	if err != nil {
		return listItemsOptions{}, err
	}

	archivedFilter, includeArchived, err := parseArchivedFilters(query.Get("archived"), query.Get("include_archived"))
	if err != nil {
		return listItemsOptions{}, err
	}
	inventoryState, err := parseInventoryState(query.Get("inventory_state"))
	if err != nil {
		return listItemsOptions{}, err
	}

	limit, err := parsePositiveInt(query.Get("limit"), defaultItemsLimit, 1, maxItemsLimit, "limit")
	if err != nil {
		return listItemsOptions{}, err
	}
	offset, err := parsePositiveInt(query.Get("offset"), 0, 0, 1_000_000, "offset")
	if err != nil {
		return listItemsOptions{}, err
	}

	sortValue := strings.TrimSpace(query.Get("sort"))
	if sortValue == "" {
		sortValue = "default"
	}
	if !isSupportedItemSort(sortValue) {
		return listItemsOptions{}, errors.New("sort must be one of default, newest, oldest, title_asc, title_desc, approx_value_asc, approx_value_desc, sold_price_asc, sold_price_desc")
	}

	search := strings.TrimSpace(query.Get("search"))
	searchPattern := ""
	if search != "" {
		searchPattern = "%" + search + "%"
	}

	var containerFilter *string
	if containerID != "" && !looseOnly {
		containerFilter = &containerID
	}
	var inventoryGroupFilter *string
	if inventoryGroupID != "" {
		inventoryGroupFilter = &inventoryGroupID
	}

	return listItemsOptions{
		Search:           search,
		SearchPattern:    searchPattern,
		ContainerID:      containerFilter,
		InventoryGroupID: inventoryGroupFilter,
		LooseOnly:        looseOnly,
		DispositionCode:  dispositionCode,
		AIEnriched:       aiEnrichedFilter,
		MissingApprox:    missingApproxFilter,
		Archived:         archivedFilter,
		IncludeArchived:  includeArchived,
		InventoryState:   inventoryState,
		Limit:            limit,
		Offset:           offset,
		Sort:             sortValue,
	}, nil
}

func parsePositiveInt(raw string, fallback int, min int, max int, field string) (int, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback, nil
	}

	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", field)
	}
	if parsed < min || parsed > max {
		return 0, fmt.Errorf("%s must be between %d and %d", field, min, max)
	}
	return parsed, nil
}

func parseOptionalBool(raw string, field string) (*bool, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil, nil
	}

	switch strings.ToLower(value) {
	case "true":
		parsed := true
		return &parsed, nil
	case "false":
		parsed := false
		return &parsed, nil
	default:
		return nil, fmt.Errorf("%s must be true or false", field)
	}
}

func parseInventoryState(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "current", nil
	}
	switch value {
	case "current", "former", "all":
		return value, nil
	default:
		return "", errors.New("inventory_state must be one of current, former, all")
	}
}

func isSupportedItemSort(value string) bool {
	switch value {
	case "default", "newest", "oldest", "title_asc", "title_desc", "approx_value_asc", "approx_value_desc", "sold_price_asc", "sold_price_desc":
		return true
	default:
		return false
	}
}

func buildItemOrderClause(searchPresent bool, sort string) string {
	if sort == "default" {
		if searchPresent {
			return "search_rank DESC, created_datetime DESC, title ASC NULLS LAST"
		}
		return "created_datetime DESC, title ASC NULLS LAST"
	}

	switch sort {
	case "newest":
		return "created_datetime DESC, title ASC NULLS LAST"
	case "oldest":
		return "created_datetime ASC, title ASC NULLS LAST"
	case "title_asc":
		return "title ASC NULLS LAST, created_datetime DESC"
	case "title_desc":
		return "title DESC NULLS LAST, created_datetime DESC"
	case "approx_value_asc":
		return "approx_value_numeric ASC NULLS LAST, created_datetime DESC"
	case "approx_value_desc":
		return "approx_value_numeric DESC NULLS LAST, created_datetime DESC"
	case "sold_price_asc":
		return "sold_price_numeric ASC NULLS LAST, created_datetime DESC"
	case "sold_price_desc":
		return "sold_price_numeric DESC NULLS LAST, created_datetime DESC"
	default:
		return "created_datetime DESC, title ASC NULLS LAST"
	}
}

func scanInventoryItemSummary(rows pgx.Rows) (models.InventoryItemSummary, int, error) {
	var item models.InventoryItemSummary
	var containerID *string
	var containerName *string
	var containerType *string
	var containerTypeID *string
	var containerTypeName *string
	var containerLocationID *string
	var containerLocationName *string
	var containerLocation *string
	var itemLocationID *string
	var itemLocationName *string
	var itemLocationDetail *string
	var primaryImageID *string
	var primaryOriginal *string
	var primaryStored *string
	var primaryMime *string
	var primaryFileSize *int64
	var primaryStatus *string
	var primaryUploadOrder *int
	var totalCount int

	if err := rows.Scan(
		&item.ID,
		&item.Title,
		&item.Description,
		&item.ApproxValue,
		&item.SoldPrice,
		&item.SoldDate,
		&item.Notes,
		&item.DispositionCode,
		&item.DispositionLabel,
		&item.CurrentInventory,
		&item.AiEnriched,
		&item.Archived,
		&item.ArchivedDatetime,
		&item.CreatedDatetime,
		&item.UpdatedDatetime,
		&item.InventoryGroupID,
		&item.InventoryGroupCode,
		&item.InventoryGroupName,
		&containerID,
		&containerName,
		&containerType,
		&containerTypeID,
		&containerTypeName,
		&containerLocationID,
		&containerLocationName,
		&containerLocation,
		&itemLocationID,
		&itemLocationName,
		&itemLocationDetail,
		&item.ImageCount,
		&primaryImageID,
		&primaryOriginal,
		&primaryStored,
		&primaryMime,
		&primaryFileSize,
		&primaryStatus,
		&primaryUploadOrder,
		&totalCount,
	); err != nil {
		return models.InventoryItemSummary{}, 0, err
	}

	if containerID != nil && containerName != nil {
		item.Container = &models.InventoryContainer{
			ID:                  *containerID,
			Name:                *containerName,
			Type:                containerType,
			ContainerTypeID:     containerTypeID,
			ContainerTypeName:   containerTypeName,
			LocationID:          containerLocationID,
			LocationName:        containerLocationName,
			LocationDescription: containerLocation,
		}
	}
	item.LocationID = itemLocationID
	item.LocationName = itemLocationName
	item.LocationDetail = itemLocationDetail

	if primaryImageID != nil && primaryStatus != nil && primaryUploadOrder != nil {
		item.PrimaryImage = &models.InventoryImage{
			ImageAssetID:     *primaryImageID,
			OriginalFilename: primaryOriginal,
			StoredFilename:   primaryStored,
			MimeType:         primaryMime,
			FileSizeBytes:    primaryFileSize,
			Status:           *primaryStatus,
			UploadOrder:      *primaryUploadOrder,
		}
	}

	return item, totalCount, nil
}

func uuidFilter(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func boolValue(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func parseArchivedFilters(archivedRaw string, includeArchivedRaw string) (*bool, bool, error) {
	archivedText := strings.TrimSpace(archivedRaw)
	if archivedText != "" {
		switch strings.ToLower(archivedText) {
		case "true":
			value := true
			return &value, false, nil
		case "false":
			value := false
			return &value, false, nil
		default:
			return nil, false, errors.New("archived must be true or false")
		}
	}

	includeArchivedText := strings.TrimSpace(includeArchivedRaw)
	if includeArchivedText == "" {
		return nil, false, nil
	}

	switch strings.ToLower(includeArchivedText) {
	case "true":
		return nil, true, nil
	case "false":
		return nil, false, nil
	default:
		return nil, false, errors.New("include_archived must be true or false")
	}
}

func mapKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	return keys
}

func normalizeRequiredOptionalString(field models.OptionalString, name string) (*string, error) {
	if !field.Set {
		return nil, nil
	}
	if field.Value == nil {
		return nil, fmt.Errorf("%s must not be blank", name)
	}

	trimmed := strings.TrimSpace(*field.Value)
	if trimmed == "" {
		return nil, fmt.Errorf("%s must not be blank", name)
	}
	return &trimmed, nil
}

func normalizeNullableOptionalString(field models.OptionalString) *string {
	if !field.Set || field.Value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*field.Value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeDispositionOptionalString(field models.OptionalString) (*string, error) {
	if !field.Set {
		return nil, nil
	}
	if field.Value == nil {
		return nil, errors.New("disposition_code is required")
	}
	trimmed := strings.TrimSpace(*field.Value)
	if trimmed == "" {
		return nil, errors.New("disposition_code is required")
	}
	return &trimmed, nil
}

func normalizeMoneyOptionalString(field models.OptionalString, name string) (*string, error) {
	value := normalizeNullableOptionalString(field)
	if value == nil {
		return nil, nil
	}
	if !moneyValuePattern.MatchString(*value) {
		return nil, fmt.Errorf("%s must be a non-negative decimal with up to two decimal places", name)
	}
	return value, nil
}

func normalizeDateOptionalString(field models.OptionalString, name string) (*string, error) {
	value := normalizeNullableOptionalString(field)
	if value == nil {
		return nil, nil
	}
	if _, err := time.Parse("2006-01-02", *value); err != nil {
		return nil, fmt.Errorf("%s must be a YYYY-MM-DD date", name)
	}
	return value, nil
}

func normalizePlainTextOptionalString(field models.OptionalString) *string {
	if !field.Set || field.Value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*field.Value)
	return &trimmed
}

func normalizeContainerPatchField(field models.OptionalString) (*string, bool, error) {
	if !field.Set {
		return nil, false, nil
	}
	if field.Value == nil {
		return nil, true, nil
	}
	value := normalizeNullableOptionalString(field)
	if value == nil {
		return nil, true, nil
	}
	if !isUUID(*value) {
		return nil, false, errors.New("container_id must be a valid UUID")
	}
	return value, true, nil
}

func normalizeLocationPatchField(field models.OptionalString) (*string, bool, error) {
	if !field.Set {
		return nil, false, nil
	}
	if field.Value == nil {
		return nil, true, nil
	}
	value := normalizeNullableOptionalString(field)
	if value == nil {
		return nil, true, nil
	}
	if !isUUID(*value) {
		return nil, false, errors.New("location_id must be a valid UUID")
	}
	return value, true, nil
}

func normalizeInventoryGroupPatchField(field models.OptionalString) (*string, bool, error) {
	if !field.Set {
		return nil, false, nil
	}
	if field.Value == nil {
		return nil, false, errors.New("inventory_group_id is required")
	}
	value := normalizeNullableOptionalString(field)
	if value == nil {
		return nil, false, errors.New("inventory_group_id is required")
	}
	if !isUUID(*value) {
		return nil, false, errors.New("inventory_group_id must be a valid UUID")
	}
	return value, true, nil
}

func nullableStringArg(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func isCurrentInventoryDisposition(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "sold", "donated", "disposed":
		return false
	default:
		return true
	}
}

func sameNullableString(left *string, right *string) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}
