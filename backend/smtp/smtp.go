package smtp

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"

	"booklet/db"
	"booklet/logger"
)

type SMTPConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Username   string `json:"username"`
	Password   string `json:"password,omitempty"`
	Encryption string `json:"encryption"` // 'none', 'ssl', 'starttls'
	FromEmail  string `json:"from_email"`
	FromName   string `json:"from_name"`
}

// IsConfigured returns true if the host is set
func (c SMTPConfig) IsConfigured() bool {
	return c.Host != ""
}

// GetSMTPConfig retrieves the SMTP settings from the DB, falling back to environment variables.
func GetSMTPConfig(ctx context.Context) (SMTPConfig, error) {
	var cfg SMTPConfig
	err := db.DB.QueryRowContext(ctx, `
		SELECT host, port, username, password, encryption, from_email, COALESCE(from_name, '')
		FROM smtp_config
		WHERE id = 'global'
	`).Scan(&cfg.Host, &cfg.Port, &cfg.Username, &cfg.Password, &cfg.Encryption, &cfg.FromEmail, &cfg.FromName)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			logger.Logf(ctx, "SMTP: No configuration found in database. Falling back to environment variables.")
			return GetSMTPConfigFromEnv(), nil
		}
		return cfg, err
	}

	return cfg, nil
}

// GetSMTPConfigFromEnv returns SMTPConfig populated from environment variables
func GetSMTPConfigFromEnv() SMTPConfig {
	host := os.Getenv("SMTP_HOST")
	portStr := os.Getenv("SMTP_PORT")
	port, _ := strconv.Atoi(portStr)
	if port == 0 {
		port = 25 // standard fallback
	}
	username := os.Getenv("SMTP_USERNAME")
	password := os.Getenv("SMTP_PASSWORD")
	encryption := os.Getenv("SMTP_ENCRYPTION")
	if encryption == "" {
		encryption = "none"
	}
	fromEmail := os.Getenv("SMTP_FROM_EMAIL")
	if fromEmail == "" {
		fromEmail = "noreply@booklet.local"
	}
	fromName := os.Getenv("SMTP_FROM_NAME")
	if fromName == "" {
		fromName = "Booklet Studio"
	}

	return SMTPConfig{
		Host:       host,
		Port:       port,
		Username:   username,
		Password:   password,
		Encryption: encryption,
		FromEmail:  fromEmail,
		FromName:   fromName,
	}
}

// SaveSMTPConfig saves the SMTP configuration to the database.
// If the password is masked (i.e., '********'), the existing password in the database is preserved.
func SaveSMTPConfig(ctx context.Context, config SMTPConfig) error {
	if config.Password == "********" {
		oldCfg, err := GetSMTPConfig(ctx)
		if err == nil && oldCfg.IsConfigured() {
			config.Password = oldCfg.Password
		}
	}

	_, err := db.DB.ExecContext(ctx, `
		INSERT INTO smtp_config (id, host, port, username, password, encryption, from_email, from_name, updated_at)
		VALUES ('global', $1, $2, $3, $4, $5, $6, $7, CURRENT_TIMESTAMP)
		ON CONFLICT (id) DO UPDATE
		SET host = EXCLUDED.host,
		    port = EXCLUDED.port,
		    username = EXCLUDED.username,
		    password = EXCLUDED.password,
		    encryption = EXCLUDED.encryption,
		    from_email = EXCLUDED.from_email,
		    from_name = EXCLUDED.from_name,
		    updated_at = CURRENT_TIMESTAMP
	`, config.Host, config.Port, config.Username, config.Password, config.Encryption, config.FromEmail, config.FromName)

	return err
}

type lineWrappingWriter struct {
	w     io.Writer
	count int
	limit int
}

func (l *lineWrappingWriter) Write(p []byte) (n int, err error) {
	for i, b := range p {
		if l.count >= l.limit {
			if _, err := l.w.Write([]byte("\r\n")); err != nil {
				return i, err
			}
			l.count = 0
		}
		if _, err := l.w.Write([]byte{b}); err != nil {
			return i, err
		}
		l.count++
	}
	return len(p), nil
}

type plainAuthBypass struct {
	identity, username, password string
}

func PlainAuthBypass(identity, username, password string) smtp.Auth {
	return &plainAuthBypass{identity, username, password}
}

func (a *plainAuthBypass) Start(server *smtp.ServerInfo) (string, []byte, error) {
	resp := []byte(a.identity + "\x00" + a.username + "\x00" + a.password)
	return "PLAIN", resp, nil
}

func (a *plainAuthBypass) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("unexpected server challenge")
	}
	return nil, nil
}

