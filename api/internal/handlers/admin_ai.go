package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultAIProviderTimeoutSeconds = 60
	defaultOllamaBaseURL            = "http://localhost:11434"
	geminiDefaultBaseURL            = "https://generativelanguage.googleapis.com"
	openAIDefaultBaseURL            = "https://api.openai.com"
	maskedAPIKeyDisplay             = "********"
)

var errProviderMustBeEnabledForActive = errors.New("active provider must be enabled")

type AIProviderStore struct {
	pool *pgxpool.Pool
}

type AIAdminHandler struct {
	store *AIProviderStore
}

type storedAIProviderConfig struct {
	ID               string
	Name             string
	ProviderType     string
	Enabled          bool
	Active           bool
	BaseURL          *string
	APIKeyValue      *string
	APIKeyEnvVar     *string
	ModelName        string
	VisionEnabled    bool
	TimeoutSeconds   int
	MaxOutputTokens  *int
	Temperature      *float64
	LastTestDatetime *time.Time
	LastTestStatus   *string
	LastErrorMessage *string
	CreatedDatetime  time.Time
	UpdatedDatetime  *time.Time
}

func NewAIProviderStore(pool *pgxpool.Pool) *AIProviderStore {
	return &AIProviderStore{pool: pool}
}

func NewAIAdminHandler(store *AIProviderStore) *AIAdminHandler {
	return &AIAdminHandler{store: store}
}

