package identity

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrInvalidAvatarFile indicates the requested avatar file does not exist, is not ready,
// is not an avatar image, or is not owned by the caller.
var ErrInvalidAvatarFile = errors.New("identity: invalid avatar file")

// PasswordHasher hashes and verifies user passwords.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (matched bool, needsRehash bool, err error)
}

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

// UserSession is the safe session projection consumed by identity without importing auth.
type UserSession struct {
	ID         uuid.UUID
	IPAddress  string
	UserAgent  string
	IsCurrent  bool
	LastUsedAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// SessionManager lists, validates, and revokes active user sessions owned by the auth module.
type SessionManager interface {
	RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error
	ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]UserSession, error)
	IsSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	RevokeUserSession(ctx context.Context, userID, sessionID uuid.UUID) (bool, error)
	RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error)
}

// AvatarFileManager validates/locks avatar files and mints short-lived inline URLs owned by the files module.
type AvatarFileManager interface {
	ValidateAndLockAvatarFile(ctx context.Context, fileID, ownerID uuid.UUID) error
	SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error)
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
