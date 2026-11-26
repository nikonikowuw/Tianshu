package identity

import (
	"time"

	"github.com/google/uuid"
)

// User status constants matching the PostgreSQL CHECK constraint on users.status.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// User is the domain representation of a user account (never includes password_hash).
type User struct {
	ID          uuid.UUID
	Username    string
	Email       string
	DisplayName string
	Status      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// CreateInitialAdminInput carries validated fields to create the first super administrator.
type CreateInitialAdminInput struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}
