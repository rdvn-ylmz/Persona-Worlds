package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
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
