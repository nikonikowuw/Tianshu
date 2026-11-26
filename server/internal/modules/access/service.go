package access

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

const (
	userAccessCacheTTL = 5 * time.Minute

	auditResourceRole   = "role"
	auditActionCreate   = "role.create"
	auditActionUpdate   = "role.update"
	auditActionDelete   = "role.delete"
	auditStatusSuccess  = "success"
	maxRoleNameRunes    = 100
	maxRoleDescRunes    = 500
	minRoleCodeLen      = 2
	maxRoleCodeLen      = 64
	roleCodeRegexSource = `^[a-z][a-z0-9_-]{1,63}$`
)

var roleCodePattern = regexp.MustCompile(roleCodeRegexSource)

// Service coordinates RBAC catalog seeding, role CRUD, role assignment, and cached permission checks.
type Service struct {
	store  store
	tx     TxRunner
	cache  cache.Cache
	i18n   *i18n.Bundle
	audit  AuditRecorder
	actors ActorResolver
	log    *slog.Logger
}

// ServiceDeps holds optional cross-cutting dependencies for access.Service.
type ServiceDeps struct {
	I18n   *i18n.Bundle
	Audit  AuditRecorder
	Actors ActorResolver
}

// NewService creates a new access Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool, tx TxRunner, c cache.Cache, log *slog.Logger, deps ...ServiceDeps) *Service {
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	return newServiceWithStore(newStore(pool), tx, c, log, d)
}

func newServiceWithStore(s store, tx TxRunner, c cache.Cache, log *slog.Logger, deps ...ServiceDeps) *Service {
	if log == nil {
		log = slog.Default()
	}
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	return &Service{
		store:  s,
		tx:     tx,
		cache:  c,
		i18n:   d.I18n,
		audit:  d.Audit,
		actors: d.Actors,
		log:    log,
	}
}

// SyncCatalog idempotently upserts all declarative permissions and built-in system roles in one transaction.
func (s *Service) SyncCatalog(ctx context.Context) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		perms := Catalog()
		for _, p := range perms {
			if err := s.store.UpsertPermission(txCtx, p); err != nil {
				return apperr.Internal(fmt.Errorf("upsert permission %s: %w", p.Code, err))
			}
		}

		for _, role := range SystemRoles() {
			newID, err := uuid.NewV7()
			if err != nil {
				return apperr.Internal(fmt.Errorf("generate role uuid: %w", err))
			}
			roleID, err := s.store.EnsureSystemRole(txCtx, newID, role)
			if err != nil {
				return apperr.Internal(fmt.Errorf("ensure system role %s: %w", role.Code, err))
			}
			if role.AllPermissions {
				for _, p := range perms {
					if err := s.store.GrantRolePermission(txCtx, roleID, p.Code); err != nil {
						return apperr.Internal(fmt.Errorf("grant %s to %s: %w", p.Code, role.Code, err))
					}
				}
			}
		}

		return s.syncSystemMenus(txCtx)
	})
}

// ListPermissionGroups returns the declarative permission catalog grouped by module.
func (s *Service) ListPermissionGroups() []PermissionGroupDef {
	return PermissionGroups()
}

// ListRoles returns a paginated list of roles with user counts and assigned permission codes.
func (s *Service) ListRoles(ctx context.Context, filter RoleListFilter) (pagination.Result[Role], error) {
	pageParams := pagination.Normalize(filter.Pagination.Page, filter.Pagination.PageSize)
	q := strings.TrimSpace(filter.Q)
	query := listRolesQuery{
		Q:                  q,
		MatchedSystemCodes: s.matchSystemRoleCodes(q),
		IsSystem:           filter.IsSystem,
		Sort:               normalizeRoleSort(filter.Sort),
		Order:              normalizeRoleOrder(filter.Order),
		Offset:             int32(pageParams.Offset()),
		Limit:              int32(pageParams.Limit()),
	}

	total, err := s.store.CountRoles(ctx, query)
	if err != nil {
		return pagination.Result[Role]{}, apperr.Internal(fmt.Errorf("count roles: %w", err))
	}
	if total == 0 {
		return pagination.NewResult(make([]Role, 0), 0, pageParams), nil
	}

	roles, err := s.store.ListRoles(ctx, query)
	if err != nil {
		return pagination.Result[Role]{}, apperr.Internal(fmt.Errorf("list roles: %w", err))
	}
	if err := s.attachRolePermissions(ctx, roles); err != nil {
		return pagination.Result[Role]{}, err
	}

	return pagination.NewResult(roles, total, pageParams), nil
}

