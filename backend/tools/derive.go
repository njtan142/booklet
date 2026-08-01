package tools

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"booklet/db"
	"booklet/embeddings"
	"booklet/jobs"
	"booklet/logger"
	"booklet/pdf"
	"booklet/storage"

	"github.com/google/uuid"
)

// deriveNamespace makes derived document ids a deterministic function of the
// job that produced them.
//
// Retries are the reason. A job that fails after inserting its output would,
// with a random id, leave one orphaned document per attempt in the user's
// library. Deriving the id from the job id instead means attempt two writes to
// the same row, so a retry repairs its predecessor rather than duplicating it.
var deriveNamespace = uuid.MustParse("6f9a1c0e-8b3d-4a2f-9c5e-1d7b3a4f6e20")

// DeriveInput is one downloaded tool input.
type DeriveInput struct {
	DocumentID string
	Name       string
	Kind       string
	MimeType   string
	TotalPages int
	// LocalPath is the input file on this worker's disk, already downloaded
	// from MinIO. Tools operate on files, never on object storage directly.
	LocalPath string
}

// PageSource identifies where one page of the derived document came from.
//
// Tools return these in derived page order, so the slice index plus one is the
// derived page number. Returning them at all is what marks a tool as
// text-preserving: the pages are copied from the parents with their embeddings
// instead of being re-extracted and re-embedded.
type PageSource struct {
	DocumentID string
	Page       int
}

// DeriveResult is what a tool hands back to RunDerive.
type DeriveResult struct {
	// OutputPath is the produced file on local disk.
	OutputPath string
	// Name is the derived document's library name.
	Name string
	// Kind and MimeType default to a PDF when left empty.
	Kind     string
	MimeType string
	// PageSources maps every derived page to its parent page, in derived page
	// order. Leave it nil when the tool changes the text, which makes RunDerive
	// run the full extract and embed pipeline instead.
	PageSources []PageSource
}

// DeriveFunc is the tool-specific part of a derivation: inputs in, one output
// file out. Everything around it — download, upload, the documents row, page
// splitting, embeddings, job output linkage, cleanup — is RunDerive's job.
type DeriveFunc func(ctx context.Context, workDir string, inputs []DeriveInput, reporter *jobs.Reporter) (*DeriveResult, error)

// RunDerive executes a tool that produces one derived document.
//
// The source document is never mutated: every tool writes a new row carrying
// derived_from_document_id and derived_via_tool, so the library keeps the whole
// lineage and a bad conversion costs nothing.
func RunDerive(ctx context.Context, job *jobs.Job, reporter *jobs.Reporter, fn DeriveFunc) error {
	workDir, err := os.MkdirTemp("", "booklet-tool-"+job.ToolSlug+"-*")
	if err != nil {
		return fmt.Errorf("failed to create work dir: %w", err)
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			logger.Logf(ctx, "Warning: failed to clean up work dir %s: %v", workDir, err)
		}
	}()

	// The API enforces arity before enqueueing, so an empty input list means a
	// job row was written by something that bypassed that check. Fail rather
	// than panic on inputs[0] inside a worker goroutine.
	if len(job.InputDocumentIDs) == 0 {
		return jobs.Permanent(fmt.Errorf("job %s has no input documents", job.ID))
	}

	reporter.Progress(0, "downloading inputs")
	inputs, err := loadInputs(ctx, workDir, job.InputDocumentIDs)
	if err != nil {
		return err
	}

	reporter.Progress(0, "running "+job.ToolSlug)
	result, err := fn(ctx, workDir, inputs, reporter)
	if err != nil {
		return err
	}
	if result == nil || result.OutputPath == "" {
		return jobs.Permanent(fmt.Errorf("%s produced no output file", job.ToolSlug))
	}
	if result.Kind == "" {
		result.Kind = "pdf"
	}
	if result.MimeType == "" {
		result.MimeType = "application/pdf"
	}

	docID := DerivedDocumentID(job.ID, 0)
	if err := writeDerivedDocument(ctx, job, docID, inputs, result, reporter); err != nil {
		// Leaving the row in 'processing' would make it indistinguishable from a
		// job still running, and the document reaper would only notice 15
		// minutes later.
		markDocumentFailed(ctx, docID)
		return err
	}

	if err := jobs.AddOutput(ctx, job.ID, docID, 0); err != nil {
		return fmt.Errorf("failed to link output document %s to job %s: %w", docID, job.ID, err)
	}

	logger.Logf(ctx, "Job %s (%s) produced document %s", job.ID, job.ToolSlug, docID)
	return nil
}

