package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fastsell-api/internal/models"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWholeSceneHandlerGetScanRejectsInvalidID(t *testing.T) {
	handler := NewWholeSceneHandler(nil)
	router := chi.NewRouter()
	router.Get("/whole-scene/scans/{id}", handler.GetScan)

	req := httptest.NewRequest(http.MethodGet, "/whole-scene/scans/not-a-uuid", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Error != "validation_failed" {
		t.Fatalf("expected validation_failed error, got %q", response.Error)
	}
}

func TestWholeSceneHandlerListReviewScansRejectsInvalidContainerID(t *testing.T) {
	handler := NewWholeSceneHandler(nil)

	req := httptest.NewRequest(http.MethodGet, "/review/whole-scene-scans?container_id=not-a-uuid", nil)
	rec := httptest.NewRecorder()

	handler.ListReviewScans(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, rec.Code)
	}

	var response struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Error != "validation_failed" {
		t.Fatalf("expected validation_failed error, got %q", response.Error)
	}
}

func TestAssignLatestWholeSceneAnalysisRunUsesFirstRun(t *testing.T) {
	scan := models.WholeSceneScan{
		AnalysisRuns: []models.WholeSceneAnalysisRun{
			{ID: "run-2", RunNumber: 2, Status: "succeeded"},
			{ID: "run-1", RunNumber: 1, Status: "failed"},
		},
	}

	assignLatestWholeSceneAnalysisRun(&scan)

	if scan.LatestAnalysisRun == nil {
		t.Fatal("expected latest analysis run")
	}
	if scan.LatestAnalysisRun.ID != "run-2" {
		t.Fatalf("expected run-2 as latest run, got %q", scan.LatestAnalysisRun.ID)
	}
}

func TestRawMessageFromBytesReturnsIndependentCopy(t *testing.T) {
	source := []byte(`{"title":"Keyboard"}`)
	message := rawMessageFromBytes(source)
	if message == nil {
		t.Fatal("expected raw message")
	}

	source[10] = 'X'
	if string(*message) != `{"title":"Keyboard"}` {
		t.Fatalf("expected copied raw message, got %s", string(*message))
	}
}

func TestNormalizeWholeSceneCreateRequestTrimsMetadata(t *testing.T) {
	hint := "  mixed electronics on workbench  "
	inventoryGroupID := " 123e4567-e89b-12d3-a456-426614174000 "
	req := wholeSceneCreateRequest{
		IntakeContext: intakeContext{
			ContainerID:    " 123e4567-e89b-12d3-a456-426614174001 ",
			LocationID:     " 123e4567-e89b-12d3-a456-426614174002 ",
			LocationDetail: "  left side  ",
		},
		Hint:             &hint,
		InventoryGroupID: &inventoryGroupID,
		Files: []uploadFileInput{
			{
				ClientFileID:     " scene_1 ",
				OriginalFilename: `C:\fakepath\Scene 1.JPG`,
				MimeType:         " IMAGE/JPEG ",
			},
		},
	}

	normalizeWholeSceneCreateRequest(&req)

	if req.IntakeContext.ContainerID != "123e4567-e89b-12d3-a456-426614174001" {
		t.Fatalf("expected trimmed container id, got %q", req.IntakeContext.ContainerID)
	}
	if req.IntakeContext.LocationDetail != "left side" {
		t.Fatalf("expected trimmed location detail, got %q", req.IntakeContext.LocationDetail)
	}
	if req.Hint == nil || *req.Hint != "mixed electronics on workbench" {
		t.Fatalf("expected trimmed hint, got %#v", req.Hint)
	}
	if req.InventoryGroupID == nil || *req.InventoryGroupID != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("expected trimmed inventory group id, got %#v", req.InventoryGroupID)
	}
	if req.Files[0].ClientFileID != "scene_1" {
		t.Fatalf("expected trimmed client file id, got %q", req.Files[0].ClientFileID)
	}
	if req.Files[0].OriginalFilename != "Scene 1.JPG" {
		t.Fatalf("expected cleaned filename, got %q", req.Files[0].OriginalFilename)
	}
	if req.Files[0].MimeType != "image/jpeg" {
		t.Fatalf("expected normalized mime type, got %q", req.Files[0].MimeType)
	}
}

func TestValidateWholeSceneCreateRequestRequiresSourceImage(t *testing.T) {
	req := wholeSceneCreateRequest{}
	err := validateWholeSceneCreateRequest(req, 25*1024*1024, &multipart.Form{File: map[string][]*multipart.FileHeader{}})
	if err == nil {
		t.Fatal("expected missing source image validation error")
	}
}

func TestValidateWholeSceneCreateRequestRejectsTooManyImages(t *testing.T) {
	req := wholeSceneCreateRequest{
		Files: make([]uploadFileInput, wholeSceneMaxImageCount+1),
	}
	form := &multipart.Form{File: map[string][]*multipart.FileHeader{}}
	for i := range req.Files {
		clientID := fmt.Sprintf("scene_%d", i+1)
		req.Files[i] = uploadFileInput{
			ClientFileID:     clientID,
			OriginalFilename: clientID + ".jpg",
			MimeType:         "image/jpeg",
		}
		form.File[multipartFileFieldName(clientID)] = []*multipart.FileHeader{{Filename: clientID + ".jpg", Size: 128}}
	}

	err := validateWholeSceneCreateRequest(req, 25*1024*1024, form)
	if err == nil {
		t.Fatal("expected too many source images validation error")
	}
}

func TestValidateWholeSceneCreateRequestRejectsDuplicateClientFileID(t *testing.T) {
	req := wholeSceneCreateRequest{
		Files: []uploadFileInput{
			{ClientFileID: "scene_1", OriginalFilename: "scene-1.jpg", MimeType: "image/jpeg"},
			{ClientFileID: "scene_1", OriginalFilename: "scene-2.jpg", MimeType: "image/jpeg"},
		},
	}
	form := &multipart.Form{File: map[string][]*multipart.FileHeader{
		multipartFileFieldName("scene_1"): {&multipart.FileHeader{Filename: "scene-1.jpg", Size: 128}},
	}}

	err := validateWholeSceneCreateRequest(req, 25*1024*1024, form)
	if err == nil {
		t.Fatal("expected duplicate client_file_id validation error")
	}
}

func TestValidateWholeSceneCreateRequestRejectsMissingFilePart(t *testing.T) {
	req := wholeSceneCreateRequest{
		Files: []uploadFileInput{
			{ClientFileID: "scene_1", OriginalFilename: "scene-1.jpg", MimeType: "image/jpeg"},
		},
	}
	form := &multipart.Form{File: map[string][]*multipart.FileHeader{}}

	err := validateWholeSceneCreateRequest(req, 25*1024*1024, form)
	if err == nil {
		t.Fatal("expected missing multipart file part validation error")
	}
}

func TestValidateWholeSceneQueueableRules(t *testing.T) {
	if err := validateWholeSceneQueueable([]wholeSceneSourceImageStatus{{Status: "processed"}, {Status: "failed"}}); err != nil {
		t.Fatalf("expected processed plus failed images to be queueable: %v", err)
	}
	if !errorsIs(validateWholeSceneQueueable([]wholeSceneSourceImageStatus{{Status: "processed"}, {Status: "pending"}}), errWholeSceneImagesInFlight) {
		t.Fatal("expected pending image to block queueing")
	}
	if !errorsIs(validateWholeSceneQueueable([]wholeSceneSourceImageStatus{{Status: "failed"}}), errWholeSceneNoUsableImages) {
		t.Fatal("expected all-failed images to be rejected")
	}
	if !errorsIs(validateWholeSceneQueueable(nil), errWholeSceneNoUsableImages) {
		t.Fatal("expected scan with no source images to be rejected")
	}
}

func TestValidateWholeSceneGeminiProvider(t *testing.T) {
	key := "test-key"
	provider := storedAIProviderConfig{
		ProviderType:   "gemini",
		Enabled:        true,
		VisionEnabled:  true,
		ModelName:      "gemini-test",
		APIKeyValue:    &key,
		TimeoutSeconds: 5,
	}
	if err := validateWholeSceneGeminiProvider(provider); err != nil {
		t.Fatalf("expected provider to be valid: %v", err)
	}

	provider.ProviderType = "openai"
	if err := validateWholeSceneGeminiProvider(provider); err == nil {
		t.Fatal("expected unsupported provider type to fail")
	}
}

