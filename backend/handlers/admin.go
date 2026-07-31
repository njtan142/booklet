package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/smtp"
)

// HandleCleanStaleProcesses triggers FailStaleProcessingDocuments to cleanup stale document/booklet states.
// Exposes this function as a secured administrative API route, requiring X-API-Key auth.
func HandleCleanStaleProcesses(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		// Fallback for development if not explicitly configured in env
		adminKey = "dev-admin-key"
	}

	reqKey := r.Header.Get("X-API-Key")
	if reqKey == "" {
		// Also allow Bearer token under Authorization
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			reqKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if reqKey != adminKey {
		logger.Logf(r.Context(), "HandleCleanStaleProcesses: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	logger.Logf(r.Context(), "HandleCleanStaleProcesses: triggering stale background processes cleanup")
	err := db.FailStaleProcessingDocuments()
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to clean stale processes: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Stale background processes cleaned up successfully",
	})
}

// HandleGetSMTPConfig retrieves the system-wide SMTP settings.
// Requires X-API-Key auth.
func HandleGetSMTPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		adminKey = "dev-admin-key"
	}

	reqKey := r.Header.Get("X-API-Key")
	if reqKey == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			reqKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if reqKey != adminKey {
		logger.Logf(r.Context(), "HandleGetSMTPConfig: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	cfg, err := smtp.GetSMTPConfig(r.Context())
	if err != nil {
		logger.Logf(r.Context(), "Error: failed to fetch SMTP config: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Mask the password for security
	if cfg.Password != "" {
		cfg.Password = "********"
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(cfg)
}

// HandleSaveSMTPConfig saves the system-wide SMTP settings.
// Requires X-API-Key auth.
func HandleSaveSMTPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		adminKey = "dev-admin-key"
	}

	reqKey := r.Header.Get("X-API-Key")
	if reqKey == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			reqKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if reqKey != adminKey {
		logger.Logf(r.Context(), "HandleSaveSMTPConfig: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var cfg smtp.SMTPConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode SMTP config: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Basic validation
	if cfg.Host == "" {
		http.Error(w, "SMTP host is required", http.StatusBadRequest)
		return
	}
	if cfg.Port <= 0 {
		http.Error(w, "SMTP port must be a positive integer", http.StatusBadRequest)
		return
	}
	if cfg.FromEmail == "" {
		http.Error(w, "Sender email is required", http.StatusBadRequest)
		return
	}

	if err := smtp.SaveSMTPConfig(r.Context(), cfg); err != nil {
		logger.Logf(r.Context(), "Error: failed to save SMTP config: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "SMTP configuration saved successfully",
	})
}

// HandleTestSMTP sends a test email to verify SMTP configuration.
// Requires X-API-Key auth.
func HandleTestSMTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	adminKey := os.Getenv("ADMIN_API_KEY")
	if adminKey == "" {
		adminKey = "dev-admin-key"
	}

	reqKey := r.Header.Get("X-API-Key")
	if reqKey == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			reqKey = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}

	if reqKey != adminKey {
		logger.Logf(r.Context(), "HandleTestSMTP: unauthorized access attempt")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	type TestSMTPRequest struct {
		Config smtp.SMTPConfig `json:"config"`
		To     string          `json:"to"`
	}

	var req TestSMTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Logf(r.Context(), "Error: failed to decode test SMTP request: %v", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.To == "" {
		http.Error(w, "recipient email 'to' is required", http.StatusBadRequest)
		return
	}

	// If password is masked, retrieve existing database password
	if req.Config.Password == "********" {
		saved, err := smtp.GetSMTPConfig(r.Context())
		if err == nil && saved.IsConfigured() {
			req.Config.Password = saved.Password
		}
	}

	subject := "Booklet Studio SMTP Connection Test"
	htmlBody := fmt.Sprintf(`
		<h3>SMTP Connection Test</h3>
		<p>This is a test email from Booklet Studio to verify your system-wide SMTP settings.</p>
		<p>If you are reading this message, your SMTP configurations are correct!</p>
		<hr/>
		<p>Timestamp: %s</p>
	`, time.Now().Format(time.RFC1123))

	err := smtp.SendEmail(r.Context(), req.Config, req.To, subject, htmlBody, "", nil)
	if err != nil {
		logger.Logf(r.Context(), "Error: SMTP test email delivery failed: %v", err)
		http.Error(w, fmt.Sprintf("SMTP test failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":  "success",
		"message": "Test email successfully sent to " + req.To,
	})
}

// HandleSMTPConfig handles both GET (retrieve) and POST (save) system-wide SMTP settings.
func HandleSMTPConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		HandleGetSMTPConfig(w, r)
	} else if r.Method == http.MethodPost {
		HandleSaveSMTPConfig(w, r)
	} else {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