func (s *AIProviderStore) List(ctx context.Context) ([]models.AIProviderConfig, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature::float8,
			last_test_datetime,
			last_test_status,
			last_error_message,
			created_datetime,
			updated_datetime
		FROM ai_provider_configs
		ORDER BY active DESC, created_datetime DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	providers := make([]models.AIProviderConfig, 0)
	for rows.Next() {
		record, err := scanStoredAIProviderConfig(rows)
		if err != nil {
			return nil, err
		}
		providers = append(providers, record.toPublic())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return providers, nil
}

func (s *AIProviderStore) Get(ctx context.Context, id string) (models.AIProviderConfig, error) {
	record, err := s.getStoredByID(ctx, s.pool, id)
	if err != nil {
		return models.AIProviderConfig{}, err
	}
	return record.toPublic(), nil
}

func (s *AIProviderStore) Create(ctx context.Context, req models.CreateAIProviderRequest) (models.AIProviderConfig, error) {
	input, err := normalizeCreateAIProviderRequest(req)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.AIProviderConfig{}, err
	}
	defer tx.Rollback(ctx)

	if input.Active && !input.Enabled {
		return models.AIProviderConfig{}, errProviderMustBeEnabledForActive
	}

	var record storedAIProviderConfig
	err = tx.QueryRow(ctx, `
		INSERT INTO ai_provider_configs (
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature
		)
		VALUES (
			$1, $2, $3, false, $4, $5, $6, $7, $8, $9, $10, $11
		)
		RETURNING
			id::text,
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature::float8,
			last_test_datetime,
			last_test_status,
			last_error_message,
			created_datetime,
			updated_datetime
	`,
		input.Name,
		input.ProviderType,
		input.Enabled,
		nullableStringArg(input.BaseURL),
		nullableStringArg(input.APIKeyValue),
		nullableStringArg(input.APIKeyEnvVar),
		input.ModelName,
		input.VisionEnabled,
		input.TimeoutSeconds,
		nullableIntArg(input.MaxOutputTokens),
		nullableFloatArg(input.Temperature),
	).Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.Enabled,
		&record.Active,
		&record.BaseURL,
		&record.APIKeyValue,
		&record.APIKeyEnvVar,
		&record.ModelName,
		&record.VisionEnabled,
		&record.TimeoutSeconds,
		&record.MaxOutputTokens,
		&record.Temperature,
		&record.LastTestDatetime,
		&record.LastTestStatus,
		&record.LastErrorMessage,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	if input.Active {
		record, err = s.setActiveTx(ctx, tx, record.ID)
		if err != nil {
			return models.AIProviderConfig{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.AIProviderConfig{}, err
	}

	return record.toPublic(), nil
}

func (s *AIProviderStore) Patch(ctx context.Context, id string, req models.PatchAIProviderRequest) (models.AIProviderConfig, error) {
	if !aiProviderPatchRequested(req) {
		return models.AIProviderConfig{}, errors.New("at least one provider field is required")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.AIProviderConfig{}, err
	}
	defer tx.Rollback(ctx)

	current, err := s.getStoredByID(ctx, tx, id)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	if req.ClearAPIKey.Set && req.ClearAPIKey.Value != nil && *req.ClearAPIKey.Value && req.APIKeyValue.Set && req.APIKeyValue.Value != nil && strings.TrimSpace(*req.APIKeyValue.Value) != "" {
		return models.AIProviderConfig{}, errors.New("api_key_value and clear_api_key cannot be used together")
	}

	updated, err := applyAIProviderPatch(current, req)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	if !updated.Enabled && updated.Active {
		return models.AIProviderConfig{}, errProviderMustBeEnabledForActive
	}

	err = tx.QueryRow(ctx, `
		UPDATE ai_provider_configs
		SET
			name = $2,
			provider_type = $3,
			enabled = $4,
			active = $5,
			base_url = $6,
			api_key_value = $7,
			api_key_env_var = $8,
			model_name = $9,
			vision_enabled = $10,
			timeout_seconds = $11,
			max_output_tokens = $12,
			temperature = $13,
			updated_datetime = now()
		WHERE id = $1
		RETURNING
			id::text,
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature::float8,
			last_test_datetime,
			last_test_status,
			last_error_message,
			created_datetime,
			updated_datetime
	`,
		id,
		updated.Name,
		updated.ProviderType,
		updated.Enabled,
		updated.Active,
		nullableStringArg(updated.BaseURL),
		nullableStringArg(updated.APIKeyValue),
		nullableStringArg(updated.APIKeyEnvVar),
		updated.ModelName,
		updated.VisionEnabled,
		updated.TimeoutSeconds,
		nullableIntArg(updated.MaxOutputTokens),
		nullableFloatArg(updated.Temperature),
	).Scan(
		&updated.ID,
		&updated.Name,
		&updated.ProviderType,
		&updated.Enabled,
		&updated.Active,
		&updated.BaseURL,
		&updated.APIKeyValue,
		&updated.APIKeyEnvVar,
		&updated.ModelName,
		&updated.VisionEnabled,
		&updated.TimeoutSeconds,
		&updated.MaxOutputTokens,
		&updated.Temperature,
		&updated.LastTestDatetime,
		&updated.LastTestStatus,
		&updated.LastErrorMessage,
		&updated.CreatedDatetime,
		&updated.UpdatedDatetime,
	)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	if req.Active.Set && req.Active.Value != nil && *req.Active.Value {
		updated, err = s.setActiveTx(ctx, tx, id)
		if err != nil {
			return models.AIProviderConfig{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return models.AIProviderConfig{}, err
	}

	return updated.toPublic(), nil
}

func (s *AIProviderStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM ai_provider_configs WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *AIProviderStore) SetActive(ctx context.Context, id string) (models.AIProviderConfig, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.AIProviderConfig{}, err
	}
	defer tx.Rollback(ctx)

	record, err := s.setActiveTx(ctx, tx, id)
	if err != nil {
		return models.AIProviderConfig{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.AIProviderConfig{}, err
	}

	return record.toPublic(), nil
}

func (s *AIProviderStore) SetAllInactive(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `UPDATE ai_provider_configs SET active = false, updated_datetime = now() WHERE active = true`)
	return err
}

func (s *AIProviderStore) Settings(ctx context.Context) (models.AISettings, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT
			id::text,
			name,
			provider_type,
			model_name
		FROM ai_provider_configs
		WHERE active = true AND enabled = true
		LIMIT 1
	`)

	var settings models.AISettings
	var providerID *string
	var providerName *string
	var providerType *string
	var modelName *string
	if err := row.Scan(&providerID, &providerName, &providerType, &modelName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return models.AISettings{AIAssistEnabled: false}, nil
		}
		return models.AISettings{}, err
	}

	settings.AIAssistEnabled = true
	settings.ActiveProviderID = providerID
	settings.ActiveProviderName = providerName
	settings.ActiveProviderType = providerType
	settings.ActiveModelName = modelName
	return settings, nil
}

func (s *AIProviderStore) PatchSettings(ctx context.Context, req models.PatchAISettingsRequest) (models.AISettings, error) {
	if !req.AIAssistEnabled.Set && !req.ActiveProviderID.Set {
		return models.AISettings{}, errors.New("at least one AI settings field is required")
	}

	if req.AIAssistEnabled.Set && req.AIAssistEnabled.Value != nil && !*req.AIAssistEnabled.Value {
		if err := s.SetAllInactive(ctx); err != nil {
			return models.AISettings{}, err
		}
		return s.Settings(ctx)
	}

	if req.ActiveProviderID.Set {
		if req.ActiveProviderID.Value == nil || strings.TrimSpace(*req.ActiveProviderID.Value) == "" {
			if err := s.SetAllInactive(ctx); err != nil {
				return models.AISettings{}, err
			}
			return s.Settings(ctx)
		}
		if _, err := s.SetActive(ctx, strings.TrimSpace(*req.ActiveProviderID.Value)); err != nil {
			return models.AISettings{}, err
		}
		return s.Settings(ctx)
	}

	settings, err := s.Settings(ctx)
	if err != nil {
		return models.AISettings{}, err
	}
	if !settings.AIAssistEnabled {
		return models.AISettings{}, errors.New("active_provider_id is required to enable AI assist")
	}
	return settings, nil
}

func (s *AIProviderStore) Test(ctx context.Context, id string) (models.AIProviderTestResult, error) {
	record, err := s.getStoredByID(ctx, s.pool, id)
	if err != nil {
		return models.AIProviderTestResult{}, err
	}

	testTime := time.Now().UTC()
	result := models.AIProviderTestResult{
		ProviderID:     record.ID,
		ProviderType:   record.ProviderType,
		ModelName:      record.ModelName,
		Status:         "failed",
		TestedDatetime: testTime,
	}

	testErr := testAIProvider(ctx, record)
	if testErr != nil {
		result.Message = testErr.Error()
	} else {
		result.Status = "success"
		result.Message = "Provider test succeeded."
	}

	if err := s.updateTestResult(ctx, id, result); err != nil {
		return models.AIProviderTestResult{}, err
	}

	return result, nil
}

func (s *AIProviderStore) updateTestResult(ctx context.Context, id string, result models.AIProviderTestResult) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE ai_provider_configs
		SET
			last_test_datetime = $2,
			last_test_status = $3,
			last_error_message = $4,
			updated_datetime = now()
		WHERE id = $1
	`,
		id,
		result.TestedDatetime,
		result.Status,
		testErrorMessageForStorage(result),
	)
	return err
}

func (s *AIProviderStore) setActiveTx(ctx context.Context, tx pgx.Tx, id string) (storedAIProviderConfig, error) {
	record, err := s.getStoredByID(ctx, tx, id)
	if err != nil {
		return storedAIProviderConfig{}, err
	}
	if !record.Enabled {
		return storedAIProviderConfig{}, errors.New("disabled provider cannot be set active")
	}

	if _, err := tx.Exec(ctx, `UPDATE ai_provider_configs SET active = false, updated_datetime = now() WHERE active = true`); err != nil {
		return storedAIProviderConfig{}, err
	}

	err = tx.QueryRow(ctx, `
		UPDATE ai_provider_configs
		SET active = true, updated_datetime = now()
		WHERE id = $1
		RETURNING
			id::text,
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature::float8,
			last_test_datetime,
			last_test_status,
			last_error_message,
			created_datetime,
			updated_datetime
	`, id).Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.Enabled,
		&record.Active,
		&record.BaseURL,
		&record.APIKeyValue,
		&record.APIKeyEnvVar,
		&record.ModelName,
		&record.VisionEnabled,
		&record.TimeoutSeconds,
		&record.MaxOutputTokens,
		&record.Temperature,
		&record.LastTestDatetime,
		&record.LastTestStatus,
		&record.LastErrorMessage,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	if err != nil {
		return storedAIProviderConfig{}, err
	}

	return record, nil
}

type aiQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *AIProviderStore) getStoredByID(ctx context.Context, q aiQuerier, id string) (storedAIProviderConfig, error) {
	row := q.QueryRow(ctx, `
		SELECT
			id::text,
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			api_key_env_var,
			model_name,
			vision_enabled,
			timeout_seconds,
			max_output_tokens,
			temperature::float8,
			last_test_datetime,
			last_test_status,
			last_error_message,
			created_datetime,
			updated_datetime
		FROM ai_provider_configs
		WHERE id = $1
	`, id)
	return scanStoredAIProviderConfig(row)
}

func scanStoredAIProviderConfig(scanner interface {
	Scan(dest ...any) error
}) (storedAIProviderConfig, error) {
	var record storedAIProviderConfig
	err := scanner.Scan(
		&record.ID,
		&record.Name,
		&record.ProviderType,
		&record.Enabled,
		&record.Active,
		&record.BaseURL,
		&record.APIKeyValue,
		&record.APIKeyEnvVar,
		&record.ModelName,
		&record.VisionEnabled,
		&record.TimeoutSeconds,
		&record.MaxOutputTokens,
		&record.Temperature,
		&record.LastTestDatetime,
		&record.LastTestStatus,
		&record.LastErrorMessage,
		&record.CreatedDatetime,
		&record.UpdatedDatetime,
	)
	return record, err
}

func (r storedAIProviderConfig) toPublic() models.AIProviderConfig {
	configured := r.hasConfiguredAPIKey()
	display := ""
	if configured {
		display = maskedAPIKeyDisplay
	}
	return models.AIProviderConfig{
		ID:               r.ID,
		Name:             r.Name,
		ProviderType:     r.ProviderType,
		Enabled:          r.Enabled,
		Active:           r.Active,
		BaseURL:          r.BaseURL,
		APIKeyConfigured: configured,
		APIKeyDisplay:    display,
		APIKeyEnvVar:     r.APIKeyEnvVar,
		ModelName:        r.ModelName,
		VisionEnabled:    r.VisionEnabled,
		TimeoutSeconds:   r.TimeoutSeconds,
		MaxOutputTokens:  r.MaxOutputTokens,
		Temperature:      r.Temperature,
		LastTestDatetime: r.LastTestDatetime,
		LastTestStatus:   r.LastTestStatus,
		LastErrorMessage: r.LastErrorMessage,
		CreatedDatetime:  r.CreatedDatetime,
		UpdatedDatetime:  r.UpdatedDatetime,
	}
}

func (r storedAIProviderConfig) hasConfiguredAPIKey() bool {
	if r.APIKeyValue != nil && strings.TrimSpace(*r.APIKeyValue) != "" {
		return true
	}
	if r.APIKeyEnvVar != nil && strings.TrimSpace(*r.APIKeyEnvVar) != "" {
		return true
	}
	return false
}

func (r storedAIProviderConfig) resolvedAPIKey() string {
	if r.APIKeyValue != nil && strings.TrimSpace(*r.APIKeyValue) != "" {
		return strings.TrimSpace(*r.APIKeyValue)
	}
	if r.APIKeyEnvVar != nil && strings.TrimSpace(*r.APIKeyEnvVar) != "" {
		return strings.TrimSpace(os.Getenv(strings.TrimSpace(*r.APIKeyEnvVar)))
	}
	return ""
}

func normalizeCreateAIProviderRequest(req models.CreateAIProviderRequest) (storedAIProviderConfig, error) {
	providerType := normalizeAIProviderType(req.ProviderType)
	if providerType == "" {
		return storedAIProviderConfig{}, errors.New("provider_type is required")
	}
	if !isSupportedAIProviderType(providerType) {
		return storedAIProviderConfig{}, errors.New("provider_type must be one of gemini, openai, ollama")
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		return storedAIProviderConfig{}, errors.New("name is required")
	}

	modelName := strings.TrimSpace(req.ModelName)
	if modelName == "" {
		return storedAIProviderConfig{}, errors.New("model_name is required")
	}

	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	active := false
	if req.Active != nil {
		active = *req.Active
	}
	visionEnabled := true
	if req.VisionEnabled != nil {
		visionEnabled = *req.VisionEnabled
	}
	timeoutSeconds := defaultAIProviderTimeoutSeconds
	if req.TimeoutSeconds != nil {
		timeoutSeconds = *req.TimeoutSeconds
	}
	if timeoutSeconds <= 0 {
		return storedAIProviderConfig{}, errors.New("timeout_seconds must be greater than 0")
	}

	var maxOutputTokens *int
	if req.MaxOutputTokens != nil {
		if *req.MaxOutputTokens <= 0 {
			return storedAIProviderConfig{}, errors.New("max_output_tokens must be greater than 0")
		}
		maxOutputTokens = req.MaxOutputTokens
	}

	var temperature *float64
	if req.Temperature != nil {
		if *req.Temperature < 0 || *req.Temperature > 2 {
			return storedAIProviderConfig{}, errors.New("temperature must be between 0 and 2")
		}
		temperature = req.Temperature
	}

	baseURL := normalizeOptionalString(req.BaseURL)
	apiKeyValue := normalizeOptionalString(req.APIKeyValue)
	apiKeyEnvVar := normalizeOptionalString(req.APIKeyEnvVar)

	if providerType == "ollama" && baseURL == nil {
		defaultBase := defaultOllamaBaseURL
		baseURL = &defaultBase
	}

	return storedAIProviderConfig{
		Name:            name,
		ProviderType:    providerType,
		Enabled:         enabled,
		Active:          active,
		BaseURL:         baseURL,
		APIKeyValue:     apiKeyValue,
		APIKeyEnvVar:    apiKeyEnvVar,
		ModelName:       modelName,
		VisionEnabled:   visionEnabled,
		TimeoutSeconds:  timeoutSeconds,
		MaxOutputTokens: maxOutputTokens,
		Temperature:     temperature,
	}, nil
}

func applyAIProviderPatch(current storedAIProviderConfig, req models.PatchAIProviderRequest) (storedAIProviderConfig, error) {
	updated := current

	if req.Name.Set {
		if req.Name.Value == nil || strings.TrimSpace(*req.Name.Value) == "" {
			return storedAIProviderConfig{}, errors.New("name is required")
		}
		updated.Name = strings.TrimSpace(*req.Name.Value)
	}

	if req.ProviderType.Set {
		if req.ProviderType.Value == nil || strings.TrimSpace(*req.ProviderType.Value) == "" {
			return storedAIProviderConfig{}, errors.New("provider_type is required")
		}
		providerType := normalizeAIProviderType(*req.ProviderType.Value)
		if !isSupportedAIProviderType(providerType) {
			return storedAIProviderConfig{}, errors.New("provider_type must be one of gemini, openai, ollama")
		}
		updated.ProviderType = providerType
	}

	if req.Enabled.Set {
		if req.Enabled.Value == nil {
			return storedAIProviderConfig{}, errors.New("enabled must be true or false")
		}
		updated.Enabled = *req.Enabled.Value
	}

	if req.Active.Set {
		if req.Active.Value == nil {
			return storedAIProviderConfig{}, errors.New("active must be true or false")
		}
		updated.Active = *req.Active.Value
	}

	if req.BaseURL.Set {
		updated.BaseURL = normalizeOptionalString(req.BaseURL.Value)
	}

	if req.APIKeyValue.Set {
		updated.APIKeyValue = normalizeOptionalString(req.APIKeyValue.Value)
	}

	if req.ClearAPIKey.Set && req.ClearAPIKey.Value != nil && *req.ClearAPIKey.Value {
		updated.APIKeyValue = nil
	}

	if req.APIKeyEnvVar.Set {
		updated.APIKeyEnvVar = normalizeOptionalString(req.APIKeyEnvVar.Value)
	}

	if req.ModelName.Set {
		if req.ModelName.Value == nil || strings.TrimSpace(*req.ModelName.Value) == "" {
			return storedAIProviderConfig{}, errors.New("model_name is required")
		}
		updated.ModelName = strings.TrimSpace(*req.ModelName.Value)
	}

	if req.VisionEnabled.Set {
		if req.VisionEnabled.Value == nil {
			return storedAIProviderConfig{}, errors.New("vision_enabled must be true or false")
		}
		updated.VisionEnabled = *req.VisionEnabled.Value
	}

	if req.TimeoutSeconds.Set {
		if req.TimeoutSeconds.Value == nil || *req.TimeoutSeconds.Value <= 0 {
			return storedAIProviderConfig{}, errors.New("timeout_seconds must be greater than 0")
		}
		updated.TimeoutSeconds = *req.TimeoutSeconds.Value
	}

	if req.MaxOutputTokens.Set {
		if req.MaxOutputTokens.Value == nil {
			updated.MaxOutputTokens = nil
		} else {
			if *req.MaxOutputTokens.Value <= 0 {
				return storedAIProviderConfig{}, errors.New("max_output_tokens must be greater than 0")
			}
			value := *req.MaxOutputTokens.Value
			updated.MaxOutputTokens = &value
		}
	}

	if req.Temperature.Set {
		if req.Temperature.Value == nil {
			updated.Temperature = nil
		} else {
			if *req.Temperature.Value < 0 || *req.Temperature.Value > 2 {
				return storedAIProviderConfig{}, errors.New("temperature must be between 0 and 2")
			}
			value := *req.Temperature.Value
			updated.Temperature = &value
		}
	}

	if updated.ProviderType == "ollama" && updated.BaseURL == nil {
		defaultBase := defaultOllamaBaseURL
		updated.BaseURL = &defaultBase
	}

	return updated, nil
}

func normalizeAIProviderType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func isSupportedAIProviderType(value string) bool {
	switch value {
	case "gemini", "openai", "ollama":
		return true
	default:
		return false
	}
}

func normalizeOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func nullableIntArg(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableFloatArg(value *float64) any {
	if value == nil {
		return nil
	}
	return *value
}

func aiProviderPatchRequested(req models.PatchAIProviderRequest) bool {
	return req.Name.Set ||
		req.ProviderType.Set ||
		req.Enabled.Set ||
		req.Active.Set ||
		req.BaseURL.Set ||
		req.APIKeyValue.Set ||
		req.APIKeyEnvVar.Set ||
		req.ModelName.Set ||
		req.VisionEnabled.Set ||
		req.TimeoutSeconds.Set ||
		req.MaxOutputTokens.Set ||
		req.Temperature.Set ||
		req.ClearAPIKey.Set
}

func testAIProvider(ctx context.Context, provider storedAIProviderConfig) error {
	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	testCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	switch provider.ProviderType {
	case "openai":
		return testOpenAIProvider(testCtx, provider)
	case "gemini":
		return testGeminiProvider(testCtx, provider)
	case "ollama":
		return testOllamaProvider(testCtx, provider)
	default:
		return errors.New("unsupported provider type")
	}
}

func testOpenAIProvider(ctx context.Context, provider storedAIProviderConfig) error {
	apiKey := provider.resolvedAPIKey()
	if apiKey == "" {
		return errors.New("OpenAI provider test requires api_key_value or api_key_env_var")
	}

	baseURL := openAIDefaultBaseURL
	if provider.BaseURL != nil {
		baseURL = strings.TrimRight(*provider.BaseURL, "/")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	payload := map[string]any{
		"model": provider.ModelName,
		"input": "Reply with OK only.",
	}
	if provider.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *provider.MaxOutputTokens
	} else {
		payload["max_output_tokens"] = 16
	}
	if provider.Temperature != nil {
		payload["temperature"] = *provider.Temperature
	}

	return doJSONProviderTest(ctx, provider.ProviderType, baseURL+"/responses", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, payload)
}

func testGeminiProvider(ctx context.Context, provider storedAIProviderConfig) error {
	apiKey := provider.resolvedAPIKey()
	if apiKey == "" {
		return errors.New("Gemini provider test requires api_key_value or api_key_env_var")
	}

	baseURL := geminiDefaultBaseURL
	if provider.BaseURL != nil {
		baseURL = strings.TrimRight(*provider.BaseURL, "/")
	}
	if !strings.Contains(baseURL, "/v1beta") {
		baseURL += "/v1beta"
	}

	modelPath := provider.ModelName
	if !strings.HasPrefix(modelPath, "models/") {
		modelPath = "models/" + modelPath
	}

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": []map[string]any{
					{"text": "Reply with OK only."},
				},
			},
		},
		"generationConfig": map[string]any{
			"maxOutputTokens": 16,
		},
	}
	if provider.MaxOutputTokens != nil {
		payload["generationConfig"].(map[string]any)["maxOutputTokens"] = *provider.MaxOutputTokens
	}
	if provider.Temperature != nil {
		payload["generationConfig"].(map[string]any)["temperature"] = *provider.Temperature
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, modelPath, url.QueryEscape(apiKey))
	return doJSONProviderTest(ctx, provider.ProviderType, endpoint, nil, payload)
}

