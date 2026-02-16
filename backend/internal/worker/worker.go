package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/safety"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	cfg config.Config
	db  *pgxpool.Pool
	llm ai.LLMClient
}

type permanentError struct {
	message string
}

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

type generatedBattleTurn struct {
	TurnIndex    int
	PersonaID    string
	PersonaName  string
	Side         string
	Claim        string
	Evidence     string
	Content      string
	Metadata     map[string]any
	QualityScore int
}

type turnQualityResult struct {
	Score   int
	Reasons []string
}

type storedBattleVerdict struct {
	Verdict   string   `json:"verdict"`
	Takeaways []string `json:"takeaways"`
}

type dbExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

const (
	battleTurnCount     = 6
	battleTurnWordLimit = 120
	battleVerdictWords  = 80
	battleMinQuality    = 60
)

var (
	specificEvidencePattern = regexp.MustCompile(`(?i)\b(for example|for instance|e\.g\.|case study|experiment|cohort|sample|pilot|baseline|before|after)\b`)
	numberPattern           = regexp.MustCompile(`\d`)
)

func (e permanentError) Error() string {
	return e.message
}

func New(cfg config.Config, db *pgxpool.Pool, llm ai.LLMClient) *Worker {
	return &Worker{cfg: cfg, db: db, llm: llm}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.WorkerPollEvery)
	defer ticker.Stop()

	for {
		if err := w.generateDigestForOnePersona(ctx); err != nil {
			log.Printf("worker digest process error: %v", err)
		}

		if err := w.generateBattleForOnePending(ctx); err != nil {
			log.Printf("worker battle process error: %v", err)
		}

		if err := w.processOne(ctx); err != nil {
			log.Printf("worker process error: %v", err)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (w *Worker) processOne(ctx context.Context) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		jobID     int64
		jobType   string
		postID    string
		personaID string
	)

	err = tx.QueryRow(ctx, `
		SELECT id, job_type, post_id::text, persona_id::text
		FROM jobs
		WHERE status IN ('PENDING', 'FAILED')
		  AND attempts < 5
		  AND available_at <= NOW()
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&jobID, &jobType, &postID, &personaID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status='PROCESSING', locked_at=NOW(), updated_at=NOW()
		WHERE id=$1
	`, jobID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if jobType != "generate_reply" {
		return w.markJobFailed(ctx, jobID, permanentError{message: fmt.Sprintf("unsupported job type: %s", jobType)})
	}

	err = w.executeGenerateReply(ctx, postID, personaID)
	if err == nil {
		return w.markJobDone(ctx, jobID)
	}

	return w.markJobFailed(ctx, jobID, err)
}

func (w *Worker) generateBattleForOnePending(ctx context.Context) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var battleID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM battles
		WHERE status = 'PENDING'
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&battleID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE battles
		SET status='PROCESSING', error='', updated_at=NOW()
		WHERE id = $1
	`, battleID); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}

	if err := w.executeGenerateBattle(ctx, battleID); err != nil {
		return w.markBattleFailed(ctx, battleID, err)
	}
	return w.markBattleDone(ctx, battleID)
}

func (w *Worker) executeGenerateBattle(ctx context.Context, battleID string) error {
	var battle struct {
		Topic string

		PersonaAID                string
		PersonaAName              string
		PersonaABio               string
		PersonaATone              string
		PersonaAWritingSamplesRaw []byte
		PersonaADoNotSayRaw       []byte
		PersonaACatchphrasesRaw   []byte
		PersonaALanguage          string
		PersonaAFormality         int

		PersonaBID                string
		PersonaBName              string
		PersonaBBio               string
		PersonaBTone              string
		PersonaBWritingSamplesRaw []byte
		PersonaBDoNotSayRaw       []byte
		PersonaBCatchphrasesRaw   []byte
		PersonaBLanguage          string
		PersonaBFormality         int
	}

	err := w.db.QueryRow(ctx, `
		SELECT
			b.topic,
			pa.id::text,
			pa.name,
			pa.bio,
			pa.tone,
			pa.writing_samples,
			pa.do_not_say,
			pa.catchphrases,
			pa.preferred_language,
			pa.formality,
			pb.id::text,
			pb.name,
			pb.bio,
			pb.tone,
			pb.writing_samples,
			pb.do_not_say,
			pb.catchphrases,
			pb.preferred_language,
			pb.formality
		FROM battles b
		JOIN personas pa ON pa.id = b.persona_a_id
		JOIN personas pb ON pb.id = b.persona_b_id
		WHERE b.id = $1
	`, battleID).Scan(
		&battle.Topic,
		&battle.PersonaAID,
		&battle.PersonaAName,
		&battle.PersonaABio,
		&battle.PersonaATone,
		&battle.PersonaAWritingSamplesRaw,
		&battle.PersonaADoNotSayRaw,
		&battle.PersonaACatchphrasesRaw,
		&battle.PersonaALanguage,
		&battle.PersonaAFormality,
		&battle.PersonaBID,
		&battle.PersonaBName,
		&battle.PersonaBBio,
		&battle.PersonaBTone,
		&battle.PersonaBWritingSamplesRaw,
		&battle.PersonaBDoNotSayRaw,
		&battle.PersonaBCatchphrasesRaw,
		&battle.PersonaBLanguage,
		&battle.PersonaBFormality,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return permanentError{message: "battle not found"}
		}
		return err
	}

	topic := strings.TrimSpace(battle.Topic)
	if err := safety.ValidateContent(topic, 240); err != nil {
		return permanentError{message: "battle topic failed safety validation"}
	}

	personaA := ai.PersonaContext{
		ID:                battle.PersonaAID,
		Name:              battle.PersonaAName,
		Bio:               battle.PersonaABio,
		Tone:              battle.PersonaATone,
		WritingSamples:    parseJSONStringSlice(battle.PersonaAWritingSamplesRaw),
		DoNotSay:          parseJSONStringSlice(battle.PersonaADoNotSayRaw),
		Catchphrases:      parseJSONStringSlice(battle.PersonaACatchphrasesRaw),
		PreferredLanguage: strings.TrimSpace(battle.PersonaALanguage),
		Formality:         battle.PersonaAFormality,
	}
	if personaA.PreferredLanguage == "" {
		personaA.PreferredLanguage = "en"
	}

	personaB := ai.PersonaContext{
		ID:                battle.PersonaBID,
		Name:              battle.PersonaBName,
		Bio:               battle.PersonaBBio,
		Tone:              battle.PersonaBTone,
		WritingSamples:    parseJSONStringSlice(battle.PersonaBWritingSamplesRaw),
		DoNotSay:          parseJSONStringSlice(battle.PersonaBDoNotSayRaw),
		Catchphrases:      parseJSONStringSlice(battle.PersonaBCatchphrasesRaw),
		PreferredLanguage: strings.TrimSpace(battle.PersonaBLanguage),
		Formality:         battle.PersonaBFormality,
	}
	if personaB.PreferredLanguage == "" {
		personaB.PreferredLanguage = "en"
	}

	history := make([]ai.BattleTurnContext, 0, battleTurnCount)
	turns := make([]generatedBattleTurn, 0, battleTurnCount)

	for i := 0; i < battleTurnCount; i++ {
		turnIndex := i + 1
		active := personaA
		opponent := personaB
		side := "FOR"
		if i%2 == 1 {
			active = personaB
			opponent = personaA
			side = "AGAINST"
		}

		turn, err := w.generateScoredBattleTurn(ctx, topic, turnIndex, active, opponent, side, history)
		if err != nil {
			return err
		}
		turns = append(turns, turn)

		history = append(history, ai.BattleTurnContext{
			TurnIndex:   turnIndex,
			PersonaName: turn.PersonaName,
			Side:        turn.Side,
			Claim:       turn.Claim,
			Evidence:    turn.Evidence,
		})
	}

	verdictOut, err := w.llm.GenerateBattleVerdict(ctx, ai.BattleVerdictInput{
		Topic:    topic,
		PersonaA: personaA,
		PersonaB: personaB,
		Turns:    history,
	})
	if err != nil {
		return err
	}

	verdictText := truncateWords(strings.TrimSpace(verdictOut.Verdict), battleVerdictWords)
	if verdictText == "" {
		return permanentError{message: "battle verdict generation returned empty verdict"}
	}
	if err := safety.ValidateContent(verdictText, 600); err != nil {
		return permanentError{message: err.Error()}
	}

	takeaways := normalizeTakeaways(verdictOut.Takeaways, topic, personaA.Name, personaB.Name)
	for _, takeaway := range takeaways {
		if err := safety.ValidateContent(takeaway, 260); err != nil {
			return permanentError{message: err.Error()}
		}
	}

	verdictPayload, err := json.Marshal(storedBattleVerdict{
		Verdict:   verdictText,
		Takeaways: takeaways,
	})
	if err != nil {
		return err
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM battle_turns
		WHERE battle_id = $1
	`, battleID); err != nil {
		return err
	}

	for _, turn := range turns {
		metadataRaw, err := json.Marshal(turn.Metadata)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO battle_turns(battle_id, turn_index, persona_id, content, metadata)
			VALUES ($1, $2, $3, $4, $5::jsonb)
		`, battleID, turn.TurnIndex, turn.PersonaID, turn.Content, metadataRaw); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(ctx, `
		UPDATE battles
		SET verdict = $2::jsonb, updated_at = NOW()
		WHERE id = $1
	`, battleID, verdictPayload); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (w *Worker) executeGenerateReply(ctx context.Context, postID, personaID string) error {
	var persona struct {
		Name            string
		Bio             string
		Tone            string
		DailyReplyQuota int
	}
	err := w.db.QueryRow(ctx, `
		SELECT name, bio, tone, daily_reply_quota
		FROM personas
		WHERE id = $1
	`, personaID).Scan(&persona.Name, &persona.Bio, &persona.Tone, &persona.DailyReplyQuota)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return permanentError{message: "persona not found"}
		}
		return err
	}

	var used int
	if err := w.db.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM quota_events
		WHERE persona_id = $1
		  AND quota_type = 'reply'
		  AND created_at >= date_trunc('day', NOW())
	`, personaID).Scan(&used); err != nil {
		return err
	}
	if used >= persona.DailyReplyQuota {
		return permanentError{message: "daily reply quota reached"}
	}

	var postContent, postStatus, roomID string
	err = w.db.QueryRow(ctx, `
		SELECT content, status::text, room_id::text
		FROM posts
		WHERE id = $1
	`, postID).Scan(&postContent, &postStatus, &roomID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return permanentError{message: "post not found"}
		}
		return err
	}
	if postStatus != "PUBLISHED" {
		return permanentError{message: "post is not published"}
	}

	var alreadyExists bool
	if err := w.db.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1
			FROM replies
			WHERE post_id = $1 AND persona_id = $2
		)
	`, postID, personaID).Scan(&alreadyExists); err != nil {
		return err
	}
	if alreadyExists {
		return permanentError{message: "reply already exists"}
	}

	rows, err := w.db.Query(ctx, `
		SELECT id::text, content
		FROM replies
		WHERE post_id = $1
		ORDER BY created_at ASC
	`, postID)
	if err != nil {
		return err
	}
	defer rows.Close()

	thread := make([]ai.ReplyContext, 0)
	for rows.Next() {
		var reply ai.ReplyContext
		if err := rows.Scan(&reply.ID, &reply.Content); err != nil {
			return err
		}
		thread = append(thread, reply)
	}

	generated, err := w.llm.GenerateReply(ctx, ai.PersonaContext{
		ID:   personaID,
		Name: persona.Name,
		Bio:  persona.Bio,
		Tone: persona.Tone,
	}, ai.PostContext{
		ID:      postID,
		Content: postContent,
	}, thread)
	if err != nil {
		return err
	}

	if err := safety.ValidateContent(generated, w.cfg.ReplyMaxLen); err != nil {
		return permanentError{message: err.Error()}
	}

	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `
		INSERT INTO replies(post_id, persona_id, authored_by, content)
		VALUES ($1, $2, 'AI', $3)
	`, postID, personaID, generated)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return permanentError{message: "reply already exists"}
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO quota_events(persona_id, quota_type)
		VALUES ($1, 'reply')
	`, personaID); err != nil {
		return err
	}

	metadata := map[string]any{
		"post_id":       postID,
		"room_id":       roomID,
		"post_preview":  truncatePreview(postContent, 200),
		"reply_preview": truncatePreview(generated, 200),
	}
	if err := insertPersonaActivityEvent(ctx, tx, personaID, "reply_generated", metadata); err != nil {
		return err
	}
	if err := insertPersonaActivityEvent(ctx, tx, personaID, "thread_participated", metadata); err != nil {
		return err
	}

	return tx.Commit(ctx)
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
	summary = truncatePreview(summary, w.cfg.SummaryMaxLen)

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
		thread.PostPreview = truncatePreview(thread.PostPreview, 220)
		stats.TopThreads = append(stats.TopThreads, thread)
	}
	if err := rows.Err(); err != nil {
		return digestStats{}, err
	}

	return stats, nil
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

