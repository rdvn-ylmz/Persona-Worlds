package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	eventPersonaCreated       = "persona_created"
	eventPreviewGenerated     = "preview_generated"
	eventPostApproved         = "post_approved"
	eventBattleCreated        = "battle_created"
	eventBattleShared         = "battle_shared"
	eventPublicProfileViewed  = "public_profile_viewed"
	eventPublicBattleViewed   = "public_battle_viewed"
	eventSignupFromShare      = "signup_from_share"
	eventRemixClick           = "remix_click" // kept for backward compatibility
	eventRemixClicked         = "remix_clicked"
	eventRemixStarted         = "remix_started"
	eventRemixCompleted       = "remix_completed"
	eventFollowClick          = "follow_click"
	eventDailyReturn          = "daily_return"
	eventNotificationClicked  = "notification_clicked"
	eventTemplateUsedFromFeed = "template_used_from_feed"
)

var (
	errUnsupportedEventName = errors.New("unsupported event_name")
	supportedEventNames     = map[string]struct{}{
		eventPersonaCreated:       {},
		eventPreviewGenerated:     {},
		eventPostApproved:         {},
		eventBattleCreated:        {},
		eventBattleShared:         {},
		eventPublicProfileViewed:  {},
		eventPublicBattleViewed:   {},
		eventSignupFromShare:      {},
		eventRemixClick:           {},
		eventRemixClicked:         {},
		eventRemixStarted:         {},
		eventRemixCompleted:       {},
		eventFollowClick:          {},
		eventDailyReturn:          {},
		eventNotificationClicked:  {},
		eventTemplateUsedFromFeed: {},
	}
	analyticsSummaryEvents = []string{
		eventBattleShared,
		eventPublicProfileViewed,
		eventPublicBattleViewed,
		eventSignupFromShare,
		eventPersonaCreated,
		eventPreviewGenerated,
		eventPostApproved,
		eventBattleCreated,
		eventRemixClick,
		eventRemixClicked,
		eventRemixStarted,
		eventRemixCompleted,
		eventFollowClick,
		eventDailyReturn,
		eventNotificationClicked,
		eventTemplateUsedFromFeed,
	}
)

type eventLoggerContextKey struct{}

type requestEventLogger struct {
	server *Server
	userID string
}