// GetRole returns a single role with its counts and assigned permission codes.
func (s *Service) GetRole(ctx context.Context, id uuid.UUID) (Role, error) {
	role, err := s.store.GetRoleByID(ctx, id)
	if errors.Is(err, errRoleNotFound) {
		return Role{}, roleNotFoundError()
	}
	if err != nil {
		return Role{}, apperr.Internal(fmt.Errorf("get role %s: %w", id, err))
	}

	permMap, err := s.store.ListPermissionCodesByRoleIDs(ctx, []uuid.UUID{role.ID})
	if err != nil {
		return Role{}, apperr.Internal(fmt.Errorf("list permissions for role %s: %w", id, err))
	}
	role.Permissions = nonNilStrings(permMap[role.ID])
	role.PermissionCount = int64(len(role.Permissions))
	return role, nil
}

// CreateRole creates a custom role with validated permission codes and writes an audit record.
func (s *Service) CreateRole(ctx context.Context, input CreateRoleInput) (Role, error) {
	code := strings.ToLower(strings.TrimSpace(input.Code))
	name := strings.TrimSpace(input.Name)
	desc := strings.TrimSpace(input.Description)

	var fieldErrs []apperr.FieldError
	if len(code) < minRoleCodeLen || len(code) > maxRoleCodeLen || !roleCodePattern.MatchString(code) {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "code",
			Message: ErrCodeInvalidRoleCode,
		})
	}
	if nameLen := utf8.RuneCountInString(name); nameLen < 1 || nameLen > maxRoleNameRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "name",
			Message: ErrCodeInvalidRoleName,
		})
	}
	if utf8.RuneCountInString(desc) > maxRoleDescRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "description",
			Message: ErrCodeInvalidRoleDesc,
		})
	}
	perms, permsOK := normalizeAndValidatePermissions(input.Permissions)
	if !permsOK {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "permissions",
			Message: ErrCodeInvalidPermissionCode,
		})
	}
	if len(fieldErrs) > 0 {
		return Role{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	roleID, err := uuid.NewV7()
	if err != nil {
		return Role{}, apperr.Internal(fmt.Errorf("generate role uuid: %w", err))
	}

	var created Role
	err = s.withinTx(ctx, func(txCtx context.Context) error {
		role, createErr := s.store.CreateRole(txCtx, createRoleRecord{
			ID:          roleID,
			Code:        code,
			Name:        name,
			Description: desc,
			ActorID:     input.ActorID,
		})
		if errors.Is(createErr, errRoleCodeConflict) {
			return roleCodeConflictError()
		}
		if createErr != nil {
			return apperr.Internal(fmt.Errorf("create role %s: %w", code, createErr))
		}

		for _, permCode := range perms {
			if grantErr := s.store.GrantRolePermission(txCtx, role.ID, permCode); grantErr != nil {
				return apperr.Internal(fmt.Errorf("grant permission %s to role %s: %w", permCode, role.ID, grantErr))
			}
		}

		role.Permissions = perms
		role.PermissionCount = int64(len(perms))
		role.UserCount = 0
		created = role

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionCreate, role.ID.String(), map[string]any{
			"code":        role.Code,
			"name":        role.Name,
			"permissions": perms,
		})
	})
	if err != nil {
		return Role{}, err
	}
	return created, nil
}

