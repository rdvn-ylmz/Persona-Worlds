package api

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"personaworlds/backend/internal/observability"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const metricsContentType = "text/plain; version=0.0.4; charset=utf-8"

func (s *Server) requestObservabilityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(wrapped, r)

		status := wrapped.Status()
		if status == 0 {
			status = http.StatusOK
		}
		latency := time.Since(startedAt)
		route := routePatternFromRequest(r)

		s.metrics.ObserveHTTPRequest(route, r.Method, status, latency)

		fields := observability.Fields{
			"request_id": requestIDFromRequest(r),
			"route":      route,
			"method":     strings.ToUpper(strings.TrimSpace(r.Method)),
			"status":     status,
			"latency_ms": latency.Milliseconds(),
		}
		if userID, ok := s.optionalUserIDFromRequest(r); ok {
			fields["user_id"] = userID
		}
		s.logger.Info("http_request", fields)
	})
}

func (s *Server) recoverJSONMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic_recovered", observability.Fields{
					"request_id": requestIDFromRequest(r),
					"route":      routePatternFromRequest(r),
					"method":     strings.ToUpper(strings.TrimSpace(r.Method)),
					"status":     http.StatusInternalServerError,
					"panic":      fmt.Sprint(rec),
					"stack":      string(debug.Stack()),
				})
				writeInternalError(w, "internal server error")
			}
		}()

		next.ServeHTTP(w, r)
	})
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if err := s.checkReady(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok":    false,
			"error": err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if s.db != nil {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		err := s.refreshQueueDepthMetrics(ctx)
		cancel()
		if err != nil {
			s.logger.Warn("queue_depth_refresh_failed", observability.Fields{"error": err.Error()})
		}
	}

	w.Header().Set("Content-Type", metricsContentType)
	_, _ = w.Write([]byte(s.metrics.Render()))
}

func (s *Server) checkReady(ctx context.Context) error {
	if s.db == nil {
		return fmt.Errorf("database is not configured")
	}

	pingStartedAt := time.Now()
	if err := s.db.Ping(ctx); err != nil {
		s.metrics.ObserveDBQuery(time.Since(pingStartedAt))
		return fmt.Errorf("database ping failed: %w", err)
	}
	s.metrics.ObserveDBQuery(time.Since(pingStartedAt))

	expectedMigrations, err := countSQLMigrations(s.cfg.MigrationsDir)
	if err != nil {
		return fmt.Errorf("could not inspect migrations: %w", err)
	}

	var appliedMigrations int
	queryStartedAt := time.Now()
	err = s.db.QueryRow(ctx, `
		SELECT COUNT(*)::int
		FROM schema_migrations
	`).Scan(&appliedMigrations)
	s.metrics.ObserveDBQuery(time.Since(queryStartedAt))
	if err != nil {
		return fmt.Errorf("could not read schema_migrations: %w", err)
	}

	if appliedMigrations < expectedMigrations {
		return fmt.Errorf("migrations pending: applied=%d expected=%d", appliedMigrations, expectedMigrations)
	}

	return nil
}

func (s *Server) refreshQueueDepthMetrics(ctx context.Context) error {
	if s.db == nil {
		s.metrics.SetQueueDepthSnapshot(map[string]int{})
		return nil
	}
	maxAttempts := s.cfg.JobMaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 5
	}
	if maxAttempts > 20 {
		maxAttempts = 20
	}

	queryStartedAt := time.Now()
	rows, err := s.db.Query(ctx, `
		SELECT job_type, COUNT(*)::int
		FROM jobs
		WHERE status IN ('PENDING', 'PROCESSING')
		   OR (status = 'FAILED' AND attempts < $1)
		GROUP BY job_type
	`, maxAttempts)
	s.metrics.ObserveDBQuery(time.Since(queryStartedAt))
	if err != nil {
		return err
	}
	defer rows.Close()

	snapshot := map[string]int{}
	for rows.Next() {
		var (
			jobType string
			count   int
		)
		if err := rows.Scan(&jobType, &count); err != nil {
			return err
		}
		snapshot[strings.TrimSpace(jobType)] = count
	}
	if err := rows.Err(); err != nil {
		return err
	}

	s.metrics.SetQueueDepthSnapshot(snapshot)
	return nil
}

func countSQLMigrations(migrationsDir string) (int, error) {
	entries, err := os.ReadDir(strings.TrimSpace(migrationsDir))
	if err != nil {
		return 0, err
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return len(files), nil
}

func routePatternFromRequest(r *http.Request) string {
	if r == nil {
		return "unknown"
	}
	ctx := chi.RouteContext(r.Context())
	if ctx == nil {
		return "unmatched"
	}
	pattern := strings.TrimSpace(ctx.RoutePattern())
	if pattern == "" {
		return "unmatched"
	}
	return pattern
}

func requestIDFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(middleware.GetReqID(r.Context()))
}
