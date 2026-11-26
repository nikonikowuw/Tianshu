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

// GetAuthCredentials looks up an active/disabled (non-deleted) user by normalized username or email.
func (s *Service) GetAuthCredentials(ctx context.Context, identifier string) (AuthCredentials, error) {
	normalized := strings.ToLower(strings.TrimSpace(identifier))
	creds, err := s.store.GetUserForAuthByIdentifier(ctx, normalized)
	if errors.Is(err, errUserNotFound) {
		return AuthCredentials{}, userNotFoundError()
	}
	if err != nil {
		return AuthCredentials{}, apperr.Internal(fmt.Errorf("lookup user credentials: %w", err))
	}
	return creds, nil
}

// GetByID returns a non-deleted user by ID.
func (s *Service) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	user, err := s.store.GetUserByID(ctx, id)
	if errors.Is(err, errUserNotFound) {
		return User{}, userNotFoundError()
	}
	if err != nil {
		return User{}, apperr.Internal(fmt.Errorf("get user %s: %w", id, err))
	}
	return user, nil
}

// RecordLogin updates last_login_at for id.
func (s *Service) RecordLogin(ctx context.Context, id uuid.UUID) error {
	if err := s.store.UpdateUserLastLogin(ctx, id); err != nil {
		return apperr.Internal(fmt.Errorf("update last login: %w", err))
	}
	return nil
}

// UpdatePasswordHash replaces the stored password hash (e.g. transparent Argon2id parameter upgrade).
func (s *Service) UpdatePasswordHash(ctx context.Context, id uuid.UUID, newHash string) error {
	if err := s.store.UpdateUserPasswordHash(ctx, id, newHash); err != nil {
		return apperr.Internal(fmt.Errorf("update password hash: %w", err))
	}
	return nil
}
