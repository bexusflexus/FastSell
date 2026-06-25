package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type WholeSceneAnalysisWorkerConfig struct {
	ScanInterval   time.Duration
	MaxImages      int
	MaxImageBytes  int64
	SafeRoots      []string
	OriginalsDir   string
	ThumbnailsDir  string
	NormalizedDir  string
	DiagnosticsDir string
}

type WholeSceneAnalysisWorker struct {
	pool *pgxpool.Pool
	cfg  WholeSceneAnalysisWorkerConfig
}

type wholeSceneAnalysisJob struct {
	RunID  string
	ScanID string
}

type wholeSceneCandidateAssistJob struct {
	CandidateID      string
	ScanID           string
	ProviderConfigID *string
}

type wholeSceneAnalysisRunRecord struct {
	ID                string
	ScanID            string
	Hint              *string
	ProviderConfigID  *string
	ProviderType      string
	ModelName         string
	PromptVersion     string
	PromptText        *string
	QueuedDatetime    time.Time
	StartedDatetime   *time.Time
	CompletedDatetime *time.Time
}

type wholeSceneAnalysisImage struct {
	ID               string
	SourceImageIndex int
	MimeType         string
	OriginalFilename string
	FileHash         string
	FileSizeBytes    int64
	Data             []byte
}

type wholeSceneScanImageSource struct {
	ID              string
	ImageAssetID    string
	SortOrder       int
	UploadSessionID string
	FilePath        string
	StoredFilename  string
	MimeType        string
}

type wholeSceneScanImageSources struct {
	byIndex        map[int]wholeSceneScanImageSource
	byScanImageID  map[string]wholeSceneScanImageSource
	byImageAssetID map[string]wholeSceneScanImageSource
}

type persistedWholeSceneAppearance struct {
	ID               string
	ScanImageID      string
	SourceImageIndex int
	Source           wholeSceneScanImageSource
	BoundingBox      wholeSceneParsedBoundingBox
	DiagnosticIndex  *int
}

type wholeSceneProviderError struct {
	message      string
	responseBody []byte
}

func (e wholeSceneProviderError) Error() string {
	return e.message
}

func NewWholeSceneAnalysisWorker(pool *pgxpool.Pool, cfg WholeSceneAnalysisWorkerConfig) *WholeSceneAnalysisWorker {
	if cfg.ScanInterval <= 0 {
		cfg.ScanInterval = 2 * time.Second
	}
	if cfg.MaxImages <= 0 {
		cfg.MaxImages = wholeSceneMaxImageCount
	}
	if cfg.MaxImageBytes <= 0 {
		cfg.MaxImageBytes = 10 * 1024 * 1024
	}

	return &WholeSceneAnalysisWorker{
		pool: pool,
		cfg:  cfg,
	}
}

func (w *WholeSceneAnalysisWorker) Run(ctx context.Context) {
	log.Printf(
		"whole scene analysis worker started scan_interval=%s max_images=%d max_image_bytes=%d",
		w.cfg.ScanInterval,
		w.cfg.MaxImages,
		w.cfg.MaxImageBytes,
	)

	if err := w.resetStuckProcessing(ctx); err != nil {
		log.Printf("whole scene analysis worker recovery failed: %v", err)
	}

	ticker := time.NewTicker(w.cfg.ScanInterval)
	defer ticker.Stop()

	if err := w.scanOnce(ctx); err != nil {
		log.Printf("whole scene analysis worker scan failed: %v", err)
	}

	for {
		select {
		case <-ctx.Done():
			log.Print("whole scene analysis worker stopped")
			return
		case <-ticker.C:
			if err := w.scanOnce(ctx); err != nil {
				log.Printf("whole scene analysis worker scan failed: %v", err)
			}
		}
	}
}

