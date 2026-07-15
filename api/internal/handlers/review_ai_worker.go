package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const reviewAssistPromptVersion = "fastsell-review-assist-v1"

type ReviewAIAssistWorkerConfig struct {
	ScanInterval  time.Duration
	MaxImages     int
	MaxImageBytes int64
	SafeRoots     []string
	BeginWrite    func() (func(), bool)
}

type ReviewAIAssistWorker struct {
	pool *pgxpool.Pool
	cfg  ReviewAIAssistWorkerConfig
}

type reviewAssistJob struct {
	UploadGroupID    string
	ProviderConfigID *string
}

type reviewAssistGroupRecord struct {
	UploadGroupID    string
	Title            *string
	Notes            *string
	Status           string
	AIAssistStatus   string
	ProviderConfigID *string
}

type reviewAssistImage struct {
	ID               string
	MimeType         string
	OriginalFilename string
	Data             []byte
}

type reviewAssistSuggestion struct {
	Title       string
	Description string
	ApproxValue *string
}

type reviewAssistInput struct {
	Group    reviewAssistGroupRecord
	Images   []reviewAssistImage
	UserHint *string
}

var reviewAssistHints sync.Map

func NewReviewAIAssistWorker(pool *pgxpool.Pool, cfg ReviewAIAssistWorkerConfig) *ReviewAIAssistWorker {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 2 * time.Second
	}
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = 6
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = 10 * 1024 * 1024
	}

	return &ReviewAIAssistWorker{
		pool: pool,
		cfg:  cfg,
	}
}

func (w *ReviewAIAssistWorker) Run(ctx context.Context) {
	log.Printf(
		"review AI assist worker started scan_interval=%s max_images=%d max_image_bytes=%d",
		w.cfg.ScanInterval,
		w.cfg.MaxImages,
		w.cfg.MaxImageBytes,
	)

	if err := w.resetStuckProcessing(ctx); err != nil {
		log.Printf("review AI assist worker recovery failed: %v", err)
	}

	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()

	if err := w.scanOnce(ctx); err != nil {
		log.Printf("review AI assist worker scan failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Print("review AI assist worker stopped")
			return
		case <-ticker.C:
			if err := w.scanOnce(ctx); err != nil {
				log.Printf("review AI assist worker scan failed: %v", err)
			}
		}
	}
}

func (w *ReviewAIAssistWorker) resetStuckProcessing(ctx context.Context) error {
	recoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	_, err := w.pool.Exec(recoveryCtx, `
		UPDATE upload_groups
		SET
			ai_assist_status = 'queued',
			ai_assist_error_message = 'requeued after API restart',
			ai_assist_started_datetime = NULL,
			ai_assist_completed_datetime = NULL,
			updated_datetime = now()
		WHERE ai_assist_status = 'processing'
	`)
	return err
}

func (w *ReviewAIAssistWorker) scanOnce(ctx context.Context) error {
	if w.cfg.BeginWrite != nil {
		done, ok := w.cfg.BeginWrite()
		if !ok {
			return nil
		}
		defer done()
	}
	job, err := w.claimNextJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	if err := w.processJob(ctx, job); err != nil {
		log.Printf("review AI assist job %s failed: %v", job.UploadGroupID, err)
	}

	return nil
}

func (w *ReviewAIAssistWorker) claimNextJob(ctx context.Context) (reviewAssistJob, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return reviewAssistJob{}, err
	}
	defer tx.Rollback(ctx)

	var job reviewAssistJob
	err = tx.QueryRow(ctx, `
		SELECT id::text, ai_assist_provider_config_id::text
		FROM upload_groups
		WHERE ai_assist_status = 'queued'
		ORDER BY ai_assist_requested_datetime ASC NULLS LAST, created_datetime ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&job.UploadGroupID, &job.ProviderConfigID)
	if err != nil {
		return reviewAssistJob{}, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE upload_groups
		SET
			ai_assist_status = 'processing',
			ai_assist_started_datetime = now(),
			ai_assist_completed_datetime = NULL,
			ai_assist_error_message = NULL,
			updated_datetime = now()
		WHERE id = $1 AND ai_assist_status = 'queued'
	`, job.UploadGroupID)
	if err != nil {
		return reviewAssistJob{}, err
	}
	if tag.RowsAffected() != 1 {
		return reviewAssistJob{}, pgx.ErrNoRows
	}

	if err := tx.Commit(ctx); err != nil {
		return reviewAssistJob{}, err
	}

	return job, nil
}

