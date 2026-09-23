package repository

import (
	"context"

	"github.com/google/uuid"

	"wallet-service/internal/models"
)

func (s *Store) GetTransferByID(ctx context.Context, id uuid.UUID) (models.Transfer, error) {
	return scanTransfer(s.pool.QueryRow(ctx, transferSelect+` WHERE id = $1`, id))
}

func (s *Store) GetTransferBySenderKey(ctx context.Context, senderID uuid.UUID, key string) (models.Transfer, error) {
	return scanTransfer(s.pool.QueryRow(ctx,
		transferSelect+` WHERE sender_id = $1 AND idempotency_key = $2`, senderID, key))
}

const transferSelect = `SELECT id, sender_id, recipient_id, amount_paise,
	idempotency_key, body_hash, sender_balance_after, status, created_at,
	reversed_at, sender_balance_after_reversal FROM transfers`

type row interface{ Scan(dest ...any) error }

func scanTransfer(r row) (models.Transfer, error) {
	var t models.Transfer
	err := r.Scan(&t.ID, &t.SenderID, &t.RecipientID, &t.AmountPaise,
		&t.IdempotencyKey, &t.BodyHash, &t.SenderBalanceAfter, &t.Status,
		&t.CreatedAt, &t.ReversedAt, &t.SenderBalanceAfterReversal)
	return t, mapNoRows(err)
}
