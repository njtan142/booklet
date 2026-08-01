package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
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
			completed_sheets TEXT NOT NULL DEFAULT '{}',
			updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`)
	if err != nil {
		return fmt.Errorf("failed to create booklet_print_progress table: %w", err)
	}

	// Add completed_sheets column if it does not exist in case table was created earlier
	_, err = DB.Exec(`
		ALTER TABLE booklet_print_progress ADD COLUMN IF NOT EXISTS completed_sheets TEXT NOT NULL DEFAULT '{}';
	`)
	if err != nil {
		return fmt.Errorf("failed to add completed_sheets column: %w", err)
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

	// 6. Permission model: groups, membership, document ownership and mode bits
	if err := migratePermissions(); err != nil {
		return err
	}

	// 7. Job queue tables (consumed by the tool worker)
	if err := migrateJobQueue(); err != nil {
		return err
	}

	// 8. Idempotent backfill of ownership for pre-permission rows
	if err := backfillOwnership(); err != nil {
		return err
	}

	// 9. Ledger for one-shot data migrations
	if err := ensureSchemaMigrations(); err != nil {
		return err
	}

	// 10. One-shot grant of the execute bit to pre-execute default modes
	if err := migrateExecuteBit(); err != nil {
		return err
	}

	log.Println("Database migrations applied successfully.")
	return nil
}

// Mode bit constants mirroring Unix rwx triples.
//
// The execute bit is what permits deriving a new document from an existing one
// (POST /api/tools/jobs checks PermRead|PermExecute on every input), so an
// owner without it cannot run a single tool on their own upload. Both defaults
// therefore set x wherever they already set r+w.
const (
	// ModeDefault is 0o744: owner rwx, group r--, other r--.
	ModeDefault = 484
	// ModeLegacy is 0o774: owner rwx, group rwx, other r--.
	// Used for the backfill so existing shared documents stay writable and
	// runnable by the legacy group, which is how pre-permission users reach
	// documents owned by the system user.
	ModeLegacy = 508
)

// modeDefaultPre is the pre-execute-bit value of ModeDefault (0o644) and
// modeLegacyPre that of ModeLegacy (0o664). migrateExecuteBit rewrites rows
// still carrying them; nothing else should reference these.
const (
	modeDefaultPre = 420
	modeLegacyPre  = 436
)

// SystemUserID owns every document created before the permission model existed.
const SystemUserID = "system"

// migrationExecuteBit names the one-shot data migration in migrateExecuteBit.
const migrationExecuteBit = "documents_execute_bit_2026_08"

// ensureSchemaMigrations creates the ledger for migrations that must run once
// rather than on every boot. Everything else in this file is idempotent by
// construction (IF NOT EXISTS, or a WHERE that self-limits), so this table
// exists only for data migrations that cannot tell "never applied" from "applied
// and then deliberately undone by a user".
func ensureSchemaMigrations() error {
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("failed to create schema_migrations table: %w", err)
	}
	return nil
}

// migrationApplied reports whether the named one-shot migration already ran.
func migrationApplied(name string) (bool, error) {
	var exists bool
	if err := DB.QueryRow(
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE name = $1)`, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to read schema_migrations for %s: %w", name, err)
	}
	return exists, nil
}

// markMigrationApplied records a one-shot migration as done. ON CONFLICT keeps
// a concurrent second instance from failing on the primary key.
func markMigrationApplied(name string) error {
	if _, err := DB.Exec(`
		INSERT INTO schema_migrations (name) VALUES ($1)
		ON CONFLICT (name) DO NOTHING;
	`, name); err != nil {
		return fmt.Errorf("failed to record migration %s: %w", name, err)
	}
	return nil
}

// LegacyGroupName is the group that all pre-permission documents belong to.
const LegacyGroupName = "legacy"