func truncatePreview(value string, maxRunes int) string {
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

func (w *Worker) generateScoredBattleTurn(
	ctx context.Context,
	topic string,
	turnIndex int,
	active ai.PersonaContext,
	opponent ai.PersonaContext,
	side string,
	history []ai.BattleTurnContext,
) (generatedBattleTurn, error) {
	buildAttempt := func(strict bool) (generatedBattleTurn, error) {
		generated, err := w.llm.GenerateBattleTurn(ctx, ai.BattleTurnInput{
			Topic:     topic,
			Persona:   active,
			Opponent:  opponent,
			Side:      side,
			TurnIndex: turnIndex,
			History:   history,
			Strict:    strict,
		})
		if err != nil {
			return generatedBattleTurn{}, err
		}

		claim := strings.TrimSpace(generated.Claim)
		evidence := strings.TrimSpace(generated.Evidence)
		if claim == "" || evidence == "" {
			return generatedBattleTurn{}, permanentError{message: "battle turn generation returned empty claim or evidence"}
		}

		content := formatBattleTurnContent(claim, evidence, battleTurnWordLimit)
		if err := safety.ValidateContent(content, 1200); err != nil {
			return generatedBattleTurn{}, permanentError{message: err.Error()}
		}

		quality := evaluateBattleTurnQuality(content, claim, evidence, history, battleTurnWordLimit)
		return generatedBattleTurn{
			TurnIndex:    turnIndex,
			PersonaID:    active.ID,
			PersonaName:  active.Name,
			Side:         side,
			Claim:        claim,
			Evidence:     evidence,
			Content:      content,
			QualityScore: quality.Score,
			Metadata: map[string]any{
				"quality_score":   quality.Score,
				"quality_label":   qualityLabel(quality.Score),
				"quality_reasons": quality.Reasons,
				"strict_prompt":   strict,
			},
		}, nil
	}

	initialTurn, err := buildAttempt(false)
	if err != nil {
		return generatedBattleTurn{}, err
	}
	if initialTurn.QualityScore >= battleMinQuality {
		return initialTurn, nil
	}

	retryTurn, err := buildAttempt(true)
	if err != nil {
		return initialTurn, nil
	}

	initialTurn.Metadata["regenerated"] = true
	retryTurn.Metadata["regenerated"] = true
	if retryTurn.QualityScore >= initialTurn.QualityScore {
		return retryTurn, nil
	}
	return initialTurn, nil
}

func formatBattleTurnContent(claim, evidence string, maxWords int) string {
	normalizedClaim := strings.TrimSpace(claim)
	normalizedEvidence := strings.TrimSpace(evidence)
	if maxWords <= 2 {
		return fmt.Sprintf("Claim: %s\nEvidence: %s", normalizedClaim, normalizedEvidence)
	}

	usableWords := maxWords - 2 // Claim: and Evidence:
	claimWords := usableWords / 2
	if claimWords < 8 {
		claimWords = 8
	}
	if claimWords > usableWords-1 {
		claimWords = usableWords - 1
	}
	evidenceWords := usableWords - claimWords
	if evidenceWords < 1 {
		evidenceWords = 1
	}

	normalizedClaim = truncateWords(normalizedClaim, claimWords)
	normalizedEvidence = truncateWords(normalizedEvidence, evidenceWords)
	return fmt.Sprintf("Claim: %s\nEvidence: %s", normalizedClaim, normalizedEvidence)
}

func truncateWords(value string, maxWords int) string {
	trimmed := strings.TrimSpace(value)
	if maxWords <= 0 || trimmed == "" {
		return ""
	}

	words := strings.Fields(trimmed)
	if len(words) <= maxWords {
		return strings.Join(words, " ")
	}
	return strings.Join(words[:maxWords], " ")
}

func evaluateBattleTurnQuality(content, claim, evidence string, history []ai.BattleTurnContext, maxWords int) turnQualityResult {
	score := 100
	reasons := make([]string, 0)

	lowerContent := strings.ToLower(strings.TrimSpace(content))
	hasClaim := strings.Contains(lowerContent, "claim:")
	hasEvidence := strings.Contains(lowerContent, "evidence:")
	if !hasClaim || !hasEvidence {
		score -= 35
		reasons = append(reasons, "Missing explicit Claim/Evidence format")
	}
	if strings.TrimSpace(claim) == "" || strings.TrimSpace(evidence) == "" {
		score -= 25
		reasons = append(reasons, "Claim or evidence is empty")
	}

	wordCount := len(strings.Fields(strings.TrimSpace(content)))
	if wordCount > maxWords {
		score -= 20
		reasons = append(reasons, "Exceeds word limit")
	}

	if !hasSpecificEvidence(evidence) {
		score -= 20
		reasons = append(reasons, "Evidence lacks specificity")
	}

	if isRepetitiveTurn(claim, evidence, history) {
		score -= 20
		reasons = append(reasons, "Repeats prior turn arguments")
	}

	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "Clear claim and specific evidence")
	}

	return turnQualityResult{
		Score:   score,
		Reasons: reasons,
	}
}

