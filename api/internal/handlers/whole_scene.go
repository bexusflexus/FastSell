package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"fastsell-api/internal/models"
	"fastsell-api/internal/respond"
	"fastsell-api/internal/upload"
	"fastsell-api/internal/uploadstatus"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	wholeSceneUploadSource          = "whole_scene"
	wholeSceneMaxImageCount         = 6
	wholeScenePromptVersion         = "fastsell-whole-scene-v1"
	wholeSceneResponseSchemaVersion = "whole_scene_candidates_v1"
)

var (
	errWholeSceneContainerNotFound = errors.New("container was not found")
	errWholeSceneArchivedContainer = errors.New("cannot assign whole scene scan to archived container")
	errWholeSceneImagesInFlight    = errors.New("whole scene scan still has source images in progress")
	errWholeSceneNoUsableImages    = errors.New("whole scene scan must have at least one successfully processed source image")
	errWholeSceneNoProvider        = errors.New("Whole Scene analysis requires an active Gemini provider")
	errWholeSceneCandidateApproved = errors.New("whole scene candidate is already approved")
	errWholeSceneCandidateRejected = errors.New("whole scene candidate is rejected")
	errWholeSceneCandidateAIBusy   = errors.New("Whole Scene candidate AI Assist is already queued or processing")
)

type WholeSceneStore struct {
	pool             *pgxpool.Pool
	files            *ManagedFileService
	intakeDir        string
	maxUploadBytes   int64
	parseMemoryBytes int64
	imageConfig      ItemImageStorageConfig
}

type WholeSceneHandler struct {
	store *WholeSceneStore
}

type wholeSceneCreateRequest struct {
	IntakeContext    intakeContext     `json:"intake_context"`
	Hint             *string           `json:"hint"`
	InventoryGroupID *string           `json:"inventory_group_id"`
	Files            []uploadFileInput `json:"files"`
}

type wholeSceneSourceImageStatus struct {
	ImageAssetID     string
	SortOrder        int
	Status           string
	OriginalFilename *string
	MimeType         *string
	FileSizeBytes    *int64
}

type wholeSceneCandidateApprovalRecord struct {
	ID               string
	ScanID           string
	Source           string
	Status           string
	Title            *string
	Description      *string
	ApproxValue      *string
	ApprovedItemID   *string
	ContainerID      *string
	LocationID       *string
	LocationDetail   *string
	InventoryGroupID string
}

type wholeSceneCandidateAssistRecord struct {
	ID                       string
	ScanID                   string
	Status                   string
	Title                    *string
	Description              *string
	ApproxValue              *string
	AIAssistStatus           string
	AIAssistProviderConfigID *string
	AIAssistUserHint         string
}

type wholeSceneMutationResult struct {
	Scan           *models.WholeSceneScan
	ScanID         string
	CleanedUp      bool
	ApprovedItemID *string
	Cleanup        *models.WholeSceneCleanupSummary
	filePaths      []string
}

func NewWholeSceneStore(pool *pgxpool.Pool, intakeDir string, maxUploadMB int64, extras ...any) *WholeSceneStore {
	var files *ManagedFileService
	imageConfig := NewItemImageStorageConfig(intakeDir, intakeDir, intakeDir, maxUploadMB, 0)
	for _, extra := range extras {
		switch value := extra.(type) {
		case *ManagedFileService:
			files = value
		case ItemImageStorageConfig:
			imageConfig = value
		}
	}
	if files == nil {
		files = NewManagedFileService([]string{intakeDir})
	}
	return &WholeSceneStore{
		pool:             pool,
		files:            files,
		intakeDir:        intakeDir,
		maxUploadBytes:   maxUploadMB * 1024 * 1024,
		parseMemoryBytes: 32 << 20,
		imageConfig:      imageConfig,
	}
}

func NewWholeSceneHandler(store *WholeSceneStore) *WholeSceneHandler {
	return &WholeSceneHandler{store: store}
}

func (s *WholeSceneStore) CreateScan(ctx context.Context, req wholeSceneCreateRequest, form *multipart.Form, writtenFiles *[]writtenUploadFile) (string, error) {
	var containerID *string
	if !req.IntakeContext.NoContainer && req.IntakeContext.ContainerID != "" {
		containerID = &req.IntakeContext.ContainerID
	}

	var locationID *string
	var locationDetail *string
	if req.IntakeContext.NoContainer {
		if req.IntakeContext.LocationID != "" {
			locationID = &req.IntakeContext.LocationID
		}
		if req.IntakeContext.LocationDetail != "" {
			locationDetail = &req.IntakeContext.LocationDetail
		}
	}

	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return "", err
	}
	defer func() {
		_ = tx.Rollback(context.Background())
	}()

	if containerID != nil {
		if err := ensureWholeSceneContainer(ctx, tx, *containerID); err != nil {
			return "", err
		}
	}
	if locationID != nil {
		if err := ensureWholeSceneLocation(ctx, tx, *locationID); err != nil {
			return "", err
		}
	}

	inventoryGroupID, err := resolveUploadInventoryGroup(ctx, tx, req.InventoryGroupID)
	if err != nil {
		return "", err
	}

	var sessionID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO upload_sessions (container_id, location_id, location_detail, source, notes, status)
		VALUES ($1, $2::uuid, $3, $4, NULL, 'pending')
		RETURNING id::text
	`, containerID, locationID, locationDetail, wholeSceneUploadSource).Scan(&sessionID); err != nil {
		return "", err
	}

	var scanID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO whole_scene_scans (
			upload_session_id,
			container_id,
			location_id,
			location_detail,
			inventory_group_id,
			hint,
			status
		)
		VALUES ($1, $2, $3::uuid, $4, $5::uuid, $6, 'created')
		RETURNING id::text
	`, sessionID, containerID, locationID, locationDetail, inventoryGroupID, req.Hint).Scan(&scanID); err != nil {
		return "", err
	}

	for fileIndex, file := range req.Files {
		saved, err := s.saveMultipartFile(file, form.File[multipartFileFieldName(file.ClientFileID)][0], writtenFiles)
		if err != nil {
			return "", err
		}

		var imageAssetID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO image_assets (
				session_id,
				upload_group_id,
				client_file_id,
				original_filename,
				stored_filename,
				file_path,
				file_hash,
				mime_type,
				file_size_bytes,
				upload_order,
				is_original,
				status
			)
			VALUES ($1, NULL, $2, $3, $4, $5, $6, $7, $8, $9, true, 'pending')
			RETURNING id::text
		`,
			sessionID,
			file.ClientFileID,
			saved.originalFilename,
			saved.storedFilename,
			saved.filePath,
			saved.hashHex,
			saved.mimeType,
			saved.sizeBytes,
			fileIndex,
		).Scan(&imageAssetID); err != nil {
			return "", err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO whole_scene_scan_images (scan_id, image_asset_id, sort_order)
			VALUES ($1::uuid, $2::uuid, $3)
		`, scanID, imageAssetID, fileIndex); err != nil {
			return "", err
		}
	}

	if _, err := uploadstatus.Recalculate(ctx, tx, sessionID); err != nil {
		return "", err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", err
	}

	return scanID, nil
}

func (s *WholeSceneStore) QueueAnalysis(ctx context.Context, scanID string) (string, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer tx.Rollback(ctx)

	var hint *string
	if err := tx.QueryRow(ctx, `
		SELECT hint
		FROM whole_scene_scans
		WHERE id = $1::uuid
		FOR UPDATE
	`, scanID).Scan(&hint); err != nil {
		return "", false, err
	}

	images, err := loadWholeSceneSourceImageStatuses(ctx, tx, scanID)
	if err != nil {
		return "", false, err
	}
	if err := validateWholeSceneQueueable(images); err != nil {
		return "", false, err
	}

	latestRun, err := latestWholeSceneAnalysisRunForUpdate(ctx, tx, scanID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", false, err
	}
	if err == nil && (latestRun.Status == "queued" || latestRun.Status == "processing") {
		if _, err := tx.Exec(ctx, `
			UPDATE whole_scene_scans
			SET status = $2,
				updated_datetime = now()
			WHERE id = $1::uuid
		`, scanID, latestRun.Status); err != nil {
			return "", false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return "", false, err
		}
		return latestRun.ID, false, nil
	}

	providerStore := NewAIProviderStore(s.pool)
	provider, err := providerStore.getActiveProvider(ctx, tx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, errWholeSceneNoProvider
		}
		return "", false, err
	}
	if err := validateWholeSceneGeminiProvider(provider); err != nil {
		return "", false, err
	}

	nextRunNumber, err := nextWholeSceneRunNumber(ctx, tx, scanID)
	if err != nil {
		return "", false, err
	}
	prompt := buildWholeScenePrompt(hint)
	requestContext, err := json.Marshal(buildWholeSceneRequestContext(scanID, hint, provider, images))
	if err != nil {
		return "", false, err
	}

	var runID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO whole_scene_analysis_runs (
			scan_id,
			run_number,
			status,
			ai_provider_config_id,
			provider_type,
			model_name,
			prompt_version,
			prompt_text,
			request_payload,
			queued_datetime
		)
		VALUES ($1::uuid, $2, 'queued', $3::uuid, $4, $5, $6, $7, $8::jsonb, now())
		RETURNING id::text
	`,
		scanID,
		nextRunNumber,
		provider.ID,
		provider.ProviderType,
		provider.ModelName,
		wholeScenePromptVersion,
		prompt,
		string(requestContext),
	).Scan(&runID); err != nil {
		return "", false, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET status = 'queued',
			updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return "", false, err
	}

	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}

	return runID, true, nil
}

func (s *WholeSceneStore) AddManualCandidate(ctx context.Context, scanID string, req models.AddWholeSceneCandidateRequest) (wholeSceneMutationResult, error) {
	title, description, approxValue, confidence, uncertainty, err := normalizeWholeSceneCandidateCreate(req)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	if err := ensureWholeSceneScanExists(ctx, tx, scanID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO whole_scene_candidates (
			scan_id,
			source,
			status,
			title,
			description,
			approx_value,
			confidence_label,
			uncertainty_notes
		)
		VALUES ($1::uuid, 'manual', 'proposed', $2, $3, $4::numeric, $5, $6)
	`, scanID, title, description, approxValue, confidence, uncertainty); err != nil {
		return wholeSceneMutationResult{}, err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneMutationResult{}, err
	}

	scan, err := s.GetScan(ctx, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	return wholeSceneMutationResult{Scan: &scan, ScanID: scanID}, nil
}

