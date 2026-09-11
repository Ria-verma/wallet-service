package service

import (
	"context"

	"github.com/google/uuid"

	"wallet-service/internal/obs"
)

type WalletRepository interface {
	GetOrCreateWallet(ctx context.Context, userID uuid.UUID, seedPaise int64) (balance int64, created, raceLost bool, err error)
	GetWalletBalance(ctx context.Context, userID uuid.UUID) (int64, error)
}

type WalletService struct {
	wallets   WalletRepository
	seedPaise int64
	metrics   *obs.Metrics
}

func NewWalletService(wallets WalletRepository, seedPaise int64, metrics *obs.Metrics) *WalletService {
	return &WalletService{wallets: wallets, seedPaise: seedPaise, metrics: metrics}
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

func (w *WalletService) GetBalance(ctx context.Context, userID uuid.UUID) (int64, error) {
	balance, err := w.wallets.GetWalletBalance(ctx, userID)
	if err != nil {
		return 0, mapRepoNotFound(err, ErrNoWallet)
	}
	return balance, nil
}