// SendEmail sends an email using the specified SMTP configuration.
func SendEmail(ctx context.Context, config SMTPConfig, to string, subject string, htmlBody string, attachmentName string, attachmentBytes []byte) error {
	if !config.IsConfigured() {
		return errors.New("SMTP is not configured")
	}

	addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
	
	// Create mail headers and body
	var msg bytes.Buffer
	msg.WriteString(fmt.Sprintf("From: %s\r\n", formatAddress(config.FromName, config.FromEmail)))
	msg.WriteString(fmt.Sprintf("To: %s\r\n", to))
	msg.WriteString(fmt.Sprintf("Subject: %s\r\n", subject))
	msg.WriteString("MIME-Version: 1.0\r\n")

	if len(attachmentBytes) > 0 {
		boundary := "booklet_mail_boundary_" + fmt.Sprintf("%d", time.Now().UnixNano())
		msg.WriteString(fmt.Sprintf("Content-Type: multipart/mixed; boundary=%s\r\n\r\n", boundary))
		
		// HTML Body Part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		msg.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
		msg.WriteString(htmlBody)
		msg.WriteString("\r\n\r\n")
		
		// Attachment Part
		msg.WriteString(fmt.Sprintf("--%s\r\n", boundary))
		msg.WriteString(fmt.Sprintf("Content-Type: application/pdf; name=\"%s\"\r\n", attachmentName))
		msg.WriteString("Content-Transfer-Encoding: base64\r\n")
		msg.WriteString(fmt.Sprintf("Content-Disposition: attachment; filename=\"%s\"\r\n\r\n", attachmentName))
		
		// Base64 encode the attachment in standard chunks
		wrapper := &lineWrappingWriter{w: &msg, limit: 76}
		encoder := base64.NewEncoder(base64.StdEncoding, wrapper)
		if _, err := encoder.Write(attachmentBytes); err != nil {
			return fmt.Errorf("failed to encode attachment: %w", err)
		}
		encoder.Close()
		
		msg.WriteString("\r\n")
		msg.WriteString(fmt.Sprintf("--%s--\r\n", boundary))
	} else {
		msg.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
		msg.WriteString(htmlBody)
	}

	// Dial connection based on encryption type
	var client *smtp.Client
	var err error

	logger.Logf(ctx, "SMTP: Connecting to %s (encryption: %s)...", addr, config.Encryption)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, // Allow self-signed certs for local development/internal services
		ServerName:         config.Host,
	}

	switch strings.ToLower(config.Encryption) {
	case "ssl":
		// Implicit TLS connection
		dialer := &net.Dialer{
			Timeout: 5 * time.Second,
		}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, tlsConfig)
		if err != nil {
			logger.Logf(ctx, "SMTP: Connection failed via SSL/TLS to %s: %v", addr, err)
			return fmt.Errorf("failed to connect via SSL/TLS: %w", err)
		}
		c, err := smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			logger.Logf(ctx, "SMTP: Failed to create client after SSL/TLS connect: %v", err)
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		client = c
	case "starttls":
		// Explicit TLS connection
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			logger.Logf(ctx, "SMTP: Connection failed to %s: %v", addr, err)
			return fmt.Errorf("failed to connect: %w", err)
		}
		c, err := smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			logger.Logf(ctx, "SMTP: Failed to create client after connect: %v", err)
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		client = c

		// Call StartTLS
		if err := client.StartTLS(tlsConfig); err != nil {
			client.Close()
			logger.Logf(ctx, "SMTP: STARTTLS handshake failed: %v", err)
			return fmt.Errorf("STARTTLS failed: %w", err)
		}
	default: // "none" or unencrypted
		conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
		if err != nil {
			logger.Logf(ctx, "SMTP: Connection failed to %s: %v", addr, err)
			return fmt.Errorf("failed to connect: %w", err)
		}
		c, err := smtp.NewClient(conn, config.Host)
		if err != nil {
			conn.Close()
			logger.Logf(ctx, "SMTP: Failed to create client: %v", err)
			return fmt.Errorf("failed to create SMTP client: %w", err)
		}
		client = c
	}
	defer client.Close()

	// Authenticate if credentials are provided
	if config.Username != "" && config.Password != "" {
		var auth smtp.Auth
		if strings.ToLower(config.Encryption) == "none" {
			auth = PlainAuthBypass("", config.Username, config.Password)
		} else {
			auth = smtp.PlainAuth("", config.Username, config.Password, config.Host)
		}
		logger.Logf(ctx, "SMTP: Authenticating user %s...", config.Username)
		if err := client.Auth(auth); err != nil {
			logger.Logf(ctx, "SMTP: Authentication failed: %v", err)
			return fmt.Errorf("SMTP auth failed for user %s: %w", config.Username, err)
		}
		logger.Logf(ctx, "SMTP: Authentication successful")
	}

	// Send commands
	if err := client.Mail(config.FromEmail); err != nil {
		logger.Logf(ctx, "SMTP: MAIL command failed: %v", err)
		return fmt.Errorf("MAIL command failed: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		logger.Logf(ctx, "SMTP: RCPT command failed: %v", err)
		return fmt.Errorf("RCPT command failed for %s: %w", to, err)
	}

	// Send body
	w, err := client.Data()
	if err != nil {
		logger.Logf(ctx, "SMTP: DATA command failed: %v", err)
		return fmt.Errorf("DATA command failed: %w", err)
	}
	defer w.Close()

	if _, err := w.Write(msg.Bytes()); err != nil {
		logger.Logf(ctx, "SMTP: Failed to write message data: %v", err)
		return fmt.Errorf("failed to write message body: %w", err)
	}

	logger.Logf(ctx, "SMTP: Email successfully sent to %s", to)
	return nil
}

func formatAddress(name, email string) string {
	addr := mail.Address{
		Name:    name,
		Address: email,
	}
	return addr.String()
}
