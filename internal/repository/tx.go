package repository

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"wallet-service/internal/models"
)

// Tx exposes the statements a transfer needs, all bound to one transaction.
type Tx struct {
	tx pgx.Tx
}

func (t Tx) WalletExists(ctx context.Context, userID uuid.UUID) (bool, error) {
	var exists bool
	err := t.tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM wallets WHERE user_id = $1)`, userID,
	).Scan(&exists)
	return exists, err
}

// InsertWallet returns created=false when the row already exists — under
// concurrency that is the request that lost the get-or-create race.
func (t Tx) InsertWallet(ctx context.Context, userID uuid.UUID, seedPaise int64) (created bool, err error) {
	tag, err := t.tx.Exec(ctx,
		`INSERT INTO wallets (user_id, balance_paise) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO NOTHING`,
		userID, seedPaise,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// LockWallet takes the row lock that serializes concurrent transfers
// touching this wallet. Callers must lock wallets in ascending user_id
// order to stay deadlock-free.
func (t Tx) LockWallet(ctx context.Context, userID uuid.UUID) (int64, error) {
	var balance int64
	err := t.tx.QueryRow(ctx,
		`SELECT balance_paise FROM wallets WHERE user_id = $1 FOR UPDATE`, userID,
	).Scan(&balance)
	return balance, mapNoRows(err)
}

// InsertTransfer records the transfer and claims the idempotency key in one
// statement. inserted=false means the (sender, key) pair already exists —
// a concurrent duplicate waits here for the winner to commit, then takes
// the same path, so a key can never be claimed twice.
func (t Tx) InsertTransfer(ctx context.Context, tr models.Transfer) (id uuid.UUID, inserted bool, err error) {
	err = t.tx.QueryRow(ctx,
		`INSERT INTO transfers (sender_id, recipient_id, amount_paise,
		     idempotency_key, body_hash, sender_balance_after)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (sender_id, idempotency_key) DO NOTHING
		 RETURNING id`,
		tr.SenderID, tr.RecipientID, tr.AmountPaise,
		tr.IdempotencyKey, tr.BodyHash, tr.SenderBalanceAfter,
	).Scan(&id)
	if err == pgx.ErrNoRows {
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, err
	}
	return id, true, nil
}

func (t Tx) UpdateWalletBalance(ctx context.Context, userID uuid.UUID, newBalance int64) error {
	_, err := t.tx.Exec(ctx,
		`UPDATE wallets SET balance_paise = $2, updated_at = now() WHERE user_id = $1`,
		userID, newBalance,
	)
	return err
}
