package identity

import (
	"time"

	"github.com/google/uuid"

	"tianshu/internal/shared/pagination"
)

// User status constants matching the PostgreSQL CHECK constraint on users.status.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// User is the domain representation of a user account (never includes password_hash).
type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
	Status       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ActorRef is a lightweight user reference for createdBy/updatedBy projections.
type ActorRef struct {
	ID       uuid.UUID
	Username string
}

// UserDetail is the full administrative projection of a user account including roles and audit actors.
type UserDetail struct {
	ID           uuid.UUID
	Username     string
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
	Status       string
	LastLoginAt  *time.Time
	Roles        []RoleRef
	CreatedBy    *ActorRef
	UpdatedBy    *ActorRef
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// Profile is the self-service projection returned by /api/v1/profile endpoints.
type Profile struct {
	ID           uuid.UUID
	Username     string
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
	AvatarURL    *string
	Status       string
	LastLoginAt  *time.Time
	Roles        []RoleRef
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// AuthCredentials is an internal credential record used exclusively by the auth adapter.
// It must never be serialized to HTTP responses or logs.
type AuthCredentials struct {
	ID           uuid.UUID
	Username     string
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
	Status       string
	PasswordHash string
}

// userPasswordRecord is the internal credential snapshot used only during self-service password verification.
type userPasswordRecord struct {
	ID           uuid.UUID
	Username     string
	Status       string
	PasswordHash string
}

// CreateInitialAdminInput carries validated fields to create the first super administrator.
type CreateInitialAdminInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

// RequestMeta carries caller network metadata for audit records.
type RequestMeta struct {
	IPAddress string
	UserAgent string
}

// UserListFilter defines query constraints for paginated user listing.
type UserListFilter struct {
	Pagination pagination.Params
	Q          string
	Statuses   []string
	RoleIDs    []uuid.UUID
	Sort       string
	Order      string
}

// CreateUserInput defines the parameters for creating a user account.
type CreateUserInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
	Status      string
	RoleIDs     []uuid.UUID
	ActorID     *uuid.UUID
	Meta        RequestMeta
}

// UpdateUserInput defines partial update fields for a user account.
// Nil pointer fields indicate omitted inputs that preserve existing values.
type UpdateUserInput struct {
	UserID      uuid.UUID
	Email       *string
	DisplayName *string
	Status      *string
	Password    *string
	RoleIDs     *[]uuid.UUID
	ActorID     *uuid.UUID
	Meta        RequestMeta
}

// DeleteUserInput defines parameters for soft-deleting a user account.
type DeleteUserInput struct {
	UserID  uuid.UUID
	ActorID *uuid.UUID
	Meta    RequestMeta
}

// UpdateProfileInput defines partial self-service updates for the authenticated user.
type UpdateProfileInput struct {
	UserID           uuid.UUID
	CurrentSessionID uuid.UUID
	Email            *string
	DisplayName      *string
	AvatarFileID     *uuid.UUID
	ClearAvatar      bool
	Meta             RequestMeta
}

// ChangePasswordInput defines parameters for changing the authenticated user's password.
type ChangePasswordInput struct {
	UserID              uuid.UUID
	CurrentSessionID    uuid.UUID
	CurrentPassword     string
	NewPassword         string
	RevokeOtherSessions *bool
	Meta                RequestMeta
}

// ChangePasswordResult reports the side effects of a password change.
type ChangePasswordResult struct {
	RevokedSessions int64
}

// RevokeSessionInput defines parameters for revoking a single active session owned by the caller.
type RevokeSessionInput struct {
	UserID           uuid.UUID
	CurrentSessionID uuid.UUID
	SessionID        uuid.UUID
	Meta             RequestMeta
}

// RevokeOtherSessionsInput defines parameters for revoking all non-current active sessions owned by the caller.
type RevokeOtherSessionsInput struct {
	UserID           uuid.UUID
	CurrentSessionID uuid.UUID
	Meta             RequestMeta
}
