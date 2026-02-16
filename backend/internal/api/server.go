package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	cfg config.Config
	db  *pgxpool.Pool
	llm ai.LLMClient
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

func New(cfg config.Config, db *pgxpool.Pool, llm ai.LLMClient) *Server {
	return &Server{cfg: cfg, db: db, llm: llm}
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

	r.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.cfg.JWTSecret))

		r.Get("/personas", s.handleListPersonas)
		r.Post("/personas", s.handleCreatePersona)
		r.Get("/personas/{id}", s.handleGetPersona)
		r.Put("/personas/{id}", s.handleUpdatePersona)
		r.Delete("/personas/{id}", s.handleDeletePersona)
		r.Post("/personas/{id}/preview", s.handlePreviewPersona)

		r.Get("/rooms", s.handleListRooms)
		r.Get("/rooms/{id}/posts", s.handleListRoomPosts)
		r.Post("/rooms/{id}/posts/draft", s.handleCreateDraft)
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

	var out Post
	err = s.db.QueryRow(r.Context(), `
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
