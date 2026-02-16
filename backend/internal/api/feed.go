package api

import (
	"context"
	"math"
	"net/http"
	"sort"
	"strings"
	"time"

	"personaworlds/backend/internal/common"
)

type FeedBattleTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FeedBattle struct {
	BattleID    string              `json:"battle_id"`
	RoomID      string              `json:"room_id"`
	RoomName    string              `json:"room_name"`
	PersonaID   string              `json:"persona_id,omitempty"`
	PersonaName string              `json:"persona_name,omitempty"`
	Topic       string              `json:"topic"`
	CreatedAt   time.Time           `json:"created_at"`
	Shares      int                 `json:"shares"`
	Remixes     int                 `json:"remixes"`
	Template    *FeedBattleTemplate `json:"template,omitempty"`
}

type FeedTemplate struct {
	TemplateID  string    `json:"template_id"`
	Name        string    `json:"name"`
	PromptRules string    `json:"prompt_rules"`
	TurnCount   int       `json:"turn_count"`
	WordLimit   int       `json:"word_limit"`
	CreatedAt   time.Time `json:"created_at"`
	UsageCount  int       `json:"usage_count"`
	IsTrending  bool      `json:"is_trending"`
}

type FeedItem struct {
	ID       string        `json:"id"`
	Kind     string        `json:"kind"`
	Reason   string        `json:"reason"`
	Reasons  []string      `json:"reasons"`
	Score    float64       `json:"score"`
	Battle   *FeedBattle   `json:"battle,omitempty"`
	Template *FeedTemplate `json:"template,omitempty"`
}

type FeedResponse struct {
	Items             []FeedItem    `json:"items"`
	HighlightTemplate *FeedTemplate `json:"highlight_template,omitempty"`
}

type feedBattleCandidate struct {
	BattleID     string
	RoomID       string
	RoomName     string
	PersonaID    string
	PersonaName  string
	Content      string
	CreatedAt    time.Time
	Shares       int
	Remixes      int
	TemplateID   string
	TemplateName string
}

type feedTemplateCandidate struct {
	TemplateID  string
	Name        string
	PromptRules string
	TurnCount   int
	WordLimit   int
	CreatedAt   time.Time
	UsageCount  int
	IsTrending  bool
}

func (s *Server) handleGetFeed(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	feed, err := s.buildFeed(r.Context(), userID)
	if err != nil {
		writeInternalError(w, "could not build feed")
		return
	}

	writeJSON(w, http.StatusOK, feed)
}

