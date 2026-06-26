package pdf

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"booklet/storage"

	"github.com/dslipak/pdf"
	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/signintech/gopdf"
)

type PageInfo struct {
	PageNumber int
	Text       string
	Width      float64
	Height     float64
	LocalPath  string
}

type BookletConfig struct {
	Margin        float64 // Margin in PDF points (1/72 inch)
	Gutter        float64 // Gutter spacing between pages in PDF points
	PaperSize     string  // "A4" or "Letter"
	SignatureSize int     // e.g. 4, 8, 16
	Guides        bool    // Draw folding/cutting guides
}

// SplitDocument splits the uploaded PDF into single-page PDFs, extracts text and page dimensions
func SplitDocument(ctx context.Context, docID string, localPath string) ([]PageInfo, error) {
	// Create a temp directory for splits inside the parent directory of localPath
	// so that it gets cleaned up when the caller cleans up the parent directory.
	tempDir := filepath.Join(filepath.Dir(localPath), "split")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	log.Printf("Splitting document %s in %s...", localPath, tempDir)

	// pdfcpu Split splits the file into parts of size 1 page.
	// We disable object streams and xref streams to ensure compatibility with gofpdi.
	conf := model.NewDefaultConfiguration()
	conf.WriteObjectStream = false
	conf.WriteXRefStream = false

	err := api.SplitFile(localPath, tempDir, 1, conf)
	if err != nil {
		return nil, fmt.Errorf("pdfcpu split failed: %w", err)
	}

	// Read files from temp directory
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read split dir: %w", err)
	}

	// We sort the files by page number to process in order.
	// pdfcpu names split pages like "input_1.pdf", "input_2.pdf", etc.
	// Let's parse the page number from filename.
	type splitFile struct {
		pageNum int
		path    string
	}
	var splitFiles []splitFile

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".pdf") {
			continue
		}
		
		name := strings.TrimSuffix(f.Name(), ".pdf")
		parts := strings.Split(name, "_")
		if len(parts) < 2 {
			continue
		}
		numStr := parts[len(parts)-1]
		pageNum, err := strconv.Atoi(numStr)
		if err != nil {
			log.Printf("Warning: failed to parse page number from filename %s: %v", f.Name(), err)
			continue
		}

		splitFiles = append(splitFiles, splitFile{
			pageNum: pageNum,
			path:    filepath.Join(tempDir, f.Name()),
		})
	}

	// Perform a simple bubble/insertion sort by page number
	for i := 0; i < len(splitFiles); i++ {
		for j := i + 1; j < len(splitFiles); j++ {
			if splitFiles[i].pageNum > splitFiles[j].pageNum {
				splitFiles[i], splitFiles[j] = splitFiles[j], splitFiles[i]
			}
		}
	}

	var pages []PageInfo
	for _, sf := range splitFiles {
		// Extract page dimensions and text content
		text, width, height, err := processSinglePage(sf.path)
		if err != nil {
			log.Printf("Warning: failed to process page %d: %v. Using defaults.", sf.pageNum, err)
			// fallback default A4 portrait dimensions
			width = 595.28
			height = 841.89
			text = ""
		}

		pages = append(pages, PageInfo{
			PageNumber: sf.pageNum,
			Text:       text,
			Width:      width,
			Height:     height,
			LocalPath:  sf.path,
		})
	}

	return pages, nil
}

func processSinglePage(filePath string) (text string, width float64, height float64, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while processing page %s: %v", filePath, r)
		}
	}()

	// 1. Open PDF to extract dimensions
	// We can use dslipak/pdf reader
	file, err := os.Open(filePath)
	if err != nil {
		return "", 0, 0, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", 0, 0, err
	}

	r, err := pdf.NewReader(file, fileInfo.Size())
	if err != nil {
		return "", 0, 0, err
	}

	if r.NumPage() < 1 {
		return "", 0, 0, fmt.Errorf("empty PDF page")
	}

	p := r.Page(1)
	if p.V.IsNull() {
		return "", 0, 0, fmt.Errorf("invalid page object")
	}

	dims, err := api.PageDimsFile(filePath)
	if err == nil && len(dims) > 0 {
		width = dims[0].Width
		height = dims[0].Height
	} else {
		// Fallback to standard A4
		width = 595.28
		height = 841.89
	}

	// 2. Extract plain text
	var textBuf strings.Builder
	plainTextReader, err := r.GetPlainText()
	if err == nil {
		if _, err := io.Copy(&textBuf, plainTextReader); err == nil {
			text = textBuf.String()
		}
	}
	if text == "" {
		// Fallback to manual concatenation if GetPlainText fails or is empty
		var manualBuf strings.Builder
		texts := p.Content().Text
		for _, txt := range texts {
			manualBuf.WriteString(txt.S)
			manualBuf.WriteString(" ")
		}
		text = manualBuf.String()
	}

	return strings.TrimSpace(text), width, height, nil
}

