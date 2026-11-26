package authctx

import (
	"context"

	"github.com/google/uuid"
)

type ctxKey struct{}

// Identity represents the authenticated subject propagated via request context.
// Roles and permissions are intentionally excluded so RBAC changes take effect immediately.
type Identity struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	Username  string
}

// WithIdentity attaches an authenticated Identity to ctx.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// IdentityFromContext extracts the authenticated Identity stored in ctx, if present.
func IdentityFromContext(ctx context.Context) (Identity, bool) {
	if ctx == nil {
		return Identity{}, false
	}
	id, ok := ctx.Value(ctxKey{}).(Identity)
	if !ok || id.UserID == uuid.Nil {
		return Identity{}, false
	}
	return id, true
}
