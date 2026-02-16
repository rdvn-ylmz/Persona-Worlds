package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/auth"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
)

func TestGenerateDigestForOnePersona(t *testing.T) {
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
	cfg.JWTSecret = "digest-worker-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)
	cfg.SummaryMaxLen = 400

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("digest-worker-%d@example.com", unique)
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
	`, fmt.Sprintf("digest-room-%d", unique), "digest-room", "Digest worker test room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	writingSamples, _ := json.Marshal([]string{"Ship small", "Measure impact", "Ask one question"})
	doNotSay, _ := json.Marshal([]string{"guaranteed growth"})
	catchphrases, _ := json.Marshal([]string{"ship, learn, iterate"})

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
	`, userID, "Digest Persona", "Tracks activity.", "direct", writingSamples, doNotSay, catchphrases, "en", 1, 5, 25).Scan(&personaID)
	if err != nil {
		t.Fatalf("insert persona failed: %v", err)
	}

	var postID string
	err = pool.QueryRow(ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
		RETURNING id::text
	`, roomID, personaID, userID, "Shipping weekly experiments improved feedback quality.").Scan(&postID)
	if err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	metadataRaw, _ := json.Marshal(map[string]any{
		"post_id":      postID,
		"room_id":      roomID,
		"post_preview": "Shipping weekly experiments improved feedback quality.",
	})

	for _, eventType := range []string{"post_created", "reply_generated", "thread_participated", "thread_participated"} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO persona_activity_events(persona_id, type, metadata)
			VALUES ($1, $2, $3::jsonb)
		`, personaID, eventType, metadataRaw); err != nil {
			t.Fatalf("insert activity event failed: %v", err)
		}
	}

	worker := New(cfg, pool, ai.NewMockClient())
	generated := false
	for i := 0; i < 20; i++ {
		if err := worker.generateDigestForOnePersona(ctx); err != nil {
			t.Fatalf("generate digest failed: %v", err)
		}

		var exists bool
		if err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM persona_digests
				WHERE persona_id = $1
				  AND date = CURRENT_DATE
			)
		`, personaID).Scan(&exists); err != nil {
			t.Fatalf("query digest existence failed: %v", err)
		}
		if exists {
			generated = true
			break
		}
	}
	if !generated {
		t.Fatalf("digest was not generated for persona")
	}

	var (
		summary  string
		statsRaw []byte
	)
	err = pool.QueryRow(ctx, `
		SELECT summary, stats
		FROM persona_digests
		WHERE persona_id = $1
		  AND date = CURRENT_DATE
	`, personaID).Scan(&summary, &statsRaw)
	if err != nil {
		t.Fatalf("load digest failed: %v", err)
	}
	if len(summary) == 0 {
		t.Fatalf("digest summary is empty")
	}

	var stats struct {
		Posts      int `json:"posts"`
		Replies    int `json:"replies"`
		TopThreads []struct {
			PostID        string `json:"post_id"`
			ActivityCount int    `json:"activity_count"`
		} `json:"top_threads"`
	}
	if err := json.Unmarshal(statsRaw, &stats); err != nil {
		t.Fatalf("decode stats failed: %v", err)
	}

	if stats.Posts != 1 {
		t.Fatalf("expected posts=1, got %d", stats.Posts)
	}
	if stats.Replies != 1 {
		t.Fatalf("expected replies=1, got %d", stats.Replies)
	}
	if len(stats.TopThreads) == 0 {
		t.Fatalf("expected at least one top thread")
	}
	if stats.TopThreads[0].PostID != postID {
		t.Fatalf("expected top thread post id %s, got %s", postID, stats.TopThreads[0].PostID)
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
