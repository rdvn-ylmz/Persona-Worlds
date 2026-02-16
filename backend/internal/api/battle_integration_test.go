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

func TestPublicBattleEndpointHidesCalibrationFields(t *testing.T) {
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
	cfg.JWTSecret = "public-battle-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("public-battle-%d@example.com", unique)
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
	`, fmt.Sprintf("public-battle-room-%d", unique), "public-battle-room", "Public battle integration room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one question"})
	doNotSayJSON, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrasesJSON, _ := json.Marshal([]string{"ship, learn, iterate"})

	var personaAID, personaBID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Public Pro", "Argues for speed.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaAID)
	if err != nil {
		t.Fatalf("insert persona A failed: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Public Con", "Argues for certainty.", "analytical", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 2, 5, 25).Scan(&personaBID)
	if err != nil {
		t.Fatalf("insert persona B failed: %v", err)
	}

	verdictJSON, _ := json.Marshal(map[string]any{
		"verdict":   "Both sides made strong claims, but evidence quality favored the FOR side.",
		"takeaways": []string{"Use concrete examples.", "Keep turns concise.", "Address opposing points directly."},
	})

	var battleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status, verdict)
		VALUES ($1, $2, $3, $4, 'DONE', $5::jsonb)
		RETURNING id::text
	`, roomID, "Should startup teams prioritize speed over certainty?", personaAID, personaBID, verdictJSON).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	for i := 1; i <= 6; i++ {
		personaID := personaAID
		if i%2 == 0 {
			personaID = personaBID
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO battle_turns(battle_id, turn_index, persona_id, content)
			VALUES ($1, $2, $3, $4)
		`, battleID, i, personaID, fmt.Sprintf("Claim: turn %d claim.\nEvidence: turn %d evidence.", i, i)); err != nil {
			t.Fatalf("insert battle turn failed: %v", err)
		}
	}

	server := New(cfg, pool, ai.NewMockClient())
	publicReq := httptest.NewRequest(http.MethodGet, "/b/"+battleID, nil)
	publicRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(publicRecorder, publicReq)
	if publicRecorder.Code != http.StatusOK {
		t.Fatalf("expected public battle 200, got %d, body: %s", publicRecorder.Code, publicRecorder.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(publicRecorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response failed: %v", err)
	}

	battleValue, ok := payload["battle"].(map[string]any)
	if !ok {
		t.Fatalf("battle payload missing or invalid")
	}

	personaAValue, ok := battleValue["persona_a"].(map[string]any)
	if !ok {
		t.Fatalf("persona_a payload missing or invalid")
	}
	personaBValue, ok := battleValue["persona_b"].(map[string]any)
	if !ok {
		t.Fatalf("persona_b payload missing or invalid")
	}

	for _, forbidden := range []string{"writing_samples", "do_not_say", "catchphrases"} {
		if _, exists := personaAValue[forbidden]; exists {
			t.Fatalf("persona_a unexpectedly contains %s", forbidden)
		}
		if _, exists := personaBValue[forbidden]; exists {
			t.Fatalf("persona_b unexpectedly contains %s", forbidden)
		}
	}

	turns, ok := battleValue["turns"].([]any)
	if !ok {
		t.Fatalf("turns payload missing or invalid")
	}
	if len(turns) != 6 {
		t.Fatalf("expected 6 turns, got %d", len(turns))
	}
}

func TestPublicBattleSummaryEndpointIntegration(t *testing.T) {
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
	cfg.JWTSecret = "public-battle-summary-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("public-battle-summary-%d@example.com", unique)
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
	`, fmt.Sprintf("public-battle-summary-room-%d", unique), "public-battle-summary-room", "Public battle summary test room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one question"})
	doNotSayJSON, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrasesJSON, _ := json.Marshal([]string{"ship, learn, iterate"})

	var personaAID, personaBID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Summary Pro", "Argues for speed.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaAID)
	if err != nil {
		t.Fatalf("insert persona A failed: %v", err)
	}

	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Summary Con", "Argues for certainty.", "analytical", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 2, 5, 25).Scan(&personaBID)
	if err != nil {
		t.Fatalf("insert persona B failed: %v", err)
	}

	verdictJSON, _ := json.Marshal(map[string]any{
		"verdict":   "Speed arguments were stronger when backed by user evidence.",
		"takeaways": []string{"Cite concrete results.", "Address risks directly.", "Keep turns concise."},
	})

	var battleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status, verdict)
		VALUES ($1, $2, $3, $4, 'DONE', $5::jsonb)
		RETURNING id::text
	`, roomID, "Should startup teams prioritize speed over certainty?", personaAID, personaBID, verdictJSON).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	server := New(cfg, pool, ai.NewMockClient())
	summaryReq := httptest.NewRequest(http.MethodGet, "/b/"+battleID+"/summary", nil)
	summaryRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(summaryRecorder, summaryReq)
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("expected summary 200, got %d, body: %s", summaryRecorder.Code, summaryRecorder.Body.String())
	}

	var summaryResp PublicBattleSummary
	if err := json.Unmarshal(summaryRecorder.Body.Bytes(), &summaryResp); err != nil {
		t.Fatalf("decode summary response failed: %v", err)
	}

	if summaryResp.Topic == "" {
		t.Fatalf("expected non-empty topic")
	}
	if summaryResp.VerdictText == "" {
		t.Fatalf("expected non-empty verdict_text")
	}
	if len(summaryResp.TopTakeaways) == 0 {
		t.Fatalf("expected at least one takeaway")
	}
	if summaryResp.RoomName == "" {
		t.Fatalf("expected non-empty room_name")
	}
}

func TestRegenerateBattleOwnerOnly(t *testing.T) {
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
	cfg.JWTSecret = "battle-regenerate-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("battle-owner-%d@example.com", unique)
	otherEmail := fmt.Sprintf("battle-other-%d@example.com", unique)
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("password hash failed: %v", err)
	}

	var ownerID, otherID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, ownerEmail, hash).Scan(&ownerID)
	if err != nil {
		t.Fatalf("insert owner failed: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, otherEmail, hash).Scan(&otherID)
	if err != nil {
		t.Fatalf("insert other user failed: %v", err)
	}

	var roomID string
	err = pool.QueryRow(ctx, `
		INSERT INTO rooms(slug, name, description)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, fmt.Sprintf("battle-reg-room-%d", unique), "battle-reg-room", "Battle regenerate room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one question"})
	doNotSayJSON, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrasesJSON, _ := json.Marshal([]string{"ship, learn, iterate"})

	var personaAID, personaBID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, ownerID, "Owner A", "Owner persona A.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaAID)
	if err != nil {
		t.Fatalf("insert persona A failed: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, ownerID, "Owner B", "Owner persona B.", "analytical", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 2, 5, 25).Scan(&personaBID)
	if err != nil {
		t.Fatalf("insert persona B failed: %v", err)
	}

	verdictJSON, _ := json.Marshal(map[string]any{
		"verdict":   "Initial verdict.",
		"takeaways": []string{"One", "Two", "Three"},
	})

	var battleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status, verdict)
		VALUES ($1, $2, $3, $4, 'DONE', $5::jsonb)
		RETURNING id::text
	`, roomID, "Should startup teams prioritize speed over certainty?", personaAID, personaBID, verdictJSON).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO battle_turns(battle_id, turn_index, persona_id, content)
		VALUES ($1, 1, $2, $3)
	`, battleID, personaAID, "Claim: initial.\nEvidence: initial example."); err != nil {
		t.Fatalf("insert initial turn failed: %v", err)
	}

	ownerToken, err := auth.CreateToken(cfg.JWTSecret, ownerID)
	if err != nil {
		t.Fatalf("create owner token failed: %v", err)
	}
	otherToken, err := auth.CreateToken(cfg.JWTSecret, otherID)
	if err != nil {
		t.Fatalf("create other token failed: %v", err)
	}

	server := New(cfg, pool, ai.NewMockClient())

	nonOwnerReq := httptest.NewRequest(http.MethodPost, "/battles/"+battleID+"/regenerate", strings.NewReader(`{}`))
	nonOwnerReq.Header.Set("Authorization", "Bearer "+otherToken)
	nonOwnerReq.Header.Set("Content-Type", "application/json")
	nonOwnerRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(nonOwnerRecorder, nonOwnerReq)
	if nonOwnerRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected non-owner regenerate 404, got %d", nonOwnerRecorder.Code)
	}

	ownerReq := httptest.NewRequest(http.MethodPost, "/battles/"+battleID+"/regenerate", strings.NewReader(`{}`))
	ownerReq.Header.Set("Authorization", "Bearer "+ownerToken)
	ownerReq.Header.Set("Content-Type", "application/json")
	ownerRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(ownerRecorder, ownerReq)
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("expected owner regenerate 200, got %d, body: %s", ownerRecorder.Code, ownerRecorder.Body.String())
	}

	var status string
	var turnCount int
	err = pool.QueryRow(ctx, `
		SELECT status::text, (SELECT COUNT(*)::int FROM battle_turns WHERE battle_id = $1)
		FROM battles
		WHERE id = $1
	`, battleID).Scan(&status, &turnCount)
	if err != nil {
		t.Fatalf("load updated battle failed: %v", err)
	}
	if status != "PENDING" {
		t.Fatalf("expected status PENDING, got %s", status)
	}
	if turnCount != 0 {
		t.Fatalf("expected turns cleared on regenerate, got %d", turnCount)
	}
}