func (w *WholeSceneAnalysisWorker) resetStuckProcessing(ctx context.Context) error {
	recoveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if _, err := w.pool.Exec(recoveryCtx, `
		WITH recovered AS (
			UPDATE whole_scene_analysis_runs
			SET
				status = 'queued',
				error_message = 'requeued after API restart',
				started_datetime = NULL,
				completed_datetime = NULL,
				updated_datetime = now()
			WHERE status = 'processing'
			RETURNING scan_id
		)
		UPDATE whole_scene_scans wss
		SET status = 'queued',
			updated_datetime = now()
		FROM recovered
		WHERE wss.id = recovered.scan_id
	`); err != nil {
		return err
	}

	_, err := w.pool.Exec(recoveryCtx, `
		UPDATE whole_scene_candidates
		SET
			ai_assist_status = 'queued',
			ai_assist_error_message = 'requeued after API restart',
			ai_assist_started_at = NULL,
			ai_assist_completed_at = NULL,
			updated_datetime = now()
		WHERE ai_assist_status = 'processing'
	`)
	return err
}

func (w *WholeSceneAnalysisWorker) scanOnce(ctx context.Context) error {
	job, err := w.claimNextJob(ctx)
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		candidateJob, candidateErr := w.claimNextCandidateAssistJob(ctx)
		if candidateErr != nil {
			if errors.Is(candidateErr, pgx.ErrNoRows) {
				return nil
			}
			return candidateErr
		}
		if err := w.processCandidateAssistJob(ctx, candidateJob); err != nil {
			log.Printf("whole scene candidate AI assist job %s failed: %v", candidateJob.CandidateID, err)
		}
		return nil
	}

	if err := w.processJob(ctx, job); err != nil {
		log.Printf("whole scene analysis run %s failed: %v", job.RunID, err)
	}

	return nil
}

func (w *WholeSceneAnalysisWorker) claimNextCandidateAssistJob(ctx context.Context) (wholeSceneCandidateAssistJob, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return wholeSceneCandidateAssistJob{}, err
	}
	defer tx.Rollback(ctx)

	var job wholeSceneCandidateAssistJob
	err = tx.QueryRow(ctx, `
		SELECT id::text, scan_id::text, ai_assist_provider_config_id::text
		FROM whole_scene_candidates
		WHERE ai_assist_status = 'queued'
			AND status IN ('proposed', 'edited')
		ORDER BY ai_assist_requested_at ASC NULLS LAST, created_datetime ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&job.CandidateID, &job.ScanID, &job.ProviderConfigID)
	if err != nil {
		return wholeSceneCandidateAssistJob{}, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			ai_assist_status = 'processing',
			ai_assist_started_at = now(),
			ai_assist_completed_at = NULL,
			ai_assist_error_message = '',
			updated_datetime = now()
		WHERE id = $1::uuid
			AND ai_assist_status = 'queued'
	`, job.CandidateID)
	if err != nil {
		return wholeSceneCandidateAssistJob{}, err
	}
	if tag.RowsAffected() != 1 {
		return wholeSceneCandidateAssistJob{}, pgx.ErrNoRows
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneCandidateAssistJob{}, err
	}

	return job, nil
}

func (w *WholeSceneAnalysisWorker) processCandidateAssistJob(ctx context.Context, job wholeSceneCandidateAssistJob) error {
	store := &WholeSceneStore{
		pool:           w.pool,
		files:          NewManagedFileService(w.cfg.SafeRoots),
		maxUploadBytes: w.cfg.MaxImageBytes,
	}
	if store.maxUploadBytes <= 0 {
		store.maxUploadBytes = 10 * 1024 * 1024
	}

	if err := store.runCandidateAIAssist(ctx, job.ScanID, job.CandidateID); err != nil {
		_ = w.markCandidateAIAssistFailed(ctx, job.CandidateID, err.Error())
		return err
	}

	log.Printf("whole scene candidate AI assist completed scan_id=%s candidate_id=%s", job.ScanID, job.CandidateID)
	return nil
}

func (w *WholeSceneAnalysisWorker) claimNextJob(ctx context.Context) (wholeSceneAnalysisJob, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return wholeSceneAnalysisJob{}, err
	}
	defer tx.Rollback(ctx)

	var job wholeSceneAnalysisJob
	err = tx.QueryRow(ctx, `
		SELECT id::text, scan_id::text
		FROM whole_scene_analysis_runs
		WHERE status = 'queued'
		ORDER BY queued_datetime ASC, run_number ASC, id ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`).Scan(&job.RunID, &job.ScanID)
	if err != nil {
		return wholeSceneAnalysisJob{}, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			status = 'processing',
			started_datetime = now(),
			completed_datetime = NULL,
			error_message = NULL,
			updated_datetime = now()
		WHERE id = $1::uuid AND status = 'queued'
	`, job.RunID)
	if err != nil {
		return wholeSceneAnalysisJob{}, err
	}
	if tag.RowsAffected() != 1 {
		return wholeSceneAnalysisJob{}, pgx.ErrNoRows
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET status = 'processing',
			updated_datetime = now()
		WHERE id = $1::uuid
	`, job.ScanID); err != nil {
		return wholeSceneAnalysisJob{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneAnalysisJob{}, err
	}

	return job, nil
}

