package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/auth"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
)

func TestPersonaPreviewEndpointIntegration(t *testing.T) {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("db connect failed: %v", err)
	}
	defer pool.Close()

	cfg := config.Load()
	cfg.DatabaseURL = databaseURL
	cfg.JWTSecret = "preview-test-secret"
	cfg.DefaultPreviewQuota = 5
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("preview-integration-%d@example.com", unique)

	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("password hash failed: %v", err)
	}

	var userID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, email, hash).Scan(&userID)
	if err != nil {
		t.Fatalf("insert user failed: %v", err)
	}

	var roomID string
	err = pool.QueryRow(ctx, `
		INSERT INTO rooms(slug, name, description)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`,
		fmt.Sprintf("preview-room-%d", unique),
		"preview-room",
		"Room used by integration preview test.",
	).Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one sharp question"})
	doNotSayJSON, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrasesJSON, _ := json.Marshal([]string{"ship, learn, iterate"})

	var personaID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Preview Persona", "Focuses on practical product lessons.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaID)
	if err != nil {
		t.Fatalf("insert persona failed: %v", err)
	}

	token, err := auth.CreateToken(cfg.JWTSecret, userID)
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	server := New(cfg, pool, ai.NewMockClient())
	requestURL := "/personas/" + personaID + "/preview?room_id=" + url.QueryEscape(roomID)
	req := httptest.NewRequest(http.MethodPost, requestURL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d, body: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Drafts []PreviewDraft `json:"drafts"`
		Quota  struct {
			Used  int `json:"used"`
			Limit int `json:"limit"`
		} `json:"quota"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	if len(response.Drafts) != 2 {
		t.Fatalf("expected 2 drafts, got %d", len(response.Drafts))
	}
	for i, draft := range response.Drafts {
		if strings.TrimSpace(draft.Content) == "" {
			t.Fatalf("draft %d is empty", i+1)
		}
		if draft.AuthoredBy != "AI" {
			t.Fatalf("draft %d authored_by mismatch: %s", i+1, draft.AuthoredBy)
		}
	}
	if response.Quota.Limit != cfg.DefaultPreviewQuota {
		t.Fatalf("expected quota limit %d, got %d", cfg.DefaultPreviewQuota, response.Quota.Limit)
	}

	var previewEvents int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM quota_events
		WHERE persona_id = $1
		  AND quota_type = 'preview'
	`, personaID).Scan(&previewEvents)
	if err != nil {
		t.Fatalf("query preview quota events failed: %v", err)
	}
	if previewEvents < 1 {
		t.Fatalf("expected at least one preview quota event")
	}
}

func migrationDirForTests(t *testing.T) string {
	t.Helper()

	candidates := []string{
		filepath.Clean(filepath.Join("..", "..", "migrations")),
		filepath.Clean(filepath.Join("migrations")),
	}
	for _, candidate := range candidates {
		if stat, err := os.Stat(candidate); err == nil && stat.IsDir() {
			return candidate
		}
	}
	t.Fatalf("could not locate migrations directory")
	return ""
}
