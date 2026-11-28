package access_test

import (
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"sync"
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

func newTestBundle(t *testing.T) *i18n.Bundle {
	t.Helper()
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", access.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}
	return bundle
}

func TestAccessRolesPostgresIntegration(t *testing.T) {
	pool := openIsolatedTestPool(t)
	txMgr := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()
	bundle := newTestBundle(t)

	svc := access.NewService(pool, access.ServiceDeps{
		Tx:    txMgr,
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	// Seed real users to exercise user_roles FK constraints, rollback, and concurrency.
	userID := uuid.Must(uuid.NewV7())
	userTwoID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES
			($1, 'member_one', 'member1@example.com', 'hash', 'Member One', 'active'),
			($2, 'member_two', 'member2@example.com', 'hash', 'Member Two', 'active')
	`, userID, userTwoID); err != nil {
		t.Fatalf("seed users: %v", err)
	}

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

	// Outer transaction rollback must NOT evict cache or persist permission changes.
	abortErr := errors.New("abort outer tx")
	rbPerms := []string{access.PermUsersRead, access.PermUsersDelete}
	err = txMgr.WithinTx(t.Context(), func(txCtx context.Context) error {
		if _, uErr := svc.UpdateRole(txCtx, access.UpdateRoleInput{
			RoleID:      role.ID,
			Permissions: &rbPerms,
		}); uErr != nil {
			return uErr
		}
		return abortErr
	})
	if !errors.Is(err, abortErr) {
		t.Fatalf("outer rollback err = %v, want abortErr", err)
	}
	if _, getErr := mem.Get(t.Context(), "access:user:"+userID.String()); getErr != nil {
		t.Fatalf("cache was evicted despite transaction rollback: %v", getErr)
	}
	canDelete, err = svc.HasPermission(t.Context(), userID, access.PermUsersDelete)
	if err != nil || canDelete {
		t.Fatalf("after rollback HasPermission(users:delete) = %v, err = %v; want false", canDelete, err)
	}

	// Committed UpdateRole invalidates cache immediately.
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

	// Localized sort=name places Super Administrator between QA Lead and Zeta Role in en-US.
	zeta, err := svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code: "zeta_role",
		Name: "Zeta Role",
	})
	if err != nil {
		t.Fatalf("CreateRole(zeta_role) error = %v", err)
	}
	enCtx := i18n.WithLocale(t.Context(), i18n.LangEnUS)
	sortedEn, err := svc.ListRoles(enCtx, access.RoleListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "name",
		Order:      "asc",
	})
	if err != nil {
		t.Fatalf("ListRoles(sort=name) error = %v", err)
	}
	gotCodes := []string{sortedEn.Items[0].Code, sortedEn.Items[1].Code, sortedEn.Items[2].Code}
	wantCodes := []string{"qa_operator", access.RoleCodeSuperAdmin, "zeta_role"}
	if !slices.Equal(gotCodes, wantCodes) {
		t.Fatalf("ListRoles(sort=name asc) = %v, want %v", gotCodes, wantCodes)
	}

	// Concurrent DeleteRole vs ReplaceUserRoles on zeta_role must serialize cleanly without orphan rows.
	start := make(chan struct{})
	var wg sync.WaitGroup
	var delErr, assignErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		delErr = svc.DeleteRole(t.Context(), access.DeleteRoleInput{RoleID: zeta.ID, ActorID: &userID})
	}()
	go func() {
		defer wg.Done()
		<-start
		assignErr = svc.ReplaceUserRoles(t.Context(), userTwoID, []uuid.UUID{zeta.ID})
	}()
	close(start)
	wg.Wait()

	if delErr == nil && assignErr == nil {
		t.Fatal("DeleteRole and ReplaceUserRoles cannot both succeed for the same role")
	}
	if delErr != nil {
		if appErr, ok := apperr.As(delErr); !ok || appErr.Code != access.ErrCodeRoleInUse {
			t.Fatalf("concurrent DeleteRole error = %v, want ROLES_IN_USE", delErr)
		}
	}
	if assignErr != nil {
		if appErr, ok := apperr.As(assignErr); !ok || appErr.Code != access.ErrCodeRoleNotFound {
			t.Fatalf("concurrent ReplaceUserRoles error = %v, want ACCESS_ROLE_NOT_FOUND", assignErr)
		}
	}

	// Concurrent ReplaceUserRoles calls for the same user must serialize via per-user advisory lock
	// so the final role assignment is one complete role set rather than a merged union.
	roleAlpha, err := svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code:        "replace_alpha",
		Name:        "Replace Alpha",
		Permissions: []string{access.PermDashboardRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(replace_alpha) error = %v", err)
	}
	roleBeta, err := svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code:        "replace_beta",
		Name:        "Replace Beta",
		Permissions: []string{access.PermFilesRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(replace_beta) error = %v", err)
	}
	replaceBarrier := make(chan struct{})
	var replaceWG sync.WaitGroup
	replaceErrs := make([]error, 2)
	replaceWG.Add(2)
	go func() {
		defer replaceWG.Done()
		<-replaceBarrier
		replaceErrs[0] = svc.ReplaceUserRoles(t.Context(), userTwoID, []uuid.UUID{roleAlpha.ID})
	}()
	go func() {
		defer replaceWG.Done()
		<-replaceBarrier
		replaceErrs[1] = svc.ReplaceUserRoles(t.Context(), userTwoID, []uuid.UUID{roleBeta.ID})
	}()
	close(replaceBarrier)
	replaceWG.Wait()
	if replaceErrs[0] != nil || replaceErrs[1] != nil {
		t.Fatalf("concurrent ReplaceUserRoles errors = %v", replaceErrs)
	}
	uaAfterReplace, err := svc.GetUserAccess(t.Context(), userTwoID)
	if err != nil {
		t.Fatalf("GetUserAccess(userTwoID) error = %v", err)
	}
	if !slices.Equal(uaAfterReplace.Roles, []string{roleAlpha.Code}) && !slices.Equal(uaAfterReplace.Roles, []string{roleBeta.Code}) {
		t.Fatalf("concurrent ReplaceUserRoles produced merged/corrupted roles = %v", uaAfterReplace.Roles)
	}

	// Deleting role while assigned to userID must fail with ROLES_IN_USE.
	err = svc.DeleteRole(t.Context(), access.DeleteRoleInput{RoleID: role.ID, ActorID: &userID})
	if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeRoleInUse {
		t.Fatalf("DeleteRole(in-use) error = %v, want ROLES_IN_USE", err)
	}

	// Search with Chinese system role name.
	zhSuper := bundle.Translate(i18n.LangZhCN, "ACCESS_ROLE_SUPER_ADMIN_NAME", nil)
	listZh, err := svc.ListRoles(t.Context(), access.RoleListFilter{
		Pagination: pagination.Normalize(1, 20),
		Q:          zhSuper,
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

func TestAccessMenusPostgresIntegration(t *testing.T) {
	pool := openIsolatedTestPool(t)
	txMgr := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()
	bundle := newTestBundle(t)

	svc := access.NewService(pool, access.ServiceDeps{
		Tx:    txMgr,
		Cache: mem,
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	// Customize a system menu's icon/visibility and re-run SyncCatalog: customizations and bilingual title_key must survive.
	tree, err := svc.ListMenuTree(t.Context())
	if err != nil || len(tree) < 2 {
		t.Fatalf("ListMenuTree() = %+v, err = %v", tree, err)
	}
	sysGeneral := tree[0]
	zhTitle := bundle.Translate(i18n.LangZhCN, sysGeneral.TitleKey, nil)
	customIcon := "Compass"
	hidden := false
	patchedSys, err := svc.UpdateMenu(t.Context(), access.UpdateMenuInput{
		MenuID:  sysGeneral.ID,
		Title:   &zhTitle,
		Icon:    &customIcon,
		Visible: &hidden,
	})
	if err != nil {
		t.Fatalf("UpdateMenu(sysGeneral) error = %v", err)
	}
	if patchedSys.Title != "" || patchedSys.Icon != customIcon || patchedSys.Visible {
		t.Fatalf("unexpected patched system menu: %+v", patchedSys)
	}
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("second SyncCatalog() error = %v", err)
	}
	reloadedTree, err := svc.ListMenuTree(t.Context())
	if err != nil {
		t.Fatalf("ListMenuTree() after resync error = %v", err)
	}
	if reloadedTree[0].Icon != customIcon || reloadedTree[0].Visible || reloadedTree[0].Title != "" {
		t.Fatalf("SyncCatalog overwrote system menu customizations: %+v", reloadedTree[0])
	}

	// Unique code constraint, child delete protection, and ON DELETE SET NULL on permissions FK.
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO permissions (code, module, name_key, description_key)
		VALUES ('temp:perm', 'access', 'TEMP_NAME', 'TEMP_DESC')
	`); err != nil {
		t.Fatalf("seed temp permission: %v", err)
	}

	dirA, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		Type:      access.MenuTypeDirectory,
		Code:      "it_dir_a",
		Title:     "Dir A",
		SortOrder: 50,
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(dirA): %v", err)
	}
	dirB, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		Type:      access.MenuTypeDirectory,
		Code:      "it_dir_b",
		Title:     "Dir B",
		SortOrder: 60,
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(dirB): %v", err)
	}

	// Duplicate menu code -> 409 MENUS_CODE_CONFLICT.
	if _, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		Type:    access.MenuTypeDirectory,
		Code:    "it_dir_a",
		Title:   "Dup Dir A",
		Visible: true,
	}); err == nil {
		t.Fatal("expected MENUS_CODE_CONFLICT")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeMenuCodeConflict {
		t.Fatalf("duplicate menu code err = %v, want MENUS_CODE_CONFLICT", err)
	}

	// Verify ON DELETE SET NULL on menus.permission_code when a deprecated permission row is removed.
	leafID := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO menus (id, parent_id, type, code, title, path, permission_code, visible, status)
		VALUES ($1, $2, 'menu', 'it_leaf_perm', 'Leaf Perm', '/it-leaf', 'temp:perm', true, 'active')
	`, leafID, dirA.ID); err != nil {
		t.Fatalf("insert leaf menu with temp:perm: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM permissions WHERE code = 'temp:perm'`); err != nil {
		t.Fatalf("delete temp:perm: %v", err)
	}
	var nullablePerm *string
	if err := pool.QueryRow(t.Context(), `SELECT permission_code FROM menus WHERE id = $1`, leafID).Scan(&nullablePerm); err != nil {
		t.Fatalf("scan leaf permission_code: %v", err)
	}
	if nullablePerm != nil {
		t.Fatalf("expected permission_code SET NULL after permission delete, got %v", *nullablePerm)
	}

	// DeleteMenu on dirA while leaf exists -> 409 MENUS_HAS_CHILDREN; after deleting leaf, dirA can stay for concurrency test.
	if err := svc.DeleteMenu(t.Context(), dirA.ID, nil, access.RequestMeta{}); err == nil {
		t.Fatal("expected MENUS_HAS_CHILDREN")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeMenuHasChildren {
		t.Fatalf("DeleteMenu(dirA with child) err = %v, want MENUS_HAS_CHILDREN", err)
	}
	if err := svc.DeleteMenu(t.Context(), leafID, nil, access.RequestMeta{}); err != nil {
		t.Fatalf("DeleteMenu(leafID) err = %v", err)
	}

	// ReorderMenus rolls back earlier updates when a subsequent item is missing.
	missingID := uuid.Must(uuid.NewV7())
	_, err = svc.ReorderMenus(t.Context(), access.ReorderMenusInput{
		Items: []access.ReorderMenuItem{
			{ID: dirA.ID, ParentID: nil, SortOrder: 999},
			{ID: missingID, ParentID: nil, SortOrder: 1},
		},
	})
	if appErr, ok := apperr.As(err); !ok || appErr.Code != access.ErrCodeMenuNotFound {
		t.Fatalf("ReorderMenus(missing) err = %v, want MENUS_NOT_FOUND", err)
	}
	var dirASort int32
	if err := pool.QueryRow(t.Context(), `SELECT sort_order FROM menus WHERE id = $1`, dirA.ID).Scan(&dirASort); err != nil {
		t.Fatalf("query dirA sort_order: %v", err)
	}
	if dirASort != 50 {
		t.Fatalf("ReorderMenus did not roll back sort_order: got %d, want 50", dirASort)
	}

	// Concurrent mutual reparenting (A -> parent B vs B -> parent A) must be serialized by
	// pg_advisory_xact_lock(menuTreeAdvisoryLockKey) so exactly one succeeds and one returns 409 MENUS_CYCLE_DETECTED.
	barrier := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-barrier
		_, errs[0] = svc.UpdateMenu(t.Context(), access.UpdateMenuInput{
			MenuID:   dirA.ID,
			ParentID: &dirB.ID,
		})
	}()
	go func() {
		defer wg.Done()
		<-barrier
		_, errs[1] = svc.UpdateMenu(t.Context(), access.UpdateMenuInput{
			MenuID:   dirB.ID,
			ParentID: &dirA.ID,
		})
	}()
	close(barrier)
	wg.Wait()

	successCount := 0
	cycleCount := 0
	for _, e := range errs {
		if e == nil {
			successCount++
			continue
		}
		if appErr, ok := apperr.As(e); ok && appErr.Code == access.ErrCodeMenuCycleDetected {
			cycleCount++
		} else {
			t.Fatalf("unexpected concurrent UpdateMenu error: %v", e)
		}
	}
	if successCount != 1 || cycleCount != 1 {
		t.Fatalf("concurrent mutual reparent results: success=%d cycle=%d (errs=%v); want 1 and 1", successCount, cycleCount, errs)
	}

	// End-to-end ListUserMenuTree over real PostgreSQL: verifies SQL sort_order, disabled/hidden/button filtering,
	// empty directory pruning, and immediate cache invalidation after role permission update.
	var menuUserID uuid.UUID
	if err := pool.QueryRow(t.Context(), `
		INSERT INTO users (id, username, email, password_hash, display_name, status)
		VALUES ($1, 'menu_user', 'menu_user@example.com', 'hash', 'Menu User', 'active')
		RETURNING id
	`, uuid.Must(uuid.NewV7())).Scan(&menuUserID); err != nil {
		t.Fatalf("insert menu_user: %v", err)
	}
	menuRole, err := svc.CreateRole(t.Context(), access.CreateRoleInput{
		Code:        "menu_viewer",
		Name:        "Menu Viewer",
		Permissions: []string{access.PermUsersRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(menu_viewer): %v", err)
	}
	if err := svc.AssignRoleByCode(t.Context(), menuUserID, menuRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode(menu_viewer): %v", err)
	}

	customDir, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		Type:      access.MenuTypeDirectory,
		Code:      "it_nav_dir",
		Title:     "Nav Dir",
		SortOrder: 5,
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(customDir): %v", err)
	}
	emptyDir, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		Type:      access.MenuTypeDirectory,
		Code:      "it_empty_dir",
		Title:     "Empty Dir",
		SortOrder: 6,
		Visible:   true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(emptyDir): %v", err)
	}
	if _, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		ParentID:       &emptyDir.ID,
		Type:           access.MenuTypeMenu,
		Code:           "it_hidden_leaf",
		Title:          "Hidden Leaf",
		Path:           "/hidden",
		PermissionCode: access.PermUsersRead,
		SortOrder:      1,
		Visible:        false,
	}); err != nil {
		t.Fatalf("CreateMenu(hidden): %v", err)
	}
	if _, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		ParentID:       &customDir.ID,
		Type:           access.MenuTypeMenu,
		Code:           "it_disabled_leaf",
		Title:          "Disabled Leaf",
		Path:           "/disabled",
		PermissionCode: access.PermUsersRead,
		SortOrder:      1,
		Visible:        true,
		Status:         access.MenuStatusDisabled,
	}); err != nil {
		t.Fatalf("CreateMenu(disabled): %v", err)
	}
	activeLeaf, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		ParentID:       &customDir.ID,
		Type:           access.MenuTypeMenu,
		Code:           "it_active_leaf",
		Title:          "Active Leaf",
		Path:           "/active",
		PermissionCode: access.PermUsersRead,
		SortOrder:      2,
		Visible:        true,
	})
	if err != nil {
		t.Fatalf("CreateMenu(activeLeaf): %v", err)
	}
	if _, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		ParentID:       &activeLeaf.ID,
		Type:           access.MenuTypeButton,
		Code:           "it_active_btn",
		Title:          "Active Btn",
		PermissionCode: access.PermUsersRead,
		Visible:        true,
	}); err != nil {
		t.Fatalf("CreateMenu(button): %v", err)
	}
	if _, err := svc.CreateMenu(t.Context(), access.CreateMenuInput{
		ParentID:       &customDir.ID,
		Type:           access.MenuTypeMenu,
		Code:           "it_audit_leaf",
		Title:          "Audit Leaf",
		Path:           "/audit-custom",
		PermissionCode: access.PermAuditRead,
		SortOrder:      1,
		Visible:        true,
	}); err != nil {
		t.Fatalf("CreateMenu(auditLeaf): %v", err)
	}

	userTree, err := svc.ListUserMenuTree(t.Context(), menuUserID)
	if err != nil {
		t.Fatalf("ListUserMenuTree() error = %v", err)
	}
	// First root should be customDir (sortOrder=5); emptyDir (only hidden child) must be pruned.
	if len(userTree) == 0 || userTree[0].Code != customDir.Code {
		t.Fatalf("unexpected first user menu root: %+v", userTree)
	}
	if len(userTree[0].Children) != 1 || userTree[0].Children[0].Code != activeLeaf.Code || len(userTree[0].Children[0].Children) != 0 {
		t.Fatalf("expected only activeLeaf with button pruned, got %+v", userTree[0].Children)
	}
	for _, root := range userTree {
		if root.Code == emptyDir.Code {
			t.Fatalf("empty directory %s should have been pruned", emptyDir.Code)
		}
	}

	// Grant audit:read to menuRole and verify ListUserMenuTree immediately reflects the newly permitted node sorted ahead (sortOrder=1 < 2).
	expandedPerms := []string{access.PermUsersRead, access.PermAuditRead}
	if _, err := svc.UpdateRole(t.Context(), access.UpdateRoleInput{
		RoleID:      menuRole.ID,
		Permissions: &expandedPerms,
	}); err != nil {
		t.Fatalf("UpdateRole(expand perms): %v", err)
	}
	updatedUserTree, err := svc.ListUserMenuTree(t.Context(), menuUserID)
	if err != nil {
		t.Fatalf("ListUserMenuTree() after UpdateRole error = %v", err)
	}
	if len(updatedUserTree[0].Children) != 2 || updatedUserTree[0].Children[0].Code != "it_audit_leaf" || updatedUserTree[0].Children[1].Code != activeLeaf.Code {
		t.Fatalf("unexpected user menu children after permission expansion: %+v", updatedUserTree[0].Children)
	}
}
