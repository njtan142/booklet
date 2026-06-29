package handlers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestHandleCleanStaleProcesses_Unauthorized(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	req := httptest.NewRequest("POST", "/api/admin/clean-stale-processes", nil)
	// Do not set X-API-Key or Authorization header

	w := httptest.NewRecorder()
	HandleCleanStaleProcesses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestHandleCleanStaleProcesses_InvalidKey(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	req := httptest.NewRequest("POST", "/api/admin/clean-stale-processes", nil)
	req.Header.Set("X-API-Key", "wrong-key")

	w := httptest.NewRecorder()
	HandleCleanStaleProcesses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestHandleCleanStaleProcesses_InvalidMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/admin/clean-stale-processes", nil)

	w := httptest.NewRecorder()
	HandleCleanStaleProcesses(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestHandleSMTPConfig_Unauthorized(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	// Test GET
	req := httptest.NewRequest("GET", "/api/admin/settings/smtp", nil)
	w := httptest.NewRecorder()
	HandleSMTPConfig(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected GET status 401, got %d", w.Result().StatusCode)
	}

	// Test POST
	req = httptest.NewRequest("POST", "/api/admin/settings/smtp", nil)
	w = httptest.NewRecorder()
	HandleSMTPConfig(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected POST status 401, got %d", w.Result().StatusCode)
	}
}

func TestHandleTestSMTP_Unauthorized(t *testing.T) {
	os.Setenv("ADMIN_API_KEY", "super-secret-key")
	defer os.Unsetenv("ADMIN_API_KEY")

	req := httptest.NewRequest("POST", "/api/admin/settings/smtp/test", nil)
	w := httptest.NewRecorder()
	HandleTestSMTP(w, req)
	if w.Result().StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", w.Result().StatusCode)
	}
}
