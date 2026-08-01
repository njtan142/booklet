package tools

import (
	"context"
	"database/sql"
	"fmt"

	"booklet/db"
	"booklet/logger"

	"github.com/google/uuid"
)

// PageMap links one page of a derived document back to the parent page it was
// produced from.
//
// It is what lets a text-preserving tool skip the embedding pipeline entirely.
// Rotating a 500-page document changes no glyph, so re-extracting and re-
// embedding every page would cost 500 Ollama round trips to reproduce vectors
// the parent already has.
type PageMap struct {
	// SourcePage is the 1-based page number in the parent document.
	SourcePage int
	// DerivedPage is the 1-based page number in the derived document.
	DerivedPage int
	// StoragePath is the derived page's own single-page PDF in MinIO. The
	// library invariant is that every page row points at its own object, so a
	// copied row must not reuse the parent's path.
	StoragePath string
	// Width and Height are the derived page's dimensions in points, as measured
	// from the freshly split output. Pass zero to inherit the parent's values.
	//
	// Note that a pdfcpu rotation does not change these: it sets the page's
	// /Rotate entry and leaves the MediaBox alone, so a rotated page reports the
	// same width and height while still displaying rotated.
	Width  float64
	Height float64
}

// CopyPageEmbeddings inserts document_pages rows for the derived document,
// carrying the parent's text and embedding vector through the mapping.
//
// The copy is an INSERT ... SELECT per page rather than a read followed by a
// write, so the embedding vector never crosses the wire: pgvector values are
// large, and a 500-page copy would otherwise ship megabytes into Go only to
// send them straight back.
//
// Every mapped source page must exist in the parent. A silently skipped page
// would leave the derived document searchable but with a hole in it, which is
// far harder to notice than a failed job.
func CopyPageEmbeddings(ctx context.Context, tx *sql.Tx, srcDocID, dstDocID string, mapping []PageMap) error {
	if len(mapping) == 0 {
		return fmt.Errorf("refusing to copy embeddings with an empty page map for document %s", dstDocID)
	}

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO document_pages (id, document_id, page_number, text_content, embedding,
		                            storage_path, width, height, created_at)
		SELECT $1, $2, $3, src.text_content, src.embedding, $4,
		       COALESCE(NULLIF($5::double precision, 0), src.width),
		       COALESCE(NULLIF($6::double precision, 0), src.height),
		       CURRENT_TIMESTAMP
		FROM document_pages src
		WHERE src.document_id = $7 AND src.page_number = $8`)
	if err != nil {
		return fmt.Errorf("failed to prepare page copy: %w", err)
	}
	defer stmt.Close()

	for _, m := range mapping {
		if m.StoragePath == "" {
			return fmt.Errorf("page map entry for derived page %d has no storage path", m.DerivedPage)
		}

		res, err := stmt.ExecContext(ctx, uuid.New(), dstDocID, m.DerivedPage, m.StoragePath,
			m.Width, m.Height, srcDocID, m.SourcePage)
		if err != nil {
			return fmt.Errorf("failed to copy page %d of %s to page %d of %s: %w",
				m.SourcePage, srcDocID, m.DerivedPage, dstDocID, err)
		}

		// Zero rows means the parent has no such page. That is a bug in the
		// tool's page map, not a transient failure, so fail loudly instead of
		// producing a document with missing pages.
		if n, err := res.RowsAffected(); err == nil && n == 0 {
			return fmt.Errorf("parent document %s has no page %d to copy to derived page %d",
				srcDocID, m.SourcePage, m.DerivedPage)
		}
	}

	logger.Logf(ctx, "Copied %d page embedding(s) from %s to %s", len(mapping), srcDocID, dstDocID)
	return nil
}

// SourcePages is one parent's contribution to a multi-parent derived document.
type SourcePages struct {
	DocumentID string
	Mapping    []PageMap
}

// CopyPageEmbeddingsFrom copies pages from several parents into one derived
// document, which is what Merge needs: each input contributes a contiguous run
// of pages, offset by everything before it.
//
// Parents are processed in the order given, and every mapping's DerivedPage is
// expected to already be absolute in the derived document.
func CopyPageEmbeddingsFrom(ctx context.Context, tx *sql.Tx, dstDocID string, sources []SourcePages) error {
	if len(sources) == 0 {
		return fmt.Errorf("refusing to copy embeddings with no source documents for %s", dstDocID)
	}
	for _, src := range sources {
		if err := CopyPageEmbeddings(ctx, tx, src.DocumentID, dstDocID, src.Mapping); err != nil {
			return err
		}
	}
	return nil
}

// ParentPageCount returns how many page rows the parent has, so a tool can
// validate its page map before writing anything.
func ParentPageCount(ctx context.Context, docID string) (int, error) {
	var n int
	err := db.DB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM document_pages WHERE document_id = $1`, docID).Scan(&n)
	return n, err
}
