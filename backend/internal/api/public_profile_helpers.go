package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode"

	"personaworlds/backend/internal/auth"

	"github.com/jackc/pgx/v5"
)

func (s *Server) optionalUserIDFromRequest(r *http.Request) (string, bool) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", false
	}
	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", false
	}
	claims, err := auth.ParseToken(s.cfg.JWTSecret, parts[1])
	if err != nil {
		return "", false
	}
	if strings.TrimSpace(claims.UserID) == "" {
		return "", false
	}
	return strings.TrimSpace(claims.UserID), true
}

func (s *Server) getPublicProfileBySlug(ctx context.Context, slug string) (PublicPersonaProfile, string, error) {
	var (
		profile     PublicPersonaProfile
		ownerUserID string
	)
	err := s.db.QueryRow(ctx, `
		SELECT
			p.id::text,
			p.user_id::text,
			pp.slug,
			p.name,
			COALESCE(NULLIF(pp.bio, ''), p.bio),
			p.tone,
			p.preferred_language,
			p.formality,
			pp.is_public,
			pp.created_at,
			COALESCE((SELECT COUNT(*)::int FROM persona_follows f WHERE f.followed_persona_id = p.id), 0),
			COALESCE((SELECT COUNT(*)::int FROM posts ps WHERE ps.persona_id = p.id AND ps.status = 'PUBLISHED'), 0)
		FROM persona_public_profiles pp
		JOIN personas p ON p.id = pp.persona_id
		WHERE pp.slug = $1
		  AND pp.is_public = TRUE
	`, slug).Scan(
		&profile.PersonaID,
		&ownerUserID,
		&profile.Slug,
		&profile.Name,
		&profile.Bio,
		&profile.Tone,
		&profile.PreferredLanguage,
		&profile.Formality,
		&profile.IsPublic,
		&profile.CreatedAt,
		&profile.Followers,
		&profile.PostsCount,
	)
	if err != nil {
		return PublicPersonaProfile{}, "", err
	}
	profile.Badges = buildPublicProfileBadges(profile)
	return profile, ownerUserID, nil
}

func (s *Server) listPublishedPostsForPersona(ctx context.Context, personaID, cursor string, limit int) ([]PublicPost, string, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	var (
		rows pgx.Rows
		err  error
	)
	if strings.TrimSpace(cursor) == "" {
		rows, err = s.db.Query(ctx, `
			SELECT p.id::text, p.room_id::text, COALESCE(r.name, ''), p.authored_by::text, p.content, p.created_at
			FROM posts p
			LEFT JOIN rooms r ON r.id = p.room_id
			WHERE p.persona_id = $1
			  AND p.status = 'PUBLISHED'
			ORDER BY p.created_at DESC, p.id DESC
			LIMIT $2
		`, personaID, limit)
	} else {
		cursorTime, cursorID, parseErr := parsePublicPostCursor(cursor)
		if parseErr != nil {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		rows, err = s.db.Query(ctx, `
			SELECT p.id::text, p.room_id::text, COALESCE(r.name, ''), p.authored_by::text, p.content, p.created_at
			FROM posts p
			LEFT JOIN rooms r ON r.id = p.room_id
			WHERE p.persona_id = $1
			  AND p.status = 'PUBLISHED'
			  AND (p.created_at < $2 OR (p.created_at = $2 AND p.id < $3::uuid))
			ORDER BY p.created_at DESC, p.id DESC
			LIMIT $4
		`, personaID, cursorTime, cursorID, limit)
	}
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()

	posts := make([]PublicPost, 0, limit)
	for rows.Next() {
		var post PublicPost
		if err := rows.Scan(&post.ID, &post.RoomID, &post.RoomName, &post.AuthoredBy, &post.Content, &post.CreatedAt); err != nil {
			return nil, "", err
		}
		posts = append(posts, post)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(posts) == limit {
		last := posts[len(posts)-1]
		nextCursor = buildPublicPostCursor(last.CreatedAt, last.ID)
	}
	return posts, nextCursor, nil
}

func (s *Server) listTopRoomsForPersona(ctx context.Context, personaID string, limit int) ([]PublicRoomStat, error) {
	if limit <= 0 {
		limit = 3
	}
	if limit > 10 {
		limit = 10
	}

	rows, err := s.db.Query(ctx, `
		SELECT r.id::text, r.name, COUNT(*)::int AS post_count
		FROM posts p
		JOIN rooms r ON r.id = p.room_id
		WHERE p.persona_id = $1
		  AND p.status = 'PUBLISHED'
		GROUP BY r.id, r.name
		ORDER BY post_count DESC, r.name ASC
		LIMIT $2
	`, personaID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	rooms := make([]PublicRoomStat, 0, limit)
	for rows.Next() {
		var room PublicRoomStat
		if err := rows.Scan(&room.RoomID, &room.RoomName, &room.PostCount); err != nil {
			return nil, err
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return rooms, nil
}

func (s *Server) ensureUniquePublicProfileSlug(ctx context.Context, baseSlug, personaID string) (string, error) {
	base := normalizePublicSlug(baseSlug)
	if base == "" {
		base = "persona"
	}

	candidate := base
	for i := 0; i < 200; i++ {
		var existingPersonaID string
		err := s.db.QueryRow(ctx, `
			SELECT persona_id::text
			FROM persona_public_profiles
			WHERE slug = $1
		`, candidate).Scan(&existingPersonaID)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(existingPersonaID) == strings.TrimSpace(personaID) {
			return candidate, nil
		}
		candidate = fmt.Sprintf("%s-%d", base, i+2)
	}
	return "", fmt.Errorf("could not allocate slug")
}

func normalizePublicSlug(value string) string {
	raw := strings.ToLower(strings.TrimSpace(value))
	if raw == "" {
		return ""
	}

	var b strings.Builder
	prevDash := false
	for _, r := range raw {
		isASCIIAlpha := r >= 'a' && r <= 'z'
		isASCIIDigit := r >= '0' && r <= '9'
		if isASCIIAlpha || isASCIIDigit {
			b.WriteRune(r)
			prevDash = false
			continue
		}

		if unicode.IsSpace(r) || r == '-' || r == '_' {
			if !prevDash && b.Len() > 0 {
				b.WriteRune('-')
				prevDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > 64 {
		slug = strings.Trim(slug[:64], "-")
	}
	return slug
}

func buildPublicProfileBadges(profile PublicPersonaProfile) []string {
	badges := []string{"Public Persona"}

	language := strings.ToUpper(strings.TrimSpace(profile.PreferredLanguage))
	if language != "" {
		badges = append(badges, language)
	}

	tone := strings.TrimSpace(profile.Tone)
	if tone != "" {
		badges = append(badges, "Tone: "+tone)
	}

	if profile.Formality >= 2 {
		badges = append(badges, "Formal")
	} else {
		badges = append(badges, "Conversational")
	}
	return badges
}

func buildPublicPostCursor(createdAt time.Time, postID string) string {
	return fmt.Sprintf("%d|%s", createdAt.UTC().UnixNano(), strings.TrimSpace(postID))
}

func parsePublicPostCursor(cursor string) (time.Time, string, error) {
	parts := strings.SplitN(strings.TrimSpace(cursor), "|", 2)
	if len(parts) != 2 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}

	nanos, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	postID := strings.TrimSpace(parts[1])
	if postID == "" || len(postID) != 36 {
		return time.Time{}, "", fmt.Errorf("invalid cursor")
	}
	return time.Unix(0, nanos).UTC(), postID, nil
}
