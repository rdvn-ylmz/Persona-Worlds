package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"personaworlds/backend/internal/common"

	"github.com/go-chi/chi/v5"
)

const (
	notificationTypeBattleRemixed = "battle_remixed"
	notificationTypeTemplateUsed  = "template_used"
	notificationTypePersonaFollow = "persona_followed"
)

type Notification struct {
	ID          int64          `json:"id"`
	ActorUserID string         `json:"actor_user_id,omitempty"`
	Type        string         `json:"type"`
	Title       string         `json:"title"`
	Body        string         `json:"body"`
	Metadata    map[string]any `json:"metadata"`
	ReadAt      *time.Time     `json:"read_at,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
}

func (s *Server) insertNotification(ctx context.Context, userID, actorUserID, notifType, title, body string, metadata map[string]any) error {
	cleanUserID := strings.TrimSpace(userID)
	if cleanUserID == "" {
		return nil
	}

	payload, err := json.Marshal(sanitizeEventMetadata(metadata))
	if err != nil {
		return err
	}

	var actorArg any
	if strings.TrimSpace(actorUserID) != "" {
		actorArg = strings.TrimSpace(actorUserID)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO notifications(user_id, actor_user_id, type, title, body, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)
	`, cleanUserID, actorArg, strings.TrimSpace(notifType), common.TruncateRunes(title, 120), common.TruncateRunes(body, 260), payload)
	return err
}

func (s *Server) unreadNotificationsCount(ctx context.Context, userID string) (int, error) {
	var count int
	err := s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM notifications
		WHERE user_id = $1
		  AND read_at IS NULL
	`, strings.TrimSpace(userID)).Scan(&count)
	return count, err
}

func (s *Server) listNotifications(ctx context.Context, userID string, limit int) ([]Notification, int, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}

	rows, err := s.db.Query(ctx, `
		SELECT
			id,
			COALESCE(actor_user_id::text, ''),
			type,
			title,
			body,
			metadata,
			read_at,
			created_at
		FROM notifications
		WHERE user_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, strings.TrimSpace(userID), limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	notifications := make([]Notification, 0, limit)
	for rows.Next() {
		var (
			notification Notification
			metadataRaw  []byte
		)
		if err := rows.Scan(
			&notification.ID,
			&notification.ActorUserID,
			&notification.Type,
			&notification.Title,
			&notification.Body,
			&metadataRaw,
			&notification.ReadAt,
			&notification.CreatedAt,
		); err != nil {
			return nil, 0, err
		}
		notification.Metadata = map[string]any{}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &notification.Metadata); err != nil {
				return nil, 0, err
			}
		}
		notifications = append(notifications, notification)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	unreadCount, err := s.unreadNotificationsCount(ctx, userID)
	if err != nil {
		return nil, 0, err
	}

	return notifications, unreadCount, nil
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	limit := 20
	if rawLimit := strings.TrimSpace(r.URL.Query().Get("limit")); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil {
			writeBadRequest(w, "limit must be a number")
			return
		}
		limit = parsed
	}

	notifications, unreadCount, err := s.listNotifications(r.Context(), userID, limit)
	if err != nil {
		writeInternalError(w, "could not load notifications")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"notifications": notifications,
		"unread_count":  unreadCount,
	})
}

func (s *Server) handleMarkNotificationRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	notificationID, err := strconv.ParseInt(strings.TrimSpace(chi.URLParam(r, "id")), 10, 64)
	if err != nil || notificationID <= 0 {
		writeBadRequest(w, "notification id is invalid")
		return
	}

	ct, err := s.db.Exec(r.Context(), `
		UPDATE notifications
		SET read_at = NOW()
		WHERE id = $1
		  AND user_id = $2
		  AND read_at IS NULL
	`, notificationID, strings.TrimSpace(userID))
	if err != nil {
		writeInternalError(w, "could not mark notification as read")
		return
	}

	unreadCount, err := s.unreadNotificationsCount(r.Context(), userID)
	if err != nil {
		writeInternalError(w, "could not load unread notifications")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updated":      ct.RowsAffected() > 0,
		"unread_count": unreadCount,
	})
}

func (s *Server) handleMarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	userID, ok := s.requireUserID(w, r)
	if !ok {
		return
	}

	ct, err := s.db.Exec(r.Context(), `
		UPDATE notifications
		SET read_at = NOW()
		WHERE user_id = $1
		  AND read_at IS NULL
	`, strings.TrimSpace(userID))
	if err != nil {
		writeInternalError(w, "could not mark notifications as read")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"updated":      ct.RowsAffected(),
		"unread_count": 0,
	})
}

func (s *Server) notifyPersonaFollowed(ctx context.Context, ownerUserID, actorUserID, personaID, slug string) error {
	cleanOwner := strings.TrimSpace(ownerUserID)
	cleanActor := strings.TrimSpace(actorUserID)
	if cleanOwner == "" || cleanOwner == cleanActor {
		return nil
	}

	return s.insertNotification(ctx, cleanOwner, cleanActor, notificationTypePersonaFollow,
		"New follower",
		"Your public persona just got a new follower.",
		map[string]any{
			"persona_id": strings.TrimSpace(personaID),
			"slug":       strings.TrimSpace(slug),
		},
	)
}

func (s *Server) notifyBattleRemixed(ctx context.Context, actorUserID, sourceBattleID, newBattleID string) error {
	cleanSource := strings.TrimSpace(sourceBattleID)
	if cleanSource == "" {
		return nil
	}

	var (
		ownerUserID string
		roomName    string
	)
	err := s.db.QueryRow(ctx, `
		SELECT p.user_id::text, COALESCE(r.name, '')
		FROM posts p
		LEFT JOIN rooms r ON r.id = p.room_id
		WHERE p.id = $1
	`, cleanSource).Scan(&ownerUserID, &roomName)
	if err != nil {
		return err
	}
	if strings.TrimSpace(ownerUserID) == "" || strings.TrimSpace(ownerUserID) == strings.TrimSpace(actorUserID) {
		return nil
	}

	body := "Someone remixed one of your battles."
	if strings.TrimSpace(roomName) != "" {
		body = fmt.Sprintf("Someone remixed your battle in %s.", strings.TrimSpace(roomName))
	}

	return s.insertNotification(ctx, ownerUserID, actorUserID, notificationTypeBattleRemixed,
		"Your battle was remixed",
		body,
		map[string]any{
			"source_battle_id": cleanSource,
			"battle_id":        strings.TrimSpace(newBattleID),
		},
	)
}

func (s *Server) notifyTemplateUsed(ctx context.Context, actorUserID string, template BattleTemplate, battleID string) error {
	ownerUserID := strings.TrimSpace(template.OwnerUserID)
	if ownerUserID == "" || ownerUserID == strings.TrimSpace(actorUserID) {
		return nil
	}

	body := "A template you created was used in a new battle."
	if strings.TrimSpace(template.Name) != "" {
		body = fmt.Sprintf("Your template \"%s\" was used in a new battle.", strings.TrimSpace(template.Name))
	}

	return s.insertNotification(ctx, ownerUserID, actorUserID, notificationTypeTemplateUsed,
		"Your template was used",
		body,
		map[string]any{
			"template_id": strings.TrimSpace(template.ID),
			"battle_id":   strings.TrimSpace(battleID),
		},
	)
}
