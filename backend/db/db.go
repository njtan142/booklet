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

// Float32ArrayToString converts a slice of floats to pgvector string format (e.g. "[0.1,0.2,0.3]")
func Float32ArrayToString(slice []float32) string {
	var strVals []string
	for _, v := range slice {
		strVals = append(strVals, fmt.Sprintf("%g", v))
	}
	return "[" + strings.Join(strVals, ",") + "]"
}

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