func (w *WholeSceneAnalysisWorker) processJob(ctx context.Context, job wholeSceneAnalysisJob) error {
	run, err := w.loadRun(ctx, job.RunID)
	if err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}
	if run.ProviderConfigID == nil || strings.TrimSpace(*run.ProviderConfigID) == "" {
		err := errors.New("Whole Scene provider configuration is missing")
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	providerStore := NewAIProviderStore(w.pool)
	provider, err := providerStore.getStoredByID(ctx, w.pool, *run.ProviderConfigID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = errors.New("Whole Scene provider configuration no longer exists")
		}
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}
	if err := validateWholeSceneGeminiProvider(provider); err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	images, err := w.loadUsableImages(ctx, job.ScanID)
	if err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	prompt := strings.TrimSpace(derefString(run.PromptText))
	if prompt == "" {
		prompt = buildWholeScenePrompt(run.Hint)
	}

	pass1Prompt := prompt
	payload := buildWholeSceneGeminiPayload(provider, pass1Prompt, images)
	requestPayload, err := json.Marshal(buildWholeSceneRequestManifest(run, provider, images))
	if err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}
	if err := w.storeRequestPayload(ctx, job.RunID, pass1Prompt, requestPayload); err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	analysisCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	rawResponse, err := callGeminiWholeSceneAnalysis(analysisCtx, provider, payload)
	if err != nil {
		var providerErr wholeSceneProviderError
		if errors.As(err, &providerErr) && len(bytes.TrimSpace(providerErr.responseBody)) > 0 {
			_ = w.storeRawResponse(ctx, job.RunID, providerErr.responseBody)
		}
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	if err := w.storeRawResponse(ctx, job.RunID, rawResponse); err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	parsed, err := parseWholeSceneGeminiResponse(rawResponse)
	if err != nil {
		message := err.Error()
		if len(parsed.Warnings) > 0 {
			message += ": " + strings.Join(parsed.Warnings, "; ")
		}
		_ = w.markParseFailed(ctx, job.RunID, job.ScanID, rawResponse, message)
		return err
	}

	localizationPrompt := buildWholeSceneLocalizationPrompt(run.Hint, parsed.Candidates)
	localizationPayload := buildWholeSceneGeminiPayload(provider, localizationPrompt, images)
	localizationCtx, localizationCancel := context.WithTimeout(ctx, timeout)
	defer localizationCancel()

	localizationRawResponse, localizationErr := callGeminiWholeSceneAnalysis(localizationCtx, provider, localizationPayload)
	localizationWarnings := make([]string, 0)
	if localizationErr != nil {
		clearWholeSceneCandidateAppearances(parsed.Candidates)
		localizationWarnings = append(localizationWarnings, "Whole Scene localization pass failed: "+localizationErr.Error())
	} else {
		localizationParseResult, parseErr := parseWholeSceneLocalizationResponse(localizationRawResponse, parsed.Candidates)
		parsed.LocalizationDiagnostics = localizationParseResult.Diagnostics
		localizationWarnings = append(localizationWarnings, localizationParseResult.Warnings...)
		for candidateIndex, warnings := range localizationParseResult.CandidateWarnings {
			if candidateIndex >= 0 && candidateIndex < len(parsed.Candidates) {
				parsed.Candidates[candidateIndex].ParseWarnings = append(parsed.Candidates[candidateIndex].ParseWarnings, warnings...)
			}
		}
		if parseErr != nil {
			clearWholeSceneCandidateAppearances(parsed.Candidates)
			localizationWarnings = append(localizationWarnings, "Whole Scene localization response was not usable: "+parseErr.Error())
		}
	}

	finalRawResponse := combineWholeSceneAnalysisResponses(rawResponse, localizationRawResponse, localizationErr, localizationWarnings)
	if err := w.storeRawResponse(ctx, job.RunID, finalRawResponse); err != nil {
		_ = w.markFailed(ctx, job.RunID, job.ScanID, err.Error())
		return err
	}

	if err := w.markSucceededWithCandidates(ctx, job.RunID, job.ScanID, finalRawResponse, parsed); err != nil {
		return err
	}

	log.Printf("whole scene analysis completed scan_id=%s run_id=%s provider=%s model=%s", job.ScanID, job.RunID, provider.ProviderType, provider.ModelName)
	return nil
}

