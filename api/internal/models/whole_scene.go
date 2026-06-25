package models

import (
	"encoding/json"
	"time"
)

type WholeSceneInventoryGroup struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

type WholeSceneImageAsset struct {
	ImageAssetID        string  `json:"image_asset_id"`
	ClientFileID        *string `json:"client_file_id"`
	OriginalFilename    *string `json:"original_filename"`
	StoredFilename      *string `json:"stored_filename"`
	MimeType            *string `json:"mime_type"`
	FileSizeBytes       *int64  `json:"file_size_bytes"`
	Status              string  `json:"status"`
	ErrorMessage        *string `json:"error_message"`
	UploadOrder         int     `json:"upload_order"`
	ThumbnailAvailable  bool    `json:"thumbnail_available"`
	NormalizedAvailable bool    `json:"normalized_available"`
}

type WholeSceneScanImage struct {
	ID              string               `json:"id"`
	ImageAssetID    string               `json:"image_asset_id"`
	SortOrder       int                  `json:"sort_order"`
	CreatedDatetime time.Time            `json:"created_datetime"`
	Image           WholeSceneImageAsset `json:"image"`
}

type WholeSceneAnalysisRun struct {
	ID                   string     `json:"id"`
	RunNumber            int        `json:"run_number"`
	Status               string     `json:"status"`
	AIProviderConfigID   *string    `json:"ai_provider_config_id"`
	ProviderType         string     `json:"provider_type"`
	ModelName            string     `json:"model_name"`
	PromptVersion        string     `json:"prompt_version"`
	RawResponseAvailable bool       `json:"raw_response_available"`
	ErrorMessage         *string    `json:"error_message"`
	QueuedDatetime       time.Time  `json:"queued_datetime"`
	StartedDatetime      *time.Time `json:"started_datetime"`
	CompletedDatetime    *time.Time `json:"completed_datetime"`
	CreatedDatetime      time.Time  `json:"created_datetime"`
	UpdatedDatetime      *time.Time `json:"updated_datetime"`
}

type WholeSceneApprovedItem struct {
	ID               string  `json:"id"`
	Title            *string `json:"title"`
	ApproxValue      *string `json:"approx_value"`
	CurrentInventory bool    `json:"current_inventory"`
	Archived         bool    `json:"archived"`
}

type WholeSceneBoundingBox struct {
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	Width  *float64 `json:"width"`
	Height *float64 `json:"height"`
}

type WholeSceneCandidateAppearance struct {
	ID               string                `json:"id"`
	CandidateID      string                `json:"candidate_id"`
	ScanImageID      string                `json:"scan_image_id"`
	SourceImageIndex *int                  `json:"source_image_index"`
	BoundingBox      WholeSceneBoundingBox `json:"bounding_box"`
	LocalizationData *json.RawMessage      `json:"localization_data"`
	ConfidenceLabel  *string               `json:"confidence_label"`
	Notes            *string               `json:"notes"`
	CreatedDatetime  time.Time             `json:"created_datetime"`
}

type WholeSceneCandidateCrop struct {
	ID               string                `json:"id"`
	CandidateID      string                `json:"candidate_id"`
	AppearanceID     *string               `json:"appearance_id"`
	ScanImageID      *string               `json:"scan_image_id"`
	CropImageAssetID *string               `json:"crop_image_asset_id"`
	Status           string                `json:"status"`
	IsPreferred      bool                  `json:"is_preferred"`
	BoundingBox      WholeSceneBoundingBox `json:"bounding_box"`
	CropMetadata     *json.RawMessage      `json:"crop_metadata"`
	ErrorMessage     *string               `json:"error_message"`
	CreatedDatetime  time.Time             `json:"created_datetime"`
	UpdatedDatetime  *time.Time            `json:"updated_datetime"`
	CropImage        *WholeSceneImageAsset `json:"crop_image"`
}

type WholeSceneCandidate struct {
	ID                       string                          `json:"id"`
	AnalysisRunID            *string                         `json:"analysis_run_id"`
	Source                   string                          `json:"source"`
	Status                   string                          `json:"status"`
	Title                    *string                         `json:"title"`
	Description              *string                         `json:"description"`
	ApproxValue              *string                         `json:"approx_value"`
	ConfidenceLabel          *string                         `json:"confidence_label"`
	UncertaintyNotes         *string                         `json:"uncertainty_notes"`
	RawCandidate             *json.RawMessage                `json:"raw_candidate"`
	ParseWarnings            *string                         `json:"parse_warnings"`
	AIAssistStatus           string                          `json:"ai_assist_status"`
	AIAssistErrorMessage     string                          `json:"ai_assist_error_message"`
	AIAssistRequestedAt      *time.Time                      `json:"ai_assist_requested_at"`
	AIAssistStartedAt        *time.Time                      `json:"ai_assist_started_at"`
	AIAssistCompletedAt      *time.Time                      `json:"ai_assist_completed_at"`
	AIAssistProviderConfigID *string                         `json:"ai_assist_provider_config_id"`
	AIAssistProvider         string                          `json:"ai_assist_provider"`
	AIAssistModel            string                          `json:"ai_assist_model"`
	ApprovedItemID           *string                         `json:"approved_item_id"`
	ApprovedDatetime         *time.Time                      `json:"approved_datetime"`
	RejectedDatetime         *time.Time                      `json:"rejected_datetime"`
	CreatedBy                *string                         `json:"created_by"`
	CreatedDatetime          time.Time                       `json:"created_datetime"`
	UpdatedBy                *string                         `json:"updated_by"`
	UpdatedDatetime          *time.Time                      `json:"updated_datetime"`
	ApprovedItem             *WholeSceneApprovedItem         `json:"approved_item"`
	Appearances              []WholeSceneCandidateAppearance `json:"appearances"`
	Crops                    []WholeSceneCandidateCrop       `json:"crops"`
}

