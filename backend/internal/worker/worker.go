package worker

import (
	"context"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/observability"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Worker struct {
	cfg     config.Config
	db      *pgxpool.Pool
	llm     ai.LLMClient
	logger  *observability.Logger
	metrics *observability.WorkerMetrics
}

type permanentError struct {
	message string
}

func (e permanentError) Error() string {
	return e.message
}

func New(cfg config.Config, db *pgxpool.Pool, llm ai.LLMClient) *Worker {
	return &Worker{
		cfg:     cfg,
		db:      db,
		llm:     llm,
		logger:  observability.NewLogger("worker"),
		metrics: observability.NewWorkerMetrics(),
	}
}

func (w *Worker) Run(ctx context.Context) {
	ticker := time.NewTicker(w.cfg.WorkerPollEvery)
	defer ticker.Stop()

	runTask := func(name string, fn func(context.Context) error) {
		taskCtx, cancel := context.WithTimeout(ctx, w.cfg.WorkerTaskTimeout)
		defer cancel()

		if err := fn(taskCtx); err != nil {
			w.logger.Error("worker_task_error", observability.Fields{
				"task":  name,
				"error": err.Error(),
			})
		}
	}

	for {
		runTask("digest_daily", w.generateDigestForOnePersona)
		runTask("digest_weekly", w.generateWeeklyDigestForOneUser)
		runTask("jobs", w.processOne)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
