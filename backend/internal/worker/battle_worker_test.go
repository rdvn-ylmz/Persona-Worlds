package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/auth"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
)

func TestGenerateBattleForOnePending(t *testing.T) {
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
	cfg.JWTSecret = "battle-worker-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("battle-worker-%d@example.com", unique)
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password failed: %v", err)
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
	`, fmt.Sprintf("battle-room-%d", unique), "battle-room", "Battle worker test room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamples, _ := json.Marshal([]string{"Ship small", "Measure impact", "Ask one question"})
	doNotSay, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrases, _ := json.Marshal([]string{"ship, learn, iterate"})

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
	`, userID, "Pro Persona", "Prefers practical execution.", "direct", writingSamples, doNotSay, catchphrases, "en", 1, 5, 25).Scan(&personaAID)
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
	`, userID, "Con Persona", "Prefers risk controls.", "skeptical", writingSamples, doNotSay, catchphrases, "en", 2, 5, 25).Scan(&personaBID)
	if err != nil {
		t.Fatalf("insert persona B failed: %v", err)
	}

	var battleID string
	err = pool.QueryRow(ctx, `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status)
		VALUES ($1, $2, $3, $4, 'PENDING')
		RETURNING id::text
	`, roomID, "Should startup teams optimize for speed over certainty?", personaAID, personaBID).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	worker := New(cfg, pool, ai.NewMockClient())
	if err := worker.generateBattleForOnePending(ctx); err != nil {
		t.Fatalf("generate battle failed: %v", err)
	}

	var (
		status    string
		verdictRaw []byte
	)
	err = pool.QueryRow(ctx, `
		SELECT status::text, verdict
		FROM battles
		WHERE id = $1
	`, battleID).Scan(&status, &verdictRaw)
	if err != nil {
		t.Fatalf("load battle failed: %v", err)
	}
	if status != "DONE" {
		t.Fatalf("expected status DONE, got %s", status)
	}

	var turnCount int
	err = pool.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM battle_turns
		WHERE battle_id = $1
	`, battleID).Scan(&turnCount)
	if err != nil {
		t.Fatalf("count battle turns failed: %v", err)
	}
	if turnCount != 6 {
		t.Fatalf("expected 6 turns, got %d", turnCount)
	}

	var verdict struct {
		Verdict   string   `json:"verdict"`
		Takeaways []string `json:"takeaways"`
	}
	if err := json.Unmarshal(verdictRaw, &verdict); err != nil {
		t.Fatalf("decode verdict failed: %v", err)
	}
	if verdict.Verdict == "" {
		t.Fatalf("expected non-empty verdict")
	}
	if len(verdict.Takeaways) != 3 {
		t.Fatalf("expected 3 takeaways, got %d", len(verdict.Takeaways))
	}
}
