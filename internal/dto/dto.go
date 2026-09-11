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
	TransferID  string    `json:"transfer_id"`
	SenderID    string    `json:"sender_id"`
	RecipientID string    `json:"recipient_id"`
	AmountPaise int64     `json:"amount_paise"`
	CreatedAt   time.Time `json:"created_at"`
}

type ErrorResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Error   string `json:"error"`
}
