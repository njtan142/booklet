package smtp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGetSMTPConfigFromEnv(t *testing.T) {
	// Setup env
	os.Setenv("SMTP_HOST", "smtp.test.local")
	os.Setenv("SMTP_PORT", "1025")
	os.Setenv("SMTP_USERNAME", "testuser")
	os.Setenv("SMTP_PASSWORD", "testpass")
	os.Setenv("SMTP_ENCRYPTION", "starttls")
	os.Setenv("SMTP_FROM_EMAIL", "test@test.local")
	os.Setenv("SMTP_FROM_NAME", "Test Sender")

	defer func() {
		os.Unsetenv("SMTP_HOST")
		os.Unsetenv("SMTP_PORT")
		os.Unsetenv("SMTP_USERNAME")
		os.Unsetenv("SMTP_PASSWORD")
		os.Unsetenv("SMTP_ENCRYPTION")
		os.Unsetenv("SMTP_FROM_EMAIL")
		os.Unsetenv("SMTP_FROM_NAME")
	}()

	cfg := GetSMTPConfigFromEnv()

	if cfg.Host != "smtp.test.local" {
		t.Errorf("Expected Host smtp.test.local, got %s", cfg.Host)
	}
	if cfg.Port != 1025 {
		t.Errorf("Expected Port 1025, got %d", cfg.Port)
	}
	if cfg.Username != "testuser" {
		t.Errorf("Expected Username testuser, got %s", cfg.Username)
	}
	if cfg.Password != "testpass" {
		t.Errorf("Expected Password testpass, got %s", cfg.Password)
	}
	if cfg.Encryption != "starttls" {
		t.Errorf("Expected Encryption starttls, got %s", cfg.Encryption)
	}
	if cfg.FromEmail != "test@test.local" {
		t.Errorf("Expected FromEmail test@test.local, got %s", cfg.FromEmail)
	}
	if cfg.FromName != "Test Sender" {
		t.Errorf("Expected FromName Test Sender, got %s", cfg.FromName)
	}
}

func TestSMTPConfig_IsConfigured(t *testing.T) {
	cfg := SMTPConfig{}
	if cfg.IsConfigured() {
		t.Error("Expected IsConfigured to be false for empty config")
	}

	cfg.Host = "smtp.test.local"
	if !cfg.IsConfigured() {
		t.Error("Expected IsConfigured to be true when Host is set")
	}
}

func startMockSMTPServer(t *testing.T, expectedUser, expectedPass string, onMessage func(rawMessage string)) (string, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to listen: %v", err)
	}

	addr := l.Addr().String()
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		defer l.Close()
		for {
			conn, err := l.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
					return
				default:
					return
				}
			}

			go func(c net.Conn) {
				defer c.Close()
				reader := bufio.NewReader(c)
				writer := bufio.NewWriter(c)

				writeLine := func(line string) {
					c.SetWriteDeadline(time.Now().Add(2 * time.Second))
					writer.WriteString(line + "\r\n")
					writer.Flush()
				}

				readLine := func() string {
					c.SetReadDeadline(time.Now().Add(2 * time.Second))
					line, err := reader.ReadString('\n')
					if err != nil {
						return ""
					}
					return strings.TrimRight(line, "\r\n")
				}

				writeLine("220 mock.local SMTP ready")

				for {
					line := readLine()
					if line == "" {
						return
					}

					upper := strings.ToUpper(line)
					if strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO") {
						writeLine("250-mock.local greet")
						writeLine("250 AUTH PLAIN")
					} else if strings.HasPrefix(upper, "AUTH PLAIN") {
						parts := strings.Split(line, " ")
						if len(parts) >= 3 {
							authPayload := parts[2]
							decoded, err := base64.StdEncoding.DecodeString(authPayload)
							if err != nil {
								writeLine("501 Invalid auth encoding")
								return
							}
							creds := strings.Split(string(decoded), "\x00")
							if len(creds) >= 3 && creds[1] == expectedUser && creds[2] == expectedPass {
								writeLine("235 Authentication successful")
							} else {
								writeLine("535 Authentication credentials invalid")
								return
							}
						} else {
							writeLine("535 Auth error")
							return
						}
					} else if strings.HasPrefix(upper, "MAIL FROM") {
						writeLine("250 OK")
					} else if strings.HasPrefix(upper, "RCPT TO") {
						writeLine("250 OK")
					} else if upper == "DATA" {
						writeLine("354 Start mail input")
						var buf bytes.Buffer
						for {
							dataLine, err := reader.ReadString('\n')
							if err != nil {
								return
							}
							if dataLine == ".\r\n" || dataLine == ".\n" {
								break
							}
							buf.WriteString(dataLine)
						}
						writeLine("250 OK message accepted")
						onMessage(buf.String())
					} else if upper == "QUIT" {
						writeLine("221 Goodbye")
						return
					} else {
						writeLine("250 OK")
					}
				}
			}(conn)
		}
	}()

	cleanup := func() {
		cancel()
		l.Close()
	}

	return addr, cleanup
}

