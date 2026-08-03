package handlers

import (
	"fmt"
	"net/http"
	"time"

	"booklet/db"
	"booklet/logger"
	"booklet/smtp"
)

// HandleCleanStaleProcesses triggers FailStaleProcessingDocuments to cleanup stale document/booklet states.
// Exposes this function as a secured administrative API route, requiring X-API-Key auth.
func HandleCleanStaleProcesses(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleCleanStaleProcesses", http.MethodPost) || !requireAPIKey(w, r, "HandleCleanStaleProcesses") {
		return
	}

	logger.Logf(r.Context(), "HandleCleanStaleProcesses: triggering stale background processes cleanup")
	err := db.FailStaleProcessingDocuments()
	if handleServerError(w, r, "HandleCleanStaleProcesses", "failed to clean stale processes", err) {
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Stale background processes cleaned up successfully",
	})
}

// HandleGetSMTPConfig retrieves the system-wide SMTP settings.
// Requires X-API-Key auth.
func HandleGetSMTPConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleGetSMTPConfig", http.MethodGet) || !requireAPIKey(w, r, "HandleGetSMTPConfig") {
		return
	}

	cfg, err := smtp.GetSMTPConfig(r.Context())
	if handleServerError(w, r, "HandleGetSMTPConfig", "failed to fetch SMTP config", err) {
		return
	}

	// Mask the password for security
	if cfg.Password != "" {
		cfg.Password = "********"
	}

	respondJSON(w, http.StatusOK, cfg)
}

// HandleSaveSMTPConfig saves the system-wide SMTP settings.
// Requires X-API-Key auth.
func HandleSaveSMTPConfig(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleSaveSMTPConfig", http.MethodPost) || !requireAPIKey(w, r, "HandleSaveSMTPConfig") {
		return
	}

	var cfg smtp.SMTPConfig
	if !decodeJSON(w, r, "HandleSaveSMTPConfig", &cfg) {
		return
	}

	// Basic validation
	if cfg.Host == "" && handleBadRequest(w, r, "HandleSaveSMTPConfig", "missing SMTP host", "SMTP host is required") {
		return
	}
	if cfg.Port <= 0 && handleBadRequest(w, r, "HandleSaveSMTPConfig", "invalid SMTP port", "SMTP port must be a positive integer") {
		return
	}
	if cfg.FromEmail == "" && handleBadRequest(w, r, "HandleSaveSMTPConfig", "missing sender email", "Sender email is required") {
		return
	}

	if err := smtp.SaveSMTPConfig(r.Context(), cfg); handleServerError(w, r, "HandleSaveSMTPConfig", "failed to save SMTP config", err) {
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "SMTP configuration saved successfully",
	})
}

// HandleTestSMTP sends a test email to verify SMTP configuration.
// Requires X-API-Key auth.
func HandleTestSMTP(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleTestSMTP", http.MethodPost) || !requireAPIKey(w, r, "HandleTestSMTP") {
		return
	}

	type TestSMTPRequest struct {
		Config smtp.SMTPConfig `json:"config"`
		To     string          `json:"to"`
	}

	var req TestSMTPRequest
	if !decodeJSON(w, r, "HandleTestSMTP", &req) {
		return
	}

	if req.To == "" && handleBadRequest(w, r, "HandleTestSMTP", "missing recipient email", "recipient email 'to' is required") {
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
	if handleServerError(w, r, "HandleTestSMTP", fmt.Sprintf("SMTP test failed: %v", err), err) {
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
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
		handleMethodNotAllowed(w, r, "HandleSMTPConfig")
	}
}
