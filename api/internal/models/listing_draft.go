package models

import "time"

type PublicSellProvider struct {
	ID               string  `json:"id"`
	ProviderType     string  `json:"provider_type"`
	DisplayName      string  `json:"display_name"`
	Enabled          bool    `json:"enabled"`
	SortOrder        int     `json:"sort_order"`
	IconKey          string  `json:"icon_key"`
	BaseURL          *string `json:"base_url"`
	SellerProfileURL *string `json:"seller_profile_url"`
}

type ListPublicSellProvidersResponse struct {
	Providers []PublicSellProvider `json:"providers"`
}

type ListingDraftItemSummary struct {
	ID              string  `json:"id"`
	Title           *string `json:"title"`
	ApproxValue     *string `json:"approx_value"`
	DispositionCode *string `json:"disposition_code"`
	ContainerID     *string `json:"container_id"`
	ContainerName   *string `json:"container_name"`
}

type ListingDraft struct {
	ID                   string                   `json:"id"`
	ItemID               string                   `json:"item_id"`
	SellProviderConfigID *string                  `json:"sell_provider_config_id"`
	ProviderType         string                   `json:"provider_type"`
	ProviderDisplayName  *string                  `json:"provider_display_name"`
	ProviderIconKey      *string                  `json:"provider_icon_key"`
	Status               string                   `json:"status"`
	Title                string                   `json:"title"`
	Description          *string                  `json:"description"`
	AskingPrice          *string                  `json:"asking_price"`
	Currency             string                   `json:"currency"`
	ListingURL           *string                  `json:"listing_url"`
	Notes                *string                  `json:"notes"`
	CreatedDatetime      time.Time                `json:"created_datetime"`
	UpdatedDatetime      *time.Time               `json:"updated_datetime"`
	Item                 *ListingDraftItemSummary `json:"item,omitempty"`
	PhotoExport          *ListingPhotoExport      `json:"photo_export,omitempty"`
}

type ListingPhotoExportFile struct {
	Filename  string `json:"filename"`
	SizeBytes int64  `json:"size_bytes"`
}

type ListingPhotoExport struct {
	ExportID   string                   `json:"export_id"`
	ExportPath string                   `json:"export_path"`
	ExpiresAt  time.Time                `json:"expires_at"`
	ImageCount int                      `json:"image_count"`
	Files      []ListingPhotoExportFile `json:"files"`
	Warnings   []string                 `json:"warnings"`
}

type ListListingDraftsResponse struct {
	Drafts []ListingDraft `json:"drafts"`
}

type GetListingDraftResponse struct {
	Draft ListingDraft `json:"draft"`
}

type CreateListingDraftRequest struct {
	SellProviderConfigID *string `json:"sell_provider_config_id"`
	ProviderType         *string `json:"provider_type"`
}

type PatchListingDraftRequest struct {
	Title       OptionalString `json:"title"`
	Description OptionalString `json:"description"`
	AskingPrice OptionalString `json:"asking_price"`
	Currency    OptionalString `json:"currency"`
	Status      OptionalString `json:"status"`
	ListingURL  OptionalString `json:"listing_url"`
	Notes       OptionalString `json:"notes"`
}

type DeleteListingDraftResponse struct {
	DraftID string `json:"draft_id"`
	Deleted bool   `json:"deleted"`
}