func TestBattleRemixEndpointIntegration(t *testing.T) {
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
	cfg.JWTSecret = "battle-remix-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("battle-remix-%d@example.com", unique)
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
	`, fmt.Sprintf("battle-remix-room-%d", unique), "battle-remix-room", "Battle remix test room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamplesJSON, _ := json.Marshal([]string{"Ship small", "Measure outcomes", "Ask one question"})
	doNotSayJSON, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrasesJSON, _ := json.Marshal([]string{"ship, learn, iterate"})

	var personaAID, personaBID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Remix A", "Persona A.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, 5, 25).Scan(&personaAID)
	if err != nil {
		t.Fatalf("insert persona A failed: %v", err)
	}
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(
			user_id, name, bio, tone,
			writing_samples, do_not_say, catchphrases,
			preferred_language, formality,
			daily_draft_quota, daily_reply_quota
		)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text
	`, userID, "Remix B", "Persona B.", "analytical", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 2, 5, 25).Scan(&personaBID)
	if err != nil {
		t.Fatalf("insert persona B failed: %v", err)
	}

	var battleID string
	originalTopic := "Should startup teams prioritize speed over certainty?"
	err = pool.QueryRow(ctx, `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status)
		VALUES ($1, $2, $3, $4, 'DONE')
		RETURNING id::text
	`, roomID, originalTopic, personaAID, personaBID).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	server := New(cfg, pool, ai.NewMockClient())

	unauthReq := httptest.NewRequest(http.MethodPost, "/battles/"+battleID+"/remix", strings.NewReader(`{}`))
	unauthReq.Header.Set("Content-Type", "application/json")
	unauthRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(unauthRecorder, unauthReq)
	if unauthRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauth remix 401, got %d", unauthRecorder.Code)
	}

	var unauthResp struct {
		RemixPayload BattleRemixPayload `json:"remix_payload"`
	}
	if err := json.Unmarshal(unauthRecorder.Body.Bytes(), &unauthResp); err != nil {
		t.Fatalf("decode unauth remix response failed: %v", err)
	}
	if unauthResp.RemixPayload.RoomID != roomID {
		t.Fatalf("expected room_id %s, got %s", roomID, unauthResp.RemixPayload.RoomID)
	}
	if unauthResp.RemixPayload.Topic != originalTopic {
		t.Fatalf("expected topic %q, got %q", originalTopic, unauthResp.RemixPayload.Topic)
	}

	token, err := auth.CreateToken(cfg.JWTSecret, userID)
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	authReq := httptest.NewRequest(http.MethodPost, "/battles/"+battleID+"/remix", strings.NewReader(`{}`))
	authReq.Header.Set("Authorization", "Bearer "+token)
	authReq.Header.Set("Content-Type", "application/json")
	authRecorder := httptest.NewRecorder()
	server.Router().ServeHTTP(authRecorder, authReq)
	if authRecorder.Code != http.StatusOK {
		t.Fatalf("expected auth remix 200, got %d, body: %s", authRecorder.Code, authRecorder.Body.String())
	}

	var authResp struct {
		RemixPayload BattleRemixPayload `json:"remix_payload"`
	}
	if err := json.Unmarshal(authRecorder.Body.Bytes(), &authResp); err != nil {
		t.Fatalf("decode auth remix response failed: %v", err)
	}
	if authResp.RemixPayload.RoomID != roomID {
		t.Fatalf("expected room_id %s, got %s", roomID, authResp.RemixPayload.RoomID)
	}
	if authResp.RemixPayload.Topic != originalTopic {
		t.Fatalf("expected topic %q, got %q", originalTopic, authResp.RemixPayload.Topic)
	}
}
