package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
)

type WeeklyDigestItem struct {
	BattleID  string    `json:"battle_id"`
	RoomID    string    `json:"room_id"`
	RoomName  string    `json:"room_name"`
	Topic     string    `json:"topic"`
	Summary   string    `json:"summary"`
	Score     float64   `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

type WeeklyDigest struct {
	WeekStart   string             `json:"week_start"`
	GeneratedAt time.Time          `json:"generated_at"`
	Items       []WeeklyDigestItem `json:"items"`
}

func (s *Server) handleGetWeeklyDigest(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	now := time.Now().UTC()
	digest, exists, isCurrentWeek, err := s.getWeeklyDigest(r.Context(), userID, now)
	if err != nil {
		writeInternalError(w, "could not load weekly digest")
		return
	}
	if !exists {
		digest = emptyWeeklyDigest(now)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"digest":          digest,
		"exists":          exists,
		"is_current_week": isCurrentWeek,
	})
}

func (s *Server) getWeeklyDigest(ctx context.Context, userID string, now time.Time) (WeeklyDigest, bool, bool, error) {
	currentWeekStart := startOfWeekUTC(now)
	digest, found, err := s.getWeeklyDigestByWeek(ctx, userID, currentWeekStart)
	if err != nil {
		return WeeklyDigest{}, false, false, err
	}
	if found {
		return digest, true, true, nil
	}

	latest, found, err := s.getLatestWeeklyDigest(ctx, userID)
	if err != nil {
		return WeeklyDigest{}, false, false, err
	}
	if found {
		return latest, true, false, nil
	}

	return WeeklyDigest{}, false, false, nil
}

func (s *Server) getWeeklyDigestByWeek(ctx context.Context, userID string, weekStart time.Time) (WeeklyDigest, bool, error) {
	var (
		rowWeekStart time.Time
		itemsRaw     []byte
		updatedAt    time.Time
	)
	err := s.db.QueryRow(ctx, `
		SELECT week_start, items, updated_at
		FROM weekly_digests
		WHERE user_id = $1
		  AND week_start = $2::date
		LIMIT 1
	`, userID, weekStart.Format("2006-01-02")).Scan(&rowWeekStart, &itemsRaw, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WeeklyDigest{}, false, nil
		}
		return WeeklyDigest{}, false, err
	}

	items, err := decodeWeeklyDigestItems(itemsRaw)
	if err != nil {
		return WeeklyDigest{}, false, err
	}

	return WeeklyDigest{
		WeekStart:   rowWeekStart.UTC().Format("2006-01-02"),
		GeneratedAt: updatedAt,
		Items:       items,
	}, true, nil
}

func (s *Server) getLatestWeeklyDigest(ctx context.Context, userID string) (WeeklyDigest, bool, error) {
	var (
		rowWeekStart time.Time
		itemsRaw     []byte
		updatedAt    time.Time
	)
	err := s.db.QueryRow(ctx, `
		SELECT week_start, items, updated_at
		FROM weekly_digests
		WHERE user_id = $1
		ORDER BY week_start DESC
		LIMIT 1
	`, userID).Scan(&rowWeekStart, &itemsRaw, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return WeeklyDigest{}, false, nil
		}
		return WeeklyDigest{}, false, err
	}

	items, err := decodeWeeklyDigestItems(itemsRaw)
	if err != nil {
		return WeeklyDigest{}, false, err
	}

	return WeeklyDigest{
		WeekStart:   rowWeekStart.UTC().Format("2006-01-02"),
		GeneratedAt: updatedAt,
		Items:       items,
	}, true, nil
}

func decodeWeeklyDigestItems(raw []byte) ([]WeeklyDigestItem, error) {
	items := []WeeklyDigestItem{}
	if len(raw) == 0 {
		return items, nil
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, err
	}
	if items == nil {
		return []WeeklyDigestItem{}, nil
	}
	return items, nil
}

func emptyWeeklyDigest(now time.Time) WeeklyDigest {
	return WeeklyDigest{
		WeekStart:   startOfWeekUTC(now).Format("2006-01-02"),
		GeneratedAt: now,
		Items:       []WeeklyDigestItem{},
	}
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
