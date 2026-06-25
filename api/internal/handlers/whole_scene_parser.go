package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

type wholeSceneParsedResponse struct {
	Candidates              []wholeSceneParsedCandidate
	Warnings                []string
	LocalizationDiagnostics []wholeSceneLocalizationDiagnostic
}

type wholeSceneParsedCandidate struct {
	CandidateKey     string
	Title            string
	Description      *string
	ApproxValue      *string
	ConfidenceLabel  *string
	UncertaintyNotes *string
	RawCandidate     json.RawMessage
	ParseWarnings    []string
	Appearances      []wholeSceneParsedAppearance
}

type wholeSceneParsedAppearance struct {
	CandidateKey     *string
	TargetLabel      *string
	SourceImageIndex *int
	SourceImageID    *string
	ImageAssetID     *string
	BoundingBox      wholeSceneParsedBoundingBox
	LocalizationData json.RawMessage
	ConfidenceLabel  *string
	Notes            *string
	DiagnosticIndex  *int
}

type wholeSceneParsedBoundingBox struct {
	X              *float64
	Y              *float64
	Width          *float64
	Height         *float64
	CoordinateMode string
	CoordinateUnit string
}

type wholeSceneLocalizationParseResult struct {
	Warnings          []string
	CandidateWarnings map[int][]string
	Diagnostics       []wholeSceneLocalizationDiagnostic
}

type wholeSceneLocalizationDiagnostic struct {
	ScanID                string                         `json:"scan_id,omitempty"`
	CandidateKey          string                         `json:"candidate_key"`
	CandidateTitle        string                         `json:"candidate_title"`
	TargetLabel           string                         `json:"target_label,omitempty"`
	SourceImageIndex      *int                           `json:"source_image_index,omitempty"`
	RawBBoxValues         []float64                      `json:"raw_bbox_values,omitempty"`
	BBoxFieldName         string                         `json:"bbox_field_name,omitempty"`
	InterpretedBBoxFormat string                         `json:"interpreted_bbox_format,omitempty"`
	InterpretedBBoxUnits  string                         `json:"interpreted_bbox_units,omitempty"`
	FinalPixelRect        *wholeSceneDiagnosticPixelRect `json:"final_pixel_rect,omitempty"`
	Confidence            string                         `json:"confidence,omitempty"`
	Status                string                         `json:"status,omitempty"`
	Found                 *bool                          `json:"found,omitempty"`
	Accepted              bool                           `json:"accepted"`
	Reason                string                         `json:"reason"`
	CropImageAssetID      string                         `json:"crop_image_asset_id,omitempty"`
	CropPath              string                         `json:"crop_path,omitempty"`
	OverlayNumber         *int                           `json:"overlay_number,omitempty"`
	OverlayPath           string                         `json:"overlay_path,omitempty"`
	diagnosticBoundingBox wholeSceneParsedBoundingBox
}

type wholeSceneDiagnosticPixelRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func parseWholeSceneGeminiResponse(rawResponse []byte) (wholeSceneParsedResponse, error) {
	var result wholeSceneParsedResponse
	document, err := extractWholeSceneCandidateDocument(rawResponse)
	if err != nil {
		return result, err
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(document, &root); err != nil {
		return result, fmt.Errorf("Whole Scene response did not contain valid candidate JSON: %w", err)
	}

	candidatesJSON, ok := firstRawField(root, "candidates", "items", "detected_items")
	if !ok {
		return result, errors.New("Whole Scene response did not contain a candidates array")
	}

	var candidateMessages []json.RawMessage
	if err := json.Unmarshal(candidatesJSON, &candidateMessages); err != nil {
		return result, errors.New("Whole Scene response candidates field was not an array")
	}

	for index, candidateJSON := range candidateMessages {
		candidate, err := parseWholeSceneCandidate(candidateJSON)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("candidate %d skipped: %s", index, err.Error()))
			continue
		}
		result.Candidates = append(result.Candidates, candidate)
	}

	if len(result.Candidates) == 0 {
		return result, errors.New("Whole Scene response did not contain any usable candidates")
	}

	return result, nil
}

