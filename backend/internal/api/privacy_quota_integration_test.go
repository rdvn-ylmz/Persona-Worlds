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

	"github.com/jackc/pgx/v5/pgxpool"
)

type integrationFixtureOptions struct {
	defaultPreviewQuota int
	dailyDraftQuota     int
	dailyReplyQuota     int
}

type integrationFixture struct {
	ctx       context.Context
	pool      *pgxpool.Pool
	cfg       config.Config
	server    *Server
	userID    string
	token     string
	roomID    string
	personaID string
}

func newIntegrationFixture(t *testing.T, opts integrationFixtureOptions) integrationFixture {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	previewQuota := opts.defaultPreviewQuota
	if previewQuota <= 0 {
		previewQuota = 5
	}
	dailyDraftQuota := opts.dailyDraftQuota
	if dailyDraftQuota <= 0 {
		dailyDraftQuota = 5
	}
	dailyReplyQuota := opts.dailyReplyQuota
	if dailyReplyQuota <= 0 {
		dailyReplyQuota = 25
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("db connect failed: %v", err)
	}
	t.Cleanup(pool.Close)

	cfg := config.Load()
	cfg.DatabaseURL = databaseURL
	cfg.JWTSecret = "privacy-quota-test-secret"
	cfg.DefaultPreviewQuota = previewQuota
	cfg.MigrationsDir = migrationDirForTests(t)

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		t.Fatalf("migrations failed: %v", err)
	}

	unique := time.Now().UnixNano()
	email := fmt.Sprintf("qa-integration-%d@example.com", unique)

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
		fmt.Sprintf("qa-room-%d", unique),
		"qa-room",
		"Room used by QA integration tests.",
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
	`, userID, "QA Persona", "A persona used by integration tests.", "direct", writingSamplesJSON, doNotSayJSON, catchphrasesJSON, "en", 1, dailyDraftQuota, dailyReplyQuota).Scan(&personaID)
	if err != nil {
		t.Fatalf("insert persona failed: %v", err)
	}

	token, err := auth.CreateToken(cfg.JWTSecret, userID)
	if err != nil {
		t.Fatalf("create token failed: %v", err)
	}

	return integrationFixture{
		ctx:       ctx,
		pool:      pool,
		cfg:       cfg,
		server:    New(cfg, pool, ai.NewMockClient()),
		userID:    userID,
		token:     token,
		roomID:    roomID,
		personaID: personaID,
	}
}

func doJSONRequest(server *Server, method, path, token, body string) *httptest.ResponseRecorder {
	var reqBody *strings.Reader
	if body == "" {
		reqBody = strings.NewReader("")
	} else {
		reqBody = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete {
		req.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)
	return recorder
}

func assertNoCalibrationFields(t *testing.T, payload []byte) {
	t.Helper()

	var decoded any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode payload failed: %v", err)
	}

	forbidden := map[string]struct{}{
		"writing_samples": {},
		"do_not_say":      {},
		"catchphrases":    {},
	}

	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if _, exists := forbidden[key]; exists {
					t.Fatalf("response leaked calibration field %q at %s", key, path)
				}
				walk(child, path+"."+key)
			}
		case []any:
			for idx, child := range value {
				walk(child, fmt.Sprintf("%s[%d]", path, idx))
			}
		}
	}

	walk(decoded, "$")
}

func TestPublicProfileDoesNotLeakPersonaCalibrationFieldsIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
	`, fixture.roomID, fixture.personaID, fixture.userID, "Public profile integration post."); err != nil {
		t.Fatalf("insert published post failed: %v", err)
	}

	publishRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/personas/"+fixture.personaID+"/publish-profile",
		fixture.token,
		`{}`,
	)
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

	publicRecorder := doJSONRequest(
		fixture.server,
		http.MethodGet,
		"/p/"+publishResp.Slug,
		"",
		"",
	)
	if publicRecorder.Code != http.StatusOK {
		t.Fatalf("expected public profile 200, got %d, body: %s", publicRecorder.Code, publicRecorder.Body.String())
	}

	assertNoCalibrationFields(t, publicRecorder.Body.Bytes())
}

func TestBattleEndpointDoesNotLeakPersonaCalibrationFieldsIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	var postID string
	err := fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
		RETURNING id::text
	`, fixture.roomID, fixture.personaID, fixture.userID, "Battle integration post.").Scan(&postID)
	if err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO replies(post_id, persona_id, user_id, authored_by, content)
		VALUES ($1, $2, $3, 'AI', $4)
	`, postID, fixture.personaID, fixture.userID, "Battle integration reply."); err != nil {
		t.Fatalf("insert reply failed: %v", err)
	}

	battleRecorder := doJSONRequest(
		fixture.server,
		http.MethodGet,
		"/b/"+postID,
		fixture.token,
		"",
	)
	if battleRecorder.Code != http.StatusOK {
		t.Fatalf("expected battle 200, got %d, body: %s", battleRecorder.Code, battleRecorder.Body.String())
	}

	assertNoCalibrationFields(t, battleRecorder.Body.Bytes())
}

