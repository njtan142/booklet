package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"booklet/logger"

	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/signintech/gopdf"
)

// MergeFilesSafe merges a list of PDF files by chunking them to keep the page tree depth low.
func MergeFilesSafe(files []string, tempDir string) (string, error) {
	if len(files) == 0 {
		return "", fmt.Errorf("no files to merge")
	}
	if len(files) == 1 {
		return files[0], nil
	}

	// Merge in chunks of 8 to prevent deep nesting of page trees.
	const chunkSize = 8
	var currentLevel []string = files

	for len(currentLevel) > 1 {
		var nextLevel []string
		for i := 0; i < len(currentLevel); i += chunkSize {
			end := i + chunkSize
			if end > len(currentLevel) {
				end = len(currentLevel)
			}
			chunk := currentLevel[i:end]
			if len(chunk) == 1 {
				nextLevel = append(nextLevel, chunk[0])
				continue
			}

			mergedPath := filepath.Join(tempDir, fmt.Sprintf("chunk_%s.pdf", uuid.New().String()))
			conf := model.NewDefaultConfiguration()
			conf.ValidationMode = model.ValidationRelaxed
			err := api.MergeCreateFile(chunk, mergedPath, false, conf)
			if err != nil {
				return "", fmt.Errorf("failed to merge chunk: %w", err)
			}
			nextLevel = append(nextLevel, mergedPath)
		}
		currentLevel = nextLevel
	}

	return currentLevel[0], nil
}

// RenderPageToPNG renders the first page of a PDF file to a PNG image using pdftoppm.
func RenderPageToPNG(ctx context.Context, pdfPath string, outPNGPath string) error {
	// We run pdftoppm -png -r 150 -f 1 -l 1 <pdfPath> <tempPrefix>
	tempPrefix := pdfPath + ".tmpimg"
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", "150", "-f", "1", "-l", "1", pdfPath, tempPrefix)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("pdftoppm failed (stderr: %s): %w", stderr.String(), err)
	}

	// pdftoppm appends "-1.png" since we rendered page 1
	generatedPath := tempPrefix + "-1.png"
	defer os.Remove(generatedPath)

	// Move the generated file to the desired output path
	content, err := os.ReadFile(generatedPath)
	if err != nil {
		return fmt.Errorf("failed to read generated PNG file: %w", err)
	}

	if err := os.WriteFile(outPNGPath, content, 0644); err != nil {
		return fmt.Errorf("failed to write output PNG file: %w", err)
	}

	return nil
}

// DrawPageInSlotSafe attempts to import a PDF page using gofpdi. If importing panics or fails,
// it falls back to rendering the page to a PNG using pdftoppm and draws it as an image.
func DrawPageInSlotSafe(ctx context.Context, pdfDoc *gopdf.GoPdf, localPath string, tempDir string, pageNum int, offsetX, offsetY, drawW, drawH float64) error {
	var tplID int
	var importErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				importErr = fmt.Errorf("panic during ImportPage: %v", r)
			}
		}()
		tplID = pdfDoc.ImportPage(localPath, 1, "/MediaBox")
	}()

	if importErr != nil {
		logger.Logf(ctx, "[DrawPageInSlotSafe] Recovery: ImportPage failed/panicked for page %d: %v. Falling back to image rendering.", pageNum, importErr)
		pngPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.png", pageNum))
		if err := RenderPageToPNG(ctx, localPath, pngPath); err != nil {
			return fmt.Errorf("failed to render page %d to image fallback: %w", pageNum, err)
		}
		
		logger.Logf(ctx, "[DrawPageInSlotSafe] Drawing page %d image fallback at (%.2f, %.2f) size (%.2f, %.2f)", pageNum, offsetX, offsetY, drawW, drawH)
		if err := pdfDoc.Image(pngPath, offsetX, offsetY, &gopdf.Rect{W: drawW, H: drawH}); err != nil {
			return fmt.Errorf("failed to draw page %d image fallback: %w", pageNum, err)
		}
		return nil
	}

	logger.Logf(ctx, "[DrawPageInSlotSafe] Successfully imported template for page %d (tplID=%d)", pageNum, tplID)
	pdfDoc.UseImportedTemplate(tplID, offsetX, offsetY, drawW, drawH)
	logger.Logf(ctx, "[DrawPageInSlotSafe] Placed template for page %d inside slot", pageNum)
	return nil
}

// OptimizePagePDF optimizes a single page PDF file in place to prevent gofpdi importer crashes/panics.
func OptimizePagePDF(ctx context.Context, localPath string) {
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.WriteObjectStream = false
	conf.WriteXRefStream = false

	tempOptPath := localPath + ".opt.pdf"
	if err := api.OptimizeFile(localPath, tempOptPath, conf); err != nil {
		logger.Logf(ctx, "Warning: failed to optimize PDF %s: %v. Proceeding with original file.", localPath, err)
		_ = os.Remove(tempOptPath)
		return
	}

	// Replace original file with the optimized one
	if err := os.Remove(localPath); err != nil {
		logger.Logf(ctx, "Warning: failed to remove original PDF %s: %v. Proceeding with original file.", localPath, err)
		_ = os.Remove(tempOptPath)
		return
	}
	if err := os.Rename(tempOptPath, localPath); err != nil {
		logger.Logf(ctx, "Warning: failed to rename optimized PDF %s: %v. Proceeding with original file.", localPath, err)
		return
	}
}
