package handlers

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (w *WholeSceneAnalysisWorker) enrichWholeSceneLocalizationDiagnostics(scanID string, sources wholeSceneScanImageSources, diagnostics []wholeSceneLocalizationDiagnostic) {
	for index := range diagnostics {
		diagnostics[index].ScanID = scanID
		if diagnostics[index].SourceImageIndex == nil || !wholeSceneBBoxComplete(diagnostics[index].diagnosticBoundingBox) {
			continue
		}
		source, warning, ok := sources.resolve(wholeSceneParsedAppearance{
			SourceImageIndex: diagnostics[index].SourceImageIndex,
			BoundingBox:      diagnostics[index].diagnosticBoundingBox,
		})
		if !ok {
			if diagnostics[index].Reason == "accepted" {
				diagnostics[index].Accepted = false
				diagnostics[index].Reason = warning
			}
			continue
		}
		if warning != "" && diagnostics[index].Reason == "accepted" {
			diagnostics[index].Reason = warning
		}
		rect, rectWarning := w.wholeSceneDiagnosticPixelRect(source, diagnostics[index].diagnosticBoundingBox)
		if rectWarning != "" {
			if diagnostics[index].Reason == "accepted" {
				diagnostics[index].Accepted = false
				diagnostics[index].Reason = rectWarning
			}
			continue
		}
		diagnostics[index].FinalPixelRect = &wholeSceneDiagnosticPixelRect{
			X:      rect.Min.X,
			Y:      rect.Min.Y,
			Width:  rect.Dx(),
			Height: rect.Dy(),
		}
	}
}

func (w *WholeSceneAnalysisWorker) wholeSceneDiagnosticPixelRect(source wholeSceneScanImageSource, bbox wholeSceneParsedBoundingBox) (image.Rectangle, string) {
	sourcePath := filepath.Clean(strings.TrimSpace(source.FilePath))
	if sourcePath == "" {
		return image.Rectangle{}, "source image path was empty"
	}
	if !isSafeManagedPath(sourcePath, w.cfg.SafeRoots) {
		return image.Rectangle{}, "source image path was outside managed storage"
	}

	sourceFile, err := os.Open(sourcePath)
	if err != nil {
		return image.Rectangle{}, "source image could not be opened"
	}
	defer sourceFile.Close()

	config, _, err := image.DecodeConfig(sourceFile)
	if err != nil {
		return image.Rectangle{}, "source image dimensions could not be decoded"
	}
	rect, err := wholeScenePixelCropRect(image.Rect(0, 0, config.Width, config.Height), bbox)
	if err != nil {
		return image.Rectangle{}, err.Error()
	}
	return rect, ""
}

func (w *WholeSceneAnalysisWorker) writeWholeSceneLocalizationDiagnostics(scanID string, sources wholeSceneScanImageSources, diagnostics []wholeSceneLocalizationDiagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	root := w.wholeSceneDiagnosticsRoot()
	if root == "" {
		return
	}
	dir := filepath.Join(root, scanID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Printf("failed to prepare Whole Scene diagnostics dir scan_id=%s: %v", scanID, err)
		return
	}

	if err := w.writeWholeSceneLocalizationOverlays(dir, sources, diagnostics); err != nil {
		log.Printf("failed to write Whole Scene localization overlays scan_id=%s: %v", scanID, err)
	}

	payload, err := json.MarshalIndent(map[string]any{
		"scan_id":   scanID,
		"decisions": diagnostics,
	}, "", "  ")
	if err != nil {
		log.Printf("failed to marshal Whole Scene localization diagnostics scan_id=%s: %v", scanID, err)
		return
	}
	if err := os.WriteFile(filepath.Join(dir, "localization-decisions.json"), payload, 0644); err != nil {
		log.Printf("failed to write Whole Scene localization diagnostics scan_id=%s: %v", scanID, err)
	}
}

func (w *WholeSceneAnalysisWorker) wholeSceneDiagnosticsRoot() string {
	if root := strings.TrimSpace(w.cfg.DiagnosticsDir); root != "" {
		return filepath.Clean(root)
	}
	originalsDir := filepath.Clean(strings.TrimSpace(w.cfg.OriginalsDir))
	if originalsDir == "." || originalsDir == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(filepath.Dir(originalsDir)), "debug", "whole-scene")
}

