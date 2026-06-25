package models

import "time"

type SellProviderConfig struct {
	ID               string     `json:"id"`
	ProviderType     string     `json:"provider_type"`
	DisplayName      string     `json:"display_name"`
	Enabled          bool       `json:"enabled"`
	SortOrder        int        `json:"sort_order"`
	IconKey          string     `json:"icon_key"`
	BaseURL          *string    `json:"base_url"`
	SellerProfileURL *string    `json:"seller_profile_url"`
	Notes            *string    `json:"notes"`
	CreatedDatetime  time.Time  `json:"created_datetime"`
	UpdatedDatetime  *time.Time `json:"updated_datetime"`
}

type ListSellProvidersResponse struct {
	Providers []SellProviderConfig `json:"providers"`
}

type GetSellProviderResponse struct {
	Provider SellProviderConfig `json:"provider"`
}

type CreateSellProviderRequest struct {
	ProviderType     string  `json:"provider_type"`
	DisplayName      string  `json:"display_name"`
	Enabled          *bool   `json:"enabled"`
	SortOrder        *int    `json:"sort_order"`
	IconKey          string  `json:"icon_key"`
	BaseURL          *string `json:"base_url"`
	SellerProfileURL *string `json:"seller_profile_url"`
	Notes            *string `json:"notes"`
}

type PatchSellProviderRequest struct {
	DisplayName      OptionalString `json:"display_name"`
	Enabled          OptionalBool   `json:"enabled"`
	SortOrder        OptionalInt    `json:"sort_order"`
	IconKey          OptionalString `json:"icon_key"`
	BaseURL          OptionalString `json:"base_url"`
	SellerProfileURL OptionalString `json:"seller_profile_url"`
	Notes            OptionalString `json:"notes"`
}

type DeleteSellProviderResponse struct {
	ProviderID string `json:"provider_id"`
	Deleted    bool   `json:"deleted"`
}
