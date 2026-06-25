package models

import (
	"encoding/json"
	"time"
)

type OptionalBool struct {
	Set   bool
	Value *bool
}

func (o *OptionalBool) UnmarshalJSON(data []byte) error {
	o.Set = true

	if string(data) == "null" {
		o.Value = nil
		return nil
	}

	var value bool
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	o.Value = &value
	return nil
}

type OptionalInt struct {
	Set   bool
	Value *int
}

func (o *OptionalInt) UnmarshalJSON(data []byte) error {
	o.Set = true

	if string(data) == "null" {
		o.Value = nil
		return nil
	}

	var value int
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	o.Value = &value
	return nil
}

type OptionalFloat64 struct {
	Set   bool
	Value *float64
}

func (o *OptionalFloat64) UnmarshalJSON(data []byte) error {
	o.Set = true

	if string(data) == "null" {
		o.Value = nil
		return nil
	}

	var value float64
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}

	o.Value = &value
	return nil
}

type AIProviderConfig struct {
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	ProviderType     string     `json:"provider_type"`
	Enabled          bool       `json:"enabled"`
	Active           bool       `json:"active"`
	BaseURL          *string    `json:"base_url"`
	APIKeyConfigured bool       `json:"api_key_configured"`
	APIKeyDisplay    string     `json:"api_key_display"`
	APIKeyEnvVar     *string    `json:"api_key_env_var"`
	ModelName        string     `json:"model_name"`
	VisionEnabled    bool       `json:"vision_enabled"`
	TimeoutSeconds   int        `json:"timeout_seconds"`
	MaxOutputTokens  *int       `json:"max_output_tokens"`
	Temperature      *float64   `json:"temperature"`
	LastTestDatetime *time.Time `json:"last_test_datetime"`
	LastTestStatus   *string    `json:"last_test_status"`
	LastErrorMessage *string    `json:"last_error_message"`
	CreatedDatetime  time.Time  `json:"created_datetime"`
	UpdatedDatetime  *time.Time `json:"updated_datetime"`
}

type ListAIProvidersResponse struct {
	Providers []AIProviderConfig `json:"providers"`
}

type GetAIProviderResponse struct {
	Provider AIProviderConfig `json:"provider"`
}

type CreateAIProviderRequest struct {
	Name            string   `json:"name"`
	ProviderType    string   `json:"provider_type"`
	Enabled         *bool    `json:"enabled"`
	Active          *bool    `json:"active"`
	BaseURL         *string  `json:"base_url"`
	APIKeyValue     *string  `json:"api_key_value"`
	APIKeyEnvVar    *string  `json:"api_key_env_var"`
	ModelName       string   `json:"model_name"`
	VisionEnabled   *bool    `json:"vision_enabled"`
	TimeoutSeconds  *int     `json:"timeout_seconds"`
	MaxOutputTokens *int     `json:"max_output_tokens"`
	Temperature     *float64 `json:"temperature"`
}

type PatchAIProviderRequest struct {
	Name            OptionalString  `json:"name"`
	ProviderType    OptionalString  `json:"provider_type"`
	Enabled         OptionalBool    `json:"enabled"`
	Active          OptionalBool    `json:"active"`
	BaseURL         OptionalString  `json:"base_url"`
	APIKeyValue     OptionalString  `json:"api_key_value"`
	APIKeyEnvVar    OptionalString  `json:"api_key_env_var"`
	ModelName       OptionalString  `json:"model_name"`
	VisionEnabled   OptionalBool    `json:"vision_enabled"`
	TimeoutSeconds  OptionalInt     `json:"timeout_seconds"`
	MaxOutputTokens OptionalInt     `json:"max_output_tokens"`
	Temperature     OptionalFloat64 `json:"temperature"`
	ClearAPIKey     OptionalBool    `json:"clear_api_key"`
}

type DeleteAIProviderResponse struct {
	ProviderID string `json:"provider_id"`
	Deleted    bool   `json:"deleted"`
}

type AIProviderTestResult struct {
	ProviderID     string    `json:"provider_id"`
	ProviderType   string    `json:"provider_type"`
	ModelName      string    `json:"model_name"`
	Status         string    `json:"status"`
	Message        string    `json:"message"`
	TestedDatetime time.Time `json:"tested_datetime"`
}

type AISettings struct {
	AIAssistEnabled    bool    `json:"ai_assist_enabled"`
	ActiveProviderID   *string `json:"active_provider_id"`
	ActiveProviderName *string `json:"active_provider_name"`
	ActiveProviderType *string `json:"active_provider_type"`
	ActiveModelName    *string `json:"active_model_name"`
}

type GetAISettingsResponse struct {
	Settings AISettings `json:"settings"`
}

type PatchAISettingsRequest struct {
	AIAssistEnabled  OptionalBool   `json:"ai_assist_enabled"`
	ActiveProviderID OptionalString `json:"active_provider_id"`
}