func parseWholeSceneLocalizationResponse(rawResponse []byte, candidates []wholeSceneParsedCandidate) (wholeSceneLocalizationParseResult, error) {
	result := wholeSceneLocalizationParseResult{CandidateWarnings: make(map[int][]string)}
	document, err := extractWholeSceneCandidateDocument(rawResponse)
	if err != nil {
		return result, err
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(document, &root); err != nil {
		return result, fmt.Errorf("Whole Scene localization response did not contain valid JSON: %w", err)
	}

	localizationsJSON, ok := firstRawField(root, "localizations", "results", "candidates")
	if !ok {
		return result, errors.New("Whole Scene localization response did not contain a localizations array")
	}

	var localizationMessages []json.RawMessage
	if err := json.Unmarshal(localizationsJSON, &localizationMessages); err != nil {
		return result, errors.New("Whole Scene localization response localizations field was not an array")
	}

	byKey := make(map[string]int)
	for index, candidate := range candidates {
		if key := normalizeWholeSceneCandidateKey(candidate.CandidateKey); key != "" {
			byKey[key] = index
		}
		candidates[index].Appearances = nil
	}

	for _, localizationJSON := range localizationMessages {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(localizationJSON, &fields); err != nil {
			result.Diagnostics = append(result.Diagnostics, wholeSceneLocalizationDiagnostic{
				Accepted: false,
				Reason:   "localization was not an object",
			})
			continue
		}

		candidateKey := derefString(trimStringPointer(rawStringField(fields, "candidate_key")))
		targetLabel := derefString(trimStringPointer(rawStringField(fields, "target_label")))
		sourceImageIndex := rawIntField(fields, "source_image_index")
		found := rawBoolField(fields, "found")
		status := normalizeWholeSceneLocalizationStatus(rawStringField(fields, "status", "result"))
		confidence := normalizeWholeSceneConfidence(rawAnyField(fields, "confidence"))
		confidenceText := derefString(confidence)
		bbox, rawBBoxValues, bboxWarning := parseStrictWholeSceneLocalizationBox2D(fields)
		bboxFieldName := strictWholeSceneBBoxFieldName(fields)
		diagnostic := wholeSceneLocalizationDiagnostic{
			CandidateKey:          candidateKey,
			TargetLabel:           targetLabel,
			SourceImageIndex:      sourceImageIndex,
			RawBBoxValues:         rawBBoxValues,
			BBoxFieldName:         bboxFieldName,
			InterpretedBBoxFormat: bbox.CoordinateMode,
			InterpretedBBoxUnits:  bbox.CoordinateUnit,
			Confidence:            confidenceText,
			Status:                status,
			Found:                 found,
			Accepted:              false,
			Reason:                "accepted",
			diagnosticBoundingBox: bbox,
		}

		candidateIndex, ok := matchWholeSceneLocalizationCandidateKey(candidateKey, byKey)
		if !ok {
			diagnostic.Reason = "candidate_key did not match a pass 1 candidate"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			continue
		}
		diagnostic.CandidateTitle = candidates[candidateIndex].Title

		if targetLabel == "" {
			diagnostic.Reason = "target_label was missing"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because target_label was missing")
			continue
		}
		if found == nil {
			diagnostic.Reason = "found was missing"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because found was missing")
			continue
		}
		if !*found || status == "not_found" || status == "missing" || status == "uncertain" {
			diagnostic.Reason = "localization returned not_found"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization returned not_found")
			continue
		}

		if confidence == nil || *confidence != "high" {
			diagnostic.Reason = "confidence was not high"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because confidence was not high")
			continue
		}
		if sourceImageIndex == nil || *sourceImageIndex < 0 {
			diagnostic.Reason = "source_image_index was missing or invalid"
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because source image reference was missing or invalid")
			continue
		}
		if bboxWarning != "" {
			diagnostic.Reason = bboxWarning
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because "+bboxWarning)
			continue
		}
		if !wholeSceneBBoxComplete(bbox) {
			diagnostic.Reason = "box_2d was missing"
			if bboxFieldName != "" && bboxFieldName != "box_2d" {
				diagnostic.Reason = fmt.Sprintf("%s is not accepted for automatic crops; use explicit box_2d", bboxFieldName)
			}
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization returned no strict box_2d")
			continue
		}
		targetLabelPtr := trimStringPointer(&targetLabel)
		if localizationTargetLabelMatchesDifferentCandidate(targetLabelPtr, candidateIndex, candidates) {
			diagnostic.Reason = fmt.Sprintf("target_label %q matched a different candidate", targetLabel)
			result.Diagnostics = append(result.Diagnostics, diagnostic)
			result.addCandidateWarning(candidateIndex, "localization skipped because "+diagnostic.Reason)
			continue
		}

		diagnostic.Accepted = true
		diagnostic.Reason = "accepted"
		diagnosticIndex := len(result.Diagnostics)
		result.Diagnostics = append(result.Diagnostics, diagnostic)
		appearance := wholeSceneParsedAppearance{
			CandidateKey:     &candidateKey,
			TargetLabel:      nil,
			SourceImageIndex: sourceImageIndex,
			BoundingBox:      bbox,
			LocalizationData: cloneRawMessage(localizationJSON),
			ConfidenceLabel:  confidence,
			DiagnosticIndex:  &diagnosticIndex,
		}
		if warning := validateWholeSceneAppearanceCandidateMatch(appearance, candidates[candidateIndex].CandidateKey, candidates[candidateIndex].Title); warning != "" {
			result.Diagnostics[diagnosticIndex].Accepted = false
			result.Diagnostics[diagnosticIndex].Reason = warning
			result.addCandidateWarning(candidateIndex, "localization skipped because "+warning)
			continue
		}
		candidates[candidateIndex].Appearances = append(candidates[candidateIndex].Appearances, appearance)
	}

	return result, nil
}

func (result *wholeSceneLocalizationParseResult) addCandidateWarning(candidateIndex int, warning string) {
	warning = strings.TrimSpace(warning)
	if warning == "" {
		return
	}
	if result.CandidateWarnings == nil {
		result.CandidateWarnings = make(map[int][]string)
	}
	result.CandidateWarnings[candidateIndex] = append(result.CandidateWarnings[candidateIndex], warning)
}

func extractWholeSceneCandidateDocument(rawResponse []byte) ([]byte, error) {
	body := bytes.TrimSpace(rawResponse)
	if len(body) == 0 {
		return nil, errors.New("Whole Scene response was empty")
	}

	if text, ok := extractGeminiTextResponse(body); ok {
		body = []byte(stripJSONCodeFence(text))
	}

	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("Whole Scene response text was empty")
	}
	return body, nil
}