func (w *ReviewAIAssistWorker) processJob(ctx context.Context, job reviewAssistJob) error {
	group, err := w.loadGroup(ctx, job.UploadGroupID)
	if err != nil {
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}
	if group.Status == "approved" {
		return nil
	}
	if group.ProviderConfigID == nil || strings.TrimSpace(*group.ProviderConfigID) == "" {
		err := errors.New("AI Assist provider configuration is missing")
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}

	providerStore := NewAIProviderStore(w.pool)
	provider, err := providerStore.getStoredByID(ctx, w.pool, *group.ProviderConfigID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("AI Assist provider configuration no longer exists")
		}
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}
	if err := validateReviewAssistProvider(provider); err != nil {
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}

	images, err := w.loadUsableImages(ctx, job.UploadGroupID)
	if err != nil {
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}
	userHint := consumeReviewAssistHint(job.UploadGroupID)

	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	assistCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	suggestion, err := analyzeReviewGroup(assistCtx, provider, reviewAssistInput{
		Group:    group,
		Images:   images,
		UserHint: userHint,
	})
	if err != nil {
		_ = w.markFailed(ctx, job.UploadGroupID, err.Error())
		return err
	}

	if err := w.markSucceeded(ctx, job.UploadGroupID, suggestion); err != nil {
		return err
	}

	log.Printf("review AI assist completed upload_group_id=%s provider=%s model=%s", job.UploadGroupID, provider.ProviderType, provider.ModelName)
	return nil
}

func (w *ReviewAIAssistWorker) loadGroup(ctx context.Context, groupID string) (reviewAssistGroupRecord, error) {
	var group reviewAssistGroupRecord
	err := w.pool.QueryRow(ctx, `
		SELECT
			id::text,
			title,
			notes,
			status,
			ai_assist_status,
			ai_assist_provider_config_id::text
		FROM upload_groups
		WHERE id = $1
	`, groupID).Scan(
		&group.UploadGroupID,
		&group.Title,
		&group.Notes,
		&group.Status,
		&group.AIAssistStatus,
		&group.ProviderConfigID,
	)
	return group, err
}

