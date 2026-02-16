package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"
)

func TestMetricsEndpointIncludesHTTPRequestsTotal(t *testing.T) {
	cfg := config.Load()
	cfg.JWTSecret = "observability-test-secret"

	server := New(cfg, nil, ai.NewMockClient())
	router := server.Router()

	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthRecorder := httptest.NewRecorder()
	router.ServeHTTP(healthRecorder, healthReq)
	if healthRecorder.Code != http.StatusOK {
		t.Fatalf("expected /healthz 200, got %d", healthRecorder.Code)
	}

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	router.ServeHTTP(metricsRecorder, metricsReq)
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("expected /metrics 200, got %d", metricsRecorder.Code)
	}

	body := metricsRecorder.Body.String()
	if !strings.Contains(body, "http_requests_total") {
		t.Fatalf("expected metrics output to include http_requests_total, got: %s", body)
	}
}

func TestReadyzReturnsServiceUnavailableWithoutDatabase(t *testing.T) {
	cfg := config.Load()
	cfg.JWTSecret = "observability-test-secret"

	server := New(cfg, nil, ai.NewMockClient())
	router := server.Router()

	readyReq := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	readyRecorder := httptest.NewRecorder()
	router.ServeHTTP(readyRecorder, readyReq)

	if readyRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected /readyz 503 without db, got %d", readyRecorder.Code)
	}
}