// DerivedDocumentID returns the stable id for a job's output at position.
func DerivedDocumentID(jobID string, position int) string {
	return uuid.NewSHA1(deriveNamespace, []byte(fmt.Sprintf("%s#%d", jobID, position))).String()
}

// DerivedName labels a derived document after its parent and the tool that
// produced it, e.g. "Report.pdf" rotated becomes "Report (rotated 90).pdf".
//
// The extension is stripped before the suffix so a chain of tools does not
// produce "Report.pdf (rotated 90) (compressed).pdf".
func DerivedName(parentName, suffix string) string {
	ext := filepath.Ext(parentName)
	base := parentName[:len(parentName)-len(ext)]
	if base == "" {
		base = "document"
	}
	if ext == "" {
		ext = ".pdf"
	}
	return fmt.Sprintf("%s (%s)%s", base, suffix, ext)
}

// loadInputs downloads every input to the work directory, preserving the order
// the caller selected them in. Merge depends on that order being the user's.
func loadInputs(ctx context.Context, workDir string, documentIDs []string) ([]DeriveInput, error) {
	inputs := make([]DeriveInput, 0, len(documentIDs))

	for i, docID := range documentIDs {
		var in DeriveInput
		var storagePath sql.NullString
		var totalPages sql.NullInt64

		err := db.DB.QueryRowContext(ctx, `
			SELECT id::text, name, kind, mime_type, total_pages, original_storage_path
			FROM documents WHERE id = $1`, docID).
			Scan(&in.DocumentID, &in.Name, &in.Kind, &in.MimeType, &totalPages, &storagePath)
		if err == sql.ErrNoRows {
			// The document was deleted between enqueue and claim; retrying will
			// not bring it back.
			return nil, jobs.Permanent(fmt.Errorf("input document %s no longer exists", docID))
		}
		if err != nil {
			return nil, fmt.Errorf("failed to load input document %s: %w", docID, err)
		}
		if !storagePath.Valid || storagePath.String == "" {
			return nil, jobs.Permanent(fmt.Errorf("input document %s has no stored original file", docID))
		}
		in.TotalPages = int(totalPages.Int64)

		// The index prefix keeps two inputs with the same name from colliding
		// on disk, which Merge would otherwise hit constantly.
		in.LocalPath = filepath.Join(workDir, fmt.Sprintf("in_%d_%s", i, filepath.Base(storagePath.String)))
		if err := storage.DownloadFile(ctx, storagePath.String, in.LocalPath); err != nil {
			return nil, fmt.Errorf("failed to download input document %s: %w", docID, err)
		}

		inputs = append(inputs, in)
	}

	return inputs, nil
}

