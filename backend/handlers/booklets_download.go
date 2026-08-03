package handlers

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"booklet/db"
	"booklet/logger"
	"booklet/pdf"
	"booklet/permissions"
	"booklet/smtp"
	"booklet/storage"

	"github.com/minio/minio-go/v7"
)

func HandleDownloadBooklet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleDownloadBooklet", http.MethodGet) {
		return
	}

	bookletID, ok := parseUUIDParam(w, r, "HandleDownloadBooklet", "id")
	if !ok {
		return
	}
	logger.Logf(r.Context(), "HandleDownloadBooklet: request download for bookletID=%s", bookletID)

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	var status, storagePath, paperSize, docID string
	var sigSize, totalOriginalPages int
	var margin, gutter float64
	var guides bool
	err := db.DB.QueryRow(`
		SELECT cb.status, cb.storage_path, cb.config_signature_size, COALESCE(d.total_pages, 0), cb.config_paper_size, cb.document_id, cb.config_margin, cb.config_gutter, cb.config_guides
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id
		WHERE cb.id = $1`, bookletID).Scan(&status, &storagePath, &sigSize, &totalOriginalPages, &paperSize, &docID, &margin, &gutter, &guides)
	if handleDBError(w, r, "DownloadBooklet", "booklet not found", err) {
		return
	}

	if status != "ready" && handleConflict(w, r, "DownloadBooklet", fmt.Sprintf("booklet %s is in status '%s', not ready for download", bookletID, status), "booklet is not ready for download") {
		return
	}

	filter := r.URL.Query().Get("filter") // fronts, backs
	sheets := r.URL.Query().Get("sheets") // e.g. 1-10 or 12
	pagesParam := r.URL.Query().Get("pages") // booklet pages that were ruined, e.g. 13-16 or 14

	logger.Logf(r.Context(), "HandleDownloadBooklet: query params - filter=%q sheets=%q pagesParam=%q", filter, sheets, pagesParam)

	if pagesParam != "" {
		startPage := 1
		endPage := totalOriginalPages

		parts := strings.Split(pagesParam, "-")
		if len(parts) == 1 {
			if p, err := strconv.Atoi(parts[0]); err == nil {
				startPage = p
				endPage = p
			}
		} else if len(parts) == 2 {
			if p, err := strconv.Atoi(parts[0]); err == nil {
				startPage = p
			}
			if e, err := strconv.Atoi(parts[1]); err == nil {
				endPage = e
			}
		}

		// Map booklet pages to physical sheet range
		startSheet, endSheet := pdf.MapPagesToSheets(startPage, endPage)
		sheets = fmt.Sprintf("%d-%d", startSheet, endSheet)
		logger.Logf(r.Context(), "HandleDownloadBooklet: mapped pagesParam %s to sheet range %s", pagesParam, sheets)
	}

	ctx := r.Context()
	targetPath := storagePath

	var localSliceFile string
	var tempSliceDir string

	// Apply filtering/slicing on-the-fly if requested
	if filter != "" || sheets != "" {
		logger.Logf(r.Context(), "HandleDownloadBooklet: slice requested. Slicing booklet targetPath=%s on-the-fly", targetPath)
		// Fetch original pages from DB to compile slice
		rows, err := db.DB.Query(`
			SELECT page_number, storage_path, width, height 
			FROM document_pages 
			WHERE document_id = $1
			ORDER BY page_number ASC`, docID)
		if handleServerError(w, r, "HandleDownloadBooklet", "database error", err) {
			return
		}
		defer rows.Close()

		var dbPages []pdf.DBPageInfo
		for rows.Next() {
			var p pdf.DBPageInfo
			if err := rows.Scan(&p.PageNumber, &p.StoragePath, &p.Width, &p.Height); err != nil {
				if handleServerError(w, r, "HandleDownloadBooklet", "database error", err) {
					return
				}
			}
			dbPages = append(dbPages, p)
		}

		tempSliceDir, err = os.MkdirTemp("", "booklet-slice-*")
		if handleServerError(w, r, "HandleDownloadBooklet", "internal server error", err) {
			return
		}
		defer os.RemoveAll(tempSliceDir)

		localSliceFile = filepath.Join(tempSliceDir, "slice.pdf")
		err = pdf.CompileBookletSlice(ctx, dbPages, pdf.BookletConfig{
			Margin:        margin,
			Gutter:        gutter,
			PaperSize:     paperSize,
			SignatureSize: sigSize,
			Guides:        guides,
		}, filter, sheets, localSliceFile)

		if handleServerError(w, r, "HandleDownloadBooklet", "failed to slice booklet pages", err) {
			return
		}
	}

	var streamFile string
	var tempStreamDir string

	if localSliceFile != "" {
		streamFile = localSliceFile
	} else {
		// Download the main compiled booklet from storage
		var err error
		tempStreamDir, err = os.MkdirTemp("", "booklet-stream-*")
		if handleServerError(w, r, "HandleDownloadBooklet", "internal server error", err) {
			return
		}
		defer os.RemoveAll(tempStreamDir)

		streamFile = filepath.Join(tempStreamDir, "temp.pdf")
		logger.Logf(r.Context(), "Streaming PDF booklet %s to client...", targetPath)
		err = storage.DownloadFile(ctx, targetPath, streamFile)
		if handleServerError(w, r, "HandleDownloadBooklet", "failed to stream from object store", err) {
			return
		}
	}

	streamPDFFile(w, r, "HandleDownloadBooklet", streamFile, "booklet.pdf")
}

