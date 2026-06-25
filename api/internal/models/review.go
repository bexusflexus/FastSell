package models

import "time"

type ReviewContainer struct {
	ID                  string  `json:"id"`
	Name                string  `json:"name"`
	Type                *string `json:"type"`
	LocationDescription *string `json:"location_description"`
}

type ReviewImageAsset struct {
	ImageAssetID     string  `json:"image_asset_id"`
	OriginalFilename *string `json:"original_filename"`
	StoredFilename   *string `json:"stored_filename"`
	FilePath         string  `json:"file_path"`
	ThumbnailPath    *string `json:"thumbnail_path"`
	NormalizedPath   *string `json:"normalized_path"`
	MimeType         *string `json:"mime_type"`
	FileSizeBytes    *int64  `json:"file_size_bytes"`
	Status           string  `json:"status"`
	UploadOrder      int     `json:"upload_order"`
}

type ReviewUploadGroup struct {
	UploadGroupID             string             `json:"upload_group_id"`
	UploadSessionID           string             `json:"upload_session_id"`
	Container                 *ReviewContainer   `json:"container"`
	LocationID                *string            `json:"location_id"`
	LocationName              *string            `json:"location_name"`
	LocationDetail            *string            `json:"location_detail"`
	InventoryGroupID          *string            `json:"inventory_group_id"`
	InventoryGroupCode        *string            `json:"inventory_group_code"`
	InventoryGroupName        *string            `json:"inventory_group_name"`
	ClientGroupID             *string            `json:"client_group_id"`
	Title                     *string            `json:"title"`
	Notes                     *string            `json:"notes"`
	SortOrder                 int                `json:"sort_order"`
	Status                    string             `json:"status"`
	CreatedDatetime           time.Time          `json:"created_datetime"`
	UpdatedDatetime           *time.Time         `json:"updated_datetime"`
	ImageCount                int                `json:"image_count"`
	ProcessedImageCount       int                `json:"processed_image_count"`
	FailedImageCount          int                `json:"failed_image_count"`
	AIAssistStatus            string             `json:"ai_assist_status"`
	AIAssistErrorMessage      *string            `json:"ai_assist_error_message"`
	AIAssistRequestedDatetime *time.Time         `json:"ai_assist_requested_datetime"`
	AIAssistStartedDatetime   *time.Time         `json:"ai_assist_started_datetime"`
	AIAssistCompletedDatetime *time.Time         `json:"ai_assist_completed_datetime"`
	AISuggestedTitle          *string            `json:"ai_suggested_title"`
	AISuggestedDescription    *string            `json:"ai_suggested_description"`
	AISuggestedApproxValue    *string            `json:"ai_suggested_approx_value"`
	Images                    []ReviewImageAsset `json:"images"`
}

type ReviewUploadGroupList struct {
	Groups []ReviewUploadGroup `json:"groups"`
}

type GetReviewUploadGroupResponse struct {
	Group ReviewUploadGroup `json:"group"`
}

type ReviewUploadGroupImageMutationResponse struct {
	Group ReviewUploadGroup `json:"group"`
}

type ApproveUploadGroupRequest struct {
	Title            *string `json:"title"`
	Description      *string `json:"description"`
	ApproxValue      *string `json:"approx_value"`
	SoldDate         *string `json:"sold_date"`
	Notes            *string `json:"notes"`
	InventoryGroupID *string `json:"inventory_group_id"`
}

type ReviewItem struct {
	ID                 string     `json:"id"`
	ContainerID        *string    `json:"container_id"`
	LocationID         *string    `json:"location_id"`
	LocationName       *string    `json:"location_name"`
	LocationDetail     *string    `json:"location_detail"`
	InventoryGroupID   *string    `json:"inventory_group_id"`
	InventoryGroupCode *string    `json:"inventory_group_code"`
	InventoryGroupName *string    `json:"inventory_group_name"`
	Title              *string    `json:"title"`
	Description        *string    `json:"description"`
	ApproxValue        *string    `json:"approx_value"`
	SoldDate           *string    `json:"sold_date"`
	Notes              string     `json:"notes"`
	AiEnriched         bool       `json:"ai_enriched"`
	CreatedDatetime    time.Time  `json:"created_datetime"`
	UpdatedDatetime    *time.Time `json:"updated_datetime"`
}

type ApproveUploadGroupResponse struct {
	Item             ReviewItem `json:"item"`
	LinkedImageCount int        `json:"linked_image_count"`
}

type QueueReviewAIAssistResponse struct {
	UploadGroupID             string     `json:"upload_group_id"`
	AIAssistStatus            string     `json:"ai_assist_status"`
	AIAssistErrorMessage      *string    `json:"ai_assist_error_message"`
	AISuggestedTitle          *string    `json:"ai_suggested_title"`
	AISuggestedDescription    *string    `json:"ai_suggested_description"`
	AISuggestedApproxValue    *string    `json:"ai_suggested_approx_value"`
	AIAssistRequestedDatetime *time.Time `json:"ai_assist_requested_datetime"`
	AIAssistStartedDatetime   *time.Time `json:"ai_assist_started_datetime"`
	AIAssistCompletedDatetime *time.Time `json:"ai_assist_completed_datetime"`
}

type QueueReviewAIAssistRequest struct {
	UserHint *string `json:"user_hint"`
}

type ReviewUploadGroupDeletePreview struct {
	UploadGroupID      string              `json:"upload_group_id"`
	UploadSessionID    string              `json:"upload_session_id"`
	ClientGroupID      *string             `json:"client_group_id"`
	Title              *string             `json:"title"`
	ImageCount         int                 `json:"image_count"`
	FileCount          int                 `json:"file_count"`
	TotalFileSizeBytes int64               `json:"total_file_size_bytes"`
	Warnings           []string            `json:"warnings"`
	Files              []DeletePreviewFile `json:"files"`
}

type ReviewUploadGroupDeleteResponse struct {
	DeletedUploadGroupID   string   `json:"deleted_upload_group_id"`
	DeletedImageAssetCount int      `json:"deleted_image_asset_count"`
	DeletedFileCount       int      `json:"deleted_file_count"`
	MissingFileCount       int      `json:"missing_file_count"`
	Warnings               []string `json:"warnings"`
}

type ReviewImageDeleteResponse struct {
	Group               *ReviewUploadGroup `json:"group"`
	DeletedImageAssetID string             `json:"deleted_image_asset_id"`
	DeletedFileCount    int                `json:"deleted_file_count"`
	MissingFileCount    int                `json:"missing_file_count"`
	Warnings            []string           `json:"warnings"`
}
