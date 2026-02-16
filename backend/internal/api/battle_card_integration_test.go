package api

import (
	"bytes"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

func TestBattleCardImageEndpointIntegration(t *testing.T) {
	fixture := newIntegrationFixture(t, integrationFixtureOptions{})

	var postID string
	err := fixture.pool.QueryRow(fixture.ctx, `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at)
		VALUES ($1, $2, $3, 'AI_DRAFT_APPROVED', 'PUBLISHED', $4, NOW())
		RETURNING id::text
	`, fixture.roomID, fixture.personaID, fixture.userID, "Battle card integration topic: should we prioritize speed over polish?").Scan(&postID)
	if err != nil {
		t.Fatalf("insert post failed: %v", err)
	}

	if _, err := fixture.pool.Exec(fixture.ctx, `
		INSERT INTO replies(post_id, persona_id, user_id, authored_by, content)
		VALUES ($1, $2, $3, 'AI', $4)
	`, postID, fixture.personaID, fixture.userID, "Move fast but keep one quality gate so learning stays trustworthy."); err != nil {
		t.Fatalf("insert reply failed: %v", err)
	}

	recorder := doJSONRequest(fixture.server, http.MethodGet, "/b/"+postID+"/card.png", "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected card endpoint 200, got %d, body: %s", recorder.Code, recorder.Body.String())
	}

	contentType := strings.TrimSpace(recorder.Header().Get("Content-Type"))
	if !strings.HasPrefix(contentType, "image/png") {
		t.Fatalf("expected content-type image/png, got %q", contentType)
	}
	if recorder.Body.Len() == 0 {
		t.Fatalf("expected non-empty card body")
	}

	pngSignature := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	if !bytes.HasPrefix(recorder.Body.Bytes(), pngSignature) {
		t.Fatalf("card body is not a valid png signature")
	}

	etag := recorder.Header().Get("ETag")
	if strings.TrimSpace(etag) == "" {
		t.Fatalf("expected etag header")
	}
	if !strings.Contains(etag, postID) {
		t.Fatalf("expected etag to contain post id, got %s", etag)
	}

	cacheControl := recorder.Header().Get("Cache-Control")
	if !strings.Contains(cacheControl, "max-age") {
		t.Fatalf("expected cache-control max-age, got %s", cacheControl)
	}

	// Request again to ensure cached path is valid and still returns image bytes.
	second := doJSONRequest(fixture.server, http.MethodGet, fmt.Sprintf("/b/%s/card.png", postID), "", "")
	if second.Code != http.StatusOK {
		t.Fatalf("second request expected 200, got %d", second.Code)
	}
	if second.Body.Len() == 0 {
		t.Fatalf("second request expected non-empty card body")
	}
}
