package repository

import (
	"context"
	"time"

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

// GetWalletBalances returns the raw balance and the spendable part of it:
// available = balance minus incoming transfers still claimable by their
// senders. One statement, so the two numbers are from the same snapshot.
func (s *Store) GetWalletBalances(ctx context.Context, userID uuid.UUID, window time.Duration) (balance, available int64, err error) {
	err = s.pool.QueryRow(ctx,
		`SELECT w.balance_paise,
		        w.balance_paise - COALESCE((
		            SELECT SUM(t.amount_paise) FROM transfers t
		            WHERE t.recipient_id = w.user_id
		              AND t.status = 'completed'
		              AND t.created_at > now() - make_interval(secs => $2)
		        ), 0)
		 FROM wallets w WHERE w.user_id = $1`,
		userID, window.Seconds(),
	).Scan(&balance, &available)
	return balance, available, mapNoRows(err)
}