func testOllamaProvider(ctx context.Context, provider storedAIProviderConfig) error {
	baseURL := defaultOllamaBaseURL
	if provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*provider.BaseURL), "/")
	}

	payload := map[string]any{
		"model":  provider.ModelName,
		"prompt": "Reply with OK only.",
		"stream": false,
	}
	if provider.Temperature != nil || provider.MaxOutputTokens != nil {
		options := map[string]any{}
		if provider.Temperature != nil {
			options["temperature"] = *provider.Temperature
		}
		if provider.MaxOutputTokens != nil {
			options["num_predict"] = *provider.MaxOutputTokens
		}
		payload["options"] = options
	}

	return doJSONProviderTest(ctx, provider.ProviderType, baseURL+"/api/generate", nil, payload)
}

func doJSONProviderTest(ctx context.Context, providerType string, endpoint string, extraHeaders map[string]string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := providerType + " provider test failed with HTTP " + resp.Status
		if bodyMessage := compactHTTPError(resp.Body); bodyMessage != "" {
			message += ": " + bodyMessage
		}
		return errors.New(message)
	}

	return nil
}

func compactHTTPError(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, 2048))
	if err != nil {
		return ""
	}
	message := strings.TrimSpace(string(raw))
	message = strings.ReplaceAll(message, "\n", " ")
	message = strings.ReplaceAll(message, "\r", " ")
	if len(message) > 400 {
		message = message[:400]
	}
	return message
}

