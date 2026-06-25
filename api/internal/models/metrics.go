package models

import "time"

type AdminMetricsSummary struct {
	TotalCurrentInventoryItems int     `json:"total_current_inventory_items"`
	TotalCurrentApproxValue    float64 `json:"total_current_approx_value"`
	ForSaleCount               int     `json:"for_sale_count"`
	InUseCount                 int     `json:"in_use_count"`
	SalePendingCount           int     `json:"sale_pending_count"`
	SoldCount                  int     `json:"sold_count"`
	DonatedCount               int     `json:"donated_count"`
	DisposedCount              int     `json:"disposed_count"`
	ArchivedCount              int     `json:"archived_count"`
	AIEnrichedCount            int     `json:"ai_enriched_count"`
	MissingApproxValueCount    int     `json:"missing_approx_value_count"`
}

type AdminMetricsTopValueItem struct {
	ID              string   `json:"id"`
	Title           *string  `json:"title"`
	ApproxValue     *float64 `json:"approx_value"`
	DispositionCode *string  `json:"disposition_code"`
	ContainerID     *string  `json:"container_id"`
	ContainerName   *string  `json:"container_name"`
	PrimaryImageID  *string  `json:"primary_image_id"`
}

type AdminMetricsDuplicateItem struct {
	ID              string   `json:"id"`
	Title           *string  `json:"title"`
	ApproxValue     *float64 `json:"approx_value"`
	DispositionCode *string  `json:"disposition_code"`
	ContainerID     *string  `json:"container_id"`
	ContainerName   *string  `json:"container_name"`
}

type AdminMetricsDuplicateTitleGroup struct {
	NormalizedTitle  string                      `json:"normalized_title"`
	Count            int                         `json:"count"`
	TotalApproxValue float64                     `json:"total_approx_value"`
	Items            []AdminMetricsDuplicateItem `json:"items"`
}

type AdminMetricsResponse struct {
	Summary              AdminMetricsSummary               `json:"summary"`
	TopValueItems        []AdminMetricsTopValueItem        `json:"top_value_items"`
	DuplicateTitleGroups []AdminMetricsDuplicateTitleGroup `json:"duplicate_title_groups"`
	GeneratedDatetime    time.Time                         `json:"generated_datetime"`
}
