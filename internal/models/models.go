package models

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID           uuid.UUID
	Username     string
	PasswordHash string
	CreatedAt    time.Time
}

type Wallet struct {
	UserID       uuid.UUID
	BalancePaise int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Transfer struct {
	ID                 uuid.UUID
	SenderID           uuid.UUID
	RecipientID        uuid.UUID
	AmountPaise        int64
	IdempotencyKey     string
	BodyHash           string
	SenderBalanceAfter int64
	CreatedAt          time.Time
}
