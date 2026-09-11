package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wallet-service/internal/service"
)

func TestWriteDomainErrorMapping(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
		wantCode   string
	}{
		{service.Invalid("bad"), http.StatusBadRequest, "validation_error"},
		{service.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{service.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{service.ErrUnknownRecipient, http.StatusNotFound, "unknown_recipient"},
		{service.ErrSelfTransfer, http.StatusBadRequest, "self_transfer"},
		{service.ErrNoWallet, http.StatusNotFound, "no_wallet"},
		{service.ErrInsufficientFunds, http.StatusUnprocessableEntity, "insufficient_funds"},
		{service.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
		{service.ErrNotFound, http.StatusNotFound, "not_found"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		writeDomainError(rec, req, c.err)
		if rec.Code != c.wantStatus {
			t.Errorf("%v: status = %d, want %d", c.err, rec.Code, c.wantStatus)
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("%v: invalid JSON body: %v", c.err, err)
		}
		if body["error"] != c.wantCode {
			t.Errorf("%v: error code = %v, want %s", c.err, body["error"], c.wantCode)
		}
		if body["success"] != false {
			t.Errorf("%v: success must be false", c.err)
		}
	}
}

func TestWriteDomainErrorUnknownBecomes503(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x", nil)
	writeDomainError(rec, req, http.ErrHandlerTimeout)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("unknown error: status = %d, want 503 (fail closed, never a torn 500)", rec.Code)
	}
}

func TestDecodeJSONRejectsBadBodies(t *testing.T) {
	for _, body := range []string{
		`{"amount_paise": "100"}`, // wrong type
		`{"amount_paise": 1.5}`,   // float paise
		`{"unknown_field": 1}`,    // unknown field
		`not json`,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		var dst struct {
			AmountPaise int64 `json:"amount_paise"`
		}
		if decodeJSON(rec, req, &dst) {
			t.Errorf("body %q was accepted", body)
		}
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: status = %d, want 400", body, rec.Code)
		}
	}
}