func TestSendEmail_NoAttachment(t *testing.T) {
	var receivedMessage string
	messageChan := make(chan string, 1)

	addr, cleanup := startMockSMTPServer(t, "user123", "pass456", func(msg string) {
		messageChan <- msg
	})
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := SMTPConfig{
		Host:       host,
		Port:       port,
		Username:   "user123",
		Password:   "pass456",
		Encryption: "none",
		FromEmail:  "sender@test.local",
		FromName:   "Sender Name",
	}

	err := SendEmail(context.Background(), cfg, "receiver@test.local", "Test Subject", "<p>Hello World</p>", "", nil)
	if err != nil {
		t.Fatalf("SendEmail failed: %v", err)
	}

	select {
	case receivedMessage = <-messageChan:
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for email message")
	}

	if !strings.Contains(receivedMessage, "From: \"Sender Name\" <sender@test.local>") {
		t.Errorf("Expected correct From header, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "To: receiver@test.local") {
		t.Errorf("Expected correct To header, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "Subject: Test Subject") {
		t.Errorf("Expected correct Subject, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "Content-Type: text/html; charset=UTF-8") {
		t.Errorf("Expected correct content type, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "<p>Hello World</p>") {
		t.Errorf("Expected html body, got:\n%s", receivedMessage)
	}
}

func TestSendEmail_WithAttachment_LineWrapping(t *testing.T) {
	var receivedMessage string
	messageChan := make(chan string, 1)

	addr, cleanup := startMockSMTPServer(t, "user123", "pass456", func(msg string) {
		messageChan <- msg
	})
	defer cleanup()

	host, portStr, _ := net.SplitHostPort(addr)
	port, _ := strconv.Atoi(portStr)

	cfg := SMTPConfig{
		Host:       host,
		Port:       port,
		Username:   "user123",
		Password:   "pass456",
		Encryption: "none",
		FromEmail:  "sender@test.local",
		FromName:   "Sender Name",
	}

	// Create a large dummy binary slice (e.g. 500 bytes) to trigger line wrapping.
	// Base64 encoding of 500 bytes is ~668 characters.
	// Wrapped at 76 characters, it should produce around 9 lines of base64.
	largeBytes := make([]byte, 500)
	for i := range largeBytes {
		largeBytes[i] = byte(i % 256)
	}

	err := SendEmail(context.Background(), cfg, "receiver@test.local", "Test Attach", "<p>Attach Body</p>", "dummy.pdf", largeBytes)
	if err != nil {
		t.Fatalf("SendEmail with attachment failed: %v", err)
	}

	select {
	case receivedMessage = <-messageChan:
	case <-time.After(3 * time.Second):
		t.Fatal("Timeout waiting for email message")
	}

	if !strings.Contains(receivedMessage, "Content-Type: multipart/mixed; boundary=") {
		t.Errorf("Expected multipart message structure, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "Content-Type: application/pdf; name=\"dummy.pdf\"") {
		t.Errorf("Expected PDF attachment header, got:\n%s", receivedMessage)
	}
	if !strings.Contains(receivedMessage, "Content-Transfer-Encoding: base64") {
		t.Errorf("Expected base64 content transfer encoding header, got:\n%s", receivedMessage)
	}

	// Verify that there are indeed lines in the attachment base64 payload that are wrapped.
	// Specifically, find the base64 part and check the line length.
	lines := strings.Split(receivedMessage, "\r\n")
	hasWrappedLines := false
	inBase64Block := false

	for _, line := range lines {
		if strings.Contains(line, "Content-Disposition: attachment;") {
			inBase64Block = true
			continue
		}
		// End of attachment is boundary
		if inBase64Block && strings.HasPrefix(line, "--") {
			inBase64Block = false
		}
		if inBase64Block && line != "" {
			if len(line) > 76 {
				t.Errorf("Base64 line exceeds 76 characters: len=%d line=%s", len(line), line)
			}
			if len(line) == 76 {
				hasWrappedLines = true
			}
		}
	}

	if !hasWrappedLines {
		t.Error("Expected to find base64 wrapped lines of exactly 76 characters")
	}
}