func TestParseWholeSceneGeminiResponseToleratesMalformedCandidates(t *testing.T) {
	candidateText := `{
		"candidates": [
			{
				"candidate_key": "vintage_keyboard",
				"title": "Vintage Keyboard",
				"description": "IBM-style mechanical keyboard",
				"approx_value": 42.5,
				"confidence": "High",
				"uncertainty": "part number not visible",
				"appearances": [
					{
						"candidate_key": "vintage_keyboard",
						"source_image_index": 0,
						"bbox": {"x": 0.1, "y": 0.2, "width": 0.3, "height": 0.4},
						"confidence": 0.8
					}
				]
			},
			{"description": "   "}
		]
	}`
	rawResponse, err := json.Marshal(map[string]any{
		"candidates": []map[string]any{
			{
				"content": map[string]any{
					"parts": []map[string]any{
						{"text": "```json\n" + candidateText + "\n```"},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal Gemini response: %v", err)
	}

	parsed, err := parseWholeSceneGeminiResponse(rawResponse)
	if err != nil {
		t.Fatalf("expected valid candidate to be preserved: %v", err)
	}
	if len(parsed.Candidates) != 1 {
		t.Fatalf("expected one parsed candidate, got %d", len(parsed.Candidates))
	}
	if len(parsed.Warnings) == 0 {
		t.Fatal("expected malformed candidate warning")
	}

	candidate := parsed.Candidates[0]
	if candidate.Title != "Vintage Keyboard" {
		t.Fatalf("expected candidate title, got %q", candidate.Title)
	}
	if candidate.ApproxValue == nil || *candidate.ApproxValue != "42.50" {
		t.Fatalf("expected normalized approx value, got %#v", candidate.ApproxValue)
	}
	if candidate.ConfidenceLabel == nil || *candidate.ConfidenceLabel != "high" {
		t.Fatalf("expected normalized confidence, got %#v", candidate.ConfidenceLabel)
	}
	if len(candidate.Appearances) != 1 {
		t.Fatalf("expected one appearance, got %d", len(candidate.Appearances))
	}
	appearance := candidate.Appearances[0]
	if appearance.SourceImageIndex == nil || *appearance.SourceImageIndex != 0 {
		t.Fatalf("expected source image index 0, got %#v", appearance.SourceImageIndex)
	}
	if appearance.BoundingBox.X == nil || *appearance.BoundingBox.X != 0.1 {
		t.Fatalf("expected parsed bounding box, got %#v", appearance.BoundingBox)
	}
}

func TestParseWholeSceneGeminiResponseAcceptsAlternateSourceAndBBoxFormats(t *testing.T) {
	parsed, err := parseWholeSceneGeminiResponse([]byte(`{
		"candidates": [
			{
				"candidate_key": "boxed_receiver",
				"title": "Boxed Receiver",
				"appearances": [
					{
						"candidate_key": "boxed_receiver",
						"image_asset_id": "11111111-1111-1111-1111-111111111111",
						"bbox": {"x1": 10, "y1": 20, "x2": 90, "y2": 80},
						"bbox_units": "percent"
					},
					{
						"candidate_key": "boxed_receiver",
						"source_image_id": "22222222-2222-2222-2222-222222222222",
						"bbox": [100, 200, 700, 800],
						"bbox_format": "xyxy",
						"bbox_units": "thousand"
					}
				]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("expected alternate bbox/source formats to parse: %v", err)
	}
	if len(parsed.Candidates) != 1 || len(parsed.Candidates[0].Appearances) != 2 {
		t.Fatalf("expected two parsed appearances, got %#v", parsed.Candidates)
	}
	first := parsed.Candidates[0].Appearances[0]
	if first.ImageAssetID == nil || *first.ImageAssetID != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("expected image_asset_id source reference, got %#v", first.ImageAssetID)
	}
	if first.BoundingBox.CoordinateMode != "xyxy" || first.BoundingBox.CoordinateUnit != "percent" {
		t.Fatalf("expected percent xyxy bbox, got %#v", first.BoundingBox)
	}
	second := parsed.Candidates[0].Appearances[1]
	if second.SourceImageID == nil || *second.SourceImageID != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("expected source_image_id source reference, got %#v", second.SourceImageID)
	}
	if second.BoundingBox.CoordinateMode != "xyxy" || second.BoundingBox.CoordinateUnit != "thousand" {
		t.Fatalf("expected thousand xyxy bbox, got %#v", second.BoundingBox)
	}
}

func TestParseWholeSceneGeminiResponseDefaultsArrayBoxesToYXYX(t *testing.T) {
	parsed, err := parseWholeSceneGeminiResponse([]byte(`{
		"candidates": [
			{
				"candidate_key": "radio",
				"title": "Radio",
				"appearances": [
					{
						"candidate_key": "radio",
						"source_image_index": 0,
						"bbox": [10, 20, 90, 80],
						"bbox_units": "percent"
					}
				]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("expected array bbox to parse: %v", err)
	}
	if len(parsed.Candidates) != 1 || len(parsed.Candidates[0].Appearances) != 1 {
		t.Fatalf("expected one parsed appearance, got %#v", parsed.Candidates)
	}
	appearance := parsed.Candidates[0].Appearances[0]
	if appearance.BoundingBox.CoordinateMode != "yxyx" || appearance.BoundingBox.CoordinateUnit != "percent" {
		t.Fatalf("expected percent yxyx bbox, got %#v", appearance.BoundingBox)
	}
}

func TestParseWholeSceneGeminiResponseSkipsMismatchedCropAssignments(t *testing.T) {
	parsed, err := parseWholeSceneGeminiResponse([]byte(`{
		"candidates": [
			{
				"candidate_key": "keyboard",
				"title": "Keyboard",
				"appearances": [
					{
						"candidate_key": "monitor",
						"source_image_index": 0,
						"bbox": {"x": 0.1, "y": 0.1, "width": 0.2, "height": 0.2}
					},
					{
						"target_label": "Keyboard",
						"source_image_index": 1,
						"bbox": {"x": 0.3, "y": 0.3, "width": 0.2, "height": 0.2}
					}
				]
			}
		]
	}`))
	if err != nil {
		t.Fatalf("expected candidate to parse with one valid appearance: %v", err)
	}
	if len(parsed.Candidates) != 1 {
		t.Fatalf("expected one parsed candidate, got %d", len(parsed.Candidates))
	}
	candidate := parsed.Candidates[0]
	if len(candidate.Appearances) != 1 {
		t.Fatalf("expected only matched appearance to remain, got %d", len(candidate.Appearances))
	}
	if candidate.Appearances[0].SourceImageIndex == nil || *candidate.Appearances[0].SourceImageIndex != 1 {
		t.Fatalf("expected source image index 1, got %#v", candidate.Appearances[0].SourceImageIndex)
	}
	if len(candidate.ParseWarnings) == 0 {
		t.Fatal("expected mismatch warning")
	}
}

func TestParseWholeSceneLocalizationResponseUsesOnlyHighConfidenceMatches(t *testing.T) {
	candidates := []wholeSceneParsedCandidate{
		{CandidateKey: "keyboard", Title: "Keyboard"},
		{CandidateKey: "monitor", Title: "Monitor"},
	}
	result, err := parseWholeSceneLocalizationResponse([]byte(`{
		"localizations": [
			{
				"candidate_key": "keyboard",
				"target_label": "Keyboard",
				"found": true,
				"confidence": "high",
				"source_image_index": 0,
				"box_2d": [100, 100, 300, 300],
				"box_units": "0_1000"
			},
			{
				"candidate_key": "monitor",
				"target_label": "Monitor",
				"found": true,
				"confidence": "medium",
				"source_image_index": 0,
				"box_2d": [400, 400, 600, 600],
				"box_units": "0_1000"
			},
			{
				"candidate_key": "keyboard",
				"target_label": "Monitor",
				"found": true,
				"confidence": "high",
				"source_image_index": 1,
				"box_2d": [600, 600, 800, 800],
				"box_units": "0_1000"
			}
		]
	}`), candidates)
	if err != nil {
		t.Fatalf("expected localization response to parse: %v", err)
	}
	if len(candidates[0].Appearances) != 1 {
		t.Fatalf("expected high-confidence keyboard localization, got %#v warnings=%#v", candidates[0].Appearances, result.Warnings)
	}
	if len(candidates[1].Appearances) != 0 {
		t.Fatalf("expected medium-confidence monitor localization to be skipped, got %#v", candidates[1].Appearances)
	}
	if len(result.CandidateWarnings[0]) == 0 || len(result.CandidateWarnings[1]) == 0 {
		t.Fatalf("expected candidate-level warnings for skipped localizations, got %#v", result.CandidateWarnings)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected localization skips to stay off scan-level warnings, got %#v", result.Warnings)
	}
}

func TestParseWholeSceneLocalizationResponseAcceptsShortTargetLabelWhenKeyMatches(t *testing.T) {
	candidates := []wholeSceneParsedCandidate{
		{CandidateKey: "seagate_portable_external_hard_drive", Title: "Seagate Portable External Hard Drive"},
		{CandidateKey: "apple_mac_mini", Title: "Apple Mac Mini"},
	}
	result, err := parseWholeSceneLocalizationResponse([]byte(`{
		"localizations": [
			{
				"candidate_key": "seagate_portable_external_hard_drive",
				"target_label": "Seagate External Drive",
				"found": true,
				"confidence": "high",
				"source_image_index": 0,
				"box_2d": [100, 100, 300, 300],
				"box_units": "0_1000"
			}
		]
	}`), candidates)
	if err != nil {
		t.Fatalf("expected localization response to parse: %v", err)
	}
	if len(candidates[0].Appearances) != 1 {
		t.Fatalf("expected shortened target_label to be accepted by candidate_key identity, got %#v warnings=%#v", candidates[0].Appearances, result.CandidateWarnings)
	}
	if len(result.CandidateWarnings) != 0 {
		t.Fatalf("expected no candidate warning, got %#v", result.CandidateWarnings)
	}
}

func TestParseWholeSceneLocalizationResponseRejectsAmbiguousBBoxVariants(t *testing.T) {
	candidates := []wholeSceneParsedCandidate{
		{CandidateKey: "remote_control", Title: "Remote Control"},
	}
	result, err := parseWholeSceneLocalizationResponse([]byte(`{
		"localizations": [
			{
				"candidate_key": "remote_control",
				"target_label": "Remote",
				"found": true,
				"confidence": "high",
				"source_image_index": 0,
				"boundingBox": {"yMin": 120, "xMin": 250, "yMax": 520, "xMax": 900},
				"bboxFormat": "yxyx",
				"bboxUnits": "thousand"
			}
		]
	}`), candidates)
	if err != nil {
		t.Fatalf("expected localization response to parse: %v", err)
	}
	if len(candidates[0].Appearances) != 0 {
		t.Fatalf("expected ambiguous boundingBox to be rejected for automatic crops, got %#v", candidates[0].Appearances)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Accepted || result.Diagnostics[0].BBoxFieldName != "boundingBox" {
		t.Fatalf("expected rejected boundingBox diagnostic, got %#v", result.Diagnostics)
	}
}

func TestParseWholeSceneLocalizationResponseAcceptsStrictBox2D(t *testing.T) {
	candidates := []wholeSceneParsedCandidate{
		{CandidateKey: "remote_control", Title: "Remote Control"},
	}
	result, err := parseWholeSceneLocalizationResponse([]byte(`{
		"localizations": [
			{
				"candidate_key": "remote_control",
				"target_label": "Remote",
				"found": true,
				"confidence": "high",
				"source_image_index": 0,
				"box_2d": [120, 250, 520, 900],
				"box_units": "0_1000"
			}
		]
	}`), candidates)
	if err != nil {
		t.Fatalf("expected strict localization to parse: %v", err)
	}
	if len(candidates[0].Appearances) != 1 {
		t.Fatalf("expected one accepted localization, got %#v warnings=%#v", candidates[0].Appearances, result.CandidateWarnings)
	}
	appearance := candidates[0].Appearances[0]
	if appearance.SourceImageIndex == nil || *appearance.SourceImageIndex != 0 {
		t.Fatalf("expected source image index 0 to be accepted, got %#v", appearance.SourceImageIndex)
	}
	if appearance.BoundingBox.CoordinateMode != "yxyx" || appearance.BoundingBox.CoordinateUnit != "thousand" {
		t.Fatalf("expected yxyx thousand bbox, got %#v", appearance.BoundingBox)
	}
	rect, err := wholeScenePixelCropRect(image.Rect(0, 0, 640, 480), appearance.BoundingBox)
	if err != nil {
		t.Fatalf("expected provider variant bbox to produce crop: %v", err)
	}
	if rect.Empty() {
		t.Fatalf("expected non-empty crop rect, got %#v", rect)
	}
	if len(result.Warnings) != 0 || len(result.CandidateWarnings) != 0 {
		t.Fatalf("expected no warnings for accepted localization, got warnings=%#v candidate_warnings=%#v", result.Warnings, result.CandidateWarnings)
	}
	if len(result.Diagnostics) != 1 || !result.Diagnostics[0].Accepted || result.Diagnostics[0].BBoxFieldName != "box_2d" {
		t.Fatalf("expected accepted strict diagnostic, got %#v", result.Diagnostics)
	}
}

func TestParseWholeSceneLocalizationResponseRejectsUnsafeStrictLocalizations(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "candidate key mismatch",
			body: `{"localizations":[{"candidate_key":"wrong_remote","target_label":"Remote","found":true,"confidence":"high","source_image_index":0,"box_2d":[100,100,300,300],"box_units":"0_1000"}]}`,
		},
		{
			name: "medium confidence",
			body: `{"localizations":[{"candidate_key":"remote_control","target_label":"Remote","found":true,"confidence":"medium","source_image_index":0,"box_2d":[100,100,300,300],"box_units":"0_1000"}]}`,
		},
		{
			name: "collapsed box",
			body: `{"localizations":[{"candidate_key":"remote_control","target_label":"Remote","found":true,"confidence":"high","source_image_index":0,"box_2d":[100,100,100,300],"box_units":"0_1000"}]}`,
		},
		{
			name: "unlabeled array bbox",
			body: `{"localizations":[{"candidate_key":"remote_control","target_label":"Remote","found":true,"confidence":"high","source_image_index":0,"bbox":[100,100,300,300],"box_units":"0_1000"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates := []wholeSceneParsedCandidate{{CandidateKey: "remote_control", Title: "Remote Control"}}
			result, err := parseWholeSceneLocalizationResponse([]byte(tt.body), candidates)
			if err != nil {
				t.Fatalf("expected localization response to parse: %v", err)
			}
			if len(candidates[0].Appearances) != 0 {
				t.Fatalf("expected no automatic crop-bearing appearance, got %#v", candidates[0].Appearances)
			}
			if len(result.Diagnostics) != 1 || result.Diagnostics[0].Accepted {
				t.Fatalf("expected one rejected diagnostic, got %#v", result.Diagnostics)
			}
			if len(result.Warnings) != 0 {
				t.Fatalf("expected rejected localization to stay off scan warnings, got %#v", result.Warnings)
			}
		})
	}
}

func TestParseWholeSceneGeminiResponseFailsWithoutUsableCandidates(t *testing.T) {
	parsed, err := parseWholeSceneGeminiResponse([]byte(`{"candidates":[{"description":"   "}]}`))
	if err == nil {
		t.Fatal("expected no usable candidates error")
	}
	if len(parsed.Candidates) != 0 {
		t.Fatalf("expected no parsed candidates, got %d", len(parsed.Candidates))
	}
	if len(parsed.Warnings) == 0 {
		t.Fatal("expected parse warning for malformed candidate")
	}
}

func TestWholeScenePixelCropRectUsesNormalizedBounds(t *testing.T) {
	x := 0.25
	y := 0.5
	width := 0.5
	height := 0.25
	rect, err := wholeScenePixelCropRect(image.Rect(0, 0, 200, 100), wholeSceneParsedBoundingBox{
		X:      &x,
		Y:      &y,
		Width:  &width,
		Height: &height,
	})
	if err != nil {
		t.Fatalf("expected valid crop rect: %v", err)
	}
	if rect.Min.X != 50 || rect.Min.Y != 50 || rect.Dx() != 100 || rect.Dy() != 25 {
		t.Fatalf("unexpected crop rect: %#v", rect)
	}

	tooWide := 0.9
	rect, err = wholeScenePixelCropRect(image.Rect(0, 0, 200, 100), wholeSceneParsedBoundingBox{
		X:      &x,
		Y:      &y,
		Width:  &tooWide,
		Height: &height,
	})
	if err != nil {
		t.Fatalf("expected out-of-range bbox to clamp: %v", err)
	}
	if rect.Min.X != 50 || rect.Min.Y != 50 || rect.Dx() != 150 || rect.Dy() != 25 {
		t.Fatalf("unexpected clamped crop rect: %#v", rect)
	}
}

func TestWholeScenePixelCropRectAcceptsPercentThousandAndPixelCoordinates(t *testing.T) {
	bounds := image.Rect(0, 0, 200, 100)

	percent := newWholeSceneParsedBoundingBox(10, 20, 60, 80, "xyxy", "percent")
	rect, err := wholeScenePixelCropRect(bounds, percent)
	if err != nil {
		t.Fatalf("expected percent crop: %v", err)
	}
	if rect.Min.X != 20 || rect.Min.Y != 20 || rect.Dx() != 100 || rect.Dy() != 60 {
		t.Fatalf("unexpected percent crop rect: %#v", rect)
	}

	thousand := newWholeSceneParsedBoundingBox(100, 200, 500, 250, "xywh", "thousand")
	rect, err = wholeScenePixelCropRect(bounds, thousand)
	if err != nil {
		t.Fatalf("expected thousand crop: %v", err)
	}
	if rect.Min.X != 20 || rect.Min.Y != 20 || rect.Dx() != 100 || rect.Dy() != 25 {
		t.Fatalf("unexpected thousand crop rect: %#v", rect)
	}

	pixels := newWholeSceneParsedBoundingBox(10, 20, 110, 80, "xyxy", "pixel")
	rect, err = wholeScenePixelCropRect(bounds, pixels)
	if err != nil {
		t.Fatalf("expected pixel crop: %v", err)
	}
	if rect.Min.X != 10 || rect.Min.Y != 20 || rect.Dx() != 100 || rect.Dy() != 60 {
		t.Fatalf("unexpected pixel crop rect: %#v", rect)
	}

	yxyx := newWholeSceneParsedBoundingBox(10, 20, 90, 80, "yxyx", "percent")
	rect, err = wholeScenePixelCropRect(bounds, yxyx)
	if err != nil {
		t.Fatalf("expected yxyx crop: %v", err)
	}
	if rect.Min.X != 40 || rect.Min.Y != 10 || rect.Dx() != 120 || rect.Dy() != 80 {
		t.Fatalf("unexpected yxyx crop rect: %#v", rect)
	}
}

func TestWholeScenePixelCropRectInfersGeminiYXYXThousandBeforePixels(t *testing.T) {
	bounds := image.Rect(0, 0, 640, 480)
	geminiBox := newWholeSceneParsedBoundingBox(120, 250, 520, 900, "yxyx", "")

	rect, err := wholeScenePixelCropRect(bounds, geminiBox)
	if err != nil {
		t.Fatalf("expected unlabeled Gemini yxyx thousand crop: %v", err)
	}
	if rect.Min.X != 160 || rect.Min.Y != 57 || rect.Dx() != 416 || rect.Dy() != 193 {
		t.Fatalf("unexpected Gemini yxyx thousand crop rect: %#v", rect)
	}
}

func TestWholeScenePixelCropRectRejectsCollapsedBoxes(t *testing.T) {
	bounds := image.Rect(0, 0, 640, 480)
	collapsed := newWholeSceneParsedBoundingBox(500, 250, 500, 900, "yxyx", "thousand")

	if _, err := wholeScenePixelCropRect(bounds, collapsed); err == nil {
		t.Fatal("expected collapsed bbox to fail")
	}
}

func TestWholeSceneResolveAppearanceSupportsOneBasedSourceImageIndexes(t *testing.T) {
	sources := wholeSceneScanImageSources{
		byIndex: map[int]wholeSceneScanImageSource{
			0: {ID: "scan-0", ImageAssetID: "asset-0", SortOrder: 0},
			1: {ID: "scan-1", ImageAssetID: "asset-1", SortOrder: 1},
		},
		byScanImageID: map[string]wholeSceneScanImageSource{
			"scan-0": {ID: "scan-0", ImageAssetID: "asset-0", SortOrder: 0},
			"scan-1": {ID: "scan-1", ImageAssetID: "asset-1", SortOrder: 1},
		},
		byImageAssetID: map[string]wholeSceneScanImageSource{
			"asset-0": {ID: "scan-0", ImageAssetID: "asset-0", SortOrder: 0},
			"asset-1": {ID: "scan-1", ImageAssetID: "asset-1", SortOrder: 1},
		},
	}
	index := 2
	source, warning, ok := sources.resolve(wholeSceneParsedAppearance{SourceImageIndex: &index})
	if !ok {
		t.Fatal("expected one-based source image index fallback to resolve")
	}
	if source.SortOrder != 1 {
		t.Fatalf("expected one-based fallback to sort order 1, got %#v", source)
	}
	if !strings.Contains(warning, "1-based") {
		t.Fatalf("expected one-based warning, got %q", warning)
	}
}

func TestWholeSceneCreateScanIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	intakeDir := t.TempDir()
	store := NewWholeSceneStore(pool, intakeDir, 1)
	handler := NewWholeSceneHandler(store)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	metadata := map[string]any{
		"hint": "messy workbench with adapters",
		"intake_context": map[string]any{
			"no_container": true,
		},
		"files": []map[string]any{
			{
				"client_file_id":    "scene_1",
				"original_filename": "scene-1.jpg",
				"mime_type":         "image/jpeg",
				"size_bytes":        4,
			},
			{
				"client_file_id":    "scene_2",
				"original_filename": "scene-2.png",
				"mime_type":         "image/png",
				"size_bytes":        4,
			},
		},
	}
	metadataBytes, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	if err := writer.WriteField(metadataFieldName, string(metadataBytes)); err != nil {
		t.Fatalf("write metadata: %v", err)
	}
	for _, clientID := range []string{"scene_1", "scene_2"} {
		part, err := writer.CreateFormFile(multipartFileFieldName(clientID), clientID+".jpg")
		if err != nil {
			t.Fatalf("create file part: %v", err)
		}
		if _, err := part.Write([]byte{0xff, 0xd8, 0xff, 0xd9}); err != nil {
			t.Fatalf("write file part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/whole-scene/scans", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()

	handler.CreateScan(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d body=%s", http.StatusCreated, rec.Code, rec.Body.String())
	}

	var response models.GetWholeSceneScanResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if response.Scan.ID == "" {
		t.Fatal("expected scan id")
	}
	if response.Scan.UploadSessionID == "" {
		t.Fatal("expected upload session id")
	}
	if response.Scan.Hint == nil || *response.Scan.Hint != "messy workbench with adapters" {
		t.Fatalf("expected persisted hint, got %#v", response.Scan.Hint)
	}
	if response.Scan.Status != "created" {
		t.Fatalf("expected scan status created, got %q", response.Scan.Status)
	}
	if len(response.Scan.Images) != 2 {
		t.Fatalf("expected 2 source images, got %d", len(response.Scan.Images))
	}
	if len(response.Scan.AnalysisRuns) != 0 {
		t.Fatalf("expected no analysis runs, got %d", len(response.Scan.AnalysisRuns))
	}
	if len(response.Scan.Candidates) != 0 {
		t.Fatalf("expected no candidates, got %d", len(response.Scan.Candidates))
	}

	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM whole_scene_scans WHERE id = $1::uuid`, response.Scan.ID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM image_assets WHERE session_id = $1::uuid`, response.Scan.UploadSessionID)
		_, _ = pool.Exec(context.Background(), `DELETE FROM upload_sessions WHERE id = $1::uuid`, response.Scan.UploadSessionID)
	}()

	var uploadSource string
	var sessionStatus string
	if err := pool.QueryRow(ctx, `
		SELECT source, status
		FROM upload_sessions
		WHERE id = $1::uuid
	`, response.Scan.UploadSessionID).Scan(&uploadSource, &sessionStatus); err != nil {
		t.Fatalf("load upload session: %v", err)
	}
	if uploadSource != wholeSceneUploadSource {
		t.Fatalf("expected upload source %q, got %q", wholeSceneUploadSource, uploadSource)
	}
	if sessionStatus != "processing" {
		t.Fatalf("expected upload session status processing, got %q", sessionStatus)
	}

	var imageCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM image_assets
		WHERE session_id = $1::uuid
			AND upload_group_id IS NULL
			AND item_id IS NULL
			AND status = 'pending'
	`, response.Scan.UploadSessionID).Scan(&imageCount); err != nil {
		t.Fatalf("count image assets: %v", err)
	}
	if imageCount != 2 {
		t.Fatalf("expected 2 unattached pending image assets, got %d", imageCount)
	}

	var associationCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM whole_scene_scan_images
		WHERE scan_id = $1::uuid
	`, response.Scan.ID).Scan(&associationCount); err != nil {
		t.Fatalf("count scan image associations: %v", err)
	}
	if associationCount != 2 {
		t.Fatalf("expected 2 scan image associations, got %d", associationCount)
	}
}

func TestWholeSceneAnalysisQueueAndWorkerIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `UPDATE ai_provider_configs SET active = false`); err != nil {
		t.Fatalf("clear active providers: %v", err)
	}

	intakeDir := t.TempDir()
	imagePath := filepath.Join(intakeDir, "scene-processed.jpg")
	if err := writeWholeSceneTestJPEG(imagePath, 40, 30); err != nil {
		t.Fatalf("write processed image: %v", err)
	}
	cropOriginalsDir := t.TempDir()
	cropThumbnailsDir := t.TempDir()
	cropNormalizedDir := t.TempDir()
	diagnosticsDir := t.TempDir()

	store := NewWholeSceneStore(pool, intakeDir, 1)
	scanID, sessionID, imageID := insertWholeSceneAnalysisFixture(t, ctx, pool, imagePath, "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)
	manualTitle := "Manual Missed Cable"
	if _, err := store.AddManualCandidate(ctx, scanID, models.AddWholeSceneCandidateRequest{Title: &manualTitle}); err != nil {
		t.Fatalf("add manual candidate before analysis: %v", err)
	}

	if _, _, err := store.QueueAnalysis(ctx, scanID); !errorsIs(err, errWholeSceneNoProvider) {
		t.Fatalf("expected missing provider error, got %v", err)
	}

	openAIKey := "test-key"
	var providerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO ai_provider_configs (
			name,
			provider_type,
			enabled,
			active,
			api_key_value,
			model_name,
			vision_enabled
		)
		VALUES ('Whole Scene unsupported test', 'openai', true, true, $1, 'gpt-test', true)
		RETURNING id::text
	`, openAIKey).Scan(&providerID); err != nil {
		t.Fatalf("insert unsupported provider: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ai_provider_configs WHERE id = $1::uuid`, providerID)
	}()
	if _, _, err := store.QueueAnalysis(ctx, scanID); err == nil || !strings.Contains(err.Error(), "Gemini") {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}

	requestSeen := make(chan []byte, 2)
	var geminiRequestCount int32
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.RawQuery, "key=test-key") {
			t.Errorf("expected Gemini API key in query, got %q", r.URL.RawQuery)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read Gemini request body: %v", err)
		}
		requestSeen <- body
		w.Header().Set("Content-Type", "application/json")
		requestNumber := atomic.AddInt32(&geminiRequestCount, 1)
		responseText := `{"candidates":[{"candidate_key":"vintage_keyboard","title":"Vintage Keyboard","description":"IBM-style keyboard","approx_value":42.5,"confidence":"high"}]}`
		if requestNumber == 2 {
			responseText = `{"localizations":[{"candidate_key":"vintage_keyboard","target_label":"Vintage Keyboard","found":true,"confidence":"high","source_image_index":0,"box_2d":[100,100,800,800],"box_units":"0_1000"}]}`
		}
		responseBody, err := json.Marshal(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": responseText},
						},
					},
				},
			},
		})
		if err != nil {
			t.Errorf("marshal Gemini response: %v", err)
		}
		_, _ = w.Write(responseBody)
	}))
	defer geminiServer.Close()

	if _, err := pool.Exec(ctx, `
		UPDATE ai_provider_configs
		SET provider_type = 'gemini',
			base_url = $2,
			api_key_value = $3,
			model_name = 'gemini-test',
			vision_enabled = true,
			enabled = true,
			active = true
		WHERE id = $1::uuid
	`, providerID, geminiServer.URL, "test-key"); err != nil {
		t.Fatalf("activate Gemini provider: %v", err)
	}

	runID, queued, err := store.QueueAnalysis(ctx, scanID)
	if err != nil {
		t.Fatalf("queue analysis: %v", err)
	}
	if !queued {
		t.Fatal("expected first queue request to create a run")
	}

	sameRunID, queuedAgain, err := store.QueueAnalysis(ctx, scanID)
	if err != nil {
		t.Fatalf("queue analysis second time: %v", err)
	}
	if queuedAgain {
		t.Fatal("expected second queue request to be idempotent")
	}
	if sameRunID != runID {
		t.Fatalf("expected idempotent queue to return run %s, got %s", runID, sameRunID)
	}

	var runCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM whole_scene_analysis_runs
		WHERE scan_id = $1::uuid
	`, scanID).Scan(&runCount); err != nil {
		t.Fatalf("count analysis runs: %v", err)
	}
	if runCount != 1 {
		t.Fatalf("expected one analysis run after idempotent queue, got %d", runCount)
	}

	worker := NewWholeSceneAnalysisWorker(pool, WholeSceneAnalysisWorkerConfig{
		ScanInterval:   time.Hour,
		MaxImages:      6,
		MaxImageBytes:  1024 * 1024,
		SafeRoots:      []string{intakeDir},
		OriginalsDir:   cropOriginalsDir,
		ThumbnailsDir:  cropThumbnailsDir,
		NormalizedDir:  cropNormalizedDir,
		DiagnosticsDir: diagnosticsDir,
	})
	if err := worker.scanOnce(ctx); err != nil {
		t.Fatalf("worker scan once: %v", err)
	}

	for i := 0; i < 2; i++ {
		select {
		case body := <-requestSeen:
			if !bytes.Contains(body, []byte("inline_data")) {
				t.Fatalf("expected Gemini request to include inline image data, got %s", string(body))
			}
		case <-time.After(2 * time.Second):
			t.Fatal("expected Gemini request")
		}
	}
	if atomic.LoadInt32(&geminiRequestCount) != 2 {
		t.Fatalf("expected two Gemini requests, got %d", atomic.LoadInt32(&geminiRequestCount))
	}
	select {
	case body := <-requestSeen:
		if !bytes.Contains(body, []byte("inline_data")) {
			t.Fatalf("expected Gemini request to include inline image data, got %s", string(body))
		}
		t.Fatalf("unexpected extra Gemini request: %s", string(body))
	default:
	}

	var runStatus string
	var scanStatus string
	var rawResponse string
	var requestPayload []byte
	if err := pool.QueryRow(ctx, `
		SELECT
			wsar.status,
			wss.status,
			wsar.raw_response_text,
			wsar.request_payload
		FROM whole_scene_analysis_runs wsar
		JOIN whole_scene_scans wss ON wss.id = wsar.scan_id
		WHERE wsar.id = $1::uuid
	`, runID).Scan(&runStatus, &scanStatus, &rawResponse, &requestPayload); err != nil {
		t.Fatalf("load completed run: %v", err)
	}
	if runStatus != "succeeded" || scanStatus != "succeeded" {
		t.Fatalf("expected succeeded statuses, got run=%q scan=%q", runStatus, scanStatus)
	}
	if !strings.Contains(rawResponse, `"candidates"`) {
		t.Fatalf("expected raw Gemini response to be preserved, got %q", rawResponse)
	}
	if !bytes.Contains(requestPayload, []byte(imageID)) {
		t.Fatalf("expected request payload to preserve source image id, got %s", string(requestPayload))
	}
	if !bytes.Contains(requestPayload, []byte("response_schema_version")) {
		t.Fatalf("expected request payload manifest to preserve response schema version, got %s", string(requestPayload))
	}
	if !bytes.Contains(requestPayload, []byte("file_hash")) {
		t.Fatalf("expected request payload manifest to preserve image hash metadata, got %s", string(requestPayload))
	}
	if bytes.Contains(requestPayload, []byte("gemini_request")) || bytes.Contains(requestPayload, []byte("inline_data")) || bytes.Contains(requestPayload, []byte("/9j/2Q==")) {
		t.Fatalf("expected request payload manifest without inline Gemini image body, got %s", string(requestPayload))
	}
	completedScan, err := store.GetScan(ctx, scanID)
	if err != nil {
		t.Fatalf("load completed scan: %v", err)
	}
	if len(completedScan.Candidates) != 2 {
		t.Fatalf("expected persisted AI candidate plus preserved manual candidate, got %d", len(completedScan.Candidates))
	}
	var foundAI bool
	var foundManual bool
	var aiCandidateID string
	var cropImageAssetID string
	for _, candidate := range completedScan.Candidates {
		if candidate.Source == "ai" && candidate.Title != nil && *candidate.Title == "Vintage Keyboard" {
			foundAI = true
			aiCandidateID = candidate.ID
			if len(candidate.Appearances) != 1 {
				t.Fatalf("expected one persisted AI appearance, got %d", len(candidate.Appearances))
			}
			if len(candidate.Crops) != 1 {
				t.Fatalf("expected one generated AI crop, got %d", len(candidate.Crops))
			}
			crop := candidate.Crops[0]
			if crop.Status != "generated" || !crop.IsPreferred || crop.CropImage == nil || crop.CropImage.Status != "processed" {
				t.Fatalf("expected generated preferred processed crop, got %#v", crop)
			}
			if crop.CropImage.StoredFilename == nil {
				t.Fatalf("expected crop stored filename, got %#v", crop.CropImage)
			}
			if crop.CropImageAssetID == nil {
				t.Fatalf("expected crop image asset id, got %#v", crop)
			}
			cropImageAssetID = *crop.CropImageAssetID
			if _, err := os.Stat(filepath.Join(cropOriginalsDir, *crop.CropImage.StoredFilename)); err != nil {
				t.Fatalf("expected crop original file: %v", err)
			}
		}
		if candidate.Source == "manual" && candidate.Title != nil && *candidate.Title == manualTitle {
			foundManual = true
		}
	}
	if !foundAI || !foundManual {
		t.Fatalf("expected AI and manual candidates to be present, got %#v", completedScan.Candidates)
	}
	if aiCandidateID == "" || cropImageAssetID == "" {
		t.Fatalf("expected AI candidate and crop image ids, got candidate=%q crop=%q", aiCandidateID, cropImageAssetID)
	}
	diagnosticsJSONPath := filepath.Join(diagnosticsDir, scanID, "localization-decisions.json")
	diagnosticsJSON, err := os.ReadFile(diagnosticsJSONPath)
	if err != nil {
		t.Fatalf("expected localization diagnostics JSON: %v", err)
	}
	for _, expected := range []string{`"accepted": true`, `"crop_image_asset_id": "` + cropImageAssetID + `"`, `"bbox_field_name": "box_2d"`, `"final_pixel_rect"`} {
		if !bytes.Contains(diagnosticsJSON, []byte(expected)) {
			t.Fatalf("expected diagnostics JSON to contain %s, got %s", expected, string(diagnosticsJSON))
		}
	}
	if _, err := os.Stat(filepath.Join(diagnosticsDir, scanID, "source-0-overlay.jpg")); err != nil {
		t.Fatalf("expected localization overlay image: %v", err)
	}

	wholeSceneHandler := NewWholeSceneHandler(store)
	approvalRouter := chi.NewRouter()
	approvalRouter.Post("/whole-scene/scans/{id}/candidates/{candidateID}/approve", wholeSceneHandler.ApproveCandidate)
	approveReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+aiCandidateID+"/approve", nil)
	approveRec := httptest.NewRecorder()
	approvalRouter.ServeHTTP(approveRec, approveReq)
	if approveRec.Code != http.StatusOK {
		t.Fatalf("expected approve status %d, got %d body=%s", http.StatusOK, approveRec.Code, approveRec.Body.String())
	}

	var approveResponse models.WholeSceneCandidateMutationResponse
	if err := json.Unmarshal(approveRec.Body.Bytes(), &approveResponse); err != nil {
		t.Fatalf("unmarshal approve response: %v", err)
	}
	if approveResponse.Scan == nil {
		t.Fatalf("expected approve response to include scan, got %#v", approveResponse)
	}
	var approvedItemID string
	for _, candidate := range approveResponse.Scan.Candidates {
		if candidate.ID == aiCandidateID {
			if candidate.Status != "approved" || candidate.ApprovedItemID == nil || candidate.ApprovedItem == nil {
				t.Fatalf("expected approved candidate with item reference, got %#v", candidate)
			}
			approvedItemID = *candidate.ApprovedItemID
		}
	}
	if approvedItemID == "" {
		t.Fatal("expected approved item id")
	}

	var approvedTitle *string
	var approvedApproxValue *string
	var approvedAIEnriched bool
	var approvedCurrentInventory bool
	var approvedDisposition string
	var approvedImageCount int
	if err := pool.QueryRow(ctx, `
		SELECT
			i.title,
			i.approx_value::text,
			i.ai_enriched,
			i.current_inventory,
			i.disposition_code,
			(
				SELECT count(*)::int
				FROM image_assets ia
				WHERE ia.item_id = i.id
			)
		FROM items i
		WHERE i.id = $1::uuid
	`, approvedItemID).Scan(
		&approvedTitle,
		&approvedApproxValue,
		&approvedAIEnriched,
		&approvedCurrentInventory,
		&approvedDisposition,
		&approvedImageCount,
	); err != nil {
		t.Fatalf("load approved item: %v", err)
	}
	if approvedTitle == nil || *approvedTitle != "Vintage Keyboard" {
		t.Fatalf("expected approved item title, got %#v", approvedTitle)
	}
	if approvedApproxValue == nil || *approvedApproxValue != "42.50" {
		t.Fatalf("expected approved item approx value 42.50, got %#v", approvedApproxValue)
	}
	if !approvedAIEnriched || !approvedCurrentInventory || approvedDisposition != "stored" {
		t.Fatalf("unexpected approved item flags ai=%t current=%t disposition=%q", approvedAIEnriched, approvedCurrentInventory, approvedDisposition)
	}
	if approvedImageCount != 1 {
		t.Fatalf("expected only selected crop attached to item, got %d images", approvedImageCount)
	}

	var attachedCropItemID *string
	if err := pool.QueryRow(ctx, `
		SELECT item_id::text
		FROM image_assets
		WHERE id = $1::uuid
	`, cropImageAssetID).Scan(&attachedCropItemID); err != nil {
		t.Fatalf("load crop image item link: %v", err)
	}
	if attachedCropItemID == nil || *attachedCropItemID != approvedItemID {
		t.Fatalf("expected crop image attached to approved item %s, got %#v", approvedItemID, attachedCropItemID)
	}
	var sourceImageItemID *string
	if err := pool.QueryRow(ctx, `
		SELECT item_id::text
		FROM image_assets
		WHERE id = $1::uuid
	`, imageID).Scan(&sourceImageItemID); err != nil {
		t.Fatalf("load source image item link: %v", err)
	}
	if sourceImageItemID != nil {
		t.Fatalf("expected source scene image to remain unattached, got item_id=%s", *sourceImageItemID)
	}

	secondApproveReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+aiCandidateID+"/approve", nil)
	secondApproveRec := httptest.NewRecorder()
	approvalRouter.ServeHTTP(secondApproveRec, secondApproveReq)
	if secondApproveRec.Code != http.StatusOK {
		t.Fatalf("expected idempotent approve status %d, got %d body=%s", http.StatusOK, secondApproveRec.Code, secondApproveRec.Body.String())
	}
	var secondApproveResponse models.WholeSceneCandidateMutationResponse
	if err := json.Unmarshal(secondApproveRec.Body.Bytes(), &secondApproveResponse); err != nil {
		t.Fatalf("unmarshal second approve response: %v", err)
	}
	if secondApproveResponse.Scan == nil {
		t.Fatalf("expected second approve response to include scan, got %#v", secondApproveResponse)
	}
	for _, candidate := range secondApproveResponse.Scan.Candidates {
		if candidate.ID == aiCandidateID {
			if candidate.ApprovedItemID == nil || *candidate.ApprovedItemID != approvedItemID {
				t.Fatalf("expected repeated approval to return existing item %s, got %#v", approvedItemID, candidate.ApprovedItemID)
			}
		}
	}

	retryRunID, retryQueued, err := store.QueueAnalysis(ctx, scanID)
	if err != nil {
		t.Fatalf("queue retry: %v", err)
	}
	if !retryQueued {
		t.Fatal("expected terminal run retry to create a new run")
	}
	var retryRunNumber int
	if err := pool.QueryRow(ctx, `
		SELECT run_number
		FROM whole_scene_analysis_runs
		WHERE id = $1::uuid
	`, retryRunID).Scan(&retryRunNumber); err != nil {
		t.Fatalf("load retry run number: %v", err)
	}
	if retryRunNumber != 2 {
		t.Fatalf("expected retry run number 2, got %d", retryRunNumber)
	}

	if _, err := pool.Exec(ctx, `
		UPDATE whole_scene_analysis_runs
		SET status = 'processing',
			started_datetime = now(),
			completed_datetime = NULL
		WHERE id = $1::uuid
	`, retryRunID); err != nil {
		t.Fatalf("mark retry processing: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE whole_scene_scans SET status = 'processing' WHERE id = $1::uuid`, scanID); err != nil {
		t.Fatalf("mark scan processing: %v", err)
	}
	if err := worker.resetStuckProcessing(ctx); err != nil {
		t.Fatalf("reset stuck processing: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT wsar.status, wss.status, wsar.error_message
		FROM whole_scene_analysis_runs wsar
		JOIN whole_scene_scans wss ON wss.id = wsar.scan_id
		WHERE wsar.id = $1::uuid
	`, retryRunID).Scan(&runStatus, &scanStatus, &rawResponse); err != nil {
		t.Fatalf("load recovered run: %v", err)
	}
	if runStatus != "queued" || scanStatus != "queued" {
		t.Fatalf("expected recovered queued statuses, got run=%q scan=%q", runStatus, scanStatus)
	}
	if !strings.Contains(rawResponse, "requeued after API restart") {
		t.Fatalf("expected recovery message, got %q", rawResponse)
	}
}

func TestWholeSceneReviewScanListIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	store := NewWholeSceneStore(pool, t.TempDir(), 1)
	scanID, sessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, "/tmp/review-list-scene.jpg", "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_analysis_runs (
			scan_id,
			run_number,
			status,
			provider_type,
			model_name,
			prompt_version,
			queued_datetime,
			completed_datetime,
			raw_response_text
		)
		VALUES ($1::uuid, 1, 'succeeded', 'gemini', 'test-model', 'test-prompt', now(), now(), '{"candidates":[]}')
	`, scanID); err != nil {
		t.Fatalf("insert analysis run: %v", err)
	}

	for _, row := range []struct {
		status string
		title  string
	}{
		{status: "proposed", title: "Pending A"},
		{status: "edited", title: "Pending B"},
		{status: "approved", title: "Approved"},
		{status: "rejected", title: "Rejected"},
	} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO whole_scene_candidates (scan_id, source, status, title)
			VALUES ($1::uuid, 'ai', $2, $3)
		`, scanID, row.status, row.title); err != nil {
			t.Fatalf("insert %s candidate: %v", row.status, err)
		}
	}

	emptyScanID, emptySessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, "/tmp/review-list-empty.jpg", "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, emptyScanID, emptySessionID)

	scans, err := store.ListReviewScans(ctx, nil)
	if err != nil {
		t.Fatalf("list review scans: %v", err)
	}

	var found *models.WholeSceneReviewScanSummary
	for index := range scans {
		if scans[index].ID == scanID {
			found = &scans[index]
		}
		if scans[index].ID == emptyScanID {
			t.Fatalf("empty scan without run or candidates should not be listed")
		}
	}
	if found == nil {
		t.Fatal("expected review scan in list")
	}
	if found.CandidateCounts.Pending != 2 {
		t.Fatalf("expected 2 pending candidates, got %d", found.CandidateCounts.Pending)
	}
	if found.CandidateCounts.Approved != 1 {
		t.Fatalf("expected 1 approved candidate, got %d", found.CandidateCounts.Approved)
	}
	if found.CandidateCounts.Rejected != 1 {
		t.Fatalf("expected 1 rejected candidate, got %d", found.CandidateCounts.Rejected)
	}
	if found.CandidateCounts.Total != 4 {
		t.Fatalf("expected 4 total candidates, got %d", found.CandidateCounts.Total)
	}
	if found.LatestAnalysisRun == nil || found.LatestAnalysisRun.Status != "succeeded" {
		t.Fatalf("expected latest succeeded analysis run, got %#v", found.LatestAnalysisRun)
	}
}

func TestWholeSceneAnalysisQueueRejectsImageStatesIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `UPDATE ai_provider_configs SET active = false`); err != nil {
		t.Fatalf("clear active providers: %v", err)
	}
	key := "test-key"
	var providerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO ai_provider_configs (
			name,
			provider_type,
			enabled,
			active,
			api_key_value,
			model_name,
			vision_enabled
		)
		VALUES ('Whole Scene queue state test', 'gemini', true, true, $1, 'gemini-test', true)
		RETURNING id::text
	`, key).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ai_provider_configs WHERE id = $1::uuid`, providerID)
	}()

	store := NewWholeSceneStore(pool, t.TempDir(), 1)
	pendingScanID, pendingSessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, "/tmp/pending.jpg", "pending")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, pendingScanID, pendingSessionID)
	if _, _, err := store.QueueAnalysis(ctx, pendingScanID); !errorsIs(err, errWholeSceneImagesInFlight) {
		t.Fatalf("expected images in flight error, got %v", err)
	}

	failedScanID, failedSessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, "/tmp/failed.jpg", "failed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, failedScanID, failedSessionID)
	if _, _, err := store.QueueAnalysis(ctx, failedScanID); !errorsIs(err, errWholeSceneNoUsableImages) {
		t.Fatalf("expected no usable images error, got %v", err)
	}
}

func TestWholeSceneCandidateAIAssistQueueAndWorkerIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	if _, err := pool.Exec(ctx, `UPDATE ai_provider_configs SET active = false`); err != nil {
		t.Fatalf("clear active providers: %v", err)
	}

	requestCount := 0
	geminiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		if requestCount == 2 {
			http.Error(w, "candidate-specific provider failure", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		responseBody, err := json.Marshal(map[string]any{
			"candidates": []map[string]any{
				{
					"content": map[string]any{
						"parts": []map[string]any{
							{"text": `{"title":"AI Assisted Candidate One","description":"Updated candidate description","approx_value":31.5}`},
						},
					},
				},
			},
		})
		if err != nil {
			t.Errorf("marshal Gemini response: %v", err)
		}
		_, _ = w.Write(responseBody)
	}))
	defer geminiServer.Close()

	key := "test-key"
	var providerID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO ai_provider_configs (
			name,
			provider_type,
			enabled,
			active,
			base_url,
			api_key_value,
			model_name,
			vision_enabled,
			timeout_seconds
		)
		VALUES ('Whole Scene candidate assist test', 'gemini', true, true, $1, $2, 'gemini-test', true, 5)
		RETURNING id::text
	`, geminiServer.URL, key).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	defer func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ai_provider_configs WHERE id = $1::uuid`, providerID)
	}()

	tempDir := t.TempDir()
	imagePath := filepath.Join(tempDir, "candidate-source.jpg")
	if err := writeWholeSceneTestJPEG(imagePath, 40, 30); err != nil {
		t.Fatalf("write candidate source: %v", err)
	}

	store := NewWholeSceneStore(pool, tempDir, 1, NewManagedFileService([]string{tempDir}))
	scanID, sessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, imagePath, "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	var scanImageID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text
		FROM whole_scene_scan_images
		WHERE scan_id = $1::uuid
		LIMIT 1
	`, scanID).Scan(&scanImageID); err != nil {
		t.Fatalf("load scan image id: %v", err)
	}

	candidateIDs := make([]string, 0, 2)
	for _, title := range []string{"Candidate One", "Candidate Two"} {
		var candidateID string
		if err := pool.QueryRow(ctx, `
			INSERT INTO whole_scene_candidates (scan_id, source, status, title)
			VALUES ($1::uuid, 'manual', 'proposed', $2)
			RETURNING id::text
		`, scanID, title).Scan(&candidateID); err != nil {
			t.Fatalf("insert candidate %s: %v", title, err)
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO whole_scene_candidate_appearances (candidate_id, scan_image_id, source_image_index)
			VALUES ($1::uuid, $2::uuid, 0)
		`, candidateID, scanImageID); err != nil {
			t.Fatalf("insert candidate appearance: %v", err)
		}
		candidateIDs = append(candidateIDs, candidateID)
	}

	for _, candidateID := range candidateIDs {
		result, err := store.QueueCandidateAIAssist(ctx, scanID, candidateID, models.AssistWholeSceneCandidateRequest{})
		if err != nil {
			t.Fatalf("queue candidate %s: %v", candidateID, err)
		}
		if result.Scan == nil {
			t.Fatalf("expected queue response scan for candidate %s", candidateID)
		}
	}

	var queuedCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM whole_scene_candidates
		WHERE scan_id = $1::uuid
			AND ai_assist_status = 'queued'
	`, scanID).Scan(&queuedCount); err != nil {
		t.Fatalf("count queued candidates: %v", err)
	}
	if queuedCount != 2 {
		t.Fatalf("expected two queued candidate jobs, got %d", queuedCount)
	}

	worker := NewWholeSceneAnalysisWorker(pool, WholeSceneAnalysisWorkerConfig{
		ScanInterval:  time.Hour,
		MaxImages:     6,
		MaxImageBytes: 1024 * 1024,
		SafeRoots:     []string{tempDir},
	})
	if err := worker.scanOnce(ctx); err != nil {
		t.Fatalf("worker scan first candidate: %v", err)
	}
	if err := worker.scanOnce(ctx); err != nil {
		t.Fatalf("worker scan second candidate: %v", err)
	}

	completedScan, err := store.GetScan(ctx, scanID)
	if err != nil {
		t.Fatalf("load completed scan: %v", err)
	}

	candidatesByID := make(map[string]models.WholeSceneCandidate)
	for _, candidate := range completedScan.Candidates {
		candidatesByID[candidate.ID] = candidate
	}

	first := candidatesByID[candidateIDs[0]]
	if first.AIAssistStatus != "succeeded" {
		t.Fatalf("expected first candidate succeeded, got %#v", first)
	}
	if first.Title == nil || *first.Title != "AI Assisted Candidate One" {
		t.Fatalf("expected first candidate updated by AI, got %#v", first.Title)
	}
	if first.ApproxValue == nil || *first.ApproxValue != "31.50" {
		t.Fatalf("expected first candidate approx value 31.50, got %#v", first.ApproxValue)
	}

	second := candidatesByID[candidateIDs[1]]
	if second.AIAssistStatus != "failed" {
		t.Fatalf("expected second candidate failed, got %#v", second)
	}
	if !strings.Contains(second.AIAssistErrorMessage, "candidate-specific provider failure") {
		t.Fatalf("expected second candidate provider failure message, got %q", second.AIAssistErrorMessage)
	}
	if second.Title == nil || *second.Title != "Candidate Two" {
		t.Fatalf("failed candidate should preserve existing title, got %#v", second.Title)
	}
}

func TestWholeSceneCandidateManagementIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	store := NewWholeSceneStore(pool, t.TempDir(), 1)
	handler := NewWholeSceneHandler(store)

	scanID, sessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, "/tmp/manual-candidate.jpg", "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	router := chi.NewRouter()
	router.Post("/whole-scene/scans/{id}/candidates", handler.AddCandidate)
	router.Patch("/whole-scene/scans/{id}/candidates/{candidateID}", handler.PatchCandidate)
	router.Post("/whole-scene/scans/{id}/candidates/{candidateID}/reject", handler.RejectCandidate)
	router.Post("/whole-scene/scans/{id}/candidates/{candidateID}/approve", handler.ApproveCandidate)

	addBody := `{"title":"Loose Adapter","description":"USB-C adapter","approx_value":"9.99","confidence_label":"medium","uncertainty_notes":"manual estimate"}`
	addReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates", strings.NewReader(addBody))
	addReq.Header.Set("Content-Type", "application/json")
	addRec := httptest.NewRecorder()
	router.ServeHTTP(addRec, addReq)
	if addRec.Code != http.StatusCreated {
		t.Fatalf("expected add status %d, got %d body=%s", http.StatusCreated, addRec.Code, addRec.Body.String())
	}

	var addResponse models.WholeSceneCandidateMutationResponse
	if err := json.Unmarshal(addRec.Body.Bytes(), &addResponse); err != nil {
		t.Fatalf("unmarshal add response: %v", err)
	}
	if addResponse.Scan == nil {
		t.Fatalf("expected add response to include scan, got %#v", addResponse)
	}
	if len(addResponse.Scan.Candidates) != 1 {
		t.Fatalf("expected one manual candidate, got %d", len(addResponse.Scan.Candidates))
	}
	candidateID := addResponse.Scan.Candidates[0].ID
	if addResponse.Scan.Candidates[0].Source != "manual" {
		t.Fatalf("expected manual candidate source, got %q", addResponse.Scan.Candidates[0].Source)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title)
		VALUES ($1::uuid, 'manual', 'proposed', 'Keep Scan Open')
	`, scanID); err != nil {
		t.Fatalf("insert pending candidate to keep scan open: %v", err)
	}

	patchBody := `{"title":"Loose USB-C Adapter","approx_value":"12.00","confidence_label":"high"}`
	patchReq := httptest.NewRequest(http.MethodPatch, "/whole-scene/scans/"+scanID+"/candidates/"+candidateID, strings.NewReader(patchBody))
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	router.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected patch status %d, got %d body=%s", http.StatusOK, patchRec.Code, patchRec.Body.String())
	}

	var patchResponse models.WholeSceneCandidateMutationResponse
	if err := json.Unmarshal(patchRec.Body.Bytes(), &patchResponse); err != nil {
		t.Fatalf("unmarshal patch response: %v", err)
	}
	if patchResponse.Scan == nil {
		t.Fatalf("expected patch response to include scan, got %#v", patchResponse)
	}
	var patchedCandidate *models.WholeSceneCandidate
	for index := range patchResponse.Scan.Candidates {
		if patchResponse.Scan.Candidates[index].ID == candidateID {
			patchedCandidate = &patchResponse.Scan.Candidates[index]
		}
	}
	if patchedCandidate == nil || patchedCandidate.Title == nil || *patchedCandidate.Title != "Loose USB-C Adapter" {
		t.Fatalf("expected patched candidate title, got %#v", patchResponse.Scan.Candidates)
	}
	if patchedCandidate.Status != "edited" {
		t.Fatalf("expected edited status, got %q", patchedCandidate.Status)
	}

	rejectReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+candidateID+"/reject", nil)
	rejectRec := httptest.NewRecorder()
	router.ServeHTTP(rejectRec, rejectReq)
	if rejectRec.Code != http.StatusOK {
		t.Fatalf("expected reject status %d, got %d body=%s", http.StatusOK, rejectRec.Code, rejectRec.Body.String())
	}
	secondRejectReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+candidateID+"/reject", nil)
	secondRejectRec := httptest.NewRecorder()
	router.ServeHTTP(secondRejectRec, secondRejectReq)
	if secondRejectRec.Code != http.StatusOK {
		t.Fatalf("expected idempotent reject status %d, got %d body=%s", http.StatusOK, secondRejectRec.Code, secondRejectRec.Body.String())
	}

	rejectedPatchReq := httptest.NewRequest(http.MethodPatch, "/whole-scene/scans/"+scanID+"/candidates/"+candidateID, strings.NewReader(`{"title":"Should Not Update"}`))
	rejectedPatchReq.Header.Set("Content-Type", "application/json")
	rejectedPatchRec := httptest.NewRecorder()
	router.ServeHTTP(rejectedPatchRec, rejectedPatchReq)
	if rejectedPatchRec.Code != http.StatusConflict {
		t.Fatalf("expected rejected patch conflict, got %d body=%s", rejectedPatchRec.Code, rejectedPatchRec.Body.String())
	}

	rejectedApproveReq := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+candidateID+"/approve", nil)
	rejectedApproveRec := httptest.NewRecorder()
	router.ServeHTTP(rejectedApproveRec, rejectedApproveReq)
	if rejectedApproveRec.Code != http.StatusConflict {
		t.Fatalf("expected rejected approve conflict, got %d body=%s", rejectedApproveRec.Code, rejectedApproveRec.Body.String())
	}
}

func TestWholeSceneTerminalCleanupWaitsForPendingCandidatesIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0640); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	store := NewWholeSceneStore(pool, tempDir, 1, NewManagedFileService([]string{tempDir}))
	scanID, sessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, sourcePath, "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	var rejectID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title)
		VALUES ($1::uuid, 'manual', 'proposed', 'Reject Me')
		RETURNING id::text
	`, scanID).Scan(&rejectID); err != nil {
		t.Fatalf("insert candidate to reject: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title)
		VALUES ($1::uuid, 'manual', 'proposed', 'Still Pending')
	`, scanID); err != nil {
		t.Fatalf("insert pending candidate: %v", err)
	}

	result, err := store.RejectCandidate(ctx, scanID, rejectID)
	if err != nil {
		t.Fatalf("reject candidate: %v", err)
	}
	if result.CleanedUp {
		t.Fatalf("scan should not be cleaned up while another candidate is pending: %#v", result)
	}
	if result.Scan == nil {
		t.Fatalf("expected refreshed scan when cleanup does not run")
	}
	if _, err := os.Stat(sourcePath); err != nil {
		t.Fatalf("source file should remain while scan is still active: %v", err)
	}
}

func TestWholeSceneTerminalCleanupPreservesApprovedItemAndImageIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.jpg")
	approvedCropPath := filepath.Join(tempDir, "approved-crop.jpg")
	unusedCropPath := filepath.Join(tempDir, "unused-crop.jpg")
	for _, path := range []string{sourcePath, approvedCropPath, unusedCropPath} {
		if err := os.WriteFile(path, []byte(filepath.Base(path)), 0640); err != nil {
			t.Fatalf("write test file %s: %v", path, err)
		}
	}

	store := NewWholeSceneStore(pool, tempDir, 1, NewManagedFileService([]string{tempDir}))
	scanID, sessionID, sourceImageID := insertWholeSceneAnalysisFixture(t, ctx, pool, sourcePath, "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	var scanImageID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text
		FROM whole_scene_scan_images
		WHERE scan_id = $1::uuid
		LIMIT 1
	`, scanID).Scan(&scanImageID); err != nil {
		t.Fatalf("load scan image id: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_analysis_runs (
			scan_id,
			run_number,
			status,
			provider_type,
			model_name,
			prompt_version,
			queued_datetime,
			completed_datetime,
			raw_response_text
		)
		VALUES ($1::uuid, 1, 'succeeded', 'gemini', 'test-model', 'test-prompt', now(), now(), '{"candidates":[]}')
	`, scanID); err != nil {
		t.Fatalf("insert analysis run: %v", err)
	}

	var rejectedCandidateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title, rejected_datetime)
		VALUES ($1::uuid, 'manual', 'rejected', 'Rejected Temporary', now())
		RETURNING id::text
	`, scanID).Scan(&rejectedCandidateID); err != nil {
		t.Fatalf("insert rejected candidate: %v", err)
	}
	var approveCandidateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title, approx_value)
		VALUES ($1::uuid, 'manual', 'proposed', 'Approve Temporary', 15.00)
		RETURNING id::text
	`, scanID).Scan(&approveCandidateID); err != nil {
		t.Fatalf("insert approval candidate: %v", err)
	}

	unusedCropImageID := insertWholeSceneTestImageAsset(t, ctx, pool, sessionID, unusedCropPath, "unused-crop.jpg")
	approvedCropImageID := insertWholeSceneTestImageAsset(t, ctx, pool, sessionID, approvedCropPath, "approved-crop.jpg")
	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_candidate_crops (
			candidate_id,
			scan_image_id,
			crop_image_asset_id,
			status,
			is_preferred,
			bounding_box_x,
			bounding_box_y,
			bounding_box_width,
			bounding_box_height
		)
		VALUES
			($1::uuid, $3::uuid, $4::uuid, 'generated', true, 0.1, 0.1, 0.4, 0.4),
			($2::uuid, $3::uuid, $5::uuid, 'generated', true, 0.2, 0.2, 0.4, 0.4)
	`, rejectedCandidateID, approveCandidateID, scanImageID, unusedCropImageID, approvedCropImageID); err != nil {
		t.Fatalf("insert crop records: %v", err)
	}

	result, err := store.ApproveCandidate(ctx, scanID, approveCandidateID, models.ApproveWholeSceneCandidateRequest{})
	if err != nil {
		t.Fatalf("approve final candidate: %v", err)
	}
	if !result.CleanedUp {
		t.Fatalf("expected final approval to clean up scan, got %#v", result)
	}
	if result.Scan != nil {
		t.Fatalf("cleaned up mutation response should not include deleted scan")
	}
	if result.ApprovedItemID == nil || *result.ApprovedItemID == "" {
		t.Fatalf("expected approved item id, got %#v", result.ApprovedItemID)
	}
	if result.Cleanup == nil || result.Cleanup.DeletedFileCount != 2 || result.Cleanup.MissingFileCount != 0 {
		t.Fatalf("expected source and unused crop files deleted with no missing files, got %#v", result.Cleanup)
	}

	var scanCount int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM whole_scene_scans WHERE id = $1::uuid`, scanID).Scan(&scanCount); err != nil {
		t.Fatalf("count scan rows: %v", err)
	}
	if scanCount != 0 {
		t.Fatalf("expected scan row deleted, got count %d", scanCount)
	}
	for label, query := range map[string]string{
		"scan images":           `SELECT count(*)::int FROM whole_scene_scan_images WHERE scan_id = $1::uuid`,
		"analysis runs":         `SELECT count(*)::int FROM whole_scene_analysis_runs WHERE scan_id = $1::uuid`,
		"candidates":            `SELECT count(*)::int FROM whole_scene_candidates WHERE scan_id = $1::uuid`,
		"candidate crops":       `SELECT count(*)::int FROM whole_scene_candidate_crops WHERE candidate_id IN ($1::uuid, $2::uuid)`,
		"candidate appearances": `SELECT count(*)::int FROM whole_scene_candidate_appearances WHERE candidate_id IN ($1::uuid, $2::uuid)`,
	} {
		var count int
		var err error
		if label == "candidate crops" || label == "candidate appearances" {
			err = pool.QueryRow(ctx, query, rejectedCandidateID, approveCandidateID).Scan(&count)
		} else {
			err = pool.QueryRow(ctx, query, scanID).Scan(&count)
		}
		if err != nil {
			t.Fatalf("count %s: %v", label, err)
		}
		if count != 0 {
			t.Fatalf("expected %s rows deleted, got %d", label, count)
		}
	}

	for label, imageID := range map[string]string{
		"source image": sourceImageID,
		"unused crop":  unusedCropImageID,
	} {
		var count int
		if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM image_assets WHERE id = $1::uuid`, imageID).Scan(&count); err != nil {
			t.Fatalf("count %s asset: %v", label, err)
		}
		if count != 0 {
			t.Fatalf("expected %s asset deleted, got count %d", label, count)
		}
	}

	var approvedItemCount int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM items WHERE id = $1::uuid`, *result.ApprovedItemID).Scan(&approvedItemCount); err != nil {
		t.Fatalf("count approved item: %v", err)
	}
	if approvedItemCount != 1 {
		t.Fatalf("expected approved item preserved, got count %d", approvedItemCount)
	}

	var approvedCropItemID string
	var approvedCropSessionID *string
	if err := pool.QueryRow(ctx, `
		SELECT item_id::text, session_id::text
		FROM image_assets
		WHERE id = $1::uuid
	`, approvedCropImageID).Scan(&approvedCropItemID, &approvedCropSessionID); err != nil {
		t.Fatalf("load approved crop asset: %v", err)
	}
	if approvedCropItemID != *result.ApprovedItemID {
		t.Fatalf("expected approved crop attached to item %s, got %s", *result.ApprovedItemID, approvedCropItemID)
	}
	if approvedCropSessionID != nil {
		t.Fatalf("expected approved crop detached from temporary upload session, got %s", *approvedCropSessionID)
	}

	var sessionCount int
	if err := pool.QueryRow(ctx, `SELECT count(*)::int FROM upload_sessions WHERE id = $1::uuid`, sessionID).Scan(&sessionCount); err != nil {
		t.Fatalf("count upload session: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("expected whole scene upload session deleted, got count %d", sessionCount)
	}
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("expected source file deleted, stat err=%v", err)
	}
	if _, err := os.Stat(unusedCropPath); !os.IsNotExist(err) {
		t.Fatalf("expected unused crop file deleted, stat err=%v", err)
	}
	if _, err := os.Stat(approvedCropPath); err != nil {
		t.Fatalf("expected approved crop file preserved: %v", err)
	}
}

func TestWholeSceneFinalRejectResponseAfterCleanupIntegration(t *testing.T) {
	databaseURL := os.Getenv("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("FASTSELL_WHOLE_SCENE_INTEGRATION_DATABASE_URL is not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.jpg")
	if err := os.WriteFile(sourcePath, []byte("source"), 0640); err != nil {
		t.Fatalf("write source file: %v", err)
	}
	store := NewWholeSceneStore(pool, tempDir, 1, NewManagedFileService([]string{tempDir}))
	handler := NewWholeSceneHandler(store)
	scanID, sessionID, _ := insertWholeSceneAnalysisFixture(t, ctx, pool, sourcePath, "processed")
	defer cleanupWholeSceneIntegrationFixture(context.Background(), pool, scanID, sessionID)

	var finalCandidateID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO whole_scene_candidates (scan_id, source, status, title)
		VALUES ($1::uuid, 'manual', 'proposed', 'Final Reject')
		RETURNING id::text
	`, scanID).Scan(&finalCandidateID); err != nil {
		t.Fatalf("insert final candidate: %v", err)
	}

	router := chi.NewRouter()
	router.Post("/whole-scene/scans/{id}/candidates/{candidateID}/reject", handler.RejectCandidate)
	req := httptest.NewRequest(http.MethodPost, "/whole-scene/scans/"+scanID+"/candidates/"+finalCandidateID+"/reject", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected final reject status %d, got %d body=%s", http.StatusOK, rec.Code, rec.Body.String())
	}

	var response models.WholeSceneCandidateMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal final reject response: %v", err)
	}
	if !response.CleanedUp || response.Scan != nil || response.ScanID != scanID {
		t.Fatalf("expected cleanup response without scan, got %#v", response)
	}
	if response.Cleanup == nil || response.Cleanup.DeletedImageAssetCount != 1 {
		t.Fatalf("expected cleanup asset count in response, got %#v", response.Cleanup)
	}
}

