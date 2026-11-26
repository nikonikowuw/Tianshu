package identity

import (
	"context"

	"github.com/google/uuid"
)

// RoleRef is the lightweight role summary consumed by identity without importing access.
type RoleRef struct {
	ID       uuid.UUID
	Code     string
	Name     string
	NameKey  string
	IsSystem bool
}

// RoleManager abstracts role lookups and user-role assignments owned by the access module.
type RoleManager interface {
	ResolveRoles(ctx context.Context, ids []uuid.UUID) ([]RoleRef, error)
	ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error)
	ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error)
	ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error)
	ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error
}

// SessionRevoker revokes active user sessions owned by the auth module.
type SessionRevoker interface {
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
}

// TxRunner executes fn inside a database transaction propagated via ctx.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// AuditEntry is the minimal audit payload produced by the identity module.
type AuditEntry struct {
	ActorID       *uuid.UUID
	ActorUsername string
	Action        string
	Resource      string
	ResourceID    string
	Status        string
	IPAddress     string
	UserAgent     string
	Metadata      map[string]any
}

// AuditRecorder appends audit events without coupling identity to the audit module.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}
