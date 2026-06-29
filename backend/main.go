package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"booklet/auth"
	"booklet/db"
	"booklet/embeddings"
	"booklet/handlers"
	"booklet/logger"
	"booklet/metrics"
	"booklet/storage"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log.Println("Starting Booklet Backend service...")

	// 1. Initialize DB Layer
	if err := db.InitDB(); err != nil {
		log.Fatalf("Fatal: Database initialization failed: %v", err)
	}

	// TODO: Expose db.FailStaleProcessingDocuments() as a secured admin API route (e.g., POST /api/admin/clean-stale-processes)
	// to be triggered by an external cron/scheduler. The endpoint should require API key authentication (with admin key rotation).

	// 2. Initialize MinIO Object Storage
	if err := storage.InitStorage(); err != nil {
		log.Fatalf("Fatal: MinIO storage initialization failed: %v", err)
	}

	// 3. Initialize Embedding Layer (Ollama / Mock)
	embeddings.InitEmbedder()

	// 4. Initialize Auth & OIDC
	auth.InitAuth()

	// 5. Register Prometheus Metrics
	metrics.RegisterMetrics()

	// 6. Setup ServeMux Router (Standard Go 1.22+ mux)
	mux := http.NewServeMux()

	// Auth routes (unprotected)
	mux.Handle("/api/auth/login", handlers.InstrumentHandler("/api/auth/login", auth.HandleLogin))
	mux.Handle("/api/auth/callback", handlers.InstrumentHandler("/api/auth/callback", auth.HandleCallback))
	mux.Handle("/api/auth/logout", handlers.InstrumentHandler("/api/auth/logout", auth.HandleLogout))
	mux.Handle("/api/auth/me", handlers.InstrumentHandler("/api/auth/me", auth.HandleMe))

	// Developer bypass route — only registered when APP_ENV=development.
	// In production this path does not exist in the mux at all (returns 404).
	if os.Getenv("APP_ENV") == "development" {
		log.Println("[DEV] Developer bypass route registered at /api/auth/dev/login")
		mux.Handle("/api/auth/dev/login", handlers.InstrumentHandler("/api/auth/dev/login", auth.HandleDevLogin))
	}

	// Document Management routes (require authentication middleware)
	mux.Handle("/api/documents", auth.RequireAuth(handlers.InstrumentHandler("/api/documents", handlers.HandleListDocuments)))
	mux.Handle("/api/documents/{id}", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}", handlers.HandleGetDocument)))
	mux.Handle("/api/documents/{id}/dismiss", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/dismiss", handlers.HandleDismissDocument)))
	mux.Handle("/api/documents/upload", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/upload", handlers.HandleUploadDocument)))
	mux.Handle("/api/documents/{id}/pages/{page_number}/pdf", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/pages/{page_number}/pdf", handlers.HandleGetPagePDF)))

	// New resume route (requires authentication middleware)
	mux.Handle("/api/documents/{id}/resume", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/resume", handlers.HandleResumeDocument)))

	// Booklet Compilation routes (require authentication middleware)
	mux.Handle("/api/documents/{id}/booklet/preview", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/booklet/preview", handlers.HandleGetBookletPreviewPDF)))
	mux.Handle("/api/documents/{id}/booklet/compile", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/booklet/compile", handlers.HandleCompileBooklet)))
	mux.Handle("/api/documents/{id}/booklet/cleanup", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/booklet/cleanup", handlers.HandleCleanupBooklets)))
	mux.Handle("/api/booklets", auth.RequireAuth(handlers.InstrumentHandler("/api/booklets", handlers.HandleListBooklets)))
	mux.Handle("/api/booklets/{id}", auth.RequireAuth(handlers.InstrumentHandler("/api/booklets/{id}", handlers.HandleGetBooklet)))
	mux.Handle("/api/booklets/{id}/download", auth.RequireAuth(handlers.InstrumentHandler("/api/booklets/{id}/download", handlers.HandleDownloadBooklet)))

	// Semantic Search route (requires authentication middleware)
	mux.Handle("/api/search", auth.RequireAuth(handlers.InstrumentHandler("/api/search", handlers.HandleSemanticSearch)))
	mux.Handle("/api/documents/{id}/search-preview", auth.RequireAuth(handlers.InstrumentHandler("/api/documents/{id}/search-preview", handlers.HandleDocumentSearchPreviewPDF)))

	// Administrative routes (requires API key authentication, OIDC not required)
	mux.Handle("/api/admin/clean-stale-processes", handlers.InstrumentHandler("/api/admin/clean-stale-processes", handlers.HandleCleanStaleProcesses))

	// Prometheus Metrics endpoint
	mux.Handle("/metrics", promhttp.Handler())

	// Apply CORS & Logging middleware
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server is running on port %s...", port)
	if err := http.ListenAndServe(":"+port, loggingMiddleware(corsMiddleware(mux))); err != nil {
		log.Fatalf("Fatal: Server failed to start: %v", err)
	}
}

// corsMiddleware sets up headers for local development between Vite and Go
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		
		allowedOriginsStr := os.Getenv("ALLOWED_ORIGINS")
		if allowedOriginsStr == "" {
			// Fallback to FRONTEND_URL if ALLOWED_ORIGINS is not set
			frontendURL := os.Getenv("FRONTEND_URL")
			if frontendURL == "" {
				frontendURL = "http://localhost:5173"
			}
			allowedOriginsStr = frontendURL
		}

		// Parse comma-separated origins
		allowedOrigins := make(map[string]bool)
		var firstOrigin string
		for _, o := range strings.Split(allowedOriginsStr, ",") {
			trimmed := strings.TrimSpace(o)
			if trimmed != "" {
				allowedOrigins[trimmed] = true
				if firstOrigin == "" {
					firstOrigin = trimmed
				}
			}
		}

		// Default fallback if parsing fails or list is empty
		if firstOrigin == "" {
			firstOrigin = "http://localhost:5173"
			allowedOrigins[firstOrigin] = true
		}

		allowOrigin := firstOrigin
		if allowedOrigins[origin] {
			allowOrigin = origin
		}

		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-API-Key")

		// Handle OPTIONS preflight request
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriterWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// skipDiagnosticLogging lists paths that are called at high frequency by
// internal infrastructure (e.g. Prometheus scraper). Allocating a full
// RequestLogger with its log-entry slice for every scrape adds continuous
// memory pressure that never fully clears between GC cycles.
var skipDiagnosticLogging = map[string]bool{
	"/metrics": true,
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// For high-frequency infra paths, skip the diagnostic logger entirely
		// to avoid constant allocations that inflate the process RSS.
		if skipDiagnosticLogging[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rl := logger.NewRequestLogger()
		ctx := logger.WithLogger(r.Context(), rl)
		r = r.WithContext(ctx)

		rw := &responseWriterWrapper{ResponseWriter: w, statusCode: http.StatusOK}

		defer func() {
			duration := time.Since(start)
			if rec := recover(); rec != nil {
				rl.Logf("CRASH: panic recovered: %v", rec)
				rl.Print(r.Method, r.URL.Path, r.RemoteAddr, http.StatusInternalServerError, duration)
				// Respond with 500 Internal Server Error
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			} else {
				rl.Print(r.Method, r.URL.Path, r.RemoteAddr, rw.statusCode, duration)
			}
		}()

		rl.Logf("Started %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(rw, r)
	})
}

