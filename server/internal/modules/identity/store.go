package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/identity/internal/dbgen"
	"tianshu/internal/platform/database"
)

type listUsersQuery struct {
	Q         string
	Statuses  []string
	FilterIDs bool
	IDs       []uuid.UUID
	Sort      string
	Order     string
	Offset    int32
	Limit     int32
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

type updateUserProfileRecord struct {
	ID          uuid.UUID
	Email       string
	DisplayName string
	Status      string
	UpdatedBy   *uuid.UUID
}

type updateOwnProfileRecord struct {
	ID           uuid.UUID
	Email        string
	DisplayName  string
	AvatarFileID *uuid.UUID
}

type store interface {
	CountUsers(ctx context.Context) (int64, error)
	CreateUser(ctx context.Context, params createUserRecord) (User, error)
	GetUserForAuthByIdentifier(ctx context.Context, normalizedIdentifier string) (AuthCredentials, error)
	GetUserByID(ctx context.Context, id uuid.UUID) (User, error)
	GetUserPasswordHashByID(ctx context.Context, id uuid.UUID) (userPasswordRecord, error)
	GetUserPasswordHashByIDForUpdate(ctx context.Context, id uuid.UUID) (userPasswordRecord, error)
	UpdateUserLastLogin(ctx context.Context, id uuid.UUID) error
	UpdateUserPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error

	CountUsersFiltered(ctx context.Context, q listUsersQuery) (int64, error)
	ListUsers(ctx context.Context, q listUsersQuery) ([]UserDetail, error)
	GetUserDetailByID(ctx context.Context, id uuid.UUID) (UserDetail, error)
	GetUserByIDForUpdate(ctx context.Context, id uuid.UUID) (User, error)
	UpdateUserProfile(ctx context.Context, rec updateUserProfileRecord) error
	UpdateOwnProfile(ctx context.Context, rec updateOwnProfileRecord) error
	UpdateOwnPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error
	UpdateUserPasswordByAdmin(ctx context.Context, id uuid.UUID, passwordHash string, updatedBy *uuid.UUID) error
	SoftDeleteUser(ctx context.Context, id uuid.UUID, updatedBy *uuid.UUID) error
	CountActiveUsersByIDs(ctx context.Context, ids []uuid.UUID) (int64, error)
	ListUsernamesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error)
	CountActiveUsersByAvatarFileID(ctx context.Context, fileID uuid.UUID) (int64, error)
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
		if mapped := mapUserUniqueConflict(err); mapped != nil {
			return User{}, mapped
		}
		return User{}, err
	}

	return User{
		ID:          row.ID,
		Username:    row.Username,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		Status:      row.Status,
		CreatedAt:   row.CreatedAt.Time.UTC(),
		UpdatedAt:   row.UpdatedAt.Time.UTC(),
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
		AvatarFileID: nullUUIDToPtr(row.AvatarFileID),
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
		ID:           row.ID,
		Username:     row.Username,
		Email:        row.Email,
		DisplayName:  row.DisplayName,
		AvatarFileID: nullUUIDToPtr(row.AvatarFileID),
		Status:       row.Status,
		CreatedAt:    row.CreatedAt.Time.UTC(),
		UpdatedAt:    row.UpdatedAt.Time.UTC(),
	}, nil
}

func (s *pgStore) GetUserPasswordHashByID(ctx context.Context, id uuid.UUID) (userPasswordRecord, error) {
	row, err := s.queries(ctx).GetUserPasswordHashByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return userPasswordRecord{}, errUserNotFound
	}
	if err != nil {
		return userPasswordRecord{}, err
	}
	return userPasswordRecord{
		ID:           row.ID,
		Username:     row.Username,
		Status:       row.Status,
		PasswordHash: row.PasswordHash,
	}, nil
}