// UpdateRole updates a custom role's metadata and/or permission set, invalidating affected user caches after commit.
func (s *Service) UpdateRole(ctx context.Context, input UpdateRoleInput) (Role, error) {
	var fieldErrs []apperr.FieldError
	var nextName *string
	if input.Name != nil {
		trimmed := strings.TrimSpace(*input.Name)
		if nameLen := utf8.RuneCountInString(trimmed); nameLen < 1 || nameLen > maxRoleNameRunes {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "name",
				Message: ErrCodeInvalidRoleName,
			})
		} else {
			nextName = &trimmed
		}
	}

	var nextDesc *string
	if input.Description != nil {
		trimmed := strings.TrimSpace(*input.Description)
		if utf8.RuneCountInString(trimmed) > maxRoleDescRunes {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "description",
				Message: ErrCodeInvalidRoleDesc,
			})
		} else {
			nextDesc = &trimmed
		}
	}

	var nextPerms []string
	updatePerms := false
	if input.Permissions != nil {
		updatePerms = true
		var ok bool
		nextPerms, ok = normalizeAndValidatePermissions(*input.Permissions)
		if !ok {
			fieldErrs = append(fieldErrs, apperr.FieldError{
				Field:   "permissions",
				Message: ErrCodeInvalidPermissionCode,
			})
		}
	}

	if len(fieldErrs) > 0 {
		return Role{}, apperr.Validation(sortAndDedupeFieldErrors(fieldErrs))
	}

	var updated Role
	err := s.withinTx(ctx, func(txCtx context.Context) error {
		existing, lockErr := s.store.GetRoleByIDForUpdate(txCtx, input.RoleID)
		if errors.Is(lockErr, errRoleNotFound) {
			return roleNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock role %s: %w", input.RoleID, lockErr))
		}
		if existing.IsSystem {
			return roleSystemImmutableError()
		}

		finalName := existing.Name
		if nextName != nil {
			finalName = *nextName
		}
		finalDesc := existing.Description
		if nextDesc != nil {
			finalDesc = *nextDesc
		}

		saved, updateErr := s.store.UpdateRole(txCtx, updateRoleRecord{
			ID:          existing.ID,
			Name:        finalName,
			Description: finalDesc,
			ActorID:     input.ActorID,
		})
		if errors.Is(updateErr, errRoleNotFound) {
			return roleNotFoundError()
		}
		if updateErr != nil {
			return apperr.Internal(fmt.Errorf("update role %s: %w", existing.ID, updateErr))
		}

		var finalPerms []string
		if updatePerms {
			if delErr := s.store.DeleteRolePermissions(txCtx, existing.ID); delErr != nil {
				return apperr.Internal(fmt.Errorf("clear permissions for role %s: %w", existing.ID, delErr))
			}
			for _, permCode := range nextPerms {
				if grantErr := s.store.GrantRolePermission(txCtx, existing.ID, permCode); grantErr != nil {
					return apperr.Internal(fmt.Errorf("grant permission %s to role %s: %w", permCode, existing.ID, grantErr))
				}
			}
			finalPerms = nextPerms

			affectedUsers, usersErr := s.store.ListRoleUserIDs(txCtx, existing.ID)
			if usersErr != nil {
				return apperr.Internal(fmt.Errorf("list affected users for role %s: %w", existing.ID, usersErr))
			}
			if len(affectedUsers) > 0 {
				userIDsCopy := slices.Clone(affectedUsers)
				database.AfterCommit(txCtx, func() {
					s.InvalidateUserCache(context.WithoutCancel(txCtx), userIDsCopy...)
				})
			}
		} else {
			permMap, permErr := s.store.ListPermissionCodesByRoleIDs(txCtx, []uuid.UUID{existing.ID})
			if permErr != nil {
				return apperr.Internal(fmt.Errorf("list permissions for role %s: %w", existing.ID, permErr))
			}
			finalPerms = nonNilStrings(permMap[existing.ID])
		}

		userCount, countErr := s.store.CountRoleUsers(txCtx, existing.ID)
		if countErr != nil {
			return apperr.Internal(fmt.Errorf("count users for role %s: %w", existing.ID, countErr))
		}

		saved.Permissions = finalPerms
		saved.PermissionCount = int64(len(finalPerms))
		saved.UserCount = userCount
		updated = saved

		meta := map[string]any{
			"code":               saved.Code,
			"name":               saved.Name,
			"permissionsUpdated": updatePerms,
		}
		if updatePerms {
			meta["permissions"] = finalPerms
		}
		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionUpdate, saved.ID.String(), meta)
	})
	if err != nil {
		return Role{}, err
	}
	return updated, nil
}

