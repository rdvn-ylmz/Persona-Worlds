package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/common"

	"github.com/jackc/pgx/v5"
)

type weeklyDigestCandidate struct {
	BattleID   string
	RoomID     string
	RoomName   string
	Content    string
	CreatedAt  time.Time
	Shares     int
	Remixes    int
	IsFollowed bool
	Score      float64
}

type weeklyDigestItem struct {
	BattleID  string    `json:"battle_id"`
	RoomID    string    `json:"room_id"`
	RoomName  string    `json:"room_name"`
	Topic     string    `json:"topic"`
	Summary   string    `json:"summary"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

func (w *Worker) generateWeeklyDigestForOneUser(ctx context.Context) error {
	var userID string
	err := w.db.QueryRow(ctx, `
		SELECT u.id::text
		FROM users u
		LEFT JOIN weekly_digests d
			ON d.user_id = u.id
		   AND d.week_start = date_trunc('week', NOW())::date
		WHERE d.id IS NULL
		   OR d.updated_at <= NOW() - INTERVAL '6 hours'
		ORDER BY COALESCE(d.updated_at, TO_TIMESTAMP(0)) ASC, u.created_at ASC
		LIMIT 1
	`).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	candidates, err := w.collectWeeklyDigestCandidates(ctx, userID, 3)
	if err != nil {
		return err
	}

	items := make([]weeklyDigestItem, 0, len(candidates))
	for _, candidate := range candidates {
		topic := extractWeeklyDigestTopic(candidate.Content, candidate.RoomName)
		summary := w.summarizeWeeklyDigestBattle(ctx, candidate, topic)
		items = append(items, weeklyDigestItem{
			BattleID:  candidate.BattleID,
			RoomID:    candidate.RoomID,
			RoomName:  candidate.RoomName,
			Topic:     topic,
			Summary:   summary,
			Score:     roundWeeklyScore(candidate.Score),
			CreatedAt: candidate.CreatedAt,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return items[i].CreatedAt.After(items[j].CreatedAt)
		}
		return items[i].Score > items[j].Score
	})

	payload, err := json.Marshal(items)
	if err != nil {
		return err
	}

	weekStart := startOfWeekUTC(time.Now().UTC())
	_, err = w.db.Exec(ctx, `
		INSERT INTO weekly_digests(user_id, week_start, items, created_at, updated_at)
		VALUES ($1, $2::date, $3::jsonb, NOW(), NOW())
		ON CONFLICT (user_id, week_start)
		DO UPDATE SET
			items = EXCLUDED.items,
			updated_at = NOW()
	`, strings.TrimSpace(userID), weekStart.Format("2006-01-02"), payload)
	return err
}

func (w *Worker) collectWeeklyDigestCandidates(ctx context.Context, userID string, limit int) ([]weeklyDigestCandidate, error) {
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}

	rows, err := w.db.Query(ctx, `
		WITH engagement AS (
			SELECT
				battle_id,
				COUNT(*) FILTER (WHERE event_name = 'battle_shared')::int AS shares,
				COUNT(*) FILTER (WHERE event_name = 'remix_completed')::int AS remixes
			FROM (
				SELECT
					event_name,
					COALESCE(NULLIF(metadata->>'source_battle_id', ''), NULLIF(metadata->>'battle_id', '')) AS battle_id
				FROM events
				WHERE created_at >= NOW() - INTERVAL '14 days'
				  AND event_name IN ('battle_shared', 'remix_completed')
			) counts
			WHERE COALESCE(battle_id, '') <> ''
			GROUP BY battle_id
		),
		seen AS (
			SELECT DISTINCT battle_id
			FROM (
				SELECT
					COALESCE(NULLIF(metadata->>'source_battle_id', ''), NULLIF(metadata->>'battle_id', '')) AS battle_id
				FROM events
				WHERE user_id = $1::uuid
				  AND created_at >= NOW() - INTERVAL '14 days'
				  AND event_name IN ('public_battle_viewed', 'battle_shared', 'remix_started', 'remix_completed', 'notification_clicked')
			) viewed
			WHERE COALESCE(battle_id, '') <> ''
		)
		SELECT
			p.id::text,
			p.room_id::text,
			COALESCE(rm.name, ''),
			p.content,
			p.created_at,
			COALESCE(eng.shares, 0)::int,
			COALESCE(eng.remixes, 0)::int,
			CASE WHEN pf.followed_persona_id IS NULL THEN FALSE ELSE TRUE END,
			(
				(CASE WHEN pf.followed_persona_id IS NULL THEN 0 ELSE 6 END)
				+ (COALESCE(eng.shares, 0) * 2)
				+ (COALESCE(eng.remixes, 0) * 3)
				+ GREATEST(0, 96 - (EXTRACT(EPOCH FROM (NOW() - p.created_at)) / 3600.0))
			)::float8 AS score
		FROM posts p
		JOIN rooms rm ON rm.id = p.room_id
		LEFT JOIN engagement eng ON eng.battle_id = p.id::text
		LEFT JOIN persona_follows pf
			ON pf.followed_persona_id = p.persona_id
		   AND pf.follower_user_id = $1::uuid
		LEFT JOIN seen s ON s.battle_id = p.id::text
		WHERE p.status = 'PUBLISHED'
		  AND p.user_id <> $1::uuid
		  AND p.created_at >= NOW() - INTERVAL '7 days'
		  AND s.battle_id IS NULL
		ORDER BY score DESC, p.created_at DESC
		LIMIT $2
	`, strings.TrimSpace(userID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]weeklyDigestCandidate, 0, limit)
	for rows.Next() {
		var item weeklyDigestCandidate
		if err := rows.Scan(
			&item.BattleID,
			&item.RoomID,
			&item.RoomName,
			&item.Content,
			&item.CreatedAt,
			&item.Shares,
			&item.Remixes,
			&item.IsFollowed,
			&item.Score,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (w *Worker) summarizeWeeklyDigestBattle(ctx context.Context, candidate weeklyDigestCandidate, topic string) string {
	replies, err := w.listWeeklyDigestReplies(ctx, candidate.BattleID, 4)
	if err == nil {
		aiSummary, aiErr := w.llm.SummarizeThread(ctx, ai.PostContext{ID: candidate.BattleID, Content: candidate.Content}, replies)
		if aiErr == nil {
			summary := normalizeWeeklyOneSentence(aiSummary, 220)
			if summary != "" {
				return summary
			}
		}
	}

	return fallbackWeeklyDigestSummary(topic, candidate.RoomName, candidate.Shares, candidate.Remixes)
}

func (w *Worker) listWeeklyDigestReplies(ctx context.Context, battleID string, limit int) ([]ai.ReplyContext, error) {
	if limit <= 0 {
		limit = 4
	}
	if limit > 10 {
		limit = 10
	}

	rows, err := w.db.Query(ctx, `
		SELECT id::text, content
		FROM replies
		WHERE post_id = $1
		ORDER BY created_at ASC
		LIMIT $2
	`, strings.TrimSpace(battleID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	thread := make([]ai.ReplyContext, 0, limit)
	for rows.Next() {
		var reply ai.ReplyContext
		if err := rows.Scan(&reply.ID, &reply.Content); err != nil {
			return nil, err
		}
		thread = append(thread, reply)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return thread, nil
}

func extractWeeklyDigestTopic(content, roomName string) string {
	lines := strings.Split(strings.TrimSpace(content), "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "topic:") {
			topic := strings.TrimSpace(trimmed[len("topic:"):])
			if topic != "" {
				return common.TruncateRunes(topic, 140)
			}
		}
	}

	clean := strings.TrimSpace(content)
	if clean != "" {
		if idx := strings.IndexAny(clean, ".!?\n"); idx > 0 {
			clean = clean[:idx]
		}
		clean = common.TruncateRunes(clean, 140)
		if clean != "" {
			return clean
		}
	}

	if strings.TrimSpace(roomName) != "" {
		return common.TruncateRunes(fmt.Sprintf("Battle in %s", strings.TrimSpace(roomName)), 140)
	}
	return "Battle discussion"
}

func normalizeWeeklyOneSentence(value string, maxRunes int) string {
	clean := strings.Join(strings.Fields(strings.ReplaceAll(value, "\n", " ")), " ")
	if clean == "" {
		return ""
	}

	end := -1
	for idx, r := range clean {
		if r == '.' || r == '!' || r == '?' {
			if idx >= 20 {
				end = idx + 1
				break
			}
		}
	}

	if end > 0 && end <= len(clean) {
		clean = strings.TrimSpace(clean[:end])
	} else {
		clean = strings.TrimSpace(common.TruncateRunes(clean, maxRunes))
		clean = strings.TrimRight(clean, ". ")
		if clean == "" {
			return ""
		}
		clean += "."
	}

	clean = strings.TrimSpace(common.TruncateRunes(clean, maxRunes))
	if clean == "" {
		return ""
	}
	if !strings.HasSuffix(clean, ".") && !strings.HasSuffix(clean, "!") && !strings.HasSuffix(clean, "?") {
		clean += "."
	}
	return clean
}

func fallbackWeeklyDigestSummary(topic, roomName string, shares, remixes int) string {
	label := strings.TrimSpace(topic)
	if label == "" {
		label = "This battle"
	}

	if shares > 0 || remixes > 0 {
		return normalizeWeeklyOneSentence(fmt.Sprintf("%s gained traction this week with %d shares and %d remixes.", label, shares, remixes), 220)
	}

	if strings.TrimSpace(roomName) != "" {
		return normalizeWeeklyOneSentence(fmt.Sprintf("%s was one of the notable discussions in %s this week.", label, strings.TrimSpace(roomName)), 220)
	}
	return normalizeWeeklyOneSentence(fmt.Sprintf("%s was one of the notable discussions this week.", label), 220)
}

func roundWeeklyScore(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
}

func startOfWeekUTC(value time.Time) time.Time {
	utc := value.UTC()
	midnight := time.Date(utc.Year(), utc.Month(), utc.Day(), 0, 0, 0, 0, time.UTC)
	weekday := int(midnight.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	return midnight.AddDate(0, 0, -(weekday - 1))
}