func (s *WholeSceneStore) PatchCandidate(ctx context.Context, scanID string, candidateID string, req models.PatchWholeSceneCandidateRequest) (wholeSceneMutationResult, error) {
	if !wholeSceneCandidatePatchRequested(req) {
		if err := s.ensureCandidateMutable(ctx, scanID, candidateID); err != nil {
			return wholeSceneMutationResult{}, err
		}
		scan, err := s.GetScan(ctx, scanID)
		if err != nil {
			return wholeSceneMutationResult{}, err
		}
		return wholeSceneMutationResult{Scan: &scan, ScanID: scanID}, nil
	}

	title, description, approxValue, confidence, uncertainty, err := normalizeWholeSceneCandidatePatch(req)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			title = CASE WHEN $3 THEN $4 ELSE title END,
			description = CASE WHEN $5 THEN $6 ELSE description END,
			approx_value = CASE WHEN $7 THEN $8::numeric ELSE approx_value END,
			confidence_label = CASE WHEN $9 THEN $10 ELSE confidence_label END,
			uncertainty_notes = CASE WHEN $11 THEN $12 ELSE uncertainty_notes END,
			status = 'edited',
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
			AND status IN ('proposed', 'edited')
	`,
		scanID,
		candidateID,
		req.Title.Set,
		title,
		req.Description.Set,
		description,
		req.ApproxValue.Set,
		approxValue,
		req.ConfidenceLabel.Set,
		confidence,
		req.UncertaintyNotes.Set,
		uncertainty,
	)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if tag.RowsAffected() == 0 {
		if err := s.ensureCandidateMutable(ctx, scanID, candidateID); err != nil {
			return wholeSceneMutationResult{}, err
		}
		return wholeSceneMutationResult{}, pgx.ErrNoRows
	}

	scan, err := s.GetScan(ctx, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	return wholeSceneMutationResult{Scan: &scan, ScanID: scanID}, nil
}

func (s *WholeSceneStore) QueueCandidateAIAssist(ctx context.Context, scanID string, candidateID string, req models.AssistWholeSceneCandidateRequest) (wholeSceneMutationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	candidate, err := loadWholeSceneCandidateAssistRecord(ctx, tx, scanID, candidateID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	switch candidate.Status {
	case "approved":
		return wholeSceneMutationResult{}, errWholeSceneCandidateApproved
	case "rejected":
		return wholeSceneMutationResult{}, errWholeSceneCandidateRejected
	}
	if candidate.AIAssistStatus == "queued" || candidate.AIAssistStatus == "processing" {
		return wholeSceneMutationResult{}, errWholeSceneCandidateAIBusy
	}

	providerStore := NewAIProviderStore(s.pool)
	provider, err := providerStore.getActiveProvider(ctx, tx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return wholeSceneMutationResult{}, errAIAssistNoProvider
		}
		return wholeSceneMutationResult{}, err
	}
	if err := validateReviewAssistProvider(provider); err != nil {
		return wholeSceneMutationResult{}, err
	}

	title, updateTitle := normalizeWholeSceneAssistField(req.Title)
	description, updateDescription := normalizeWholeSceneAssistField(req.Description)
	approxValue, updateApproxValue := normalizeWholeSceneAssistField(req.ApproxValue)
	if updateApproxValue && approxValue != nil && !moneyValuePattern.MatchString(*approxValue) {
		return wholeSceneMutationResult{}, errors.New("approx_value must be a non-negative decimal with up to two decimal places")
	}
	userHint := strings.TrimSpace(derefString(req.UserHint))

	tag, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			title = CASE WHEN $3::boolean THEN $4 ELSE title END,
			description = CASE WHEN $5::boolean THEN $6 ELSE description END,
			approx_value = CASE
				WHEN $7::boolean THEN CASE WHEN $8::text IS NULL THEN NULL ELSE $8::numeric END
				ELSE approx_value
			END,
			ai_assist_status = 'queued',
			ai_assist_error_message = '',
			ai_assist_requested_at = now(),
			ai_assist_started_at = NULL,
			ai_assist_completed_at = NULL,
			ai_assist_provider_config_id = $9::uuid,
			ai_assist_provider = $10,
			ai_assist_model = $11,
			ai_assist_user_hint = $12,
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
			AND status IN ('proposed', 'edited')
			AND ai_assist_status NOT IN ('queued', 'processing')
	`, scanID, candidateID, updateTitle, title, updateDescription, description, updateApproxValue, nullableStringArg(approxValue), provider.ID, provider.ProviderType, provider.ModelName, userHint)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if tag.RowsAffected() != 1 {
		return wholeSceneMutationResult{}, errWholeSceneCandidateAIBusy
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneMutationResult{}, err
	}

	scan, err := s.GetScan(ctx, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	return wholeSceneMutationResult{Scan: &scan, ScanID: scanID}, nil
}

func (s *WholeSceneStore) runCandidateAIAssist(ctx context.Context, scanID string, candidateID string) error {
	candidate, err := s.loadWholeSceneCandidateAssistRecord(ctx, scanID, candidateID)
	if err != nil {
		return err
	}
	if candidate.Status != "proposed" && candidate.Status != "edited" {
		return errors.New("Whole Scene candidate is no longer editable")
	}
	if candidate.AIAssistProviderConfigID == nil || strings.TrimSpace(*candidate.AIAssistProviderConfigID) == "" {
		return errors.New("Whole Scene candidate AI Assist provider configuration is missing")
	}

	providerStore := NewAIProviderStore(s.pool)
	provider, err := providerStore.getStoredByID(ctx, s.pool, *candidate.AIAssistProviderConfigID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errors.New("Whole Scene candidate AI Assist provider configuration no longer exists")
		}
		return err
	}
	if err := validateReviewAssistProvider(provider); err != nil {
		return err
	}

	image, err := s.loadWholeSceneCandidateAssistImage(ctx, scanID, candidateID)
	if err != nil {
		return err
	}

	title := candidate.Title
	description := candidate.Description
	approxValue := candidate.ApproxValue
	hint := trimOptionalString(&candidate.AIAssistUserHint)
	if approxValue != nil {
		valueHint := "Current candidate approximate value: " + *approxValue
		if hint != nil {
			combined := *hint + "\n" + valueHint
			hint = &combined
		} else {
			hint = &valueHint
		}
	}

	timeout := time.Duration(provider.TimeoutSeconds) * time.Second
	assistCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	suggestion, err := analyzeReviewGroup(assistCtx, provider, reviewAssistInput{
		Group: reviewAssistGroupRecord{
			UploadGroupID: candidateID,
			Title:         title,
			Notes:         description,
			Status:        candidate.Status,
		},
		Images:   []reviewAssistImage{image},
		UserHint: hint,
	})
	if err != nil {
		return err
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			title = $3,
			description = $4,
			approx_value = CASE WHEN $5::text IS NULL THEN NULL ELSE $5::numeric END,
			status = 'edited',
			ai_assist_status = 'succeeded',
			ai_assist_error_message = '',
			ai_assist_completed_at = now(),
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
			AND status IN ('proposed', 'edited')
			AND ai_assist_status = 'processing'
	`, scanID, candidateID, suggestion.Title, suggestion.Description, nullableStringArg(suggestion.ApproxValue))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *WholeSceneStore) RejectCandidate(ctx context.Context, scanID string, candidateID string) (wholeSceneMutationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			status = 'rejected',
			rejected_datetime = COALESCE(rejected_datetime, now()),
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
			AND status <> 'approved'
	`, scanID, candidateID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if tag.RowsAffected() == 0 {
		if err := s.ensureCandidateRejectable(ctx, scanID, candidateID); err != nil {
			return wholeSceneMutationResult{}, err
		}
		return wholeSceneMutationResult{}, pgx.ErrNoRows
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	result, err := s.cleanupWholeSceneIfFullyReviewed(ctx, tx, scanID, nil)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return wholeSceneMutationResult{}, err
	}
	return s.finishWholeSceneMutation(ctx, result, scanID)
}