func (s *Server) buildFeed(ctx context.Context, userID string) (FeedResponse, error) {
	followedBattles, err := s.listFollowedBattlesForFeed(ctx, userID, 20)
	if err != nil {
		return FeedResponse{}, err
	}

	trendingBattles, err := s.listTrendingBattlesForFeed(ctx, userID, 20)
	if err != nil {
		return FeedResponse{}, err
	}

	templates, err := s.listNewTemplatesForFeed(ctx, 12)
	if err != nil {
		return FeedResponse{}, err
	}

	highlightTemplate := selectHighlightTemplate(templates)
	if highlightTemplate != nil {
		for idx := range templates {
			if strings.TrimSpace(templates[idx].TemplateID) == strings.TrimSpace(highlightTemplate.TemplateID) {
				templates[idx].IsTrending = true
				break
			}
		}
	}

	itemsByKey := map[string]*FeedItem{}
	reasonsByKey := map[string]map[string]struct{}{}

	addReason := func(key, reason string) {
		if strings.TrimSpace(reason) == "" {
			return
		}
		reasonSet, exists := reasonsByKey[key]
		if !exists {
			reasonSet = map[string]struct{}{}
			reasonsByKey[key] = reasonSet
		}
		reasonSet[strings.TrimSpace(reason)] = struct{}{}
	}

	addBattle := func(candidate feedBattleCandidate, reason string, score float64) {
		if strings.TrimSpace(candidate.BattleID) == "" {
			return
		}

		key := "battle:" + strings.TrimSpace(candidate.BattleID)
		item, exists := itemsByKey[key]
		if !exists {
			battle := &FeedBattle{
				BattleID:    candidate.BattleID,
				RoomID:      candidate.RoomID,
				RoomName:    candidate.RoomName,
				PersonaID:   candidate.PersonaID,
				PersonaName: candidate.PersonaName,
				Topic:       buildBattleCardTopic(candidate.Content, candidate.RoomName),
				CreatedAt:   candidate.CreatedAt,
				Shares:      candidate.Shares,
				Remixes:     candidate.Remixes,
			}
			if strings.TrimSpace(candidate.TemplateID) != "" {
				battle.Template = &FeedBattleTemplate{
					ID:   candidate.TemplateID,
					Name: strings.TrimSpace(candidate.TemplateName),
				}
			}
			item = &FeedItem{
				ID:     key,
				Kind:   "battle",
				Reason: strings.TrimSpace(reason),
				Score:  roundFeedScore(score),
				Battle: battle,
			}
			itemsByKey[key] = item
		} else {
			if item.Battle != nil {
				if candidate.Shares > item.Battle.Shares {
					item.Battle.Shares = candidate.Shares
				}
				if candidate.Remixes > item.Battle.Remixes {
					item.Battle.Remixes = candidate.Remixes
				}
			}
			if score > item.Score {
				item.Score = roundFeedScore(score)
				item.Reason = strings.TrimSpace(reason)
			}
		}
		addReason(key, reason)
	}

	addTemplate := func(candidate feedTemplateCandidate, reason string, score float64) {
		if strings.TrimSpace(candidate.TemplateID) == "" {
			return
		}
		key := "template:" + strings.TrimSpace(candidate.TemplateID)
		item, exists := itemsByKey[key]
		if !exists {
			template := &FeedTemplate{
				TemplateID:  candidate.TemplateID,
				Name:        candidate.Name,
				PromptRules: common.TruncateRunes(candidate.PromptRules, 220),
				TurnCount:   candidate.TurnCount,
				WordLimit:   candidate.WordLimit,
				CreatedAt:   candidate.CreatedAt,
				UsageCount:  candidate.UsageCount,
				IsTrending:  candidate.IsTrending,
			}
			item = &FeedItem{
				ID:       key,
				Kind:     "template",
				Reason:   strings.TrimSpace(reason),
				Score:    roundFeedScore(score),
				Template: template,
			}
			itemsByKey[key] = item
		} else if score > item.Score {
			item.Score = roundFeedScore(score)
			item.Reason = strings.TrimSpace(reason)
		}
		addReason(key, reason)
	}

	for _, battle := range followedBattles {
		addBattle(battle, "followed_persona", scoreFollowedBattle(battle.CreatedAt, battle.Shares, battle.Remixes))
	}

	for _, battle := range trendingBattles {
		addBattle(battle, "trending_battle", scoreTrendingBattle(battle.CreatedAt, battle.Shares, battle.Remixes))
	}

	for _, template := range templates {
		addTemplate(template, "new_template", scoreNewTemplate(template.CreatedAt, template.UsageCount))
	}

	items := make([]FeedItem, 0, len(itemsByKey))
	for key, item := range itemsByKey {
		reasons := sortedFeedReasons(reasonsByKey[key])
		if reasons == nil {
			reasons = []string{}
		}
		item.Reasons = reasons
		if strings.TrimSpace(item.Reason) == "" && len(reasons) > 0 {
			item.Reason = reasons[0]
		}
		items = append(items, *item)
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].Score == items[j].Score {
			return feedItemCreatedAt(items[i]).After(feedItemCreatedAt(items[j]))
		}
		return items[i].Score > items[j].Score
	})

	if len(items) > 50 {
		items = items[:50]
	}

	response := FeedResponse{Items: items}
	if response.Items == nil {
		response.Items = []FeedItem{}
	}
	if highlightTemplate != nil {
		copyTemplate := *highlightTemplate
		response.HighlightTemplate = &copyTemplate
	}

	return response, nil
}

