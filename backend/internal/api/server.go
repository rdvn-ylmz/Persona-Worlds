package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/auth"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/safety"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	cfg                config.Config
	db                 *pgxpool.Pool
	llm                ai.LLMClient
	publicReadLimiter  *ipRateLimiter
	publicWriteLimiter *ipRateLimiter
}

type Persona struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	Bio               string    `json:"bio"`
	Tone              string    `json:"tone"`
	WritingSamples    []string  `json:"writing_samples"`
	DoNotSay          []string  `json:"do_not_say"`
	Catchphrases      []string  `json:"catchphrases"`
	PreferredLanguage string    `json:"preferred_language"`
	Formality         int       `json:"formality"`
	DailyDraftQuota   int       `json:"daily_draft_quota"`
	DailyReplyQuota   int       `json:"daily_reply_quota"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Room struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

type Post struct {
	ID         string    `json:"id"`
	RoomID     string    `json:"room_id"`
	PersonaID  string    `json:"persona_id,omitempty"`
	Persona    string    `json:"persona_name,omitempty"`
	AuthoredBy string    `json:"authored_by"`
	Status     string    `json:"status"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Reply struct {
	ID         string    `json:"id"`
	PostID     string    `json:"post_id"`
	PersonaID  string    `json:"persona_id,omitempty"`
	Persona    string    `json:"persona_name,omitempty"`
	AuthoredBy string    `json:"authored_by"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type PreviewDraft struct {
	Label      string `json:"label"`
	Content    string `json:"content"`
	AuthoredBy string `json:"authored_by"`
}

type DigestThread struct {
	PostID        string    `json:"post_id"`
	RoomID        string    `json:"room_id,omitempty"`
	RoomName      string    `json:"room_name,omitempty"`
	PostPreview   string    `json:"post_preview,omitempty"`
	ActivityCount int       `json:"activity_count"`
	LastActivity  time.Time `json:"last_activity_at"`
}

type DigestStats struct {
	Posts      int            `json:"posts"`
	Replies    int            `json:"replies"`
	TopThreads []DigestThread `json:"top_threads"`
}

type PersonaDigest struct {
	PersonaID   string      `json:"persona_id"`
	Date        string      `json:"date"`
	Summary     string      `json:"summary"`
	Stats       DigestStats `json:"stats"`
	HasActivity bool        `json:"has_activity"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

type dbExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type ipRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]rateLimitBucket
}

type rateLimitBucket struct {
	count       int
	windowStart time.Time
}

