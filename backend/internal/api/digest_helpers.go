package api

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *Server) personaOwnedByUser(ctx context.Context, userID, personaID string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM personas
			WHERE id = $1 AND user_id = $2
		)
	`, personaID, userID).Scan(&exists)
	return exists, err
}

func (s *Server) getDigestForDate(ctx context.Context, personaID string, date time.Time) (PersonaDigest, bool, error) {
	var (
		digest PersonaDigest
		stats  []byte
		rawDay time.Time
	)

	err := s.db.QueryRow(ctx, `
		SELECT persona_id::text, date, summary, stats, updated_at
		FROM persona_digests
		WHERE persona_id = $1
		  AND date = $2::date
	`, personaID, date.Format("2006-01-02")).Scan(&digest.PersonaID, &rawDay, &digest.Summary, &stats, &digest.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PersonaDigest{}, false, nil
		}
		return PersonaDigest{}, false, err
	}

	digest.Date = rawDay.UTC().Format("2006-01-02")
	if err := hydrateDigestStats(&digest, stats); err != nil {
		return PersonaDigest{}, false, err
	}
	return digest, true, nil
}

func (s *Server) getLatestDigest(ctx context.Context, personaID string) (PersonaDigest, bool, error) {
	var (
		digest PersonaDigest
		stats  []byte
		rawDay time.Time
	)

	err := s.db.QueryRow(ctx, `
		SELECT persona_id::text, date, summary, stats, updated_at
		FROM persona_digests
		WHERE persona_id = $1
		ORDER BY date DESC
		LIMIT 1
	`, personaID).Scan(&digest.PersonaID, &rawDay, &digest.Summary, &stats, &digest.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PersonaDigest{}, false, nil
		}
		return PersonaDigest{}, false, err
	}

	digest.Date = rawDay.UTC().Format("2006-01-02")
	if err := hydrateDigestStats(&digest, stats); err != nil {
		return PersonaDigest{}, false, err
	}
	return digest, true, nil
}

func hydrateDigestStats(digest *PersonaDigest, statsRaw []byte) error {
	digest.Stats = DigestStats{
		TopThreads: []DigestThread{},
	}
	if len(statsRaw) > 0 {
		if err := json.Unmarshal(statsRaw, &digest.Stats); err != nil {
			return err
		}
	}
	if digest.Stats.TopThreads == nil {
		digest.Stats.TopThreads = []DigestThread{}
	}
	digest.HasActivity = digest.Stats.Posts > 0 || digest.Stats.Replies > 0 || len(digest.Stats.TopThreads) > 0
	if strings.TrimSpace(digest.Summary) == "" && !digest.HasActivity {
		digest.Summary = "No activity yet today. Once the persona posts or replies, this digest will update."
	}
	return nil
}

func emptyDigest(personaID string, date time.Time) PersonaDigest {
	return PersonaDigest{
		PersonaID: personaID,
		Date:      date.UTC().Format("2006-01-02"),
		Summary:   "No activity yet today. Once the persona posts or replies, this digest will update.",
		Stats: DigestStats{
			Posts:      0,
			Replies:    0,
			TopThreads: []DigestThread{},
		},
		HasActivity: false,
		UpdatedAt:   time.Now().UTC(),
	}
}
