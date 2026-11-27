package auth

import (
	"time"

	"github.com/google/uuid"
)

// User represents the authenticated user profile returned in login/me responses.
type User struct {
	ID           uuid.UUID
	Username     string
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
	AvatarURL    *string
	Status       string
}

// ActiveSession is the safe read-only projection of an active user session (never exposes token hashes).
type ActiveSession struct {
	ID         uuid.UUID
	IPAddress  string
	UserAgent  string
	LastUsedAt time.Time
	CreatedAt  time.Time
	ExpiresAt  time.Time
}

// LoginInput carries credentials and client metadata for authentication.
type LoginInput struct {
	Identifier string
	Password   string
	ClientIP   string
	UserAgent  string
}

// RefreshInput carries refresh cookie value and client metadata.
type RefreshInput struct {
	RefreshToken string
	ClientIP     string
	UserAgent    string
}

// LogoutInput carries session identifiers and client metadata for logout auditing.
type LogoutInput struct {
	RefreshToken string
	AccessToken  string
	SessionID    uuid.UUID
	UserID       uuid.UUID
	ClientIP     string
	UserAgent    string
}

// LoginResult contains the issued access token, raw refresh token (for HttpOnly cookie), and session profile.
type LoginResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	User         User
	Roles        []string
	Permissions  []string
}

// RefreshResult contains the refreshed access token and optional rotated refresh token.
// When RotateCookie is false (concurrent refresh inside grace window), the caller keeps the existing cookie.
type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	RotateCookie bool
	ExpiresAt    time.Time
}

// MeResult contains the authenticated user's current profile, roles, and permissions.
type MeResult struct {
	User        User
	Roles       []string
	Permissions []string
}

// sessionRecord is the internal persistence representation of user_sessions.
type sessionRecord struct {
	ID               uuid.UUID
	UserID           uuid.UUID
	RefreshTokenHash string
	UserAgent        string
	IPAddress        string
	ExpiresAt        time.Time
	RevokedAt        *time.Time
	LastUsedAt       time.Time
	CreatedAt        time.Time
}

// refreshHistoryRecord is the internal persistence representation of refresh_token_history.
type refreshHistoryRecord struct {
	TokenHash  string
	SessionID  uuid.UUID
	ReplacedAt time.Time
	ExpiresAt  time.Time
}
