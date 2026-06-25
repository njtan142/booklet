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

	"booklet/storage"

	"github.com/dslipak/pdf"
	"github.com/google/uuid"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
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

// CompileBooklet programmatically positions single-page PDFs onto a landscape canvas using pdfcpu
func CompileBooklet(ctx context.Context, dbPages []DBPageInfo, config BookletConfig) (string, error) {
	// Create a temp directory for downloaded single pages
	tempDir, err := os.MkdirTemp("", "booklet-compile-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	log.Printf("Compiling booklet using pdfcpu for %d pages (Signature size: %d)...", len(dbPages), config.SignatureSize)

	// Sort database pages sequentially by page number
	sort.Slice(dbPages, func(i, j int) bool {
		return dbPages[i].PageNumber < dbPages[j].PageNumber
	})

	// Download all required single-page PDF files locally in order
	var localPagePaths []string
	for _, dbPage := range dbPages {
		localPath := filepath.Join(tempDir, fmt.Sprintf("page_%d.pdf", dbPage.PageNumber))
		err := storage.DownloadFile(ctx, dbPage.StoragePath, localPath)
		if err != nil {
			return "", fmt.Errorf("failed to download page %d: %w", dbPage.PageNumber, err)
		}
		localPagePaths = append(localPagePaths, localPath)
	}

	if len(localPagePaths) == 0 {
		return "", fmt.Errorf("no pages to compile")
	}

	// Merge all single-page PDFs into a single sequential PDF file first using chunked merging.
	// This ensures the page tree depth remains shallow and does not exceed pdfcpu recursion limits.
	tempMergedPath, err := MergeFilesSafe(localPagePaths, tempDir)
	if err != nil {
		return "", fmt.Errorf("failed to merge single pages safely: %w", err)
	}

	// Configure paper size
	var paperSize string
	if strings.ToLower(config.PaperSize) == "letter" {
		paperSize = "Letter"
	} else {
		paperSize = "A4"
	}

	// Map signature size to folio size (number of sheets per signature)
	// pdfcpu groups signature pages as 4 * foliosize
	folioSize := config.SignatureSize / 4
	if folioSize <= 0 {
		folioSize = 1
	}

	// Build N-Up/Booklet configuration description string
	// pdfcpu takes margin in points
	desc := fmt.Sprintf("formsize:%s, margin:%.2f, multifolio:on, foliosize:%d", paperSize, config.Margin, folioSize)

	nup, err := api.PDFBookletConfig(2, desc, nil)
	if err != nil {
		return "", fmt.Errorf("failed to configure pdfcpu booklet: %w", err)
	}

	// Generate booklet PDF using pdfcpu booklet engine
	bookletID := uuid.New().String()
	localOutPath := filepath.Join(tempDir, fmt.Sprintf("booklet_%s.pdf", bookletID))

	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.Limits.MaxRecursionDepth = 10000
	err = api.BookletFile([]string{tempMergedPath}, localOutPath, nil, nup, conf)
	if err != nil {
		return "", fmt.Errorf("pdfcpu booklet generation failed: %w", err)
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
	log.Printf("[FilterBookletPages] Starting for booklet %s, filterType=%s, sheetRange=%s", bookletStoragePath, filterType, sheetRange)
	
	// Create a temp directory
	tempDir, err := os.MkdirTemp("", "booklet-filter-*")
	if err != nil {
		log.Printf("[FilterBookletPages] Error creating temp dir: %v", err)
		return "", fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer func() {
		log.Printf("[FilterBookletPages] Cleaning up temp dir: %s", tempDir)
		os.RemoveAll(tempDir)
	}()

	// Download original compiled booklet
	localBookletPath := filepath.Join(tempDir, "booklet.pdf")
	log.Printf("[FilterBookletPages] Downloading booklet from storage path: %s -> %s", bookletStoragePath, localBookletPath)
	err = storage.DownloadFile(ctx, bookletStoragePath, localBookletPath)
	if err != nil {
		log.Printf("[FilterBookletPages] Error downloading booklet: %v", err)
		return "", err
	}

	// Inspect booklet file size
	fi, err := os.Stat(localBookletPath)
	if err != nil {
		log.Printf("[FilterBookletPages] Error stating local booklet: %v", err)
		return "", fmt.Errorf("failed to stat local booklet: %w", err)
	}
	log.Printf("[FilterBookletPages] Local booklet size: %d bytes", fi.Size())

	// Open booklet to get page count using dslipak/pdf (pure Go reader, no strict validation)
	log.Printf("[FilterBookletPages] Parsing page count with dslipak/pdf reader...")
	fBooklet, err := os.Open(localBookletPath)
	if err != nil {
		log.Printf("[FilterBookletPages] Error opening local booklet for parsing: %v", err)
		return "", fmt.Errorf("failed to open booklet file: %w", err)
	}
	pdfReader, err := pdf.NewReader(fBooklet, fi.Size())
	if err != nil {
		fBooklet.Close()
		log.Printf("[FilterBookletPages] Error creating dslipak/pdf reader: %v", err)
		return "", fmt.Errorf("failed to parse booklet with pdf reader: %w", err)
	}
	totalBookletPages := pdfReader.NumPage()
	fBooklet.Close()

	log.Printf("[FilterBookletPages] Successfully read booklet page count using dslipak/pdf: %d pages", totalBookletPages)

	// Total sheets is totalBookletPages / 2
	totalSheets := totalBookletPages / 2
	log.Printf("[FilterBookletPages] Calculated total sheets: %d", totalSheets)

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
		log.Printf("[FilterBookletPages] Parsed sheet range: %s -> startSheet=%d, endSheet=%d", sheetRange, startSheet, endSheet)
	}

	// Validate sheet ranges
	if startSheet < 1 {
		startSheet = 1
	}
	if endSheet > totalSheets {
		endSheet = totalSheets
	}
	if startSheet > endSheet {
		log.Printf("[FilterBookletPages] Invalid sheet range computed: startSheet=%d > endSheet=%d", startSheet, endSheet)
		return "", fmt.Errorf("invalid sheet range: %s", sheetRange)
	}

	log.Printf("[FilterBookletPages] Final validated sheet range: startSheet=%d, endSheet=%d", startSheet, endSheet)

	// Collect the exact list of booklet page numbers (1-based) to extract
	var selectedPagesStr []string

	for sheetNum := startSheet; sheetNum <= endSheet; sheetNum++ {
		frontPageNum := 2*sheetNum - 1
		backPageNum := 2 * sheetNum

		switch strings.ToLower(filterType) {
		case "fronts":
			selectedPagesStr = append(selectedPagesStr, strconv.Itoa(frontPageNum))
		case "backs":
			selectedPagesStr = append(selectedPagesStr, strconv.Itoa(backPageNum))
		default:
			selectedPagesStr = append(selectedPagesStr, strconv.Itoa(frontPageNum), strconv.Itoa(backPageNum))
		}
	}

	log.Printf("[FilterBookletPages] Selected booklet page indices: %v", selectedPagesStr)

	if len(selectedPagesStr) == 0 {
		log.Printf("[FilterBookletPages] Error: no pages selected by filter")
		return "", fmt.Errorf("no pages selected by filter")
	}

	localFilteredPath := filepath.Join(tempDir, "filtered.pdf")

	log.Printf("[FilterBookletPages] Slicing pages using pdfcpu.CollectFile...")
	conf := model.NewDefaultConfiguration()
	conf.ValidationMode = model.ValidationRelaxed
	conf.Limits.MaxRecursionDepth = 10000 // Raise recursion limit for booklet page slicing/collection
	err = api.CollectFile(localBookletPath, localFilteredPath, selectedPagesStr, conf)
	if err != nil {
		log.Printf("[FilterBookletPages] pdfcpu collect failed: %v", err)
		return "", fmt.Errorf("pdfcpu collect failed: %w", err)
	}
	log.Printf("[FilterBookletPages] pdfcpu collect succeeded")

	// Upload filtered PDF to MinIO
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

// MapPagesToSheets maps a range of 1-based booklet PDF pages to the physical sheet range that contains them
func MapPagesToSheets(startPage int, endPage int) (int, int) {
	startSheet := (startPage + 1) / 2
	endSheet := (endPage + 1) / 2
	return startSheet, endSheet
}