func (s *Server) listFollowedBattlesForFeed(ctx context.Context, userID string, limit int) ([]feedBattleCandidate, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		WITH event_counts AS (
			SELECT
				COALESCE(NULLIF(e.metadata->>'source_battle_id', ''), NULLIF(e.metadata->>'battle_id', '')) AS battle_id,
				COUNT(*) FILTER (WHERE e.event_name = 'battle_shared')::int AS shares,
				COUNT(*) FILTER (WHERE e.event_name = 'remix_completed')::int AS remixes
			FROM events e
			WHERE e.created_at >= NOW() - INTERVAL '14 days'
			  AND e.event_name IN ('battle_shared', 'remix_completed')
			GROUP BY COALESCE(NULLIF(e.metadata->>'source_battle_id', ''), NULLIF(e.metadata->>'battle_id', ''))
		)
		SELECT
			p.id::text,
			p.room_id::text,
			COALESCE(rm.name, ''),
			COALESCE(p.persona_id::text, ''),
			COALESCE(pr.name, ''),
			p.content,
			p.created_at,
			COALESCE(ec.shares, 0)::int,
			COALESCE(ec.remixes, 0)::int,
			COALESCE(p.template_id::text, ''),
			COALESCE(t.name, '')
		FROM posts p
		JOIN persona_follows pf
			ON pf.followed_persona_id = p.persona_id
		   AND pf.follower_user_id = $1::uuid
		JOIN rooms rm ON rm.id = p.room_id
		LEFT JOIN personas pr ON pr.id = p.persona_id
		LEFT JOIN templates t ON t.id = p.template_id
		LEFT JOIN event_counts ec ON ec.battle_id = p.id::text
		WHERE p.status = 'PUBLISHED'
		ORDER BY p.created_at DESC
		LIMIT $2
	`, strings.TrimSpace(userID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]feedBattleCandidate, 0, limit)
	for rows.Next() {
		var item feedBattleCandidate
		if err := rows.Scan(
			&item.BattleID,
			&item.RoomID,
			&item.RoomName,
			&item.PersonaID,
			&item.PersonaName,
			&item.Content,
			&item.CreatedAt,
			&item.Shares,
			&item.Remixes,
			&item.TemplateID,
			&item.TemplateName,
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

func (s *Server) listTrendingBattlesForFeed(ctx context.Context, userID string, limit int) ([]feedBattleCandidate, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		WITH event_counts AS (
			SELECT
				COALESCE(NULLIF(e.metadata->>'source_battle_id', ''), NULLIF(e.metadata->>'battle_id', '')) AS battle_id,
				COUNT(*) FILTER (WHERE e.event_name = 'battle_shared')::int AS shares,
				COUNT(*) FILTER (WHERE e.event_name = 'remix_completed')::int AS remixes
			FROM events e
			WHERE e.created_at >= NOW() - INTERVAL '14 days'
			  AND e.event_name IN ('battle_shared', 'remix_completed')
			GROUP BY COALESCE(NULLIF(e.metadata->>'source_battle_id', ''), NULLIF(e.metadata->>'battle_id', ''))
		)
		SELECT
			p.id::text,
			p.room_id::text,
			COALESCE(rm.name, ''),
			COALESCE(p.persona_id::text, ''),
			COALESCE(pr.name, ''),
			p.content,
			p.created_at,
			COALESCE(ec.shares, 0)::int,
			COALESCE(ec.remixes, 0)::int,
			COALESCE(p.template_id::text, ''),
			COALESCE(t.name, '')
		FROM posts p
		JOIN rooms rm ON rm.id = p.room_id
		LEFT JOIN personas pr ON pr.id = p.persona_id
		LEFT JOIN templates t ON t.id = p.template_id
		LEFT JOIN event_counts ec ON ec.battle_id = p.id::text
		WHERE p.status = 'PUBLISHED'
		  AND p.user_id <> $1::uuid
		  AND (COALESCE(ec.shares, 0) > 0 OR COALESCE(ec.remixes, 0) > 0)
		ORDER BY
			(COALESCE(ec.shares, 0) * 2 + COALESCE(ec.remixes, 0) * 4) DESC,
			p.created_at DESC
		LIMIT $2
	`, strings.TrimSpace(userID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]feedBattleCandidate, 0, limit)
	for rows.Next() {
		var item feedBattleCandidate
		if err := rows.Scan(
			&item.BattleID,
			&item.RoomID,
			&item.RoomName,
			&item.PersonaID,
			&item.PersonaName,
			&item.Content,
			&item.CreatedAt,
			&item.Shares,
			&item.Remixes,
			&item.TemplateID,
			&item.TemplateName,
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

func (s *Server) listNewTemplatesForFeed(ctx context.Context, limit int) ([]feedTemplateCandidate, error) {
	if limit <= 0 {
		limit = 12
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			t.id::text,
			t.name,
			t.prompt_rules,
			t.turn_count,
			t.word_limit,
			t.created_at,
			COALESCE(usage.usage_count, 0)::int
		FROM templates t
		LEFT JOIN LATERAL (
			SELECT COUNT(*)::int AS usage_count
			FROM posts p
			WHERE p.template_id = t.id
			  AND p.status = 'PUBLISHED'
			  AND p.created_at >= NOW() - INTERVAL '30 days'
		) usage ON TRUE
		WHERE t.is_public = TRUE
		ORDER BY t.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]feedTemplateCandidate, 0, limit)
	for rows.Next() {
		var item feedTemplateCandidate
		if err := rows.Scan(
			&item.TemplateID,
			&item.Name,
			&item.PromptRules,
			&item.TurnCount,
			&item.WordLimit,
			&item.CreatedAt,
			&item.UsageCount,
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

func selectHighlightTemplate(candidates []feedTemplateCandidate) *FeedTemplate {
	if len(candidates) == 0 {
		return nil
	}

	sorted := make([]feedTemplateCandidate, len(candidates))
	copy(sorted, candidates)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].UsageCount == sorted[j].UsageCount {
			return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
		}
		return sorted[i].UsageCount > sorted[j].UsageCount
	})

	selected := sorted[0]
	return &FeedTemplate{
		TemplateID:  selected.TemplateID,
		Name:        selected.Name,
		PromptRules: common.TruncateRunes(selected.PromptRules, 220),
		TurnCount:   selected.TurnCount,
		WordLimit:   selected.WordLimit,
		CreatedAt:   selected.CreatedAt,
		UsageCount:  selected.UsageCount,
		IsTrending:  true,
	}
}

func scoreFollowedBattle(createdAt time.Time, shares, remixes int) float64 {
	ageHours := time.Since(createdAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	return 95 + float64(shares*2+remixes*4) - ageHours*0.35
}

func scoreTrendingBattle(createdAt time.Time, shares, remixes int) float64 {
	ageHours := time.Since(createdAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	return 70 + float64(shares*3+remixes*5) - ageHours*0.25
}

func scoreNewTemplate(createdAt time.Time, usageCount int) float64 {
	ageHours := time.Since(createdAt).Hours()
	if ageHours < 0 {
		ageHours = 0
	}
	return 60 + float64(usageCount*2) - ageHours*0.12
}

func roundFeedScore(value float64) float64 {
	return math.Round(value*100) / 100
}

func sortedFeedReasons(reasonSet map[string]struct{}) []string {
	if len(reasonSet) == 0 {
		return []string{}
	}

	reasons := make([]string, 0, len(reasonSet))
	for reason := range reasonSet {
		reasons = append(reasons, reason)
	}

	rank := map[string]int{
		"followed_persona": 1,
		"trending_battle":  2,
		"new_template":     3,
	}
	sort.Slice(reasons, func(i, j int) bool {
		ri, iExists := rank[reasons[i]]
		rj, jExists := rank[reasons[j]]
		if iExists && jExists && ri != rj {
			return ri < rj
		}
		if iExists != jExists {
			return iExists
		}
		return reasons[i] < reasons[j]
	})
	return reasons
}

func feedItemCreatedAt(item FeedItem) time.Time {
	if item.Battle != nil {
		return item.Battle.CreatedAt
	}
	if item.Template != nil {
		return item.Template.CreatedAt
	}
	return time.Time{}
}
