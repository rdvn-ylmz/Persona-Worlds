package worker

import (
	"context"
	"log"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"

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
