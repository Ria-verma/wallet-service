package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"wallet-service/internal/models"
	"wallet-service/internal/repository"
)

type UserRepository interface {
	CreateUser(ctx context.Context, username, passwordHash string) (models.User, error)
	GetUserByUsername(ctx context.Context, username string) (models.User, error)
}

type AuthService struct {
	users    UserRepository
	secret   []byte
	tokenTTL time.Duration
}

func NewAuthService(users UserRepository, secret string, tokenTTL time.Duration) *AuthService {
	return &AuthService{users: users, secret: []byte(secret), tokenTTL: tokenTTL}
}

var usernameRe = regexp.MustCompile(`^[a-zA-Z0-9_.-]{3,32}$`)

func (a *AuthService) Signup(ctx context.Context, username, password string) (models.User, string, error) {
	if !usernameRe.MatchString(username) {
		return models.User{}, "", Invalid("username must be 3-32 chars of letters, digits, '_', '.', '-'")
	}
	if len(password) < 8 || len(password) > 72 { // 72 = bcrypt input limit
		return models.User{}, "", Invalid("password must be 8-72 characters")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return models.User{}, "", fmt.Errorf("hash password: %w", err)
	}

	user, err := a.users.CreateUser(ctx, username, string(hash))
	if errors.Is(err, repository.ErrDuplicate) {
		return models.User{}, "", ErrUsernameTaken
	}
	if err != nil {
		return models.User{}, "", err
	}

	token, err := a.issueToken(user.ID)
	return user, token, err
}

func (a *AuthService) Login(ctx context.Context, username, password string) (models.User, string, error) {
	user, err := a.users.GetUserByUsername(ctx, username)
	if errors.Is(err, repository.ErrNotFound) {
		return models.User{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return models.User{}, "", err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return models.User{}, "", ErrInvalidCredentials
	}
	token, err := a.issueToken(user.ID)
	return user, token, err
}

func (a *AuthService) issueToken(userID uuid.UUID) (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   userID.String(),
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(a.tokenTTL)),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.secret)
}

// VerifyToken is the ONLY source of caller identity in the system — no
// header or body field is ever trusted for "who is calling".
func (a *AuthService) VerifyToken(tokenString string) (uuid.UUID, error) {
	token, err := jwt.ParseWithClaims(tokenString, &jwt.RegisteredClaims{},
		func(t *jwt.Token) (any, error) { return a.secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
	)
	if err != nil || !token.Valid {
		return uuid.Nil, ErrInvalidCredentials
	}
	sub, err := token.Claims.GetSubject()
	if err != nil {
		return uuid.Nil, ErrInvalidCredentials
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return uuid.Nil, ErrInvalidCredentials
	}
	return id, nil
}