// writeDerivedDocument uploads the output, records it, and populates its pages.
func writeDerivedDocument(ctx context.Context, job *jobs.Job, docID string, inputs []DeriveInput, result *DeriveResult, reporter *jobs.Reporter) error {
	groupID, err := db.PrimaryGroupID(job.UserID)
	if err != nil {
		return fmt.Errorf("failed to resolve primary group for %s: %w", job.UserID, err)
	}

	originalKey := fmt.Sprintf("documents/%s/original.pdf", docID)
	if err := storage.UploadFile(ctx, originalKey, result.OutputPath, result.MimeType); err != nil {
		return fmt.Errorf("failed to upload derived output: %w", err)
	}

	// The first input is the lineage parent. For Merge that is a simplification,
	// but derived_from_document_id is a single column and the full input list
	// stays recoverable through job_inputs.
	parentID := inputs[0].DocumentID

	// ON CONFLICT makes a retry overwrite its own previous attempt rather than
	// failing on the deterministic primary key.
	if _, err := db.DB.ExecContext(ctx, `
		INSERT INTO documents (id, name, total_pages, split_pages, parsed_pages, status,
		                       original_storage_path, owner_id, group_id, mode, kind, mime_type,
		                       original_filename, derived_from_document_id, derived_via_tool,
		                       derived_via_job_id, created_at, updated_at)
		VALUES ($1, $2, 0, 0, 0, 'processing', $3, $4, $5, $6, $7, $8, $2, $9, $10, $11,
		        CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			status = 'processing',
			split_pages = 0,
			parsed_pages = 0,
			original_storage_path = EXCLUDED.original_storage_path,
			updated_at = CURRENT_TIMESTAMP`,
		docID, result.Name, originalKey, job.UserID, groupID, db.ModeDefault,
		result.Kind, result.MimeType, parentID, job.ToolSlug, job.ID); err != nil {
		return fmt.Errorf("failed to insert derived document: %w", err)
	}

	// A retry re-splits from scratch, so any pages left by the failed attempt
	// would collide with the unique (document_id, page_number) index.
	if _, err := db.DB.ExecContext(ctx, `DELETE FROM document_pages WHERE document_id = $1`, docID); err != nil {
		return fmt.Errorf("failed to clear stale pages of %s: %w", docID, err)
	}

	pages, err := splitAndUpload(ctx, docID, result.OutputPath, reporter)
	if err != nil {
		return err
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE documents SET total_pages = $1, split_pages = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, len(pages), docID); err != nil {
		return fmt.Errorf("failed to record page count for %s: %w", docID, err)
	}

	if result.PageSources != nil {
		if err := copyPagesFromParents(ctx, docID, pages, result.PageSources); err != nil {
			return err
		}
	} else if err := embedPages(ctx, docID, pages, reporter); err != nil {
		return err
	}

	if _, err := db.DB.ExecContext(ctx, `
		UPDATE documents SET status = 'ready', parsed_pages = $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2`, len(pages), docID); err != nil {
		return fmt.Errorf("failed to mark %s ready: %w", docID, err)
	}

	return nil
}

// splitPage is one page of the derived document after splitting.
type splitPage struct {
	Number      int
	Text        string
	Width       float64
	Height      float64
	StoragePath string
}

