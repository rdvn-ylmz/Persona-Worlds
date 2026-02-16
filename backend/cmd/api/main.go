package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"personaworlds/backend/internal/ai"
	"personaworlds/backend/internal/api"
	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
	"personaworlds/backend/internal/observability"
)

func main() {
	cfg := config.Load()
	logger := observability.NewLogger("api")

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
	server := api.New(cfg, pool, llm)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           server.Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	serverErrCh := make(chan error, 1)

	go func() {
		logger.Info("api_listening", observability.Fields{
			"addr": ":" + cfg.Port,
		})
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrCh <- err
		}
	}()

	select {
	case <-ctx.Done():
	case err := <-serverErrCh:
		logger.Error("http_server_failed", observability.Fields{"error": err.Error()})
		stop()
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful_shutdown_failed", observability.Fields{"error": err.Error()})
	}
	logger.Info("api_stopped", nil)
}