func (s *pgStore) GetUserPasswordHashByIDForUpdate(ctx context.Context, id uuid.UUID) (userPasswordRecord, error) {
	row, err := s.queries(ctx).GetUserPasswordHashByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return userPasswordRecord{}, errUserNotFound
	}
	if err != nil {
		return userPasswordRecord{}, err
	}
	return userPasswordRecord{
		ID:           row.ID,
		Username:     row.Username,
		Status:       row.Status,
		PasswordHash: row.PasswordHash,
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

func (s *pgStore) CountUsersFiltered(ctx context.Context, q listUsersQuery) (int64, error) {
	return s.queries(ctx).CountUsersFiltered(ctx, dbgen.CountUsersFilteredParams{
		Q:         escapeLikePattern(q.Q),
		Statuses:  nonNilStrings(q.Statuses),
		FilterIds: q.FilterIDs,
		Ids:       nonNilUUIDs(q.IDs),
	})
}

func (s *pgStore) ListUsers(ctx context.Context, q listUsersQuery) ([]UserDetail, error) {
	rows, err := s.queries(ctx).ListUsers(ctx, dbgen.ListUsersParams{
		Q:          escapeLikePattern(q.Q),
		Statuses:   nonNilStrings(q.Statuses),
		FilterIds:  q.FilterIDs,
		Ids:        nonNilUUIDs(q.IDs),
		SortBy:     q.Sort,
		SortOrder:  q.Order,
		PageOffset: q.Offset,
		PageLimit:  q.Limit,
	})
	if err != nil {
		return nil, err
	}
	out := make([]UserDetail, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapDetailRow(
			r.ID, r.Username, r.Email, r.DisplayName, uuid.NullUUID{}, r.Status,
			r.LastLoginAt, r.CreatedBy, r.CreatedByUsername, r.UpdatedBy, r.UpdatedByUsername,
			r.CreatedAt, r.UpdatedAt,
		))
	}
	return out, nil
}

func (s *pgStore) GetUserDetailByID(ctx context.Context, id uuid.UUID) (UserDetail, error) {
	r, err := s.queries(ctx).GetUserDetailByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return UserDetail{}, errUserNotFound
	}
	if err != nil {
		return UserDetail{}, err
	}
	return mapDetailRow(
		r.ID, r.Username, r.Email, r.DisplayName, r.AvatarFileID, r.Status,
		r.LastLoginAt, r.CreatedBy, r.CreatedByUsername, r.UpdatedBy, r.UpdatedByUsername,
		r.CreatedAt, r.UpdatedAt,
	), nil
}

func (s *pgStore) GetUserByIDForUpdate(ctx context.Context, id uuid.UUID) (User, error) {
	r, err := s.queries(ctx).GetUserByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, errUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return User{
		ID:           r.ID,
		Username:     r.Username,
		Email:        r.Email,
		DisplayName:  r.DisplayName,
		AvatarFileID: nullUUIDToPtr(r.AvatarFileID),
		Status:       r.Status,
		CreatedAt:    r.CreatedAt.Time.UTC(),
		UpdatedAt:    r.UpdatedAt.Time.UTC(),
	}, nil
}