type DBPageInfo struct {
	PageNumber  int
	StoragePath string
	Width       float64
	Height      float64
}

// CompileBooklet programmatically positions single-page PDFs onto a landscape canvas
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



// CompileBooklet programmatically positions single-page PDFs onto a landscape canvas using gopdf
func CompileBooklet(ctx context.Context, dbPages []DBPageInfo, config BookletConfig) (string, error) {
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("Compiling booklet using gopdf for %d pages (Signature size: %d, Margin: %.2f, Gutter: %.2f)...", len(dbPages), config.SignatureSize, config.Margin, config.Gutter)

	// Sort database pages sequentially by page number
	sort.Slice(dbPages, func(i, j int) bool {
		return dbPages[i].PageNumber < dbPages[j].PageNumber
	})

	// Download all required single-page PDF files locally in order
	var localPagePaths []string
	pagesMap := make(map[int]DBPageInfo)
	for _, dbPage := range dbPages {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", dbPage.PageNumber))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return "", fmt.Errorf("failed to download page %d: %w", dbPage.PageNumber, err)
		}
		localPagePaths = append(localPagePaths, localPath)
		pagesMap[dbPage.PageNumber] = dbPage
	}

	if len(localPagePaths) == 0 {
		return "", fmt.Errorf("no pages to compile")
	}

	// Calculate booklet imposition layout (sheets of front/back sides)
	sheets := CalculateBookletLayout(len(dbPages), config.SignatureSize)

	// Create new PDF document
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(config.PaperSize) == "letter" {
		// Letter Landscape: 792.00 x 612.00
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else {
		// Default A4 Landscape: 841.89 x 595.28
		sheetWidth = 841.89
		sheetHeight = 595.28
	}

	pdfDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})

	// Calculate layout metrics
	margin := config.Margin
	gutter := config.Gutter

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	// Draw sheets
	for _, sheet := range sheets {
		pdfDoc.AddPage()

		// Helper function to draw page inside a slot (left or right)
		drawPageInSlot := func(pageNum int, isRightSlot bool) error {
			if pageNum == 0 {
				// Blank/padded page, don't draw anything
				return nil
			}

			// Since pageNum is 1-based original page index
			dbPage, exists := pagesMap[pageNum]
			if !exists {
				return nil // Page out of scope
			}
			localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))

			// Calculate slot bounds
			var slotX float64
			if isRightSlot {
				slotX = margin + slotWidth + gutter
			} else {
				slotX = margin
			}
			slotY := margin

			// Calculate scale factors to fit page within slot (keep aspect ratio)
			scaleW := slotWidth / dbPage.Width
			scaleH := availHeight / dbPage.Height
			scale := math.Min(scaleW, scaleH)

			drawW := dbPage.Width * scale
			drawH := dbPage.Height * scale

			// Center page inside the slot
			offsetX := slotX + (slotWidth-drawW)/2
			offsetY := slotY + (availHeight-drawH)/2

			// Import and place template
			tplID := pdfDoc.ImportPage(localPath, 1, "/MediaBox")
			pdfDoc.UseImportedTemplate(tplID, offsetX, offsetY, drawW, drawH)

			return nil
		}

		// Draw Left Page
		if err := drawPageInSlot(sheet.LeftPage, false); err != nil {
			return "", err
		}

		// Draw Right Page
		if err := drawPageInSlot(sheet.RightPage, true); err != nil {
			return "", err
		}

		// Draw folding guidelines if enabled
		if config.Guides {
			pdfDoc.SetLineWidth(0.5)
			pdfDoc.SetStrokeColor(180, 180, 180)
			pdfDoc.SetLineType("dashed")
			pdfDoc.Line(sheetWidth/2, 0, sheetWidth/2, sheetHeight)
			pdfDoc.SetLineType("solid")
		}
	}

	// Write compiled PDF to local temp file
	bookletID := uuid.New().String()
	localOutPath := filepath.Join(tempDir, fmt.Sprintf("booklet_%s.pdf", bookletID))
	
	err = pdfDoc.WritePdf(localOutPath)
	if err != nil {
		return "", fmt.Errorf("failed to write compiled PDF: %w", err)
	}

	// Upload compiled booklet to MinIO
	storageKey := fmt.Sprintf("booklets/%s.pdf", bookletID)
	err = storage.UploadFile(ctx, storageKey, localOutPath, "application/pdf")
	if err != nil {
		return "", fmt.Errorf("failed to upload booklet to MinIO: %w", err)
	}

	return storageKey, nil
}