// migratePermissions creates the group tables and adds ownership, lineage and
// file-kind columns to documents.
func migratePermissions() error {
	log.Println("Creating groups and group_members tables...")
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS groups (
			id UUID PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			is_personal BOOLEAN NOT NULL DEFAULT FALSE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
		);
	`); err != nil {
		return fmt.Errorf("failed to create groups table: %w", err)
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS group_members (
			group_id UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
			user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (group_id, user_id)
		);
	`); err != nil {
		return fmt.Errorf("failed to create group_members table: %w", err)
	}
	if _, err := DB.Exec(`CREATE INDEX IF NOT EXISTS idx_group_members_user ON group_members(user_id);`); err != nil {
		return fmt.Errorf("failed to index group_members: %w", err)
	}

	if _, err := DB.Exec(`ALTER TABLE users ADD COLUMN IF NOT EXISTS primary_group_id UUID REFERENCES groups(id);`); err != nil {
		return fmt.Errorf("failed to add users.primary_group_id: %w", err)
	}

	log.Println("Adding ownership, lineage and kind columns to documents...")
	documentColumns := []string{
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS owner_id TEXT REFERENCES users(id);`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS group_id UUID REFERENCES groups(id);`,
		fmt.Sprintf(`ALTER TABLE documents ADD COLUMN IF NOT EXISTS mode SMALLINT NOT NULL DEFAULT %d;`, ModeDefault),
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS derived_from_document_id UUID REFERENCES documents(id) ON DELETE SET NULL;`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS derived_via_tool TEXT;`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS derived_via_job_id UUID;`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'pdf';`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS mime_type TEXT NOT NULL DEFAULT 'application/pdf';`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS original_filename TEXT;`,
		`ALTER TABLE documents ADD COLUMN IF NOT EXISTS is_encrypted BOOLEAN NOT NULL DEFAULT FALSE;`,
		// Non-paginated rows (kind='source'/'export') carry no meaningful page count.
		`ALTER TABLE documents ALTER COLUMN total_pages DROP NOT NULL;`,
		`CREATE INDEX IF NOT EXISTS idx_documents_owner ON documents(owner_id);`,
		`CREATE INDEX IF NOT EXISTS idx_documents_derived_from ON documents(derived_from_document_id);`,
	}
	for _, stmt := range documentColumns {
		if _, err := DB.Exec(stmt); err != nil {
			return fmt.Errorf("failed to migrate documents (%s): %w", stmt, err)
		}
	}

	return nil
}

