package repository

import (
	"context"

	"github.com/google/uuid"

	"wallet-service/internal/models"
)

func (s *Store) CreateUser(ctx context.Context, username, passwordHash string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx,
		`INSERT INTO users (username, password_hash) VALUES ($1, $2)
		 RETURNING id, username, password_hash, created_at`,
		username, passwordHash,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if isUniqueViolation(err) {
		return models.User{}, ErrDuplicate
	}
	return u, err
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE username = $1`,
		username,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	return u, mapNoRows(err)
}

func (s *Store) GetUserByID(ctx context.Context, id uuid.UUID) (models.User, error) {
	var u models.User
	err := s.pool.QueryRow(ctx,
		`SELECT id, username, password_hash, created_at FROM users WHERE id = $1`,
		id,
	).Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	return u, mapNoRows(err)
}