func insertWholeSceneAnalysisFixture(t *testing.T, ctx context.Context, pool *pgxpool.Pool, imagePath string, imageStatus string) (string, string, string) {
	t.Helper()

	var inventoryGroupID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text
		FROM inventory_groups
		WHERE code = $1
	`, defaultInventoryGroupCode).Scan(&inventoryGroupID); err != nil {
		t.Fatalf("load default inventory group: %v", err)
	}

	var sessionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO upload_sessions (source, status)
		VALUES ($1, 'processed')
		RETURNING id::text
	`, wholeSceneUploadSource).Scan(&sessionID); err != nil {
		t.Fatalf("insert upload session: %v", err)
	}

	var scanID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO whole_scene_scans (upload_session_id, inventory_group_id, hint, status)
		VALUES ($1::uuid, $2::uuid, 'test hint', 'created')
		RETURNING id::text
	`, sessionID, inventoryGroupID).Scan(&scanID); err != nil {
		t.Fatalf("insert scan: %v", err)
	}

	var imageID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_assets (
			session_id,
			original_filename,
			stored_filename,
			file_path,
			file_hash,
			mime_type,
			file_size_bytes,
			is_original,
			status,
			upload_order
		)
		VALUES ($1::uuid, 'scene.jpg', 'scene.jpg', $2, 'hash-' || gen_random_uuid()::text, 'image/jpeg', 4, true, $3, 0)
		RETURNING id::text
	`, sessionID, imagePath, imageStatus).Scan(&imageID); err != nil {
		t.Fatalf("insert image asset: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO whole_scene_scan_images (scan_id, image_asset_id, sort_order)
		VALUES ($1::uuid, $2::uuid, 0)
	`, scanID, imageID); err != nil {
		t.Fatalf("insert scan image: %v", err)
	}

	return scanID, sessionID, imageID
}

func insertWholeSceneTestImageAsset(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sessionID string, imagePath string, filename string) string {
	t.Helper()

	var imageID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO image_assets (
			session_id,
			original_filename,
			stored_filename,
			file_path,
			file_hash,
			mime_type,
			file_size_bytes,
			is_original,
			status,
			upload_order
		)
		VALUES ($1::uuid, $2, $2, $3, 'hash-' || gen_random_uuid()::text, 'image/jpeg', 4, true, 'processed', 0)
		RETURNING id::text
	`, sessionID, filename, imagePath).Scan(&imageID); err != nil {
		t.Fatalf("insert image asset %s: %v", filename, err)
	}
	return imageID
}

func cleanupWholeSceneIntegrationFixture(ctx context.Context, pool *pgxpool.Pool, scanID string, sessionID string) {
	_, _ = pool.Exec(ctx, `DELETE FROM whole_scene_scans WHERE id = $1::uuid`, scanID)
	_, _ = pool.Exec(ctx, `DELETE FROM image_assets WHERE session_id = $1::uuid`, sessionID)
	_, _ = pool.Exec(ctx, `DELETE FROM upload_sessions WHERE id = $1::uuid`, sessionID)
}

func writeWholeSceneTestJPEG(path string, width int, height int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 5), G: uint8(y * 5), B: 120, A: 255})
		}
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	encodeErr := jpeg.Encode(file, img, &jpeg.Options{Quality: 90})
	closeErr := file.Close()
	if encodeErr != nil {
		return encodeErr
	}
	return closeErr
}

func errorsIs(err error, target error) bool {
	return err != nil && errors.Is(err, target)
}
