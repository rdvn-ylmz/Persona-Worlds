package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/common"

	"github.com/jackc/pgx/v5"
)

type digestThread struct {
	PostID        string    `json:"post_id"`
	RoomID        string    `json:"room_id,omitempty"`
	RoomName      string    `json:"room_name,omitempty"`
	PostPreview   string    `json:"post_preview,omitempty"`
	ActivityCount int       `json:"activity_count"`
	LastActivity  time.Time `json:"last_activity_at"`
}

type digestStats struct {
	Posts      int            `json:"posts"`
	Replies    int            `json:"replies"`
	TopThreads []digestThread `json:"top_threads"`
}

func (w *Worker) generateDigestForOnePersona(ctx context.Context) error {
	var persona struct {
		ID                string
		Name              string
		Bio               string
		Tone              string
		WritingSamplesRaw []byte
		DoNotSayRaw       []byte
		CatchphrasesRaw   []byte
		PreferredLanguage string
		Formality         int
	}

	err := w.db.QueryRow(ctx, `
		SELECT
			p.id::text,
			p.name,
			p.bio,
			p.tone,
			p.writing_samples,
			p.do_not_say,
			p.catchphrases,
			p.preferred_language,
			p.formality
		FROM personas p
		LEFT JOIN persona_digests d
			ON d.persona_id = p.id
		   AND d.date = CURRENT_DATE
		WHERE d.id IS NULL
		   OR EXISTS (
				SELECT 1
				FROM persona_activity_events e
				WHERE e.persona_id = p.id
				  AND e.created_at >= date_trunc('day', NOW())
				  AND e.created_at > COALESCE(d.updated_at, TO_TIMESTAMP(0))
		   )
		ORDER BY COALESCE(d.updated_at, TO_TIMESTAMP(0)) ASC, p.created_at ASC
		LIMIT 1
	`).Scan(
		&persona.ID,
		&persona.Name,
		&persona.Bio,
		&persona.Tone,
		&persona.WritingSamplesRaw,
		&persona.DoNotSayRaw,
		&persona.CatchphrasesRaw,
		&persona.PreferredLanguage,
		&persona.Formality,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	personaCtx := ai.PersonaContext{
		ID:                persona.ID,
		Name:              persona.Name,
		Bio:               persona.Bio,
		Tone:              persona.Tone,
		WritingSamples:    parseJSONStringSlice(persona.WritingSamplesRaw),
		DoNotSay:          parseJSONStringSlice(persona.DoNotSayRaw),
		Catchphrases:      parseJSONStringSlice(persona.CatchphrasesRaw),
		PreferredLanguage: strings.TrimSpace(persona.PreferredLanguage),
		Formality:         persona.Formality,
	}
	if personaCtx.PreferredLanguage == "" {
		personaCtx.PreferredLanguage = "en"
	}

	stats, err := w.collectDigestStats(ctx, persona.ID)
	if err != nil {
		return err
	}

	summary := noActivityDigestSummary(personaCtx)
	if stats.Posts > 0 || stats.Replies > 0 || len(stats.TopThreads) > 0 {
		aiThreads := make([]ai.DigestThreadContext, 0, len(stats.TopThreads))
		for _, thread := range stats.TopThreads {
			aiThreads = append(aiThreads, ai.DigestThreadContext{
				PostID:        thread.PostID,
				RoomName:      thread.RoomName,
				PostPreview:   thread.PostPreview,
				ActivityCount: thread.ActivityCount,
			})
		}

		aiSummary, aiErr := w.llm.SummarizePersonaActivity(ctx, personaCtx, ai.DigestStats{
			Posts:   stats.Posts,
			Replies: stats.Replies,
		}, aiThreads)
		if aiErr != nil {
			summary = fallbackDigestSummary(personaCtx, stats)
		} else {
			summary = strings.TrimSpace(aiSummary)
		}
	}

	if summary == "" {
		summary = fallbackDigestSummary(personaCtx, stats)
	}
	summary = common.TruncateRunes(summary, w.cfg.SummaryMaxLen)

	statsJSON, err := json.Marshal(stats)
	if err != nil {
		return err
	}

	_, err = w.db.Exec(ctx, `
		INSERT INTO persona_digests(persona_id, date, summary, stats, created_at, updated_at)
		VALUES ($1, CURRENT_DATE, $2, $3::jsonb, NOW(), NOW())
		ON CONFLICT (persona_id, date)
		DO UPDATE SET
			summary = EXCLUDED.summary,
			stats = EXCLUDED.stats,
			updated_at = NOW()
	`, persona.ID, summary, statsJSON)
	if err != nil {
		return err
	}

	return nil
}

func (w *Worker) collectDigestStats(ctx context.Context, personaID string) (digestStats, error) {
	stats := digestStats{
		TopThreads: []digestThread{},
	}

	if err := w.db.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(CASE WHEN type = 'post_created' THEN 1 ELSE 0 END), 0)::int AS posts,
			COALESCE(SUM(CASE WHEN type = 'reply_generated' THEN 1 ELSE 0 END), 0)::int AS replies
		FROM persona_activity_events
		WHERE persona_id = $1
		  AND created_at >= date_trunc('day', NOW())
	`, personaID).Scan(&stats.Posts, &stats.Replies); err != nil {
		return digestStats{}, err
	}

	rows, err := w.db.Query(ctx, `
		SELECT
			e.metadata->>'post_id' AS post_id,
			COALESCE(MAX(e.metadata->>'room_id'), COALESCE(MAX(p.room_id::text), '')) AS room_id,
			COALESCE(MAX(r.name), '') AS room_name,
			COALESCE(MAX(NULLIF(e.metadata->>'post_preview', '')), COALESCE(MAX(p.content), '')) AS post_preview,
			COUNT(*)::int AS activity_count,
			MAX(e.created_at) AS last_activity
		FROM persona_activity_events e
		LEFT JOIN posts p ON p.id::text = e.metadata->>'post_id'
		LEFT JOIN rooms r ON r.id = p.room_id
		WHERE e.persona_id = $1
		  AND e.type = 'thread_participated'
		  AND e.created_at >= date_trunc('day', NOW())
		  AND COALESCE(e.metadata->>'post_id', '') <> ''
		GROUP BY e.metadata->>'post_id'
		ORDER BY activity_count DESC, last_activity DESC
		LIMIT 3
	`, personaID)
	if err != nil {
		return digestStats{}, err
	}
	defer rows.Close()

	for rows.Next() {
		var thread digestThread
		if err := rows.Scan(
			&thread.PostID,
			&thread.RoomID,
			&thread.RoomName,
			&thread.PostPreview,
			&thread.ActivityCount,
			&thread.LastActivity,
		); err != nil {
			return digestStats{}, err
		}
		thread.PostPreview = common.TruncateRunes(thread.PostPreview, 220)
		stats.TopThreads = append(stats.TopThreads, thread)
	}
	if err := rows.Err(); err != nil {
		return digestStats{}, err
	}

	return stats, nil
}

func parseJSONStringSlice(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}

	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return []string{}
	}
	return values
}

func noActivityDigestSummary(persona ai.PersonaContext) string {
	if strings.ToLower(strings.TrimSpace(persona.PreferredLanguage)) == "tr" {
		return "Bugün henüz yeni aktivite yok. Yeni gönderiler ve yanıtlar olduğunda burada özetlenecek."
	}
	return "No activity yet today. New posts and replies will show up here as they happen."
}

func fallbackDigestSummary(persona ai.PersonaContext, stats digestStats) string {
	if stats.Posts == 0 && stats.Replies == 0 {
		return noActivityDigestSummary(persona)
	}

	parts := make([]string, 0, len(stats.TopThreads))
	for _, thread := range stats.TopThreads {
		label := strings.TrimSpace(thread.RoomName)
		if label == "" {
			label = "thread"
		}
		parts = append(parts, fmt.Sprintf("%s (%d events)", label, thread.ActivityCount))
	}

	topThreadText := "no standout threads"
	if len(parts) > 0 {
		topThreadText = strings.Join(parts, ", ")
	}

	if strings.ToLower(strings.TrimSpace(persona.PreferredLanguage)) == "tr" {
		return fmt.Sprintf("Bugün %d gönderi ve %d yanıt üretildi. Öne çıkan tartışmalar: %s.", stats.Posts, stats.Replies, topThreadText)
	}
	return fmt.Sprintf("Today there were %d posts and %d replies. The most active threads were: %s.", stats.Posts, stats.Replies, topThreadText)
}
