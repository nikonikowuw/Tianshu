package access

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access/internal/dbgen"
	"tianshu/internal/platform/database"
)

const (
	pgCodeForeignKeyViolation = "23503"
	pgCodeUniqueViolation     = "23505"
)

type listRolesQuery struct {
	Q                  string
	MatchedSystemCodes []string
	IsSystem           *bool
	Sort               string
	Order              string
	Offset             int32
	Limit              int32
}

type createRoleRecord struct {
	ID          uuid.UUID
	Code        string
	Name        string
	Description string
	ActorID     *uuid.UUID
}

type updateRoleRecord struct {
	ID          uuid.UUID
	Name        string
	Description string
	ActorID     *uuid.UUID
}

// store defines the private persistence boundary owned by the access module.
type store interface {
	UpsertPermission(ctx context.Context, def PermissionDef) error
	EnsureSystemRole(ctx context.Context, id uuid.UUID, def SystemRoleDef) (uuid.UUID, error)
	GrantRolePermission(ctx context.Context, roleID uuid.UUID, permCode string) error
	GetRoleIDByCode(ctx context.Context, code string) (uuid.UUID, error)
	AssignUserRole(ctx context.Context, userID, roleID uuid.UUID) error
	ListUserRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error)
	ListUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error)
	ListRoleUserIDs(ctx context.Context, roleID uuid.UUID) ([]uuid.UUID, error)

	CountRoles(ctx context.Context, q listRolesQuery) (int64, error)
	ListRoles(ctx context.Context, q listRolesQuery) ([]Role, error)
	GetRoleByID(ctx context.Context, id uuid.UUID) (Role, error)
	GetRoleByIDForUpdate(ctx context.Context, id uuid.UUID) (Role, error)
	ListPermissionCodesByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) (map[uuid.UUID][]string, error)
	CreateRole(ctx context.Context, rec createRoleRecord) (Role, error)
	UpdateRole(ctx context.Context, rec updateRoleRecord) (Role, error)
	DeleteRolePermissions(ctx context.Context, roleID uuid.UUID) error
	CountRoleUsers(ctx context.Context, roleID uuid.UUID) (int64, error)
	DeleteRole(ctx context.Context, id uuid.UUID) error

	ListRoleOptions(ctx context.Context) ([]RoleRef, error)
	ListRolesByIDs(ctx context.Context, ids []uuid.UUID) ([]RoleRef, error)
	ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error)
	DeleteUserRoles(ctx context.Context, userID uuid.UUID) error
	ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error)
	ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error)
}

// TxRunner executes fn inside a database transaction propagated via ctx.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
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

func (s *pgStore) UpsertPermission(ctx context.Context, def PermissionDef) error {
	return s.queries(ctx).UpsertPermission(ctx, dbgen.UpsertPermissionParams{
		Code:           def.Code,
		Module:         def.Module,
		NameKey:        def.NameKey,
		DescriptionKey: def.DescriptionKey,
	})
}

func (s *pgStore) EnsureSystemRole(ctx context.Context, id uuid.UUID, def SystemRoleDef) (uuid.UUID, error) {
	return s.queries(ctx).EnsureSystemRole(ctx, dbgen.EnsureSystemRoleParams{
		ID:             id,
		Code:           def.Code,
		NameKey:        pgtype.Text{String: def.NameKey, Valid: def.NameKey != ""},
		DescriptionKey: pgtype.Text{String: def.DescriptionKey, Valid: def.DescriptionKey != ""},
	})
}

func (s *pgStore) GrantRolePermission(ctx context.Context, roleID uuid.UUID, permCode string) error {
	return s.queries(ctx).GrantRolePermission(ctx, dbgen.GrantRolePermissionParams{
		RoleID:         roleID,
		PermissionCode: permCode,
	})
}

func (s *pgStore) GetRoleIDByCode(ctx context.Context, code string) (uuid.UUID, error) {
	id, err := s.queries(ctx).GetRoleIDByCode(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, errRoleNotFound
	}
	return id, err
}

func (s *pgStore) AssignUserRole(ctx context.Context, userID, roleID uuid.UUID) error {
	err := s.queries(ctx).AssignUserRole(ctx, dbgen.AssignUserRoleParams{
		UserID: userID,
		RoleID: roleID,
	})
	if isForeignKeyViolation(err, "user_roles_role_id_fkey") {
		return errRoleNotFound
	}
	return err
}

