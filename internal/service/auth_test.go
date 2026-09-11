package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"wallet-service/internal/models"
	"wallet-service/internal/repository"
)

type fakeUsers struct {
	byName map[string]models.User
}

func newFakeUsers() *fakeUsers { return &fakeUsers{byName: map[string]models.User{}} }

func (f *fakeUsers) CreateUser(_ context.Context, username, hash string) (models.User, error) {
	if _, ok := f.byName[username]; ok {
		return models.User{}, repository.ErrDuplicate
	}
	u := models.User{ID: uuid.New(), Username: username, PasswordHash: hash}
	f.byName[username] = u
	return u, nil
}

func (f *fakeUsers) GetUserByUsername(_ context.Context, username string) (models.User, error) {
	u, ok := f.byName[username]
	if !ok {
		return models.User{}, repository.ErrNotFound
	}
	return u, nil
}

func newTestAuth() *AuthService {
	return NewAuthService(newFakeUsers(), "unit-test-secret-0123456789", time.Hour)
}

func TestSignupValidation(t *testing.T) {
	a := newTestAuth()
	cases := []struct{ username, password string }{
		{"ab", "password123"},           // username too short
		{"has spaces", "password123"},   // invalid chars
		{"validuser", "short"},          // password too short
		{"", "password123"},             // empty username
	}
	for _, c := range cases {
		var ve ValidationError
		if _, _, err := a.Signup(context.Background(), c.username, c.password); !errors.As(err, &ve) {
			t.Errorf("Signup(%q, %q): want ValidationError, got %v", c.username, c.password, err)
		}
	}
}

func TestSignupAndTokenRoundtrip(t *testing.T) {
	a := newTestAuth()
	user, token, err := a.Signup(context.Background(), "alice", "password123")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	got, err := a.VerifyToken(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != user.ID {
		t.Errorf("token subject = %s, want %s", got, user.ID)
	}
}

func TestSignupDuplicateUsername(t *testing.T) {
	a := newTestAuth()
	if _, _, err := a.Signup(context.Background(), "alice", "password123"); err != nil {
		t.Fatalf("first signup: %v", err)
	}
	if _, _, err := a.Signup(context.Background(), "alice", "password456"); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("want ErrUsernameTaken, got %v", err)
	}
}

func TestLoginWrongPassword(t *testing.T) {
	a := newTestAuth()
	a.Signup(context.Background(), "alice", "password123")
	if _, _, err := a.Login(context.Background(), "alice", "wrong-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("want ErrInvalidCredentials, got %v", err)
	}
	if _, _, err := a.Login(context.Background(), "nobody", "password123"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown user: want ErrInvalidCredentials, got %v", err)
	}
}

func TestVerifyTokenRejectsGarbageAndForeignSignature(t *testing.T) {
	a := newTestAuth()
	if _, err := a.VerifyToken("not-a-jwt"); err == nil {
		t.Error("garbage token accepted")
	}
	// Token signed with a different secret must be rejected.
	other := NewAuthService(newFakeUsers(), "another-secret-9876543210", time.Hour)
	_, token, err := other.Signup(context.Background(), "mallory", "password123")
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	if _, err := a.VerifyToken(token); err == nil {
		t.Error("token with foreign signature accepted")
	}
}