func (s *pgStore) UpdateUserProfile(ctx context.Context, rec updateUserProfileRecord) error {
	_, err := s.queries(ctx).UpdateUserProfile(ctx, dbgen.UpdateUserProfileParams{
		ID:          rec.ID,
		Email:       rec.Email,
		DisplayName: rec.DisplayName,
		Status:      rec.Status,
		UpdatedBy:   ptrToNullUUID(rec.UpdatedBy),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errUserNotFound
	}
	if err != nil {
		if mapped := mapUserUniqueConflict(err); mapped != nil {
			return mapped
		}
		return err
	}
	return nil
}

func (s *pgStore) UpdateOwnProfile(ctx context.Context, rec updateOwnProfileRecord) error {
	_, err := s.queries(ctx).UpdateOwnProfile(ctx, dbgen.UpdateOwnProfileParams{
		ID:           rec.ID,
		Email:        rec.Email,
		DisplayName:  rec.DisplayName,
		AvatarFileID: ptrToNullUUID(rec.AvatarFileID),
		UpdatedBy:    uuid.NullUUID{UUID: rec.ID, Valid: true},
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return errUserNotFound
	}
	if err != nil {
		if mapped := mapUserUniqueConflict(err); mapped != nil {
			return mapped
		}
		return err
	}
	return nil
}

func (s *pgStore) UpdateOwnPasswordHash(ctx context.Context, id uuid.UUID, passwordHash string) error {
	return s.queries(ctx).UpdateOwnPasswordHash(ctx, dbgen.UpdateOwnPasswordHashParams{
		ID:           id,
		PasswordHash: passwordHash,
		UpdatedBy:    uuid.NullUUID{UUID: id, Valid: true},
	})
}

func (s *pgStore) UpdateUserPasswordByAdmin(ctx context.Context, id uuid.UUID, passwordHash string, updatedBy *uuid.UUID) error {
	return s.queries(ctx).UpdateUserPasswordByAdmin(ctx, dbgen.UpdateUserPasswordByAdminParams{
		ID:           id,
		PasswordHash: passwordHash,
		UpdatedBy:    ptrToNullUUID(updatedBy),
	})
}

func (s *pgStore) SoftDeleteUser(ctx context.Context, id uuid.UUID, updatedBy *uuid.UUID) error {
	return s.queries(ctx).SoftDeleteUser(ctx, dbgen.SoftDeleteUserParams{
		ID:        id,
		UpdatedBy: ptrToNullUUID(updatedBy),
	})
}

func (s *pgStore) CountActiveUsersByIDs(ctx context.Context, ids []uuid.UUID) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	return s.queries(ctx).CountActiveUsersByIDs(ctx, ids)
}

func (s *pgStore) ListUsernamesByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	if len(ids) == 0 {
		return make(map[uuid.UUID]string), nil
	}
	rows, err := s.queries(ctx).ListUsernamesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]string, len(rows))
	for _, r := range rows {
		out[r.ID] = r.Username
	}
	return out, nil
}

func (s *pgStore) CountActiveUsersByAvatarFileID(ctx context.Context, fileID uuid.UUID) (int64, error) {
	return s.queries(ctx).CountActiveUsersByAvatarFileID(ctx, fileID)
}

func mapDetailRow(
	id uuid.UUID,
	username, email, displayName string,
	avatarFileID uuid.NullUUID,
	status string,
	lastLoginAt pgtype.Timestamptz,
	createdBy uuid.NullUUID,
	createdByUsername pgtype.Text,
	updatedBy uuid.NullUUID,
	updatedByUsername pgtype.Text,
	createdAt, updatedAt pgtype.Timestamptz,
) UserDetail {
	var lastLoginPtr *time.Time
	if lastLoginAt.Valid {
		t := lastLoginAt.Time.UTC()
		lastLoginPtr = &t
	}
	return UserDetail{
		ID:           id,
		Username:     username,
		Email:        email,
		DisplayName:  displayName,
		AvatarFileID: nullUUIDToPtr(avatarFileID),
		Status:       status,
		LastLoginAt:  lastLoginPtr,
		Roles:        make([]RoleRef, 0),
		CreatedBy:    toActorRef(createdBy, createdByUsername),
		UpdatedBy:    toActorRef(updatedBy, updatedByUsername),
		CreatedAt:    createdAt.Time.UTC(),
		UpdatedAt:    updatedAt.Time.UTC(),
	}
}

func nullUUIDToPtr(val uuid.NullUUID) *uuid.UUID {
	if !val.Valid || val.UUID == uuid.Nil {
		return nil
	}
	copied := val.UUID
	return &copied
}

func toActorRef(id uuid.NullUUID, username pgtype.Text) *ActorRef {
	if !id.Valid {
		return nil
	}
	return &ActorRef{
		ID:       id.UUID,
		Username: username.String,
	}
}

func ptrToNullUUID(val *uuid.UUID) uuid.NullUUID {
	if val == nil || *val == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *val, Valid: true}
}

func mapUserUniqueConflict(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		switch pgErr.ConstraintName {
		case "uq_users_username_lower":
			return errUsernameConflict
		case "uq_users_email_lower":
			return errEmailConflict
		}
	}
	return nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLikePattern(s string) string {
	return likeEscaper.Replace(s)
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}

func nonNilUUIDs(in []uuid.UUID) []uuid.UUID {
	if in == nil {
		return make([]uuid.UUID, 0)
	}
	return in
}
