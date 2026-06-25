package models

import (
	"encoding/json"
	"time"
)

type OptionalString struct {
	Set   bool
	Value *string
}

func (o *OptionalString) UnmarshalJSON(data []byte) error {
	o.Set = true

	if string(data) == "null" {
		o.Value = nil
		return nil
	}

	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	o.Value = &value
	return nil
}

type InventoryContainer struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Type                *string `json:"type"`
	ContainerTypeID     *string `json:"container_type_id"`
	ContainerTypeName   *string `json:"container_type_name"`
	LocationID          *string `json:"location_id"`
	LocationName        *string `json:"location_name"`
	LocationDescription *string `json:"location_description"`
}

type InventoryImage struct {
	ImageAssetID        string  `json:"image_asset_id"`
	OriginalFilename    *string `json:"original_filename"`
	StoredFilename      *string `json:"stored_filename"`
	MimeType            *string `json:"mime_type"`
	FileSizeBytes       *int64  `json:"file_size_bytes"`
	Status              string  `json:"status"`
	UploadOrder         int     `json:"upload_order"`
	ThumbnailAvailable  bool    `json:"thumbnail_available"`
	NormalizedAvailable bool    `json:"normalized_available"`
}

type InventoryItemSummary struct {
	ID                 string              `json:"id"`
	Title              *string             `json:"title"`
	Description        *string             `json:"description"`
	ApproxValue        *string             `json:"approx_value"`
	SoldPrice          *string             `json:"sold_price"`
	SoldDate           *string             `json:"sold_date"`
	Notes              string              `json:"notes"`
	DispositionCode    *string             `json:"disposition_code"`
	DispositionLabel   *string             `json:"disposition_label"`
	CurrentInventory   bool                `json:"current_inventory"`
	AiEnriched         bool                `json:"ai_enriched"`
	Archived           bool                `json:"archived"`
	ArchivedDatetime   *time.Time          `json:"archived_datetime"`
	CreatedDatetime    time.Time           `json:"created_datetime"`
	UpdatedDatetime    *time.Time          `json:"updated_datetime"`
	InventoryGroupID   *string             `json:"inventory_group_id"`
	InventoryGroupCode *string             `json:"inventory_group_code"`
	InventoryGroupName *string             `json:"inventory_group_name"`
	Container          *InventoryContainer `json:"container"`
	LocationID         *string             `json:"location_id"`
	LocationName       *string             `json:"location_name"`
	LocationDetail     *string             `json:"location_detail"`
	ImageCount         int                 `json:"image_count"`
	PrimaryImage       *InventoryImage     `json:"primary_image"`
}

type InventoryItemDetail struct {
	ID                 string              `json:"id"`
	Title              *string             `json:"title"`
	Description        *string             `json:"description"`
	ApproxValue        *string             `json:"approx_value"`
	SoldPrice          *string             `json:"sold_price"`
	SoldDate           *string             `json:"sold_date"`
	Notes              string              `json:"notes"`
	DispositionCode    *string             `json:"disposition_code"`
	DispositionLabel   *string             `json:"disposition_label"`
	CurrentInventory   bool                `json:"current_inventory"`
	AiEnriched         bool                `json:"ai_enriched"`
	Archived           bool                `json:"archived"`
	ArchivedDatetime   *time.Time          `json:"archived_datetime"`
	CreatedDatetime    time.Time           `json:"created_datetime"`
	UpdatedDatetime    *time.Time          `json:"updated_datetime"`
	InventoryGroupID   *string             `json:"inventory_group_id"`
	InventoryGroupCode *string             `json:"inventory_group_code"`
	InventoryGroupName *string             `json:"inventory_group_name"`
	Container          *InventoryContainer `json:"container"`
	LocationID         *string             `json:"location_id"`
	LocationName       *string             `json:"location_name"`
	LocationDetail     *string             `json:"location_detail"`
	ImageCount         int                 `json:"image_count"`
	Images             []InventoryImage    `json:"images"`
}

type ListItemsResponse struct {
	Items      []InventoryItemSummary `json:"items"`
	TotalCount int                    `json:"total_count"`
	Limit      int                    `json:"limit"`
	Offset     int                    `json:"offset"`
}

type GetItemResponse struct {
	Item InventoryItemDetail `json:"item"`
}

type ItemDispositionHistoryEntry struct {
	ID                       string    `json:"id"`
	ItemID                   string    `json:"item_id"`
	PreviousDispositionCode  *string   `json:"previous_disposition_code"`
	PreviousDispositionLabel *string   `json:"previous_disposition_label"`
	NewDispositionCode       string    `json:"new_disposition_code"`
	NewDispositionLabel      *string   `json:"new_disposition_label"`
	PreviousCurrentInventory bool      `json:"previous_current_inventory"`
	NewCurrentInventory      bool      `json:"new_current_inventory"`
	ChangedDatetime          time.Time `json:"changed_datetime"`
	ChangedBy                *string   `json:"changed_by"`
}

type ListItemDispositionHistoryResponse struct {
	History []ItemDispositionHistoryEntry `json:"history"`
}

type ItemImageDeleteResponse struct {
	Item                InventoryItemDetail `json:"item"`
	DeletedImageAssetID string              `json:"deleted_image_asset_id"`
	DeletedFileCount    int                 `json:"deleted_file_count"`
	MissingFileCount    int                 `json:"missing_file_count"`
	Warnings            []string            `json:"warnings"`
}

type DeletePreviewFile struct {
	ImageAssetID string `json:"image_asset_id"`
	Kind         string `json:"kind"`
	Path         string `json:"path"`
	Exists       bool   `json:"exists"`
	SizeBytes    int64  `json:"size_bytes"`
}

type ItemDeletePreview struct {
	ItemID                   string              `json:"item_id"`
	Title                    *string             `json:"title"`
	Container                *InventoryContainer `json:"container"`
	ImageCount               int                 `json:"image_count"`
	FileCount                int                 `json:"file_count"`
	TotalFileSizeBytes       int64               `json:"total_file_size_bytes"`
	LinkedUploadGroupCount   int                 `json:"linked_upload_group_count"`
	LinkedUploadSessionCount int                 `json:"linked_upload_session_count"`
	Warnings                 []string            `json:"warnings"`
	Files                    []DeletePreviewFile `json:"files"`
}

type ItemDeleteResponse struct {
	DeletedItemID          string   `json:"deleted_item_id"`
	DeletedImageAssetCount int      `json:"deleted_image_asset_count"`
	DeletedFileCount       int      `json:"deleted_file_count"`
	MissingFileCount       int      `json:"missing_file_count"`
	Warnings               []string `json:"warnings"`
}

type PatchItemRequest struct {
	Title            OptionalString `json:"title"`
	Description      OptionalString `json:"description"`
	ApproxValue      OptionalString `json:"approx_value"`
	SoldPrice        OptionalString `json:"sold_price"`
	SoldDate         OptionalString `json:"sold_date"`
	Notes            OptionalString `json:"notes"`
	DispositionCode  OptionalString `json:"disposition_code"`
	ContainerID      OptionalString `json:"container_id"`
	LocationID       OptionalString `json:"location_id"`
	LocationDetail   OptionalString `json:"location_detail"`
	InventoryGroupID OptionalString `json:"inventory_group_id"`
}

type ItemDisposition struct {
	Code      string `json:"code"`
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
	IsActive  bool   `json:"is_active"`
}

type ListItemDispositionsResponse struct {
	Dispositions []ItemDisposition `json:"dispositions"`
}
