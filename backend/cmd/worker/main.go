package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
	"personaworlds/backend/internal/worker"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect failed: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	llm := ai.NewFromConfig(cfg)
	w := worker.New(cfg, pool, llm)

	log.Printf("worker started with poll interval %s", cfg.WorkerPollEvery)
	w.Run(ctx)
	log.Println("worker stopped")
}
