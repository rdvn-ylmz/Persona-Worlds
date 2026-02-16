package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"personaworlds/backend/internal/auth"
)

func TestFeedIncludesFollowedTrendingAndTemplatesIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	otherUserID, _, err := createIntegrationUser(fixture, fmt.Sprintf("feed-other-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("create other user failed: %v", err)
	}

	var otherPersonaID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO personas(user_id, name, bio, tone, daily_draft_quota, daily_reply_quota)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text
	`, otherUserID, "Feed Persona", "Publishes battles for feed tests.", "direct", 5, 25).Scan(&otherPersonaID)
	if err != nil {
		t.Fatalf("insert other persona failed: %v", err)
	}

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO persona_follows(follower_user_id, followed_persona_id)
		VALUES ($1, $2)
	`, fixture.userID, otherPersonaID); err != nil {
		t.Fatalf("insert follow failed: %v", err)
	}

	var templateID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO templates(owner_user_id, name, prompt_rules, turn_count, word_limit, is_public)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		RETURNING id::text
	`, otherUserID, "Feed Template", "Alternate concise arguments.", 6, 120).Scan(&templateID)
	if err != nil {
		t.Fatalf("insert template failed: %v", err)
	}

	var battleID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at, template_id)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW(), $5)
		RETURNING id::text
	`, fixture.roomID, otherPersonaID, otherUserID, "Topic: Should teams ship a changelog every week?", templateID).Scan(&battleID)
	if err != nil {
		t.Fatalf("insert battle failed: %v", err)
	}

	shareMetadata, _ := json.Marshal(map[string]any{"battle_id": battleID})
	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO events(user_id, event_name, metadata)
		VALUES ($1, 'battle_shared', $2::jsonb), ($1, 'battle_shared', $2::jsonb)
	`, otherUserID, shareMetadata); err != nil {
		t.Fatalf("insert share events failed: %v", err)
	}

	remixMetadata, _ := json.Marshal(map[string]any{"source_battle_id": battleID, "battle_id": battleID})
	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO events(user_id, event_name, metadata)
		VALUES ($1, 'remix_completed', $2::jsonb)
	`, otherUserID, remixMetadata); err != nil {
		t.Fatalf("insert remix events failed: %v", err)
	}

	recorder := doJSONRequest(fixture.server, http.MethodGet, "/feed", fixture.token, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected feed 200, got %d, body: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Items []struct {
			Kind    string   `json:"kind"`
			Reasons []string `json:"reasons"`
			Battle  *struct {
				BattleID string `json:"battle_id"`
			} `json:"battle"`
			Template *struct {
				TemplateID string `json:"template_id"`
			} `json:"template"`
		} `json:"items"`
		HighlightTemplate *struct {
			TemplateID string `json:"template_id"`
		} `json:"highlight_template"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode feed response failed: %v", err)
	}
	if len(response.Items) == 0 {
		t.Fatalf("expected feed items")
	}

	foundFollowed := false
	foundTrending := false
	foundTemplate := false

	for _, item := range response.Items {
		if item.Kind == "battle" && item.Battle != nil && item.Battle.BattleID == battleID {
			if stringSliceContains(item.Reasons, "followed_persona") {
				foundFollowed = true
			}
			if stringSliceContains(item.Reasons, "trending_battle") {
				foundTrending = true
			}
		}
		if item.Kind == "template" && item.Template != nil {
			if item.Template.TemplateID == templateID {
				foundTemplate = true
			}
		}
	}

	if !foundFollowed {
		t.Fatalf("expected followed_persona battle in feed")
	}
	if !foundTrending {
		t.Fatalf("expected trending_battle battle in feed")
	}
	if !foundTemplate {
		t.Fatalf("expected new template in feed")
	}
	if response.HighlightTemplate == nil || strings.TrimSpace(response.HighlightTemplate.TemplateID) == "" {
		t.Fatalf("expected highlight_template")
	}
}

func TestNotificationsLifecycleIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	actorUserID, actorToken, err := createIntegrationUser(fixture, fmt.Sprintf("notif-actor-%d@example.com", time.Now().UnixNano()))
	if err != nil {
		t.Fatalf("create actor user failed: %v", err)
	}

	publishRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/personas/"+fixture.personaID+"/publish-profile",
		fixture.token,
		`{}`,
	)
	if publishRecorder.Code != http.StatusOK {
		t.Fatalf("publish profile expected 200, got %d, body: %s", publishRecorder.Code, publishRecorder.Body.String())
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

	followRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/p/"+publishResp.Slug+"/follow",
		actorToken,
		`{}`,
	)
	if followRecorder.Code != http.StatusOK {
		t.Fatalf("follow expected 200, got %d, body: %s", followRecorder.Code, followRecorder.Body.String())
	}

	var templateID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO templates(owner_user_id, name, prompt_rules, turn_count, word_limit, is_public)
		VALUES ($1, $2, $3, $4, $5, TRUE)
		RETURNING id::text
	`, fixture.userID, "Owner Template", "Debate with one claim per turn.", 6, 120).Scan(&templateID)
	if err != nil {
		t.Fatalf("insert owner template failed: %v", err)
	}

	var sourceBattleID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO posts(room_id, user_id, authored_by, status, content, published_at, template_id)
		VALUES ($1, $2, 'HUMAN', 'PUBLISHED', $3, NOW(), $4)
		RETURNING id::text
	`, fixture.roomID, fixture.userID, "Topic: Should async standups be default for remote teams?", templateID).Scan(&sourceBattleID)
	if err != nil {
		t.Fatalf("insert source battle failed: %v", err)
	}

	intentRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/battles/"+sourceBattleID+"/remix-intent",
		actorToken,
		`{}`,
	)
	if intentRecorder.Code != http.StatusOK {
		t.Fatalf("remix intent expected 200, got %d, body: %s", intentRecorder.Code, intentRecorder.Body.String())
	}

	var intentResp struct {
		RemixToken string `json:"remix_token"`
	}
	if err := json.Unmarshal(intentRecorder.Body.Bytes(), &intentResp); err != nil {
		t.Fatalf("decode remix intent failed: %v", err)
	}
	if strings.TrimSpace(intentResp.RemixToken) == "" {
		t.Fatalf("remix token is empty")
	}

	createBattleBody := fmt.Sprintf(`{"topic":"Remix notification integration battle","template_id":"%s","remix_token":"%s"}`,
		templateID,
		intentResp.RemixToken,
	)
	createBattleRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/rooms/"+fixture.roomID+"/battles",
		actorToken,
		createBattleBody,
	)
	if createBattleRecorder.Code != http.StatusCreated {
		t.Fatalf("create battle expected 201, got %d, body: %s", createBattleRecorder.Code, createBattleRecorder.Body.String())
	}

	notificationsRecorder := doJSONRequest(fixture.server, http.MethodGet, "/notifications", fixture.token, "")
	if notificationsRecorder.Code != http.StatusOK {
		t.Fatalf("notifications expected 200, got %d, body: %s", notificationsRecorder.Code, notificationsRecorder.Body.String())
	}

	var notificationsResp struct {
		Notifications []struct {
			ID   int64  `json:"id"`
			Type string `json:"type"`
		} `json:"notifications"`
		UnreadCount int `json:"unread_count"`
	}
	if err := json.Unmarshal(notificationsRecorder.Body.Bytes(), &notificationsResp); err != nil {
		t.Fatalf("decode notifications failed: %v", err)
	}
	if notificationsResp.UnreadCount < 3 {
		t.Fatalf("expected unread notifications >= 3, got %d", notificationsResp.UnreadCount)
	}

	types := map[string]bool{}
	for _, notification := range notificationsResp.Notifications {
		types[notification.Type] = true
	}
	for _, expectedType := range []string{notificationTypePersonaFollow, notificationTypeTemplateUsed, notificationTypeBattleRemixed} {
		if !types[expectedType] {
			t.Fatalf("expected notification type %s", expectedType)
		}
	}

	if len(notificationsResp.Notifications) == 0 {
		t.Fatalf("expected notifications to be present")
	}
	firstID := notificationsResp.Notifications[0].ID
	markReadRecorder := doJSONRequest(fixture.server, http.MethodPost, fmt.Sprintf("/notifications/%d/read", firstID), fixture.token, `{}`)
	if markReadRecorder.Code != http.StatusOK {
		t.Fatalf("mark read expected 200, got %d, body: %s", markReadRecorder.Code, markReadRecorder.Body.String())
	}

	var markReadResp struct {
		UnreadCount int `json:"unread_count"`
	}
	if err := json.Unmarshal(markReadRecorder.Body.Bytes(), &markReadResp); err != nil {
		t.Fatalf("decode mark read failed: %v", err)
	}
	if markReadResp.UnreadCount != notificationsResp.UnreadCount-1 {
		t.Fatalf("expected unread count %d after mark read, got %d", notificationsResp.UnreadCount-1, markReadResp.UnreadCount)
	}

	if strings.TrimSpace(actorUserID) == "" {
		t.Fatalf("actor user id should not be empty")
	}
}

func TestWeeklyDigestEndpointIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	weekStart := startOfWeekUTC(time.Now().UTC())
	itemsJSON, _ := json.Marshal([]WeeklyDigestItem{
		{
			BattleID:  "battle-123",
			RoomID:    fixture.roomID,
			RoomName:  "qa-room",
			Topic:     "Weekly digest topic",
			Summary:   "One concise weekly summary sentence.",
			Score:     98.5,
			CreatedAt: time.Now().UTC().Add(-24 * time.Hour),
		},
	})

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO weekly_digests(user_id, week_start, items)
		VALUES ($1, $2::date, $3::jsonb)
		ON CONFLICT (user_id, week_start)
		DO UPDATE SET items = EXCLUDED.items, updated_at = NOW()
	`, fixture.userID, weekStart.Format("2006-01-02"), itemsJSON); err != nil {
		t.Fatalf("insert weekly digest failed: %v", err)
	}

	recorder := doJSONRequest(fixture.server, http.MethodGet, "/digest/weekly", fixture.token, "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("weekly digest expected 200, got %d, body: %s", recorder.Code, recorder.Body.String())
	}

	var response struct {
		Digest struct {
			Items []WeeklyDigestItem `json:"items"`
		} `json:"digest"`
		Exists        bool `json:"exists"`
		IsCurrentWeek bool `json:"is_current_week"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode weekly digest response failed: %v", err)
	}
	if !response.Exists {
		t.Fatalf("expected exists=true")
	}
	if !response.IsCurrentWeek {
		t.Fatalf("expected is_current_week=true")
	}
	if len(response.Digest.Items) != 1 {
		t.Fatalf("expected 1 weekly digest item, got %d", len(response.Digest.Items))
	}
	if strings.TrimSpace(response.Digest.Items[0].Summary) == "" {
		t.Fatalf("expected non-empty summary")
	}
}

func createIntegrationUser(fixture integrationFixture, email string) (string, string, error) {
	hash, err := auth.HashPassword("password123")
	if err != nil {
		return "", "", err
	}

	var userID string
	err = fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, email, hash).Scan(&userID)
	if err != nil {
		return "", "", err
	}

	token, err := auth.CreateToken(fixture.cfg.JWTSecret, userID)
	if err != nil {
		return "", "", err
	}
	return userID, token, nil
}

func stringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == strings.TrimSpace(target) {
			return true
		}
	}
	return false
}