func (w *WholeSceneAnalysisWorker) loadRun(ctx context.Context, runID string) (wholeSceneAnalysisRunRecord, error) {
	var run wholeSceneAnalysisRunRecord
	err := w.pool.QueryRow(ctx, `
		SELECT
			wsar.id::text,
			wsar.scan_id::text,
			wss.hint,
			wsar.ai_provider_config_id::text,
			wsar.provider_type,
			wsar.model_name,
			wsar.prompt_version,
			wsar.prompt_text,
			wsar.queued_datetime,
			wsar.started_datetime,
			wsar.completed_datetime
		FROM whole_scene_analysis_runs wsar
		JOIN whole_scene_scans wss ON wss.id = wsar.scan_id
		WHERE wsar.id = $1::uuid
	`, runID).Scan(
		&run.ID,
		&run.ScanID,
		&run.Hint,
		&run.ProviderConfigID,
		&run.ProviderType,
		&run.ModelName,
		&run.PromptVersion,
		&run.PromptText,
		&run.QueuedDatetime,
		&run.StartedDatetime,
		&run.CompletedDatetime,
	)
	return run, err
}

func (w *WholeSceneAnalysisWorker) loadUsableImages(ctx context.Context, scanID string) ([]wholeSceneAnalysisImage, error) {
	rows, err := w.pool.Query(ctx, `
		SELECT
			ia.id::text,
			wssi.sort_order,
			ia.file_path,
			ia.mime_type,
			ia.original_filename,
			ia.file_hash,
			ia.file_size_bytes
		FROM whole_scene_scan_images wssi
		JOIN image_assets ia ON ia.id = wssi.image_asset_id
		WHERE wssi.scan_id = $1::uuid
			AND ia.status = 'processed'
		ORDER BY wssi.sort_order ASC, wssi.created_datetime ASC, wssi.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]wholeSceneAnalysisImage, 0)
	for rows.Next() {
		var imageID string
		var sourceImageIndex int
		var filePath string
		var mimeType *string
		var originalFilename *string
		var fileHash *string
		var fileSizeBytes *int64
		if err := rows.Scan(&imageID, &sourceImageIndex, &filePath, &mimeType, &originalFilename, &fileHash, &fileSizeBytes); err != nil {
			return nil, err
		}

		if w.cfg.MaxImages > 0 && len(images) >= w.cfg.MaxImages {
			return nil, fmt.Errorf("Whole Scene analysis found more than %d usable images", w.cfg.MaxImages)
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
			return nil, fmt.Errorf("failed to read processed Whole Scene image %s", imageID)
		}
		if int64(len(data)) == 0 {
			continue
		}
		if int64(len(data)) > w.cfg.MaxImageBytes {
			continue
		}
		storedSizeBytes := int64(len(data))
		if fileSizeBytes != nil {
			storedSizeBytes = *fileSizeBytes
		}

		images = append(images, wholeSceneAnalysisImage{
			ID:               imageID,
			SourceImageIndex: sourceImageIndex,
			MimeType:         normalizedMime,
			OriginalFilename: derefString(originalFilename),
			FileHash:         derefString(fileHash),
			FileSizeBytes:    storedSizeBytes,
			Data:             data,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(images) == 0 {
		return nil, errors.New("Whole Scene analysis found no usable processed JPEG or PNG images")
	}

	return images, nil
}

func (w *WholeSceneAnalysisWorker) storeRequestPayload(ctx context.Context, runID string, prompt string, requestPayload []byte) error {
	_, err := w.pool.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			prompt_text = $2,
			request_payload = $3::jsonb,
			updated_datetime = now()
		WHERE id = $1::uuid
	`, runID, prompt, string(requestPayload))
	return err
}