type PublicPersonaProfile struct {
	PersonaID         string    `json:"persona_id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Bio               string    `json:"bio"`
	Tone              string    `json:"tone"`
	PreferredLanguage string    `json:"preferred_language"`
	Formality         int       `json:"formality"`
	IsPublic          bool      `json:"is_public"`
	Followers         int       `json:"followers"`
	PostsCount        int       `json:"posts_count"`
	Badges            []string  `json:"badges"`
	CreatedAt         time.Time `json:"created_at"`
}

type PublicPost struct {
	ID         string    `json:"id"`
	RoomID     string    `json:"room_id"`
	RoomName   string    `json:"room_name"`
	AuthoredBy string    `json:"authored_by"`
	Content    string    `json:"content"`
	CreatedAt  time.Time `json:"created_at"`
}

type PublicRoomStat struct {
	RoomID    string `json:"room_id"`
	RoomName  string `json:"room_name"`
	PostCount int    `json:"post_count"`
}

type BattlePersona struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Bio               string `json:"bio"`
	Tone              string `json:"tone"`
	PreferredLanguage string `json:"preferred_language"`
	Formality         int    `json:"formality"`
}

type BattleVerdict struct {
	Verdict   string   `json:"verdict"`
	Takeaways []string `json:"takeaways"`
}

type BattleTurnMetadata struct {
	QualityScore int      `json:"quality_score"`
	QualityLabel string   `json:"quality_label"`
	Reasons      []string `json:"quality_reasons,omitempty"`
	Regenerated  bool     `json:"regenerated"`
}

type BattleTurn struct {
	BattleID  string             `json:"battle_id"`
	TurnIndex int                `json:"turn_index"`
	PersonaID string             `json:"persona_id"`
	Persona   string             `json:"persona_name"`
	Content   string             `json:"content"`
	Metadata  BattleTurnMetadata `json:"metadata"`
	CreatedAt time.Time          `json:"created_at"`
}

type Battle struct {
	ID        string        `json:"id"`
	RoomID    string        `json:"room_id"`
	RoomName  string        `json:"room_name"`
	Topic     string        `json:"topic"`
	Status    string        `json:"status"`
	PersonaA  BattlePersona `json:"persona_a"`
	PersonaB  BattlePersona `json:"persona_b"`
	Turns     []BattleTurn  `json:"turns"`
	Verdict   BattleVerdict `json:"verdict"`
	Error     string        `json:"error,omitempty"`
	CreatedAt time.Time     `json:"created_at"`
	UpdatedAt time.Time     `json:"updated_at"`
}

type PublicBattleSummary struct {
	Topic        string   `json:"topic"`
	VerdictText  string   `json:"verdict_text"`
	TopTakeaways []string `json:"top_takeaways"`
	RoomName     string   `json:"room_name"`
}

type BattleRemixPayload struct {
	RoomID string `json:"room_id"`
	Topic  string `json:"topic"`
}

const dailyBattleCreateLimit = 10

func New(cfg config.Config, db *pgxpool.Pool, llm ai.LLMClient) *Server {
	return &Server{
		cfg:                cfg,
		db:                 db,
		llm:                llm,
		publicReadLimiter:  newIPRateLimiter(120, time.Minute),
		publicWriteLimiter: newIPRateLimiter(30, time.Minute),
	}
}

func newIPRateLimiter(limit int, window time.Duration) *ipRateLimiter {
	return &ipRateLimiter{
		limit:   limit,
		window:  window,
		buckets: map[string]rateLimitBucket{},
	}
}

func (rl *ipRateLimiter) allow(key string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	bucket, exists := rl.buckets[key]
	if !exists || now.Sub(bucket.windowStart) >= rl.window {
		rl.buckets[key] = rateLimitBucket{
			count:       1,
			windowStart: now,
		}
		rl.gc(now)
		return true
	}

	if bucket.count >= rl.limit {
		return false
	}
	bucket.count++
	rl.buckets[key] = bucket
	return true
}

func (rl *ipRateLimiter) gc(now time.Time) {
	for key, bucket := range rl.buckets {
		if now.Sub(bucket.windowStart) >= rl.window*2 {
			delete(rl.buckets, key)
		}
	}
}

func (s *Server) publicReadRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := requestClientIP(r)
		if !s.publicReadLimiter.allow(clientIP, time.Now()) {
			writeError(w, http.StatusTooManyRequests, "public profile rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) publicWriteRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := requestClientIP(r)
		if !s.publicWriteLimiter.allow(clientIP, time.Now()) {
			writeError(w, http.StatusTooManyRequests, "follow rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return "unknown"
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{s.cfg.FrontendOrigin, "http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	r.Route("/auth", func(r chi.Router) {
		r.Post("/signup", s.handleSignup)
		r.Post("/login", s.handleLogin)
	})

	r.Route("/p/{slug}", func(r chi.Router) {
		r.With(s.publicReadRateLimitMiddleware).Get("/", s.handleGetPublicProfile)
		r.With(s.publicReadRateLimitMiddleware).Get("/posts", s.handleGetPublicProfilePosts)
		r.With(s.publicWriteRateLimitMiddleware).Post("/follow", s.handleFollowPublicProfile)
	})

	r.With(s.publicReadRateLimitMiddleware).Get("/b/{id}/summary", s.handleGetPublicBattleSummary)
	r.With(s.publicReadRateLimitMiddleware).Get("/b/{id}", s.handleGetPublicBattle)
	r.Post("/battles/{id}/remix", s.handleBattleRemix)

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.cfg.JWTSecret))

		r.Get("/personas", s.handleListPersonas)
		r.Post("/personas", s.handleCreatePersona)
		r.Get("/personas/{id}", s.handleGetPersona)
		r.Put("/personas/{id}", s.handleUpdatePersona)
		r.Delete("/personas/{id}", s.handleDeletePersona)
		r.Post("/personas/{id}/preview", s.handlePreviewPersona)
		r.Get("/personas/{id}/digest/today", s.handleGetTodayDigest)
		r.Get("/personas/{id}/digest/latest", s.handleGetLatestDigest)
		r.Post("/personas/{id}/publish-profile", s.handlePublishPersonaProfile)
		r.Post("/personas/{id}/unpublish-profile", s.handleUnpublishPersonaProfile)

		r.Get("/rooms", s.handleListRooms)
		r.Get("/rooms/{id}/posts", s.handleListRoomPosts)
		r.Post("/rooms/{id}/battles", s.handleCreateBattle)
		r.Post("/rooms/{id}/posts/draft", s.handleCreateDraft)
		r.Get("/battles/{id}", s.handleGetBattle)
		r.Post("/battles/{id}/regenerate", s.handleRegenerateBattle)
		r.Post("/posts/{id}/approve", s.handleApprovePost)
		r.Post("/posts/{id}/generate-replies", s.handleGenerateReplies)
		r.Get("/posts/{id}/thread", s.handleGetThread)
	})

	return r
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if !strings.Contains(req.Email, "@") || len(req.Password) < 8 {
		writeError(w, http.StatusBadRequest, "invalid email or password")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not hash password")
		return
	}

	var userID string
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO users(email, password_hash)
		VALUES ($1, $2)
		RETURNING id::text
	`, req.Email, hash).Scan(&userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "email already registered")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create user")
		return
	}

	token, err := auth.CreateToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{"token": token, "user_id": userID})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Email = strings.TrimSpace(strings.ToLower(req.Email))

	var userID, hash string
	err := s.db.QueryRow(r.Context(), `
		SELECT id::text, password_hash
		FROM users
		WHERE email = $1
	`, req.Email).Scan(&userID, &hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not query user")
		return
	}

	if !auth.VerifyPassword(hash, req.Password) {
		writeError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := auth.CreateToken(s.cfg.JWTSecret, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"token": token, "user_id": userID})
}

func (s *Server) handleGetPublicProfile(w http.ResponseWriter, r *http.Request) {
	slug := normalizePublicSlug(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, http.StatusNotFound, "public profile not found")
		return
	}

	profile, _, err := s.getPublicProfileBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "public profile not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load public profile")
		return
	}

	latestPosts, nextCursor, err := s.listPublishedPostsForPersona(r.Context(), profile.PersonaID, "", 10)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load public posts")
		return
	}

	topRooms, err := s.listTopRoomsForPersona(r.Context(), profile.PersonaID, 3)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load top rooms")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"profile":      profile,
		"latest_posts": latestPosts,
		"top_rooms":    topRooms,
		"next_cursor":  nextCursor,
	})
}

func (s *Server) handleGetPublicProfilePosts(w http.ResponseWriter, r *http.Request) {
	slug := normalizePublicSlug(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, http.StatusNotFound, "public profile not found")
		return
	}

	profile, _, err := s.getPublicProfileBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "public profile not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load public profile")
		return
	}

	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	posts, nextCursor, err := s.listPublishedPostsForPersona(r.Context(), profile.PersonaID, cursor, 10)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"posts":       posts,
		"next_cursor": nextCursor,
	})
}