type WholeSceneScan struct {
	ID                string                   `json:"id"`
	UploadSessionID   string                   `json:"upload_session_id"`
	Container         *InventoryContainer      `json:"container"`
	LocationID        *string                  `json:"location_id"`
	LocationName      *string                  `json:"location_name"`
	LocationDetail    *string                  `json:"location_detail"`
	InventoryGroup    WholeSceneInventoryGroup `json:"inventory_group"`
	Hint              *string                  `json:"hint"`
	Status            string                   `json:"status"`
	CreatedBy         *string                  `json:"created_by"`
	CreatedDatetime   time.Time                `json:"created_datetime"`
	UpdatedBy         *string                  `json:"updated_by"`
	UpdatedDatetime   *time.Time               `json:"updated_datetime"`
	Images            []WholeSceneScanImage    `json:"images"`
	AnalysisRuns      []WholeSceneAnalysisRun  `json:"analysis_runs"`
	LatestAnalysisRun *WholeSceneAnalysisRun   `json:"latest_analysis_run"`
	Candidates        []WholeSceneCandidate    `json:"candidates"`
}

type WholeSceneCandidateCounts struct {
	Pending  int `json:"pending"`
	Approved int `json:"approved"`
	Rejected int `json:"rejected"`
	Total    int `json:"total"`
}

type WholeSceneReviewScanSummary struct {
	ID                string                    `json:"id"`
	UploadSessionID   string                    `json:"upload_session_id"`
	Container         *InventoryContainer       `json:"container"`
	LocationID        *string                   `json:"location_id"`
	LocationName      *string                   `json:"location_name"`
	LocationDetail    *string                   `json:"location_detail"`
	InventoryGroup    WholeSceneInventoryGroup  `json:"inventory_group"`
	Hint              *string                   `json:"hint"`
	Status            string                    `json:"status"`
	ImageCount        int                       `json:"image_count"`
	ProcessedImages   int                       `json:"processed_image_count"`
	FailedImages      int                       `json:"failed_image_count"`
	CandidateCounts   WholeSceneCandidateCounts `json:"candidate_counts"`
	LatestAnalysisRun *WholeSceneAnalysisRun    `json:"latest_analysis_run"`
	Images            []WholeSceneScanImage     `json:"images"`
	CreatedDatetime   time.Time                 `json:"created_datetime"`
	UpdatedDatetime   *time.Time                `json:"updated_datetime"`
}

type ListWholeSceneReviewScansResponse struct {
	Scans []WholeSceneReviewScanSummary `json:"scans"`
}

type GetWholeSceneScanResponse struct {
	Scan WholeSceneScan `json:"scan"`
}

type WholeSceneCleanupSummary struct {
	DeletedImageAssetCount    int      `json:"deleted_image_asset_count"`
	DeletedUploadSessionCount int      `json:"deleted_upload_session_count"`
	DeletedFileCount          int      `json:"deleted_file_count"`
	MissingFileCount          int      `json:"missing_file_count"`
	Warnings                  []string `json:"warnings"`
}

type WholeSceneCandidateMutationResponse struct {
	Scan           *WholeSceneScan           `json:"scan,omitempty"`
	ScanID         string                    `json:"scan_id"`
	CleanedUp      bool                      `json:"cleaned_up"`
	ApprovedItemID *string                   `json:"approved_item_id"`
	Cleanup        *WholeSceneCleanupSummary `json:"cleanup,omitempty"`
}

type WholeSceneCandidateImageDeleteResponse struct {
	Scan                *WholeSceneScan `json:"scan,omitempty"`
	ScanID              string          `json:"scan_id"`
	DeletedCropID       string          `json:"deleted_crop_id"`
	DeletedImageAssetID string          `json:"deleted_image_asset_id"`
	DeletedFileCount    int             `json:"deleted_file_count"`
	MissingFileCount    int             `json:"missing_file_count"`
	Warnings            []string        `json:"warnings"`
}

type DeleteWholeSceneScanResponse struct {
	ScanID  string                   `json:"scan_id"`
	Cleanup WholeSceneCleanupSummary `json:"cleanup"`
}

type QueueWholeSceneAnalysisResponse struct {
	Scan        WholeSceneScan         `json:"scan"`
	AnalysisRun *WholeSceneAnalysisRun `json:"analysis_run"`
	Queued      bool                   `json:"queued"`
}

type AddWholeSceneCandidateRequest struct {
	Title            *string `json:"title"`
	Description      *string `json:"description"`
	ApproxValue      *string `json:"approx_value"`
	ConfidenceLabel  *string `json:"confidence_label"`
	UncertaintyNotes *string `json:"uncertainty_notes"`
}

type PatchWholeSceneCandidateRequest struct {
	Title            OptionalString `json:"title"`
	Description      OptionalString `json:"description"`
	ApproxValue      OptionalString `json:"approx_value"`
	ConfidenceLabel  OptionalString `json:"confidence_label"`
	UncertaintyNotes OptionalString `json:"uncertainty_notes"`
}

type ApproveWholeSceneCandidateRequest struct {
	InventoryGroupID *string `json:"inventory_group_id"`
}

type AssistWholeSceneCandidateRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	ApproxValue *string `json:"approx_value"`
	UserHint    *string `json:"user_hint"`
}
