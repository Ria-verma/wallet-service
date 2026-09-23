package dto

import "time"

type SignupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type AuthResponse struct {
	Token  string `json:"token"`
	UserID string `json:"user_id"`
}

type AccountResponse struct {
	UserID       string `json:"user_id"`
	BalancePaise int64  `json:"balance_paise"`
}

type BalanceResponse struct {
	BalancePaise int64 `json:"balance_paise"`
	// Spendable part of the balance: excludes money received within the
	// claim window, which its senders can still claim back.
	AvailablePaise int64 `json:"available_paise"`
}

type TransferRequest struct {
	ToUser         string `json:"to_user"`
	AmountPaise    int64  `json:"amount_paise"`
	IdempotencyKey string `json:"idempotency_key"`
}

type TransferResponse struct {
	TransferID string `json:"transfer_id"`
	NewBalance int64  `json:"new_balance"`
}

type TransferDetails struct {
	TransferID  string     `json:"transfer_id"`
	SenderID    string     `json:"sender_id"`
	RecipientID string     `json:"recipient_id"`
	AmountPaise int64      `json:"amount_paise"`
	Status      string     `json:"status"`
	CreatedAt   time.Time  `json:"created_at"`
	ReversedAt  *time.Time `json:"reversed_at,omitempty"`
}

type ClaimResponse struct {
	TransferID string    `json:"transfer_id"`
	Status     string    `json:"status"`
	NewBalance int64     `json:"new_balance"`
	ReversedAt time.Time `json:"reversed_at"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}
