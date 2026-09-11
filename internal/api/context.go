package api

import (
	"context"

	"github.com/google/uuid"
)

type userIDKey struct{}

func withUserID(ctx context.Context, id uuid.UUID) context.Context {
	return context.WithValue(ctx, userIDKey{}, id)
}

// UserID returns the authenticated caller, as established by the auth
// middleware from the verified JWT. It is the only identity source
// handlers are allowed to use.
func UserID(ctx context.Context) uuid.UUID {
	id, _ := ctx.Value(userIDKey{}).(uuid.UUID)
	return id
}