func (s *Server) eventLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID, _ := s.optionalUserIDFromRequest(r)
		ctx := context.WithValue(r.Context(), eventLoggerContextKey{}, &requestEventLogger{
			server: s,
			userID: strings.TrimSpace(userID),
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func eventLoggerFromContext(ctx context.Context) *requestEventLogger {
	logger, _ := ctx.Value(eventLoggerContextKey{}).(*requestEventLogger)
	return logger
}

func (l *requestEventLogger) log(ctx context.Context, eventName string, metadata map[string]any) error {
	if l == nil || l.server == nil {
		return nil
	}
	return l.server.insertEvent(ctx, l.userID, eventName, metadata)
}

func (s *Server) logEventFromRequest(r *http.Request, eventName string, metadata map[string]any) error {
	if r == nil {
		return nil
	}
	if logger := eventLoggerFromContext(r.Context()); logger != nil {
		return logger.log(r.Context(), eventName, metadata)
	}
	userID, _ := s.optionalUserIDFromRequest(r)
	return s.insertEvent(r.Context(), userID, eventName, metadata)
}

func (s *Server) insertEvent(ctx context.Context, userID, eventName string, metadata map[string]any) error {
	name := strings.ToLower(strings.TrimSpace(eventName))
	if _, ok := supportedEventNames[name]; !ok {
		return errUnsupportedEventName
	}

	payload, err := json.Marshal(sanitizeEventMetadata(metadata))
	if err != nil {
		return err
	}

	var userIDArg any
	if strings.TrimSpace(userID) != "" {
		userIDArg = strings.TrimSpace(userID)
	}

	_, err = s.db.Exec(ctx, `
		INSERT INTO events(user_id, event_name, metadata)
		VALUES ($1, $2, $3::jsonb)
	`, userIDArg, name, payload)
	return err
}

func (s *Server) handleCreateEvent(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventName string         `json:"event_name"`
		Metadata  map[string]any `json:"metadata"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeBadRequest(w, err.Error())
		return
	}

	if err := s.logEventFromRequest(r, req.EventName, req.Metadata); err != nil {
		if errors.Is(err, errUnsupportedEventName) {
			writeBadRequest(w, "unsupported event_name")
			return
		}
		writeInternalError(w, "could not record event")
		return
	}

	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleAnalyticsSummary(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUserID(w, r); !ok {
		return
	}

	now := time.Now().UTC()
	last24h, err := s.countEventsSince(r.Context(), now.Add(-24*time.Hour))
	if err != nil {
		writeInternalError(w, "could not compute analytics summary")
		return
	}
	last7d, err := s.countEventsSince(r.Context(), now.Add(-7*24*time.Hour))
	if err != nil {
		writeInternalError(w, "could not compute analytics summary")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"generated_at": now.Format(time.RFC3339),
		"last_24h":     last24h,
		"last_7d":      last7d,
		"funnel_7d": map[string]int{
			"share":   last7d[eventBattleShared],
			"view":    last7d[eventPublicProfileViewed],
			"signup":  last7d[eventSignupFromShare],
			"persona": last7d[eventPersonaCreated],
			"battle":  last7d[eventBattleCreated],
		},
		"remix_funnel_7d": map[string]int{
			"public_views":    last7d[eventPublicBattleViewed],
			"remix_clicked":   last7d[eventRemixClicked],
			"remix_started":   last7d[eventRemixStarted],
			"remix_completed": last7d[eventRemixCompleted],
		},
		"retention_7d": map[string]int{
			"daily_return":            last7d[eventDailyReturn],
			"notification_clicked":    last7d[eventNotificationClicked],
			"template_used_from_feed": last7d[eventTemplateUsedFromFeed],
		},
	})
}

func (s *Server) countEventsSince(ctx context.Context, since time.Time) (map[string]int, error) {
	counts := make(map[string]int, len(analyticsSummaryEvents))
	for _, eventName := range analyticsSummaryEvents {
		counts[eventName] = 0
	}

	rows, err := s.db.Query(ctx, `
		SELECT event_name, COUNT(*)::int
		FROM events
		WHERE created_at >= $1
		GROUP BY event_name
	`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			eventName string
			count     int
		)
		if err := rows.Scan(&eventName, &count); err != nil {
			return nil, err
		}
		eventName = strings.TrimSpace(strings.ToLower(eventName))
		if eventName == "" {
			continue
		}
		counts[eventName] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return counts, nil
}

func sanitizeEventMetadata(metadata map[string]any) map[string]any {
	if metadata == nil {
		return map[string]any{}
	}

	out := map[string]any{}
	for key, value := range metadata {
		cleanKey := strings.TrimSpace(key)
		if cleanKey == "" || isRawTextMetadataKey(cleanKey) {
			continue
		}
		cleanValue := sanitizeEventValue(value, 0)
		if cleanValue == nil {
			continue
		}
		out[cleanKey] = cleanValue
	}
	return out
}

func sanitizeEventValue(value any, depth int) any {
	if depth >= 4 || value == nil {
		return nil
	}

	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return nil
		}
		return truncateRunes(trimmed, 180)
	case bool, float32, float64, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return typed
	case []string:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			clean := sanitizeEventValue(item, depth+1)
			if asString, ok := clean.(string); ok && asString != "" {
				items = append(items, asString)
			}
			if len(items) == 25 {
				break
			}
		}
		if len(items) == 0 {
			return nil
		}
		return items
	case []any:
		items := make([]any, 0, len(typed))
		for _, item := range typed {
			clean := sanitizeEventValue(item, depth+1)
			if clean != nil {
				items = append(items, clean)
			}
			if len(items) == 25 {
				break
			}
		}
		if len(items) == 0 {
			return nil
		}
		return items
	case map[string]any:
		object := map[string]any{}
		for key, item := range typed {
			cleanKey := strings.TrimSpace(key)
			if cleanKey == "" || isRawTextMetadataKey(cleanKey) {
				continue
			}
			clean := sanitizeEventValue(item, depth+1)
			if clean != nil {
				object[cleanKey] = clean
			}
			if len(object) == 25 {
				break
			}
		}
		if len(object) == 0 {
			return nil
		}
		return object
	default:
		coerced := strings.TrimSpace(fmt.Sprintf("%v", typed))
		if coerced == "" || coerced == "<nil>" {
			return nil
		}
		return truncateRunes(coerced, 180)
	}
}

func isRawTextMetadataKey(key string) bool {
	lower := strings.ToLower(strings.TrimSpace(key))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "content") ||
		strings.Contains(lower, "message") ||
		strings.Contains(lower, "text") ||
		strings.Contains(lower, "body")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