func extractGeminiTextResponse(body []byte) (string, bool) {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil {
		return "", false
	}

	candidatesJSON, ok := root["candidates"]
	if !ok {
		return "", false
	}

	var geminiCandidates []map[string]json.RawMessage
	if err := json.Unmarshal(candidatesJSON, &geminiCandidates); err != nil {
		return "", false
	}

	var texts []string
	for _, candidate := range geminiCandidates {
		contentJSON, ok := candidate["content"]
		if !ok {
			continue
		}
		var content map[string]json.RawMessage
		if err := json.Unmarshal(contentJSON, &content); err != nil {
			continue
		}
		partsJSON, ok := content["parts"]
		if !ok {
			continue
		}
		var parts []map[string]json.RawMessage
		if err := json.Unmarshal(partsJSON, &parts); err != nil {
			continue
		}
		for _, part := range parts {
			text := trimStringPointer(rawStringField(part, "text"))
			if text != nil {
				texts = append(texts, *text)
			}
		}
	}

	if len(texts) == 0 {
		return "", false
	}
	return strings.Join(texts, "\n"), true
}

func stripJSONCodeFence(value string) string {
	text := strings.TrimSpace(value)
	if !strings.HasPrefix(text, "```") {
		return text
	}

	lines := strings.Split(text, "\n")
	if len(lines) >= 2 {
		lines = lines[1:]
		if len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[len(lines)-1]), "```") {
			lines = lines[:len(lines)-1]
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	return text
}

func parseWholeSceneCandidate(candidateJSON json.RawMessage) (wholeSceneParsedCandidate, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(candidateJSON, &fields); err != nil {
		return wholeSceneParsedCandidate{}, errors.New("candidate was not an object")
	}

	title := trimStringPointer(rawStringField(fields, "title", "name", "item_name"))
	description := trimStringPointer(rawStringField(fields, "description", "notes"))
	if title == nil && description != nil {
		derived := truncateReviewAssistText(*description, 120)
		title = &derived
	}
	if title == nil {
		return wholeSceneParsedCandidate{}, errors.New("candidate was missing a usable title")
	}

	var warnings []string
	if _, ok := firstRawField(fields, "title", "name", "item_name"); !ok && description != nil {
		warnings = append(warnings, "title was derived from description")
	}

	approxValue, approxWarning := parseWholeSceneApproxValue(fields)
	if approxWarning != "" {
		warnings = append(warnings, approxWarning)
	}

	confidence := normalizeWholeSceneConfidence(rawAnyField(fields, "confidence_label", "confidence", "certainty"))
	uncertainty := trimStringPointer(rawStringField(fields, "uncertainty_notes", "uncertainty", "uncertain", "risk", "notes"))

	candidateKey := trimStringPointer(rawStringField(fields, "candidate_key", "candidate_id", "stable_key", "item_key"))
	if candidateKey == nil {
		derived := wholeSceneStableCandidateKey(*title, description)
		candidateKey = &derived
		warnings = append(warnings, "candidate_key was derived from title")
	}

	appearances, appearanceWarnings := parseWholeSceneAppearances(fields, *candidateKey, *title)
	warnings = append(warnings, appearanceWarnings...)

	return wholeSceneParsedCandidate{
		CandidateKey:     *candidateKey,
		Title:            *title,
		Description:      description,
		ApproxValue:      approxValue,
		ConfidenceLabel:  confidence,
		UncertaintyNotes: uncertainty,
		RawCandidate:     cloneRawMessage(candidateJSON),
		ParseWarnings:    warnings,
		Appearances:      appearances,
	}, nil
}