func (s *Server) handleFollowPublicProfile(w http.ResponseWriter, r *http.Request) {
	slug := normalizePublicSlug(chi.URLParam(r, "slug"))
	if slug == "" {
		writeError(w, http.StatusNotFound, "public profile not found")
		return
	}

	profile, ownerUserID, err := s.getPublicProfileBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "public profile not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load public profile")
		return
	}

	followerUserID, ok := s.optionalUserIDFromRequest(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error": "signup_required",
		})
		return
	}

	if followerUserID == ownerUserID {
		writeError(w, http.StatusConflict, "cannot follow your own persona")
		return
	}

	ct, err := s.db.Exec(r.Context(), `
		INSERT INTO persona_follows(follower_user_id, followed_persona_id)
		VALUES ($1, $2)
		ON CONFLICT (follower_user_id, followed_persona_id) DO NOTHING
	`, followerUserID, profile.PersonaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not follow persona")
		return
	}

	var followers int
	if err := s.db.QueryRow(r.Context(), `
		SELECT COUNT(*)::int
		FROM persona_follows
		WHERE followed_persona_id = $1
	`, profile.PersonaID).Scan(&followers); err != nil {
		writeError(w, http.StatusInternalServerError, "could not load followers")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"followed":  ct.RowsAffected() > 0,
		"followers": followers,
	})
}

func (s *Server) handlePublishPersonaProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}

	personaID := chi.URLParam(r, "id")
	var req struct {
		Slug string `json:"slug"`
		Bio  string `json:"bio"`
	}
	if err := decodeJSONAllowEmpty(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var persona struct {
		Name string
		Bio  string
	}
	err := s.db.QueryRow(r.Context(), `
		SELECT name, bio
		FROM personas
		WHERE id = $1 AND user_id = $2
	`, personaID, userID).Scan(&persona.Name, &persona.Bio)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "persona not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load persona")
		return
	}

	requestedSlug := strings.TrimSpace(req.Slug)
	normalizedRequestedSlug := normalizePublicSlug(requestedSlug)
	if requestedSlug != "" && normalizedRequestedSlug == "" {
		writeError(w, http.StatusBadRequest, "slug must contain only letters, numbers, spaces, hyphen or underscore")
		return
	}

	publicBio := strings.TrimSpace(req.Bio)
	if publicBio == "" {
		publicBio = strings.TrimSpace(persona.Bio)
	}

	var currentSlug string
	err = s.db.QueryRow(r.Context(), `
		SELECT slug
		FROM persona_public_profiles
		WHERE persona_id = $1
	`, personaID).Scan(&currentSlug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not inspect profile")
		return
	}

	baseSlug := normalizePublicSlug(persona.Name)
	if baseSlug == "" {
		baseSlug = "persona"
	}
	if normalizedRequestedSlug != "" {
		baseSlug = normalizedRequestedSlug
	} else if strings.TrimSpace(currentSlug) != "" {
		baseSlug = strings.TrimSpace(currentSlug)
	}

	finalSlug, err := s.ensureUniquePublicProfileSlug(r.Context(), baseSlug, personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create unique slug")
		return
	}

	var out struct {
		Slug      string
		IsPublic  bool
		Bio       string
		CreatedAt time.Time
	}
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO persona_public_profiles(persona_id, slug, is_public, bio)
		VALUES ($1, $2, TRUE, $3)
		ON CONFLICT (persona_id)
		DO UPDATE SET
			slug = EXCLUDED.slug,
			is_public = TRUE,
			bio = EXCLUDED.bio
		RETURNING slug, is_public, bio, created_at
	`, personaID, finalSlug, publicBio).Scan(&out.Slug, &out.IsPublic, &out.Bio, &out.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			writeError(w, http.StatusConflict, "slug already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not publish profile")
		return
	}

	shareURL := fmt.Sprintf("%s/p/%s", strings.TrimRight(s.cfg.FrontendOrigin, "/"), out.Slug)
	writeJSON(w, http.StatusOK, map[string]any{
		"persona_id": personaID,
		"slug":       out.Slug,
		"is_public":  out.IsPublic,
		"bio":        out.Bio,
		"created_at": out.CreatedAt,
		"share_url":  shareURL,
	})
}

func (s *Server) handleUnpublishPersonaProfile(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	var exists bool
	if err := s.db.QueryRow(r.Context(), `
		SELECT EXISTS(
			SELECT 1
			FROM personas
			WHERE id = $1 AND user_id = $2
		)
	`, personaID, userID).Scan(&exists); err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate persona")
		return
	}
	if !exists {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	var slug string
	err := s.db.QueryRow(r.Context(), `
		SELECT slug
		FROM persona_public_profiles
		WHERE persona_id = $1
	`, personaID).Scan(&slug)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusInternalServerError, "could not load profile")
		return
	}

	if !errors.Is(err, pgx.ErrNoRows) {
		if _, err := s.db.Exec(r.Context(), `
			UPDATE persona_public_profiles
			SET is_public = FALSE
			WHERE persona_id = $1
		`, personaID); err != nil {
			writeError(w, http.StatusInternalServerError, "could not unpublish profile")
			return
		}
	}

	shareURL := ""
	if strings.TrimSpace(slug) != "" {
		shareURL = fmt.Sprintf("%s/p/%s", strings.TrimRight(s.cfg.FrontendOrigin, "/"), slug)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"persona_id": personaID,
		"slug":       strings.TrimSpace(slug),
		"is_public":  false,
		"share_url":  shareURL,
	})
}

func (s *Server) handleListPersonas(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}

	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, name, bio, tone, writing_samples, do_not_say, catchphrases, preferred_language, formality, daily_draft_quota, daily_reply_quota, created_at, updated_at
		FROM personas
		WHERE user_id = $1
		ORDER BY created_at DESC
	`, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list personas")
		return
	}
	defer rows.Close()

	personas := make([]Persona, 0)
	for rows.Next() {
		var p Persona
		if err := scanPersona(rows, &p); err != nil {
			writeError(w, http.StatusInternalServerError, "could not scan persona")
			return
		}
		personas = append(personas, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"personas": personas})
}