// DeleteRole removes a custom role inside a transaction after row-locking and verifying it has no assigned users.
func (s *Service) DeleteRole(ctx context.Context, input DeleteRoleInput) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		existing, lockErr := s.store.GetRoleByIDForUpdate(txCtx, input.RoleID)
		if errors.Is(lockErr, errRoleNotFound) {
			return roleNotFoundError()
		}
		if lockErr != nil {
			return apperr.Internal(fmt.Errorf("lock role %s for delete: %w", input.RoleID, lockErr))
		}
		if existing.IsSystem {
			return roleSystemImmutableError()
		}

		userCount, countErr := s.store.CountRoleUsers(txCtx, existing.ID)
		if countErr != nil {
			return apperr.Internal(fmt.Errorf("count users for role %s: %w", existing.ID, countErr))
		}
		if userCount > 0 {
			return roleInUseError()
		}

		if delErr := s.store.DeleteRole(txCtx, existing.ID); delErr != nil {
			return apperr.Internal(fmt.Errorf("delete role %s: %w", existing.ID, delErr))
		}

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditActionDelete, existing.ID.String(), map[string]any{
			"code": existing.Code,
			"name": existing.Name,
		})
	})
}

// AssignRoleByCode assigns a role to userID and schedules cache invalidation after transaction commit.
func (s *Service) AssignRoleByCode(ctx context.Context, userID uuid.UUID, roleCode string) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		roleID, err := s.store.GetRoleIDByCode(txCtx, roleCode)
		if errors.Is(err, errRoleNotFound) {
			return roleNotFoundError()
		}
		if err != nil {
			return apperr.Internal(fmt.Errorf("lookup role %s: %w", roleCode, err))
		}
		if err := s.store.AssignUserRole(txCtx, userID, roleID); err != nil {
			if errors.Is(err, errRoleNotFound) {
				return roleNotFoundError()
			}
			return apperr.Internal(fmt.Errorf("assign role %s to user %s: %w", roleCode, userID, err))
		}

		// Register invalidation hook so nested callers only evict cache once the outermost tx commits.
		database.AfterCommit(txCtx, func() {
			s.InvalidateUserCache(context.WithoutCancel(txCtx), userID)
		})
		return nil
	})
}

// ListRoleOptions returns all roles in lightweight form for dropdowns and filters.
func (s *Service) ListRoleOptions(ctx context.Context) ([]RoleRef, error) {
	refs, err := s.store.ListRoleOptions(ctx)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list role options: %w", err))
	}
	return refs, nil
}

// ResolveRoles returns matching RoleRefs for the provided role IDs (missing IDs are omitted).
func (s *Service) ResolveRoles(ctx context.Context, ids []uuid.UUID) ([]RoleRef, error) {
	if len(ids) == 0 {
		return make([]RoleRef, 0), nil
	}
	refs, err := s.store.ListRolesByIDs(ctx, ids)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("resolve roles: %w", err))
	}
	return refs, nil
}

// ListRolesByUserIDs returns role summaries grouped by user ID.
func (s *Service) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error) {
	if len(userIDs) == 0 {
		return make(map[uuid.UUID][]RoleRef), nil
	}
	m, err := s.store.ListRolesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list roles by user ids: %w", err))
	}
	return m, nil
}

