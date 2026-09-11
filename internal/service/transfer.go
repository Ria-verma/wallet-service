package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"wallet-service/internal/models"
	"wallet-service/internal/obs"
	"wallet-service/internal/repository"
)

type TransferStore interface {
	WithTx(ctx context.Context, fn func(tx repository.Tx) error) error
	GetTransferBySenderKey(ctx context.Context, senderID uuid.UUID, key string) (models.Transfer, error)
	GetTransferByID(ctx context.Context, id uuid.UUID) (models.Transfer, error)
	GetUserByUsername(ctx context.Context, username string) (models.User, error)
}

type TransferService struct {
	store     TransferStore
	seedPaise int64
	metrics   *obs.Metrics
}

func NewTransferService(store TransferStore, seedPaise int64, metrics *obs.Metrics) *TransferService {
	return &TransferService{store: store, seedPaise: seedPaise, metrics: metrics}
}

type TransferResult struct {
	TransferID uuid.UUID
	NewBalance int64
	Replayed   bool
}

// errKeyExists aborts the transaction when the idempotency key is already
// claimed; the caller then resolves replay-vs-conflict outside the tx.
var errKeyExists = errors.New("idempotency key already claimed")

// Execute moves amountPaise from sender to the user named toUsername.
//
// Everything that must hold together — recipient wallet get-or-create,
// idempotency-key claim, funds check, debit and credit — happens in ONE
// database transaction, so the key exists if and only if the money moved.
// Wallet rows are always acquired in ascending user_id order (both the
// upserts and the FOR UPDATE locks), which makes concurrent A→B and B→A
// transfers deadlock-free.
func (t *TransferService) Execute(ctx context.Context, senderID uuid.UUID, toUsername string, amountPaise int64, key string) (TransferResult, error) {
	if amountPaise <= 0 {
		t.metrics.TransfersRejected.WithLabelValues("invalid_amount").Inc()
		return TransferResult{}, Invalid("amount_paise must be a positive integer")
	}
	if key == "" || len(key) > 128 {
		return TransferResult{}, Invalid("idempotency_key is required (1-128 chars)")
	}

	recipient, err := t.store.GetUserByUsername(ctx, toUsername)
	if errors.Is(err, repository.ErrNotFound) {
		t.metrics.TransfersRejected.WithLabelValues("unknown_recipient").Inc()
		return TransferResult{}, ErrUnknownRecipient
	}
	if err != nil {
		return TransferResult{}, err
	}
	if recipient.ID == senderID {
		t.metrics.TransfersRejected.WithLabelValues("self_transfer").Inc()
		return TransferResult{}, ErrSelfTransfer
	}

	bodyHash := hashBody(toUsername, amountPaise)

	var result TransferResult
	err = t.store.WithTx(ctx, func(tx repository.Tx) error {
		// Get-or-create both wallets, then lock both, always in ascending
		// user_id order.
		ids := []uuid.UUID{senderID, recipient.ID}
		sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })

		for _, id := range ids {
			exists, err := tx.WalletExists(ctx, id)
			if err != nil {
				return err
			}
			if exists {
				continue
			}
			created, err := tx.InsertWallet(ctx, id, t.seedPaise)
			if err != nil {
				return err
			}
			if !created {
				// Observed absent, then the insert conflicted: a concurrent
				// request created this wallet first. Correctness is unharmed
				// (ON CONFLICT DO NOTHING); we just record that it happened.
				t.metrics.GetOrCreateRaceLost.Inc()
				obs.Log(ctx).Info("get_or_create_race_lost", "user_id", id)
			}
		}

		balances := make(map[uuid.UUID]int64, 2)
		for _, id := range ids {
			bal, err := tx.LockWallet(ctx, id)
			if err != nil {
				return err
			}
			balances[id] = bal
		}

		// Claim the idempotency key BEFORE the funds check: a replay of an
		// already-applied transfer must return its original outcome even if
		// the sender's balance has since dropped.
		newBalance := balances[senderID] - amountPaise
		transferID, inserted, err := tx.InsertTransfer(ctx, models.Transfer{
			SenderID:           senderID,
			RecipientID:        recipient.ID,
			AmountPaise:        amountPaise,
			IdempotencyKey:     key,
			BodyHash:           bodyHash,
			SenderBalanceAfter: newBalance,
		})
		if err != nil {
			return err
		}
		if !inserted {
			return errKeyExists
		}

		if newBalance < 0 {
			// Rolling back also discards the transfer row above, so a
			// rejection never claims the idempotency key.
			return ErrInsufficientFunds
		}

		if err := tx.UpdateWalletBalance(ctx, senderID, newBalance); err != nil {
			return err
		}
		if err := tx.UpdateWalletBalance(ctx, recipient.ID, balances[recipient.ID]+amountPaise); err != nil {
			return err
		}

		result = TransferResult{TransferID: transferID, NewBalance: newBalance}
		return nil
	})

	switch {
	case err == nil:
		t.metrics.TransfersApplied.Inc()
		obs.Log(ctx).Info("transfer_applied",
			"transfer_id", result.TransferID, "sender_id", senderID,
			"recipient_id", recipient.ID, "amount_paise", amountPaise)
		return result, nil

	case errors.Is(err, errKeyExists):
		return t.resolveExistingKey(ctx, senderID, key, bodyHash)

	case errors.Is(err, ErrInsufficientFunds):
		t.metrics.TransfersRejected.WithLabelValues("insufficient_funds").Inc()
		obs.Log(ctx).Info("insufficient_funds_rejected",
			"sender_id", senderID, "amount_paise", amountPaise)
		return TransferResult{}, err

	default:
		return TransferResult{}, err
	}
}

// resolveExistingKey handles a claimed idempotency key after rollback:
// same body → replay the stored outcome; different body → conflict.
func (t *TransferService) resolveExistingKey(ctx context.Context, senderID uuid.UUID, key, bodyHash string) (TransferResult, error) {
	existing, err := t.store.GetTransferBySenderKey(ctx, senderID, key)
	if err != nil {
		return TransferResult{}, fmt.Errorf("load transfer for claimed key: %w", err)
	}
	if existing.BodyHash != bodyHash {
		t.metrics.TransfersRejected.WithLabelValues("idempotency_conflict").Inc()
		obs.Log(ctx).Warn("idempotency_conflict",
			"sender_id", senderID, "transfer_id", existing.ID)
		return TransferResult{}, ErrIdempotencyConflict
	}
	t.metrics.IdempotentReplays.Inc()
	obs.Log(ctx).Info("idempotent_replay",
		"sender_id", senderID, "transfer_id", existing.ID)
	return TransferResult{
		TransferID: existing.ID,
		NewBalance: existing.SenderBalanceAfter,
		Replayed:   true,
	}, nil
}

// GetByID returns a transfer only to its participants; anyone else gets
// not-found so transfer IDs don't leak existence.
func (t *TransferService) GetByID(ctx context.Context, callerID, transferID uuid.UUID) (models.Transfer, error) {
	tr, err := t.store.GetTransferByID(ctx, transferID)
	if err != nil {
		return models.Transfer{}, mapRepoNotFound(err, ErrNotFound)
	}
	if tr.SenderID != callerID && tr.RecipientID != callerID {
		return models.Transfer{}, ErrNotFound
	}
	return tr, nil
}

func hashBody(toUsername string, amountPaise int64) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s|%d", toUsername, amountPaise))
	return hex.EncodeToString(sum[:])
}

func mapRepoNotFound(err error, domainErr error) error {
	if errors.Is(err, repository.ErrNotFound) {
		return domainErr
	}
	return err
}