func (s *Server) handleCreatePersona(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}

	var req struct {
		Name              string   `json:"name"`
		Bio               string   `json:"bio"`
		Tone              string   `json:"tone"`
		WritingSamples    []string `json:"writing_samples"`
		DoNotSay          []string `json:"do_not_say"`
		Catchphrases      []string `json:"catchphrases"`
		PreferredLanguage string   `json:"preferred_language"`
		Formality         int      `json:"formality"`
		DailyDraftQuota   int      `json:"daily_draft_quota"`
		DailyReplyQuota   int      `json:"daily_reply_quota"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	input, err := normalizePersonaInput(req.Name, req.Bio, req.Tone, req.WritingSamples, req.DoNotSay, req.Catchphrases, req.PreferredLanguage, req.Formality)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.DailyDraftQuota <= 0 {
		req.DailyDraftQuota = s.cfg.DefaultDraftQuota
	}
	if req.DailyReplyQuota <= 0 {
		req.DailyReplyQuota = s.cfg.DefaultReplyQuota
	}

	var p Persona
	writingSamplesJSON, _ := json.Marshal(input.WritingSamples)
	doNotSayJSON, _ := json.Marshal(input.DoNotSay)
	catchphrasesJSON, _ := json.Marshal(input.Catchphrases)

	err = scanPersona(s.db.QueryRow(r.Context(), `
		INSERT INTO personas(user_id, name, bio, tone, writing_samples, do_not_say, catchphrases, preferred_language, formality, daily_draft_quota, daily_reply_quota)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6::jsonb, $7::jsonb, $8, $9, $10, $11)
		RETURNING id::text, name, bio, tone, writing_samples, do_not_say, catchphrases, preferred_language, formality, daily_draft_quota, daily_reply_quota, created_at, updated_at
	`, userID, input.Name, input.Bio, input.Tone, writingSamplesJSON, doNotSayJSON, catchphrasesJSON, input.PreferredLanguage, input.Formality, req.DailyDraftQuota, req.DailyReplyQuota), &p)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create persona")
		return
	}

	writeJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPersona(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	p, err := s.getPersonaByID(r.Context(), userID, personaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "persona not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not get persona")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePersona(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	var req struct {
		Name              string   `json:"name"`
		Bio               string   `json:"bio"`
		Tone              string   `json:"tone"`
		WritingSamples    []string `json:"writing_samples"`
		DoNotSay          []string `json:"do_not_say"`
		Catchphrases      []string `json:"catchphrases"`
		PreferredLanguage string   `json:"preferred_language"`
		Formality         int      `json:"formality"`
		DailyDraftQuota   int      `json:"daily_draft_quota"`
		DailyReplyQuota   int      `json:"daily_reply_quota"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	input, err := normalizePersonaInput(req.Name, req.Bio, req.Tone, req.WritingSamples, req.DoNotSay, req.Catchphrases, req.PreferredLanguage, req.Formality)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if input.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if req.DailyDraftQuota <= 0 || req.DailyReplyQuota <= 0 {
		writeError(w, http.StatusBadRequest, "quotas must be positive")
		return
	}

	var p Persona
	writingSamplesJSON, _ := json.Marshal(input.WritingSamples)
	doNotSayJSON, _ := json.Marshal(input.DoNotSay)
	catchphrasesJSON, _ := json.Marshal(input.Catchphrases)

	err = scanPersona(s.db.QueryRow(r.Context(), `
		UPDATE personas
		SET name=$1, bio=$2, tone=$3, writing_samples=$4::jsonb, do_not_say=$5::jsonb, catchphrases=$6::jsonb, preferred_language=$7, formality=$8, daily_draft_quota=$9, daily_reply_quota=$10, updated_at=NOW()
		WHERE id=$11 AND user_id=$12
		RETURNING id::text, name, bio, tone, writing_samples, do_not_say, catchphrases, preferred_language, formality, daily_draft_quota, daily_reply_quota, created_at, updated_at
	`, input.Name, input.Bio, input.Tone, writingSamplesJSON, doNotSayJSON, catchphrasesJSON, input.PreferredLanguage, input.Formality, req.DailyDraftQuota, req.DailyReplyQuota, personaID, userID), &p)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "persona not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not update persona")
		return
	}

	writeJSON(w, http.StatusOK, p)
}

func (s *Server) handleDeletePersona(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	ct, err := s.db.Exec(r.Context(), "DELETE FROM personas WHERE id=$1 AND user_id=$2", personaID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete persona")
		return
	}
	if ct.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func (s *Server) handlePreviewPersona(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}

	personaID := chi.URLParam(r, "id")
	roomID := strings.TrimSpace(r.URL.Query().Get("room_id"))
	if roomID == "" {
		writeError(w, http.StatusBadRequest, "room_id query param is required")
		return
	}

	persona, err := s.getPersonaByID(r.Context(), userID, personaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "persona not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load persona")
		return
	}

	room, err := s.getRoomByID(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load room")
		return
	}

	used, err := s.currentQuotaUsage(r.Context(), personaID, "preview")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check preview quota")
		return
	}
	if used >= s.cfg.DefaultPreviewQuota {
		writeError(w, http.StatusTooManyRequests, "daily preview quota reached")
		return
	}

	drafts := make([]PreviewDraft, 0, 2)
	for variant := 1; variant <= 2; variant++ {
		draft, err := s.llm.GeneratePostDraft(r.Context(), personaToAIContext(persona), ai.RoomContext{
			ID:          room.ID,
			Name:        room.Name,
			Description: room.Description,
			Variant:     variant,
		})
		if err != nil {
			writeError(w, http.StatusBadGateway, fmt.Sprintf("llm preview failed: %v", err))
			return
		}
		if err := safety.ValidateContent(draft, s.cfg.DraftMaxLen); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		drafts = append(drafts, PreviewDraft{
			Label:      fmt.Sprintf("AI Preview %d", variant),
			Content:    draft,
			AuthoredBy: "AI",
		})
	}

	if _, err := s.db.Exec(r.Context(), `
		INSERT INTO quota_events(persona_id, quota_type)
		VALUES ($1, 'preview')
	`, personaID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record preview quota")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"drafts": drafts,
		"quota": map[string]any{
			"used":  used + 1,
			"limit": s.cfg.DefaultPreviewQuota,
		},
	})
}

func (s *Server) handleGetTodayDigest(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	owned, err := s.personaOwnedByUser(r.Context(), userID, personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check persona")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	digest, exists, err := s.getDigestForDate(r.Context(), personaID, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load digest")
		return
	}
	if !exists {
		digest = emptyDigest(personaID, time.Now().UTC())
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"digest": digest,
		"exists": exists,
	})
}