// ListUserIDsByRoleIDs returns distinct user IDs holding any of the given role IDs.
func (s *Service) ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	if len(roleIDs) == 0 {
		return make([]uuid.UUID, 0), nil
	}
	ids, err := s.store.ListUserIDsByRoleIDs(ctx, roleIDs)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list user ids by role ids: %w", err))
	}
	return ids, nil
}

// ListUserIDsByRoleCode returns user IDs holding the specified role code.
func (s *Service) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	ids, err := s.store.ListUserIDsByRoleCode(ctx, code)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list user ids by role code %s: %w", code, err))
	}
	return ids, nil
}

// ReplaceUserRoles replaces all role assignments for userID and invalidates the user's permission cache after commit.
func (s *Service) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		if err := s.store.DeleteUserRoles(txCtx, userID); err != nil {
			return apperr.Internal(fmt.Errorf("clear roles for user %s: %w", userID, err))
		}
		for _, roleID := range roleIDs {
			if err := s.store.AssignUserRole(txCtx, userID, roleID); err != nil {
				if errors.Is(err, errRoleNotFound) {
					return roleNotFoundError()
				}
				return apperr.Internal(fmt.Errorf("assign role %s to user %s: %w", roleID, userID, err))
			}
		}
		database.AfterCommit(txCtx, func() {
			s.InvalidateUserCache(context.WithoutCancel(txCtx), userID)
		})
		return nil
	})
}

// GetUserAccess returns the current roles and permissions for userID using read-through caching.
func (s *Service) GetUserAccess(ctx context.Context, userID uuid.UUID) (UserAccess, error) {
	key := userCacheKey(userID)
	if s.cache != nil {
		raw, err := s.cache.Get(ctx, key)
		if err == nil {
			var cached UserAccess
			if unmarshalErr := json.Unmarshal(raw, &cached); unmarshalErr == nil {
				return ensureNonNilSlices(cached), nil
			}
		} else if !errors.Is(err, cache.ErrNotFound) {
			s.log.Warn("access cache get failed, falling back to database", "user_id", userID, "error", err)
		}
	}

	roles, err := s.store.ListUserRoleCodes(ctx, userID)
	if err != nil {
		return UserAccess{}, apperr.Internal(fmt.Errorf("list user roles: %w", err))
	}
	perms, err := s.store.ListUserPermissions(ctx, userID)
	if err != nil {
		return UserAccess{}, apperr.Internal(fmt.Errorf("list user permissions: %w", err))
	}

	result := ensureNonNilSlices(UserAccess{
		Roles:       roles,
		Permissions: perms,
	})

	if s.cache != nil {
		if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
			if setErr := s.cache.Set(ctx, key, encoded, userAccessCacheTTL); setErr != nil {
				s.log.Warn("access cache set failed", "user_id", userID, "error", setErr)
			}
		}
	}
	return result, nil
}

// HasPermission reports whether userID currently holds permissionCode.
func (s *Service) HasPermission(ctx context.Context, userID uuid.UUID, permissionCode string) (bool, error) {
	ua, err := s.GetUserAccess(ctx, userID)
	if err != nil {
		return false, err
	}
	return slices.Contains(ua.Permissions, permissionCode), nil
}

// InvalidateUserCache removes cached roles and permissions for the provided user IDs.
func (s *Service) InvalidateUserCache(ctx context.Context, userIDs ...uuid.UUID) {
	if s.cache == nil || len(userIDs) == 0 {
		return
	}
	keys := make([]string, 0, len(userIDs))
	for _, id := range userIDs {
		if id != uuid.Nil {
			keys = append(keys, userCacheKey(id))
		}
	}
	if len(keys) == 0 {
		return
	}
	if err := s.cache.Delete(ctx, keys...); err != nil {
		s.log.Warn("access cache invalidate failed", "count", len(keys), "error", err)
	}
}

