package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"sync"
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
	userAccessCacheTTL   = 5 * time.Minute
	cacheShardCount      = 64
	maxShardGenEntries   = 256
	cacheDeleteChunkSize = 500

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

type userCacheToken struct {
	epoch uint64
	gen   uint64
}

type userCacheShard struct {
	mu    sync.Mutex
	epoch uint64
	gens  map[uuid.UUID]uint64
}

// Service coordinates RBAC catalog seeding, role CRUD, role assignment, and cached permission checks.
type Service struct {
	store       store
	tx          TxRunner
	cache       cache.Cache
	i18n        *i18n.Bundle
	audit       AuditRecorder
	actors      ActorResolver
	log         *slog.Logger
	cacheShards [cacheShardCount]userCacheShard
}

// ServiceDeps holds optional cross-cutting dependencies for access.Service.
type ServiceDeps struct {
	Tx     TxRunner
	Cache  cache.Cache
	I18n   *i18n.Bundle
	Audit  AuditRecorder
	Actors ActorResolver
	Log    *slog.Logger
}

// NewService creates a new access Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool, deps ...ServiceDeps) *Service {
	var d ServiceDeps
	if len(deps) > 0 {
		d = deps[0]
	}
	if d.Tx == nil && pool != nil {
		d.Tx = database.NewTxManager(pool)
	}
	return newServiceWithStore(newStore(pool), d)
}

func newServiceWithStore(s store, d ServiceDeps) *Service {
	log := d.Log
	if log == nil {
		log = slog.Default()
	}
	svc := &Service{
		store:  s,
		tx:     d.Tx,
		cache:  d.Cache,
		i18n:   d.I18n,
		audit:  d.Audit,
		actors: d.Actors,
		log:    log,
	}
	for i := range svc.cacheShards {
		svc.cacheShards[i].gens = make(map[uuid.UUID]uint64)
	}
	return svc
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
	if !isValidText(q) {
		return pagination.Result[Role]{}, apperr.Validation([]apperr.FieldError{{
			Field:   "q",
			Message: "VALIDATION_DEFAULT",
		}})
	}
	query := listRolesQuery{
		Q:                  q,
		MatchedSystemCodes: s.matchSystemRoleCodes(q),
		SuperAdminName:     s.resolveSystemRoleSortName(ctx),
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
	if !isValidText(name) || utf8.RuneCountInString(name) < 1 || utf8.RuneCountInString(name) > maxRoleNameRunes {
		fieldErrs = append(fieldErrs, apperr.FieldError{
			Field:   "name",
			Message: ErrCodeInvalidRoleName,
		})
	}
	if !isValidText(desc) || utf8.RuneCountInString(desc) > maxRoleDescRunes {
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
		return Role{}, apperr.Validation(fieldErrs)
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
				if errors.Is(grantErr, errInvalidPermission) {
					return apperr.Validation([]apperr.FieldError{{Field: "permissions", Message: ErrCodeInvalidPermissionCode}})
				}
				return apperr.Internal(fmt.Errorf("grant permission %s to role %s: %w", permCode, role.ID, grantErr))
			}
		}

		role.Permissions = perms
		role.PermissionCount = int64(len(perms))
		role.UserCount = 0
		created = role

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditResourceRole, auditActionCreate, role.ID.String(), map[string]any{
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
		if !isValidText(trimmed) || utf8.RuneCountInString(trimmed) < 1 || utf8.RuneCountInString(trimmed) > maxRoleNameRunes {
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
		if !isValidText(trimmed) || utf8.RuneCountInString(trimmed) > maxRoleDescRunes {
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
		return Role{}, apperr.Validation(fieldErrs)
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
					if errors.Is(grantErr, errInvalidPermission) {
						return apperr.Validation([]apperr.FieldError{{Field: "permissions", Message: ErrCodeInvalidPermissionCode}})
					}
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
		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditResourceRole, auditActionUpdate, saved.ID.String(), meta)
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

		return s.recordAudit(txCtx, input.ActorID, input.Meta, auditResourceRole, auditActionDelete, existing.ID.String(), map[string]any{
			"code": existing.Code,
			"name": existing.Name,
		})
	})
}

// AssignRoleByCode assigns a role to userID and schedules cache invalidation after transaction commit.
func (s *Service) AssignRoleByCode(ctx context.Context, userID uuid.UUID, roleCode string) error {
	return s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, userRoleAdvisoryLockKey(userID)); lockErr != nil {
			return apperr.Internal(lockErr)
		}
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
			if errors.Is(err, errUserNotFound) {
				return apperr.NotFound(apperr.CodeNotFound)
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
	return nonNilUUIDs(ids), nil
}

// ListUserIDsByRoleCode returns user IDs holding the specified role code.
func (s *Service) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	ids, err := s.store.ListUserIDsByRoleCode(ctx, code)
	if err != nil {
		return nil, apperr.Internal(fmt.Errorf("list user ids by role code %s: %w", code, err))
	}
	return nonNilUUIDs(ids), nil
}