// migrateJobQueue creates the Postgres-backed job queue consumed by the worker.
func migrateJobQueue() error {
	log.Println("Creating job queue tables...")
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id UUID PRIMARY KEY,
			user_id TEXT NOT NULL REFERENCES users(id),
			tool_slug TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			params JSONB NOT NULL DEFAULT '{}',
			progress_current INT NOT NULL DEFAULT 0,
			progress_total   INT NOT NULL DEFAULT 0,
			progress_step TEXT,
			error TEXT,
			attempt INT NOT NULL DEFAULT 0,
			max_attempts INT NOT NULL DEFAULT 3,
			run_after TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			locked_by TEXT,
			heartbeat_at TIMESTAMP WITH TIME ZONE,
			created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
			started_at TIMESTAMP WITH TIME ZONE,
			completed_at TIMESTAMP WITH TIME ZONE
		);
	`); err != nil {
		return fmt.Errorf("failed to create jobs table: %w", err)
	}

	jobIndexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_jobs_claim ON jobs(status, run_after) WHERE status = 'queued';`,
		`CREATE INDEX IF NOT EXISTS idx_jobs_user ON jobs(user_id, created_at DESC);`,
	}
	for _, stmt := range jobIndexes {
		if _, err := DB.Exec(stmt); err != nil {
			return fmt.Errorf("failed to index jobs: %w", err)
		}
	}

	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS job_inputs (
			job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			position INT NOT NULL,
			PRIMARY KEY (job_id, position)
		);
	`); err != nil {
		return fmt.Errorf("failed to create job_inputs table: %w", err)
	}

	// UNIQUE (job_id, position) is deliberate: without it every output row could
	// keep the default position = 0 and silently lose page order for the ordered
	// multi-output tools (Split, PDF to JPG).
	if _, err := DB.Exec(`
		CREATE TABLE IF NOT EXISTS job_outputs (
			job_id UUID NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
			document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
			position INT NOT NULL DEFAULT 0,
			PRIMARY KEY (job_id, document_id),
			UNIQUE (job_id, position)
		);
	`); err != nil {
		return fmt.Errorf("failed to create job_outputs table: %w", err)
	}

	return nil
}

// backfillOwnership assigns every ownerless document to the system user and the
// legacy group with ModeLegacy (0o774), so existing users keep read, write and
// execute access. Safe to run on every boot.
func backfillOwnership() error {
	if _, err := DB.Exec(`
		INSERT INTO users (id, email, name, updated_at)
		VALUES ($1, 'system@booklet.local', 'System', CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO NOTHING;
	`, SystemUserID); err != nil {
		return fmt.Errorf("failed to upsert system user: %w", err)
	}

	legacyGroupID, err := EnsureGroup(LegacyGroupName, false)
	if err != nil {
		return fmt.Errorf("failed to ensure legacy group: %w", err)
	}

	res, err := DB.Exec(`
		UPDATE documents
		SET owner_id = $1, group_id = $2, mode = $3
		WHERE owner_id IS NULL;
	`, SystemUserID, legacyGroupID, ModeLegacy)
	if err != nil {
		return fmt.Errorf("failed to backfill document ownership: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Backfilled ownership for %d pre-permission documents.", n)
	}

	return nil
}

// migrateExecuteBit grants the execute bit to documents created before it was
// needed to run a tool.
//
// POST /api/tools/jobs has always required PermRead|PermExecute on every input,
// but ModeDefault was 0o644 and ModeLegacy 0o664 — neither sets x in any triple
// — so the endpoint denied every non-admin caller on every document, including
// the owner of a file they had just uploaded. Admins only escaped it because
// IsAdmin short-circuits the check.
//
// This one runs exactly once, unlike the other migrations here. It cannot be
// idempotent-per-boot: 0o644 is both the old default and a mode a user may
// deliberately choose in the share dialog, and those two are indistinguishable
// in the row. Re-running on every boot would keep re-granting execute on a
// document whose owner had just revoked it. The marker in schema_migrations is
// what makes "existing rows" mean the ones that existed at upgrade time.
//
// updated_at is deliberately not touched, so this does not reshuffle library
// listings that sort by it.
func migrateExecuteBit() error {
	// ADD COLUMN IF NOT EXISTS cannot restate the default of a column that
	// already exists, so the value in migratePermissions only applies to a fresh
	// install. Databases migrated by an earlier build need this explicitly, and
	// it is safe to reassert on every boot.
	if _, err := DB.Exec(fmt.Sprintf(
		`ALTER TABLE documents ALTER COLUMN mode SET DEFAULT %d;`, ModeDefault)); err != nil {
		return fmt.Errorf("failed to update documents.mode default: %w", err)
	}

	applied, err := migrationApplied(migrationExecuteBit)
	if err != nil {
		return err
	}
	if applied {
		return nil
	}

	res, err := DB.Exec(`
		UPDATE documents
		SET mode = CASE mode WHEN $1 THEN $2 WHEN $3 THEN $4 ELSE mode END
		WHERE mode IN ($1, $3);
	`, modeDefaultPre, ModeDefault, modeLegacyPre, ModeLegacy)
	if err != nil {
		return fmt.Errorf("failed to add execute bit to legacy document modes: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("Granted the execute bit to %d document(s) at a pre-execute default mode.", n)
	}

	return markMigrationApplied(migrationExecuteBit)
}

// EnsureGroup returns the id of the named group, creating it when absent.
func EnsureGroup(name string, isPersonal bool) (string, error) {
	var id string
	err := DB.QueryRow(`SELECT id FROM groups WHERE name = $1`, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return "", err
	}

	newID := uuid.New().String()
	// ON CONFLICT covers a concurrent creator racing us between the SELECT and
	// the INSERT; the RETURNING clause then yields nothing, so re-read.
	err = DB.QueryRow(`
		INSERT INTO groups (id, name, is_personal)
		VALUES ($1, $2, $3)
		ON CONFLICT (name) DO NOTHING
		RETURNING id;
	`, newID, name, isPersonal).Scan(&id)
	if err == sql.ErrNoRows {
		if err := DB.QueryRow(`SELECT id FROM groups WHERE name = $1`, name).Scan(&id); err != nil {
			return "", err
		}
		return id, nil
	}
	if err != nil {
		return "", err
	}
	return id, nil
}

// EnsurePersonalGroup creates the caller's personal group, adds them to it and
// sets users.primary_group_id when it is not already set. Called on every login
// so that pre-existing users are migrated lazily.
func EnsurePersonalGroup(userID string) (string, error) {
	var existing sql.NullString
	if err := DB.QueryRow(`SELECT primary_group_id FROM users WHERE id = $1`, userID).Scan(&existing); err != nil {
		return "", fmt.Errorf("failed to read primary group for %s: %w", userID, err)
	}
	if existing.Valid && existing.String != "" {
		// Membership may still be missing if a previous run failed midway.
		if _, err := DB.Exec(`
			INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING;
		`, existing.String, userID); err != nil {
			return "", fmt.Errorf("failed to ensure group membership for %s: %w", userID, err)
		}
		return existing.String, nil
	}

	// Namespaced by user id so the unique group name can never collide with an
	// admin-created group.
	groupID, err := EnsureGroup("user:"+userID, true)
	if err != nil {
		return "", fmt.Errorf("failed to create personal group for %s: %w", userID, err)
	}

	if _, err := DB.Exec(`
		INSERT INTO group_members (group_id, user_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING;
	`, groupID, userID); err != nil {
		return "", fmt.Errorf("failed to add %s to personal group: %w", userID, err)
	}

	if _, err := DB.Exec(`
		UPDATE users SET primary_group_id = $1 WHERE id = $2 AND primary_group_id IS NULL;
	`, groupID, userID); err != nil {
		return "", fmt.Errorf("failed to set primary group for %s: %w", userID, err)
	}

	return groupID, nil
}

// PrimaryGroupID returns the caller's primary group, creating it if needed.
func PrimaryGroupID(userID string) (string, error) {
	return EnsurePersonalGroup(userID)
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

