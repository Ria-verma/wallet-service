package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Sentinel errors the service layer maps onto domain errors.
var (
	ErrNotFound  = errors.New("repository: not found")
	ErrDuplicate = errors.New("repository: duplicate")
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// WithTx runs fn inside a single database transaction. Any error (or panic)
// rolls the whole transaction back — this is what guarantees a transfer is
// all-or-nothing.
func (s *Store) WithTx(ctx context.Context, fn func(tx Tx) error) error {
	pgtx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer pgtx.Rollback(ctx) // no-op after successful commit

	if err := fn(Tx{tx: pgtx}); err != nil {
		return err
	}
	if err := pgtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func mapNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