func (w *WholeSceneAnalysisWorker) writeWholeSceneLocalizationOverlays(dir string, sources wholeSceneScanImageSources, diagnostics []wholeSceneLocalizationDiagnostic) error {
	bySource := make(map[int][]int)
	for index := range diagnostics {
		if diagnostics[index].SourceImageIndex == nil || diagnostics[index].FinalPixelRect == nil {
			continue
		}
		sourceIndex := *diagnostics[index].SourceImageIndex
		if _, ok := sources.byIndex[sourceIndex]; ok {
			bySource[sourceIndex] = append(bySource[sourceIndex], index)
		}
	}
	if len(bySource) == 0 {
		return nil
	}

	sourceIndexes := make([]int, 0, len(bySource))
	for sourceIndex := range bySource {
		sourceIndexes = append(sourceIndexes, sourceIndex)
	}
	sort.Ints(sourceIndexes)

	for _, sourceIndex := range sourceIndexes {
		source := sources.byIndex[sourceIndex]
		sourcePath := filepath.Clean(strings.TrimSpace(source.FilePath))
		if sourcePath == "" || !isSafeManagedPath(sourcePath, w.cfg.SafeRoots) {
			continue
		}
		file, err := os.Open(sourcePath)
		if err != nil {
			return err
		}
		src, _, err := image.Decode(file)
		_ = file.Close()
		if err != nil {
			return err
		}

		bounds := src.Bounds()
		canvas := image.NewRGBA(bounds)
		draw.Draw(canvas, bounds, src, bounds.Min, draw.Src)
		for overlayNumber, diagnosticIndex := range bySource[sourceIndex] {
			number := overlayNumber + 1
			diagnostics[diagnosticIndex].OverlayNumber = &number
			overlayPath := filepath.Join(dir, fmt.Sprintf("source-%d-overlay.jpg", sourceIndex))
			diagnostics[diagnosticIndex].OverlayPath = overlayPath
			rect := diagnostics[diagnosticIndex].FinalPixelRect
			box := image.Rect(rect.X, rect.Y, rect.X+rect.Width, rect.Y+rect.Height)
			boxColor := color.RGBA{R: 230, G: 70, B: 55, A: 255}
			if diagnostics[diagnosticIndex].Accepted {
				boxColor = color.RGBA{R: 70, G: 220, B: 120, A: 255}
			}
			drawDiagnosticRect(canvas, box, boxColor)
			drawDiagnosticLabel(canvas, box.Min.X+4, box.Min.Y+4, strconv.Itoa(number), boxColor)
		}

		outputPath := filepath.Join(dir, fmt.Sprintf("source-%d-overlay.jpg", sourceIndex))
		out, err := os.Create(outputPath)
		if err != nil {
			return err
		}
		err = jpeg.Encode(out, canvas, &jpeg.Options{Quality: 88})
		closeErr := out.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func drawDiagnosticRect(canvas *image.RGBA, rect image.Rectangle, c color.RGBA) {
	rect = rect.Intersect(canvas.Bounds())
	if rect.Empty() {
		return
	}
	for inset := 0; inset < 3; inset++ {
		r := image.Rect(rect.Min.X+inset, rect.Min.Y+inset, rect.Max.X-inset, rect.Max.Y-inset)
		if r.Empty() {
			continue
		}
		for x := r.Min.X; x < r.Max.X; x++ {
			canvas.Set(x, r.Min.Y, c)
			canvas.Set(x, r.Max.Y-1, c)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			canvas.Set(r.Min.X, y, c)
			canvas.Set(r.Max.X-1, y, c)
		}
	}
}

func drawDiagnosticLabel(canvas *image.RGBA, x int, y int, text string, c color.RGBA) {
	padding := 2
	width := len(text)*4 + padding*2
	height := 7 + padding*2
	bg := color.RGBA{R: 0, G: 0, B: 0, A: 220}
	draw.Draw(canvas, image.Rect(x, y, x+width, y+height).Intersect(canvas.Bounds()), &image.Uniform{C: bg}, image.Point{}, draw.Src)
	cursorX := x + padding
	for _, r := range text {
		drawDiagnosticDigit(canvas, cursorX, y+padding, r, c)
		cursorX += 4
	}
}

func drawDiagnosticDigit(canvas *image.RGBA, x int, y int, r rune, c color.RGBA) {
	glyphs := map[rune][]string{
		'0': {"111", "101", "101", "101", "111"},
		'1': {"010", "110", "010", "010", "111"},
		'2': {"111", "001", "111", "100", "111"},
		'3': {"111", "001", "111", "001", "111"},
		'4': {"101", "101", "111", "001", "001"},
		'5': {"111", "100", "111", "001", "111"},
		'6': {"111", "100", "111", "101", "111"},
		'7': {"111", "001", "010", "010", "010"},
		'8': {"111", "101", "111", "101", "111"},
		'9': {"111", "101", "111", "001", "111"},
	}
	rows, ok := glyphs[r]
	if !ok {
		return
	}
	for rowIndex, row := range rows {
		for colIndex, pixel := range row {
			if pixel != '1' {
				continue
			}
			px := x + colIndex
			py := y + rowIndex
			if image.Pt(px, py).In(canvas.Bounds()) {
				canvas.Set(px, py, c)
			}
		}
	}
}