// HandleEmailBooklet downloads a booklet PDF and sends it as an email attachment.
// Requires standard OIDC/mock user auth.
func HandleEmailBooklet(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, "HandleEmailBooklet", http.MethodPost) {
		return
	}

	bookletID, ok := parseUUIDParam(w, r, "HandleEmailBooklet", "id")
	if !ok {
		return
	}

	if !enforceBookletAccess(w, r, bookletID, permissions.PermRead) {
		return
	}

	type EmailRequest struct {
		Email string `json:"email"`
	}

	var req EmailRequest
	if !decodeJSON(w, r, "HandleEmailBooklet", &req) {
		return
	}

	if req.Email == "" && handleBadRequest(w, r, "HandleEmailBooklet", "missing recipient email", "recipient email is required") {
		return
	}

	// Check if SMTP is configured
	smtpCfg, err := smtp.GetSMTPConfig(r.Context())
	if (err != nil || !smtpCfg.IsConfigured()) && handleServiceUnavailable(w, r, "HandleEmailBooklet", fmt.Sprintf("SMTP not configured or error: %v", err), "SMTP server is not configured by the administrator") {
		return
	}

	// Fetch booklet and original document name
	var status, storagePath, docName string
	err = db.DB.QueryRowContext(r.Context(), `
		SELECT cb.status, cb.storage_path, d.name
		FROM compiled_booklets cb
		JOIN documents d ON cb.document_id = d.id
		WHERE cb.id = $1
	`, bookletID).Scan(&status, &storagePath, &docName)

	if handleDBError(w, r, "HandleEmailBooklet", "booklet not found", err) {
		return
	}

	if (status != "ready" || storagePath == "") && handleBadRequest(w, r, "HandleEmailBooklet", "booklet not ready", "booklet is not compiled or compilation failed") {
		return
	}

	// Fetch PDF from MinIO
	object, err := storage.MinioClient.GetObject(r.Context(), storage.BucketName, storagePath, minio.GetObjectOptions{})
	if handleServerError(w, r, "HandleEmailBooklet", "failed to retrieve PDF from storage", err) {
		return
	}
	defer object.Close()

	pdfBytes, err := io.ReadAll(object)
	if handleServerError(w, r, "HandleEmailBooklet", "failed to read booklet data", err) {
		return
	}

	// Compose Email
	attachmentName := fmt.Sprintf("%s_booklet.pdf", strings.ReplaceAll(docName, " ", "_"))
	subject := fmt.Sprintf("Your Booklet PDF: %s", docName)
	htmlBody := fmt.Sprintf(`
		<h3>Your Booklet is Ready!</h3>
		<p>Hi there,</p>
		<p>Please find attached the compiled PDF booklet for <strong>%s</strong> from Booklet Studio.</p>
		<p>Best regards,<br/>Booklet Studio Team</p>
	`, docName)

	err = smtp.SendEmail(r.Context(), smtpCfg, req.Email, subject, htmlBody, attachmentName, pdfBytes)
	if handleServerError(w, r, "HandleEmailBooklet", "failed to send booklet email", err) {
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{
		"status":  "success",
		"message": "Booklet PDF successfully emailed to " + req.Email,
	})
}
