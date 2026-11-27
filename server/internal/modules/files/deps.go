package files

import (
	"context"

	"github.com/google/uuid"
)

// TxRunner executes fn inside a database transaction propagated via ctx.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// ActorResolver resolves user usernames without importing the identity module.
type ActorResolver interface {
	GetUsername(ctx context.Context, userID uuid.UUID) (string, error)
	ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error)
}

// AvatarRefChecker verifies whether a file is still referenced by active user avatars.
type AvatarRefChecker interface {
	IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error)
}

// AvatarCallerValidator verifies that an avatar upload caller is an active, non-deleted user
// operating from an unrevoked, unexpired session.
type AvatarCallerValidator interface {
	EnsureActiveAvatarCaller(ctx context.Context, userID, sessionID uuid.UUID) error
}

// AuditEntry is the minimal audit payload produced by the files module.
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

// AuditRecorder appends audit events without importing the audit module.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}