func testErrorMessageForStorage(result models.AIProviderTestResult) any {
	if result.Status == "success" {
		return nil
	}
	return result.Message
}

func (h *AIAdminHandler) ListProviders(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	providers, err := h.store.List(ctx)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load AI providers")
		return
	}
	respond.JSON(w, http.StatusOK, models.ListAIProvidersResponse{Providers: providers})
}

func (h *AIAdminHandler) CreateProvider(w http.ResponseWriter, r *http.Request) {
	var req models.CreateAIProviderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Create(ctx, req)
	if err != nil {
		h.respondAIProviderError(w, err, "failed to create AI provider")
		return
	}

	respond.JSON(w, http.StatusCreated, models.GetAIProviderResponse{Provider: provider})
}

func (h *AIAdminHandler) GetProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Get(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "AI provider was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load AI provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetAIProviderResponse{Provider: provider})
}

func (h *AIAdminHandler) PatchProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	var req models.PatchAIProviderRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.Patch(ctx, id, req)
	if err != nil {
		h.respondAIProviderError(w, err, "failed to update AI provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetAIProviderResponse{Provider: provider})
}

func (h *AIAdminHandler) DeleteProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	if err := h.store.Delete(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "AI provider was not found")
			return
		}
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete AI provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.DeleteAIProviderResponse{ProviderID: id, Deleted: true})
}