func (s *pgStore) ListUserRoleCodes(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.queries(ctx).ListUserRoleCodes(ctx, userID)
}

func (s *pgStore) ListUserPermissions(ctx context.Context, userID uuid.UUID) ([]string, error) {
	return s.queries(ctx).ListUserPermissions(ctx, userID)
}

func (s *pgStore) ListRoleUserIDs(ctx context.Context, roleID uuid.UUID) ([]uuid.UUID, error) {
	return s.queries(ctx).ListRoleUserIDs(ctx, roleID)
}

func (s *pgStore) CountRoles(ctx context.Context, q listRolesQuery) (int64, error) {
	hasIsSystem, isSystem := unpackOptionalBool(q.IsSystem)
	return s.queries(ctx).CountRoles(ctx, dbgen.CountRolesParams{
		Q:                  escapeLikePattern(q.Q),
		MatchedSystemCodes: nonNilStrings(q.MatchedSystemCodes),
		HasIsSystem:        hasIsSystem,
		IsSystem:           isSystem,
	})
}

func (s *pgStore) ListRoles(ctx context.Context, q listRolesQuery) ([]Role, error) {
	hasIsSystem, isSystem := unpackOptionalBool(q.IsSystem)
	rows, err := s.queries(ctx).ListRoles(ctx, dbgen.ListRolesParams{
		Q:                  escapeLikePattern(q.Q),
		MatchedSystemCodes: nonNilStrings(q.MatchedSystemCodes),
		HasIsSystem:        hasIsSystem,
		IsSystem:           isSystem,
		SortBy:             q.Sort,
		SortOrder:          q.Order,
		PageOffset:         q.Offset,
		PageLimit:          q.Limit,
	})
	if err != nil {
		return nil, err
	}
	roles := make([]Role, 0, len(rows))
	for _, row := range rows {
		roles = append(roles, Role{
			ID:              row.ID,
			Code:            row.Code,
			Name:            row.Name,
			Description:     row.Description,
			NameKey:         row.NameKey.String,
			DescriptionKey:  row.DescriptionKey.String,
			IsSystem:        row.IsSystem,
			UserCount:       row.UserCount,
			PermissionCount: row.PermissionCount,
			Permissions:     make([]string, 0),
			CreatedBy:       nullUUIDToPtr(row.CreatedBy),
			UpdatedBy:       nullUUIDToPtr(row.UpdatedBy),
			CreatedAt:       row.CreatedAt.Time.UTC(),
			UpdatedAt:       row.UpdatedAt.Time.UTC(),
		})
	}
	return roles, nil
}

func (s *pgStore) GetRoleByID(ctx context.Context, id uuid.UUID) (Role, error) {
	row, err := s.queries(ctx).GetRoleByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, errRoleNotFound
	}
	if err != nil {
		return Role{}, err
	}
	return Role{
		ID:              row.ID,
		Code:            row.Code,
		Name:            row.Name,
		Description:     row.Description,
		NameKey:         row.NameKey.String,
		DescriptionKey:  row.DescriptionKey.String,
		IsSystem:        row.IsSystem,
		UserCount:       row.UserCount,
		PermissionCount: row.PermissionCount,
		Permissions:     make([]string, 0),
		CreatedBy:       nullUUIDToPtr(row.CreatedBy),
		UpdatedBy:       nullUUIDToPtr(row.UpdatedBy),
		CreatedAt:       row.CreatedAt.Time.UTC(),
		UpdatedAt:       row.UpdatedAt.Time.UTC(),
	}, nil
}

func (s *pgStore) GetRoleByIDForUpdate(ctx context.Context, id uuid.UUID) (Role, error) {
	row, err := s.queries(ctx).GetRoleByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, errRoleNotFound
	}
	if err != nil {
		return Role{}, err
	}
	return mapDBRole(row), nil
}

func (s *pgStore) ListPermissionCodesByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	result := make(map[uuid.UUID][]string, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	rows, err := s.queries(ctx).ListPermissionCodesByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.RoleID] = append(result[row.RoleID], row.PermissionCode)
	}
	return result, nil
}

