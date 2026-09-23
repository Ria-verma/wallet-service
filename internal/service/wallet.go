package service

import (
	"context"
	"time"

	"github.com/google/uuid"

	"wallet-service/internal/obs"
)

type WalletRepository interface {
	GetOrCreateWallet(ctx context.Context, userID uuid.UUID, seedPaise int64) (balance int64, created, raceLost bool, err error)
	GetWalletBalances(ctx context.Context, userID uuid.UUID, window time.Duration) (balance, available int64, err error)
}

type WalletService struct {
	wallets     WalletRepository
	seedPaise   int64
	claimWindow time.Duration
	metrics     *obs.Metrics
}

func NewWalletService(wallets WalletRepository, seedPaise int64, claimWindow time.Duration, metrics *obs.Metrics) *WalletService {
	return &WalletService{wallets: wallets, seedPaise: seedPaise, claimWindow: claimWindow, metrics: metrics}
}

func (w *WalletService) GetOrCreate(ctx context.Context, userID uuid.UUID) (int64, bool, error) {
	balance, created, raceLost, err := w.wallets.GetOrCreateWallet(ctx, userID, w.seedPaise)
	if err != nil {
		return 0, false, err
	}
	if raceLost {
		w.metrics.GetOrCreateRaceLost.Inc()
		obs.Log(ctx).Info("get_or_create_race_lost", "user_id", userID)
	}
	if created {
		obs.Log(ctx).Info("wallet_created", "user_id", userID, "seed_paise", w.seedPaise)
	}
	return balance, created, nil
}

// GetBalance returns the raw balance and the spendable part of it —
// available excludes incoming transfers still claimable by their senders.
func (w *WalletService) GetBalance(ctx context.Context, userID uuid.UUID) (balance, available int64, err error) {
	balance, available, err = w.wallets.GetWalletBalances(ctx, userID, w.claimWindow)
	if err != nil {
		return 0, 0, mapRepoNotFound(err, ErrNoWallet)
	}
	return balance, available, nil
}