// CompileBookletSlice compiles only specific physical sheets and/or sides (fronts/backs) of a booklet directly from single pages
func CompileBookletSlice(ctx context.Context, dbPages []DBPageInfo, config BookletConfig, filterType string, sheetRange string) (string, error) {
	log.Printf("[CompileBookletSlice] Compiling slice for signatureSize=%d, filterType=%s, sheetRange=%s", config.SignatureSize, filterType, sheetRange)
	
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-slice-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Sort database pages sequentially by page number
	sort.Slice(dbPages, func(i, j int) bool {
		return dbPages[i].PageNumber < dbPages[j].PageNumber
	})

	// Calculate all booklet sheets first
	allSides := CalculateBookletLayout(len(dbPages), config.SignatureSize)
	totalBookletPages := len(allSides)
	totalSheets := totalBookletPages / 2

	// Determine start and end sheets (1-based indices)
	startSheet := 1
	endSheet := totalSheets

	if sheetRange != "" {
		parts := strings.Split(sheetRange, "-")
		if len(parts) == 1 {
			if s, err := strconv.Atoi(parts[0]); err == nil {
				startSheet = s
				endSheet = s
			}
		} else if len(parts) == 2 {
			if s, err := strconv.Atoi(parts[0]); err == nil {
				startSheet = s
			}
			if e, err := strconv.Atoi(parts[1]); err == nil {
				endSheet = e
			}
		}
	}

	// Validate sheet ranges
	if startSheet < 1 {
		startSheet = 1
	}
	if endSheet > totalSheets {
		endSheet = totalSheets
	}
	if startSheet > endSheet {
		return "", fmt.Errorf("invalid sheet range: %s", sheetRange)
	}

	// Select the sides we want to render
	var selectedSides []SheetSide
	for sheetNum := startSheet; sheetNum <= endSheet; sheetNum++ {
		frontIdx := 2 * (sheetNum - 1)
		backIdx := 2*(sheetNum - 1) + 1

		switch strings.ToLower(filterType) {
		case "fronts":
			if frontIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[frontIdx])
			}
		case "backs":
			if backIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[backIdx])
			}
		default:
			if frontIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[frontIdx])
			}
			if backIdx < len(allSides) {
				selectedSides = append(selectedSides, allSides[backIdx])
			}
		}
	}

	if len(selectedSides) == 0 {
		return "", fmt.Errorf("no sheets selected by filter")
	}

	// Find the exact pages we need to download
	pagesMap := make(map[int]DBPageInfo)
	for _, dbPage := range dbPages {
		pagesMap[dbPage.PageNumber] = dbPage
	}

	neededPages := make(map[int]bool)
	for _, side := range selectedSides {
		if side.LeftPage > 0 {
			neededPages[side.LeftPage] = true
		}
		if side.RightPage > 0 {
			neededPages[side.RightPage] = true
		}
	}

	// Download only the needed single pages
	for pageNum := range neededPages {
		dbPage, exists := pagesMap[pageNum]
		if !exists {
			continue
		}
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return "", fmt.Errorf("failed to download page %d: %w", pageNum, err)
		}
	}

	// Create new PDF document
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(config.PaperSize) == "letter" {
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else {
		sheetWidth = 841.89
		sheetHeight = 595.28
	}

	pdfDoc.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})

	// Calculate layout metrics
	margin := config.Margin
	gutter := config.Gutter

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	// Draw selected sheet sides
	for _, sheet := range selectedSides {
		pdfDoc.AddPage()

		// Helper function to draw page inside a slot (left or right)
		drawPageInSlot := func(pageNum int, isRightSlot bool) error {
			if pageNum == 0 {
				return nil
			}

			dbPage, exists := pagesMap[pageNum]
			if !exists {
				return nil
			}
			localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))

			// Calculate slot bounds
			var slotX float64
			if isRightSlot {
				slotX = margin + slotWidth + gutter
			} else {
				slotX = margin
			}
			slotY := margin

			// Calculate scale factors
			scaleW := slotWidth / dbPage.Width
			scaleH := availHeight / dbPage.Height
			scale := math.Min(scaleW, scaleH)

			drawW := dbPage.Width * scale
			drawH := dbPage.Height * scale

			// Center page inside the slot
			offsetX := slotX + (slotWidth-drawW)/2
			offsetY := slotY + (availHeight-drawH)/2

			// Import and place template
			tplID := pdfDoc.ImportPage(localPath, 1, "/MediaBox")
			pdfDoc.UseImportedTemplate(tplID, offsetX, offsetY, drawW, drawH)

			return nil
		}

		// Draw Left Page
		if err := drawPageInSlot(sheet.LeftPage, false); err != nil {
			return "", err
		}

		// Draw Right Page
		if err := drawPageInSlot(sheet.RightPage, true); err != nil {
			return "", err
		}

		// Draw folding guidelines if enabled
		if config.Guides {
			pdfDoc.SetLineWidth(0.5)
			pdfDoc.SetStrokeColor(180, 180, 180)
			pdfDoc.SetLineType("dashed")
			pdfDoc.Line(sheetWidth/2, 0, sheetWidth/2, sheetHeight)
			pdfDoc.SetLineType("solid")
		}
	}

	// Write compiled PDF slice to local temp file
	sliceID := uuid.New().String()
	localOutPath := filepath.Join(tempDir, fmt.Sprintf("slice_%s.pdf", sliceID))
	
	err = pdfDoc.WritePdf(localOutPath)
	if err != nil {
		return "", fmt.Errorf("failed to write compiled PDF slice: %w", err)
	}

	// Upload compiled slice to MinIO
	storageKey := fmt.Sprintf("temp_filtered/%s.pdf", sliceID)
	err = storage.UploadFile(ctx, storageKey, localOutPath, "application/pdf")
	if err != nil {
		return "", fmt.Errorf("failed to upload booklet slice to MinIO: %w", err)
	}

	return storageKey, nil
}