func (s *Service) attachRolePermissions(ctx context.Context, roles []Role) error {
	if len(roles) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(roles))
	for _, r := range roles {
		ids = append(ids, r.ID)
	}
	permMap, err := s.store.ListPermissionCodesByRoleIDs(ctx, ids)
	if err != nil {
		return apperr.Internal(fmt.Errorf("list role permissions: %w", err))
	}
	for idx := range roles {
		perms := nonNilStrings(permMap[roles[idx].ID])
		roles[idx].Permissions = perms
		roles[idx].PermissionCount = int64(len(perms))
	}
	return nil
}

func (s *Service) matchSystemRoleCodes(q string) []string {
	if q == "" || s.i18n == nil {
		return make([]string, 0)
	}
	needle := strings.ToLower(q)
	matched := make([]string, 0)
	for _, sysRole := range SystemRoles() {
		found := false
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			name := strings.ToLower(s.i18n.Translate(lang, sysRole.NameKey, nil))
			desc := strings.ToLower(s.i18n.Translate(lang, sysRole.DescriptionKey, nil))
			if strings.Contains(name, needle) || strings.Contains(desc, needle) {
				found = true
				break
			}
		}
		if found {
			matched = append(matched, sysRole.Code)
		}
	}
	return matched
}

func (s *Service) recordAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	meta RequestMeta,
	action string,
	resourceID string,
	metadata map[string]any,
) error {
	if s.audit == nil {
		return nil
	}
	actorUsername := ""
	if actorID != nil && *actorID != uuid.Nil && s.actors != nil {
		username, err := s.actors.GetUsername(ctx, *actorID)
		if err != nil {
			s.log.Warn("resolve actor username for access audit failed", "actor_id", actorID.String(), "error", err)
		} else {
			actorUsername = username
		}
	}
	if err := s.audit.Record(ctx, AuditEntry{
		ActorID:       actorID,
		ActorUsername: actorUsername,
		Action:        action,
		Resource:      auditResourceRole,
		ResourceID:    resourceID,
		Status:        auditStatusSuccess,
		IPAddress:     meta.IPAddress,
		UserAgent:     meta.UserAgent,
		Metadata:      metadata,
	}); err != nil {
		return apperr.Internal(fmt.Errorf("record audit %s: %w", action, err))
	}
	return nil
}

func (s *Service) withinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if s.tx == nil {
		return fn(ctx)
	}
	return s.tx.WithinTx(ctx, fn)
}

func userCacheKey(userID uuid.UUID) string {
	return "access:user:" + userID.String()
}

func ensureNonNilSlices(ua UserAccess) UserAccess {
	if ua.Roles == nil {
		ua.Roles = make([]string, 0)
	}
	if ua.Permissions == nil {
		ua.Permissions = make([]string, 0)
	}
	return ua
}

func normalizeAndValidatePermissions(raw []string) ([]string, bool) {
	if len(raw) == 0 {
		return make([]string, 0), true
	}
	seen := make(map[string]struct{}, len(raw))
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		code := strings.TrimSpace(item)
		if !IsValidPermissionCode(code) {
			return nil, false
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	slices.Sort(out)
	return out, true
}

func normalizeRoleSort(sort string) string {
	switch sort {
	case "created_at", "updated_at", "code", "name":
		return sort
	default:
		return ""
	}
}

func normalizeRoleOrder(order string) string {
	if strings.EqualFold(order, "asc") {
		return "asc"
	}
	return "desc"
}

func sortAndDedupeFieldErrors(in []apperr.FieldError) []apperr.FieldError {
	byField := make(map[string]apperr.FieldError, len(in))
	for _, fe := range in {
		if _, exists := byField[fe.Field]; !exists {
			byField[fe.Field] = fe
		}
	}
	out := make([]apperr.FieldError, 0, len(byField))
	for _, fe := range byField {
		out = append(out, fe)
	}
	slices.SortFunc(out, func(a, b apperr.FieldError) int {
		return cmp.Compare(a.Field, b.Field)
	})
	return out
}
