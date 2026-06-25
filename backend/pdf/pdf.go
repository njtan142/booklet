package pdf

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
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
	texts := p.Content().Text
	
	// Concatenate text elements
	for _, txt := range texts {
		textBuf.WriteString(txt.S)
		textBuf.WriteString(" ")
	}

	return strings.TrimSpace(textBuf.String()), width, height, nil
}

type DBPageInfo struct {
	PageNumber  int
	StoragePath string
	Width       float64
	Height      float64
}

// CompileBooklet programmatically positions single-page PDFs onto a landscape canvas
func CompileBooklet(ctx context.Context, dbPages []DBPageInfo, config BookletConfig) (string, error) {
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("Compiling booklet for %d pages (Signature size: %d)...", len(dbPages), config.SignatureSize)

	// Map pages by page number for easy lookup
	pagesMap := make(map[int]DBPageInfo)
	for _, p := range dbPages {
		pagesMap[p.PageNumber] = p
	}

	// Calculate booklet layout
	sheets := CalculateBookletLayout(len(dbPages), config.SignatureSize)

	// Download all required single-page PDF files locally
	localPagePaths := make(map[int]string)
	for pageNum, dbPage := range pagesMap {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", pageNum))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return "", fmt.Errorf("failed to download page %d: %w", pageNum, err)
		}
		localPagePaths[pageNum] = localPath
	}

	// Create new PDF document
	pdf := gopdf.GoPdf{}

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

	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: sheetWidth, H: sheetHeight}})


	// Calculate layout metrics
	// Margin is padding around the grid
	// Gutter is space between the two pages
	margin := config.Margin
	gutter := config.Gutter

	availWidth := sheetWidth - (2 * margin) - gutter
	slotWidth := availWidth / 2
	availHeight := sheetHeight - (2 * margin)

	log.Printf("Canvas size: %.2f x %.2f, Slot size: %.2f x %.2f", sheetWidth, sheetHeight, slotWidth, availHeight)

	// Draw sheets
	for _, sheet := range sheets {
		pdf.AddPage()

		// Helper function to draw page inside a slot (left or right)
		drawPageInSlot := func(pageNum int, isRightSlot bool) error {
			if pageNum == 0 {
				// Blank/padded page, don't draw anything
				return nil
			}

			localPath := localPagePaths[pageNum]
			dbPage := pagesMap[pageNum]

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
			tplID := pdf.ImportPage(localPath, 1, "/MediaBox")
			pdf.UseImportedTemplate(tplID, offsetX, offsetY, drawW, drawH)

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
	}

	// Write compiled PDF to local temp file
	bookletID := uuid.New().String()
	localOutPath := filepath.Join(tempDir, fmt.Sprintf("booklet_%s.pdf", bookletID))
	
	err = pdf.WritePdf(localOutPath)
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

// FilterBookletPages extracts a subset of pages from a compiled booklet (fronts-only, backs-only, or custom sheet range)
func FilterBookletPages(ctx context.Context, bookletStoragePath string, filterType string, sheetRange string) (string, error) {
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "booklet-filter-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Download original compiled booklet
	localBookletPath := filepath.Join(tempDir, "booklet.pdf")
	err = storage.DownloadFile(ctx, bookletStoragePath, localBookletPath)
	if err != nil {
		return "", err
	}

	// Get total pages of booklet
	totalBookletPages, err := api.PageCountFile(localBookletPath)
	if err != nil {
		return "", fmt.Errorf("failed to get booklet page count: %w", err)
	}

	// Total sheets is totalBookletPages / 2
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

	// Collect the exact list of booklet page numbers (1-based) to extract
	var selectedPages []string

	for sheetNum := startSheet; sheetNum <= endSheet; sheetNum++ {
		// Sheet 'sheetNum' corresponds to booklet pages:
		frontPageNum := 2*sheetNum - 1
		backPageNum := 2 * sheetNum

		switch strings.ToLower(filterType) {
		case "fronts":
			selectedPages = append(selectedPages, strconv.Itoa(frontPageNum))
		case "backs":
			selectedPages = append(selectedPages, strconv.Itoa(backPageNum))
		default:
			// Include both front and back
			selectedPages = append(selectedPages, strconv.Itoa(frontPageNum), strconv.Itoa(backPageNum))
		}
	}

	if len(selectedPages) == 0 {
		return "", fmt.Errorf("no pages selected by filter")
	}

	// Extract selected pages using pdfcpu Collect
	localFilteredPath := filepath.Join(tempDir, "filtered.pdf")
	err = api.CollectFile(localBookletPath, localFilteredPath, selectedPages, nil)
	if err != nil {
		return "", fmt.Errorf("pdfcpu collect failed: %w", err)
	}

	// Upload filtered PDF to MinIO (we store it under a temp path or stream it)
	// Since we want this to be stateless, we upload it with a unique key
	filteredID := uuid.New().String()
	storageKey := fmt.Sprintf("temp_filtered/%s.pdf", filteredID)
	err = storage.UploadFile(ctx, storageKey, localFilteredPath, "application/pdf")
	if err != nil {
		return "", fmt.Errorf("failed to upload filtered PDF: %w", err)
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

// MapPagesToSheets maps a range of 1-based original PDF booklet pages to the physical sheet range that contains them
func MapPagesToSheets(numPages int, signatureSize int, startPage int, endPage int) (int, int) {
	sheets := CalculateBookletLayout(numPages, signatureSize)
	totalSheets := len(sheets) / 2

	minSheet := totalSheets + 1
	maxSheet := 0

	for i := 0; i < totalSheets; i++ {
		front := sheets[2*i]
		back := sheets[2*i+1]

		pages := []int{front.LeftPage, front.RightPage, back.LeftPage, back.RightPage}
		containsRuined := false
		for _, p := range pages {
			if p >= startPage && p <= endPage {
				containsRuined = true
				break
			}
		}

		if containsRuined {
			sheetNum := i + 1
			if sheetNum < minSheet {
				minSheet = sheetNum
			}
			if sheetNum > maxSheet {
				maxSheet = sheetNum
			}
		}
	}

	if maxSheet == 0 {
		return 1, totalSheets
	}
	return minSheet, maxSheet
}

