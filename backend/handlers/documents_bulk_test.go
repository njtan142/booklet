package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandleBulkDeleteDocuments_InvalidMethod(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/documents/bulk-delete", nil)
	w := httptest.NewRecorder()

	HandleBulkDeleteDocuments(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405 Method Not Allowed, got %d", resp.StatusCode)
	}
}

func TestHandleBulkDeleteDocuments_InvalidBody(t *testing.T) {
	req := httptest.NewRequest("POST", "/api/documents/bulk-delete", bytes.NewBufferString("invalid json"))
	w := httptest.NewRecorder()

	HandleBulkDeleteDocuments(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400 Bad Request, got %d", resp.StatusCode)
	}
}

func TestHandleBulkDeleteDocuments_EmptyIDs(t *testing.T) {
	body, _ := json.Marshal(BulkDeleteRequest{IDs: []string{}})
	req := httptest.NewRequest("POST", "/api/documents/bulk-delete", bytes.NewBuffer(body))
	w := httptest.NewRecorder()

	HandleBulkDeleteDocuments(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200 OK, got %d", resp.StatusCode)
	}

	var res BulkDeleteResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if res.DeletedCount != 0 {
		t.Errorf("Expected deleted_count 0, got %d", res.DeletedCount)
	}
}
