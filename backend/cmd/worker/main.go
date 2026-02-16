package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
	"personaworlds/backend/internal/observability"
	"personaworlds/backend/internal/worker"
)

func main() {
	cfg := config.Load()
	logger := observability.NewLogger("worker")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("startup_failed", observability.Fields{
			"step":  "db_connect",
			"error": err.Error(),
		})
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		logger.Error("startup_failed", observability.Fields{
			"step":  "run_migrations",
			"error": err.Error(),
		})
		os.Exit(1)
	}

	llm := ai.NewFromConfig(cfg)
	w := worker.New(cfg, pool, llm)
	observabilityServer := &http.Server{
		Addr:              ":" + cfg.WorkerObservabilityPort,
		Handler:           w.ObservabilityHandler(),
		ReadHeaderTimeout: cfg.APIReadTimeout,
		ReadTimeout:       cfg.APIReadTimeout,
		WriteTimeout:      cfg.APIWriteTimeout,
		IdleTimeout:       cfg.APIIdleTimeout,
	}
	observabilityErrCh := make(chan error, 1)

	go func() {
		logger.Info("worker_observability_listening", observability.Fields{
			"addr": ":" + cfg.WorkerObservabilityPort,
		})
		if err := observabilityServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			observabilityErrCh <- err
		}
	}()

	logger.Info("worker_started", observability.Fields{
		"poll_every_ms": cfg.WorkerPollEvery.Milliseconds(),
	})
	workerDoneCh := make(chan struct{})
	go func() {
		w.Run(ctx)
		close(workerDoneCh)
	}()

	select {
	case <-ctx.Done():
	case err := <-observabilityErrCh:
		logger.Error("worker_observability_server_failed", observability.Fields{"error": err.Error()})
		stop()
	case <-workerDoneCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := observabilityServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("worker_observability_shutdown_failed", observability.Fields{"error": err.Error()})
	}
	<-workerDoneCh
	logger.Info("worker_stopped", nil)
}
