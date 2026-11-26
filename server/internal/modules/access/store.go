package access

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access/internal/dbgen"
	"tianshu/internal/platform/database"
)

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
	return s.queries(ctx).AssignUserRole(ctx, dbgen.AssignUserRoleParams{
		UserID: userID,
		RoleID: roleID,
	})
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
