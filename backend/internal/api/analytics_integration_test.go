package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAnalyticsTrackingAndSummaryIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	createPersonaBody := `{
		"name":"Analytics Persona",
		"bio":"Tracks funnel events safely.",
		"tone":"direct",
		"writing_samples":["Ship and learn.","Share practical results.","Ask one concrete question."],
		"do_not_say":["guaranteed growth"],
		"catchphrases":["ship, learn, iterate"],
		"preferred_language":"en",
		"formality":1,
		"daily_draft_quota":5,
		"daily_reply_quota":25
	}`
	createPersonaRecorder := doJSONRequest(fixture.server, http.MethodPost, "/personas", fixture.token, createPersonaBody)
	if createPersonaRecorder.Code != http.StatusCreated {
		t.Fatalf("expected create persona 201, got %d, body: %s", createPersonaRecorder.Code, createPersonaRecorder.Body.String())
	}

	var createdPersona Persona
	if err := json.Unmarshal(createPersonaRecorder.Body.Bytes(), &createdPersona); err != nil {
		t.Fatalf("decode created persona failed: %v", err)
	}
	if strings.TrimSpace(createdPersona.ID) == "" {
		t.Fatalf("created persona id is empty")
	}

	previewPath := "/personas/" + createdPersona.ID + "/preview?room_id=" + fixture.roomID
	previewRecorder := doJSONRequest(fixture.server, http.MethodPost, previewPath, fixture.token, `{}`)
	if previewRecorder.Code != http.StatusOK {
		t.Fatalf("expected preview 200, got %d, body: %s", previewRecorder.Code, previewRecorder.Body.String())
	}

	draftBody := fmt.Sprintf(`{"persona_id":"%s"}`, createdPersona.ID)
	draftRecorder := doJSONRequest(fixture.server, http.MethodPost, "/rooms/"+fixture.roomID+"/posts/draft", fixture.token, draftBody)
	if draftRecorder.Code != http.StatusCreated {
		t.Fatalf("expected draft 201, got %d, body: %s", draftRecorder.Code, draftRecorder.Body.String())
	}

	var draft Post
	if err := json.Unmarshal(draftRecorder.Body.Bytes(), &draft); err != nil {
		t.Fatalf("decode draft response failed: %v", err)
	}
	if strings.TrimSpace(draft.ID) == "" {
		t.Fatalf("draft id is empty")
	}

	approveRecorder := doJSONRequest(fixture.server, http.MethodPost, "/posts/"+draft.ID+"/approve", fixture.token, `{}`)
	if approveRecorder.Code != http.StatusOK {
		t.Fatalf("expected approve 200, got %d, body: %s", approveRecorder.Code, approveRecorder.Body.String())
	}

	generateRepliesBody := fmt.Sprintf(`{"persona_ids":["%s"]}`, fixture.personaID)
	generateRepliesRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/posts/"+draft.ID+"/generate-replies",
		fixture.token,
		generateRepliesBody,
	)
	if generateRepliesRecorder.Code != http.StatusOK {
		t.Fatalf("expected generate replies 200, got %d, body: %s", generateRepliesRecorder.Code, generateRepliesRecorder.Body.String())
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

	publicRecorder := doJSONRequest(fixture.server, http.MethodGet, "/p/"+publishResp.Slug, "", "")
	if publicRecorder.Code != http.StatusOK {
		t.Fatalf("expected public profile 200, got %d, body: %s", publicRecorder.Code, publicRecorder.Body.String())
	}

	shareEventRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/events",
		fixture.token,
		`{"event_name":"battle_shared","metadata":{"surface":"dashboard"}}`,
	)
	if shareEventRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected share event 202, got %d, body: %s", shareEventRecorder.Code, shareEventRecorder.Body.String())
	}

	remixEventRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/events",
		"",
		fmt.Sprintf(`{"event_name":"remix_click","metadata":{"slug":"%s","surface":"public_profile"}}`, publishResp.Slug),
	)
	if remixEventRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected remix event 202, got %d, body: %s", remixEventRecorder.Code, remixEventRecorder.Body.String())
	}

	followEventRecorder := doJSONRequest(
		fixture.server,
		http.MethodPost,
		"/events",
		"",
		fmt.Sprintf(`{"event_name":"follow_click","metadata":{"slug":"%s","surface":"public_profile"}}`, publishResp.Slug),
	)
	if followEventRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected follow event 202, got %d, body: %s", followEventRecorder.Code, followEventRecorder.Body.String())
	}

	signupBody := fmt.Sprintf(
		`{"email":"signup-from-share-%d@example.com","password":"password123","share_slug":"%s"}`,
		time.Now().UnixNano(),
		publishResp.Slug,
	)
	signupRecorder := doJSONRequest(fixture.server, http.MethodPost, "/auth/signup", "", signupBody)
	if signupRecorder.Code != http.StatusCreated {
		t.Fatalf("expected signup 201, got %d, body: %s", signupRecorder.Code, signupRecorder.Body.String())
	}

	summaryRecorder := doJSONRequest(fixture.server, http.MethodGet, "/admin/analytics/summary", fixture.token, "")
	if summaryRecorder.Code != http.StatusOK {
		t.Fatalf("expected summary 200, got %d, body: %s", summaryRecorder.Code, summaryRecorder.Body.String())
	}

	var summary struct {
		Last24h map[string]int `json:"last_24h"`
		Last7d  map[string]int `json:"last_7d"`
	}
	if err := json.Unmarshal(summaryRecorder.Body.Bytes(), &summary); err != nil {
		t.Fatalf("decode summary failed: %v", err)
	}

	requiredEvents := []string{
		eventPersonaCreated,
		eventPreviewGenerated,
		eventPostApproved,
		eventBattleCreated,
		eventBattleShared,
		eventPublicProfileViewed,
		eventSignupFromShare,
	}
	for _, eventName := range requiredEvents {
		if summary.Last24h[eventName] < 1 {
			t.Fatalf("expected last_24h[%s] >= 1, got %d", eventName, summary.Last24h[eventName])
		}
		if summary.Last7d[eventName] < 1 {
			t.Fatalf("expected last_7d[%s] >= 1, got %d", eventName, summary.Last7d[eventName])
		}
	}

	var userScopedEvents int
	if err := fixture.pool.QueryRow(fixture.ctx, `
		SELECT COUNT(*)::int
		FROM events
		WHERE event_name = $1
		  AND user_id = $2
	`, eventBattleShared, fixture.userID).Scan(&userScopedEvents); err != nil {
		t.Fatalf("count user scoped share events failed: %v", err)
	}
	if userScopedEvents < 1 {
		t.Fatalf("expected at least one user-scoped %s event", eventBattleShared)
	}
}
