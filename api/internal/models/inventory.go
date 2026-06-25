package models

import "time"

type InventoryContainerSummary struct {
	ContainerID         string     `json:"container_id"`
	ContainerName       string     `json:"container_name"`
	IsSynthetic         bool       `json:"is_synthetic"`
	ContainerType       *string    `json:"container_type"`
	ContainerTypeID     *string    `json:"container_type_id"`
	ContainerTypeName   *string    `json:"container_type_name"`
	LocationID          *string    `json:"location_id"`
	LocationName        *string    `json:"location_name"`
	LocationDescription *string    `json:"location_description"`
	Notes               *string    `json:"notes"`
	ItemCount           int        `json:"item_count"`
	ActiveItemCount     int        `json:"active_item_count"`
	ArchivedItemCount   int        `json:"archived_item_count"`
	ForSaleCount        int        `json:"for_sale_count"`
	InUseCount          int        `json:"in_use_count"`
	SalePendingCount    int        `json:"sale_pending_count"`
	SoldCount           int        `json:"sold_count"`
	DonatedCount        int        `json:"donated_count"`
	DisposedCount       int        `json:"disposed_count"`
	TotalApproxValue    float64    `json:"total_approx_value"`
	LatestItemDatetime  *time.Time `json:"latest_item_datetime"`
}
