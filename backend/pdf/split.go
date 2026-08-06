package pdf

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"booklet/logger"

	"github.com/dslipak/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

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

				// Immediately remove single page file after processing to free disk space and OS file cache
				_ = os.Remove(t.sf.path)
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
	timeout := 5 * time.Second
	if envVal := os.Getenv("PAGE_PARSING_TIMEOUT_SECONDS"); envVal != "" {
		if val, err := strconv.Atoi(envVal); err == nil && val > 0 {
			timeout = time.Duration(val) * time.Second
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

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
		txt, w, h, e := processSinglePageInner(ctx, filePath)
		resChan <- result{text: txt, width: w, height: h, err: e}
	}()

	select {
	case res := <-resChan:
		return res.text, res.width, res.height, res.err
	case <-ctx.Done():
		log.Printf("[processSinglePage] WARNING: Timeout (%v) processing page %s. Skipping and treating as empty.", timeout, filePath)
		return "", 595.28, 841.89, fmt.Errorf("timeout processing page %s", filePath)
	}
}

func processSinglePageInner(ctx context.Context, filePath string) (text string, width float64, height float64, err error) {
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

	// 2. Extract plain text using pdftotext tool
	if !contentsVal.IsNull() {
		cmd := exec.CommandContext(ctx, "pdftotext", filePath, "-")
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			text = strings.TrimSpace(out.String())
			log.Printf("[processSinglePage] Text extracted successfully using pdftotext, length: %d for %s", len(text), filePath)
		} else {
			log.Printf("[processSinglePage] Warning: pdftotext failed for %s: %v. Using empty text.", filePath, err)
			text = ""
		}
	} else {
		log.Printf("[processSinglePage] Contents key is null, skipping text extraction for %s", filePath)
	}

	log.Printf("[processSinglePage] Finished processing for: %s", filePath)
	return text, width, height, nil
}