func (s *Server) handleGetLatestDigest(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	personaID := chi.URLParam(r, "id")

	owned, err := s.personaOwnedByUser(r.Context(), userID, personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check persona")
		return
	}
	if !owned {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	digest, exists, err := s.getLatestDigest(r.Context(), personaID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load digest")
		return
	}
	if !exists {
		digest = emptyDigest(personaID, time.Now().UTC())
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"digest": digest,
		"exists": exists,
	})
}

func (s *Server) handleListRooms(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(r.Context(), `
		SELECT id::text, slug, name, description, created_at
		FROM rooms
		ORDER BY name ASC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list rooms")
		return
	}
	defer rows.Close()

	rooms := make([]Room, 0)
	for rows.Next() {
		var rm Room
		if err := rows.Scan(&rm.ID, &rm.Slug, &rm.Name, &rm.Description, &rm.CreatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "could not scan room")
			return
		}
		rooms = append(rooms, rm)
	}

	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

func (s *Server) handleListRoomPosts(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	roomID := chi.URLParam(r, "id")

	rows, err := s.db.Query(r.Context(), `
		SELECT p.id::text, p.room_id::text, COALESCE(p.persona_id::text, ''), COALESCE(pr.name, ''), p.authored_by::text, p.status::text, p.content, p.created_at, p.updated_at
		FROM posts p
		LEFT JOIN personas pr ON pr.id = p.persona_id
		WHERE p.room_id = $1
		  AND (p.status = 'PUBLISHED' OR p.user_id = $2)
		ORDER BY p.created_at DESC
		LIMIT 100
	`, roomID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list posts")
		return
	}
	defer rows.Close()

	posts := make([]Post, 0)
	for rows.Next() {
		var p Post
		if err := rows.Scan(&p.ID, &p.RoomID, &p.PersonaID, &p.Persona, &p.AuthoredBy, &p.Status, &p.Content, &p.CreatedAt, &p.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "could not scan post")
			return
		}
		posts = append(posts, p)
	}

	writeJSON(w, http.StatusOK, map[string]any{"posts": posts})
}

func (s *Server) handleCreateBattle(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	roomID := chi.URLParam(r, "id")

	var req struct {
		Topic      string `json:"topic"`
		PersonaAID string `json:"persona_a_id"`
		PersonaBID string `json:"persona_b_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	req.Topic = strings.TrimSpace(req.Topic)
	req.PersonaAID = strings.TrimSpace(req.PersonaAID)
	req.PersonaBID = strings.TrimSpace(req.PersonaBID)

	if req.Topic == "" {
		writeError(w, http.StatusBadRequest, "topic is required")
		return
	}
	if req.PersonaAID == "" || req.PersonaBID == "" {
		writeError(w, http.StatusBadRequest, "persona_a_id and persona_b_id are required")
		return
	}
	if req.PersonaAID == req.PersonaBID {
		writeError(w, http.StatusBadRequest, "choose two different personas")
		return
	}
	if err := safety.ValidateContent(req.Topic, 240); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	room, err := s.getRoomByID(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load room")
		return
	}

	personaAOwned, err := s.personaOwnedByUser(r.Context(), userID, req.PersonaAID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate persona")
		return
	}
	personaBOwned, err := s.personaOwnedByUser(r.Context(), userID, req.PersonaBID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not validate persona")
		return
	}
	if !personaAOwned || !personaBOwned {
		writeError(w, http.StatusNotFound, "persona not found")
		return
	}

	var createdToday int
	if err := s.db.QueryRow(r.Context(), `
		SELECT COUNT(*)::int
		FROM battle_creation_events
		WHERE user_id = $1
		  AND created_at >= date_trunc('day', NOW())
	`, userID).Scan(&createdToday); err != nil {
		writeError(w, http.StatusInternalServerError, "could not check battle rate limit")
		return
	}
	if createdToday >= dailyBattleCreateLimit {
		writeError(w, http.StatusTooManyRequests, "daily battle limit reached")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	var battleID string
	err = tx.QueryRow(r.Context(), `
		INSERT INTO battles(room_id, topic, persona_a_id, persona_b_id, status, verdict, error)
		VALUES ($1, $2, $3, $4, 'PENDING', '{}'::jsonb, '')
		RETURNING id::text
	`, room.ID, req.Topic, req.PersonaAID, req.PersonaBID).Scan(&battleID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create battle")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		INSERT INTO battle_creation_events(user_id, battle_id)
		VALUES ($1, $2)
	`, userID, battleID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not enqueue battle")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit battle")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"battle_id":   battleID,
		"status":      "PENDING",
		"share_url":   fmt.Sprintf("%s/b/%s", strings.TrimRight(s.cfg.FrontendOrigin, "/"), battleID),
		"room_id":     room.ID,
		"room_name":   room.Name,
		"topic":       req.Topic,
		"queued":      true,
		"daily_limit": dailyBattleCreateLimit,
	})
}

func (s *Server) handleGetBattle(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeError(w, http.StatusNotFound, "battle not found")
		return
	}

	battle, err := s.getBattleByID(r.Context(), battleID, userID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "battle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load battle")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"battle": battle})
}

func (s *Server) handleRegenerateBattle(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeError(w, http.StatusNotFound, "battle not found")
		return
	}

	_, err := s.getBattleByID(r.Context(), battleID, userID, true)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "battle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load battle")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `
		UPDATE battles
		SET status='PENDING', verdict='{}'::jsonb, error='', updated_at=NOW()
		WHERE id = $1
	`, battleID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not requeue battle")
		return
	}

	if _, err := tx.Exec(r.Context(), `
		DELETE FROM battle_turns
		WHERE battle_id = $1
	`, battleID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not clear battle turns")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit battle regeneration")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"battle_id": battleID,
		"status":    "PENDING",
		"requeued":  true,
	})
}

func (s *Server) handleBattleRemix(w http.ResponseWriter, r *http.Request) {
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeError(w, http.StatusNotFound, "battle not found")
		return
	}

	payload, err := s.getBattleRemixPayload(r.Context(), battleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "battle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load remix payload")
		return
	}

	if _, ok := s.optionalUserIDFromRequest(r); !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{
			"error":         "signup_required",
			"remix_payload": payload,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"remix_payload": payload,
	})
}

func (s *Server) handleGetPublicBattle(w http.ResponseWriter, r *http.Request) {
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeError(w, http.StatusNotFound, "battle not found")
		return
	}

	battle, err := s.getBattleByID(r.Context(), battleID, "", false)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "battle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load battle")
		return
	}

	battle.Error = ""
	writeJSON(w, http.StatusOK, map[string]any{"battle": battle})
}

func (s *Server) handleGetPublicBattleSummary(w http.ResponseWriter, r *http.Request) {
	battleID := strings.TrimSpace(chi.URLParam(r, "id"))
	if battleID == "" {
		writeError(w, http.StatusNotFound, "battle not found")
		return
	}

	summary, err := s.getPublicBattleSummary(r.Context(), battleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "battle not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load battle summary")
		return
	}

	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleCreateDraft(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	roomID := chi.URLParam(r, "id")

	var req struct {
		PersonaID string `json:"persona_id"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.PersonaID == "" {
		writeError(w, http.StatusBadRequest, "persona_id is required")
		return
	}

	persona, err := s.getPersonaByID(r.Context(), userID, req.PersonaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "persona not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load persona")
		return
	}

	room, err := s.getRoomByID(r.Context(), roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "room not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load room")
		return
	}

	used, err := s.currentQuotaUsage(r.Context(), req.PersonaID, "draft")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not check quota")
		return
	}
	if used >= persona.DailyDraftQuota {
		writeError(w, http.StatusTooManyRequests, "daily draft quota reached")
		return
	}

	draft, err := s.llm.GeneratePostDraft(r.Context(), personaToAIContext(persona), ai.RoomContext{
		ID:          room.ID,
		Name:        room.Name,
		Description: room.Description,
		Variant:     1,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("llm draft failed: %v", err))
		return
	}

	if err := safety.ValidateContent(draft, s.cfg.DraftMaxLen); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	var post Post
	err = s.db.QueryRow(r.Context(), `
		INSERT INTO posts(room_id, persona_id, user_id, authored_by, status, content)
		VALUES ($1, $2, $3, 'AI', 'DRAFT', $4)
		RETURNING id::text, room_id::text, COALESCE(persona_id::text, ''), authored_by::text, status::text, content, created_at, updated_at
	`, roomID, req.PersonaID, userID, draft).
		Scan(&post.ID, &post.RoomID, &post.PersonaID, &post.AuthoredBy, &post.Status, &post.Content, &post.CreatedAt, &post.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not create draft")
		return
	}
	post.Persona = persona.Name

	if _, err := s.db.Exec(r.Context(), `
		INSERT INTO quota_events(persona_id, quota_type)
		VALUES ($1, 'draft')
	`, req.PersonaID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not record quota")
		return
	}

	writeJSON(w, http.StatusCreated, post)
}

