package access_test

import (
	"context"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
	"tianshu/migrations"
)

func openIsolatedTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn, dsnExplicit := os.LookupEnv("TIANSHU_TEST_DSN")
	if !dsnExplicit {
		dsn = config.Default().Database.DSN
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		if dsnExplicit {
			t.Fatalf("postgres unavailable: %v", err)
		}
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
		if dsnExplicit {
			t.Fatalf("postgres ping failed: %v", err)
		}
		t.Skipf("postgres ping failed: %v", err)
	}

	schema := "access_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		adminPool.Close()
		t.Fatalf("create isolated schema %s: %v", schema, err)
	}

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = adminPool.Exec(cleanupCtx, "DROP SCHEMA IF EXISTS "+schema+" CASCADE")
		adminPool.Close()
	})

	poolCfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	poolCfg.ConnConfig.RuntimeParams["search_path"] = schema

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		t.Fatalf("open schema pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := database.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return pool
}

func TestAccessRolesPostgresIntegration(t *testing.T) {
	pool := openIsolatedTestPool(t)
	txMgr := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", access.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	svc := access.NewService(pool, txMgr, mem, logger.New("test", "error", io.Discard), access.ServiceDeps{
		I18n: bundle,
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	// Seed a real user row to exercise user_roles FK constraints and cache invalidation.
	userID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES ($1, 'member_one', 'member@example.com', 'hash', 'Member One', 'active')
	`, userID); err != nil {
		t.Fatalf("seed user: %v", err)
	}

	// Create custom role with initial permissions.
	role, err := svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code:        "qa_operator",
		Name:        "QA Operator",
		Description: "Can view users and audit logs",
		Permissions: []string{access.PermUsersRead, access.PermAuditRead},
		ActorID:     &userID,
	})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if !slices.Equal(role.Permissions, []string{access.PermAuditRead, access.PermUsersRead}) {
		t.Fatalf("created permissions = %v", role.Permissions)
	}

	// Unique constraint on roles.code must map to ROLES_CODE_CONFLICT.
	_, err = svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code: "qa_operator",
		Name: "Duplicate QA",
	})
	if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeRoleCodeConflict {
		t.Fatalf("duplicate code error = %v, want ROLES_CODE_CONFLICT", err)
	}

	// Assign role to user and warm permission cache.
	if err := svc.AssignRoleByCode(t.Context(), userID, role.Code); err != nil {
		t.Fatalf("AssignRoleByCode() error = %v", err)
	}
	canDelete, err := svc.HasPermission(t.Context(), userID, access.PermUsersDelete)
	if err != nil || canDelete {
		t.Fatalf("before update HasPermission(users:delete) = %v, err = %v", canDelete, err)
	}

	// Update role permissions and verify cache invalidation takes effect immediately.
	nextPerms := []string{access.PermUsersRead, access.PermUsersDelete}
	nextName := "QA Lead"
	updated, err := svc.UpdateRole(t.Context(), access.UpdateRoleInput{
		RoleID:      role.ID,
		Name:        &nextName,
		Permissions: &nextPerms,
		ActorID:     &userID,
	})
	if err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if updated.UserCount != 1 || updated.PermissionCount != 2 {
		t.Fatalf("unexpected counts after update: %+v", updated)
	}

	canDelete, err = svc.HasPermission(t.Context(), userID, access.PermUsersDelete)
	if err != nil || !canDelete {
		t.Fatalf("after update HasPermission(users:delete) = %v, err = %v; want true", canDelete, err)
	}
	canAudit, err := svc.HasPermission(t.Context(), userID, access.PermAuditRead)
	if err != nil || canAudit {
		t.Fatalf("after update HasPermission(audit:read) = %v, err = %v; want false", canAudit, err)
	}

	// Deleting role while assigned to a user must fail with ROLES_IN_USE.
	err = svc.DeleteRole(t.Context(), access.DeleteRoleInput{RoleID: role.ID, ActorID: &userID})
	if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeRoleInUse {
		t.Fatalf("DeleteRole(in-use) error = %v, want ROLES_IN_USE", err)
	}

	// Search with escaped wildcard and Chinese system role name.
	listZh, err := svc.ListRoles(t.Context(), access.RoleListFilter{
		Pagination: pagination.Normalize(1, 20),
		Q:          "超级",
	})
	if err != nil {
		t.Fatalf("ListRoles(zh query) error = %v", err)
	}
	if listZh.Total != 1 || listZh.Items[0].Code != access.RoleCodeSuperAdmin {
		t.Fatalf("zh search result = %+v, want super_admin", listZh)
	}

	// Detach user and delete role cleanly.
	if _, err := pool.Exec(t.Context(), `DELETE FROM user_roles WHERE role_id = $1`, role.ID); err != nil {
		t.Fatalf("detach user_roles: %v", err)
	}
	if err := svc.DeleteRole(t.Context(), access.DeleteRoleInput{RoleID: role.ID, ActorID: &userID}); err != nil {
		t.Fatalf("DeleteRole(unused) error = %v", err)
	}
}
