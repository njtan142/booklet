package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var DB *sql.DB

// InitDB initializes connection and runs schema migrations
func InitDB() error {
	connStr := os.Getenv("DATABASE_URL")
	if connStr == "" {
		// Fallback default
		connStr = "postgres://postgres:postgres@localhost:5432/booklet?sslmode=disable"
	}

	var db *sql.DB
	var err error

	// Retry database connection on startup (crucial for docker-compose synchronization)
	for i := 1; i <= 10; i++ {
		log.Printf("Connecting to Postgres (attempt %d/10)...", i)
		db, err = sql.Open("postgres", connStr)
		if err == nil {
			err = db.Ping()
		}
		if err == nil {
			break
		}
		log.Printf("Postgres is not ready yet: %v. Retrying in 3 seconds...", err)
		time.Sleep(3 * time.Second)
	}

	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}

	DB = db
	log.Println("Database connection established.")

	if err := runMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

func runMigrations() error {
	// Enable pgvector extension
	log.Println("Enabling pg_vector extension...")
	if _, err := DB.Exec("CREATE EXTENSION IF NOT EXISTS vector;"); err != nil {
		return fmt.Errorf("failed to enable vector extension: %w", err)
	}

	// 1. Users Table
	log.Println("Creating users table...")
	_, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			email TEXT UNIQUE NOT NULL,
			name TEXT,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	// 2. Documents Table
	log.Println("Creating documents table...")
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS documents (
			id UUID PRIMARY KEY,
			name TEXT NOT NULL,
			total_pages INT NOT NULL,
			split_pages INT DEFAULT 0,
			parsed_pages INT DEFAULT 0,
			status TEXT NOT NULL,
			is_dismissed BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	// Add is_dismissed column if it doesn't exist for compatibility
	_, _ = DB.Exec(`ALTER TABLE documents ADD COLUMN IF NOT EXISTS is_dismissed BOOLEAN DEFAULT FALSE;`)
	_, _ = DB.Exec(`ALTER TABLE documents ADD COLUMN IF NOT EXISTS split_pages INT DEFAULT 0;`)
	_, _ = DB.Exec(`ALTER TABLE documents ADD COLUMN IF NOT EXISTS parsed_pages INT DEFAULT 0;`)
	_, _ = DB.Exec(`ALTER TABLE documents ADD COLUMN IF NOT EXISTS original_storage_path TEXT;`)

	// 3. Document Pages Table (using 384 dimensions for all-minilm embeddings by default)
	log.Println("Creating document_pages table...")
	dim := os.Getenv("EMBEDDING_DIMENSION")
	if dim == "" {
		dim = "384" // default to 384 for all-minilm, use 768 for nomic-embed-text
	}
	createPagesTableSQL := fmt.Sprintf(`
		CREATE TABLE IF NOT EXISTS document_pages (
			id UUID PRIMARY KEY,
			document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			page_number INT NOT NULL,
			text_content TEXT NOT NULL,
			embedding vector(%s),
			storage_path TEXT NOT NULL,
			width DOUBLE PRECISION NOT NULL,
			height DOUBLE PRECISION NOT NULL,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`, dim)
	
	if _, err = DB.Exec(createPagesTableSQL); err != nil {
		return err
	}

	// Add unique index on document_id + page_number
	_, _ = DB.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_doc_pages_num ON document_pages(document_id, page_number);
	`)

	// Create HNSW index for vector searches (using cosine distance ops)
	log.Println("Creating vector search index...")
	_, _ = DB.Exec(`
		CREATE INDEX IF NOT EXISTS idx_doc_pages_embedding ON document_pages USING hnsw (embedding vector_cosine_ops);
	`)

	// 4. Compiled Booklets Table
	log.Println("Creating compiled_booklets table...")
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS compiled_booklets (
			id UUID PRIMARY KEY,
			document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			status TEXT NOT NULL,
			storage_path TEXT,
			config_margin DOUBLE PRECISION NOT NULL,
			config_gutter DOUBLE PRECISION NOT NULL,
			config_paper_size TEXT NOT NULL,
			config_signature_size INT NOT NULL,
			config_guides BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return err
	}

	// Add config_guides column if it does not exist in case table was created earlier
	_, err = DB.Exec(`
		ALTER TABLE compiled_booklets ADD COLUMN IF NOT EXISTS config_guides BOOLEAN NOT NULL DEFAULT FALSE;
	`)
	if err != nil {
		return err
	}

	// 4b. Booklet Print Progress Table
	log.Println("Creating booklet_print_progress table...")
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS booklet_print_progress (
			booklet_id UUID PRIMARY KEY REFERENCES compiled_booklets(id) ON DELETE CASCADE,
			batch_size INT NOT NULL DEFAULT 10,
			completed_batches TEXT NOT NULL DEFAULT '{}',
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create booklet_print_progress table: %w", err)
	}

	// 5. SMTP Config Table
	log.Println("Creating smtp_config table...")
	_, err = DB.Exec(`
		CREATE TABLE IF NOT EXISTS smtp_config (
			id TEXT PRIMARY KEY DEFAULT 'global',
			host TEXT NOT NULL,
			port INT NOT NULL,
			username TEXT NOT NULL,
			password TEXT NOT NULL,
			encryption TEXT NOT NULL,
			from_email TEXT NOT NULL,
			from_name TEXT,
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create smtp_config table: %w", err)
	}

	log.Println("Database migrations applied successfully.")
	return nil
}

// Float32ArrayToString converts a slice of floats to pgvector string format (e.g. "[0.1,0.2,0.3]")
func Float32ArrayToString(slice []float32) string {
	var strVals []string
	for _, v := range slice {
		strVals = append(strVals, fmt.Sprintf("%g", v))
	}
	return "[" + strings.Join(strVals, ",") + "]"
}

// TODO: Expose this function as a secured administrative API route (e.g., POST /api/admin/clean-stale-processes)
// triggered by an external cron/scheduler. The endpoint should require API key authentication (with admin key rotation).
//
// FailStaleProcessingDocuments marks all documents in 'processing' or 'queued' status and compiled booklets in 'compiling' status as 'failed' if they are older than 15 minutes.
func FailStaleProcessingDocuments() error {
	log.Println("Cleaning up stale background processes (older than 15 minutes) from database...")
	
	// Fail stale documents
	res, err := DB.Exec(`
		UPDATE documents 
		SET status = 'failed', updated_at = CURRENT_TIMESTAMP 
		WHERE (status = 'processing' OR status = 'queued')
		  AND updated_at < CURRENT_TIMESTAMP - INTERVAL '15 minutes'
	`)
	if err != nil {
		return fmt.Errorf("failed to clean up stale documents: %w", err)
	}
	docCount, _ := res.RowsAffected()
	if docCount > 0 {
		log.Printf("Marked %d stale processing documents as failed.", docCount)
	}

	// Fail stale compiled booklets
	res, err = DB.Exec(`
		UPDATE compiled_booklets 
		SET status = 'failed' 
		WHERE status = 'compiling'
		  AND created_at < CURRENT_TIMESTAMP - INTERVAL '15 minutes'
	`)
	if err != nil {
		return fmt.Errorf("failed to clean up stale compiled booklets: %w", err)
	}
	bookletCount, _ := res.RowsAffected()
	if bookletCount > 0 {
		log.Printf("Marked %d stale compiling booklets as failed.", bookletCount)
	}

	return nil
}

