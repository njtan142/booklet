package pdf

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"booklet/logger"
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

// GetPageCount returns the total page count of a PDF file
func GetPageCount(localPath string) (int, error) {
	return api.PageCountFile(localPath)
}

// SplitDocument splits the uploaded PDF into single-page PDFs, extracts text and page dimensions, and processes them incrementally
func SplitDocument(ctx context.Context, docID string, localPath string, onProgress func(current, total int, step string), onPage func(page PageInfo) error) error {
	// Create a temp directory for splits inside the parent directory of localPath
	// so that it gets cleaned up when the caller cleans up the parent directory.
	tempDir := filepath.Join(filepath.Dir(localPath), "split")
	if err := os.MkdirAll(tempDir, 0755); err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}

	logger.Logf(ctx, "Splitting document %s in %s...", localPath, tempDir)

	// 1. Get the total number of pages in the PDF file
	numPages, err := api.PageCountFile(localPath)
	if err != nil {
		return fmt.Errorf("failed to get page count: %w", err)
	}

	if onProgress != nil {
		onProgress(0, numPages, "splitting PDF")
	}

	// We disable object streams and xref streams to ensure compatibility with gofpdi.
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.WriteObjectStream = false
	conf.WriteXRefStream = false

	chunkSize := 100
	if envVal := os.Getenv("SPLIT_CHUNK_SIZE"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			chunkSize = val
		}
	}
	for startPage := 1; startPage <= numPages; startPage += chunkSize {
		endPage := startPage + chunkSize - 1
		if endPage > numPages {
			endPage = numPages
		}

		var pagesToExtract []string
		for p := startPage; p <= endPage; p++ {
			pagesToExtract = append(pagesToExtract, strconv.Itoa(p))
		}

		logger.Logf(ctx, "[SplitDocument] Splitting page range %d-%d of %d...", startPage, endPage, numPages)
		err = api.ExtractPagesFile(localPath, tempDir, pagesToExtract, conf)
		if err != nil {
			return fmt.Errorf("pdfcpu page extraction failed for range %d-%d: %w", startPage, endPage, err)
		}

		if onProgress != nil {
			onProgress(endPage, numPages, "splitting PDF")
		}
	}

	// Read files from temp directory
	files, err := os.ReadDir(tempDir)
	if err != nil {
		return fmt.Errorf("failed to read split dir: %w", err)
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
			logger.Logf(ctx, "Warning: failed to parse page number from filename %s: %v", f.Name(), err)
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

	numWorkers := 4 // parse up to 4 pages in parallel
	if envVal := os.Getenv("PARALLEL_PAGE_PARSERS"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			numWorkers = val
		}
	}

	type task struct {
		idx int
		sf  splitFile
	}
	taskChan := make(chan task, len(splitFiles))
	for idx, sf := range splitFiles {
		taskChan <- task{idx: idx, sf: sf}
	}
	close(taskChan)

	var wg sync.WaitGroup
	var errOnce sync.Once
	var firstErr error

	// We use atomic counter for progress tracking in onProgress
	var parsedCount int32

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range taskChan {
				// If another worker failed, abort early
				if firstErr != nil {
					return
				}

				// Extract page dimensions and text content
				text, width, height, err := processSinglePage(t.sf.path)
				if err != nil {
					logger.Logf(ctx, "Warning: failed to process page %d: %v. Using defaults.", t.sf.pageNum, err)
					// fallback default A4 portrait dimensions
					width = 595.28
					height = 841.89
					text = ""
				}

				page := PageInfo{
					PageNumber: t.sf.pageNum,
					Text:       text,
					Width:      width,
					Height:     height,
					LocalPath:  t.sf.path,
				}

				if onProgress != nil {
					completed := atomic.AddInt32(&parsedCount, 1)
					onProgress(int(completed), len(splitFiles), "parsing page text")
				}

				if err := onPage(page); err != nil {
					errOnce.Do(func() {
						firstErr = err
					})
					return
				}
			}
		}()
	}

	wg.Wait()
	if firstErr != nil {
		return firstErr
	}

	return nil
}

func processSinglePage(filePath string) (text string, width float64, height float64, err error) {
	type result struct {
		text   string
		width  float64
		height float64
		err    error
	}

	resChan := make(chan result, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				resChan <- result{
					err: fmt.Errorf("panic while processing page %s: %v", filePath, r),
				}
			}
		}()
		txt, w, h, e := processSinglePageInner(filePath)
		resChan <- result{text: txt, width: w, height: h, err: e}
	}()

	timeout := 5 * time.Second
	if envVal := os.Getenv("PAGE_PARSING_TIMEOUT_SECONDS"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			timeout = time.Duration(val) * time.Second
		}
	}

	select {
	case res := <-resChan:
		return res.text, res.width, res.height, res.err
	case <-time.After(timeout):
		log.Printf("[processSinglePage] WARNING: Timeout (%v) processing page %s. Skipping and treating as empty.", timeout, filePath)
		return "", 595.28, 841.89, fmt.Errorf("timeout processing page %s", filePath)
	}
}

