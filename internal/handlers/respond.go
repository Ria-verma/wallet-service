package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-service/internal/dto"
	"wallet-service/internal/obs"
	"wallet-service/internal/service"
)

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, dto.ErrorResponse{Success: false, Message: msg, Error: code})
}

// writeDomainError maps domain errors to HTTP responses. Unknown errors —
// in practice, the database being slow or unreachable — become 503: the
// transfer path fails closed rather than guessing (consistency over
// availability), and a 503 is honest "retry later", never a torn write.
func writeDomainError(w http.ResponseWriter, r *http.Request, err error) {
	var ve service.ValidationError
	switch {
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "validation_error", ve.Msg)
	case errors.Is(err, service.ErrUsernameTaken):
		writeError(w, http.StatusConflict, "username_taken", "that username is already registered")
	case errors.Is(err, service.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
	case errors.Is(err, service.ErrUnknownRecipient):
		writeError(w, http.StatusNotFound, "unknown_recipient", "recipient user does not exist")
	case errors.Is(err, service.ErrSelfTransfer):
		writeError(w, http.StatusBadRequest, "self_transfer", "cannot transfer to yourself")
	case errors.Is(err, service.ErrNoWallet):
		writeError(w, http.StatusNotFound, "no_wallet", "no wallet yet; call POST /accounts first")
	case errors.Is(err, service.ErrInsufficientFunds):
		writeError(w, http.StatusUnprocessableEntity, "insufficient_funds", "available balance is lower than the transfer amount (recently received money is on hold while its senders can still claim it back)")
	case errors.Is(err, service.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "idempotency_key was already used with a different request body")
	case errors.Is(err, service.ErrClaimWindowExpired):
		writeError(w, http.StatusConflict, "claim_window_expired", "the claim window for this transfer has expired")
	case errors.Is(err, service.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "resource not found")
	default:
		obs.Log(r.Context()).Error("db_unavailable", "error", err.Error())
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "temporarily unable to process the request; safe to retry")
	}
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", "request body must be valid JSON: "+err.Error())
		return false
	}
	return true
}
