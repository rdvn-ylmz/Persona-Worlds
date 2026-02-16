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

func TestGenerateWeeklyDigestForOneUser(t *testing.T) {
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
	cfg.JWTSecret = "weekly-digest-worker-test-secret"
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	ownerEmail := fmt.Sprintf("weekly-owner-%d@example.com", unique)
	followerEmail := fmt.Sprintf("weekly-follower-%d@example.com", unique)

	ownerHash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("owner hash failed: %v", err)
	}
	followerHash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("follower hash failed: %v", err)
	}

	var ownerUserID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, ownerEmail, ownerHash).Scan(&ownerUserID)
	if err != nil {
		t.Fatalf("insert owner user failed: %v", err)
	}

	var followerUserID string
	err = pool.QueryRow(ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, followerEmail, followerHash).Scan(&followerUserID)
	if err != nil {
		t.Fatalf("insert follower user failed: %v", err)
	}

	var roomID string
	err = pool.QueryRow(ctx, `
		INSERT INTO rooms(slug, name, description)
		VALUES ($1, $2, $3)
		RETURNING id::text
	`, fmt.Sprintf("weekly-room-%d", unique), "weekly-room", "Weekly digest worker room").Scan(&roomID)
	if err != nil {
		t.Fatalf("insert room failed: %v", err)
	}

	var personaID string
	err = pool.QueryRow(ctx, `
		INSERT INTO personas(user_id, name, bio, tone, daily_draft_quota, daily_reply_quota)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, ownerUserID, "Weekly Persona", "Shares practical updates.", "direct", 5, 25).Scan(&personaID)
	if err != nil {
		t.Fatalf("insert persona failed: %v", err)
	}

	if _, err := pool.Exec(ctx, `
		INSERT INTO persona_follows(follower_user_id, followed_persona_id)
		VALUES ($1, $2)
	`, followerUserID, personaID); err != nil {
		t.Fatalf("insert follow failed: %v", err)
	}

	battleIDs := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		var battleID string
		content := fmt.Sprintf("Topic: Weekly candidate %d\nBattle opening: this is candidate %d.", i+1, i+1)
		err := pool.QueryRow(ctx, `
			INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at, created_at)
			VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW(), NOW() - ($5 || ' hours')::interval)
			RETURNING id::text
		`, roomID, personaID, ownerUserID, content, i*6).Scan(&battleID)
		if err != nil {
			t.Fatalf("insert battle %d failed: %v", i+1, err)
		}
		battleIDs = append(battleIDs, battleID)
	}

	for idx, battleID := range battleIDs {
		shareMetadata, _ := json.Marshal(map[string]any{"battle_id": battleID})
		for i := 0; i <= idx; i++ {
			if _, err := pool.Exec(ctx, `
				INSERT INTO events(user_id, event_name, metadata)
				VALUES ($1, 'battle_shared', $2::jsonb)
			`, ownerUserID, shareMetadata); err != nil {
				t.Fatalf("insert share event failed: %v", err)
			}
		}
	}

	remixMetadata, _ := json.Marshal(map[string]any{"source_battle_id": battleIDs[0], "battle_id": battleIDs[0]})
	if _, err := pool.Exec(ctx, `
		INSERT INTO events(user_id, event_name, metadata)
		VALUES ($1, 'remix_completed', $2::jsonb)
	`, ownerUserID, remixMetadata); err != nil {
		t.Fatalf("insert remix event failed: %v", err)
	}

	worker := New(cfg, pool, ai.NewMockClient())
	weekStart := startOfWeekUTC(time.Now().UTC())

	generated := false
	for i := 0; i < 12; i++ {
		if err := worker.generateWeeklyDigestForOneUser(ctx); err != nil {
			t.Fatalf("generate weekly digest failed: %v", err)
		}

		var exists bool
		err := pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1
				FROM weekly_digests
				WHERE user_id = $1
				  AND week_start = $2::date
			)
		`, followerUserID, weekStart.Format("2006-01-02")).Scan(&exists)
		if err != nil {
			t.Fatalf("check weekly digest existence failed: %v", err)
		}
		if exists {
			generated = true
			break
		}
	}

	if !generated {
		t.Fatalf("weekly digest was not generated for follower user")
	}

	var itemsRaw []byte
	err = pool.QueryRow(ctx, `
		SELECT items
		FROM weekly_digests
		WHERE user_id = $1
		  AND week_start = $2::date
	`, followerUserID, weekStart.Format("2006-01-02")).Scan(&itemsRaw)
	if err != nil {
		t.Fatalf("load weekly digest items failed: %v", err)
	}

	var items []weeklyDigestItem
	if err := json.Unmarshal(itemsRaw, &items); err != nil {
		t.Fatalf("decode weekly digest items failed: %v", err)
	}
	if len(items) == 0 {
		t.Fatalf("expected at least one weekly digest item")
	}
	if len(items) > 3 {
		t.Fatalf("expected at most 3 weekly digest items, got %d", len(items))
	}

	for idx, item := range items {
		if item.BattleID == "" {
			t.Fatalf("item %d battle id is empty", idx)
		}
		if item.Summary == "" {
			t.Fatalf("item %d summary is empty", idx)
		}
	}
}
