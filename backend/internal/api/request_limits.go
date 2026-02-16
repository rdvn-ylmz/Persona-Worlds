package api

import (
	"context"
	"net/http"
	"strings"

	"personaworlds/backend/internal/observability"
)

func (s *Server) requestContextTimeoutMiddleware(next http.Handler) http.Handler {
	timeout := s.cfg.APIRequestTimeout
	if timeout <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), timeout)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) maxBodyBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	if limit <= 0 {
		limit = 1 << 20
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r == nil {
				next.ServeHTTP(w, r)
				return
			}
			method := strings.ToUpper(strings.TrimSpace(r.Method))
			if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
				r.Body = http.MaxBytesReader(w, r.Body, limit)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) writeRateLimitResponse(w http.ResponseWriter, r *http.Request, scope, endpoint, message string) {
	if strings.TrimSpace(endpoint) == "" {
		endpoint = routePatternFromRequest(r)
	}
	s.metrics.IncRateLimited(scope, endpoint)

	fields := observability.Fields{
		"request_id": requestIDFromRequest(r),
		"route":      routePatternFromRequest(r),
		"method":     strings.ToUpper(strings.TrimSpace(r.Method)),
		"status":     http.StatusTooManyRequests,
		"scope":      strings.TrimSpace(scope),
		"endpoint":   strings.TrimSpace(endpoint),
	}
	if userID, ok := s.optionalUserIDFromRequest(r); ok {
		fields["user_id"] = userID
	}
	s.logger.Warn("rate_limited", fields)
	writeTooManyRequests(w, message)
}
