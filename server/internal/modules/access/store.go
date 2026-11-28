package access

import (
	"context"
	"errors"
	"slices"
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
	roleUserBatchLimit        = int32(1000)
)

type listRolesQuery struct {
	Q                  string
	MatchedSystemCodes []string
	SuperAdminName     string
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

type createMenuRecord struct {
	ID             uuid.UUID
	ParentID       *uuid.UUID
	Type           string
	Code           string
	Title          string
	Path           string
	Icon           string
	PermissionCode string
	SortOrder      int32
	Visible        bool
	Status         string
	ActorID        *uuid.UUID
}

type updateMenuRecord struct {
	ID             uuid.UUID
	ParentID       *uuid.UUID
	Type           string
	Title          string
	Path           string
	Icon           string
	PermissionCode string
	SortOrder      int32
	Visible        bool
	Status         string
	ActorID        *uuid.UUID
}

// store defines the private persistence boundary owned by the access module.
type store interface {
	UpsertPermission(ctx context.Context, def PermissionDef) error
	EnsureSystemRole(ctx context.Context, id uuid.UUID, def SystemRoleDef) (uuid.UUID, error)
	GrantRolePermission(ctx context.Context, roleID uuid.UUID, permCode string) error
	GetRoleIDByCode(ctx context.Context, code string) (uuid.UUID, error)
	AssignUserRole(ctx context.Context, userID, roleID uuid.UUID) error
	GetUserAccessData(ctx context.Context, userID uuid.UUID) (UserAccess, error)
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

	EnsureSystemMenu(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, def SystemMenuDef) (uuid.UUID, error)
	ListMenus(ctx context.Context) ([]Menu, error)
	GetMenuByID(ctx context.Context, id uuid.UUID) (Menu, error)
	GetMenuByIDForUpdate(ctx context.Context, id uuid.UUID) (Menu, error)
	CreateMenu(ctx context.Context, rec createMenuRecord) (Menu, error)
	UpdateMenu(ctx context.Context, rec updateMenuRecord) (Menu, error)
	UpdateMenuOrder(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, sortOrder int32, actorID *uuid.UUID) error
	CountMenuChildren(ctx context.Context, id uuid.UUID) (int64, error)
	DeleteMenu(ctx context.Context, id uuid.UUID) error
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
	err := s.queries(ctx).GrantRolePermission(ctx, dbgen.GrantRolePermissionParams{
		RoleID:         roleID,
		PermissionCode: permCode,
	})
	if isForeignKeyViolation(err, "role_permissions_role_id_fkey") {
		return errRoleNotFound
	}
	if isForeignKeyViolation(err, "role_permissions_permission_code_fkey") {
		return errInvalidPermission
	}
	return err
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
	if isForeignKeyViolation(err, "user_roles_user_id_fkey") {
		return errUserNotFound
	}
	return err
}

func (s *pgStore) GetUserAccessData(ctx context.Context, userID uuid.UUID) (UserAccess, error) {
	rows, err := s.queries(ctx).ListUserRolesAndPermissions(ctx, userID)
	if err != nil {
		return UserAccess{}, err
	}
	roleSet := make(map[string]struct{})
	permSet := make(map[string]struct{})
	for _, row := range rows {
		if row.RoleCode != "" {
			roleSet[row.RoleCode] = struct{}{}
		}
		if row.PermissionCode != "" {
			permSet[row.PermissionCode] = struct{}{}
		}
	}
	roles := make([]string, 0, len(roleSet))
	for code := range roleSet {
		roles = append(roles, code)
	}
	slices.Sort(roles)

	perms := make([]string, 0, len(permSet))
	for code := range permSet {
		perms = append(perms, code)
	}
	slices.Sort(perms)

	return UserAccess{
		Roles:       roles,
		Permissions: perms,
	}, nil
}

func (s *pgStore) ListRoleUserIDs(ctx context.Context, roleID uuid.UUID) ([]uuid.UUID, error) {
	out := make([]uuid.UUID, 0)
	afterID := uuid.Nil
	for {
		batch, err := s.queries(ctx).ListRoleUserIDsBatch(ctx, dbgen.ListRoleUserIDsBatchParams{
			RoleID:      roleID,
			AfterUserID: afterID,
			BatchLimit:  roleUserBatchLimit,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if int32(len(batch)) < roleUserBatchLimit {
			break
		}
		afterID = batch[len(batch)-1]
	}
	return out, nil
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
		SuperAdminName:     q.SuperAdminName,
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
			ID:             row.ID,
			Code:           row.Code,
			Name:           row.Name,
			Description:    row.Description,
			NameKey:        row.NameKey.String,
			DescriptionKey: row.DescriptionKey.String,
			IsSystem:       row.IsSystem,
			UserCount:      row.UserCount,
			Permissions:    make([]string, 0),
			CreatedBy:      nullUUIDToPtr(row.CreatedBy),
			UpdatedBy:      nullUUIDToPtr(row.UpdatedBy),
			CreatedAt:      row.CreatedAt.Time.UTC(),
			UpdatedAt:      row.UpdatedAt.Time.UTC(),
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
		ID:             row.ID,
		Code:           row.Code,
		Name:           row.Name,
		Description:    row.Description,
		NameKey:        row.NameKey.String,
		DescriptionKey: row.DescriptionKey.String,
		IsSystem:       row.IsSystem,
		UserCount:      row.UserCount,
		Permissions:    make([]string, 0),
		CreatedBy:      nullUUIDToPtr(row.CreatedBy),
		UpdatedBy:      nullUUIDToPtr(row.UpdatedBy),
		CreatedAt:      row.CreatedAt.Time.UTC(),
		UpdatedAt:      row.UpdatedAt.Time.UTC(),
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
		if isUniqueViolation(err, "roles_code_key") {
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
	ids, err := s.queries(ctx).ListUserIDsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, err
	}
	return nonNilUUIDs(ids), nil
}

func (s *pgStore) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	ids, err := s.queries(ctx).ListUserIDsByRoleCode(ctx, code)
	if err != nil {
		return nil, err
	}
	return nonNilUUIDs(ids), nil
}

func (s *pgStore) EnsureSystemMenu(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, def SystemMenuDef) (uuid.UUID, error) {
	return s.queries(ctx).EnsureSystemMenu(ctx, dbgen.EnsureSystemMenuParams{
		ID:             id,
		ParentID:       ptrToNullUUID(parentID),
		Type:           def.Type,
		Code:           def.Code,
		TitleKey:       pgtype.Text{String: def.TitleKey, Valid: def.TitleKey != ""},
		Path:           def.Path,
		Icon:           def.Icon,
		PermissionCode: pgtype.Text{String: def.PermissionCode, Valid: def.PermissionCode != ""},
		SortOrder:      def.SortOrder,
	})
}

func (s *pgStore) ListMenus(ctx context.Context) ([]Menu, error) {
	rows, err := s.queries(ctx).ListMenus(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Menu, 0, len(rows))
	for _, r := range rows {
		out = append(out, mapDBMenu(r))
	}
	return out, nil
}

func (s *pgStore) GetMenuByID(ctx context.Context, id uuid.UUID) (Menu, error) {
	r, err := s.queries(ctx).GetMenuByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Menu{}, errMenuNotFound
	}
	if err != nil {
		return Menu{}, err
	}
	return mapDBMenu(r), nil
}

func (s *pgStore) GetMenuByIDForUpdate(ctx context.Context, id uuid.UUID) (Menu, error) {
	r, err := s.queries(ctx).GetMenuByIDForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Menu{}, errMenuNotFound
	}
	if err != nil {
		return Menu{}, err
	}
	return mapDBMenu(r), nil
}