func (s *WholeSceneStore) ApproveCandidate(ctx context.Context, scanID string, candidateID string, req models.ApproveWholeSceneCandidateRequest) (wholeSceneMutationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	candidate, err := loadWholeSceneCandidateForApproval(ctx, tx, scanID, candidateID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if candidate.Status == "rejected" {
		return wholeSceneMutationResult{}, errWholeSceneCandidateRejected
	}
	if candidate.Status == "approved" {
		result, err := s.cleanupWholeSceneIfFullyReviewed(ctx, tx, scanID, candidate.ApprovedItemID)
		if err != nil {
			return wholeSceneMutationResult{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return wholeSceneMutationResult{}, err
		}
		return s.finishWholeSceneMutation(ctx, result, scanID)
	}

	inventoryGroupID := candidate.InventoryGroupID
	if req.InventoryGroupID != nil {
		resolvedID, err := resolveUploadInventoryGroup(ctx, tx, req.InventoryGroupID)
		if err != nil {
			return wholeSceneMutationResult{}, err
		}
		inventoryGroupID = resolvedID
	}

	aiEnriched := candidate.Source == "ai"
	var containerArg any
	if candidate.ContainerID != nil {
		containerArg = *candidate.ContainerID
	}
	var locationArg any
	var locationDetailArg any
	if candidate.ContainerID == nil {
		if candidate.LocationID != nil {
			locationArg = *candidate.LocationID
		}
		if candidate.LocationDetail != nil {
			locationDetailArg = *candidate.LocationDetail
		}
	}
	var approxValueArg any
	if candidate.ApproxValue != nil {
		approxValueArg = *candidate.ApproxValue
	}

	var itemID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO items (
			container_id,
			title,
			description,
			ai_enriched,
			approx_value,
			inventory_group_id,
			location_id,
			location_detail
		)
		VALUES ($1, $2, $3, $4, CASE WHEN $5::text IS NULL THEN NULL ELSE $5::numeric(12,2) END, $6::uuid, $7::uuid, $8)
		RETURNING id::text
	`,
		containerArg,
		candidate.Title,
		candidate.Description,
		aiEnriched,
		approxValueArg,
		inventoryGroupID,
		locationArg,
		locationDetailArg,
	).Scan(&itemID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	if err := attachWholeSceneCandidateImagesToItem(ctx, tx, candidateID, itemID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	tag, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET
			status = 'approved',
			approved_item_id = $3::uuid,
			approved_datetime = COALESCE(approved_datetime, now()),
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
			AND status IN ('proposed', 'edited')
	`, scanID, candidateID, itemID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	if tag.RowsAffected() == 0 {
		return wholeSceneMutationResult{}, errors.New("whole scene candidate approval state changed before it could be linked")
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	result, err := s.cleanupWholeSceneIfFullyReviewed(ctx, tx, scanID, &itemID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneMutationResult{}, err
	}

	return s.finishWholeSceneMutation(ctx, result, scanID)
}

func (s *WholeSceneStore) saveMultipartFile(file uploadFileInput, header *multipart.FileHeader, writtenFiles *[]writtenUploadFile) (savedUploadFile, error) {
	if header.Size > s.maxUploadBytes {
		return savedUploadFile{}, fmt.Errorf("%w: file %q exceeds max upload size", errInvalidUploadRequest, file.ClientFileID)
	}

	return saveUploadMultipartFile(s.intakeDir, s.maxUploadBytes, file, header, writtenFiles)
}

func (s *WholeSceneStore) ensureCandidateImageDirectories() error {
	for _, dir := range []string{s.imageConfig.OriginalsDir, s.imageConfig.ThumbnailsDir, s.imageConfig.NormalizedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return nil
}

func (s *WholeSceneStore) maxCandidateImageBodyBytes() int64 {
	const maxExpectedFiles = 20
	return int64(maxExpectedFiles)*s.imageConfig.MaxUploadBytes + itemImageUploadFormOverhead
}

func (s *WholeSceneStore) saveCandidateImageUpload(header *multipart.FileHeader, writtenFiles *[]writtenItemImageFile) (savedItemImageFile, error) {
	if header.Size <= 0 {
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q is empty", errInvalidUploadRequest, header.Filename)
	}
	if header.Size > s.imageConfig.MaxUploadBytes {
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q exceeds max upload size", errInvalidUploadRequest, header.Filename)
	}

	src, err := header.Open()
	if err != nil {
		return savedItemImageFile{}, err
	}
	defer src.Close()

	originalFilename := cleanOriginalFilename(header.Filename)
	if originalFilename == "" {
		return savedItemImageFile{}, fmt.Errorf("%w: original filename is required", errInvalidUploadRequest)
	}

	declaredMimeType := strings.TrimSpace(header.Header.Get("Content-Type"))
	storedFilename, err := upload.NewStoredFilename(originalFilename, declaredMimeType)
	if err != nil {
		return savedItemImageFile{}, fmt.Errorf("%w: %v", errInvalidUploadRequest, err)
	}

	destinationPath := filepath.Join(s.imageConfig.OriginalsDir, storedFilename)
	dst, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return savedItemImageFile{}, err
	}

	hasher := sha256.New()
	limitedReader := io.LimitReader(src, s.imageConfig.MaxUploadBytes+1)
	bytesWritten, copyErr := io.Copy(dst, io.TeeReader(limitedReader, hasher))
	closeErr := dst.Close()
	if copyErr != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, copyErr
	}
	if closeErr != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, closeErr
	}
	if bytesWritten > s.imageConfig.MaxUploadBytes {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, fmt.Errorf("%w: uploaded image %q exceeds max upload size", errInvalidUploadRequest, originalFilename)
	}

	detectedMimeType, err := detectSupportedImageMime(destinationPath)
	if err != nil {
		_ = os.Remove(destinationPath)
		return savedItemImageFile{}, fmt.Errorf("%w: %v", errInvalidUploadRequest, err)
	}

	*writtenFiles = append(*writtenFiles, writtenItemImageFile{path: destinationPath})

	return savedItemImageFile{
		originalFilename: originalFilename,
		storedFilename:   storedFilename,
		filePath:         destinationPath,
		mimeType:         detectedMimeType,
		sizeBytes:        bytesWritten,
		hashHex:          hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}

func (s *WholeSceneStore) cleanupSavedCandidateVariants(savedFiles []savedItemImageFile) {
	for _, file := range savedFiles {
		_ = os.Remove(filepath.Join(s.imageConfig.ThumbnailsDir, file.storedFilename))
		_ = os.Remove(filepath.Join(s.imageConfig.NormalizedDir, file.storedFilename))
	}
}

func (s *WholeSceneStore) regenerateCandidateImageVariants(record itemImageUploadRecord) (string, string, error) {
	cleanOriginalPath := filepath.Clean(strings.TrimSpace(record.FilePath))
	if !isSafeManagedPath(cleanOriginalPath, s.files.safeRoots) {
		return "", "", fmt.Errorf("%w: %s", errUnsafeManagedFilePath, cleanOriginalPath)
	}

	sourceFile, err := os.Open(cleanOriginalPath)
	if err != nil {
		return "", "", err
	}
	defer sourceFile.Close()

	thumbnailPath := filepath.Join(s.imageConfig.ThumbnailsDir, record.StoredFilename)
	normalizedPath := filepath.Join(s.imageConfig.NormalizedDir, record.StoredFilename)

	srcImage, imageFormat, err := image.Decode(sourceFile)
	if err != nil {
		log.Printf("whole scene candidate image upload using original file as fallback variants image_asset_id=%s error=%v", record.ID, err)
		if err := copyImageVariantFile(cleanOriginalPath, thumbnailPath); err != nil {
			return "", "", err
		}
		if err := copyImageVariantFile(cleanOriginalPath, normalizedPath); err != nil {
			return "", "", err
		}
		return thumbnailPath, normalizedPath, nil
	}

	if err := writeVariantImage(srcImage, imageFormat, thumbnailPath, defaultThumbnailMaxEdge); err != nil {
		return "", "", err
	}
	if err := writeVariantImage(srcImage, imageFormat, normalizedPath, defaultNormalizedMaxEdge); err != nil {
		return "", "", err
	}

	return thumbnailPath, normalizedPath, nil
}

func (s *WholeSceneStore) AppendCandidateImages(ctx context.Context, scanID string, candidateID string, savedFiles []savedItemImageFile) (wholeSceneMutationResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	defer tx.Rollback(ctx)

	var uploadSessionID string
	var candidateStatus string
	if err := tx.QueryRow(ctx, `
		SELECT wss.upload_session_id::text, wsc.status
		FROM whole_scene_candidates wsc
		JOIN whole_scene_scans wss ON wss.id = wsc.scan_id
		WHERE wsc.scan_id = $1::uuid
			AND wsc.id = $2::uuid
		FOR UPDATE OF wsc
	`, scanID, candidateID).Scan(&uploadSessionID, &candidateStatus); err != nil {
		return wholeSceneMutationResult{}, err
	}
	if candidateStatus == "approved" {
		return wholeSceneMutationResult{}, errWholeSceneCandidateApproved
	}
	if candidateStatus == "rejected" {
		return wholeSceneMutationResult{}, errWholeSceneCandidateRejected
	}

	var existingGeneratedCount int
	var nextUploadOrder int
	if err := tx.QueryRow(ctx, `
		SELECT
			count(*) FILTER (WHERE wscc.status = 'generated' AND wscc.crop_image_asset_id IS NOT NULL)::int,
			COALESCE(max(ia.upload_order), -1)::int + 1
		FROM whole_scene_candidate_crops wscc
		LEFT JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
		WHERE wscc.candidate_id = $1::uuid
	`, candidateID).Scan(&existingGeneratedCount, &nextUploadOrder); err != nil {
		return wholeSceneMutationResult{}, err
	}
	if s.imageConfig.MaxImagesPerItem > 0 && existingGeneratedCount+len(savedFiles) > s.imageConfig.MaxImagesPerItem {
		return wholeSceneMutationResult{}, fmt.Errorf("%w: candidate cannot have more than %d images", errInvalidUploadRequest, s.imageConfig.MaxImagesPerItem)
	}

	variantUpdates := make([]itemImageVariantUpdate, 0, len(savedFiles))
	for index, file := range savedFiles {
		var imageAssetID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO image_assets (
				session_id,
				upload_group_id,
				original_filename,
				stored_filename,
				file_path,
				file_hash,
				mime_type,
				file_size_bytes,
				upload_order,
				is_original,
				status
			)
			VALUES ($1::uuid, NULL, $2, $3, $4, $5, $6, $7, $8, true, 'processed')
			RETURNING id::text
		`,
			uploadSessionID,
			file.originalFilename,
			file.storedFilename,
			file.filePath,
			file.hashHex,
			file.mimeType,
			file.sizeBytes,
			nextUploadOrder+index,
		).Scan(&imageAssetID); err != nil {
			return wholeSceneMutationResult{}, err
		}

		isPreferred := existingGeneratedCount == 0 && index == 0
		if _, err := tx.Exec(ctx, `
			INSERT INTO whole_scene_candidate_crops (
				candidate_id,
				crop_image_asset_id,
				status,
				is_preferred,
				crop_metadata
			)
			VALUES ($1::uuid, $2::uuid, 'generated', $3, '{"source":"manual_upload"}'::jsonb)
		`, candidateID, imageAssetID, isPreferred); err != nil {
			return wholeSceneMutationResult{}, err
		}

		thumbnailPath, normalizedPath, err := s.regenerateCandidateImageVariants(itemImageUploadRecord{
			ID:             imageAssetID,
			FilePath:       file.filePath,
			StoredFilename: file.storedFilename,
			MimeType:       &file.mimeType,
		})
		if err != nil {
			return wholeSceneMutationResult{}, err
		}
		variantUpdates = append(variantUpdates, itemImageVariantUpdate{
			ImageAssetID:   imageAssetID,
			ThumbnailPath:  thumbnailPath,
			NormalizedPath: normalizedPath,
		})
	}

	for _, update := range variantUpdates {
		if _, err := tx.Exec(ctx, `
			UPDATE image_assets
			SET thumbnail_path = $2,
				normalized_path = $3,
				updated_datetime = now()
			WHERE id = $1::uuid
		`, update.ImageAssetID, update.ThumbnailPath, update.NormalizedPath); err != nil {
			return wholeSceneMutationResult{}, err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET status = CASE WHEN status = 'proposed' THEN 'edited' ELSE status END,
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
	`, scanID, candidateID); err != nil {
		return wholeSceneMutationResult{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return wholeSceneMutationResult{}, err
	}

	return s.finishWholeSceneMutation(ctx, wholeSceneMutationResult{ScanID: scanID}, scanID)
}

func (s *WholeSceneStore) DeleteCandidateImage(ctx context.Context, scanID string, candidateID string, cropID string) (models.WholeSceneCandidateImageDeleteResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	defer tx.Rollback(ctx)

	var imageID string
	var filePath string
	var thumbnailPath *string
	var normalizedPath *string
	var itemID *string
	if err := tx.QueryRow(ctx, `
		SELECT
			ia.id::text,
			ia.file_path,
			ia.thumbnail_path,
			ia.normalized_path,
			ia.item_id::text
		FROM whole_scene_candidate_crops wscc
		JOIN whole_scene_candidates wsc ON wsc.id = wscc.candidate_id
		JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
		WHERE wsc.scan_id = $1::uuid
			AND wscc.candidate_id = $2::uuid
			AND wscc.id = $3::uuid
		FOR UPDATE OF wscc, wsc, ia
	`, scanID, candidateID, cropID).Scan(&imageID, &filePath, &thumbnailPath, &normalizedPath, &itemID); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	if itemID != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, errWholeSceneCandidateApproved
	}

	refs := []managedFileReference{{
		ImageAssetID: imageID,
		Kind:         "original",
		Path:         filePath,
	}}
	if thumbnailPath != nil && strings.TrimSpace(*thumbnailPath) != "" {
		refs = append(refs, managedFileReference{
			ImageAssetID: imageID,
			Kind:         "thumbnail",
			Path:         *thumbnailPath,
		})
	}
	if normalizedPath != nil && strings.TrimSpace(*normalizedPath) != "" {
		refs = append(refs, managedFileReference{
			ImageAssetID: imageID,
			Kind:         "normalized",
			Path:         *normalizedPath,
		})
	}
	inspection, err := s.files.InspectReferences(refs)
	if err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}

	if _, err := tx.Exec(ctx, `
		DELETE FROM whole_scene_candidate_crops
		WHERE id = $1::uuid
			AND candidate_id = $2::uuid
	`, cropID, candidateID); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	deleteTag, err := tx.Exec(ctx, `
		DELETE FROM image_assets
		WHERE id = $1::uuid
			AND item_id IS NULL
	`, imageID)
	if err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	if deleteTag.RowsAffected() == 0 {
		return models.WholeSceneCandidateImageDeleteResponse{}, pgx.ErrNoRows
	}

	if _, err := tx.Exec(ctx, `
		WITH next_preferred AS (
			SELECT id
			FROM whole_scene_candidate_crops
			WHERE candidate_id = $1::uuid
				AND status = 'generated'
				AND crop_image_asset_id IS NOT NULL
			ORDER BY created_datetime ASC, id ASC
			LIMIT 1
		)
		UPDATE whole_scene_candidate_crops
		SET is_preferred = (id IN (SELECT id FROM next_preferred)),
			updated_datetime = now()
		WHERE candidate_id = $1::uuid
			AND NOT EXISTS (
				SELECT 1
				FROM whole_scene_candidate_crops
				WHERE candidate_id = $1::uuid
					AND is_preferred = true
			)
	`, candidateID); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_candidates
		SET status = CASE WHEN status = 'proposed' THEN 'edited' ELSE status END,
			updated_datetime = now()
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
	`, scanID, candidateID); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE whole_scene_scans
		SET updated_datetime = now()
		WHERE id = $1::uuid
	`, scanID); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}

	scan, err := s.GetScan(ctx, scanID)
	if err != nil {
		return models.WholeSceneCandidateImageDeleteResponse{}, err
	}
	deletedFiles, missingFiles, warnings := s.files.DeleteFiles(inspection.DeletePaths, "whole scene candidate image delete")
	return models.WholeSceneCandidateImageDeleteResponse{
		Scan:                &scan,
		ScanID:              scanID,
		DeletedCropID:       cropID,
		DeletedImageAssetID: imageID,
		DeletedFileCount:    deletedFiles,
		MissingFileCount:    missingFiles,
		Warnings:            warnings,
	}, nil
}

