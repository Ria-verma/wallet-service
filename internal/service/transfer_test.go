package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wallet-service/internal/models"
	"wallet-service/internal/obs"
	"wallet-service/internal/repository"
)

// stubStore fails the test if the transfer path reaches persistence —
// used to prove validation short-circuits before any database work.
type stubStore struct{ t *testing.T }

func (s stubStore) WithTx(context.Context, func(tx repository.Tx) error) error {
	s.t.Fatal("WithTx called before validation passed")
	return nil
}
func (s stubStore) GetTransferBySenderKey(context.Context, uuid.UUID, string) (models.Transfer, error) {
	s.t.Fatal("GetTransferBySenderKey called before validation passed")
	return models.Transfer{}, nil
}
func (s stubStore) GetTransferByID(context.Context, uuid.UUID) (models.Transfer, error) {
	s.t.Fatal("GetTransferByID called before validation passed")
	return models.Transfer{}, nil
}
func (s stubStore) GetUserByUsername(context.Context, string) (models.User, error) {
	s.t.Fatal("GetUserByUsername called before validation passed")
	return models.User{}, nil
}

func TestTransferValidationShortCircuits(t *testing.T) {
	svc := NewTransferService(stubStore{t}, 100000, 24*time.Hour, obs.NewMetrics())
	sender := uuid.New()

	cases := []struct {
		name   string
		amount int64
		key    string
	}{
		{"zero amount", 0, "key-1"},
		{"negative amount", -50, "key-1"},
		{"missing key", 100, ""},
		{"oversized key", 100, string(make([]byte, 200))},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var ve ValidationError
			_, err := svc.Execute(context.Background(), sender, "bob", c.amount, c.key)
			if !errors.As(err, &ve) {
				t.Errorf("want ValidationError, got %v", err)
			}
		})
	}
}

// claimStore serves one transfer read-only and fails the test if the claim
// path reaches the transaction — used to prove authorization and replay
// short-circuit before any locking.
type claimStore struct {
	stubStore
	tr models.Transfer
}

func (s claimStore) GetTransferByID(context.Context, uuid.UUID) (models.Transfer, error) {
	return s.tr, nil
}

func TestClaimBackOnlySenderMayClaim(t *testing.T) {
	sender, recipient := uuid.New(), uuid.New()
	tr := models.Transfer{ID: uuid.New(), SenderID: sender, RecipientID: recipient,
		AmountPaise: 500, Status: models.TransferCompleted}
	svc := NewTransferService(claimStore{stubStore{t}, tr}, 100000, 24*time.Hour, obs.NewMetrics())

	for _, caller := range []uuid.UUID{recipient, uuid.New()} {
		if _, err := svc.ClaimBack(context.Background(), caller, tr.ID); !errors.Is(err, ErrNotFound) {
			t.Errorf("caller %s: want ErrNotFound (no existence leak), got %v", caller, err)
		}
	}
}

func TestClaimBackReplaysReversedWithoutTx(t *testing.T) {
	sender := uuid.New()
	reversedAt := time.Now().Add(-time.Hour)
	balanceAfter := int64(100500)
	tr := models.Transfer{ID: uuid.New(), SenderID: sender, RecipientID: uuid.New(),
		AmountPaise: 500, Status: models.TransferReversed,
		ReversedAt: &reversedAt, SenderBalanceAfterReversal: &balanceAfter}
	svc := NewTransferService(claimStore{stubStore{t}, tr}, 100000, 24*time.Hour, obs.NewMetrics())

	result, err := svc.ClaimBack(context.Background(), sender, tr.ID)
	if err != nil {
		t.Fatalf("replay claim: %v", err)
	}
	if !result.Replayed {
		t.Error("second claim must be marked Replayed")
	}
	if result.NewBalance != balanceAfter || !result.ReversedAt.Equal(reversedAt) {
		t.Errorf("replay must return the stored outcome, got %+v", result)
	}
}

func TestHashBody(t *testing.T) {
	if hashBody("bob", 100) != hashBody("bob", 100) {
		t.Error("same body must hash identically")
	}
	if hashBody("bob", 100) == hashBody("bob", 101) {
		t.Error("different amount must change the hash")
	}
	if hashBody("bob", 100) == hashBody("carol", 100) {
		t.Error("different recipient must change the hash")
	}
	// Delimiter matters: ("ab", 1) must differ from ("a", 11) etc.
	if hashBody("ab1", 1) == hashBody("ab", 11) {
		t.Error("ambiguous concatenation in body hash")
	}
}
