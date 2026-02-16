package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"personaworlds/backend/internal/common"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func (s *Server) handleListPublicTemplates(w http.ResponseWriter, r *http.Request) {
	templates, err := s.listPublicTemplates(r.Context(), 100)
	if err != nil {
		writeInternalError(w, "could not list templates")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"templates": templates,
	})
}

func (s *Server) listPublicTemplates(ctx context.Context, limit int) ([]BattleTemplate, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			id::text,
			COALESCE(owner_user_id::text, ''),
			name,
			prompt_rules,
			turn_count,
			word_limit,
			created_at,
			is_public
		FROM templates
		WHERE is_public = TRUE
		ORDER BY created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	templates := make([]BattleTemplate, 0, limit)
	for rows.Next() {
		var item BattleTemplate
		if err := rows.Scan(
			&item.ID,
			&item.OwnerUserID,
			&item.Name,
			&item.PromptRules,
			&item.TurnCount,
			&item.WordLimit,
			&item.CreatedAt,
			&item.IsPublic,
		); err != nil {
			return nil, err
		}
		templates = append(templates, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return templates, nil
}

func (s *Server) handleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	var req struct {
		Name        string `json:"name"`
		PromptRules string `json:"prompt_rules"`
		TurnCount   int    `json:"turn_count"`
		WordLimit   int    `json:"word_limit"`
		IsPublic    bool   `json:"is_public"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	if err := validateTemplateInput(req.Name, req.PromptRules, req.TurnCount, req.WordLimit); err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	var out BattleTemplate
	err := s.db.QueryRow(r.Context(), `
		INSERT INTO templates(owner_user_id, name, prompt_rules, turn_count, word_limit, is_public)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, owner_user_id::text, name, prompt_rules, turn_count, word_limit, created_at, is_public
	`, userID, strings.TrimSpace(req.Name), strings.TrimSpace(req.PromptRules), req.TurnCount, req.WordLimit, req.IsPublic).Scan(
		&out.ID,
		&out.OwnerUserID,
		&out.Name,
		&out.PromptRules,
		&out.TurnCount,
		&out.WordLimit,
		&out.CreatedAt,
		&out.IsPublic,
	)
	if err != nil {
		writeInternalError(w, "could not create template")
		return
	}

	writeJSON(w, http.StatusCreated, out)
}

func validateTemplateInput(name, promptRules string, turnCount, wordLimit int) error {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return fmt.Errorf("name is required")
	}
	if len([]rune(cleanName)) > 80 {
		return fmt.Errorf("name must be <= 80 chars")
	}

	if turnCount < 2 || turnCount > 20 {
		return fmt.Errorf("turn_count must be between 2 and 20")
	}
	if wordLimit < 40 || wordLimit > 500 {
		return fmt.Errorf("word_limit must be between 40 and 500")
	}

	if err := validateTemplatePromptRules(promptRules); err != nil {
		return err
	}
	return nil
}

func validateTemplatePromptRules(value string) error {
	clean := strings.TrimSpace(value)
	if clean == "" {
		return fmt.Errorf("prompt_rules is required")
	}
	if len([]rune(clean)) > 1200 {
		return fmt.Errorf("prompt_rules must be <= 1200 chars")
	}

	lower := strings.ToLower(clean)
	forbiddenSnippets := []string{
		"system prompt",
		"developer message",
		"hidden instruction",
		"ignore previous instructions",
		"<system>",
		"prompt injection",
	}
	for _, snippet := range forbiddenSnippets {
		if strings.Contains(lower, snippet) {
			return fmt.Errorf("prompt_rules contains forbidden instruction pattern")
		}
	}

	if strings.Contains(lower, "http://") ||
		strings.Contains(lower, "https://") ||
		strings.Contains(lower, "www.") ||
		strings.Contains(lower, "mailto:") {
		return fmt.Errorf("prompt_rules cannot include external links")
	}

	return nil
}

func (s *Server) loadTemplateForUser(ctx context.Context, templateID, userID string) (BattleTemplate, error) {
	var out BattleTemplate
	err := s.db.QueryRow(ctx, `
		SELECT
			id::text,
			COALESCE(owner_user_id::text, ''),
			name,
			prompt_rules,
			turn_count,
			word_limit,
			created_at,
			is_public
		FROM templates
		WHERE id = $1
		  AND (is_public = TRUE OR owner_user_id = $2::uuid)
	`, strings.TrimSpace(templateID), strings.TrimSpace(userID)).Scan(
		&out.ID,
		&out.OwnerUserID,
		&out.Name,
		&out.PromptRules,
		&out.TurnCount,
		&out.WordLimit,
		&out.CreatedAt,
		&out.IsPublic,
	)
	return out, err
}

func (s *Server) loadDefaultTemplate(ctx context.Context) (BattleTemplate, error) {
	var out BattleTemplate
	err := s.db.QueryRow(ctx, `
		SELECT
			id::text,
			COALESCE(owner_user_id::text, ''),
			name,
			prompt_rules,
			turn_count,
			word_limit,
			created_at,
			is_public
		FROM templates
		WHERE is_public = TRUE
		ORDER BY CASE WHEN LOWER(name) = LOWER('Claim/Evidence 6 turns') THEN 0 ELSE 1 END, created_at ASC
		LIMIT 1
	`).Scan(
		&out.ID,
		&out.OwnerUserID,
		&out.Name,
		&out.PromptRules,
		&out.TurnCount,
		&out.WordLimit,
		&out.CreatedAt,
		&out.IsPublic,
	)
	return out, err
}

func (s *Server) handleCreateBattle(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}
	roomID := strings.TrimSpace(chi.URLParam(r, "id"))
	if roomID == "" {
		writeBadRequest(w, "room id is required")
		return
	}

	room, err := s.getRoomByID(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNotFound(w, "room not found")
			return
		}
		writeInternalError(w, "could not load room")
		return
	}

	var req struct {
		Topic      string `json:"topic"`
		TemplateID string `json:"template_id"`
		RemixToken string `json:"remix_token"`
		ProStyle   string `json:"pro_style"`
		ConStyle   string `json:"con_style"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	topic := strings.TrimSpace(req.Topic)
	templateID := strings.TrimSpace(req.TemplateID)
	proStyle := strings.TrimSpace(req.ProStyle)
	conStyle := strings.TrimSpace(req.ConStyle)
	remixUsed := false

	if strings.TrimSpace(req.RemixToken) != "" {
		intent, err := parseRemixIntentToken(s.cfg.JWTSecret, req.RemixToken)
		if err != nil {
			writeBadRequest(w, "invalid remix_token")
			return
		}
		if strings.TrimSpace(intent.RoomID) != roomID {
			writeBadRequest(w, "remix_token does not match room")
			return
		}
		if topic == "" {
			topic = strings.TrimSpace(intent.Topic)
		}
		if templateID == "" {
			templateID = strings.TrimSpace(intent.TemplateID)
		}
		if proStyle == "" {
			proStyle = strings.TrimSpace(intent.ProStyle)
		}
		if conStyle == "" {
			conStyle = strings.TrimSpace(intent.ConStyle)
		}
		remixUsed = true
	}

	topic = common.TruncateRunes(topic, 180)
	if strings.TrimSpace(topic) == "" {
		writeBadRequest(w, "topic is required")
		return
	}
	if proStyle == "" {
		proStyle = "Bold and practical"
	}
	if conStyle == "" {
		conStyle = "Skeptical and evidence-first"
	}
	proStyle = common.TruncateRunes(proStyle, 80)
	conStyle = common.TruncateRunes(conStyle, 80)

	var template BattleTemplate
	if templateID == "" {
		template, err = s.loadDefaultTemplate(r.Context())
		if err != nil {
			writeInternalError(w, "could not load default template")
			return
		}
	} else {
		template, err = s.loadTemplateForUser(r.Context(), templateID, userID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				writeNotFound(w, "template not found")
				return
			}
			writeInternalError(w, "could not load template")
			return
		}
	}

	content := fmt.Sprintf(
		"Topic: %s\nTemplate: %s\nPro style: %s\nCon style: %s\n\nBattle opening: keep arguments concise and evidence-based.",
		topic,
		template.Name,
		proStyle,
		conStyle,
	)
	content = common.TruncateRunes(content, s.cfg.DraftMaxLen)

	var out Post
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content, published_at, template_id)
		VALUES ($1, NULL, $2, 'HUMAN', 'PUBLISHED', $3, NOW(), $4::uuid)
		RETURNING id::text, room_id::text, '', '', authored_by::text, status::text, content, created_at, updated_at
	`, room.ID, userID, content, template.ID).Scan(
		&out.ID,
		&out.RoomID,
		&out.PersonaID,
		&out.Persona,
		&out.AuthoredBy,
		&out.Status,
		&out.Content,
		&out.CreatedAt,
		&out.UpdatedAt,
	)
	if err != nil {
		writeInternalError(w, "could not create battle")
		return
	}

	enqueuedReplies := s.enqueueBattleReplies(r.Context(), userID, out.ID, template)

	_ = s.logEventFromRequest(r, eventBattleCreated, map[string]any{
		"battle_id":   out.ID,
		"room_id":     room.ID,
		"template_id": template.ID,
	})
	if remixUsed {
		_ = s.logEventFromRequest(r, eventRemixCompleted, map[string]any{
			"battle_id":   out.ID,
			"room_id":     room.ID,
			"template_id": template.ID,
		})
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"battle_id":          out.ID,
		"post":               out,
		"room_name":          room.Name,
		"template":           template,
		"enqueued_replies":   enqueuedReplies,
		"remix_used":         remixUsed,
		"suggested_next_url": fmt.Sprintf("/b/%s", out.ID),
	})
}

func (s *Server) enqueueBattleReplies(ctx context.Context, userID, postID string, template BattleTemplate) int {
	personaIDs, err := s.resolvePersonaIDsForReplyGeneration(ctx, userID, nil)
	if err != nil || len(personaIDs) == 0 {
		return 0
	}

	maxReplies := 2
	if template.TurnCount >= 8 {
		maxReplies = 3
	}
	if len(personaIDs) < maxReplies {
		maxReplies = len(personaIDs)
	}

	enqueued := 0
	for _, personaID := range personaIDs[:maxReplies] {
		persona, err := s.getPersonaByID(ctx, userID, personaID)
		if err != nil {
			continue
		}
		used, err := s.currentQuotaUsage(ctx, personaID, "reply")
		if err != nil || used >= persona.DailyReplyQuota {
			continue
		}

		payload := fmt.Sprintf(`{"post_id":"%s","persona_id":"%s","template_id":"%s"}`, postID, personaID, template.ID)
		if _, err := s.db.Exec(ctx, `
			INSERT INTO jobs(job_type, post_id, persona_id, payload, status, available_at)
			VALUES ('generate_reply', $1, $2, $3::jsonb, 'PENDING', NOW())
		`, postID, personaID, payload); err != nil {
			continue
		}
		enqueued++
	}
	return enqueued
}