func (s *Server) handleApprovePost(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	postID := chi.URLParam(r, "id")

	var current Post
	var ownerUserID string
	err := s.db.QueryRow(r.Context(), `
		SELECT id::text, room_id::text, COALESCE(persona_id::text, ''), authored_by::text, status::text, content, created_at, updated_at, user_id::text
		FROM posts
		WHERE id = $1
	`, postID).Scan(&current.ID, &current.RoomID, &current.PersonaID, &current.AuthoredBy, &current.Status, &current.Content, &current.CreatedAt, &current.UpdatedAt, &ownerUserID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load post")
		return
	}

	if ownerUserID != userID {
		writeError(w, http.StatusForbidden, "not allowed")
		return
	}
	if current.Status != "DRAFT" {
		writeError(w, http.StatusConflict, "only drafts can be approved")
		return
	}

	var req struct {
		Content string `json:"content"`
	}
	if err := decodeJSONAllowEmpty(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	content := current.Content
	if strings.TrimSpace(req.Content) != "" {
		content = req.Content
	}

	if err := safety.ValidateContent(content, s.cfg.DraftMaxLen); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not start transaction")
		return
	}
	defer tx.Rollback(r.Context())

	var out Post
	err = tx.QueryRow(r.Context(), `
		UPDATE posts
		SET content=$1, status='PUBLISHED', authored_by='AI_DRAFT_APPROVED', published_at=NOW(), updated_at=NOW()
		WHERE id=$2
		RETURNING id::text, room_id::text, COALESCE(persona_id::text, ''), authored_by::text, status::text, content, created_at, updated_at
	`, content, postID).
		Scan(&out.ID, &out.RoomID, &out.PersonaID, &out.AuthoredBy, &out.Status, &out.Content, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not approve post")
		return
	}

	if strings.TrimSpace(out.PersonaID) != "" {
		metadata := map[string]any{
			"post_id":      out.ID,
			"room_id":      out.RoomID,
			"post_preview": truncateText(out.Content, 220),
		}
		if err := insertPersonaActivityEvent(r.Context(), tx, out.PersonaID, "post_created", metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "could not record activity")
			return
		}
		if err := insertPersonaActivityEvent(r.Context(), tx, out.PersonaID, "thread_participated", metadata); err != nil {
			writeError(w, http.StatusInternalServerError, "could not record activity")
			return
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "could not commit post approval")
		return
	}

	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGenerateReplies(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	postID := chi.URLParam(r, "id")

	var postStatus string
	err := s.db.QueryRow(r.Context(), `
		SELECT status::text
		FROM posts
		WHERE id=$1
	`, postID).Scan(&postStatus)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load post")
		return
	}
	if postStatus != "PUBLISHED" {
		writeError(w, http.StatusConflict, "replies can be generated only for published posts")
		return
	}

	var req struct {
		PersonaIDs []string `json:"persona_ids"`
	}
	if err := decodeJSONAllowEmpty(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	personaIDs, err := s.resolvePersonaIDsForReplyGeneration(r.Context(), userID, req.PersonaIDs)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	enqueued := 0
	skipped := 0

	for _, personaID := range personaIDs {
		persona, err := s.getPersonaByID(r.Context(), userID, personaID)
		if err != nil {
			skipped++
			continue
		}

		used, err := s.currentQuotaUsage(r.Context(), personaID, "reply")
		if err != nil {
			skipped++
			continue
		}
		if used >= persona.DailyReplyQuota {
			skipped++
			continue
		}

		var alreadyReplied bool
		err = s.db.QueryRow(r.Context(), `
			SELECT EXISTS(
				SELECT 1 FROM replies WHERE post_id=$1 AND persona_id=$2
			)
		`, postID, personaID).Scan(&alreadyReplied)
		if err != nil || alreadyReplied {
			skipped++
			continue
		}

		var pending bool
		err = s.db.QueryRow(r.Context(), `
			SELECT EXISTS(
				SELECT 1 FROM jobs
				WHERE post_id=$1 AND persona_id=$2 AND job_type='generate_reply' AND status IN ('PENDING', 'PROCESSING')
			)
		`, postID, personaID).Scan(&pending)
		if err != nil || pending {
			skipped++
			continue
		}

		payload := fmt.Sprintf(`{"post_id":"%s","persona_id":"%s"}`, postID, personaID)
		if _, err := s.db.Exec(r.Context(), `
			INSERT INTO jobs(job_type, post_id, persona_id, payload, status, available_at)
			VALUES ('generate_reply', $1, $2, $3::jsonb, 'PENDING', NOW())
		`, postID, personaID, payload); err != nil {
			skipped++
			continue
		}
		enqueued++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"enqueued": enqueued,
		"skipped":  skipped,
	})
}