func hasSpecificEvidence(evidence string) bool {
	trimmed := strings.TrimSpace(evidence)
	if trimmed == "" {
		return false
	}
	if numberPattern.MatchString(trimmed) {
		return true
	}
	return specificEvidencePattern.MatchString(trimmed)
}

func isRepetitiveTurn(claim, evidence string, history []ai.BattleTurnContext) bool {
	claimNorm := normalizeText(claim)
	evidenceNorm := normalizeText(evidence)
	if claimNorm == "" && evidenceNorm == "" {
		return false
	}

	for _, previous := range history {
		prevClaim := normalizeText(previous.Claim)
		prevEvidence := normalizeText(previous.Evidence)

		if claimNorm != "" && claimNorm == prevClaim {
			return true
		}
		if evidenceNorm != "" && evidenceNorm == prevEvidence {
			return true
		}
		if jaccardSimilarity(claimNorm, prevClaim) >= 0.78 {
			return true
		}
		if jaccardSimilarity(evidenceNorm, prevEvidence) >= 0.78 {
			return true
		}
	}
	return false
}

func normalizeText(value string) string {
	lower := strings.ToLower(strings.TrimSpace(value))
	if lower == "" {
		return ""
	}
	var b strings.Builder
	lastSpace := false
	for _, r := range lower {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteRune(' ')
			lastSpace = true
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func jaccardSimilarity(a, b string) float64 {
	if strings.TrimSpace(a) == "" || strings.TrimSpace(b) == "" {
		return 0
	}

	setA := map[string]struct{}{}
	for _, token := range strings.Fields(a) {
		setA[token] = struct{}{}
	}
	setB := map[string]struct{}{}
	for _, token := range strings.Fields(b) {
		setB[token] = struct{}{}
	}

	intersection := 0
	for token := range setA {
		if _, exists := setB[token]; exists {
			intersection++
		}
	}
	if intersection == 0 {
		return 0
	}

	union := len(setA)
	for token := range setB {
		if _, exists := setA[token]; !exists {
			union++
		}
	}
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

func qualityLabel(score int) string {
	if score >= 80 {
		return "HIGH"
	}
	if score >= 60 {
		return "MED"
	}
	return "LOW"
}

func normalizeTakeaways(raw []string, topic, personaAName, personaBName string) []string {
	cleaned := make([]string, 0, 3)
	seen := map[string]struct{}{}
	for _, item := range raw {
		trimmed := truncateWords(strings.TrimSpace(item), 24)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, trimmed)
		if len(cleaned) == 3 {
			break
		}
	}

	fallback := []string{
		fmt.Sprintf("Concrete evidence changed the quality of the %s debate.", topic),
		fmt.Sprintf("%s and %s benefited from concise claim framing.", personaAName, personaBName),
		"Short alternating turns made the discussion easier to follow.",
	}
	for _, item := range fallback {
		if len(cleaned) == 3 {
			break
		}
		trimmed := truncateWords(strings.TrimSpace(item), 24)
		if trimmed == "" {
			continue
		}
		key := strings.ToLower(trimmed)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		cleaned = append(cleaned, trimmed)
	}
	return cleaned
}

func (w *Worker) markBattleDone(ctx context.Context, battleID string) error {
	_, err := w.db.Exec(ctx, `
		UPDATE battles
		SET status='DONE', error='', updated_at=NOW()
		WHERE id = $1
	`, battleID)
	if err == nil {
		log.Printf("battle %s completed", battleID)
	}
	return err
}

func (w *Worker) markBattleFailed(ctx context.Context, battleID string, failure error) error {
	errorText := truncatePreview(failure.Error(), 500)
	_, err := w.db.Exec(ctx, `
		UPDATE battles
		SET status='FAILED', error=$2, updated_at=NOW()
		WHERE id = $1
	`, battleID, errorText)
	if err == nil {
		log.Printf("battle %s failed: %s", battleID, errorText)
	}
	return err
}

func (w *Worker) markJobDone(ctx context.Context, jobID int64) error {
	_, err := w.db.Exec(ctx, `
		UPDATE jobs
		SET status='DONE', error=NULL, locked_at=NULL, updated_at=NOW()
		WHERE id=$1
	`, jobID)
	if err == nil {
		log.Printf("job %d completed", jobID)
	}
	return err
}

func (w *Worker) markJobFailed(ctx context.Context, jobID int64, failure error) error {
	_, isPermanent := failure.(permanentError)
	if isPermanent {
		_, err := w.db.Exec(ctx, `
			UPDATE jobs
			SET status='FAILED', attempts=5, error=$2, locked_at=NULL, updated_at=NOW()
			WHERE id=$1
		`, jobID, failure.Error())
		if err == nil {
			log.Printf("job %d permanently failed: %s", jobID, failure.Error())
		}
		return err
	}

	_, err := w.db.Exec(ctx, `
		UPDATE jobs
		SET status='FAILED', attempts=attempts+1, error=$2, locked_at=NULL, available_at=NOW()+INTERVAL '30 seconds', updated_at=NOW()
		WHERE id=$1
	`, jobID, failure.Error())
	if err == nil {
		log.Printf("job %d failed and will retry: %s", jobID, failure.Error())
	}
	return err
}
