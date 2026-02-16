package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/auth"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
)

func TestPublicPersonaProfileFlowIntegration(t *testing.T) {
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
	cfg.JWTSecret = "public-profile-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("owner-public-%d@example.com", unique)
	followerEmail := fmt.Sprintf("follower-public-%d@example.com", unique)

	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("password hash failed: %v", err)
	}

	var ownerID, followerID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, ownerEmail, hash).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert owner user failed: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, followerEmail, hash).Scan(&followerID)
	if err != nil {
		t.Fatalf("insert follower user failed: %v", err)
	}

	var roomID string
	err = pool.QueryRow(ctx, `
		INSERT INTO rooms(slug, name, description)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, fmt.Sprintf("public-room-%d", unique), "public-room", "Public profile integration room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one question"})
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
	`, ownerID, "Public Persona", "A persona ready to be shared.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaID)
	if err != nil {
		t.Fatalf("insert persona failed: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
	`, roomID, personaID, ownerID, "Public profile post for visitor feed."); err != nil {
		t.Fatalf("insert published post failed: %v", err)
	}

	ownerToken, err := auth.CreateToken(cfg.JWTSecret, ownerID)
	if err != nil {
		t.Fatalf("create owner token failed: %v", err)
	}
	followerToken, err := auth.CreateToken(cfg.JWTSecret, followerID)
	if err != nil {
		t.Fatalf("create follower token failed: %v", err)
	}

	server := New(cfg, pool, ai.NewMockClient())

	publishReq := httptest.NewRequest(http.MethodPost, "/personas/"+personaID+"/publish-profile", strings.NewReader(`{}`))
	publishReq.Header.Set("Authorization", "Bearer "+ownerToken)
	publishReq.Header.Set("Content-Type", "application/json")
	publishRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(publishRecorder, publishReq)
	if publishRecorder.Code != http.StatusOK {
		t.Fatalf("expected publish 200, got %d, body: %s", publishRecorder.Code, publishRecorder.Body.String())
	}

	var publishResp struct {
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(publishRecorder.Body.Bytes(), &publishResp); err != nil {
		t.Fatalf("decode publish response failed: %v", err)
	}
	if strings.TrimSpace(publishResp.Slug) == "" {
		t.Fatalf("publish slug is empty")
	}

	publicReq := httptest.NewRequest(http.MethodGet, "/p/"+publishResp.Slug, nil)
	publicRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(publicRecorder, publicReq)
	if publicRecorder.Code != http.StatusOK {
		t.Fatalf("expected public profile 200, got %d, body: %s", publicRecorder.Code, publicRecorder.Body.String())
	}

	var publicResp struct {
		Profile struct {
			PersonaID string `json:"persona_id"`
		} `json:"profile"`
		LatestPosts []PublicPost     `json:"latest_posts"`
		TopRooms    []PublicRoomStat `json:"top_rooms"`
	}
	if err := json.Unmarshal(publicRecorder.Body.Bytes(), &publicResp); err != nil {
		t.Fatalf("decode public response failed: %v", err)
	}
	if publicResp.Profile.PersonaID != personaID {
		t.Fatalf("persona mismatch in public profile")
	}
	if len(publicResp.LatestPosts) == 0 {
		t.Fatalf("expected at least one latest post")
	}
	if len(publicResp.TopRooms) == 0 {
		t.Fatalf("expected at least one top room")
	}

	followNoAuthReq := httptest.NewRequest(http.MethodPost, "/p/"+publishResp.Slug+"/follow", strings.NewReader(`{}`))
	followNoAuthReq.Header.Set("Content-Type", "application/json")
	followNoAuthRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(followNoAuthRecorder, followNoAuthReq)
	if followNoAuthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected follow without auth 401, got %d", followNoAuthRecorder.Code)
	}
	if !strings.Contains(followNoAuthRecorder.Body.String(), "signup_required") {
		t.Fatalf("expected signup_required error, got %s", followNoAuthRecorder.Body.String())
	}

	followReq := httptest.NewRequest(http.MethodPost, "/p/"+publishResp.Slug+"/follow", strings.NewReader(`{}`))
	followReq.Header.Set("Authorization", "Bearer "+followerToken)
	followReq.Header.Set("Content-Type", "application/json")
	followRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(followRecorder, followReq)
	if followRecorder.Code != http.StatusOK {
		t.Fatalf("expected follow 200, got %d, body: %s", followRecorder.Code, followRecorder.Body.String())
	}

	var followResp struct {
		Followed bool `json:"followed"`
	}
	if err := json.Unmarshal(followRecorder.Body.Bytes(), &followResp); err != nil {
		t.Fatalf("decode follow response failed: %v", err)
	}
	if !followResp.Followed {
		t.Fatalf("expected followed=true on first follow")
	}
}