// ReplaceUserRoles replaces all role assignments for userID and invalidates the user's permission cache after commit.
func (s *Service) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	uniqueRoleIDs := dedupeUUIDs(roleIDs)
	return s.withinTx(ctx, func(txCtx context.Context) error {
		if lockErr := database.AcquireAdvisoryXactLock(txCtx, userRoleAdvisoryLockKey(userID)); lockErr != nil {
			return apperr.Internal(lockErr)
		}
		if err := s.store.DeleteUserRoles(txCtx, userID); err != nil {
			return apperr.Internal(fmt.Errorf("clear roles for user %s: %w", userID, err))
		}
		for _, roleID := range uniqueRoleIDs {
			if err := s.store.AssignUserRole(txCtx, userID, roleID); err != nil {
				if errors.Is(err, errRoleNotFound) {
					return roleNotFoundError()
				}
				if errors.Is(err, errUserNotFound) {
					return apperr.NotFound(apperr.CodeNotFound)
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
	_, inTx := database.TxFromContext(ctx)
	if s.cache != nil && !inTx {
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

	tokBefore := s.readCacheToken(userID)

	ua, err := s.store.GetUserAccessData(ctx, userID)
	if err != nil {
		return UserAccess{}, apperr.Internal(fmt.Errorf("load user access %s: %w", userID, err))
	}
	result := ensureNonNilSlices(ua)

	if s.cache != nil && !inTx {
		if encoded, marshalErr := json.Marshal(result); marshalErr == nil {
			s.writeCacheIfUnchanged(ctx, userID, key, tokBefore, encoded)
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
	var byShard [cacheShardCount][]uuid.UUID
	seen := make(map[uuid.UUID]struct{}, len(userIDs))
	for _, id := range userIDs {
		if id == uuid.Nil {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		idx := cacheShardIndex(id)
		byShard[idx] = append(byShard[idx], id)
	}
	if len(seen) == 0 {
		return
	}

	for idx := range byShard {
		ids := byShard[idx]
		if len(ids) == 0 {
			continue
		}
		keys := make([]string, 0, len(ids))
		for _, id := range ids {
			keys = append(keys, userCacheKey(id))
		}

		sh := &s.cacheShards[idx]
		sh.mu.Lock()
		if sh.gens == nil {
			sh.gens = make(map[uuid.UUID]uint64, len(ids))
		}
		for _, id := range ids {
			sh.gens[id]++
		}
		if len(sh.gens) > maxShardGenEntries {
			// Rotate the shard epoch and clear per-user counters so cache generation state stays bounded.
			sh.epoch++
			clear(sh.gens)
		}
		for start := 0; start < len(keys); start += cacheDeleteChunkSize {
			end := min(start+cacheDeleteChunkSize, len(keys))
			if err := s.cache.Delete(ctx, keys[start:end]...); err != nil {
				s.log.Warn("access cache invalidate failed", "count", end-start, "error", err)
			}
		}
		sh.mu.Unlock()
	}
}

func (s *Service) readCacheToken(userID uuid.UUID) userCacheToken {
	sh := &s.cacheShards[cacheShardIndex(userID)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	return userCacheToken{
		epoch: sh.epoch,
		gen:   sh.gens[userID],
	}
}

func (s *Service) writeCacheIfUnchanged(ctx context.Context, userID uuid.UUID, key string, expected userCacheToken, encoded []byte) {
	sh := &s.cacheShards[cacheShardIndex(userID)]
	sh.mu.Lock()
	defer sh.mu.Unlock()
	if sh.epoch != expected.epoch || sh.gens[userID] != expected.gen {
		return
	}
	if setErr := s.cache.Set(ctx, key, encoded, userAccessCacheTTL); setErr != nil {
		s.log.Warn("access cache set failed", "user_id", userID, "error", setErr)
	}
}

func cacheShardIndex(userID uuid.UUID) int {
	return int(userID[15]) & (cacheShardCount - 1)
}

func userRoleAdvisoryLockKey(userID uuid.UUID) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("tianshu:user_roles:"))
	_, _ = h.Write(userID[:])
	// Force MSB=1 so per-user lock keys live in negative int64 space and never collide with static keys.
	return int64(h.Sum64() | (uint64(1) << 63))
}

func dedupeUUIDs(ids []uuid.UUID) []uuid.UUID {
	if len(ids) <= 1 {
		return ids
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
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

func (s *Service) resolveSystemRoleSortName(ctx context.Context) string {
	if s.i18n == nil {
		return RoleCodeSuperAdmin
	}
	locale := i18n.LocaleFromContext(ctx)
	for _, r := range SystemRoles() {
		if r.Code == RoleCodeSuperAdmin {
			if translated := s.i18n.Translate(locale, r.NameKey, nil); translated != "" && translated != r.NameKey {
				return translated
			}
		}
	}
	return RoleCodeSuperAdmin
}

func (s *Service) recordAudit(
	ctx context.Context,
	actorID *uuid.UUID,
	meta RequestMeta,
	resource string,
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
		Resource:      resource,
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
		s.log.Warn("access.Service running without TxRunner; transaction guarantees are disabled")
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