func TestPublicProfilePostsDoesNotLeakPersonaCalibrationFieldsIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
	`, fixture.roomID, fixture.personaID, fixture.userID, "Public posts integration check."); err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	publishRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/personas/"+fixture.personaID+"/publish-profile",
		fixture.token,
		`{}`,
	)
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

	postsRecorder := doJSONRequest(
		fixture.server,
		http.MethodGet,
		"/p/"+publishResp.Slug+"/posts",
		"",
		"",
	)
	if postsRecorder.Code != http.StatusOK {
		t.Fatalf("expected public posts 200, got %d, body: %s", postsRecorder.Code, postsRecorder.Body.String())
	}

	assertNoCalibrationFields(t, postsRecorder.Body.Bytes())
}

func TestPublicBattleMetaDoesNotLeakPersonaCalibrationFieldsIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	var postID string
	err := fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
		RETURNING id::text
	`, fixture.roomID, fixture.personaID, fixture.userID, "Public battle meta integration post.").Scan(&postID)
	if err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	metaRecorder := doJSONRequest(
		fixture.server,
		http.MethodGet,
		"/b/"+postID+"/meta",
		"",
		"",
	)
	if metaRecorder.Code != http.StatusOK {
		t.Fatalf("expected public battle meta 200, got %d, body: %s", metaRecorder.Code, metaRecorder.Body.String())
	}

	assertNoCalibrationFields(t, metaRecorder.Body.Bytes())
}

func TestPreviewQuotaAndBattleDailyLimitIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{
		defaultPreviewQuota: 2,
		dailyDraftQuota:     1,
	})

	previewPath := "/personas/" + fixture.personaID + "/preview?room_id=" + fixture.roomID
	for i := 1; i <= fixture.cfg.DefaultPreviewQuota; i++ {
		previewRecorder := doJSONRequest(fixture.server, http.MethodPost, previewPath, fixture.token, `{}`)
		if previewRecorder.Code != http.StatusOK {
			t.Fatalf("preview request %d expected 200, got %d, body: %s", i, previewRecorder.Code, previewRecorder.Body.String())
		}
	}

	previewLimitRecorder := doJSONRequest(fixture.server, http.MethodPost, previewPath, fixture.token, `{}`)
	if previewLimitRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected preview limit 429, got %d, body: %s", previewLimitRecorder.Code, previewLimitRecorder.Body.String())
	}
	if !strings.Contains(previewLimitRecorder.Body.String(), "daily preview quota reached") {
		t.Fatalf("expected preview limit message, got %s", previewLimitRecorder.Body.String())
	}

	draftPath := "/rooms/" + fixture.roomID + "/posts/draft"
	draftBody := fmt.Sprintf(`{"persona_id":"%s"}`, fixture.personaID)
	firstDraftRecorder := doJSONRequest(fixture.server, http.MethodPost, draftPath, fixture.token, draftBody)
	if firstDraftRecorder.Code != http.StatusCreated {
		t.Fatalf("expected first draft 201, got %d, body: %s", firstDraftRecorder.Code, firstDraftRecorder.Body.String())
	}

	draftLimitRecorder := doJSONRequest(fixture.server, http.MethodPost, draftPath, fixture.token, draftBody)
	if draftLimitRecorder.Code != http.StatusTooManyRequests {
		t.Fatalf("expected draft limit 429, got %d, body: %s", draftLimitRecorder.Code, draftLimitRecorder.Body.String())
	}
	if !strings.Contains(draftLimitRecorder.Body.String(), "daily draft quota reached") {
		t.Fatalf("expected draft limit message, got %s", draftLimitRecorder.Body.String())
	}

	var previewEvents int
	if err := fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM quota_events
		WHERE persona_id = $1
		  AND quota_type = 'preview'
	`, fixture.personaID).Scan(&previewEvents); err != nil {
		t.Fatalf("count preview quota events failed: %v", err)
	}
	if previewEvents != fixture.cfg.DefaultPreviewQuota {
		t.Fatalf("expected %d preview events, got %d", fixture.cfg.DefaultPreviewQuota, previewEvents)
	}

	var draftEvents int
	if err := fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)
		FROM quota_events
		WHERE persona_id = $1
		  AND quota_type = 'draft'
	`, fixture.personaID).Scan(&draftEvents); err != nil {
		t.Fatalf("count draft quota events failed: %v", err)
	}
	if draftEvents != 1 {
		t.Fatalf("expected 1 draft event, got %d", draftEvents)
	}
}
