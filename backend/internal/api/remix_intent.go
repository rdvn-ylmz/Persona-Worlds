package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"personaworlds/backend/internal/common"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
)

type remixIntentToken struct {
	BattleID   string
	RoomID     string
	Topic      string
	ProStyle   string
	ConStyle   string
	TemplateID string
	ExpiresAt  time.Time
}

type remixIntentTemplateSummary struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	TurnCount int    `json:"turn_count"`
	WordLimit int    `json:"word_limit"`
}

func (s *Server) handleGetPublicBattleMeta(w http.ResponseWriter, r *http.Request) {
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeNotFound(w, "battle not found")
		return
	}

	var (
		out struct {
			BattleID  string `json:"battle_id"`
			RoomID    string `json:"room_id"`
			RoomName  string `json:"room_name"`
			Topic     string `json:"topic"`
			CreatedAt string `json:"created_at"`
			Template  any    `json:"template,omitempty"`
			ShareURL  string `json:"share_url"`
			CardURL   string `json:"card_url"`
		}
		content      string
		templateID   string
		templateName string
		createdAt    time.Time
	)

	err := s.db.QueryRow(r.Context(), `
		SELECT
			p.id::text,
			p.room_id::text,
			COALESCE(rm.name, ''),
			p.content,
			COALESCE(p.template_id::text, ''),
			COALESCE(t.name, ''),
			p.created_at
		FROM posts p
		JOIN rooms rm ON rm.id = p.room_id
		LEFT JOIN templates t ON t.id = p.template_id
		WHERE p.id = $1
		  AND p.status = 'PUBLISHED'
	`, battleID).Scan(
		&out.BattleID,
		&out.RoomID,
		&out.RoomName,
		&content,
		&templateID,
		&templateName,
		&createdAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNotFound(w, "battle not found")
			return
		}
		writeInternalError(w, "could not load battle")
		return
	}

	out.Topic = buildBattleCardTopic(content, out.RoomName)
	out.CreatedAt = createdAt.UTC().Format(time.RFC3339)
	out.ShareURL = fmt.Sprintf("%s/b/%s", strings.TrimRight(s.cfg.FrontendOrigin, "/"), out.BattleID)
	out.CardURL = fmt.Sprintf("/b/%s/card.png", out.BattleID)
	if strings.TrimSpace(templateID) != "" {
		out.Template = map[string]any{
			"id":   strings.TrimSpace(templateID),
			"name": strings.TrimSpace(templateName),
		}
	}

	_ = s.logEventFromRequest(r, eventPublicBattleViewed, map[string]any{
		"battle_id": out.BattleID,
		"room_id":   out.RoomID,
	})

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateBattleRemixIntent(w http.ResponseWriter, r *http.Request) {
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeNotFound(w, "battle not found")
		return
	}

	var (
		roomID      string
		roomName    string
		postContent string
		templateID  string
	)
	err := s.db.QueryRow(r.Context(), `
		SELECT
			p.room_id::text,
			COALESCE(rm.name, ''),
			p.content,
			COALESCE(p.template_id::text, '')
		FROM posts p
		JOIN rooms rm ON rm.id = p.room_id
		WHERE p.id = $1
		  AND p.status = 'PUBLISHED'
	`, battleID).Scan(&roomID, &roomName, &postContent, &templateID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNotFound(w, "battle not found")
			return
		}
		writeInternalError(w, "could not load battle")
		return
	}

	topic := buildBattleCardTopic(postContent, roomName)
	proStyle := "Bold, practical, concise"
	conStyle := "Skeptical, evidence-first, concise"

	suggestedTemplates, err := s.listSuggestedRemixTemplates(r.Context(), templateID, 4)
	if err != nil {
		writeInternalError(w, "could not load templates")
		return
	}

	preferredTemplateID := strings.TrimSpace(templateID)
	if preferredTemplateID == "" && len(suggestedTemplates) > 0 {
		preferredTemplateID = suggestedTemplates[0].ID
	}

	token, expiresAt, err := createRemixIntentToken(s.cfg.JWTSecret, remixIntentToken{
		BattleID:   battleID,
		RoomID:     roomID,
		Topic:      topic,
		ProStyle:   proStyle,
		ConStyle:   conStyle,
		TemplateID: preferredTemplateID,
	}, 30*time.Minute)
	if err != nil {
		writeInternalError(w, "could not create remix intent")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "pw_remix_intent",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		MaxAge:   int((30 * time.Minute).Seconds()),
		SameSite: http.SameSiteLaxMode,
	})

	_ = s.logEventFromRequest(r, eventRemixStarted, map[string]any{
		"battle_id": battleID,
		"room_id":   roomID,
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"battle_id":            battleID,
		"room_id":              roomID,
		"room_name":            roomName,
		"topic":                topic,
		"pro_style":            proStyle,
		"con_style":            conStyle,
		"suggested_templates":  suggestedTemplates,
		"remix_token":          token,
		"remix_token_expires":  expiresAt.UTC().Format(time.RFC3339),
		"target_create_route":  fmt.Sprintf("/rooms/%s/battles", roomID),
		"target_battle_source": battleID,
	})
}

