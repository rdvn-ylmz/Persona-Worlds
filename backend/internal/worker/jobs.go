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
	"personaworlds/backend/internal/observability"
	"personaworlds/backend/internal/safety"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func (w *Worker) processOne(ctx context.Context) error {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var (
		jobID      int64
		jobType    string
		postID     string
		personaID  string
		payloadRaw []byte
	)

	selectStartedAt := time.Now()
	err = tx.QueryRow(ctx, `
		SELECT id, job_type, post_id::text, persona_id::text, payload
		FROM jobs
		WHERE status IN ('PENDING', 'FAILED')
		  AND attempts < 5
		  AND available_at <= NOW()
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED
	`).Scan(&jobID, &jobType, &postID, &personaID, &payloadRaw)
	w.metrics.ObserveDBQuery(time.Since(selectStartedAt))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	traceID := extractTraceID(payloadRaw)

	lockStartedAt := time.Now()
	if _, err := tx.Exec(ctx, `
		UPDATE jobs
		SET status='PROCESSING', locked_at=NOW(), updated_at=NOW()
		WHERE id=$1
	`, jobID); err != nil {
		w.metrics.ObserveDBQuery(time.Since(lockStartedAt))
		return err
	}
	w.metrics.ObserveDBQuery(time.Since(lockStartedAt))

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	startedAt := time.Now()
	w.logger.Info("job_started", observability.Fields{
		"job_id":     jobID,
		"job_type":   strings.TrimSpace(jobType),
		"trace_id":   traceID,
		"request_id": traceID,
	})

	if jobType != "generate_reply" {
		return w.markJobFailed(ctx, jobID, jobType, traceID, permanentError{message: fmt.Sprintf("unsupported job type: %s", jobType)}, time.Since(startedAt))
	}

	err = w.executeGenerateReply(ctx, postID, personaID)
	if err == nil {
		return w.markJobDone(ctx, jobID, jobType, traceID, time.Since(startedAt))
	}

	return w.markJobFailed(ctx, jobID, jobType, traceID, err, time.Since(startedAt))
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
		"post_preview":  common.TruncateRunes(postContent, 200),
		"reply_preview": common.TruncateRunes(generated, 200),
	}
	if err := common.InsertPersonaActivityEvent(ctx, tx, personaID, "reply_generated", metadata); err != nil {
		return err
	}
	if err := common.InsertPersonaActivityEvent(ctx, tx, personaID, "thread_participated", metadata); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (w *Worker) markJobDone(ctx context.Context, jobID int64, jobType, traceID string, duration time.Duration) error {
	queryStartedAt := time.Now()
	_, err := w.db.Exec(ctx, `
		UPDATE jobs
		SET status='DONE', error=NULL, locked_at=NULL, updated_at=NOW()
		WHERE id=$1
	`, jobID)
	w.metrics.ObserveDBQuery(time.Since(queryStartedAt))
	if err != nil {
		return err
	}

	w.metrics.ObserveJobProcessed(jobType, "done", traceID, duration)
	w.logger.Info("job_completed", observability.Fields{
		"job_id":     jobID,
		"job_type":   strings.TrimSpace(jobType),
		"status":     "done",
		"latency_ms": duration.Milliseconds(),
		"trace_id":   traceID,
		"request_id": traceID,
	})
	return nil
}

func (w *Worker) markJobFailed(ctx context.Context, jobID int64, jobType, traceID string, failure error, duration time.Duration) error {
	_, isPermanent := failure.(permanentError)
	if isPermanent {
		queryStartedAt := time.Now()
		_, err := w.db.Exec(ctx, `
			UPDATE jobs
			SET status='FAILED', attempts=5, error=$2, locked_at=NULL, updated_at=NOW()
			WHERE id=$1
		`, jobID, failure.Error())
		w.metrics.ObserveDBQuery(time.Since(queryStartedAt))
		if err != nil {
			return err
		}

		w.metrics.ObserveJobProcessed(jobType, "failed", traceID, duration)
		w.logger.Error("job_failed_permanently", observability.Fields{
			"job_id":     jobID,
			"job_type":   strings.TrimSpace(jobType),
			"status":     "failed",
			"latency_ms": duration.Milliseconds(),
			"error":      failure.Error(),
			"trace_id":   traceID,
			"request_id": traceID,
		})
		return nil
	}

	queryStartedAt := time.Now()
	_, err := w.db.Exec(ctx, `
		UPDATE jobs
		SET status='FAILED', attempts=attempts+1, error=$2, locked_at=NULL, available_at=NOW()+INTERVAL '30 seconds', updated_at=NOW()
		WHERE id=$1
	`, jobID, failure.Error())
	w.metrics.ObserveDBQuery(time.Since(queryStartedAt))
	if err != nil {
		return err
	}

	w.metrics.ObserveJobProcessed(jobType, "retry", traceID, duration)
	w.metrics.IncrementJobRetry(jobType)
	w.logger.Error("job_failed_retrying", observability.Fields{
		"job_id":     jobID,
		"job_type":   strings.TrimSpace(jobType),
		"status":     "retry",
		"latency_ms": duration.Milliseconds(),
		"error":      failure.Error(),
		"trace_id":   traceID,
		"request_id": traceID,
	})
	return nil
}

func extractTraceID(payloadRaw []byte) string {
	if len(payloadRaw) == 0 {
		return ""
	}

	var payload map[string]any
	if err := json.Unmarshal(payloadRaw, &payload); err != nil {
		return ""
	}

	traceID, _ := payload["trace_id"].(string)
	return strings.TrimSpace(traceID)
}
