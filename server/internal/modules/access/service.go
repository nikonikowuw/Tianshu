package access

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/database"
	"tianshu/internal/shared/apperr"
)

const userAccessCacheTTL = 5 * time.Minute

// Service coordinates RBAC catalog seeding, role assignment, and cached permission checks.
type Service struct {
	store store
	tx    TxRunner
	cache cache.Cache
	log   *slog.Logger
}

// NewService creates a new access Service backed by PostgreSQL.
func NewService(pool *pgxpool.Pool, tx TxRunner, c cache.Cache, log *slog.Logger) *Service {
	return newServiceWithStore(newStore(pool), tx, c, log)
}

func newServiceWithStore(s store, tx TxRunner, c cache.Cache, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store: s,
		tx:    tx,
		cache: c,
		log:   log,
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
		return nil
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
			return apperr.Internal(fmt.Errorf("assign role %s to user %s: %w", roleCode, userID, err))
		}

		// Register invalidation hook so nested callers only evict cache once the outermost tx commits.
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