func matchWholeSceneLocalizationCandidateKey(candidateKey string, byKey map[string]int) (int, bool) {
	normalized := normalizeWholeSceneCandidateKey(candidateKey)
	if normalized == "" {
		return 0, false
	}
	index, ok := byKey[normalized]
	return index, ok
}

func localizationTargetLabelMatchesDifferentCandidate(targetLabel *string, candidateIndex int, candidates []wholeSceneParsedCandidate) bool {
	if targetLabel == nil {
		return false
	}
	target := normalizeWholeSceneCandidateKey(*targetLabel)
	if target == "" {
		return false
	}
	for index, candidate := range candidates {
		if index == candidateIndex {
			continue
		}
		if target == normalizeWholeSceneCandidateKey(candidate.Title) || target == normalizeWholeSceneCandidateKey(candidate.CandidateKey) {
			return true
		}
	}
	return false
}

func parseWholeSceneApproxValue(fields map[string]json.RawMessage) (*string, string) {
	valueJSON, ok := firstRawField(fields, "approx_value", "approximate_value", "estimated_value", "value")
	if !ok || len(bytes.TrimSpace(valueJSON)) == 0 || bytes.Equal(bytes.TrimSpace(valueJSON), []byte("null")) {
		return nil, ""
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(valueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil, "approx_value could not be parsed"
	}

	var valueText string
	switch value := decoded.(type) {
	case json.Number:
		valueText = value.String()
	case string:
		valueText = strings.TrimSpace(value)
	default:
		return nil, "approx_value had an unsupported shape"
	}

	valueText = strings.TrimSpace(strings.TrimPrefix(valueText, "$"))
	valueText = strings.ReplaceAll(valueText, ",", "")
	if valueText == "" {
		return nil, ""
	}

	parsed, err := strconv.ParseFloat(valueText, 64)
	if err != nil || parsed < 0 {
		return nil, "approx_value was not a non-negative number"
	}

	normalized := fmt.Sprintf("%.2f", parsed)
	if !moneyValuePattern.MatchString(normalized) {
		return nil, "approx_value exceeded supported precision"
	}
	return &normalized, ""
}

func normalizeWholeSceneConfidence(raw json.RawMessage) *string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		value := "unknown"
		return &value
	}

	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		value := "unknown"
		return &value
	}

	switch value := decoded.(type) {
	case json.Number:
		number, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			break
		}
		switch {
		case number >= 0.75:
			confidence := "high"
			return &confidence
		case number >= 0.4:
			confidence := "medium"
			return &confidence
		default:
			confidence := "low"
			return &confidence
		}
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		normalized = strings.NewReplacer("-", "_").Replace(normalized)
		switch normalized {
		case "high", "very high", "very_high", "high confidence", "high_confidence", "highly confident", "highly_confident", "confident", "certain", "strong":
			confidence := "high"
			return &confidence
		case "medium", "moderate", "med":
			confidence := "medium"
			return &confidence
		case "low", "weak", "uncertain", "unsure":
			confidence := "low"
			return &confidence
		case "unknown", "":
			confidence := "unknown"
			return &confidence
		}
	}

	confidence := "unknown"
	return &confidence
}

func normalizeWholeSceneLocalizationStatus(value *string) string {
	normalized := strings.ToLower(strings.TrimSpace(derefString(value)))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	switch normalized {
	case "found", "localized", "located", "present", "visible", "detected", "success", "succeeded":
		return "found"
	case "not_found", "missing", "absent", "not_present", "not_visible", "uncertain", "ambiguous", "failed":
		return "not_found"
	default:
		return normalized
	}
}