func (s *pgStore) CreateMenu(ctx context.Context, rec createMenuRecord) (Menu, error) {
	r, err := s.queries(ctx).CreateMenu(ctx, dbgen.CreateMenuParams{
		ID:             rec.ID,
		ParentID:       ptrToNullUUID(rec.ParentID),
		Type:           rec.Type,
		Code:           rec.Code,
		Title:          rec.Title,
		Path:           rec.Path,
		Icon:           rec.Icon,
		PermissionCode: pgtype.Text{String: rec.PermissionCode, Valid: rec.PermissionCode != ""},
		SortOrder:      rec.SortOrder,
		Visible:        rec.Visible,
		Status:         rec.Status,
		CreatedBy:      ptrToNullUUID(rec.ActorID),
		UpdatedBy:      ptrToNullUUID(rec.ActorID),
	})
	if err != nil {
		if isUniqueViolation(err, "menus_code_key") {
			return Menu{}, errMenuCodeConflict
		}
		if isForeignKeyViolation(err, "menus_parent_id_fkey") {
			return Menu{}, errMenuInvalidParent
		}
		if isForeignKeyViolation(err, "menus_permission_code_fkey") {
			return Menu{}, errInvalidPermission
		}
		return Menu{}, err
	}
	return mapDBMenu(r), nil
}

