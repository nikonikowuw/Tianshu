package identity_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/platform/storage"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
	"tianshu/migrations"
)

type accessAdapter struct {
	svc *access.Service
}

func (a *accessAdapter) ResolveRoles(ctx context.Context, ids []uuid.UUID) ([]identity.RoleRef, error) {
	refs, err := a.svc.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	return mapRoleRefs(refs), nil
}

func (a *accessAdapter) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]identity.RoleRef, error) {
	m, err := a.svc.ListRolesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]identity.RoleRef, len(m))
	for uid, refs := range m {
		out[uid] = mapRoleRefs(refs)
	}
	return out, nil
}

func (a *accessAdapter) ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	return a.svc.ListUserIDsByRoleIDs(ctx, roleIDs)
}

func (a *accessAdapter) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	return a.svc.ListUserIDsByRoleCode(ctx, code)
}

func (a *accessAdapter) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return a.svc.ReplaceUserRoles(ctx, userID, roleIDs)
}

func mapRoleRefs(in []access.RoleRef) []identity.RoleRef {
	out := make([]identity.RoleRef, 0, len(in))
	for _, r := range in {
		out = append(out, identity.RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey,
			IsSystem: r.IsSystem,
		})
	}
	return out
}

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

	schema := "identity_it_" + strings.ReplaceAll(uuid.NewString(), "-", "")
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
		t.Fatalf("migrate schema: %v", err)
	}
	return pool
}

func newTestBundle(t *testing.T) *i18n.Bundle {
	t.Helper()
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	if err := bundle.RegisterFS("access", access.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(access): %v", err)
	}
	if err := bundle.RegisterFS("identity", identity.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity): %v", err)
	}
	if err := bundle.RegisterFS("auth", auth.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(auth): %v", err)
	}
	if err := bundle.RegisterFS("files", files.LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(files): %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle.Validate: %v", err)
	}
	return bundle
}