func processSinglePageInner(filePath string) (text string, width float64, height float64, err error) {
	log.Printf("[processSinglePageInner] Starting processing for: %s", filePath)
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic while processing page %s: %v", filePath, r)
			log.Printf("[processSinglePage] Recovered from panic on %s: %v", filePath, r)
		}
	}()

	// 1. Open PDF to extract dimensions
	file, err := os.Open(filePath)
	if err != nil {
		log.Printf("[processSinglePage] Failed to open file %s: %v", filePath, err)
		return "", 0, 0, err
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		log.Printf("[processSinglePage] Failed to stat file %s: %v", filePath, err)
		return "", 0, 0, err
	}

	log.Printf("[processSinglePage] Creating new dslipak/pdf reader for %s", filePath)
	r, err := pdf.NewReader(file, fileInfo.Size())
	if err != nil {
		log.Printf("[processSinglePage] Failed to create reader for %s: %v", filePath, err)
		return "", 0, 0, err
	}
	log.Printf("[processSinglePage] Reader created successfully for %s. NumPages: %d", filePath, r.NumPage())

	if r.NumPage() < 1 {
		log.Printf("[processSinglePage] Empty PDF page for %s", filePath)
		return "", 0, 0, fmt.Errorf("empty PDF page")
	}

	log.Printf("[processSinglePage] Getting page 1 for %s", filePath)
	p := r.Page(1)
	if p.V.IsNull() {
		log.Printf("[processSinglePage] Invalid page object for %s", filePath)
		return "", 0, 0, fmt.Errorf("invalid page object")
	}

	contentsVal := p.V.Key("Contents")


	// Extract page dimensions directly from page object MediaBox or CropBox in memory
	box := p.V.Key("CropBox")
	if box.IsNull() {
		box = p.V.Key("MediaBox")
	}
	if !box.IsNull() && box.Len() >= 4 {
		llx := box.Index(0).Float64()
		lly := box.Index(1).Float64()
		urx := box.Index(2).Float64()
		ury := box.Index(3).Float64()
		width = urx - llx
		height = ury - lly
	}

	// Fallback to standard A4 if dimensions are invalid or zero
	if width <= 0 || height <= 0 {
		width = 595.28
		height = 841.89
	}
	log.Printf("[processSinglePage] Dimensions extracted: %.2f x %.2f for %s", width, height, filePath)

	// 2. Extract plain text
	if !contentsVal.IsNull() {
		var manualBuf strings.Builder
		content := p.Content()
		for _, txt := range content.Text {
			manualBuf.WriteString(txt.S)
			manualBuf.WriteString(" ")
		}
		text = strings.TrimSpace(manualBuf.String())
		log.Printf("[processSinglePage] Text extracted successfully, length: %d for %s", len(text), filePath)
	} else {
		log.Printf("[processSinglePage] Contents key is null, skipping text extraction for %s", filePath)
	}

	log.Printf("[processSinglePage] Finished processing for: %s", filePath)
	return text, width, height, nil
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

	logger.Logf(ctx, "Compiling booklet using gopdf for %d pages (Signature size: %d, Margin: %.2f, Gutter: %.2f)...", len(dbPages), config.SignatureSize, config.Margin, config.Gutter)

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
		// Letter Landscape: 8.5 x 11 in
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else if strings.ToLower(config.PaperSize) == "folio" {
		// Folio Landscape: 8.5 x 13 in
		sheetWidth = 936.00
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

// CompileBookletSlice compiles only specific physical sheets and/or sides (fronts/backs) of a booklet directly from single pages to a local file
func CompileBookletSlice(ctx context.Context, dbPages []DBPageInfo, config BookletConfig, filterType string, sheetRange string, localOutPath string) error {
	logger.Logf(ctx, "[CompileBookletSlice] Compiling slice for signatureSize=%d, filterType=%s, sheetRange=%s to %s", config.SignatureSize, filterType, sheetRange, localOutPath)
	
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-slice-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
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
		return fmt.Errorf("invalid sheet range: %s", sheetRange)
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
		return fmt.Errorf("no sheets selected by filter")
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
			return fmt.Errorf("failed to download page %d: %w", pageNum, err)
		}
	}

	// Create new PDF document
	pdfDoc := gopdf.GoPdf{}

	// Configure paper size
	var sheetWidth, sheetHeight float64
	if strings.ToLower(config.PaperSize) == "letter" {
		sheetWidth = 792.00
		sheetHeight = 612.00
	} else if strings.ToLower(config.PaperSize) == "folio" {
		// Folio Landscape: 8.5 x 13 in
		sheetWidth = 936.00
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
			return err
		}

		// Draw Right Page
		if err := drawPageInSlot(sheet.RightPage, true); err != nil {
			return err
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

	// Write compiled PDF slice directly to destination
	err = pdfDoc.WritePdf(localOutPath)
	if err != nil {
		return fmt.Errorf("failed to write compiled PDF slice: %w", err)
	}

	return nil
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