func (w *ReviewAIAssistWorker) loadUsableImages(ctx context.Context, groupID string) ([]reviewAssistImage, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT
			id::text,
			file_path,
			mime_type,
			original_filename,
			file_size_bytes
		FROM image_assets
		WHERE upload_group_id = $1
			AND status = 'processed'
			AND item_id IS NULL
		ORDER BY upload_order ASC, created_datetime ASC
	`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]reviewAssistImage, 0, w.cfg.MaxImages)
	for rows.Next() {
		if len(images) >= w.cfg.MaxImages {
			break
		}

		var imageID string
		var filePath string
		var mimeType *string
		var originalFilename *string
		var fileSizeBytes *int64
		if err := rows.Scan(&imageID, &filePath, &mimeType, &originalFilename, &fileSizeBytes); err != nil {
			return nil, err
		}

		cleanPath := filepath.Clean(filePath)
		if !isSafeManagedPath(cleanPath, w.cfg.SafeRoots) {
			continue
		}
		if fileSizeBytes != nil && *fileSizeBytes > w.cfg.MaxImageBytes {
			continue
		}

		normalizedMime := normalizeReviewAssistMIME(mimeType, cleanPath)
		if normalizedMime == "" {
			continue
		}

		data, err := os.ReadFile(cleanPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read processed image %s", imageID)
		}
		if int64(len(data)) == 0 {
			continue
		}
		if int64(len(data)) > w.cfg.MaxImageBytes {
			continue
		}

		images = append(images, reviewAssistImage{
			ID:               imageID,
			MimeType:         normalizedMime,
			OriginalFilename: derefString(originalFilename),
			Data:             data,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, errors.New("AI Assist found no usable processed JPEG or PNG images")
	}

	return images, nil
}

func (w *ReviewAIAssistWorker) markSucceeded(ctx context.Context, groupID string, suggestion reviewAssistSuggestion) error {
	tag, err := w.pool.Exec(ctx, `
		UPDATE upload_groups
		SET
			ai_assist_status = 'succeeded',
			ai_assist_error_message = NULL,
			ai_assist_completed_datetime = now(),
			ai_suggested_title = $2,
			ai_suggested_description = $3,
			ai_suggested_approx_value = CASE WHEN $4::text IS NULL THEN NULL ELSE $4::numeric(12,2) END,
			updated_datetime = now()
		WHERE id = $1 AND status <> 'approved'
	`, groupID, suggestion.Title, suggestion.Description, nullableStringArg(suggestion.ApproxValue))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func (w *ReviewAIAssistWorker) markFailed(ctx context.Context, groupID string, message string) error {
	message = truncateReviewAssistText(strings.TrimSpace(message), 300)
	if message == "" {
		message = "AI Assist failed"
	}

	tag, err := w.pool.Exec(ctx, `
		UPDATE upload_groups
		SET
			ai_assist_status = 'failed',
			ai_assist_error_message = $2,
			ai_assist_completed_datetime = now(),
			updated_datetime = now()
		WHERE id = $1 AND status <> 'approved'
	`, groupID, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func validateReviewAssistProvider(provider storedAIProviderConfig) error {
	if !provider.Enabled {
		return errors.New("active AI provider is disabled")
	}
	if !provider.VisionEnabled {
		return errors.New("active AI provider does not have vision enabled")
	}
	if strings.TrimSpace(provider.ModelName) == "" {
		return errors.New("active AI provider is missing model_name")
	}

	switch provider.ProviderType {
	case "ollama":
		baseURL := strings.TrimSpace(derefString(provider.BaseURL))
		if baseURL == "" {
			return errors.New("active Ollama provider requires base_url")
		}
	case "gemini":
		if provider.resolvedAPIKey() == "" {
			return errors.New("active Gemini provider requires api_key_value or api_key_env_var")
		}
	case "openai":
		if provider.resolvedAPIKey() == "" {
			return errors.New("active OpenAI provider requires api_key_value or api_key_env_var")
		}
	default:
		return errors.New("unsupported AI provider type")
	}

	return nil
}

func rememberReviewAssistHint(groupID string, userHint *string) {
	hint := strings.TrimSpace(derefString(userHint))
	if hint == "" {
		forgetReviewAssistHint(groupID)
		return
	}
	reviewAssistHints.Store(groupID, hint)
}

func consumeReviewAssistHint(groupID string) *string {
	value, ok := reviewAssistHints.LoadAndDelete(groupID)
	if !ok {
		return nil
	}
	hint, ok := value.(string)
	if !ok {
		return nil
	}
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return nil
	}
	return &hint
}

func forgetReviewAssistHint(groupID string) {
	reviewAssistHints.Delete(groupID)
}

func normalizeReviewAssistMIME(mimeType *string, filePath string) string {
	normalized := strings.ToLower(strings.TrimSpace(derefString(mimeType)))
	switch normalized {
	case "image/jpeg", "image/jpg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	}

	switch strings.ToLower(filepath.Ext(filePath)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	default:
		return ""
	}
}

func analyzeReviewGroup(ctx context.Context, provider storedAIProviderConfig, input reviewAssistInput) (reviewAssistSuggestion, error) {
	prompt := buildReviewAssistPrompt(input.Group.Title, input.Group.Notes, input.UserHint)

	var rawText string
	var err error
	switch provider.ProviderType {
	case "ollama":
		rawText, err = callOllamaReviewAssist(ctx, provider, prompt, input.Images)
	case "gemini":
		rawText, err = callGeminiReviewAssist(ctx, provider, prompt, input.Images)
	case "openai":
		rawText, err = callOpenAIReviewAssist(ctx, provider, prompt, input.Images)
	default:
		err = errors.New("unsupported AI provider type")
	}
	if err != nil {
		return reviewAssistSuggestion{}, err
	}

	return parseReviewAssistSuggestion(rawText)
}

func buildReviewAssistPrompt(existingTitle *string, existingNotes *string, userHint *string) string {
	title := strings.TrimSpace(derefString(existingTitle))
	notes := strings.TrimSpace(derefString(existingNotes))
	hint := strings.TrimSpace(derefString(userHint))

	hintSection := ""
	if hint != "" {
		hintSection = fmt.Sprintf(`
The user provided this optional hint for this AI Assist run:
%q

Use the hint as helpful context when identifying the item and writing the title, description, category, condition notes, and estimated value.
Do not blindly assume the hint is correct.
If the hint conflicts with visible evidence in the image, preserve uncertainty in the response.
`, hint)
	}

	return fmt.Sprintf(`Prompt version: %s

You are helping create a listing for eBay, Facebook Marketplace, or Craigslist.
Identify the object using visible evidence from the images.
Prefer manufacturer and model when visible.
Do not invent a manufacturer or model when uncertain.
Title format should be:
<Manufacturer> <Model> <Item Type> <Key Identifying Details>

Description should explain:
- what the object is
- what it is used for
- what systems/devices it is used with
- visible condition
- likely era or manufacture period when reasonable
- approximate resale value
- important uncertainty

Price is approximate and image-based.
Do not claim live market research.
Do not claim tested or working unless image evidence proves it.
Do not say rare unless there is evidence.
Return only strict JSON.
No markdown fences.
No extra commentary.

Required JSON:
{
  "title": "string",
  "description": "string",
  "approx_value": 0
}

Optional context from the upload group:
- Existing title: %q
- Existing notes: %q
%s`, reviewAssistPromptVersion, title, notes, hintSection)
}

func callOllamaReviewAssist(ctx context.Context, provider storedAIProviderConfig, prompt string, images []reviewAssistImage) (string, error) {
	baseURL := defaultOllamaBaseURL
	if provider.BaseURL != nil && strings.TrimSpace(*provider.BaseURL) != "" {
		baseURL = strings.TrimRight(strings.TrimSpace(*provider.BaseURL), "/")
	}

	imagePayload := make([]string, 0, len(images))
	for _, image := range images {
		imagePayload = append(imagePayload, base64.StdEncoding.EncodeToString(image.Data))
	}

	payload := map[string]any{
		"model":  provider.ModelName,
		"prompt": prompt,
		"stream": false,
		"format": "json",
		"images": imagePayload,
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

	body, err := doReviewAssistJSONRequest(ctx, http.MethodPost, baseURL+"/api/generate", nil, payload)
	if err != nil {
		return "", err
	}

	var response struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", errors.New("Ollama returned an unreadable response")
	}
	if strings.TrimSpace(response.Response) == "" {
		return "", errors.New("Ollama returned an empty response")
	}
	return response.Response, nil
}

func callGeminiReviewAssist(ctx context.Context, provider storedAIProviderConfig, prompt string, images []reviewAssistImage) (string, error) {
	apiKey := provider.resolvedAPIKey()
	if apiKey == "" {
		return "", errors.New("Gemini provider requires api_key_value or api_key_env_var")
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

	parts := make([]map[string]any, 0, len(images)+1)
	parts = append(parts, map[string]any{"text": prompt})
	for _, image := range images {
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": image.MimeType,
				"data":      base64.StdEncoding.EncodeToString(image.Data),
			},
		})
	}

	payload := map[string]any{
		"contents": []map[string]any{
			{
				"parts": parts,
			},
		},
		"generationConfig": map[string]any{
			"responseMimeType": "application/json",
			"maxOutputTokens":  512,
		},
	}
	if provider.MaxOutputTokens != nil {
		payload["generationConfig"].(map[string]any)["maxOutputTokens"] = *provider.MaxOutputTokens
	}
	if provider.Temperature != nil {
		payload["generationConfig"].(map[string]any)["temperature"] = *provider.Temperature
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, modelPath, url.QueryEscape(apiKey))
	body, err := doReviewAssistJSONRequest(ctx, http.MethodPost, endpoint, nil, payload)
	if err != nil {
		return "", err
	}

	var response struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", errors.New("Gemini returned an unreadable response")
	}
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return part.Text, nil
			}
		}
	}
	return "", errors.New("Gemini returned an empty response")
}

func callOpenAIReviewAssist(ctx context.Context, provider storedAIProviderConfig, prompt string, images []reviewAssistImage) (string, error) {
	apiKey := provider.resolvedAPIKey()
	if apiKey == "" {
		return "", errors.New("OpenAI provider requires api_key_value or api_key_env_var")
	}

	baseURL := openAIDefaultBaseURL
	if provider.BaseURL != nil {
		baseURL = strings.TrimRight(*provider.BaseURL, "/")
	}
	if !strings.HasSuffix(baseURL, "/v1") {
		baseURL += "/v1"
	}

	content := make([]map[string]any, 0, len(images)+1)
	content = append(content, map[string]any{
		"type": "input_text",
		"text": prompt,
	})
	for _, image := range images {
		content = append(content, map[string]any{
			"type":      "input_image",
			"image_url": "data:" + image.MimeType + ";base64," + base64.StdEncoding.EncodeToString(image.Data),
		})
	}

	payload := map[string]any{
		"model": provider.ModelName,
		"input": []map[string]any{
			{
				"role":    "user",
				"content": content,
			},
		},
		"max_output_tokens": 512,
	}
	if provider.MaxOutputTokens != nil {
		payload["max_output_tokens"] = *provider.MaxOutputTokens
	}
	if provider.Temperature != nil {
		payload["temperature"] = *provider.Temperature
	}

	body, err := doReviewAssistJSONRequest(ctx, http.MethodPost, baseURL+"/responses", map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, payload)
	if err != nil {
		return "", err
	}

	var response struct {
		Output []struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", errors.New("OpenAI returned an unreadable response")
	}
	for _, output := range response.Output {
		for _, contentPart := range output.Content {
			if strings.TrimSpace(contentPart.Text) != "" {
				return contentPart.Text, nil
			}
		}
	}
	return "", errors.New("OpenAI returned an empty response")
}

func doReviewAssistJSONRequest(ctx context.Context, method string, endpoint string, extraHeaders map[string]string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for key, value := range extraHeaders {
		req.Header.Set(key, value)
	}

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := "AI provider request failed with HTTP " + resp.Status
		if bodyMessage := compactHTTPError(bytes.NewReader(responseBody)); bodyMessage != "" {
			message += ": " + bodyMessage
		}
		return nil, errors.New(message)
	}

	return responseBody, nil
}

func parseReviewAssistSuggestion(rawText string) (reviewAssistSuggestion, error) {
	payload := extractJSONObject(rawText)
	if strings.TrimSpace(payload) == "" {
		return reviewAssistSuggestion{}, errors.New("AI provider returned no JSON suggestion")
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &raw); err != nil {
		return reviewAssistSuggestion{}, errors.New("AI provider returned invalid JSON")
	}

	var title string
	if err := json.Unmarshal(raw["title"], &title); err != nil {
		return reviewAssistSuggestion{}, errors.New("AI suggestion is missing title")
	}
	title = truncateReviewAssistText(strings.TrimSpace(title), 240)
	if title == "" {
		return reviewAssistSuggestion{}, errors.New("AI suggestion title is empty")
	}

	var description string
	if err := json.Unmarshal(raw["description"], &description); err != nil {
		return reviewAssistSuggestion{}, errors.New("AI suggestion is missing description")
	}
	description = truncateReviewAssistText(strings.TrimSpace(description), 4000)
	if description == "" {
		return reviewAssistSuggestion{}, errors.New("AI suggestion description is empty")
	}

	approxValue, err := parseReviewAssistApproxValue(raw["approx_value"])
	if err != nil {
		return reviewAssistSuggestion{}, err
	}

	return reviewAssistSuggestion{
		Title:       title,
		Description: description,
		ApproxValue: approxValue,
	}, nil
}

func parseReviewAssistApproxValue(raw json.RawMessage) (*string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}

	var number json.Number
	if err := json.Unmarshal(raw, &number); err == nil {
		value, err := strconv.ParseFloat(number.String(), 64)
		if err != nil {
			return nil, errors.New("AI suggestion approx_value is invalid")
		}
		if value < 0 {
			return nil, errors.New("AI suggestion approx_value must be non-negative")
		}
		formatted := fmt.Sprintf("%.2f", value)
		return &formatted, nil
	}

	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return nil, errors.New("AI suggestion approx_value is invalid")
	}
	text = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(text, "$", ""), ",", ""))
	if text == "" {
		return nil, nil
	}
	if !approxValuePattern.MatchString(text) {
		return nil, errors.New("AI suggestion approx_value is invalid")
	}
	return &text, nil
}

func extractJSONObject(input string) string {
	start := strings.IndexByte(input, '{')
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false
	for index := start; index < len(input); index++ {
		ch := input[index]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return input[start : index+1]
			}
		}
	}

	return ""
}

func truncateReviewAssistText(value string, limit int) string {
	if limit <= 0 || len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit])
}

func (s *AIProviderStore) getActiveProvider(ctx context.Context, q aiQuerier) (storedAIProviderConfig, error) {
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
		WHERE active = true
		LIMIT 1
	`)
	return scanStoredAIProviderConfig(row)
}