func (s *pgStore) UpdateMenu(ctx context.Context, rec updateMenuRecord) (Menu, error) {
	r, err := s.queries(ctx).UpdateMenu(ctx, dbgen.UpdateMenuParams{
		ID:             rec.ID,
		ParentID:       ptrToNullUUID(rec.ParentID),
		Type:           rec.Type,
		Title:          rec.Title,
		Path:           rec.Path,
		Icon:           rec.Icon,
		PermissionCode: pgtype.Text{String: rec.PermissionCode, Valid: rec.PermissionCode != ""},
		SortOrder:      rec.SortOrder,
		Visible:        rec.Visible,
		Status:         rec.Status,
		UpdatedBy:      ptrToNullUUID(rec.ActorID),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Menu{}, errMenuNotFound
	}
	if err != nil {
		if isForeignKeyViolation(err, "menus_parent_id_fkey") {
			return Menu{}, errMenuInvalidParent
		}
		if isForeignKeyViolation(err, "menus_permission_code_fkey") {
			return Menu{}, errInvalidPermission
		}
		return Menu{}, err
	}
	return mapDBMenu(r), nil
}

func (s *pgStore) UpdateMenuOrder(ctx context.Context, id uuid.UUID, parentID *uuid.UUID, sortOrder int32, actorID *uuid.UUID) error {
	err := s.queries(ctx).UpdateMenuOrder(ctx, dbgen.UpdateMenuOrderParams{
		ID:        id,
		ParentID:  ptrToNullUUID(parentID),
		SortOrder: sortOrder,
		UpdatedBy: ptrToNullUUID(actorID),
	})
	if isForeignKeyViolation(err, "menus_parent_id_fkey") {
		return errMenuInvalidParent
	}
	return err
}

func (s *pgStore) CountMenuChildren(ctx context.Context, id uuid.UUID) (int64, error) {
	return s.queries(ctx).CountMenuChildren(ctx, uuid.NullUUID{UUID: id, Valid: true})
}

func (s *pgStore) DeleteMenu(ctx context.Context, id uuid.UUID) error {
	err := s.queries(ctx).DeleteMenu(ctx, id)
	if isForeignKeyViolation(err, "menus_parent_id_fkey") {
		return errMenuHasChildren
	}
	return err
}

func mapDBMenu(r dbgen.Menu) Menu {
	return Menu{
		ID:             r.ID,
		ParentID:       nullUUIDToPtr(r.ParentID),
		Type:           r.Type,
		Code:           r.Code,
		Title:          r.Title,
		TitleKey:       r.TitleKey.String,
		Path:           r.Path,
		Icon:           r.Icon,
		PermissionCode: r.PermissionCode.String,
		SortOrder:      r.SortOrder,
		Visible:        r.Visible,
		Status:         r.Status,
		IsSystem:       r.IsSystem,
		Children:       make([]Menu, 0),
		CreatedBy:      nullUUIDToPtr(r.CreatedBy),
		UpdatedBy:      nullUUIDToPtr(r.UpdatedBy),
		CreatedAt:      r.CreatedAt.Time.UTC(),
		UpdatedAt:      r.UpdatedAt.Time.UTC(),
	}
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

func nonNilUUIDs(in []uuid.UUID) []uuid.UUID {
	if in == nil {
		return make([]uuid.UUID, 0)
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

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgCodeUniqueViolation && pgErr.ConstraintName == constraint
}

func isForeignKeyViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgCodeForeignKeyViolation && pgErr.ConstraintName == constraint
}
