package api

import (
	"log/slog"
	"net/http"
	"time"

	"wallet-service/internal/obs"
)

// Routes decouples the router from concrete handlers.
type Routes struct {
	Signup         http.HandlerFunc
	Login          http.HandlerFunc
	CreateAccount  http.HandlerFunc
	GetMyAccount   http.HandlerFunc
	CreateTransfer http.HandlerFunc
	GetTransfer    http.HandlerFunc
	Healthz        http.HandlerFunc
	Readyz         http.HandlerFunc
	Metrics        http.Handler
	Logs           http.Handler
}

func NewRouter(routes Routes, verifier TokenVerifier, m *obs.Metrics, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	authed := func(h http.HandlerFunc) http.Handler {
		return authenticate(verifier, m, h)
	}

	mux.HandleFunc("POST /auth/signup", routes.Signup)
	mux.HandleFunc("POST /auth/login", routes.Login)
	mux.Handle("POST /accounts", authed(routes.CreateAccount))
	mux.Handle("GET /accounts/me", authed(routes.GetMyAccount))
	mux.Handle("POST /transfers", authed(routes.CreateTransfer))
	mux.Handle("GET /transfers/{id}", authed(routes.GetTransfer))
	mux.HandleFunc("GET /healthz", routes.Healthz)
	mux.HandleFunc("GET /readyz", routes.Readyz)
	mux.Handle("GET /metrics", routes.Metrics)
	mux.Handle("GET /logs", routes.Logs)

	return recovery(requestID(logger, metrics(m, withTimeout(10*time.Second, mux))))
}
