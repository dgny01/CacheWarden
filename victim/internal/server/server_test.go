package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestHandler() *Handler {
	return New(slog.New(slog.NewTextHandler(io.Discard, nil)), 8*1024, 1)
}

func TestHealth(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	if response.Body.String() != "OK\n" {
		t.Fatalf("body = %q, want OK", response.Body.String())
	}
}

func TestWorkReturnsMeasuredResult(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/work", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusOK)
	}
	var body struct {
		Checksum   string  `json:"checksum"`
		DurationMS float64 `json:"duration_ms"`
		WorkloadB  int     `json:"workload_bytes"`
		Iterations int     `json:"iterations"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Checksum == "" || body.DurationMS < 0 || body.WorkloadB != 8*1024 || body.Iterations != 1 {
		t.Fatalf("unexpected response: %+v", body)
	}
}

func TestMetricsExposeRequiredSeries(t *testing.T) {
	handler := newTestHandler()
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/work", nil))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	for _, metric := range []string{
		"cachewarden_http_requests_total",
		"cachewarden_http_request_duration_seconds",
		"cachewarden_work_duration_seconds",
		"cachewarden_http_errors_total",
		"cachewarden_http_active_requests",
	} {
		if !strings.Contains(response.Body.String(), metric) {
			t.Errorf("metrics output does not contain %q", metric)
		}
	}
}

func TestRejectsUnsupportedMethod(t *testing.T) {
	response := httptest.NewRecorder()
	newTestHandler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/work", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