func (w *WholeSceneAnalysisWorker) storeRawResponse(ctx context.Context, runID string, rawResponse []byte) error {
	_, err := w.pool.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			raw_response_text = $2,
			updated_datetime = now()
		WHERE id = $1::uuid
	`, runID, string(rawResponse))
	return err
}

func clearWholeSceneCandidateAppearances(candidates []wholeSceneParsedCandidate) {
	for index := range candidates {
		candidates[index].Appearances = nil
	}
}

func combineWholeSceneAnalysisResponses(candidateResponse []byte, localizationResponse []byte, localizationErr error, warnings []string) []byte {
	payload := map[string]any{
		"candidate_pass_response": json.RawMessage(candidateResponse),
		"localization_warnings":   warnings,
	}
	if len(bytes.TrimSpace(localizationResponse)) > 0 {
		payload["localization_pass_response"] = json.RawMessage(localizationResponse)
	}
	if localizationErr != nil {
		payload["localization_error"] = localizationErr.Error()
	}
	combined, err := json.Marshal(payload)
	if err != nil {
		return candidateResponse
	}
	return combined
}

func (w *WholeSceneAnalysisWorker) markSucceededWithCandidates(ctx context.Context, runID string, scanID string, rawResponse []byte, parsed wholeSceneParsedResponse) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	scanImages, err := loadWholeSceneScanImageSources(ctx, tx, scanID)
	if err != nil {
		return err
	}
	w.enrichWholeSceneLocalizationDiagnostics(scanID, scanImages, parsed.LocalizationDiagnostics)

	if _, err := tx.Exec(ctx, `
		DELETE FROM whole_scene_candidates
		WHERE analysis_run_id = $1::uuid
			AND source = 'ai'
			AND approved_item_id IS NULL
			AND status <> 'approved'
	`, runID); err != nil {
		return err
	}

	runWarnings := append([]string{}, parsed.Warnings...)
	for _, candidate := range parsed.Candidates {
		candidateWarnings := append([]string{}, candidate.ParseWarnings...)
		appearances := make([]wholeSceneParsedAppearance, 0, len(candidate.Appearances))
		for _, appearance := range candidate.Appearances {
			source, warning, ok := scanImages.resolve(appearance)
			if !ok {
				candidateWarnings = append(candidateWarnings, warning)
				continue
			}
			if warning != "" {
				candidateWarnings = append(candidateWarnings, warning)
			}
			appearance.SourceImageIndex = &source.SortOrder
			if wholeSceneBBoxComplete(appearance.BoundingBox) {
				normalized, bboxWarning := w.normalizeWholeSceneAppearanceBoundingBox(source, appearance.BoundingBox)
				if bboxWarning != "" {
					candidateWarnings = append(candidateWarnings, fmt.Sprintf("appearance source_image_index %d bounding box skipped: %s", source.SortOrder, bboxWarning))
					appearance.BoundingBox = wholeSceneParsedBoundingBox{}
					if appearance.DiagnosticIndex != nil && *appearance.DiagnosticIndex >= 0 && *appearance.DiagnosticIndex < len(parsed.LocalizationDiagnostics) {
						parsed.LocalizationDiagnostics[*appearance.DiagnosticIndex].Accepted = false
						parsed.LocalizationDiagnostics[*appearance.DiagnosticIndex].Reason = bboxWarning
					}
				} else {
					appearance.BoundingBox = normalized
				}
			}
			appearances = append(appearances, appearance)
		}

		parseWarnings := joinWholeSceneWarnings(candidateWarnings)

		var candidateID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO whole_scene_candidates (
				scan_id,
				analysis_run_id,
				source,
				status,
				title,
				description,
				approx_value,
				confidence_label,
				uncertainty_notes,
				raw_candidate,
				parse_warnings
			)
			VALUES ($1::uuid, $2::uuid, 'ai', 'proposed', $3, $4, $5::numeric, $6, $7, $8::jsonb, $9)
			RETURNING id::text
		`,
			scanID,
			runID,
			candidate.Title,
			candidate.Description,
			candidate.ApproxValue,
			candidate.ConfidenceLabel,
			candidate.UncertaintyNotes,
			string(candidate.RawCandidate),
			parseWarnings,
		).Scan(&candidateID); err != nil {
			return err
		}

		persistedAppearances := make([]persistedWholeSceneAppearance, 0, len(appearances))
		for _, appearance := range appearances {
			sourceImageIndex := *appearance.SourceImageIndex
			source := scanImages.byIndex[sourceImageIndex]
			localizationData := string(appearance.LocalizationData)
			var appearanceID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO whole_scene_candidate_appearances (
					candidate_id,
					scan_image_id,
					source_image_index,
					bounding_box_x,
					bounding_box_y,
					bounding_box_width,
					bounding_box_height,
					localization_data,
					confidence_label,
					notes
				)
				VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
				RETURNING id::text
			`,
				candidateID,
				source.ID,
				sourceImageIndex,
				appearance.BoundingBox.X,
				appearance.BoundingBox.Y,
				appearance.BoundingBox.Width,
				appearance.BoundingBox.Height,
				localizationData,
				appearance.ConfidenceLabel,
				appearance.Notes,
			).Scan(&appearanceID); err != nil {
				return err
			}
			persistedAppearances = append(persistedAppearances, persistedWholeSceneAppearance{
				ID:               appearanceID,
				ScanImageID:      source.ID,
				SourceImageIndex: sourceImageIndex,
				Source:           source,
				BoundingBox:      appearance.BoundingBox,
				DiagnosticIndex:  appearance.DiagnosticIndex,
			})
		}

		cropWarnings, cropInfo := w.generatePreferredWholeSceneCrop(ctx, tx, candidateID, persistedAppearances)
		if cropInfo != nil && cropInfo.DiagnosticIndex != nil && *cropInfo.DiagnosticIndex >= 0 && *cropInfo.DiagnosticIndex < len(parsed.LocalizationDiagnostics) {
			parsed.LocalizationDiagnostics[*cropInfo.DiagnosticIndex].CropImageAssetID = cropInfo.CropImageAssetID
			parsed.LocalizationDiagnostics[*cropInfo.DiagnosticIndex].CropPath = cropInfo.CropPath
		}
		if len(cropWarnings) > 0 {
			candidateWarnings = append(candidateWarnings, cropWarnings...)
			parseWarnings = joinWholeSceneWarnings(candidateWarnings)
			if _, err := tx.Exec(ctx, `
				UPDATE whole_scene_candidates
				SET parse_warnings = $2,
					updated_datetime = now()
				WHERE id = $1::uuid
			`, candidateID, parseWarnings); err != nil {
				return err
			}
		}
	}

	status := "succeeded"
	errorMessage := joinWholeSceneWarnings(runWarnings)
	if errorMessage != nil {
		status = "partial"
		truncated := truncateReviewAssistText(*errorMessage, 500)
		errorMessage = &truncated
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			status = $3,
			raw_response_text = $2,
			error_message = $4,
			completed_datetime = now(),
			updated_datetime = now()
		WHERE id = $1::uuid
	`, runID, string(rawResponse), status, errorMessage); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET status = $2,
			updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID, status); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	w.writeWholeSceneLocalizationDiagnostics(scanID, scanImages, parsed.LocalizationDiagnostics)
	return nil
}