func (s *Server) listSuggestedRemixTemplates(ctx context.Context, preferredTemplateID string, limit int) ([]remixIntentTemplateSummary, error) {
	if limit <= 0 {
		limit = 4
	}
	if limit > 20 {
		limit = 20
	}

	items := make([]remixIntentTemplateSummary, 0, limit)
	seen := map[string]struct{}{}
	add := func(item remixIntentTemplateSummary) {
		if len(items) >= limit {
			return
		}
		key := strings.TrimSpace(item.ID)
		if key == "" {
			return
		}
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		items = append(items, item)
	}

	if strings.TrimSpace(preferredTemplateID) != "" {
		var preferred remixIntentTemplateSummary
		err := s.db.QueryRow(ctx, `
			SELECT id::text, name, turn_count, word_limit
			FROM templates
			WHERE id = $1
			  AND is_public = TRUE
		`, strings.TrimSpace(preferredTemplateID)).Scan(&preferred.ID, &preferred.Name, &preferred.TurnCount, &preferred.WordLimit)
		if err == nil {
			add(preferred)
		}
	}

	rows, err := s.db.Query(ctx, `
		SELECT id::text, name, turn_count, word_limit
		FROM templates
		WHERE is_public = TRUE
		ORDER BY CASE WHEN LOWER(name) = LOWER('Claim/Evidence 6 turns') THEN 0 ELSE 1 END, created_at DESC
		LIMIT $1
	`, limit+4)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var item remixIntentTemplateSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.TurnCount, &item.WordLimit); err != nil {
			return nil, err
		}
		add(item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return items, nil
}

func createRemixIntentToken(secret string, intent remixIntentToken, ttl time.Duration) (string, time.Time, error) {
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	expiresAt := time.Now().UTC().Add(ttl)

	claims := jwt.MapClaims{
		"battle_id":   strings.TrimSpace(intent.BattleID),
		"room_id":     strings.TrimSpace(intent.RoomID),
		"topic":       common.TruncateRunes(strings.TrimSpace(intent.Topic), 180),
		"pro_style":   common.TruncateRunes(strings.TrimSpace(intent.ProStyle), 80),
		"con_style":   common.TruncateRunes(strings.TrimSpace(intent.ConStyle), 80),
		"template_id": strings.TrimSpace(intent.TemplateID),
		"exp":         expiresAt.Unix(),
		"iat":         time.Now().UTC().Unix(),
		"typ":         "remix_intent",
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(secret))
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, expiresAt, nil
}

func parseRemixIntentToken(secret, tokenValue string) (remixIntentToken, error) {
	claims := jwt.MapClaims{}
	token, err := jwt.ParseWithClaims(tokenValue, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || token == nil || !token.Valid {
		return remixIntentToken{}, fmt.Errorf("invalid token")
	}

	if typ, _ := claims["typ"].(string); strings.TrimSpace(typ) != "remix_intent" {
		return remixIntentToken{}, fmt.Errorf("invalid token type")
	}

	expFloat, ok := claims["exp"].(float64)
	if !ok {
		return remixIntentToken{}, fmt.Errorf("missing exp")
	}
	exp := time.Unix(int64(expFloat), 0).UTC()
	if time.Now().UTC().After(exp) {
		return remixIntentToken{}, fmt.Errorf("token expired")
	}

	out := remixIntentToken{
		BattleID:   strings.TrimSpace(asStringClaim(claims["battle_id"])),
		RoomID:     strings.TrimSpace(asStringClaim(claims["room_id"])),
		Topic:      common.TruncateRunes(strings.TrimSpace(asStringClaim(claims["topic"])), 180),
		ProStyle:   common.TruncateRunes(strings.TrimSpace(asStringClaim(claims["pro_style"])), 80),
		ConStyle:   common.TruncateRunes(strings.TrimSpace(asStringClaim(claims["con_style"])), 80),
		TemplateID: strings.TrimSpace(asStringClaim(claims["template_id"])),
		ExpiresAt:  exp,
	}

	if out.RoomID == "" {
		return remixIntentToken{}, fmt.Errorf("invalid token payload")
	}
	return out, nil
}

func asStringClaim(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	default:
		return fmt.Sprintf("%v", typed)
	}
}