// splitAndUpload splits the derived file into single-page PDFs and stores each
// one, keeping the library invariant that every page has its own object.
func splitAndUpload(ctx context.Context, docID, localPath string, reporter *jobs.Reporter) ([]splitPage, error) {
	var mu sync.Mutex
	pages := []splitPage{}

	// SplitDocument fans onPage out across several goroutines, so both the
	// slice and the progress counter need the lock.
	err := pdf.SplitDocument(ctx, docID, localPath,
		func(current, total int, step string) {
			reporter.SetTotal(total)
			reporter.Progress(current, step)
		},
		func(page pdf.PageInfo) error {
			objectName := fmt.Sprintf("documents/%s/pages/page_%d.pdf", docID, page.PageNumber)
			if err := storage.UploadFile(ctx, objectName, page.LocalPath, "application/pdf"); err != nil {
				return fmt.Errorf("failed to upload page %d: %w", page.PageNumber, err)
			}

			mu.Lock()
			pages = append(pages, splitPage{
				Number:      page.PageNumber,
				Text:        page.Text,
				Width:       page.Width,
				Height:      page.Height,
				StoragePath: objectName,
			})
			mu.Unlock()
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("failed to split derived document: %w", err)
	}

	sort.Slice(pages, func(i, j int) bool { return pages[i].Number < pages[j].Number })
	return pages, nil
}

// copyPagesFromParents carries the parents' text and embeddings onto the
// derived pages, which is the whole reason a text-preserving tool is cheap.
func copyPagesFromParents(ctx context.Context, docID string, pages []splitPage, sources []PageSource) error {
	sourceList, err := buildSourcePages(pages, sources)
	if err != nil {
		return jobs.Permanent(err)
	}

	for _, src := range sourceList {
		parentPages, err := ParentPageCount(ctx, src.DocumentID)
		if err != nil {
			return fmt.Errorf("failed to count pages of parent %s: %w", src.DocumentID, err)
		}
		// A parent with no page rows never finished its own pipeline, so there
		// is nothing to copy and the derived document would be unsearchable.
		if parentPages == 0 {
			return jobs.Permanent(fmt.Errorf("parent document %s has no indexed pages to copy", src.DocumentID))
		}
		for _, m := range src.Mapping {
			if m.SourcePage < 1 || m.SourcePage > parentPages {
				return jobs.Permanent(fmt.Errorf(
					"page map references page %d of parent %s, which has %d page(s)",
					m.SourcePage, src.DocumentID, parentPages))
			}
		}
	}

	tx, err := db.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := CopyPageEmbeddingsFrom(ctx, tx, docID, sourceList); err != nil {
		return err
	}

	return tx.Commit()
}

// buildSourcePages groups a tool's flat page map by parent document.
//
// The derived pages' dimensions come from the freshly split output rather than
// from the parent, so a tool that genuinely changes page geometry needs no
// special case here. Rotation is not such a tool: pdfcpu sets /Rotate and
// leaves the MediaBox alone, so a rotated page measures the same as its parent
// (see pdf.TestRotateDoesNotSwapReportedDimensions).
//
// Parents are returned in first-appearance order, which keeps Merge's inserts
// in the user's chosen input order.
func buildSourcePages(pages []splitPage, sources []PageSource) ([]SourcePages, error) {
	if len(pages) != len(sources) {
		return nil, fmt.Errorf("derived document has %d page(s) but the tool mapped %d; the page map is wrong",
			len(pages), len(sources))
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("derived document has no pages to map")
	}

	byParent := map[string][]PageMap{}
	order := []string{}
	for i, page := range pages {
		src := sources[i]
		if src.DocumentID == "" {
			return nil, fmt.Errorf("page map entry for derived page %d names no parent document", page.Number)
		}
		if _, seen := byParent[src.DocumentID]; !seen {
			order = append(order, src.DocumentID)
		}
		byParent[src.DocumentID] = append(byParent[src.DocumentID], PageMap{
			SourcePage:  src.Page,
			DerivedPage: page.Number,
			StoragePath: page.StoragePath,
			Width:       page.Width,
			Height:      page.Height,
		})
	}

	out := make([]SourcePages, 0, len(order))
	for _, parentID := range order {
		out = append(out, SourcePages{DocumentID: parentID, Mapping: byParent[parentID]})
	}
	return out, nil
}

// embedPages runs the full pipeline for tools that change the text.
func embedPages(ctx context.Context, docID string, pages []splitPage, reporter *jobs.Reporter) error {
	reporter.SetTotal(len(pages))

	for i, page := range pages {
		vec, err := embeddings.ActiveEmbedder.Embed(ctx, page.Text)
		if err != nil {
			// Matches the upload pipeline: a page with a zero vector is still
			// readable and printable, so a flaky embedder must not lose the
			// document.
			logger.Logf(ctx, "Warning: failed to embed page %d of %s: %v", page.Number, docID, err)
			vec = make([]float32, embeddings.ActiveEmbedder.Dimension())
		}

		if _, err := db.DB.ExecContext(ctx, `
			INSERT INTO document_pages (id, document_id, page_number, text_content, embedding,
			                            storage_path, width, height, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, CURRENT_TIMESTAMP)`,
			uuid.New(), docID, page.Number, page.Text, db.Float32ArrayToString(vec),
			page.StoragePath, page.Width, page.Height); err != nil {
			return fmt.Errorf("failed to save page %d of %s: %w", page.Number, docID, err)
		}

		reporter.Progress(i+1, "embedding pages")
	}

	return nil
}

func markDocumentFailed(ctx context.Context, docID string) {
	if _, err := db.DB.ExecContext(ctx, `
		UPDATE documents SET status = 'failed', updated_at = CURRENT_TIMESTAMP
		WHERE id = $1`, docID); err != nil {
		logger.Logf(ctx, "Warning: failed to mark derived document %s failed: %v", docID, err)
	}
}