func (s *Server) handleGetThread(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing user")
		return
	}
	postID := chi.URLParam(r, "id")

	var post Post
	var postOwner string
	err := s.db.QueryRow(r.Context(), `
		SELECT p.id::text, p.room_id::text, COALESCE(p.persona_id::text, ''), COALESCE(pr.name, ''), p.authored_by::text, p.status::text, p.content, p.created_at, p.updated_at, p.user_id::text
		FROM posts p
		LEFT JOIN personas pr ON pr.id = p.persona_id
		WHERE p.id = $1
	`, postID).Scan(&post.ID, &post.RoomID, &post.PersonaID, &post.Persona, &post.AuthoredBy, &post.Status, &post.Content, &post.CreatedAt, &post.UpdatedAt, &postOwner)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusNotFound, "post not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not load post")
		return
	}

	if post.Status != "PUBLISHED" && postOwner != userID {
		writeError(w, http.StatusForbidden, "not allowed")
		return
	}

	rows, err := s.db.Query(r.Context(), `
		SELECT r.id::text, r.post_id::text, COALESCE(r.persona_id::text, ''), COALESCE(p.name, ''), r.authored_by::text, r.content, r.created_at, r.updated_at
		FROM replies r
		LEFT JOIN personas p ON p.id = r.persona_id
		WHERE r.post_id = $1
		ORDER BY r.created_at ASC
	`, postID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load replies")
		return
	}
	defer rows.Close()

	replies := make([]Reply, 0)
	thread := make([]ai.ReplyContext, 0)
	for rows.Next() {
		var reply Reply
		if err := rows.Scan(&reply.ID, &reply.PostID, &reply.PersonaID, &reply.Persona, &reply.AuthoredBy, &reply.Content, &reply.CreatedAt, &reply.UpdatedAt); err != nil {
			writeError(w, http.StatusInternalServerError, "could not scan reply")
			return
		}
		replies = append(replies, reply)
		thread = append(thread, ai.ReplyContext{ID: reply.ID, Content: reply.Content})
	}

	summary, err := s.llm.SummarizeThread(r.Context(), ai.PostContext{ID: post.ID, Content: post.Content}, thread)
	if err != nil {
		summary = "Thread summary unavailable right now."
	}
	if len([]rune(summary)) > s.cfg.SummaryMaxLen {
		runes := []rune(summary)
		summary = string(runes[:s.cfg.SummaryMaxLen])
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"post":       post,
		"replies":    replies,
		"ai_summary": summary,
	})
}

func (s *Server) getBattleByID(ctx context.Context, battleID, userID string, requireOwnership bool) (Battle, error) {
	var (
		battle     Battle
		verdictRaw []byte
	)

	baseQuery := `
		SELECT
			b.id::text,
			b.room_id::text,
			COALESCE(r.name, ''),
			b.topic,
			b.status::text,
			pa.id::text,
			pa.name,
			pa.bio,
			pa.tone,
			pa.preferred_language,
			pa.formality,
			pb.id::text,
			pb.name,
			pb.bio,
			pb.tone,
			pb.preferred_language,
			pb.formality,
			b.verdict,
			COALESCE(b.error, ''),
			b.created_at,
			b.updated_at
		FROM battles b
		JOIN rooms r ON r.id = b.room_id
		JOIN personas pa ON pa.id = b.persona_a_id
		JOIN personas pb ON pb.id = b.persona_b_id
		WHERE b.id = $1
	`

	query := baseQuery
	args := []any{battleID}
	if requireOwnership {
		query += " AND pa.user_id = $2 AND pb.user_id = $2"
		args = append(args, userID)
	}

	err := s.db.QueryRow(ctx, query, args...).Scan(
		&battle.ID,
		&battle.RoomID,
		&battle.RoomName,
		&battle.Topic,
		&battle.Status,
		&battle.PersonaA.ID,
		&battle.PersonaA.Name,
		&battle.PersonaA.Bio,
		&battle.PersonaA.Tone,
		&battle.PersonaA.PreferredLanguage,
		&battle.PersonaA.Formality,
		&battle.PersonaB.ID,
		&battle.PersonaB.Name,
		&battle.PersonaB.Bio,
		&battle.PersonaB.Tone,
		&battle.PersonaB.PreferredLanguage,
		&battle.PersonaB.Formality,
		&verdictRaw,
		&battle.Error,
		&battle.CreatedAt,
		&battle.UpdatedAt,
	)
	if err != nil {
		return Battle{}, err
	}

	if err := hydrateBattleVerdict(&battle, verdictRaw); err != nil {
		return Battle{}, err
	}

	turns, err := s.listBattleTurns(ctx, battle.ID)
	if err != nil {
		return Battle{}, err
	}
	battle.Turns = turns
	return battle, nil
}

