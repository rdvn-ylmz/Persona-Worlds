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

	for {
		if err := w.generateDigestForOnePersona(ctx); err != nil {
			w.logger.Error("worker_digest_process_error", observability.Fields{
				"error": err.Error(),
			})
		}

		if err := w.generateWeeklyDigestForOneUser(ctx); err != nil {
			w.logger.Error("worker_weekly_digest_process_error", observability.Fields{
				"error": err.Error(),
			})
		}

		if err := w.processOne(ctx); err != nil {
			w.logger.Error("worker_process_error", observability.Fields{
				"error": err.Error(),
			})
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
