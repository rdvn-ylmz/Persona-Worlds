package api

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"personaworlds/backend/internal/ai"
)

func (s *Server) getPersonaByID(ctx context.Context, userID, personaID string) (Persona, error) {
	var p Persona
	err := scanPersona(s.db.QueryRow(ctx, `
		SELECT id::text, name, bio, tone, writing_samples, do_not_say, catchphrases, preferred_language, formality, daily_draft_quota, daily_reply_quota, created_at, updated_at
		FROM personas
		WHERE id = $1 AND user_id = $2
	`, personaID, userID), &p)
	return p, err
}

func (s *Server) getRoomByID(ctx context.Context, roomID string) (Room, error) {
	var rm Room
	err := s.db.QueryRow(ctx, `
		SELECT id::text, slug, name, description, created_at
		FROM rooms
		WHERE id = $1
	`, roomID).Scan(&rm.ID, &rm.Slug, &rm.Name, &rm.Description, &rm.CreatedAt)
	return rm, err
}

func (s *Server) currentQuotaUsage(ctx context.Context, personaID, quotaType string) (int, error) {
	var used int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM quota_events
		WHERE persona_id = $1
		  AND quota_type = $2
		  AND created_at >= date_trunc('day', NOW())
	`, personaID, quotaType).Scan(&used)
	return used, err
}

func (s *Server) resolvePersonaIDsForReplyGeneration(ctx context.Context, userID string, provided []string) ([]string, error) {
	if len(provided) > 0 {
		ids := make([]string, 0, len(provided))
		seen := map[string]struct{}{}
		for _, id := range provided {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, exists := seen[id]; exists {
				continue
			}
			seen[id] = struct{}{}

			var exists bool
			err := s.db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM personas WHERE id=$1 AND user_id=$2)", id, userID).Scan(&exists)
			if err != nil {
				return nil, fmt.Errorf("could not validate persona")
			}
			if !exists {
				return nil, fmt.Errorf("persona %s not found", id)
			}
			ids = append(ids, id)
		}
		if len(ids) == 0 {
			return nil, fmt.Errorf("persona_ids cannot be empty")
		}
		return ids, nil
	}

	rows, err := s.db.Query(ctx, `
		SELECT id::text
		FROM personas
		WHERE user_id = $1
		ORDER BY created_at ASC
		LIMIT 3
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0, 3)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no personas available")
	}
	return ids, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

type personaInput struct {
	Name              string
	Bio               string
	Tone              string
	WritingSamples    []string
	DoNotSay          []string
	Catchphrases      []string
	PreferredLanguage string
	Formality         int
}

func scanPersona(row rowScanner, p *Persona) error {
	var writingSamplesRaw []byte
	var doNotSayRaw []byte
	var catchphrasesRaw []byte

	if err := row.Scan(
		&p.ID,
		&p.Name,
		&p.Bio,
		&p.Tone,
		&writingSamplesRaw,
		&doNotSayRaw,
		&catchphrasesRaw,
		&p.PreferredLanguage,
		&p.Formality,
		&p.DailyDraftQuota,
		&p.DailyReplyQuota,
		&p.CreatedAt,
		&p.UpdatedAt,
	); err != nil {
		return err
	}

	if len(writingSamplesRaw) > 0 {
		if err := json.Unmarshal(writingSamplesRaw, &p.WritingSamples); err != nil {
			return err
		}
	}
	if len(doNotSayRaw) > 0 {
		if err := json.Unmarshal(doNotSayRaw, &p.DoNotSay); err != nil {
			return err
		}
	}
	if len(catchphrasesRaw) > 0 {
		if err := json.Unmarshal(catchphrasesRaw, &p.Catchphrases); err != nil {
			return err
		}
	}

	if p.WritingSamples == nil {
		p.WritingSamples = []string{}
	}
	if p.DoNotSay == nil {
		p.DoNotSay = []string{}
	}
	if p.Catchphrases == nil {
		p.Catchphrases = []string{}
	}
	return nil
}

func normalizePersonaInput(name, bio, tone string, writingSamples, doNotSay, catchphrases []string, preferredLanguage string, formality int) (personaInput, error) {
	cleanName := strings.TrimSpace(name)
	if cleanName == "" {
		return personaInput{}, fmt.Errorf("name is required")
	}

	cleanWritingSamples := normalizeStringSlice(writingSamples)
	if len(cleanWritingSamples) != 3 {
		return personaInput{}, fmt.Errorf("writing_samples must contain exactly 3 short examples")
	}
	for _, sample := range cleanWritingSamples {
		if len([]rune(sample)) > 180 {
			return personaInput{}, fmt.Errorf("writing_samples items must be <= 180 chars")
		}
	}

	cleanDoNotSay := normalizeStringSlice(doNotSay)
	for _, item := range cleanDoNotSay {
		if len([]rune(item)) > 120 {
			return personaInput{}, fmt.Errorf("do_not_say items must be <= 120 chars")
		}
	}

	cleanCatchphrases := normalizeStringSlice(catchphrases)
	for _, item := range cleanCatchphrases {
		if len([]rune(item)) > 80 {
			return personaInput{}, fmt.Errorf("catchphrases items must be <= 80 chars")
		}
	}

	language := strings.ToLower(strings.TrimSpace(preferredLanguage))
	if language != "tr" && language != "en" {
		return personaInput{}, fmt.Errorf("preferred_language must be tr or en")
	}

	if formality < 0 || formality > 3 {
		return personaInput{}, fmt.Errorf("formality must be between 0 and 3")
	}

	return personaInput{
		Name:              cleanName,
		Bio:               strings.TrimSpace(bio),
		Tone:              strings.TrimSpace(tone),
		WritingSamples:    cleanWritingSamples,
		DoNotSay:          cleanDoNotSay,
		Catchphrases:      cleanCatchphrases,
		PreferredLanguage: language,
		Formality:         formality,
	}, nil
}

func normalizeStringSlice(items []string) []string {
	cleaned := make([]string, 0, len(items))
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}
	return cleaned
}

func personaToAIContext(persona Persona) ai.PersonaContext {
	return ai.PersonaContext{
		ID:                persona.ID,
		Name:              persona.Name,
		Bio:               persona.Bio,
		Tone:              persona.Tone,
		WritingSamples:    persona.WritingSamples,
		DoNotSay:          persona.DoNotSay,
		Catchphrases:      persona.Catchphrases,
		PreferredLanguage: persona.PreferredLanguage,
		Formality:         persona.Formality,
	}
}
