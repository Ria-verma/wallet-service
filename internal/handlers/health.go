package handlers

import (
	"context"
	"net/http"
	"time"
)

type Pinger interface {
	Ping(ctx context.Context) error
}

type HealthHandler struct {
	db Pinger
}

func NewHealthHandler(db Pinger) *HealthHandler {
	return &HealthHandler{db: db}
}

// Index gives visitors of the bare domain a map of the API instead of a 404.
func (h *HealthHandler) Index(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"service": "wallet-service",
		"docs":    "https://github.com/Ria-verma/wallet-service",
		"endpoints": []string{
			"POST /auth/signup {username, password}",
			"POST /auth/login {username, password}",
			"POST /accounts (Bearer)",
			"GET /accounts/me (Bearer)",
			"POST /transfers {to_user, amount_paise, idempotency_key} (Bearer)",
			"GET /transfers/{id} (Bearer)",
			"GET /healthz | /readyz | /metrics | /logs",
		},
	})
}

// Liveness: the process is up and serving.
func (h *HealthHandler) Healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// Readiness: includes the datastore, so a dead database takes the instance
// out of rotation instead of letting it accept writes it cannot honor.
func (h *HealthHandler) Readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	if err := h.db.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unready", "reason": "database unreachable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}