func TestIdentityConcurrentLastSuperAdminAndSoftDeleteRebuild(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()
	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()
	bundle := newTestBundle(t)

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	jwtMgr, err := security.NewJWTManager("integration-test-jwt-secret-at-least-32-bytes", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	accessSvc := access.NewService(pool, access.ServiceDeps{
		Tx:    tx,
		Cache: mem,
		I18n:  bundle,
		Log:   log,
	})
	if err := accessSvc.SyncCatalog(ctx); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	sessAdapter := &authSessionAdapter{}
	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:       tx,
		Roles:    &accessAdapter{svc: accessSvc},
		Sessions: sessAdapter,
		Log:      log,
	})

	authSvc := auth.NewService(config.Default().Auth, pool, auth.ServiceDeps{
		Identity: &authIdentityBridge{svc: idSvc},
		Access:   &authAccessBridge{svc: accessSvc},
		Hasher:   hasher,
		JWT:      jwtMgr,
		Tx:       tx,
		Cache:    mem,
		Log:      log,
	})
	sessAdapter.svc = authSvc

	u1, err := idSvc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username: "admin1",
		Email:    "admin1@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin(1): %v", err)
	}
	if err := accessSvc.AssignRoleByCode(ctx, u1.ID, access.RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode(1): %v", err)
	}

	login1, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "admin1", Password: "password123", ClientIP: "127.0.0.1", UserAgent: "A1"})
	if err != nil {
		t.Fatalf("Login(admin1): %v", err)
	}
	claims1, err := jwtMgr.Verify(login1.AccessToken)
	if err != nil {
		t.Fatalf("Verify(admin1): %v", err)
	}

	opts, err := accessSvc.ListRoleOptions(ctx)
	if err != nil {
		t.Fatalf("ListRoleOptions: %v", err)
	}
	var superRoleID uuid.UUID
	for _, o := range opts {
		if o.Code == access.RoleCodeSuperAdmin {
			superRoleID = o.ID
		}
	}

	u2, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "admin2",
		Email:          "admin2@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{superRoleID},
		ActorID:        &u1.ID,
		ActorSessionID: claims1.SessionID,
	})
	if err != nil {
		t.Fatalf("CreateUser(admin2): %v", err)
	}
	login2, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "admin2", Password: "password123", ClientIP: "127.0.0.2", UserAgent: "A2"})
	if err != nil {
		t.Fatalf("Login(admin2): %v", err)
	}
	claims2, err := jwtMgr.Verify(login2.AccessToken)
	if err != nil {
		t.Fatalf("Verify(admin2): %v", err)
	}

	// Concurrent mutual disable under advisory lock: exactly 1 succeeds, the loser is rejected (disabled/unauthorized/last super)
	// and exactly 1 active super_admin remains.
	start := make(chan struct{})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	disabled := identity.StatusDisabled
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		_, errs[0] = idSvc.UpdateUser(ctx, identity.UpdateUserInput{
			UserID:         u1.ID,
			Status:         &disabled,
			ActorID:        &u2.ID,
			ActorSessionID: claims2.SessionID,
		})
	}()
	go func() {
		defer wg.Done()
		<-start
		_, errs[1] = idSvc.UpdateUser(ctx, identity.UpdateUserInput{
			UserID:         u2.ID,
			Status:         &disabled,
			ActorID:        &u1.ID,
			ActorSessionID: claims1.SessionID,
		})
	}()
	close(start)
	wg.Wait()

	var okCount, rejectedCount int
	for _, e := range errs {
		if e == nil {
			okCount++
			continue
		}
		appErr, ok := apperr.As(e)
		if !ok {
			t.Fatalf("expected *apperr.Error, got %v", e)
		}
		if appErr.Code == "AUTH_ACCOUNT_DISABLED" || appErr.Code == apperr.CodeUnauthorized || appErr.Code == identity.ErrCodeLastSuperAdmin {
			rejectedCount++
		} else {
			t.Fatalf("unexpected concurrent error: %v", e)
		}
	}
	if okCount != 1 || rejectedCount != 1 {
		t.Fatalf("expected 1 success and 1 rejection, got ok=%d rejected=%d errs=%v", okCount, rejectedCount, errs)
	}

	// Determine the surviving active super_admin and disabled loser.
	var (
		activeID, disabledID     uuid.UUID
		activeSess, disabledSess uuid.UUID
	)
	if errs[0] == nil {
		// u2 disabled u1
		activeID, activeSess = u2.ID, claims2.SessionID
		disabledID, disabledSess = u1.ID, claims1.SessionID
	} else {
		// u1 disabled u2
		activeID, activeSess = u1.ID, claims1.SessionID
		disabledID, disabledSess = u2.ID, claims2.SessionID
	}

	// Disabled loser cannot self-re-enable or mint a backdoor super_admin with its residual AT/session.
	active := identity.StatusActive
	if _, err := idSvc.UpdateUser(ctx, identity.UpdateUserInput{
		UserID:         disabledID,
		Status:         &active,
		ActorID:        &disabledID,
		ActorSessionID: disabledSess,
	}); err == nil {
		t.Fatal("disabled admin must not self-re-enable")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != "AUTH_ACCOUNT_DISABLED" {
		t.Fatalf("disabled self-re-enable = %v, want AUTH_ACCOUNT_DISABLED", err)
	}

	if _, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "backdoor",
		Email:          "backdoor@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{superRoleID},
		ActorID:        &disabledID,
		ActorSessionID: disabledSess,
	}); err == nil {
		t.Fatal("disabled admin must not create backdoor super_admin")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != "AUTH_ACCOUNT_DISABLED" {
		t.Fatalf("disabled backdoor create = %v, want AUTH_ACCOUNT_DISABLED", err)
	}

	// Soft-delete an ordinary user with a role & active session, verify both are cleared, and recreate with same username/email.
	customRole, err := accessSvc.CreateRole(ctx, access.CreateRoleInput{
		Code:        "temp_role",
		Name:        "Temp Role",
		Permissions: []string{"users:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	alice, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "alice",
		Email:          "alice@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{customRole.ID},
		ActorID:        &activeID,
		ActorSessionID: activeSess,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	aliceLogin, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "alice", Password: "password123", ClientIP: "10.0.0.5", UserAgent: "AliceUA"})
	if err != nil {
		t.Fatalf("login alice: %v", err)
	}

	if err := idSvc.DeleteUser(ctx, identity.DeleteUserInput{
		UserID:         alice.ID,
		ActorID:        &activeID,
		ActorSessionID: activeSess,
	}); err != nil {
		t.Fatalf("delete alice: %v", err)
	}
	// Active session must be revoked on soft delete.
	if _, err := authSvc.Refresh(ctx, auth.RefreshInput{RefreshToken: aliceLogin.RefreshToken}); err == nil {
		t.Fatal("expected soft-deleted user's refresh token to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("expected 401 on soft-deleted refresh, got %v", err)
	}
	// Clearing user_roles on soft delete releases ROLES_IN_USE so customRole can be deleted.
	if err := accessSvc.DeleteRole(ctx, access.DeleteRoleInput{RoleID: customRole.ID}); err != nil {
		t.Fatalf("expected custom role deletable after user soft delete, got %v", err)
	}

	alice2, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "alice",
		Email:          "alice@example.com",
		Password:       "password123",
		ActorID:        &activeID,
		ActorSessionID: activeSess,
	})
	if err != nil {
		t.Fatalf("recreate alice after soft delete: %v", err)
	}
	if alice2.ID == alice.ID {
		t.Fatalf("expected new UUIDv7 for recreated user")
	}

	// Concurrent mutual DeleteUser between active super_admins must leave exactly 1 active super_admin.
	if err := idSvc.DeleteUser(ctx, identity.DeleteUserInput{
		UserID:         disabledID,
		ActorID:        &activeID,
		ActorSessionID: activeSess,
	}); err != nil {
		t.Fatalf("cleanup disabled super_admin: %v", err)
	}
	ds2, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "super_del2",
		Email:          "super_del2@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{superRoleID},
		ActorID:        &activeID,
		ActorSessionID: activeSess,
	})
	if err != nil {
		t.Fatalf("create super_del2: %v", err)
	}
	ds2Login, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "super_del2", Password: "password123", ClientIP: "127.0.0.1", UserAgent: "Del2"})
	if err != nil {
		t.Fatalf("login super_del2: %v", err)
	}
	ds2Claims, err := jwtMgr.Verify(ds2Login.AccessToken)
	if err != nil {
		t.Fatalf("verify super_del2: %v", err)
	}

	delStart := make(chan struct{})
	var delWG sync.WaitGroup
	delErrs := make([]error, 2)
	delWG.Add(2)
	go func() {
		defer delWG.Done()
		<-delStart
		delErrs[0] = idSvc.DeleteUser(ctx, identity.DeleteUserInput{
			UserID:         activeID,
			ActorID:        &ds2.ID,
			ActorSessionID: ds2Claims.SessionID,
		})
	}()
	go func() {
		defer delWG.Done()
		<-delStart
		delErrs[1] = idSvc.DeleteUser(ctx, identity.DeleteUserInput{
			UserID:         ds2.ID,
			ActorID:        &activeID,
			ActorSessionID: activeSess,
		})
	}()
	close(delStart)
	delWG.Wait()

	var delOK, delRejected int
	for _, e := range delErrs {
		if e == nil {
			delOK++
			continue
		}
		appErr, ok := apperr.As(e)
		if !ok {
			t.Fatalf("expected *apperr.Error from concurrent DeleteUser, got %v", e)
		}
		if appErr.Code == apperr.CodeUnauthorized || appErr.Code == identity.ErrCodeLastSuperAdmin || appErr.Code == identity.ErrCodeSuperAdminProtect {
			delRejected++
		} else {
			t.Fatalf("unexpected concurrent DeleteUser error: %v", e)
		}
	}
	if delOK != 1 || delRejected != 1 {
		t.Fatalf("concurrent mutual DeleteUser: ok=%d rejected=%d errs=%v", delOK, delRejected, delErrs)
	}

	// Surviving super_admin + newly created third super_admin: concurrent DeleteUser vs role-demote UpdateUser.
	survivorID, survivorSess := activeID, activeSess
	if delErrs[0] == nil {
		survivorID, survivorSess = ds2.ID, ds2Claims.SessionID
	}
	ds3, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:       "super_del3",
		Email:          "super_del3@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{superRoleID},
		ActorID:        &survivorID,
		ActorSessionID: survivorSess,
	})
	if err != nil {
		t.Fatalf("create super_del3: %v", err)
	}
	ds3Login, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "super_del3", Password: "password123", ClientIP: "127.0.0.1", UserAgent: "Del3"})
	if err != nil {
		t.Fatalf("login super_del3: %v", err)
	}
	ds3Claims, err := jwtMgr.Verify(ds3Login.AccessToken)
	if err != nil {
		t.Fatalf("verify super_del3: %v", err)
	}

	raceStart := make(chan struct{})
	var raceWG sync.WaitGroup
	raceErrs := make([]error, 2)
	emptyRoles := []uuid.UUID{}
	raceWG.Add(2)
	go func() {
		defer raceWG.Done()
		<-raceStart
		raceErrs[0] = idSvc.DeleteUser(ctx, identity.DeleteUserInput{
			UserID:         survivorID,
			ActorID:        &ds3.ID,
			ActorSessionID: ds3Claims.SessionID,
		})
	}()
	go func() {
		defer raceWG.Done()
		<-raceStart
		_, raceErrs[1] = idSvc.UpdateUser(ctx, identity.UpdateUserInput{
			UserID:         ds3.ID,
			RoleIDs:        &emptyRoles,
			ActorID:        &survivorID,
			ActorSessionID: survivorSess,
		})
	}()
	close(raceStart)
	raceWG.Wait()

	var raceOK, raceRej int
	for _, e := range raceErrs {
		if e == nil {
			raceOK++
			continue
		}
		if _, ok := apperr.As(e); !ok {
			t.Fatalf("expected *apperr.Error in DeleteUser vs UpdateUser race, got %v", e)
		}
		raceRej++
	}
	if raceOK != 1 || raceRej != 1 {
		t.Fatalf("concurrent DeleteUser vs UpdateUser: ok=%d rej=%d errs=%v", raceOK, raceRej, raceErrs)
	}
}