func parseWholeSceneAppearances(fields map[string]json.RawMessage, candidateKey string, candidateTitle string) ([]wholeSceneParsedAppearance, []string) {
	appearancesJSON, ok := firstRawField(fields, "appearances", "source_image_appearances", "locations")
	if !ok {
		return nil, nil
	}

	var rawAppearances []json.RawMessage
	if err := json.Unmarshal(appearancesJSON, &rawAppearances); err != nil {
		return nil, []string{"appearances field was not an array"}
	}

	appearances := make([]wholeSceneParsedAppearance, 0, len(rawAppearances))
	warnings := make([]string, 0)
	for index, appearanceJSON := range rawAppearances {
		appearance, appearanceWarnings, ok := parseWholeSceneAppearance(appearanceJSON)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("appearance %d skipped: source image reference was missing or invalid", index))
			continue
		}
		if warning := validateWholeSceneAppearanceCandidateMatch(appearance, candidateKey, candidateTitle); warning != "" {
			warnings = append(warnings, fmt.Sprintf("appearance %d skipped: %s", index, warning))
			continue
		}
		warnings = append(warnings, appearanceWarnings...)
		appearances = append(appearances, appearance)
	}

	return appearances, warnings
}

func parseWholeSceneAppearance(appearanceJSON json.RawMessage) (wholeSceneParsedAppearance, []string, bool) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(appearanceJSON, &fields); err != nil {
		return wholeSceneParsedAppearance{}, nil, false
	}

	sourceImageIndex := rawIntField(fields, "source_image_index", "sourceImageIndex", "image_index", "imageIndex", "source_index", "sourceIndex")
	if sourceImageIndex != nil && *sourceImageIndex < 0 {
		sourceImageIndex = nil
	}
	sourceImageID := trimStringPointer(rawStringField(fields, "source_image_id", "sourceImageId", "scan_image_id", "scanImageId"))
	imageAssetID := trimStringPointer(rawStringField(fields, "image_asset_id", "imageAssetId", "source_image_asset_id", "sourceImageAssetId"))
	if sourceImageIndex == nil && sourceImageID == nil && imageAssetID == nil {
		return wholeSceneParsedAppearance{}, nil, false
	}

	candidateKey := trimStringPointer(rawStringField(fields, "candidate_key", "candidateKey", "candidate_id", "candidateId", "stable_key", "stableKey", "item_key", "itemKey"))
	targetLabel := trimStringPointer(rawStringField(fields, "target_label", "targetLabel", "candidate_title", "candidateTitle", "title", "item_name", "itemName", "name", "label"))
	var warnings []string
	bbox, bboxWarning := parseWholeSceneBoundingBox(fields)
	if bboxWarning != "" {
		warnings = append(warnings, bboxWarning)
	}

	return wholeSceneParsedAppearance{
		CandidateKey:     candidateKey,
		TargetLabel:      targetLabel,
		SourceImageIndex: sourceImageIndex,
		SourceImageID:    sourceImageID,
		ImageAssetID:     imageAssetID,
		BoundingBox:      bbox,
		LocalizationData: cloneRawMessage(appearanceJSON),
		ConfidenceLabel:  normalizeWholeSceneConfidence(rawAnyField(fields, "confidence_label", "confidenceLabel", "confidence", "confidence_score", "confidenceScore")),
		Notes:            trimStringPointer(rawStringField(fields, "notes", "uncertainty", "label")),
	}, warnings, true
}

func validateWholeSceneAppearanceCandidateMatch(appearance wholeSceneParsedAppearance, candidateKey string, candidateTitle string) string {
	if appearance.CandidateKey != nil {
		if normalizeWholeSceneCandidateKey(*appearance.CandidateKey) != normalizeWholeSceneCandidateKey(candidateKey) {
			return fmt.Sprintf("candidate_key %q did not match candidate %q", *appearance.CandidateKey, candidateKey)
		}
	}
	if appearance.TargetLabel != nil {
		if !wholeSceneLabelsMatch(candidateTitle, *appearance.TargetLabel) {
			return fmt.Sprintf("target_label %q did not match candidate title %q", *appearance.TargetLabel, candidateTitle)
		}
		return ""
	}
	if appearance.CandidateKey != nil {
		return ""
	}
	if wholeSceneBBoxComplete(appearance.BoundingBox) {
		return "crop-bearing appearance did not include candidate_key or target_label"
	}
	return ""
}