func (h *AIAdminHandler) SetActiveProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	provider, err := h.store.SetActive(ctx, id)
	if err != nil {
		h.respondAIProviderError(w, err, "failed to set active AI provider")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetAIProviderResponse{Provider: provider})
}

func (h *AIAdminHandler) TestProvider(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(id) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "provider id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 70*time.Second)
	defer cancel()

	result, err := h.store.Test(ctx, id)
	if err != nil {
		h.respondAIProviderError(w, err, "failed to test AI provider")
		return
	}

	respond.JSON(w, http.StatusOK, result)
}

func (h *AIAdminHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	settings, err := h.store.Settings(ctx)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load AI settings")
		return
	}
	respond.JSON(w, http.StatusOK, models.GetAISettingsResponse{Settings: settings})
}

func (h *AIAdminHandler) PatchSettings(w http.ResponseWriter, r *http.Request) {
	var req models.PatchAISettingsRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	settings, err := h.store.PatchSettings(ctx, req)
	if err != nil {
		h.respondAIProviderError(w, err, "failed to update AI settings")
		return
	}

	respond.JSON(w, http.StatusOK, models.GetAISettingsResponse{Settings: settings})
}

func (h *AIAdminHandler) respondAIProviderError(w http.ResponseWriter, err error, fallback string) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "AI provider was not found")
	case errors.Is(err, errProviderMustBeEnabledForActive):
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
	default:
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
	}
}
