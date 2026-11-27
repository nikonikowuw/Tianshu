package auth

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/security"
)

// UserCredentials holds internal credential fields supplied by the identity adapter.
type UserCredentials struct {
	User         User
	PasswordHash string
}

// UserAccess holds resolved roles and permissions supplied by the access adapter.
type UserAccess struct {
	Roles       []string
	Permissions []string
}

// IdentityProvider is the minimal dependency on identity.Service, wired via internal/app adapter.
type IdentityProvider interface {
	GetAuthCredentials(ctx context.Context, identifier string) (UserCredentials, error)
	GetByID(ctx context.Context, userID uuid.UUID) (User, error)
	RecordLogin(ctx context.Context, userID uuid.UUID) error
	UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error
}

// AccessProvider is the minimal dependency on access.Service, wired via internal/app adapter.
type AccessProvider interface {
	GetUserAccess(ctx context.Context, userID uuid.UUID) (UserAccess, error)
}

// PasswordVerifier abstracts Argon2id password verification, hashing, and timing-equalization.
type PasswordVerifier interface {
	Hash(password string) (string, error)
	Verify(password, encodedHash string) (matched bool, needsRehash bool, err error)
	VerifyDummy(password string)
}

// TxRunner executes fn inside a database transaction propagated via ctx.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// AuditEntry describes the minimal audit event emitted by auth.
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

// AuditRecorder appends audit events without coupling auth to the audit module.
type AuditRecorder interface {
	Record(ctx context.Context, entry AuditEntry) error
}

// AvatarURLSigner mints short-lived inline signed URLs for user avatars without coupling auth to files.
type AvatarURLSigner interface {
	SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error)
}

// ServiceDeps groups external collaborators injected into auth.Service at construction time.
type ServiceDeps struct {
	Identity     IdentityProvider
	Access       AccessProvider
	Hasher       PasswordVerifier
	JWT          *security.JWTManager
	Tx           TxRunner
	Cache        cache.Cache
	Audit        AuditRecorder
	AvatarSigner AvatarURLSigner
	Log          *slog.Logger
}