func parseWholeSceneCreateMetadata(form *multipart.Form) (wholeSceneCreateRequest, error) {
	var req wholeSceneCreateRequest
	if form == nil {
		return req, errors.New("metadata is required")
	}

	values := form.Value[metadataFieldName]
	if len(values) == 0 || strings.TrimSpace(values[0]) == "" {
		return req, errors.New("metadata is required")
	}

	decoder := json.NewDecoder(strings.NewReader(values[0]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		return req, errors.New("metadata must be valid JSON")
	}

	return req, nil
}

func normalizeWholeSceneCreateRequest(req *wholeSceneCreateRequest) {
	req.IntakeContext.ContainerID = strings.TrimSpace(req.IntakeContext.ContainerID)
	req.IntakeContext.ContainerName = strings.TrimSpace(req.IntakeContext.ContainerName)
	req.IntakeContext.LocationID = strings.TrimSpace(req.IntakeContext.LocationID)
	req.IntakeContext.LocationDetail = strings.TrimSpace(req.IntakeContext.LocationDetail)
	req.Hint = trimOptionalString(req.Hint)
	req.InventoryGroupID = trimOptionalString(req.InventoryGroupID)

	for fileIndex := range req.Files {
		file := &req.Files[fileIndex]
		file.ClientFileID = strings.TrimSpace(file.ClientFileID)
		file.OriginalFilename = cleanOriginalFilename(file.OriginalFilename)
		file.MimeType = strings.ToLower(strings.TrimSpace(file.MimeType))
	}
}

func validateWholeSceneCreateRequest(req wholeSceneCreateRequest, maxUploadBytes int64, form *multipart.Form) error {
	if req.IntakeContext.ContainerID != "" && !isUUID(req.IntakeContext.ContainerID) {
		return errors.New("intake_context.container_id must be a valid UUID")
	}
	if req.IntakeContext.LocationID != "" && !isUUID(req.IntakeContext.LocationID) {
		return errors.New("intake_context.location_id must be a valid UUID")
	}
	if req.InventoryGroupID != nil && !isUUID(*req.InventoryGroupID) {
		return errors.New("inventory_group_id must be a valid UUID")
	}
	if len(req.Files) == 0 {
		return errors.New("at least one source image is required")
	}
	if len(req.Files) > wholeSceneMaxImageCount {
		return fmt.Errorf("whole scene scans support at most %d source images", wholeSceneMaxImageCount)
	}

	seenFiles := make(map[string]struct{})
	for fileIndex, file := range req.Files {
		if file.ClientFileID == "" {
			return fmt.Errorf("files[%d].client_file_id is required", fileIndex)
		}
		if _, exists := seenFiles[file.ClientFileID]; exists {
			return fmt.Errorf("duplicate client_file_id %q", file.ClientFileID)
		}
		seenFiles[file.ClientFileID] = struct{}{}

		if file.SizeBytes > maxUploadBytes {
			return fmt.Errorf("file %q exceeds max upload size", file.ClientFileID)
		}
		if !upload.IsAllowedImage(file.OriginalFilename, file.MimeType) {
			return fmt.Errorf("file %q must be jpeg, png, heic, or heif", file.ClientFileID)
		}

		partName := multipartFileFieldName(file.ClientFileID)
		parts := form.File[partName]
		if len(parts) == 0 {
			return fmt.Errorf("missing multipart file part %q", partName)
		}
		if len(parts) > 1 {
			return fmt.Errorf("multipart file part %q must appear once", partName)
		}
		if parts[0].Size > maxUploadBytes {
			return fmt.Errorf("file %q exceeds max upload size", file.ClientFileID)
		}
	}

	return nil
}

func validateWholeSceneQueueable(images []wholeSceneSourceImageStatus) error {
	if len(images) == 0 {
		return errWholeSceneNoUsableImages
	}

	processedCount := 0
	for _, image := range images {
		switch image.Status {
		case "processed":
			processedCount++
		case "failed":
		default:
			return errWholeSceneImagesInFlight
		}
	}
	if processedCount == 0 {
		return errWholeSceneNoUsableImages
	}

	return nil
}

func normalizeWholeSceneCandidateCreate(req models.AddWholeSceneCandidateRequest) (*string, *string, *string, *string, *string, error) {
	title := trimOptionalString(req.Title)
	if title == nil {
		return nil, nil, nil, nil, nil, errors.New("title must not be blank")
	}

	description := trimOptionalString(req.Description)
	approxValue, err := normalizeWholeSceneMoneyPointer(req.ApproxValue, "approx_value")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	confidence, err := normalizeWholeSceneConfidencePointer(req.ConfidenceLabel)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	uncertainty := trimOptionalString(req.UncertaintyNotes)

	return title, description, approxValue, confidence, uncertainty, nil
}

func normalizeWholeSceneCandidatePatch(req models.PatchWholeSceneCandidateRequest) (*string, *string, *string, *string, *string, error) {
	title, err := normalizeRequiredOptionalString(req.Title, "title")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	description := normalizeNullableOptionalString(req.Description)
	approxValue, err := normalizeMoneyOptionalString(req.ApproxValue, "approx_value")
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	confidence, err := normalizeWholeSceneConfidenceOptionalString(req.ConfidenceLabel)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	uncertainty := normalizeNullableOptionalString(req.UncertaintyNotes)

	return title, description, approxValue, confidence, uncertainty, nil
}

func normalizeWholeSceneMoneyPointer(value *string, name string) (*string, error) {
	value = trimOptionalString(value)
	if value == nil {
		return nil, nil
	}
	if !moneyValuePattern.MatchString(*value) {
		return nil, fmt.Errorf("%s must be a non-negative decimal with up to two decimal places", name)
	}
	return value, nil
}

func normalizeWholeSceneConfidencePointer(value *string) (*string, error) {
	value = trimOptionalString(value)
	if value == nil {
		return nil, nil
	}
	normalized := strings.ToLower(*value)
	switch normalized {
	case "high", "medium", "low", "unknown":
		return &normalized, nil
	default:
		return nil, errors.New("confidence_label must be high, medium, low, or unknown")
	}
}

func normalizeWholeSceneConfidenceOptionalString(field models.OptionalString) (*string, error) {
	if !field.Set {
		return nil, nil
	}
	if field.Value == nil {
		return nil, nil
	}
	return normalizeWholeSceneConfidencePointer(field.Value)
}

func wholeSceneCandidatePatchRequested(req models.PatchWholeSceneCandidateRequest) bool {
	return req.Title.Set ||
		req.Description.Set ||
		req.ApproxValue.Set ||
		req.ConfidenceLabel.Set ||
		req.UncertaintyNotes.Set
}

func (s *WholeSceneStore) cleanupWholeSceneIfFullyReviewed(ctx context.Context, tx pgx.Tx, scanID string, approvedItemID *string) (wholeSceneMutationResult, error) {
	result := wholeSceneMutationResult{
		ScanID:         scanID,
		ApprovedItemID: approvedItemID,
	}

	var uploadSessionID string
	if err := tx.QueryRow(ctx, `
		SELECT upload_session_id::text
		FROM whole_scene_scans
		WHERE id = $1::uuid
		FOR UPDATE
	`, scanID).Scan(&uploadSessionID); err != nil {
		return wholeSceneMutationResult{}, err
	}

	rows, err := tx.Query(ctx, `
		SELECT id
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
		FOR UPDATE
	`, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	for rows.Next() {
		var ignored string
		if err := rows.Scan(&ignored); err != nil {
			rows.Close()
			return wholeSceneMutationResult{}, err
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return wholeSceneMutationResult{}, err
	}
	rows.Close()

	var totalCandidates int
	var pendingCandidates int
	if err := tx.QueryRow(ctx, `
		SELECT
			count(*)::int,
			count(*) FILTER (WHERE status NOT IN ('approved', 'rejected'))::int
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
	`, scanID).Scan(&totalCandidates, &pendingCandidates); err != nil {
		return wholeSceneMutationResult{}, err
	}
	if totalCandidates == 0 || pendingCandidates > 0 {
		return result, nil
	}

	return cleanupWholeSceneScanRows(ctx, tx, scanID, uploadSessionID, result)
}

func cleanupWholeSceneScanRows(ctx context.Context, tx pgx.Tx, scanID string, uploadSessionID string, result wholeSceneMutationResult) (wholeSceneMutationResult, error) {
	filePaths, err := loadWholeSceneCleanupFilePaths(ctx, tx, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	var deletedImageAssets int
	var deletedUploadSessions int
	if err := tx.QueryRow(ctx, `
		WITH cleanup_asset_ids AS (
			SELECT ia.id
			FROM whole_scene_scan_images wssi
			JOIN image_assets ia ON ia.id = wssi.image_asset_id
			WHERE wssi.scan_id = $1::uuid
				AND ia.item_id IS NULL
			UNION
			SELECT ia.id
			FROM whole_scene_candidate_crops wscc
			JOIN whole_scene_candidates wsc ON wsc.id = wscc.candidate_id
			JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
			WHERE wsc.scan_id = $1::uuid
				AND ia.item_id IS NULL
		),
		deleted_scan AS (
			DELETE FROM whole_scene_scans
			WHERE id = $1::uuid
			RETURNING id
		),
		deleted_assets AS (
			DELETE FROM image_assets ia
			WHERE ia.id IN (SELECT id FROM cleanup_asset_ids)
				AND ia.item_id IS NULL
				AND (SELECT count(*) FROM deleted_scan) > 0
			RETURNING id
		),
		detached_preserved_assets AS (
			UPDATE image_assets ia
			SET session_id = NULL
			WHERE ia.session_id = $2::uuid
				AND ia.item_id IS NOT NULL
				AND (SELECT count(*) FROM deleted_scan) > 0
			RETURNING id
		),
		deleted_session AS (
			DELETE FROM upload_sessions us
			WHERE us.id = $2::uuid
				AND NOT EXISTS (
					SELECT 1
					FROM upload_groups ug
					WHERE ug.session_id = us.id
				)
			RETURNING id
		)
		SELECT
			(SELECT count(*)::int FROM deleted_assets),
			(SELECT count(*)::int FROM deleted_session)
	`, scanID, uploadSessionID).Scan(&deletedImageAssets, &deletedUploadSessions); err != nil {
		return wholeSceneMutationResult{}, err
	}

	result.CleanedUp = true
	result.filePaths = filePaths
	result.Cleanup = &models.WholeSceneCleanupSummary{
		DeletedImageAssetCount:    deletedImageAssets,
		DeletedUploadSessionCount: deletedUploadSessions,
		Warnings:                  []string{},
	}
	return result, nil
}

func (s *WholeSceneStore) DeleteScan(ctx context.Context, scanID string) (models.DeleteWholeSceneScanResponse, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return models.DeleteWholeSceneScanResponse{}, err
	}
	defer tx.Rollback(ctx)

	var uploadSessionID string
	if err := tx.QueryRow(ctx, `
		SELECT upload_session_id::text
		FROM whole_scene_scans
		WHERE id = $1::uuid
		FOR UPDATE
	`, scanID).Scan(&uploadSessionID); err != nil {
		return models.DeleteWholeSceneScanResponse{}, err
	}

	result := wholeSceneMutationResult{ScanID: scanID}
	result, err = cleanupWholeSceneScanRows(ctx, tx, scanID, uploadSessionID, result)
	if err != nil {
		return models.DeleteWholeSceneScanResponse{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return models.DeleteWholeSceneScanResponse{}, err
	}

	cleanup := models.WholeSceneCleanupSummary{Warnings: []string{}}
	if result.Cleanup != nil {
		cleanup = *result.Cleanup
	}
	if result.CleanedUp && len(result.filePaths) > 0 {
		deletedFiles, missingFiles, warnings := s.files.DeleteFiles(result.filePaths, "whole scene delete")
		cleanup.DeletedFileCount = deletedFiles
		cleanup.MissingFileCount = missingFiles
		cleanup.Warnings = warnings
	}

	return models.DeleteWholeSceneScanResponse{
		ScanID:  scanID,
		Cleanup: cleanup,
	}, nil
}

func loadWholeSceneCleanupFilePaths(ctx context.Context, q reviewQuerier, scanID string) ([]string, error) {
	rows, err := q.Query(ctx, `
		WITH cleanup_asset_ids AS (
			SELECT ia.id
			FROM whole_scene_scan_images wssi
			JOIN image_assets ia ON ia.id = wssi.image_asset_id
			WHERE wssi.scan_id = $1::uuid
				AND ia.item_id IS NULL
			UNION
			SELECT ia.id
			FROM whole_scene_candidate_crops wscc
			JOIN whole_scene_candidates wsc ON wsc.id = wscc.candidate_id
			JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
			WHERE wsc.scan_id = $1::uuid
				AND ia.item_id IS NULL
		),
		cleanup_paths AS (
			SELECT DISTINCT paths.path
			FROM image_assets ia
			JOIN cleanup_asset_ids cai ON cai.id = ia.id
			CROSS JOIN LATERAL (
				VALUES (ia.file_path), (ia.thumbnail_path), (ia.normalized_path)
			) AS paths(path)
			WHERE paths.path IS NOT NULL
				AND paths.path <> ''
				AND NOT EXISTS (
					SELECT 1
					FROM image_assets other
					WHERE other.id NOT IN (SELECT id FROM cleanup_asset_ids)
						AND (
							other.file_path = paths.path
							OR other.thumbnail_path = paths.path
							OR other.normalized_path = paths.path
						)
				)
		)
		SELECT path
		FROM cleanup_paths
		ORDER BY path
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	filePaths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		filePaths = append(filePaths, path)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return filePaths, nil
}

func (s *WholeSceneStore) finishWholeSceneMutation(ctx context.Context, result wholeSceneMutationResult, scanID string) (wholeSceneMutationResult, error) {
	if result.CleanedUp {
		if result.Cleanup != nil {
			deletedFiles, missingFiles, warnings := s.files.DeleteFiles(result.filePaths, "whole scene cleanup")
			result.Cleanup.DeletedFileCount = deletedFiles
			result.Cleanup.MissingFileCount = missingFiles
			result.Cleanup.Warnings = warnings
		}
		return result, nil
	}

	scan, err := s.GetScan(ctx, scanID)
	if err != nil {
		return wholeSceneMutationResult{}, err
	}
	result.Scan = &scan
	return result, nil
}

func wholeSceneCandidateMutationResponse(result wholeSceneMutationResult) models.WholeSceneCandidateMutationResponse {
	return models.WholeSceneCandidateMutationResponse{
		Scan:           result.Scan,
		ScanID:         result.ScanID,
		CleanedUp:      result.CleanedUp,
		ApprovedItemID: result.ApprovedItemID,
		Cleanup:        result.Cleanup,
	}
}

func ensureWholeSceneScanExists(ctx context.Context, q reviewQuerier, scanID string) error {
	var exists bool
	if err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM whole_scene_scans
			WHERE id = $1::uuid
		)
	`, scanID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *WholeSceneStore) ensureCandidateMutable(ctx context.Context, scanID string, candidateID string) error {
	var status string
	if err := s.pool.QueryRow(ctx, `
		SELECT status
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
	`, scanID, candidateID).Scan(&status); err != nil {
		return err
	}
	switch status {
	case "approved":
		return errWholeSceneCandidateApproved
	case "rejected":
		return errWholeSceneCandidateRejected
	default:
		return nil
	}
}

func (s *WholeSceneStore) ensureCandidateRejectable(ctx context.Context, scanID string, candidateID string) error {
	var status string
	if err := s.pool.QueryRow(ctx, `
		SELECT status
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
	`, scanID, candidateID).Scan(&status); err != nil {
		return err
	}
	if status == "approved" {
		return errWholeSceneCandidateApproved
	}
	return nil
}

func (s *WholeSceneStore) loadWholeSceneCandidateAssistRecord(ctx context.Context, scanID string, candidateID string) (wholeSceneCandidateAssistRecord, error) {
	return loadWholeSceneCandidateAssistRecord(ctx, s.pool, scanID, candidateID)
}

func loadWholeSceneCandidateAssistRecord(ctx context.Context, q reviewQuerier, scanID string, candidateID string) (wholeSceneCandidateAssistRecord, error) {
	var candidate wholeSceneCandidateAssistRecord
	err := q.QueryRow(ctx, `
		SELECT
			id::text,
			scan_id::text,
			status,
			title,
			description,
			approx_value::text,
			ai_assist_status,
			ai_assist_provider_config_id::text,
			ai_assist_user_hint
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
			AND id = $2::uuid
		FOR UPDATE
	`, scanID, candidateID).Scan(
		&candidate.ID,
		&candidate.ScanID,
		&candidate.Status,
		&candidate.Title,
		&candidate.Description,
		&candidate.ApproxValue,
		&candidate.AIAssistStatus,
		&candidate.AIAssistProviderConfigID,
		&candidate.AIAssistUserHint,
	)
	return candidate, err
}

func normalizeWholeSceneAssistField(value *string) (*string, bool) {
	if value == nil {
		return nil, false
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil, true
	}
	return &trimmed, true
}

func (s *WholeSceneStore) loadWholeSceneCandidateAssistImage(ctx context.Context, scanID string, candidateID string) (reviewAssistImage, error) {
	image, err := s.loadWholeSceneCandidatePreferredCrop(ctx, scanID, candidateID)
	if err == nil {
		return image, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return reviewAssistImage{}, err
	}
	image, err = s.loadWholeSceneCandidateSourceImage(ctx, scanID, candidateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return reviewAssistImage{}, errors.New("Whole Scene candidate has no usable crop or source image for AI Assist")
		}
		return reviewAssistImage{}, err
	}
	return image, nil
}

func (s *WholeSceneStore) loadWholeSceneCandidatePreferredCrop(ctx context.Context, scanID string, candidateID string) (reviewAssistImage, error) {
	return s.loadWholeSceneAssistImage(ctx, `
		SELECT
			ia.id::text,
			ia.file_path,
			ia.mime_type,
			ia.original_filename,
			ia.file_size_bytes
		FROM whole_scene_candidate_crops wscc
		JOIN whole_scene_candidates wsc ON wsc.id = wscc.candidate_id
		JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
		WHERE wsc.scan_id = $1::uuid
			AND wsc.id = $2::uuid
			AND wscc.status = 'generated'
			AND ia.status = 'processed'
		ORDER BY wscc.is_preferred DESC, wscc.created_datetime ASC, wscc.id ASC
		LIMIT 1
	`, scanID, candidateID)
}

func (s *WholeSceneStore) loadWholeSceneCandidateSourceImage(ctx context.Context, scanID string, candidateID string) (reviewAssistImage, error) {
	return s.loadWholeSceneAssistImage(ctx, `
		SELECT
			ia.id::text,
			ia.file_path,
			ia.mime_type,
			ia.original_filename,
			ia.file_size_bytes
		FROM whole_scene_candidate_appearances wsca
		JOIN whole_scene_scan_images wssi ON wssi.id = wsca.scan_image_id
		JOIN image_assets ia ON ia.id = wssi.image_asset_id
		WHERE wsca.candidate_id = $2::uuid
			AND wssi.scan_id = $1::uuid
			AND ia.status = 'processed'
		ORDER BY wsca.created_datetime ASC, wsca.id ASC
		LIMIT 1
	`, scanID, candidateID)
}

func (s *WholeSceneStore) loadWholeSceneAssistImage(ctx context.Context, query string, scanID string, candidateID string) (reviewAssistImage, error) {
	var imageID string
	var filePath string
	var mimeType *string
	var originalFilename *string
	var fileSizeBytes *int64
	if err := s.pool.QueryRow(ctx, query, scanID, candidateID).Scan(&imageID, &filePath, &mimeType, &originalFilename, &fileSizeBytes); err != nil {
		return reviewAssistImage{}, err
	}

	cleanPath := filepath.Clean(filePath)
	if !isSafeManagedPath(cleanPath, s.files.safeRoots) {
		return reviewAssistImage{}, fmt.Errorf("%w: %s", errUnsafeManagedFilePath, cleanPath)
	}
	if fileSizeBytes != nil && *fileSizeBytes > s.maxUploadBytes {
		return reviewAssistImage{}, errors.New("Whole Scene candidate image is too large for AI Assist")
	}
	normalizedMime := normalizeReviewAssistMIME(mimeType, cleanPath)
	if normalizedMime == "" {
		return reviewAssistImage{}, errors.New("Whole Scene candidate image must be JPEG or PNG for AI Assist")
	}

	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return reviewAssistImage{}, fmt.Errorf("failed to read Whole Scene candidate image %s", imageID)
	}
	if int64(len(data)) == 0 {
		return reviewAssistImage{}, errors.New("Whole Scene candidate image is empty")
	}
	if int64(len(data)) > s.maxUploadBytes {
		return reviewAssistImage{}, errors.New("Whole Scene candidate image is too large for AI Assist")
	}

	return reviewAssistImage{
		ID:               imageID,
		MimeType:         normalizedMime,
		OriginalFilename: derefString(originalFilename),
		Data:             data,
	}, nil
}

func loadWholeSceneCandidateForApproval(ctx context.Context, q reviewQuerier, scanID string, candidateID string) (wholeSceneCandidateApprovalRecord, error) {
	var candidate wholeSceneCandidateApprovalRecord
	err := q.QueryRow(ctx, `
		SELECT
			wsc.id::text,
			wsc.scan_id::text,
			wsc.source,
			wsc.status,
			wsc.title,
			wsc.description,
			wsc.approx_value::text,
			wsc.approved_item_id::text,
			wss.container_id::text,
			wss.location_id::text,
			wss.location_detail,
			wss.inventory_group_id::text
		FROM whole_scene_candidates wsc
		JOIN whole_scene_scans wss ON wss.id = wsc.scan_id
		WHERE wsc.scan_id = $1::uuid
			AND wsc.id = $2::uuid
		FOR UPDATE OF wsc
	`, scanID, candidateID).Scan(
		&candidate.ID,
		&candidate.ScanID,
		&candidate.Source,
		&candidate.Status,
		&candidate.Title,
		&candidate.Description,
		&candidate.ApproxValue,
		&candidate.ApprovedItemID,
		&candidate.ContainerID,
		&candidate.LocationID,
		&candidate.LocationDetail,
		&candidate.InventoryGroupID,
	)
	return candidate, err
}

func attachWholeSceneCandidateImagesToItem(ctx context.Context, tx pgx.Tx, candidateID string, itemID string) error {
	rows, err := tx.Query(ctx, `
		SELECT crop_image_asset_id::text
		FROM whole_scene_candidate_crops
		WHERE candidate_id = $1::uuid
			AND status = 'generated'
			AND crop_image_asset_id IS NOT NULL
		ORDER BY is_preferred DESC, created_datetime ASC, id ASC
	`, candidateID)
	if err != nil {
		return err
	}
	defer rows.Close()

	imageIDs := make([]string, 0)
	for rows.Next() {
		var imageID string
		if err := rows.Scan(&imageID); err != nil {
			return err
		}
		imageIDs = append(imageIDs, imageID)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for index, imageID := range imageIDs {
		tag, err := tx.Exec(ctx, `
			UPDATE image_assets
			SET
				item_id = $2::uuid,
				upload_order = $3,
				updated_datetime = now()
			WHERE id = $1::uuid
				AND item_id IS NULL
		`, imageID, itemID, index)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return errors.New("selected Whole Scene crop image is already attached to an item")
		}
	}
	return nil
}

func loadWholeSceneSourceImageStatuses(ctx context.Context, q reviewQuerier, scanID string) ([]wholeSceneSourceImageStatus, error) {
	rows, err := q.Query(ctx, `
		SELECT
			ia.id::text,
			wssi.sort_order,
			ia.status,
			ia.original_filename,
			ia.mime_type,
			ia.file_size_bytes
		FROM whole_scene_scan_images wssi
		JOIN image_assets ia ON ia.id = wssi.image_asset_id
		WHERE wssi.scan_id = $1::uuid
		ORDER BY wssi.sort_order ASC, wssi.created_datetime ASC, wssi.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]wholeSceneSourceImageStatus, 0)
	for rows.Next() {
		var image wholeSceneSourceImageStatus
		if err := rows.Scan(
			&image.ImageAssetID,
			&image.SortOrder,
			&image.Status,
			&image.OriginalFilename,
			&image.MimeType,
			&image.FileSizeBytes,
		); err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func latestWholeSceneAnalysisRunForUpdate(ctx context.Context, q reviewQuerier, scanID string) (models.WholeSceneAnalysisRun, error) {
	var run models.WholeSceneAnalysisRun
	err := q.QueryRow(ctx, `
		SELECT
			id::text,
			run_number,
			status,
			ai_provider_config_id::text,
			provider_type,
			model_name,
			prompt_version,
			raw_response_text IS NOT NULL AND btrim(raw_response_text) <> '',
			error_message,
			queued_datetime,
			started_datetime,
			completed_datetime,
			created_datetime,
			updated_datetime
		FROM whole_scene_analysis_runs
		WHERE scan_id = $1::uuid
		ORDER BY run_number DESC, queued_datetime DESC, id DESC
		FOR UPDATE
		LIMIT 1
	`, scanID).Scan(
		&run.ID,
		&run.RunNumber,
		&run.Status,
		&run.AIProviderConfigID,
		&run.ProviderType,
		&run.ModelName,
		&run.PromptVersion,
		&run.RawResponseAvailable,
		&run.ErrorMessage,
		&run.QueuedDatetime,
		&run.StartedDatetime,
		&run.CompletedDatetime,
		&run.CreatedDatetime,
		&run.UpdatedDatetime,
	)
	return run, err
}

func nextWholeSceneRunNumber(ctx context.Context, q reviewQuerier, scanID string) (int, error) {
	var nextRunNumber int
	if err := q.QueryRow(ctx, `
		SELECT COALESCE(max(run_number), 0) + 1
		FROM whole_scene_analysis_runs
		WHERE scan_id = $1::uuid
	`, scanID).Scan(&nextRunNumber); err != nil {
		return 0, err
	}
	return nextRunNumber, nil
}

func buildWholeSceneRequestContext(scanID string, hint *string, provider storedAIProviderConfig, images []wholeSceneSourceImageStatus) map[string]any {
	sourceImages := make([]map[string]any, 0)
	for _, image := range images {
		if image.Status != "processed" {
			continue
		}
		sourceImages = append(sourceImages, map[string]any{
			"image_asset_id":     image.ImageAssetID,
			"source_image_index": image.SortOrder,
			"original_filename":  nullableStringValue(image.OriginalFilename),
			"mime_type":          nullableStringValue(image.MimeType),
			"file_size_bytes":    nullableInt64Value(image.FileSizeBytes),
		})
	}

	return map[string]any{
		"scan_id":        scanID,
		"prompt_version": wholeScenePromptVersion,
		"hint":           nullableStringValue(hint),
		"provider": map[string]any{
			"provider_type": provider.ProviderType,
			"model_name":    provider.ModelName,
		},
		"source_images": sourceImages,
	}
}

func buildWholeScenePrompt(hint *string) string {
	hintText := strings.TrimSpace(derefString(hint))
	hintSection := ""
	if hintText != "" {
		hintSection = fmt.Sprintf(`
The user provided this optional scan-level hint:
%q

Use the hint only as guidance. Do not assume it is correct when image evidence conflicts with it.
`, hintText)
	}

	return fmt.Sprintf(`Prompt version: %s

You are helping FastSell quickly identify physical inventory candidates from a multi-image scene scan.
Analyze all images together as overlapping views of the same physical scene.
Find distinct physical-item candidates that a human can later review.
Speed and useful recall are more important than perfect identification.
Do not create trusted inventory. Only propose candidates.
Return only strict JSON. No markdown fences. No extra commentary.

Required JSON:
{
  "candidates": [
    {
      "candidate_key": "short_stable_item_key",
      "title": "string",
      "description": "string",
      "approx_value": 0,
      "confidence": "high|medium|low",
      "uncertainty": "string",
      "appearances": [
        {
          "candidate_key": "short_stable_item_key",
          "target_label": "same item label",
          "source_image_index": 0,
          "image_asset_id": "string",
          "bbox": {
            "x": 0.0,
            "y": 0.0,
            "width": 0.0,
            "height": 0.0
          },
          "bbox_format": "xywh",
          "bbox_units": "normalized"
        }
      ]
    }
  ]
}

For each appearance, return the source_image_index from the source image manifest. Include image_asset_id when available.
Use a stable candidate_key for each candidate and repeat the exact same candidate_key on every appearance for that candidate.
Identify candidate items first, then locate each known candidate_key in the source images. Do not move a bounding box from one candidate_key to another.
Prefer bbox_format "xywh" with bbox_units "normalized" and x/y/width/height values from 0 to 1.
Prefer bbox objects over arrays. If you must use an array, use bbox_format "yxyx" with [ymin, xmin, ymax, xmax].
If you cannot use normalized xywh, set bbox_format to "xyxy", "yxyx", or "xywh" and bbox_units to "percent", "thousand", or "pixel".
Bounding boxes are optional; omit them when localization is uncertain or the box might belong to a different candidate_key.
%s`, wholeScenePromptVersion, hintSection)
}

func buildWholeSceneLocalizationPrompt(hint *string, candidates []wholeSceneParsedCandidate) string {
	targets := make([]map[string]any, 0, len(candidates))
	for _, candidate := range candidates {
		targets = append(targets, map[string]any{
			"candidate_key": candidate.CandidateKey,
			"title":         candidate.Title,
			"description":   nullableStringValue(candidate.Description),
		})
	}
	targetJSON, err := json.Marshal(targets)
	if err != nil {
		targetJSON = []byte("[]")
	}

	hintText := strings.TrimSpace(derefString(hint))
	hintSection := ""
	if hintText != "" {
		hintSection = fmt.Sprintf(`
The user provided this optional scan-level hint:
%q

Use the hint only as guidance. Do not invent a localization that is not visible in the images.
`, hintText)
	}

	return fmt.Sprintf(`Prompt version: %s localization

You are localizing known FastSell Whole Scene inventory candidates.
The candidate labels below were already identified in a separate pass. Do not rename, rewrite, merge, split, value, or reinterpret them.
For each candidate_key, find the best visible source image and bounding box for that exact candidate only.
Wrong crops are worse than no crop. Return not_found when the candidate is hidden, ambiguous, partly confused with another item, or not confidently localizable.
Return only strict JSON. No markdown fences. No extra commentary.

Candidate targets:
%s

Required JSON:
{
  "localizations": [
    {
      "candidate_key": "same_key_from_targets",
      "target_label": "same title from targets",
      "found": true,
      "confidence": "high|medium|low",
      "source_image_index": 0,
      "image_asset_id": "string",
      "box_2d": [100, 200, 400, 600],
      "box_units": "0_1000"
    }
  ]
}

Return exactly one localization object per candidate_key.
Use found true only when confidence is high and the box tightly encloses the target candidate.
Use found false with no box_2d when the target is uncertain or the box might belong to another candidate.
For found true, box_2d is required and must be exactly [ymin, xmin, ymax, xmax] using 0-1000 coordinates relative to the selected source image.
Set box_units to exactly "0_1000".
Do not return bbox, boundingBox, box, box2d, xyxy, xywh, pixel, percent, or normalized 0-1 boxes for localization.
Do not guess. It is better to return found false than a questionable box.
Use source_image_index values from the source image manifest and include image_asset_id when available.
%s`, wholeScenePromptVersion, string(targetJSON), hintSection)
}

func validateWholeSceneGeminiProvider(provider storedAIProviderConfig) error {
	if !provider.Enabled {
		return errors.New("active Gemini provider is disabled")
	}
	if !provider.VisionEnabled {
		return errors.New("active Gemini provider does not have vision enabled")
	}
	if strings.TrimSpace(provider.ModelName) == "" {
		return errors.New("active Gemini provider is missing model_name")
	}
	if provider.ProviderType != "gemini" {
		return errors.New("Whole Scene analysis currently requires an active Gemini provider")
	}
	if provider.resolvedAPIKey() == "" {
		return errors.New("active Gemini provider requires api_key_value or api_key_env_var")
	}
	return nil
}

func nullableStringValue(value *string) any {
	if value == nil {
		return nil
	}
	return *value
}

func nullableInt64Value(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

func wholeSceneStringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func wholeSceneIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func wholeSceneTimeValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func (s *WholeSceneStore) GetScan(ctx context.Context, scanID string) (models.WholeSceneScan, error) {
	scan, err := s.loadScan(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}

	images, err := s.loadScanImages(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}
	scan.Images = images

	analysisRuns, err := s.loadAnalysisRuns(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}
	scan.AnalysisRuns = analysisRuns
	assignLatestWholeSceneAnalysisRun(&scan)

	candidates, err := s.loadCandidates(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}
	if len(candidates) == 0 {
		scan.Candidates = candidates
		return scan, nil
	}

	appearances, err := s.loadCandidateAppearances(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}
	crops, err := s.loadCandidateCrops(ctx, scanID)
	if err != nil {
		return models.WholeSceneScan{}, err
	}

	candidateIndexes := make(map[string]int, len(candidates))
	for index := range candidates {
		candidateIndexes[candidates[index].ID] = index
	}
	for _, appearance := range appearances {
		index, ok := candidateIndexes[appearance.CandidateID]
		if !ok {
			continue
		}
		candidates[index].Appearances = append(candidates[index].Appearances, appearance)
	}
	for _, crop := range crops {
		index, ok := candidateIndexes[crop.CandidateID]
		if !ok {
			continue
		}
		candidates[index].Crops = append(candidates[index].Crops, crop)
	}

	scan.Candidates = candidates
	return scan, nil
}

func (s *WholeSceneStore) ListReviewScans(ctx context.Context, containerID *string) ([]models.WholeSceneReviewScanSummary, error) {
	rows, err := s.pool.Query(ctx, `
		WITH latest_runs AS (
			SELECT DISTINCT ON (scan_id)
				id,
				scan_id,
				run_number,
				status,
				ai_provider_config_id,
				provider_type,
				model_name,
				prompt_version,
				raw_response_text,
				error_message,
				queued_datetime,
				started_datetime,
				completed_datetime,
				created_datetime,
				updated_datetime
			FROM whole_scene_analysis_runs
			ORDER BY scan_id, run_number DESC, queued_datetime DESC, id DESC
		),
		image_counts AS (
			SELECT
				wssi.scan_id,
				count(*)::int AS image_count,
				count(*) FILTER (WHERE ia.status = 'processed')::int AS processed_image_count,
				count(*) FILTER (WHERE ia.status = 'failed')::int AS failed_image_count
			FROM whole_scene_scan_images wssi
			JOIN image_assets ia ON ia.id = wssi.image_asset_id
			GROUP BY wssi.scan_id
		),
		candidate_counts AS (
			SELECT
				scan_id,
				count(*) FILTER (WHERE status IN ('proposed', 'edited'))::int AS pending_candidate_count,
				count(*) FILTER (WHERE status = 'approved')::int AS approved_candidate_count,
				count(*) FILTER (WHERE status = 'rejected')::int AS rejected_candidate_count,
				count(*)::int AS total_candidate_count
			FROM whole_scene_candidates
			GROUP BY scan_id
		)
		SELECT
			wss.id::text,
			wss.upload_session_id::text,
			wss.container_id::text,
			c.name,
			c.type,
			c.container_type_id::text,
			ct.name,
			c.location_id::text,
			cl.name,
			c.location_description,
			wss.location_id::text,
			sl.name,
			wss.location_detail,
			ig.id::text,
			ig.code,
			ig.name,
			wss.hint,
			wss.status,
			COALESCE(ic.image_count, 0),
			COALESCE(ic.processed_image_count, 0),
			COALESCE(ic.failed_image_count, 0),
			COALESCE(cc.pending_candidate_count, 0),
			COALESCE(cc.approved_candidate_count, 0),
			COALESCE(cc.rejected_candidate_count, 0),
			COALESCE(cc.total_candidate_count, 0),
			lr.id::text,
			lr.run_number,
			lr.status,
			lr.ai_provider_config_id::text,
			lr.provider_type,
			lr.model_name,
			lr.prompt_version,
			lr.raw_response_text IS NOT NULL AND btrim(lr.raw_response_text) <> '',
			lr.error_message,
			lr.queued_datetime,
			lr.started_datetime,
			lr.completed_datetime,
			lr.created_datetime,
			lr.updated_datetime,
			wss.created_datetime,
			wss.updated_datetime
		FROM whole_scene_scans wss
		JOIN inventory_groups ig ON ig.id = wss.inventory_group_id
		LEFT JOIN containers c ON c.id = wss.container_id
		LEFT JOIN container_types ct ON ct.id = c.container_type_id
		LEFT JOIN locations cl ON cl.id = c.location_id
		LEFT JOIN locations sl ON sl.id = wss.location_id
		LEFT JOIN latest_runs lr ON lr.scan_id = wss.id
		LEFT JOIN image_counts ic ON ic.scan_id = wss.id
		LEFT JOIN candidate_counts cc ON cc.scan_id = wss.id
		WHERE ($1::uuid IS NULL OR wss.container_id = $1::uuid)
		ORDER BY
			COALESCE(cc.pending_candidate_count, 0) DESC,
			COALESCE(wss.updated_datetime, wss.created_datetime) DESC,
			wss.id DESC
	`, containerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	scans := make([]models.WholeSceneReviewScanSummary, 0)
	for rows.Next() {
		var scan models.WholeSceneReviewScanSummary
		var containerID *string
		var containerName *string
		var containerType *string
		var containerTypeID *string
		var containerTypeName *string
		var containerLocationID *string
		var containerLocationName *string
		var containerLocationDescription *string
		var run models.WholeSceneAnalysisRun
		var runID *string
		var runNumber *int
		var runStatus *string
		var runProviderType *string
		var runModelName *string
		var runPromptVersion *string
		var runRawResponseAvailable *bool
		var runQueuedDatetime *time.Time
		var runCreatedDatetime *time.Time
		if err := rows.Scan(
			&scan.ID,
			&scan.UploadSessionID,
			&containerID,
			&containerName,
			&containerType,
			&containerTypeID,
			&containerTypeName,
			&containerLocationID,
			&containerLocationName,
			&containerLocationDescription,
			&scan.LocationID,
			&scan.LocationName,
			&scan.LocationDetail,
			&scan.InventoryGroup.ID,
			&scan.InventoryGroup.Code,
			&scan.InventoryGroup.Name,
			&scan.Hint,
			&scan.Status,
			&scan.ImageCount,
			&scan.ProcessedImages,
			&scan.FailedImages,
			&scan.CandidateCounts.Pending,
			&scan.CandidateCounts.Approved,
			&scan.CandidateCounts.Rejected,
			&scan.CandidateCounts.Total,
			&runID,
			&runNumber,
			&runStatus,
			&run.AIProviderConfigID,
			&runProviderType,
			&runModelName,
			&runPromptVersion,
			&runRawResponseAvailable,
			&run.ErrorMessage,
			&runQueuedDatetime,
			&run.StartedDatetime,
			&run.CompletedDatetime,
			&runCreatedDatetime,
			&run.UpdatedDatetime,
			&scan.CreatedDatetime,
			&scan.UpdatedDatetime,
		); err != nil {
			return nil, err
		}
		if containerID != nil && containerName != nil {
			scan.Container = &models.InventoryContainer{
				ID:                  *containerID,
				Name:                *containerName,
				Type:                containerType,
				ContainerTypeID:     containerTypeID,
				ContainerTypeName:   containerTypeName,
				LocationID:          containerLocationID,
				LocationName:        containerLocationName,
				LocationDescription: containerLocationDescription,
			}
		}
		if runID != nil {
			run.ID = *runID
			run.RunNumber = wholeSceneIntValue(runNumber)
			run.Status = wholeSceneStringValue(runStatus)
			run.ProviderType = wholeSceneStringValue(runProviderType)
			run.ModelName = wholeSceneStringValue(runModelName)
			run.PromptVersion = wholeSceneStringValue(runPromptVersion)
			run.RawResponseAvailable = boolValue(runRawResponseAvailable)
			run.QueuedDatetime = wholeSceneTimeValue(runQueuedDatetime)
			run.CreatedDatetime = wholeSceneTimeValue(runCreatedDatetime)
			scan.LatestAnalysisRun = &run
		}
		scans = append(scans, scan)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for index := range scans {
		images, err := s.loadScanImages(ctx, scans[index].ID)
		if err != nil {
			return nil, err
		}
		scans[index].Images = images
	}

	return scans, nil
}

func (h *WholeSceneHandler) CreateScan(w http.ResponseWriter, r *http.Request) {
	if err := os.MkdirAll(h.store.intakeDir, 0755); err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_unavailable", "failed to prepare intake directory")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.store.maxBodyBytes())
	if err := r.ParseMultipartForm(h.store.parseMemoryBytes); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid multipart form data")
		return
	}

	req, err := parseWholeSceneCreateMetadata(r.MultipartForm)
	if err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	normalizeWholeSceneCreateRequest(&req)
	if err := validateWholeSceneCreateRequest(req, h.store.maxUploadBytes, r.MultipartForm); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	writtenFiles := make([]writtenUploadFile, 0)
	scanID, err := h.store.CreateScan(ctx, req, r.MultipartForm, &writtenFiles)
	if err != nil {
		log.Printf("whole scene scan upload failed: %v", err)
		cleanupWrittenFiles(writtenFiles)
		switch {
		case errors.Is(err, errWholeSceneContainerNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "container was not found")
		case errors.Is(err, errWholeSceneArchivedContainer):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign whole scene scan to archived container.")
		case errors.Is(err, errArchivedLocation):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign whole scene scan to archived location.")
		case errors.Is(err, errItemGroupNotFound):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
		case errors.Is(err, errArchivedItemGroup):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign whole scene scan to archived inventory group.")
		case errors.Is(err, errInvalidUploadRequest):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "upload_failed", "failed to create whole scene scan")
		}
		return
	}

	scan, err := h.store.GetScan(ctx, scanID)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load whole scene scan")
		return
	}

	respond.JSON(w, http.StatusCreated, models.GetWholeSceneScanResponse{Scan: scan})
}

func (h *WholeSceneHandler) ListReviewScans(w http.ResponseWriter, r *http.Request) {
	containerID := strings.TrimSpace(r.URL.Query().Get("container_id"))
	if containerID != "" && !isUUID(containerID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "container_id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	var containerFilter *string
	if containerID != "" {
		containerFilter = &containerID
	}

	scans, err := h.store.ListReviewScans(ctx, containerFilter)
	if err != nil {
		log.Printf("failed to load whole scene review scans: %v", err)
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load whole scene review scans")
		return
	}

	respond.JSON(w, http.StatusOK, models.ListWholeSceneReviewScansResponse{Scans: scans})
}

func (h *WholeSceneHandler) QueueAnalysis(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	runID, queued, err := h.store.QueueAnalysis(ctx, scanID)
	if err != nil {
		log.Printf("failed to queue whole scene analysis for scan %s: %v", scanID, err)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "whole scene scan was not found")
		case errors.Is(err, errWholeSceneImagesInFlight):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, errWholeSceneNoUsableImages):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		case errors.Is(err, errWholeSceneNoProvider):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		default:
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		}
		return
	}

	scan, err := h.store.GetScan(ctx, scanID)
	if err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load whole scene scan")
		return
	}

	response := models.QueueWholeSceneAnalysisResponse{
		Scan:   scan,
		Queued: queued,
	}
	if run := findWholeSceneAnalysisRun(scan.AnalysisRuns, runID); run != nil {
		response.AnalysisRun = run
	}
	respond.JSON(w, http.StatusAccepted, response)
}

func (h *WholeSceneHandler) AddCandidate(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}

	var req models.AddWholeSceneCandidateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.store.AddManualCandidate(ctx, scanID, req)
	if err != nil {
		h.respondCandidateMutationError(w, err, "failed to add whole scene candidate")
		return
	}

	respond.JSON(w, http.StatusCreated, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) PatchCandidate(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}
	if !isUUID(candidateID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene candidate id must be a valid UUID")
		return
	}

	var req models.PatchWholeSceneCandidateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.store.PatchCandidate(ctx, scanID, candidateID, req)
	if err != nil {
		h.respondCandidateMutationError(w, err, "failed to update whole scene candidate")
		return
	}

	respond.JSON(w, http.StatusOK, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) AssistCandidate(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}
	if !isUUID(candidateID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene candidate id must be a valid UUID")
		return
	}

	var req models.AssistWholeSceneCandidateRequest
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.store.QueueCandidateAIAssist(ctx, scanID, candidateID, req)
	if err != nil {
		h.respondCandidateMutationError(w, err, "failed to run Whole Scene candidate AI Assist")
		return
	}

	respond.JSON(w, http.StatusOK, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) RejectCandidate(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}
	if !isUUID(candidateID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene candidate id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.store.RejectCandidate(ctx, scanID, candidateID)
	if err != nil {
		h.respondCandidateMutationError(w, err, "failed to reject whole scene candidate")
		return
	}

	respond.JSON(w, http.StatusOK, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) ApproveCandidate(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}
	if !isUUID(candidateID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene candidate id must be a valid UUID")
		return
	}

	var req models.ApproveWholeSceneCandidateRequest
	if r.Body != nil {
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid JSON request body")
			return
		}
	}
	if req.InventoryGroupID != nil {
		trimmed := strings.TrimSpace(*req.InventoryGroupID)
		if trimmed == "" {
			req.InventoryGroupID = nil
		} else {
			if !isUUID(trimmed) {
				respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "inventory_group_id must be a valid UUID")
				return
			}
			req.InventoryGroupID = &trimmed
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	result, err := h.store.ApproveCandidate(ctx, scanID, candidateID, req)
	if err != nil {
		h.respondCandidateMutationError(w, err, "failed to approve whole scene candidate")
		return
	}

	respond.JSON(w, http.StatusOK, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) UploadCandidateImages(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	if !isUUID(scanID) || !isUUID(candidateID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "scan id and candidate id must be valid UUIDs")
		return
	}

	if err := h.store.ensureCandidateImageDirectories(); err != nil {
		respond.ErrorCode(w, http.StatusInternalServerError, "storage_unavailable", "failed to prepare image directories")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, h.store.maxCandidateImageBodyBytes())
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "invalid multipart form data")
		return
	}

	headers := r.MultipartForm.File[itemImageMultipartField]
	if len(headers) == 0 {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "at least one image file is required")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	savedFiles := make([]savedItemImageFile, 0, len(headers))
	writtenFiles := make([]writtenItemImageFile, 0, len(headers))
	for _, header := range headers {
		saved, err := h.store.saveCandidateImageUpload(header, &writtenFiles)
		if err != nil {
			cleanupWrittenItemFiles(writtenFiles)
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
			return
		}
		savedFiles = append(savedFiles, saved)
	}

	result, err := h.store.AppendCandidateImages(ctx, scanID, candidateID, savedFiles)
	if err != nil {
		cleanupWrittenItemFiles(writtenFiles)
		h.store.cleanupSavedCandidateVariants(savedFiles)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "Whole Scene candidate was not found")
		case errors.Is(err, errWholeSceneCandidateApproved), errors.Is(err, errWholeSceneCandidateRejected):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		case errors.Is(err, errInvalidUploadRequest):
			respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", err.Error())
		default:
			log.Printf("failed to upload Whole Scene candidate images scan_id=%s candidate_id=%s: %v", scanID, candidateID, err)
			respond.ErrorCode(w, http.StatusInternalServerError, "upload_failed", "failed to upload Whole Scene candidate images")
		}
		return
	}

	respond.JSON(w, http.StatusCreated, wholeSceneCandidateMutationResponse(result))
}

func (h *WholeSceneHandler) DeleteCandidateImage(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	candidateID := strings.TrimSpace(chi.URLParam(r, "candidateID"))
	cropID := strings.TrimSpace(chi.URLParam(r, "cropID"))
	if !isUUID(scanID) || !isUUID(candidateID) || !isUUID(cropID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "scan id, candidate id, and crop id must be valid UUIDs")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	response, err := h.store.DeleteCandidateImage(ctx, scanID, candidateID, cropID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "Whole Scene candidate image was not found")
		case errors.Is(err, errWholeSceneCandidateApproved), errors.Is(err, errUnsafeManagedFilePath), errors.Is(err, errManagedFileIsDirectory):
			respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
		default:
			log.Printf("failed to delete Whole Scene candidate image scan_id=%s candidate_id=%s crop_id=%s: %v", scanID, candidateID, cropID, err)
			respond.ErrorCode(w, http.StatusInternalServerError, "delete_failed", "failed to delete Whole Scene candidate image")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func (h *WholeSceneHandler) respondCandidateMutationError(w http.ResponseWriter, err error, fallbackMessage string) {
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "whole scene scan or candidate was not found")
	case errors.Is(err, errWholeSceneCandidateApproved):
		respond.ErrorCode(w, http.StatusConflict, "conflict", "whole scene candidate is already approved")
	case errors.Is(err, errWholeSceneCandidateRejected):
		respond.ErrorCode(w, http.StatusConflict, "conflict", "whole scene candidate is rejected")
	case errors.Is(err, errWholeSceneCandidateAIBusy):
		respond.ErrorCode(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, errItemGroupNotFound):
		respond.ErrorCode(w, http.StatusNotFound, "not_found", "inventory group was not found")
	case errors.Is(err, errArchivedItemGroup):
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "Cannot assign item to archived inventory group.")
	default:
		message := strings.TrimSpace(err.Error())
		if message == "" {
			message = fallbackMessage
		}
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", message)
	}
}

func (s *WholeSceneStore) loadScan(ctx context.Context, scanID string) (models.WholeSceneScan, error) {
	var scan models.WholeSceneScan
	var containerID *string
	var containerName *string
	var containerType *string
	var containerTypeID *string
	var containerTypeName *string
	var containerLocationID *string
	var containerLocationName *string
	var containerLocationDescription *string

	err := s.pool.QueryRow(ctx, `
		SELECT
			wss.id::text,
			wss.upload_session_id::text,
			wss.container_id::text,
			c.name,
			c.type,
			c.container_type_id::text,
			ct.name,
			c.location_id::text,
			cl.name,
			c.location_description,
			wss.location_id::text,
			sl.name,
			wss.location_detail,
			ig.id::text,
			ig.code,
			ig.name,
			wss.hint,
			wss.status,
			wss.created_by,
			wss.created_datetime,
			wss.updated_by,
			wss.updated_datetime
		FROM whole_scene_scans wss
		JOIN inventory_groups ig ON ig.id = wss.inventory_group_id
		LEFT JOIN containers c ON c.id = wss.container_id
		LEFT JOIN container_types ct ON ct.id = c.container_type_id
		LEFT JOIN locations cl ON cl.id = c.location_id
		LEFT JOIN locations sl ON sl.id = wss.location_id
		WHERE wss.id = $1::uuid
	`, scanID).Scan(
		&scan.ID,
		&scan.UploadSessionID,
		&containerID,
		&containerName,
		&containerType,
		&containerTypeID,
		&containerTypeName,
		&containerLocationID,
		&containerLocationName,
		&containerLocationDescription,
		&scan.LocationID,
		&scan.LocationName,
		&scan.LocationDetail,
		&scan.InventoryGroup.ID,
		&scan.InventoryGroup.Code,
		&scan.InventoryGroup.Name,
		&scan.Hint,
		&scan.Status,
		&scan.CreatedBy,
		&scan.CreatedDatetime,
		&scan.UpdatedBy,
		&scan.UpdatedDatetime,
	)
	if err != nil {
		return models.WholeSceneScan{}, err
	}

	if containerID != nil && containerName != nil {
		scan.Container = &models.InventoryContainer{
			ID:                  *containerID,
			Name:                *containerName,
			Type:                containerType,
			ContainerTypeID:     containerTypeID,
			ContainerTypeName:   containerTypeName,
			LocationID:          containerLocationID,
			LocationName:        containerLocationName,
			LocationDescription: containerLocationDescription,
		}
	}

	return scan, nil
}

func (s *WholeSceneStore) loadScanImages(ctx context.Context, scanID string) ([]models.WholeSceneScanImage, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			wssi.id::text,
			wssi.image_asset_id::text,
			wssi.sort_order,
			wssi.created_datetime,
			ia.client_file_id,
			ia.original_filename,
			ia.stored_filename,
			ia.mime_type,
			ia.file_size_bytes,
			ia.status,
			ia.error_message,
			ia.upload_order,
			ia.thumbnail_path IS NOT NULL AND ia.thumbnail_path <> '',
			ia.normalized_path IS NOT NULL AND ia.normalized_path <> ''
		FROM whole_scene_scan_images wssi
		JOIN image_assets ia ON ia.id = wssi.image_asset_id
		WHERE wssi.scan_id = $1::uuid
		ORDER BY wssi.sort_order ASC, wssi.created_datetime ASC, wssi.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]models.WholeSceneScanImage, 0)
	for rows.Next() {
		var image models.WholeSceneScanImage
		if err := rows.Scan(
			&image.ID,
			&image.ImageAssetID,
			&image.SortOrder,
			&image.CreatedDatetime,
			&image.Image.ClientFileID,
			&image.Image.OriginalFilename,
			&image.Image.StoredFilename,
			&image.Image.MimeType,
			&image.Image.FileSizeBytes,
			&image.Image.Status,
			&image.Image.ErrorMessage,
			&image.Image.UploadOrder,
			&image.Image.ThumbnailAvailable,
			&image.Image.NormalizedAvailable,
		); err != nil {
			return nil, err
		}
		image.Image.ImageAssetID = image.ImageAssetID
		images = append(images, image)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return images, nil
}

func (s *WholeSceneStore) loadAnalysisRuns(ctx context.Context, scanID string) ([]models.WholeSceneAnalysisRun, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			id::text,
			run_number,
			status,
			ai_provider_config_id::text,
			provider_type,
			model_name,
			prompt_version,
			raw_response_text IS NOT NULL AND btrim(raw_response_text) <> '',
			error_message,
			queued_datetime,
			started_datetime,
			completed_datetime,
			created_datetime,
			updated_datetime
		FROM whole_scene_analysis_runs
		WHERE scan_id = $1::uuid
		ORDER BY run_number DESC, queued_datetime DESC, id DESC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	runs := make([]models.WholeSceneAnalysisRun, 0)
	for rows.Next() {
		var run models.WholeSceneAnalysisRun
		if err := rows.Scan(
			&run.ID,
			&run.RunNumber,
			&run.Status,
			&run.AIProviderConfigID,
			&run.ProviderType,
			&run.ModelName,
			&run.PromptVersion,
			&run.RawResponseAvailable,
			&run.ErrorMessage,
			&run.QueuedDatetime,
			&run.StartedDatetime,
			&run.CompletedDatetime,
			&run.CreatedDatetime,
			&run.UpdatedDatetime,
		); err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return runs, nil
}

func (s *WholeSceneStore) loadCandidates(ctx context.Context, scanID string) ([]models.WholeSceneCandidate, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			wsc.id::text,
			wsc.analysis_run_id::text,
			wsc.source,
			wsc.status,
			wsc.title,
			wsc.description,
			wsc.approx_value::text,
			wsc.confidence_label,
			wsc.uncertainty_notes,
			wsc.raw_candidate,
			wsc.parse_warnings,
			wsc.ai_assist_status,
			wsc.ai_assist_error_message,
			wsc.ai_assist_requested_at,
			wsc.ai_assist_started_at,
			wsc.ai_assist_completed_at,
			wsc.ai_assist_provider_config_id::text,
			wsc.ai_assist_provider,
			wsc.ai_assist_model,
			wsc.approved_item_id::text,
			wsc.approved_datetime,
			wsc.rejected_datetime,
			wsc.created_by,
			wsc.created_datetime,
			wsc.updated_by,
			wsc.updated_datetime,
			i.title,
			i.approx_value::text,
			COALESCE(i.current_inventory, true),
			COALESCE(i.archived, false)
		FROM whole_scene_candidates wsc
		LEFT JOIN items i ON i.id = wsc.approved_item_id
		WHERE wsc.scan_id = $1::uuid
		ORDER BY wsc.created_datetime ASC, wsc.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	candidates := make([]models.WholeSceneCandidate, 0)
	for rows.Next() {
		var candidate models.WholeSceneCandidate
		var rawCandidate []byte
		var approvedTitle *string
		var approvedApproxValue *string
		var approvedCurrentInventory *bool
		var approvedArchived *bool
		if err := rows.Scan(
			&candidate.ID,
			&candidate.AnalysisRunID,
			&candidate.Source,
			&candidate.Status,
			&candidate.Title,
			&candidate.Description,
			&candidate.ApproxValue,
			&candidate.ConfidenceLabel,
			&candidate.UncertaintyNotes,
			&rawCandidate,
			&candidate.ParseWarnings,
			&candidate.AIAssistStatus,
			&candidate.AIAssistErrorMessage,
			&candidate.AIAssistRequestedAt,
			&candidate.AIAssistStartedAt,
			&candidate.AIAssistCompletedAt,
			&candidate.AIAssistProviderConfigID,
			&candidate.AIAssistProvider,
			&candidate.AIAssistModel,
			&candidate.ApprovedItemID,
			&candidate.ApprovedDatetime,
			&candidate.RejectedDatetime,
			&candidate.CreatedBy,
			&candidate.CreatedDatetime,
			&candidate.UpdatedBy,
			&candidate.UpdatedDatetime,
			&approvedTitle,
			&approvedApproxValue,
			&approvedCurrentInventory,
			&approvedArchived,
		); err != nil {
			return nil, err
		}
		candidate.RawCandidate = rawMessageFromBytes(rawCandidate)
		candidate.Appearances = []models.WholeSceneCandidateAppearance{}
		candidate.Crops = []models.WholeSceneCandidateCrop{}
		if candidate.ApprovedItemID != nil {
			candidate.ApprovedItem = &models.WholeSceneApprovedItem{
				ID:               *candidate.ApprovedItemID,
				Title:            approvedTitle,
				ApproxValue:      approvedApproxValue,
				CurrentInventory: boolValue(approvedCurrentInventory),
				Archived:         boolValue(approvedArchived),
			}
		}
		candidates = append(candidates, candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return candidates, nil
}

func (s *WholeSceneStore) loadCandidateAppearances(ctx context.Context, scanID string) ([]models.WholeSceneCandidateAppearance, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			wsca.id::text,
			wsca.candidate_id::text,
			wsca.scan_image_id::text,
			wsca.source_image_index,
			wsca.bounding_box_x::float8,
			wsca.bounding_box_y::float8,
			wsca.bounding_box_width::float8,
			wsca.bounding_box_height::float8,
			wsca.localization_data,
			wsca.confidence_label,
			wsca.notes,
			wsca.created_datetime
		FROM whole_scene_candidate_appearances wsca
		JOIN whole_scene_candidates wsc ON wsc.id = wsca.candidate_id
		WHERE wsc.scan_id = $1::uuid
		ORDER BY wsca.created_datetime ASC, wsca.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	appearances := make([]models.WholeSceneCandidateAppearance, 0)
	for rows.Next() {
		var appearance models.WholeSceneCandidateAppearance
		var localizationData []byte
		if err := rows.Scan(
			&appearance.ID,
			&appearance.CandidateID,
			&appearance.ScanImageID,
			&appearance.SourceImageIndex,
			&appearance.BoundingBox.X,
			&appearance.BoundingBox.Y,
			&appearance.BoundingBox.Width,
			&appearance.BoundingBox.Height,
			&localizationData,
			&appearance.ConfidenceLabel,
			&appearance.Notes,
			&appearance.CreatedDatetime,
		); err != nil {
			return nil, err
		}
		appearance.LocalizationData = rawMessageFromBytes(localizationData)
		appearances = append(appearances, appearance)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return appearances, nil
}

func (s *WholeSceneStore) loadCandidateCrops(ctx context.Context, scanID string) ([]models.WholeSceneCandidateCrop, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			wscc.id::text,
			wscc.candidate_id::text,
			wscc.appearance_id::text,
			wscc.scan_image_id::text,
			wscc.crop_image_asset_id::text,
			wscc.status,
			wscc.is_preferred,
			wscc.bounding_box_x::float8,
			wscc.bounding_box_y::float8,
			wscc.bounding_box_width::float8,
			wscc.bounding_box_height::float8,
			wscc.crop_metadata,
			wscc.error_message,
			wscc.created_datetime,
			wscc.updated_datetime,
			ia.original_filename,
			ia.stored_filename,
			ia.mime_type,
			ia.file_size_bytes,
			ia.status,
			ia.error_message,
			ia.upload_order,
			ia.thumbnail_path IS NOT NULL AND ia.thumbnail_path <> '',
			ia.normalized_path IS NOT NULL AND ia.normalized_path <> ''
		FROM whole_scene_candidate_crops wscc
		JOIN whole_scene_candidates wsc ON wsc.id = wscc.candidate_id
		LEFT JOIN image_assets ia ON ia.id = wscc.crop_image_asset_id
		WHERE wsc.scan_id = $1::uuid
		ORDER BY wscc.is_preferred DESC, wscc.created_datetime ASC, wscc.id ASC
	`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	crops := make([]models.WholeSceneCandidateCrop, 0)
	for rows.Next() {
		var crop models.WholeSceneCandidateCrop
		var cropMetadata []byte
		var cropOriginalFilename *string
		var cropStoredFilename *string
		var cropMimeType *string
		var cropFileSizeBytes *int64
		var cropStatus *string
		var cropErrorMessage *string
		var cropUploadOrder *int
		var cropThumbnailAvailable *bool
		var cropNormalizedAvailable *bool
		if err := rows.Scan(
			&crop.ID,
			&crop.CandidateID,
			&crop.AppearanceID,
			&crop.ScanImageID,
			&crop.CropImageAssetID,
			&crop.Status,
			&crop.IsPreferred,
			&crop.BoundingBox.X,
			&crop.BoundingBox.Y,
			&crop.BoundingBox.Width,
			&crop.BoundingBox.Height,
			&cropMetadata,
			&crop.ErrorMessage,
			&crop.CreatedDatetime,
			&crop.UpdatedDatetime,
			&cropOriginalFilename,
			&cropStoredFilename,
			&cropMimeType,
			&cropFileSizeBytes,
			&cropStatus,
			&cropErrorMessage,
			&cropUploadOrder,
			&cropThumbnailAvailable,
			&cropNormalizedAvailable,
		); err != nil {
			return nil, err
		}
		crop.CropMetadata = rawMessageFromBytes(cropMetadata)
		if crop.CropImageAssetID != nil && cropStatus != nil && cropUploadOrder != nil {
			crop.CropImage = &models.WholeSceneImageAsset{
				ImageAssetID:        *crop.CropImageAssetID,
				OriginalFilename:    cropOriginalFilename,
				StoredFilename:      cropStoredFilename,
				MimeType:            cropMimeType,
				FileSizeBytes:       cropFileSizeBytes,
				Status:              *cropStatus,
				ErrorMessage:        cropErrorMessage,
				UploadOrder:         *cropUploadOrder,
				ThumbnailAvailable:  boolValue(cropThumbnailAvailable),
				NormalizedAvailable: boolValue(cropNormalizedAvailable),
			}
		}
		crops = append(crops, crop)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return crops, nil
}

func (h *WholeSceneHandler) GetScan(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	scan, err := h.store.GetScan(ctx, scanID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "whole scene scan was not found")
		default:
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to load whole scene scan")
		}
		return
	}

	respond.JSON(w, http.StatusOK, models.GetWholeSceneScanResponse{Scan: scan})
}

func (h *WholeSceneHandler) DeleteScan(w http.ResponseWriter, r *http.Request) {
	scanID := strings.TrimSpace(chi.URLParam(r, "id"))
	if !isUUID(scanID) {
		respond.ErrorCode(w, http.StatusBadRequest, "validation_failed", "whole scene scan id must be a valid UUID")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	response, err := h.store.DeleteScan(ctx, scanID)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			respond.ErrorCode(w, http.StatusNotFound, "not_found", "whole scene scan was not found")
		default:
			log.Printf("failed to delete whole scene scan %s: %v", scanID, err)
			respond.ErrorCode(w, http.StatusInternalServerError, "database_error", "failed to delete whole scene scan")
		}
		return
	}

	respond.JSON(w, http.StatusOK, response)
}

func assignLatestWholeSceneAnalysisRun(scan *models.WholeSceneScan) {
	if len(scan.AnalysisRuns) == 0 {
		scan.LatestAnalysisRun = nil
		return
	}
	latest := scan.AnalysisRuns[0]
	scan.LatestAnalysisRun = &latest
}

func findWholeSceneAnalysisRun(runs []models.WholeSceneAnalysisRun, runID string) *models.WholeSceneAnalysisRun {
	for index := range runs {
		if runs[index].ID == runID {
			return &runs[index]
		}
	}
	return nil
}

func rawMessageFromBytes(value []byte) *json.RawMessage {
	if len(value) == 0 {
		return nil
	}
	copied := append([]byte(nil), value...)
	message := json.RawMessage(copied)
	return &message
}

func ensureWholeSceneContainer(ctx context.Context, q reviewQuerier, containerID string) error {
	var archived bool
	if err := q.QueryRow(ctx, `
		SELECT archived
		FROM containers
		WHERE id = $1::uuid
	`, containerID).Scan(&archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return errWholeSceneContainerNotFound
		}
		return err
	}
	if archived {
		return errWholeSceneArchivedContainer
	}
	return nil
}

func ensureWholeSceneLocation(ctx context.Context, q reviewQuerier, locationID string) error {
	var archived bool
	if err := q.QueryRow(ctx, `
		SELECT archived
		FROM locations
		WHERE id = $1::uuid
	`, locationID).Scan(&archived); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: location was not found", errInvalidUploadRequest)
		}
		return err
	}
	if archived {
		return errArchivedLocation
	}
	return nil
}

func (s *WholeSceneStore) maxBodyBytes() int64 {
	const maxFormOverheadBytes = 10 << 20
	return s.maxUploadBytes*wholeSceneMaxImageCount + maxFormOverheadBytes
}
