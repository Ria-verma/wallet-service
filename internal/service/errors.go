package service

import "errors"

// Domain errors. Handlers map these onto HTTP status codes; the string is
// also the machine-readable "error" field in responses.
var (
	ErrUsernameTaken       = errors.New("username_taken")
	ErrInvalidCredentials  = errors.New("invalid_credentials")
	ErrUnknownRecipient    = errors.New("unknown_recipient")
	ErrSelfTransfer        = errors.New("self_transfer")
	ErrNoWallet            = errors.New("no_wallet")
	ErrInsufficientFunds   = errors.New("insufficient_funds")
	ErrIdempotencyConflict = errors.New("idempotency_conflict")
	ErrNotFound            = errors.New("not_found")
	ErrClaimWindowExpired  = errors.New("claim_window_expired")
)

// ValidationError carries a human-readable reason for a 400.
type ValidationError struct {
	Msg string
}

func (e ValidationError) Error() string { return e.Msg }

func Invalid(msg string) error { return ValidationError{Msg: msg} }