func wholeSceneStableCandidateKey(title string, description *string) string {
	source := strings.TrimSpace(title)
	if source == "" && description != nil {
		source = strings.TrimSpace(*description)
	}
	key := normalizeWholeSceneCandidateKey(source)
	if key == "" {
		return "candidate"
	}
	return key
}

func normalizeWholeSceneCandidateKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var builder strings.Builder
	lastSeparator := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastSeparator = false
		case !lastSeparator:
			builder.WriteByte('_')
			lastSeparator = true
		}
	}
	return strings.Trim(builder.String(), "_")
}

func wholeSceneLabelsMatch(candidateTitle string, targetLabel string) bool {
	candidate := normalizeWholeSceneCandidateKey(candidateTitle)
	target := normalizeWholeSceneCandidateKey(targetLabel)
	if candidate == "" || target == "" {
		return false
	}
	return candidate == target || strings.Contains(candidate, target) || strings.Contains(target, candidate)
}

func parseWholeSceneBoundingBox(fields map[string]json.RawMessage) (wholeSceneParsedBoundingBox, string) {
	bboxFields := fields
	if bboxJSON, ok := firstRawField(fields, "bbox", "bounding_box", "boundingBox", "box"); ok {
		if len(bytes.TrimSpace(bboxJSON)) == 0 || bytes.Equal(bytes.TrimSpace(bboxJSON), []byte("null")) {
			return wholeSceneParsedBoundingBox{}, ""
		}
		if bytes.HasPrefix(bytes.TrimSpace(bboxJSON), []byte("[")) {
			return parseWholeSceneBoundingBoxArray(fields, bboxJSON)
		}
		if err := json.Unmarshal(bboxJSON, &bboxFields); err != nil {
			return wholeSceneParsedBoundingBox{}, "bounding box was not an object or array"
		}
	}

	mode := normalizeWholeSceneBBoxMode(rawStringValue(bboxFields, fields, "bbox_format", "bboxFormat", "format", "coordinate_format", "coordinateFormat"))
	unit := normalizeWholeSceneBBoxUnit(rawStringValue(bboxFields, fields, "bbox_units", "bboxUnits", "units", "coordinate_units", "coordinateUnits", "coordinate_system", "coordinateSystem"))

	x2 := rawFloatField(bboxFields, "x2", "right")
	y2 := rawFloatField(bboxFields, "y2", "bottom")
	x1 := rawFloatField(bboxFields, "x1")
	y1 := rawFloatField(bboxFields, "y1")
	if x1 == nil {
		x1 = rawFloatField(bboxFields, "xmin", "xMin")
	}
	if y1 == nil {
		y1 = rawFloatField(bboxFields, "ymin", "yMin")
	}
	if x2 == nil {
		x2 = rawFloatField(bboxFields, "xmax", "xMax")
	}
	if y2 == nil {
		y2 = rawFloatField(bboxFields, "ymax", "yMax")
	}
	if x2 != nil || y2 != nil {
		if x1 == nil {
			x1 = rawFloatField(bboxFields, "left")
		}
		if y1 == nil {
			y1 = rawFloatField(bboxFields, "top")
		}
	}
	if x1 != nil || y1 != nil || x2 != nil || y2 != nil {
		if x1 == nil || y1 == nil || x2 == nil || y2 == nil {
			return wholeSceneParsedBoundingBox{}, "bounding box was incomplete"
		}
		if !validWholeSceneBBoxNumbers(*x1, *y1, *x2, *y2) {
			return wholeSceneParsedBoundingBox{}, "bounding box contained invalid coordinates"
		}
		return wholeSceneParsedBoundingBox{X: x1, Y: y1, Width: x2, Height: y2, CoordinateMode: "xyxy", CoordinateUnit: unit}, ""
	}

	x := rawFloatField(bboxFields, "x", "left")
	y := rawFloatField(bboxFields, "y", "top")
	width := rawFloatField(bboxFields, "width", "w")
	height := rawFloatField(bboxFields, "height", "h")
	if x == nil && y == nil && width == nil && height == nil {
		return wholeSceneParsedBoundingBox{}, ""
	}
	if x == nil || y == nil || width == nil || height == nil {
		return wholeSceneParsedBoundingBox{}, "bounding box was incomplete"
	}
	if !validWholeSceneBBoxNumbers(*x, *y, *width, *height) {
		return wholeSceneParsedBoundingBox{}, "bounding box contained invalid coordinates"
	}

	return wholeSceneParsedBoundingBox{X: x, Y: y, Width: width, Height: height, CoordinateMode: mode, CoordinateUnit: unit}, ""
}