func (w *WholeSceneAnalysisWorker) markParseFailed(ctx context.Context, runID string, scanID string, rawResponse []byte, message string) error {
	message = truncateReviewAssistText(strings.TrimSpace(message), 500)
	if message == "" {
		message = "Whole Scene response did not contain any usable candidates"
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			status = 'failed',
			raw_response_text = $2,
			error_message = $3,
			completed_datetime = now(),
			updated_datetime = now()
		WHERE id = $1::uuid
	`, runID, string(rawResponse), message); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET status = 'failed',
			updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func loadWholeSceneScanImageSources(ctx context.Context, q reviewQuerier, scanID string) (wholeSceneScanImageSources, error) {
	rows, err := q.Query(ctx, `
		SELECT
			wssi.sort_order,
			wssi.id::text,
			ia.id::text,
			ia.session_id::text,
			ia.file_path,
			ia.stored_filename,
			ia.mime_type
		FROM whole_scene_scan_images wssi
		JOIN image_assets ia ON ia.id = wssi.image_asset_id
		WHERE wssi.scan_id = $1::uuid
		ORDER BY wssi.sort_order ASC, wssi.created_datetime ASC, wssi.id ASC
	`, scanID)
	if err != nil {
		return wholeSceneScanImageSources{}, err
	}
	defer rows.Close()

	images := wholeSceneScanImageSources{
		byIndex:        make(map[int]wholeSceneScanImageSource),
		byScanImageID:  make(map[string]wholeSceneScanImageSource),
		byImageAssetID: make(map[string]wholeSceneScanImageSource),
	}
	for rows.Next() {
		var sortOrder int
		var source wholeSceneScanImageSource
		var uploadSessionID *string
		var storedFilename *string
		var mimeType *string
		if err := rows.Scan(
			&sortOrder,
			&source.ID,
			&source.ImageAssetID,
			&uploadSessionID,
			&source.FilePath,
			&storedFilename,
			&mimeType,
		); err != nil {
			return wholeSceneScanImageSources{}, err
		}
		source.SortOrder = sortOrder
		if _, exists := images.byIndex[sortOrder]; !exists {
			source.UploadSessionID = derefString(uploadSessionID)
			source.StoredFilename = derefString(storedFilename)
			source.MimeType = derefString(mimeType)
			images.byIndex[sortOrder] = source
			images.byScanImageID[source.ID] = source
			images.byImageAssetID[source.ImageAssetID] = source
		}
	}
	if err := rows.Err(); err != nil {
		return wholeSceneScanImageSources{}, err
	}
	return images, nil
}

func (sources wholeSceneScanImageSources) resolve(appearance wholeSceneParsedAppearance) (wholeSceneScanImageSource, string, bool) {
	var warning string
	if appearance.SourceImageIndex != nil {
		if source, ok := sources.byIndex[*appearance.SourceImageIndex]; ok {
			return source, "", true
		}
		if *appearance.SourceImageIndex > 0 {
			if source, ok := sources.byIndex[*appearance.SourceImageIndex-1]; ok {
				return source, fmt.Sprintf("appearance source_image_index %d was interpreted as 1-based and mapped to scan source index %d", *appearance.SourceImageIndex, source.SortOrder), true
			}
		}
		warning = fmt.Sprintf("appearance source_image_index %d was not part of this scan", *appearance.SourceImageIndex)
	}
	if appearance.SourceImageID != nil {
		if source, ok := sources.byScanImageID[*appearance.SourceImageID]; ok {
			return source, warning, true
		}
	}
	if appearance.ImageAssetID != nil {
		if source, ok := sources.byImageAssetID[*appearance.ImageAssetID]; ok {
			return source, warning, true
		}
	}
	if warning != "" {
		return wholeSceneScanImageSource{}, warning, false
	}
	if len(sources.byIndex) == 1 {
		for _, source := range sources.byIndex {
			return source, "appearance source image was inferred because this scan has one source image", true
		}
	}
	return wholeSceneScanImageSource{}, "appearance skipped because source image reference did not match this scan", false
}

func (w *WholeSceneAnalysisWorker) normalizeWholeSceneAppearanceBoundingBox(source wholeSceneScanImageSource, bbox wholeSceneParsedBoundingBox) (wholeSceneParsedBoundingBox, string) {
	sourcePath := filepath.Clean(strings.TrimSpace(source.FilePath))
	if sourcePath == "" {
		return wholeSceneParsedBoundingBox{}, "source image path was empty"
	}
	if !isSafeManagedPath(sourcePath, w.cfg.SafeRoots) {
		return wholeSceneParsedBoundingBox{}, "source image path was outside managed storage"
	}
	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return wholeSceneParsedBoundingBox{}, "source image could not be opened"
	}
	defer sourceFile.Close()

	config, _, err := image.DecodeConfig(sourceFile)
	if err != nil {
		return wholeSceneParsedBoundingBox{}, "source image dimensions could not be decoded"
	}
	normalized, err := normalizeWholeSceneBoundingBox(image.Rect(0, 0, config.Width, config.Height), bbox)
	if err != nil {
		return wholeSceneParsedBoundingBox{}, err.Error()
	}
	return normalized, ""
}

func (w *WholeSceneAnalysisWorker) markFailed(ctx context.Context, runID string, scanID string, message string) error {
	message = truncateReviewAssistText(strings.TrimSpace(message), 500)
	if message == "" {
		message = "Whole Scene analysis failed"
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET
			status = 'failed',
			error_message = $2,
			completed_datetime = now(),
			updated_datetime = now()
		WHERE id = $1::uuid
	`, runID, message); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET status = 'failed',
			updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (w *WholeSceneAnalysisWorker) markCandidateAIAssistFailed(ctx context.Context, candidateID string, message string) error {
	message = truncateReviewAssistText(strings.TrimSpace(message), 500)
	if message == "" {
		message = "Whole Scene candidate AI Assist failed"
	}

	tag, err := w.pool.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			ai_assist_status = 'failed',
			ai_assist_error_message = $2,
			ai_assist_completed_at = now(),
			updated_datetime = now()
		WHERE id = $1::uuid
			AND ai_assist_status = 'processing'
			AND status IN ('proposed', 'edited')
	`, candidateID, message)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return nil
	}
	return nil
}

func buildWholeSceneGeminiPayload(provider storedAIProviderConfig, prompt string, images []wholeSceneAnalysisImage) map[string]any {
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
		"generationConfig": wholeSceneGeminiGenerationConfig(provider),
	}
	return payload
}

func buildWholeSceneRequestManifest(run wholeSceneAnalysisRunRecord, provider storedAIProviderConfig, images []wholeSceneAnalysisImage) map[string]any {
	return map[string]any{
		"scan_id":                 run.ScanID,
		"analysis_run_id":         run.ID,
		"prompt_version":          run.PromptVersion,
		"response_schema_version": wholeSceneResponseSchemaVersion,
		"hint":                    nullableStringValue(run.Hint),
		"provider": map[string]any{
			"ai_provider_config_id": provider.ID,
			"provider_type":         provider.ProviderType,
			"model_name":            provider.ModelName,
		},
		"generation_config": wholeSceneGeminiGenerationConfig(provider),
		"source_images":     wholeSceneAnalysisImagePayloadMetadata(images),
	}
}

func wholeSceneGeminiGenerationConfig(provider storedAIProviderConfig) map[string]any {
	config := map[string]any{
		"responseMimeType": "application/json",
		"maxOutputTokens":  2048,
	}
	if provider.MaxOutputTokens != nil {
		config["maxOutputTokens"] = *provider.MaxOutputTokens
	}
	if provider.Temperature != nil {
		config["temperature"] = *provider.Temperature
	}
	return config
}

func wholeSceneAnalysisImagePayloadMetadata(images []wholeSceneAnalysisImage) []map[string]any {
	result := make([]map[string]any, 0, len(images))
	for _, image := range images {
		result = append(result, map[string]any{
			"image_asset_id":     image.ID,
			"source_image_index": image.SourceImageIndex,
			"mime_type":          image.MimeType,
			"original_filename":  image.OriginalFilename,
			"file_hash":          image.FileHash,
			"file_size_bytes":    image.FileSizeBytes,
		})
	}
	return result
}

func callGeminiWholeSceneAnalysis(ctx context.Context, provider storedAIProviderConfig, payload map[string]any) ([]byte, error) {
	apiKey := provider.resolvedAPIKey()
	if apiKey == "" {
		return nil, errors.New("Gemini provider requires api_key_value or api_key_env_var")
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

	endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", baseURL, modelPath, url.QueryEscape(apiKey))
	return doWholeSceneJSONRequest(ctx, http.MethodPost, endpoint, payload)
}

func doWholeSceneJSONRequest(ctx context.Context, method string, endpoint string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	const maxWholeSceneResponseBytes = 10 * 1024 * 1024
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxWholeSceneResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(responseBody) > maxWholeSceneResponseBytes {
		return nil, errors.New("Gemini response exceeded maximum supported size")
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := "Gemini request failed with HTTP " + resp.Status
		if bodyMessage := compactHTTPError(bytes.NewReader(responseBody)); bodyMessage != "" {
			message += ": " + bodyMessage
		}
		return nil, wholeSceneProviderError{message: message, responseBody: responseBody}
	}
	if len(bytes.TrimSpace(responseBody)) == 0 {
		return nil, errors.New("Gemini returned an empty response")
	}

	return responseBody, nil
}