type SheetSide struct {
	LeftPage  int // 1-based page number, 0 for blank
	RightPage int // 1-based page number, 0 for blank
}

// CalculateBookletLayout calculates the sequence of pages for a custom duplex booklet
func CalculateBookletLayout(numPages int, signatureSize int) []SheetSide {
	S := signatureSize
	if S <= 0 || S%4 != 0 {
		S = 4 // Fallback to 4
	}

	N := numPages
	// M must be smallest multiple of S greater than or equal to N
	M := int(math.Ceil(float64(N)/float64(S))) * S

	var sheets []SheetSide
	numSignatures := M / S
	for sig := 0; sig < numSignatures; sig++ {
		basePage := sig * S // 0-based offset
		numSigSheets := S / 4

		for k := 0; k < numSigSheets; k++ {
			// Calculate the 1-based page index within this signature
			p1 := basePage + (2*k + 1)
			p2 := basePage + (2*k + 2)
			p3 := basePage + (S - 2*k - 1)
			p4 := basePage + (S - 2*k)

			// Apply blank page filtering (if index > N, it's a padded blank page)
			getP := func(idx int) int {
				if idx > N {
					return 0
				}
				return idx
			}

			// Front side of sheet: Left = p4, Right = p1
			sheets = append(sheets, SheetSide{
				LeftPage:  getP(p4),
				RightPage: getP(p1),
			})

			// Back side of sheet: Left = p2, Right = p3
			sheets = append(sheets, SheetSide{
				LeftPage:  getP(p2),
				RightPage: getP(p3),
			})
		}
	}
	return sheets
}

// MapPagesToSheets maps a range of 1-based booklet PDF pages to the physical sheet range that contains them
func MapPagesToSheets(startPage int, endPage int) (int, int) {
	startSheet := (startPage + 1) / 2
	endSheet := (endPage + 1) / 2
	return startSheet, endSheet
}