func parseStrictWholeSceneLocalizationBox2D(fields map[string]json.RawMessage) (wholeSceneParsedBoundingBox, []float64, string) {
	bboxJSON, ok := firstRawField(fields, "box_2d")
	if !ok {
		return wholeSceneParsedBoundingBox{}, nil, ""
	}

	values, err := parseWholeSceneNumberArray(bboxJSON)
	if err != nil {
		return wholeSceneParsedBoundingBox{}, nil, err.Error()
	}
	if len(values) != 4 {
		return wholeSceneParsedBoundingBox{}, values, "box_2d must contain exactly four coordinates"
	}
	if !validWholeSceneBBoxNumbers(values...) {
		return wholeSceneParsedBoundingBox{}, values, "box_2d contained invalid coordinates"
	}

	unit := normalizeWholeSceneStrictBox2DUnit(rawStringValue(fields, nil, "box_units"))
	if unit != "thousand" {
		return wholeSceneParsedBoundingBox{}, values, "box_units must be 0_1000 or thousand"
	}

	yMin := values[0]
	xMin := values[1]
	yMax := values[2]
	xMax := values[3]
	if yMax <= yMin || xMax <= xMin {
		return wholeSceneParsedBoundingBox{}, values, "box_2d produced an empty crop"
	}

	return wholeSceneParsedBoundingBox{
		X:              &yMin,
		Y:              &xMin,
		Width:          &yMax,
		Height:         &xMax,
		CoordinateMode: "yxyx",
		CoordinateUnit: "thousand",
	}, values, ""
}

func parseWholeSceneNumberArray(value json.RawMessage) ([]float64, error) {
	values := make([]float64, 0, 4)
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	var decoded []json.Number
	if err := decoder.Decode(&decoded); err != nil {
		return nil, errors.New("box_2d was not a numeric array")
	}
	for _, entry := range decoded {
		parsed, err := strconv.ParseFloat(entry.String(), 64)
		if err != nil {
			return nil, errors.New("box_2d contained invalid coordinates")
		}
		values = append(values, parsed)
	}
	return values, nil
}

func strictWholeSceneBBoxFieldName(fields map[string]json.RawMessage) string {
	for _, name := range []string{"box_2d", "bbox", "boundingBox", "bounding_box", "box", "box2d"} {
		if _, ok := fields[name]; ok {
			return name
		}
	}
	return ""
}

func normalizeWholeSceneStrictBox2DUnit(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	switch {
	case normalized == "0_1000", normalized == "zero_to_1000", normalized == "normalized_1000", normalized == "normalized_0_1000":
		return "thousand"
	case strings.Contains(normalized, "1000"), strings.Contains(normalized, "thousand"):
		return "thousand"
	default:
		return ""
	}
}

func parseWholeSceneBoundingBoxArray(fields map[string]json.RawMessage, bboxJSON json.RawMessage) (wholeSceneParsedBoundingBox, string) {
	values := make([]float64, 0, 4)
	decoder := json.NewDecoder(bytes.NewReader(bboxJSON))
	decoder.UseNumber()
	var decoded []json.Number
	if err := decoder.Decode(&decoded); err != nil {
		return wholeSceneParsedBoundingBox{}, "bounding box array was invalid"
	}
	if len(decoded) != 4 {
		return wholeSceneParsedBoundingBox{}, "bounding box array must contain four coordinates"
	}
	for _, value := range decoded {
		parsed, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return wholeSceneParsedBoundingBox{}, "bounding box array contained invalid coordinates"
		}
		values = append(values, parsed)
	}
	if !validWholeSceneBBoxNumbers(values...) {
		return wholeSceneParsedBoundingBox{}, "bounding box contained invalid coordinates"
	}
	x := values[0]
	y := values[1]
	width := values[2]
	height := values[3]
	mode := normalizeWholeSceneBBoxMode(rawStringValue(fields, nil, "bbox_format", "bboxFormat", "format", "coordinate_format", "coordinateFormat"))
	if mode == "" {
		mode = "yxyx"
	}
	unit := normalizeWholeSceneBBoxUnit(rawStringValue(fields, nil, "bbox_units", "bboxUnits", "units", "coordinate_units", "coordinateUnits", "coordinate_system", "coordinateSystem"))
	return wholeSceneParsedBoundingBox{X: &x, Y: &y, Width: &width, Height: &height, CoordinateMode: mode, CoordinateUnit: unit}, ""
}

