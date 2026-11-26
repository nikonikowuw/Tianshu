package identity

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/identity/internal/dbgen"
	"tianshu/internal/platform/database"
)

type store interface {
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, params createUserRecord) (User, error)
	GetUserForAuthByIdentifier(ctx context.Context, normalizedIdentifier string) (AuthCredentials, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	UpdateUserLastLogin(ctx context.Context, id uuid.UUID) error
	UpdateUserPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error
}

type createUserRecord struct {
	ID           uuid.UUID
	Username     string
	Email        string
	PasswordHash string
	DisplayName  string
	Status       string
	CreatedBy    uuid.NullUUID
	UpdatedBy    uuid.NullUUID
}

type pgStore struct {
	pool *pgxpool.Pool
}

var _ store = (*pgStore)(nil)

func newStore(pool *pgxpool.Pool) *pgStore {
	return &pgStore{pool: pool}
}

func (s *pgStore) queries(ctx context.Context) *dbgen.Queries {
	return dbgen.New(database.Executor(ctx, s.pool))
}

func (s *pgStore) CountUsers(ctx context.Context) (int64, error) {
	return s.queries(ctx).CountUsers(ctx)
}

func (s *pgStore) CreateUser(ctx context.Context, rec createUserRecord) (User, error) {
	row, err := s.queries(ctx).CreateUser(ctx, dbgen.CreateUserParams{
		ID:           rec.ID,
		Username:     rec.Username,
		Email:        rec.Email,
		PasswordHash: rec.PasswordHash,
		DisplayName:  rec.DisplayName,
		Status:       rec.Status,
		CreatedBy:    rec.CreatedBy,
		UpdatedBy:    rec.UpdatedBy,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case "uq_users_username_lower":
				return User{}, errUsernameConflict
			case "uq_users_email_lower":
				return User{}, errEmailConflict
			}
		}
		return User{}, err
	}

	return User{
		ID:          row.ID,
		Username:    row.Username,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      row.Status,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func (s *pgStore) GetUserForAuthByIdentifier(ctx context.Context, normalizedIdentifier string) (AuthCredentials, error) {
	row, err := s.queries(ctx).GetUserForAuthByIdentifier(ctx, normalizedIdentifier)
	if errors.Is(err, pgx.ErrNoRows) {
		return AuthCredentials{}, errUserNotFound
	}
	if err != nil {
		return AuthCredentials{}, err
	}
	return AuthCredentials{
		ID:           row.ID,
		Username:     row.Username,
		Email:        row.Email,
		DisplayName:  row.DisplayName,
		Status:       row.Status,
		PasswordHash: row.PasswordHash,
	}, nil
}

func (s *pgStore) GetUserByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := s.queries(ctx).GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return User{
		ID:          row.ID,
		Username:    row.Username,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      row.Status,
		CreatedAt:   row.CreatedAt.Time,
		UpdatedAt:   row.UpdatedAt.Time,
	}, nil
}

func (s *pgStore) UpdateUserLastLogin(ctx context.Context, id uuid.UUID) error {
	return s.queries(ctx).UpdateUserLastLogin(ctx, id)
}

func (s *pgStore) UpdateUserPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return s.queries(ctx).UpdateUserPasswordHash(ctx, dbgen.UpdateUserPasswordHashParams{
		ID:           id,
		PasswordHash: passwordHash,
	})
}