func (s *Server) listBattleTurns(ctx context.Context, battleID string) ([]BattleTurn, error) {
	rows, err := s.db.Query(ctx, `
		SELECT
			bt.battle_id::text,
			bt.turn_index,
			bt.persona_id::text,
			COALESCE(p.name, ''),
			bt.content,
			bt.metadata,
			bt.created_at
		FROM battle_turns bt
		LEFT JOIN personas p ON p.id = bt.persona_id
		WHERE bt.battle_id = $1
		ORDER BY bt.turn_index ASC
	`, battleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	turns := make([]BattleTurn, 0)
	for rows.Next() {
		var turn BattleTurn
		var metadataRaw []byte
		if err := rows.Scan(&turn.BattleID, &turn.TurnIndex, &turn.PersonaID, &turn.Persona, &turn.Content, &metadataRaw, &turn.CreatedAt); err != nil {
			return nil, err
		}

		turn.Metadata = BattleTurnMetadata{
			QualityLabel: "LOW",
			Reasons:      []string{},
		}
		if len(metadataRaw) > 0 {
			if err := json.Unmarshal(metadataRaw, &turn.Metadata); err != nil {
				return nil, err
			}
		}
		turn.Metadata.QualityLabel = strings.ToUpper(strings.TrimSpace(turn.Metadata.QualityLabel))
		if turn.Metadata.QualityLabel == "" {
			turn.Metadata.QualityLabel = "LOW"
		}
		if turn.Metadata.Reasons == nil {
			turn.Metadata.Reasons = []string{}
		}
		turns = append(turns, turn)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return turns, nil
}

func hydrateBattleVerdict(battle *Battle, verdictRaw []byte) error {
	battle.Verdict = BattleVerdict{
		Verdict:   "",
		Takeaways: []string{},
	}
	if len(verdictRaw) == 0 {
		return nil
	}
	if err := json.Unmarshal(verdictRaw, &battle.Verdict); err != nil {
		return err
	}
	battle.Verdict.Verdict = strings.TrimSpace(battle.Verdict.Verdict)
	if battle.Verdict.Takeaways == nil {
		battle.Verdict.Takeaways = []string{}
	}
	cleanTakeaways := make([]string, 0, len(battle.Verdict.Takeaways))
	for _, takeaway := range battle.Verdict.Takeaways {
		trimmed := strings.TrimSpace(takeaway)
		if trimmed != "" {
			cleanTakeaways = append(cleanTakeaways, trimmed)
		}
	}
	battle.Verdict.Takeaways = cleanTakeaways
	return nil
}

func (s *Server) getPublicBattleSummary(ctx context.Context, battleID string) (PublicBattleSummary, error) {
	var (
		summary    PublicBattleSummary
		verdictRaw []byte
	)
	err := s.db.QueryRow(ctx, `
		SELECT
			b.topic,
			COALESCE(r.name, ''),
			b.verdict
		FROM battles b
		JOIN rooms r ON r.id = b.room_id
		WHERE b.id = $1
	`, battleID).Scan(&summary.Topic, &summary.RoomName, &verdictRaw)
	if err != nil {
		return PublicBattleSummary{}, err
	}

	summary.Topic = strings.TrimSpace(summary.Topic)
	summary.RoomName = strings.TrimSpace(summary.RoomName)
	summary.TopTakeaways = []string{}
	summary.VerdictText = ""

	if len(verdictRaw) > 0 {
		var verdict BattleVerdict
		if err := json.Unmarshal(verdictRaw, &verdict); err != nil {
			return PublicBattleSummary{}, err
		}
		summary.VerdictText = strings.TrimSpace(verdict.Verdict)
		for _, takeaway := range verdict.Takeaways {
			trimmed := strings.TrimSpace(takeaway)
			if trimmed == "" {
				continue
			}
			summary.TopTakeaways = append(summary.TopTakeaways, trimmed)
			if len(summary.TopTakeaways) >= 3 {
				break
			}
		}
	}

	if summary.VerdictText == "" {
		summary.VerdictText = "A structured persona debate is in progress."
	}
	if summary.TopTakeaways == nil {
		summary.TopTakeaways = []string{}
	}
	return summary, nil
}

func (s *Server) getBattleRemixPayload(ctx context.Context, battleID string) (BattleRemixPayload, error) {
	var payload BattleRemixPayload
	err := s.db.QueryRow(ctx, `
		SELECT room_id::text, topic
		FROM battles
		WHERE id = $1
	`, battleID).Scan(&payload.RoomID, &payload.Topic)
	if err != nil {
		return BattleRemixPayload{}, err
	}
	payload.RoomID = strings.TrimSpace(payload.RoomID)
	payload.Topic = strings.TrimSpace(payload.Topic)
	return payload, nil
}

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

func insertPersonaActivityEvent(ctx context.Context, executor dbExecutor, personaID, eventType string, metadata map[string]any) error {
	if metadata == nil {
		metadata = map[string]any{}
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return err
	}

	_, err = executor.Exec(ctx, `
		INSERT INTO persona_activity_events(persona_id, type, metadata)
		VALUES ($1, $2, $3::jsonb)
	`, personaID, eventType, raw)
	return err
}

func truncateText(value string, maxRunes int) string {
	trimmed := strings.TrimSpace(value)
	if maxRunes <= 0 {
		return trimmed
	}

	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return strings.TrimSpace(string(runes[:maxRunes]))
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

func decodeJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func decodeJSONAllowEmpty(r *http.Request, dst any) error {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	err := decoder.Decode(dst)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}