func (s *pgStore) CreateRole(ctx context.Context, rec createRoleRecord) (Role, error) {
	row, err := s.queries(ctx).CreateRole(ctx, dbgen.CreateRoleParams{
		ID:          rec.ID,
		Code:        rec.Code,
		Name:        rec.Name,
		Description: rec.Description,
		CreatedBy:   ptrToNullUUID(rec.ActorID),
		UpdatedBy:   ptrToNullUUID(rec.ActorID),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Role{}, errRoleCodeConflict
		}
		return Role{}, err
	}
	return mapDBRole(row), nil
}

func (s *pgStore) UpdateRole(ctx context.Context, rec updateRoleRecord) (Role, error) {
	row, err := s.queries(ctx).UpdateRole(ctx, dbgen.UpdateRoleParams{
		ID:          rec.ID,
		Name:        rec.Name,
		Description: rec.Description,
		UpdatedBy:   ptrToNullUUID(rec.ActorID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Role{}, errRoleNotFound
	}
	if err != nil {
		return Role{}, err
	}
	return mapDBRole(row), nil
}

func (s *pgStore) DeleteRolePermissions(ctx context.Context, roleID uuid.UUID) error {
	return s.queries(ctx).DeleteRolePermissions(ctx, roleID)
}

func (s *pgStore) CountRoleUsers(ctx context.Context, roleID uuid.UUID) (int64, error) {
	return s.queries(ctx).CountRoleUsers(ctx, roleID)
}

func (s *pgStore) DeleteRole(ctx context.Context, id uuid.UUID) error {
	return s.queries(ctx).DeleteRole(ctx, id)
}

func (s *pgStore) ListRoleOptions(ctx context.Context) ([]RoleRef, error) {
	rows, err := s.queries(ctx).ListRoleOptions(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RoleRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey.String,
			IsSystem: r.IsSystem,
		})
	}
	return out, nil
}

func (s *pgStore) ListRolesByIDs(ctx context.Context, ids []uuid.UUID) ([]RoleRef, error) {
	if len(ids) == 0 {
		return make([]RoleRef, 0), nil
	}
	rows, err := s.queries(ctx).ListRolesByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := make([]RoleRef, 0, len(rows))
	for _, r := range rows {
		out = append(out, RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey.String,
			IsSystem: r.IsSystem,
		})
	}
	return out, nil
}

func (s *pgStore) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error) {
	result := make(map[uuid.UUID][]RoleRef, len(userIDs))
	if len(userIDs) == 0 {
		return result, nil
	}
	rows, err := s.queries(ctx).ListRolesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.UserID] = append(result[r.UserID], RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey.String,
			IsSystem: r.IsSystem,
		})
	}
	return result, nil
}

func (s *pgStore) DeleteUserRoles(ctx context.Context, userID uuid.UUID) error {
	return s.queries(ctx).DeleteUserRoles(ctx, userID)
}

func (s *pgStore) ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(roleIDs) == 0 {
		return make([]uuid.UUID, 0), nil
	}
	return s.queries(ctx).ListUserIDsByRoleIDs(ctx, roleIDs)
}

func (s *pgStore) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	return s.queries(ctx).ListUserIDsByRoleCode(ctx, code)
}

func mapDBRole(row dbgen.Role) Role {
	return Role{
		ID:             row.ID,
		Code:           row.Code,
		Name:           row.Name,
		Description:    row.Description,
		NameKey:        row.NameKey.String,
		DescriptionKey: row.DescriptionKey.String,
		IsSystem:       row.IsSystem,
		Permissions:    make([]string, 0),
		CreatedBy:      nullUUIDToPtr(row.CreatedBy),
		UpdatedBy:      nullUUIDToPtr(row.UpdatedBy),
		CreatedAt:      row.CreatedAt.Time.UTC(),
		UpdatedAt:      row.UpdatedAt.Time.UTC(),
	}
}

func nullUUIDToPtr(val uuid.NullUUID) *uuid.UUID {
	if !val.Valid {
		return nil
	}
	id := val.UUID
	return &id
}

func ptrToNullUUID(val *uuid.UUID) uuid.NullUUID {
	if val == nil || *val == uuid.Nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *val, Valid: true}
}

func unpackOptionalBool(val *bool) (bool, bool) {
	if val == nil {
		return false, false
	}
	return true, *val
}

func nonNilStrings(in []string) []string {
	if in == nil {
		return make([]string, 0)
	}
	return in
}

func escapeLikePattern(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgCodeUniqueViolation
}

func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgCodeForeignKeyViolation && pgErr.ConstraintName == constraint
}