func normalizeWholeSceneBBoxMode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "yxyx"), strings.Contains(value, "y1x1y2x2"), strings.Contains(value, "ymin"), strings.Contains(value, "top_left_bottom_right"):
		return "yxyx"
	case strings.Contains(value, "xyxy"), strings.Contains(value, "x1"), strings.Contains(value, "corner"):
		return "xyxy"
	case value == "":
		return ""
	default:
		return "xywh"
	}
}

func normalizeWholeSceneBBoxUnit(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch {
	case strings.Contains(value, "pixel"), value == "px":
		return "pixel"
	case strings.Contains(value, "percent"), value == "%":
		return "percent"
	case strings.Contains(value, "1000"), strings.Contains(value, "thousand"):
		return "thousand"
	case strings.Contains(value, "normal"), value == "0..1", value == "0-1":
		return "normalized"
	default:
		return ""
	}
}

func validWholeSceneBBoxNumbers(values ...float64) bool {
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func rawStringValue(primary map[string]json.RawMessage, secondary map[string]json.RawMessage, names ...string) string {
	if primary != nil {
		if value := trimStringPointer(rawStringField(primary, names...)); value != nil {
			return *value
		}
	}
	if secondary != nil {
		if value := trimStringPointer(rawStringField(secondary, names...)); value != nil {
			return *value
		}
	}
	return ""
}

func firstRawField(fields map[string]json.RawMessage, names ...string) (json.RawMessage, bool) {
	for _, name := range names {
		if value, ok := fields[name]; ok {
			return value, true
		}
	}
	return nil, false
}

func rawAnyField(fields map[string]json.RawMessage, names ...string) json.RawMessage {
	value, _ := firstRawField(fields, names...)
	return value
}

func rawStringField(fields map[string]json.RawMessage, names ...string) *string {
	valueJSON, ok := firstRawField(fields, names...)
	if !ok || len(bytes.TrimSpace(valueJSON)) == 0 || bytes.Equal(bytes.TrimSpace(valueJSON), []byte("null")) {
		return nil
	}
	var value string
	if err := json.Unmarshal(valueJSON, &value); err != nil {
		return nil
	}
	return &value
}

func rawIntField(fields map[string]json.RawMessage, names ...string) *int {
	valueJSON, ok := firstRawField(fields, names...)
	if !ok {
		return nil
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(valueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}
	switch value := decoded.(type) {
	case json.Number:
		integer, err := strconv.Atoi(value.String())
		if err != nil {
			return nil
		}
		return &integer
	case string:
		integer, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil {
			return nil
		}
		return &integer
	default:
		return nil
	}
}

func rawBoolField(fields map[string]json.RawMessage, names ...string) *bool {
	valueJSON, ok := firstRawField(fields, names...)
	if !ok {
		return nil
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(valueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}
	switch value := decoded.(type) {
	case bool:
		return &value
	case string:
		normalized := strings.ToLower(strings.TrimSpace(value))
		normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
		switch normalized {
		case "true", "yes", "found", "localized", "located", "present", "visible", "detected", "success", "succeeded":
			result := true
			return &result
		case "false", "no", "not_found", "missing", "absent", "not_present", "not_visible", "uncertain", "ambiguous", "failed":
			result := false
			return &result
		}
	default:
		return nil
	}
	return nil
}

func rawFloatField(fields map[string]json.RawMessage, names ...string) *float64 {
	valueJSON, ok := firstRawField(fields, names...)
	if !ok {
		return nil
	}
	var decoded any
	decoder := json.NewDecoder(bytes.NewReader(valueJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return nil
	}
	switch value := decoded.(type) {
	case json.Number:
		floatValue, err := strconv.ParseFloat(value.String(), 64)
		if err != nil {
			return nil
		}
		return &floatValue
	case string:
		floatValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return nil
		}
		return &floatValue
	default:
		return nil
	}
}

func trimStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func cloneRawMessage(value json.RawMessage) json.RawMessage {
	if value == nil {
		return nil
	}
	cloned := make([]byte, len(value))
	copy(cloned, value)
	return json.RawMessage(cloned)
}

func joinWholeSceneWarnings(warnings []string) *string {
	cleaned := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			cleaned = append(cleaned, warning)
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	joined := strings.Join(cleaned, "; ")
	return &joined
}
