package repository

import (
	"context"

	"github.com/google/uuid"
)

// GetOrCreateWallet is the race-free get-or-create for POST /accounts.
//
// The SELECT-first is purely for observability (so a lost race is
// distinguishable from "existed all along"); correctness comes from the
// INSERT ... ON CONFLICT DO NOTHING, which the database resolves atomically.
// raceLost is true when we observed the wallet absent and still lost the
// insert — i.e. a concurrent request created it in between.
func (s *Store) GetOrCreateWallet(ctx context.Context, userID uuid.UUID, seedPaise int64) (balance int64, created, raceLost bool, err error) {
	var exists bool
	if err = s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM wallets WHERE user_id = $1)`, userID,
	).Scan(&exists); err != nil {
		return 0, false, false, err
	}

	if !exists {
		tag, insErr := s.pool.Exec(ctx,
			`INSERT INTO wallets (user_id, balance_paise) VALUES ($1, $2)
			 ON CONFLICT (user_id) DO NOTHING`,
			userID, seedPaise,
		)
		if insErr != nil {
			return 0, false, false, insErr
		}
		created = tag.RowsAffected() == 1
		raceLost = !created
	}

	err = s.pool.QueryRow(ctx,
		`SELECT balance_paise FROM wallets WHERE user_id = $1`, userID,
	).Scan(&balance)
	return balance, created, raceLost, mapNoRows(err)
}

func (s *Store) GetWalletBalance(ctx context.Context, userID uuid.UUID) (int64, error) {
	var balance int64
	err := s.pool.QueryRow(ctx,
		`SELECT balance_paise FROM wallets WHERE user_id = $1`, userID,
	).Scan(&balance)
	return balance, mapNoRows(err)
}
