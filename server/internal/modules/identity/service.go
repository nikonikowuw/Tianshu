package identity

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

// PasswordHasher hashes plaintext passwords for user storage.
type PasswordHasher interface {
	Hash(password string) (string, error)
}

// Service owns user lifecycle persistence and invariants.
type Service struct {
	store  store
	hasher PasswordHasher
}

// NewService creates an identity Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool, hasher PasswordHasher) *Service {
	return &Service{
		store:  newStore(pool),
		hasher: hasher,
	}
}

// CountUsers returns the number of non-deleted user accounts.
func (s *Service) CountUsers(ctx context.Context) (int64, error) {
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return 0, apperr.Internal(fmt.Errorf("count users: %w", err))
	}
	return count, nil
}

// CreateInitialAdmin creates the first active administrator account using UUIDv7 and Argon2id.
// Callers are expected to coordinate transaction and advisory locking at the orchestration boundary.
func (s *Service) CreateInitialAdmin(ctx context.Context, input CreateInitialAdminInput) (User, error) {
	username := strings.ToLower(strings.TrimSpace(input.Username))
	email := strings.ToLower(strings.TrimSpace(input.Email))
	displayName := strings.TrimSpace(input.DisplayName)
	if len(username) < 3 || len(username) > 64 {
		return User{}, apperr.Validation([]apperr.FieldError{{
			Field:   "username",
			Message: "username must be between 3 and 64 characters",
		}})
	}
	if displayName == "" {
		displayName = username
	}

	hash, err := s.hasher.Hash(input.Password)
	if errors.Is(err, security.ErrInvalidPasswordLength) {
		return User{}, invalidPasswordError()
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("hash initial admin password: %w", err))
	}

	userID, err := uuid.NewV7()
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("generate user uuid: %w", err))
	}

	user, err := s.store.CreateUser(ctx, createUserRecord{
		ID:           userID,
		Username:     username,
		Email:        email,
		PasswordHash: hash,
		DisplayName:  displayName,
		Status:       StatusActive,
	})
	if errors.Is(err, errUsernameConflict) {
		return User{}, usernameExistsError()
	}
	if errors.Is(err, errEmailConflict) {
		return User{}, emailExistsError()
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("create initial admin: %w", err))
	}
	return user, nil
}
