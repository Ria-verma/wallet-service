package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"wallet-service/internal/obs"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// requestID gives every request a correlation id, carried in the response
// header and on every log line the request emits.
func requestID(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-Id", id)
		reqLogger := logger.With("request_id", id, "method", r.Method, "path", r.URL.Path)
		next.ServeHTTP(w, r.WithContext(obs.WithLogger(r.Context(), reqLogger)))
	})
}

func metrics(m *obs.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next.ServeHTTP(rec, r)
		elapsed := time.Since(start)

		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		m.HTTPRequests.WithLabelValues(route, r.Method, strconv.Itoa(rec.status)).Inc()
		m.HTTPDuration.WithLabelValues(route).Observe(elapsed.Seconds())

		obs.Log(r.Context()).Info("request",
			"status", rec.status, "duration_ms", elapsed.Milliseconds())
	})
}

func recovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if p := recover(); p != nil {
				obs.Log(r.Context()).Error("panic", "panic", p)
				http.Error(w, `{"success":false,"message":"internal error","error":"internal"}`, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// withTimeout bounds every request with a context deadline. pgx cancels
// in-flight queries when the deadline hits, so a slow or hung database
// turns into a fast, safe 503 instead of an unbounded wait (fail closed).
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type TokenVerifier interface {
	VerifyToken(token string) (uuid.UUID, error)
}

// authenticate extracts the caller from the Bearer token. Identity comes
// ONLY from the verified token, never from a header or body field.
func authenticate(verifier TokenVerifier, m *obs.Metrics, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || token == "" {
			authFail(w, r, m, "missing bearer token")
			return
		}
		userID, err := verifier.VerifyToken(token)
		if err != nil {
			authFail(w, r, m, "invalid or expired token")
			return
		}
		ctx := withUserID(r.Context(), userID)
		ctx = obs.WithLogger(ctx, obs.Log(ctx).With("user_id", userID.String()))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authFail(w http.ResponseWriter, r *http.Request, m *obs.Metrics, reason string) {
	m.AuthFailures.Inc()
	obs.Log(r.Context()).Warn("auth_failure", "reason", reason)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"success":false,"message":"` + reason + `","error":"unauthorized"}`))
}