type failingAuditRecorder struct {
	err error
}

func (f *failingAuditRecorder) Record(_ context.Context, _ identity.AuditEntry) error {
	return f.err
}

func TestIdentityUniqueConflictsListSQLAndTxRollbackPostgres(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()
	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()
	bundle := newTestBundle(t)

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}

	accessSvc := access.NewService(pool, access.ServiceDeps{
		Tx:    tx,
		Cache: mem,
		I18n:  bundle,
		Log:   log,
	})
	if err := accessSvc.SyncCatalog(ctx); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:    tx,
		Roles: &accessAdapter{svc: accessSvc},
		Log:   log,
	})

	admin, err := idSvc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username: "root_sql",
		Email:    "root.sql@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	if err := accessSvc.AssignRoleByCode(ctx, admin.ID, access.RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode: %v", err)
	}

	roleOps, err := accessSvc.CreateRole(ctx, access.CreateRoleInput{
		Code:        "ops_viewer",
		Name:        "Ops Viewer",
		Permissions: []string{"users:read"},
	})
	if err != nil {
		t.Fatalf("CreateRole: %v", err)
	}

	// 1. Real Postgres case-insensitive unique conflicts (23505 on uq_users_username_lower / uq_users_email_lower).
	if _, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username: "ROOT_SQL",
		Email:    "other@example.com",
		Password: "password123",
		ActorID:  &admin.ID,
	}); err == nil {
		t.Fatal("expected 409 username conflict")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != identity.ErrCodeUsernameConflict {
		t.Fatalf("expected %s, got %v", identity.ErrCodeUsernameConflict, err)
	}
	if _, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username: "distinct_user",
		Email:    "ROOT.SQL@EXAMPLE.COM",
		Password: "password123",
		ActorID:  &admin.ID,
	}); err == nil {
		t.Fatal("expected 409 email conflict")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != identity.ErrCodeEmailConflict {
		t.Fatalf("expected %s, got %v", identity.ErrCodeEmailConflict, err)
	}

	// 2. ListUsers SQL: ILIKE escaping (%, _, \), status/role filters, all 4 sort columns + NULLS LAST + default sort with order=asc.
	uSpecial, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:    "alpha_user",
		Email:       "alpha@example.com",
		DisplayName: `100%_match\display`,
		Password:    "password123",
		Status:      identity.StatusDisabled,
		RoleIDs:     []uuid.UUID{roleOps.ID},
		ActorID:     &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateUser(uSpecial): %v", err)
	}
	uLoggedIn, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username:    "zeta_user",
		Email:       "zeta@example.com",
		DisplayName: "Plain Display",
		Password:    "password123",
		ActorID:     &admin.ID,
	})
	if err != nil {
		t.Fatalf("CreateUser(uLoggedIn): %v", err)
	}
	// Mixed-case email lookup + CompleteLogin (with CAS rehash) in real Postgres transaction.
	zetaCreds, err := idSvc.GetAuthCredentials(ctx, "  ZETA@EXAMPLE.COM ")
	if err != nil || zetaCreds.ID != uLoggedIn.ID {
		t.Fatalf("GetAuthCredentials(mixed-case email) = (%+v, %v)", zetaCreds, err)
	}
	rehashed, err := hasher.Hash("password123")
	if err != nil {
		t.Fatalf("hasher.Hash: %v", err)
	}
	if err := tx.WithinTx(ctx, func(txCtx context.Context) error {
		return idSvc.CompleteLogin(txCtx, identity.CompleteLoginInput{
			UserID:               uLoggedIn.ID,
			ExpectedPasswordHash: zetaCreds.PasswordHash,
			UpgradedPasswordHash: rehashed,
		})
	}); err != nil {
		t.Fatalf("CompleteLogin: %v", err)
	}
	// Mismatched expected hash returns 401 without overwriting stored hash.
	if err := tx.WithinTx(ctx, func(txCtx context.Context) error {
		return idSvc.CompleteLogin(txCtx, identity.CompleteLoginInput{
			UserID:               uLoggedIn.ID,
			ExpectedPasswordHash: "wrong-hash",
			UpgradedPasswordHash: "should-not-apply",
		})
	}); err == nil {
		t.Fatal("expected 401 on mismatched CompleteLogin")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("expected 401 on mismatched CompleteLogin, got %v", err)
	}

	// Duplicate email on UpdateUser triggers real Postgres 23505 -> 409 USERS_EMAIL_CONFLICT.
	dupEmail := "ALPHA@EXAMPLE.COM"
	if _, err := idSvc.UpdateUser(ctx, identity.UpdateUserInput{
		UserID:  uLoggedIn.ID,
		Email:   &dupEmail,
		ActorID: &admin.ID,
	}); err == nil {
		t.Fatal("expected 409 email conflict on UpdateUser")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != identity.ErrCodeEmailConflict {
		t.Fatalf("expected %s, got %v", identity.ErrCodeEmailConflict, err)
	}

	// Escaped ILIKE must only match literal "100%_match\" and not "Plain Display".
	escaped, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Q:          `100%_match\`,
		Statuses:   []string{identity.StatusDisabled},
		RoleIDs:    []uuid.UUID{roleOps.ID},
	})
	if err != nil || escaped.Total != 1 || len(escaped.Items) != 1 || escaped.Items[0].ID != uSpecial.ID {
		t.Fatalf("escaped ListUsers = (%+v, %v), want only %s", escaped, err, uSpecial.ID)
	}
	if escaped.Items[0].CreatedBy == nil || escaped.Items[0].CreatedBy.Username != "root_sql" {
		t.Fatalf("expected LEFT JOIN createdBy projection, got %+v", escaped.Items[0].CreatedBy)
	}

	// Sort by username DESC, updated_at ASC/DESC, last_login_at ASC NULLS LAST, and DESC NULLS LAST.
	byUsernameDesc, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "username",
		Order:      "desc",
	})
	if err != nil || len(byUsernameDesc.Items) != 3 || byUsernameDesc.Items[0].Username != "zeta_user" || byUsernameDesc.Items[2].Username != "alpha_user" {
		t.Fatalf("sort username desc = (%+v, %v)", byUsernameDesc, err)
	}
	// Touch uSpecial so its updated_at is strictly newest, and verify updated_by LEFT JOIN.
	updatedDisplay := `100%_match\display`
	if _, err := idSvc.UpdateUser(ctx, identity.UpdateUserInput{
		UserID:      uSpecial.ID,
		DisplayName: &updatedDisplay,
		ActorID:     &admin.ID,
	}); err != nil {
		t.Fatalf("touch uSpecial updated_at: %v", err)
	}
	byUpdatedDesc, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "updated_at",
		Order:      "desc",
	})
	if err != nil || len(byUpdatedDesc.Items) != 3 || byUpdatedDesc.Items[0].ID != uSpecial.ID || byUpdatedDesc.Items[0].UpdatedBy == nil || byUpdatedDesc.Items[0].UpdatedBy.Username != "root_sql" {
		t.Fatalf("sort updated_at desc = (%+v, %v)", byUpdatedDesc, err)
	}
	byUpdatedAsc, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "updated_at",
		Order:      "asc",
	})
	if err != nil || len(byUpdatedAsc.Items) != 3 || byUpdatedAsc.Items[2].ID != uSpecial.ID {
		t.Fatalf("sort updated_at asc = (%+v, %v)", byUpdatedAsc, err)
	}

	byLogin, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "last_login_at",
		Order:      "desc",
	})
	if err != nil || len(byLogin.Items) != 3 || byLogin.Items[0].ID != uLoggedIn.ID {
		t.Fatalf("sort last_login_at desc (NULLS LAST) = (%+v, %v)", byLogin, err)
	}
	byLoginAsc, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "last_login_at",
		Order:      "asc",
	})
	if err != nil || len(byLoginAsc.Items) != 3 || byLoginAsc.Items[0].ID != uLoggedIn.ID ||
		byLoginAsc.Items[1].LastLoginAt != nil || byLoginAsc.Items[2].LastLoginAt != nil {
		t.Fatalf("sort last_login_at asc (NULLS LAST) = (%+v, %v)", byLoginAsc, err)
	}
	// Empty sort with order=asc sorts created_at ASC (admin created first).
	byDefaultAsc, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 20),
		Order:      "asc",
	})
	if err != nil || len(byDefaultAsc.Items) != 3 || byDefaultAsc.Items[0].ID != admin.ID {
		t.Fatalf("default sort order=asc = (%+v, %v), want admin first", byDefaultAsc, err)
	}

	// CountUsers, multi-page LIMIT/OFFSET pagination (pageSize=10 across 11 users), and ListUsernamesByIDs with soft-deleted creator.
	var extraIDs []uuid.UUID
	for i := range 8 {
		u, createErr := idSvc.CreateUser(ctx, identity.CreateUserInput{
			Username: fmt.Sprintf("page_user_%02d", i),
			Email:    fmt.Sprintf("page_user_%02d@example.com", i),
			Password: "password123",
			ActorID:  &admin.ID,
		})
		if createErr != nil {
			t.Fatalf("CreateUser(page_user_%02d): %v", i, createErr)
		}
		extraIDs = append(extraIDs, u.ID)
	}
	totalCount, err := idSvc.CountUsers(ctx)
	if err != nil || totalCount != 11 {
		t.Fatalf("CountUsers = (%d, %v), want 11", totalCount, err)
	}
	page1, err := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(1, 10),
		Sort:       "username",
		Order:      "asc",
	})
	page2, err2 := idSvc.ListUsers(ctx, identity.UserListFilter{
		Pagination: pagination.Normalize(2, 10),
		Sort:       "username",
		Order:      "asc",
	})
	if err != nil || err2 != nil || page1.Total != 11 || len(page1.Items) != 10 || page2.Total != 11 || len(page2.Items) != 1 || page2.Items[0].Username != "zeta_user" {
		t.Fatalf("multi-page ListUsers failed: p1=(%+v, %v) p2=(%+v, %v)", page1, err, page2, err2)
	}
	// Soft-delete one extra user and confirm ListUsernamesByIDs retains its username.
	if err := idSvc.DeleteUser(ctx, identity.DeleteUserInput{
		UserID:  extraIDs[0],
		ActorID: &admin.ID,
	}); err != nil {
		t.Fatalf("DeleteUser(extra): %v", err)
	}
	namesMap, err := idSvc.ListUsernamesByIDs(ctx, []uuid.UUID{admin.ID, extraIDs[0]})
	if err != nil || namesMap[admin.ID] != "root_sql" || namesMap[extraIDs[0]] != "page_user_00" {
		t.Fatalf("ListUsernamesByIDs = (%+v, %v)", namesMap, err)
	}

	// 3. Cross-module tx rollback: prime access cache, then trigger failing AuditRecorder inside UpdateUser.
	// Neither users/user_roles changes nor AfterCommit cache invalidation may persist.
	if _, err := accessSvc.GetUserAccess(ctx, uSpecial.ID); err != nil {
		t.Fatalf("prime GetUserAccess cache: %v", err)
	}
	rollbackSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:    tx,
		Roles: &accessAdapter{svc: accessSvc},
		Audit: &failingAuditRecorder{err: errors.New("audit write failure")},
		Log:   log,
	})
	newDisplay := "Should Rollback"
	emptyRoles := []uuid.UUID{}
	if _, err := rollbackSvc.UpdateUser(ctx, identity.UpdateUserInput{
		UserID:      uSpecial.ID,
		DisplayName: &newDisplay,
		RoleIDs:     &emptyRoles,
		ActorID:     &admin.ID,
	}); err == nil {
		t.Fatal("expected rollback error from failing AuditRecorder")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 500 || appErr.Code != apperr.CodeInternal {
		t.Fatalf("expected 500 INTERNAL_ERROR from failing AuditRecorder, got %v", err)
	}
	reloaded, err := idSvc.GetUser(ctx, uSpecial.ID)
	if err != nil {
		t.Fatalf("GetUser after rollback: %v", err)
	}
	if reloaded.DisplayName != `100%_match\display` || len(reloaded.Roles) != 1 || reloaded.Roles[0].ID != roleOps.ID {
		t.Fatalf("expected users and user_roles rolled back cleanly, got %+v", reloaded)
	}
	if _, cacheErr := mem.Get(ctx, "access:user:"+uSpecial.ID.String()); cacheErr != nil {
		t.Fatalf("AfterCommit cache invalidation must not fire on rolled-back tx: err=%v", cacheErr)
	}
}

type authSessionAdapter struct {
	svc *auth.Service
}

func (a *authSessionAdapter) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	return a.svc.RevokeAllUserSessions(ctx, userID)
}

func (a *authSessionAdapter) ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]identity.UserSession, error) {
	items, err := a.svc.ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]identity.UserSession, 0, len(items))
	for _, s := range items {
		out = append(out, identity.UserSession{
			ID:         s.ID,
			IPAddress:  s.IPAddress,
			UserAgent:  s.UserAgent,
			LastUsedAt: s.LastUsedAt,
			CreatedAt:  s.CreatedAt,
			ExpiresAt:  s.ExpiresAt,
		})
	}
	return out, nil
}

func (a *authSessionAdapter) IsSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.IsSessionActive(ctx, userID, sessionID)
}

func (a *authSessionAdapter) LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.LockActiveSessionForShare(ctx, userID, sessionID)
}

func (a *authSessionAdapter) RevokeUserSession(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return a.svc.RevokeUserSession(ctx, userID, sessionID)
}

func (a *authSessionAdapter) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	return a.svc.RevokeOtherUserSessions(ctx, userID, keepSessionID)
}

type filesAvatarAdapter struct {
	svc *files.Service
}

func (a *filesAvatarAdapter) ValidateAndLockAvatarFile(ctx context.Context, fileID, ownerID uuid.UUID) error {
	err := a.svc.ValidateAndLockAvatarFile(ctx, fileID, ownerID)
	if appErr, ok := apperr.As(err); ok && appErr.Code == files.ErrCodeNotFound {
		return identity.ErrInvalidAvatarFile
	}
	return err
}

func (a *filesAvatarAdapter) SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error) {
	return a.svc.SignAvatarURL(ctx, fileID)
}

type authIdentityBridge struct {
	svc *identity.Service
}

func (b *authIdentityBridge) GetAuthCredentials(ctx context.Context, identifier string) (auth.UserCredentials, error) {
	creds, err := b.svc.GetAuthCredentials(ctx, identifier)
	if err != nil {
		return auth.UserCredentials{}, err
	}
	return auth.UserCredentials{
		User: auth.User{
			ID:           creds.ID,
			Username:     creds.Username,
			Email:        creds.Email,
			DisplayName:  creds.DisplayName,
			AvatarFileID: creds.AvatarFileID,
			Status:       creds.Status,
		},
		PasswordHash: creds.PasswordHash,
	}, nil
}

func (b *authIdentityBridge) GetByID(ctx context.Context, userID uuid.UUID) (auth.User, error) {
	u, err := b.svc.GetByID(ctx, userID)
	if err != nil {
		return auth.User{}, err
	}
	return auth.User{
		ID:           u.ID,
		Username:     u.Username,
		Email:        u.Email,
		DisplayName:  u.DisplayName,
		AvatarFileID: u.AvatarFileID,
		Status:       u.Status,
	}, nil
}

func (b *authIdentityBridge) CompleteLogin(ctx context.Context, input auth.CompleteLoginInput) error {
	return b.svc.CompleteLogin(ctx, identity.CompleteLoginInput{
		UserID:               input.UserID,
		ExpectedPasswordHash: input.ExpectedPasswordHash,
		UpgradedPasswordHash: input.UpgradedPasswordHash,
	})
}

type authAccessBridge struct {
	svc *access.Service
}

func (b *authAccessBridge) GetUserAccess(ctx context.Context, userID uuid.UUID) (auth.UserAccess, error) {
	ua, err := b.svc.GetUserAccess(ctx, userID)
	if err != nil {
		return auth.UserAccess{}, err
	}
	return auth.UserAccess{Roles: ua.Roles, Permissions: ua.Permissions}, nil
}

type filesIdentityBridge struct {
	svc *identity.Service
}

func (b *filesIdentityBridge) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := b.svc.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func (b *filesIdentityBridge) ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	return b.svc.ListUsernamesByIDs(ctx, userIDs)
}

func (b *filesIdentityBridge) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	return b.svc.IsAvatarFileInUse(ctx, fileID)
}

type identityAuditBridge struct {
	svc *audit.Service
}

func (b *identityAuditBridge) Record(ctx context.Context, entry identity.AuditEntry) error {
	return b.svc.Record(ctx, audit.RecordInput{
		ActorID:       entry.ActorID,
		ActorUsername: entry.ActorUsername,
		Action:        entry.Action,
		Resource:      entry.Resource,
		ResourceID:    entry.ResourceID,
		Status:        entry.Status,
		IPAddress:     entry.IPAddress,
		UserAgent:     entry.UserAgent,
		Metadata:      entry.Metadata,
	})
}

func makeSmallPNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := range 16 {
		for x := range 16 {
			img.Set(x, y, color.RGBA{R: 64, G: 128, B: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func TestProfileAvatarLockPasswordAndSessionInvalidationPostgres(t *testing.T) {
	pool := openIsolatedTestPool(t)
	ctx := t.Context()
	log := logger.New("test", "error", io.Discard)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()
	bundle := newTestBundle(t)

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}
	jwtMgr, err := security.NewJWTManager("integration-test-jwt-secret-at-least-32-bytes", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager: %v", err)
	}

	auditSvc := audit.NewService(pool)
	sessAdapter := &authSessionAdapter{}
	filesAdapter := &filesAvatarAdapter{}
	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:       tx,
		Sessions: sessAdapter,
		Files:    filesAdapter,
		Audit:    &identityAuditBridge{svc: auditSvc},
		Log:      log,
	})

	accessSvc := access.NewService(pool, access.ServiceDeps{
		Tx:    tx,
		Cache: mem,
		I18n:  bundle,
		Log:   log,
	})
	if err := accessSvc.SyncCatalog(ctx); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	localStore, err := storage.NewLocal(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocal: %v", err)
	}
	reg, err := storage.NewRegistry(storage.DriverLocal, localStore)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	fileBridge := &filesIdentityBridge{svc: idSvc}
	fileSvc := files.NewService(pool, reg, files.ServiceConfig{
		SignedURLSecret: "integration-test-signed-secret-at-least-32b",
		SignedURLTTL:    5 * time.Minute,
	}, files.ServiceDeps{
		Tx:        tx,
		Actors:    fileBridge,
		AvatarRef: fileBridge,
		Log:       log,
	})
	filesAdapter.svc = fileSvc

	authSvc := auth.NewService(
		config.Default().Auth,
		pool,
		auth.ServiceDeps{
			Identity:     &authIdentityBridge{svc: idSvc},
			Access:       &authAccessBridge{svc: accessSvc},
			Hasher:       hasher,
			JWT:          jwtMgr,
			Tx:           tx,
			Cache:        mem,
			AvatarSigner: filesAdapter,
			Log:          log,
		},
	)
	sessAdapter.svc = authSvc

	user, err := idSvc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username:    "profileuser",
		Email:       "profile@example.com",
		DisplayName: "Profile User",
		Password:    "OldPassword123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}

	// 1. Login device 1, upload avatar, bind to profile (FOR SHARE lock), verify FILES_IN_USE protection & auth /me signed URL.
	login1, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.1",
		UserAgent:  "DeviceOne",
	})
	if err != nil {
		t.Fatalf("Login(1): %v", err)
	}
	claims1, err := jwtMgr.Verify(login1.AccessToken)
	if err != nil {
		t.Fatalf("Verify(1): %v", err)
	}

	avatarFile, err := fileSvc.UploadAvatar(ctx, files.UploadInput{
		Filename:   "avatar.png",
		Reader:     bytes.NewReader(makeSmallPNGBytes(t)),
		UploaderID: &user.ID,
	})
	if err != nil {
		t.Fatalf("UploadAvatar: %v", err)
	}

	prof, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		AvatarFileID:     &avatarFile.ID,
	})
	if err != nil {
		t.Fatalf("UpdateProfile bind avatar: %v", err)
	}
	if prof.AvatarURL == nil || !strings.HasPrefix(*prof.AvatarURL, "/api/v1/files/signed/") {
		t.Fatalf("expected signed avatarUrl, got %v", prof.AvatarURL)
	}

	// Bound avatar file cannot be deleted while referenced.
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarFile.ID, ActorID: &user.ID}); err == nil {
		t.Fatal("expected FILES_IN_USE deleting bound avatar")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Code != files.ErrCodeInUse {
		t.Fatalf("expected %s, got %v", files.ErrCodeInUse, err)
	}

	// /auth/me includes avatarUrl.
	me, err := authSvc.Me(ctx, user.ID, claims1.SessionID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.User.AvatarURL == nil || !strings.HasPrefix(*me.User.AvatarURL, "/api/v1/files/signed/") {
		t.Fatalf("Me() AvatarURL = %v, want signed URL", me.User.AvatarURL)
	}

	// Clear avatar releases reference so file deletion succeeds.
	if _, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		ClearAvatar:      true,
	}); err != nil {
		t.Fatalf("UpdateProfile clear avatar: %v", err)
	}
	if err := fileSvc.Delete(ctx, files.DeleteInput{FileID: avatarFile.ID, ActorID: &user.ID}); err != nil {
		t.Fatalf("Delete released avatar: %v", err)
	}

	// 1b. Concurrent avatar bind (FOR SHARE) vs file delete (FOR UPDATE) must serialize cleanly with zero dangling reference.
	raceAvatar, err := fileSvc.UploadAvatar(ctx, files.UploadInput{
		Filename:   "race.png",
		Reader:     bytes.NewReader(makeSmallPNGBytes(t)),
		UploaderID: &user.ID,
	})
	if err != nil {
		t.Fatalf("UploadAvatar(race): %v", err)
	}
	raceStart := make(chan struct{})
	var (
		raceWG          sync.WaitGroup
		bindErr, delErr error
	)
	raceWG.Add(2)
	go func() {
		defer raceWG.Done()
		<-raceStart
		_, bindErr = idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
			UserID:           user.ID,
			CurrentSessionID: claims1.SessionID,
			AvatarFileID:     &raceAvatar.ID,
		})
	}()
	go func() {
		defer raceWG.Done()
		<-raceStart
		delErr = fileSvc.Delete(ctx, files.DeleteInput{FileID: raceAvatar.ID, ActorID: &user.ID})
	}()
	close(raceStart)
	raceWG.Wait()

	switch {
	case bindErr == nil && delErr != nil:
		if appErr, ok := apperr.As(delErr); !ok || appErr.Code != files.ErrCodeInUse {
			t.Fatalf("when bind wins, delete must return %s, got %v", files.ErrCodeInUse, delErr)
		}
		if _, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
			UserID:           user.ID,
			CurrentSessionID: claims1.SessionID,
			ClearAvatar:      true,
		}); err != nil {
			t.Fatalf("clear race avatar: %v", err)
		}
	case delErr == nil && bindErr != nil:
		if appErr, ok := apperr.As(bindErr); !ok || appErr.Code != apperr.CodeValidationFailed {
			t.Fatalf("when delete wins, bind must return 422, got %v", bindErr)
		}
		latest, err := idSvc.GetProfile(ctx, user.ID, claims1.SessionID)
		if err != nil || latest.AvatarFileID != nil {
			t.Fatalf("expected no dangling avatar reference when delete wins, got (%+v, %v)", latest, err)
		}
	default:
		t.Fatalf("concurrent bind/delete must have exactly one winner: bindErr=%v delErr=%v", bindErr, delErr)
	}

	// 2. Multi-device sessions: kick device 2 -> refresh fails AND device 2 gets 401 on all /profile* endpoints.
	login2, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.2",
		UserAgent:  "DeviceTwo",
	})
	if err != nil {
		t.Fatalf("Login(2): %v", err)
	}
	claims2, err := jwtMgr.Verify(login2.AccessToken)
	if err != nil {
		t.Fatalf("Verify(2): %v", err)
	}

	sessions, err := idSvc.ListProfileSessions(ctx, user.ID, claims1.SessionID)
	if err != nil || len(sessions) != 2 || !sessions[0].IsCurrent {
		t.Fatalf("ListProfileSessions = (%+v, %v), want 2 with current first", sessions, err)
	}

	if err := idSvc.RevokeProfileSession(ctx, identity.RevokeSessionInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		SessionID:        claims2.SessionID,
	}); err != nil {
		t.Fatalf("RevokeProfileSession(2): %v", err)
	}
	if _, err := authSvc.Refresh(ctx, auth.RefreshInput{RefreshToken: login2.RefreshToken}); err == nil {
		t.Fatal("expected kicked device 2 refresh to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("expected 401 on kicked device 2 refresh, got %v", err)
	}

	// Revoked device 2 must NOT be able to view/modify profile or retaliate against device 1.
	assert401 := func(op string, err error) {
		t.Helper()
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != 401 {
			t.Fatalf("%s with revoked session = %v, want 401", op, err)
		}
	}
	_, err = idSvc.GetProfile(ctx, user.ID, claims2.SessionID)
	assert401("GetProfile", err)
	hackedName := "Hijacked"
	_, err = idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		DisplayName:      &hackedName,
	})
	assert401("UpdateProfile", err)
	_, err = idSvc.ListProfileSessions(ctx, user.ID, claims2.SessionID)
	assert401("ListProfileSessions", err)
	err = idSvc.RevokeProfileSession(ctx, identity.RevokeSessionInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		SessionID:        claims1.SessionID,
	})
	assert401("RevokeProfileSession(retaliation)", err)
	_, err = idSvc.RevokeOtherProfileSessions(ctx, identity.RevokeOtherSessionsInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
	})
	assert401("RevokeOtherProfileSessions(retaliation)", err)
	_, err = idSvc.ChangePassword(ctx, identity.ChangePasswordInput{
		UserID:           user.ID,
		CurrentSessionID: claims2.SessionID,
		CurrentPassword:  "OldPassword123",
		NewPassword:      "HijackedPass999",
	})
	assert401("ChangePassword(revoked)", err)

	// Device 1 must still be active and untouched.
	remaining, err := idSvc.ListProfileSessions(ctx, user.ID, claims1.SessionID)
	if err != nil || len(remaining) != 1 || remaining[0].ID != claims1.SessionID {
		t.Fatalf("device 1 session must remain active after retaliation attempt, got (%+v, %v)", remaining, err)
	}

	// 3. ChangePassword with revokeOtherSessions=true invalidates device 3 and updates login credential.
	login3, err := authSvc.Login(ctx, auth.LoginInput{
		Identifier: "profileuser",
		Password:   "OldPassword123",
		ClientIP:   "10.0.0.3",
		UserAgent:  "DeviceThree",
	})
	if err != nil {
		t.Fatalf("Login(3): %v", err)
	}

	res, err := idSvc.ChangePassword(ctx, identity.ChangePasswordInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		CurrentPassword:  "OldPassword123",
		NewPassword:      "NewPassword456",
	})
	if err != nil || res.RevokedSessions != 1 {
		t.Fatalf("ChangePassword = (%+v, %v), want 1 revoked", res, err)
	}
	if _, err := authSvc.Refresh(ctx, auth.RefreshInput{RefreshToken: login3.RefreshToken}); err == nil {
		t.Fatal("expected device 3 refresh to fail after ChangePassword")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("expected 401 on device 3 refresh after ChangePassword, got %v", err)
	}
	if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: "OldPassword123"}); err == nil {
		t.Fatal("expected old password login to fail")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
		t.Fatalf("expected 401 on old password login, got %v", err)
	}
	if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: "NewPassword456"}); err != nil {
		t.Fatalf("expected new password login to succeed, got %v", err)
	}

	// 4. Real Postgres UpdateOwnProfile email conflict + concurrent ChangePassword vs admin password reset serialization.
	otherUser, err := idSvc.CreateUser(ctx, identity.CreateUserInput{
		Username: "otherprofile",
		Email:    "other.profile@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateUser(otherprofile): %v", err)
	}
	conflictEmail := "OTHER.PROFILE@EXAMPLE.COM"
	if _, err := idSvc.UpdateProfile(ctx, identity.UpdateProfileInput{
		UserID:           user.ID,
		CurrentSessionID: claims1.SessionID,
		Email:            &conflictEmail,
	}); err == nil {
		t.Fatal("expected 409 email conflict on UpdateProfile")
	} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 409 || appErr.Code != identity.ErrCodeEmailConflict {
		t.Fatalf("expected 409 %s, got %v", identity.ErrCodeEmailConflict, err)
	}

	// Concurrent ChangePassword vs admin UpdateUser(password reset) on user.ID under FOR UPDATE row lock.
	_ = otherUser
	pwRaceStart := make(chan struct{})
	var pwWG sync.WaitGroup
	var chgErr, resetErr error
	adminResetPw := "AdminReset789"
	pwWG.Add(2)
	go func() {
		defer pwWG.Done()
		<-pwRaceStart
		_, chgErr = idSvc.ChangePassword(ctx, identity.ChangePasswordInput{
			UserID:           user.ID,
			CurrentSessionID: claims1.SessionID,
			CurrentPassword:  "NewPassword456",
			NewPassword:      "SelfChosen999",
		})
	}()
	go func() {
		defer pwWG.Done()
		<-pwRaceStart
		_, resetErr = idSvc.UpdateUser(ctx, identity.UpdateUserInput{
			UserID:   user.ID,
			Password: &adminResetPw,
		})
	}()
	close(pwRaceStart)
	pwWG.Wait()

	if resetErr != nil {
		t.Fatalf("admin password reset failed: %v", resetErr)
	}
	if chgErr != nil {
		// Admin reset committed first (or invalidated claims1 session before/inside lock); self-change must fail cleanly with 400 or 401 and NOT overwrite AdminReset789.
		appErr, ok := apperr.As(chgErr)
		if !ok || (appErr.Code != identity.ErrCodeProfileInvalidCurrentPassword && appErr.Status != 401) {
			t.Fatalf("unexpected concurrent ChangePassword error: %v", chgErr)
		}
		if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: adminResetPw}); err != nil {
			t.Fatalf("admin reset password should remain effective: %v", err)
		}
	} else {
		// ChangePassword committed first; admin reset ran second and revoked sessions, so AdminReset789 must be effective.
		if _, err := authSvc.Login(ctx, auth.LoginInput{Identifier: "profileuser", Password: adminResetPw}); err != nil {
			t.Fatalf("final admin reset password should be effective: %v", err)
		}
	}
}
