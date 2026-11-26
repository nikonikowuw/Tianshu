package identity_test

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/config"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
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

	dsn := os.Getenv("TIANSHU_TEST_DSN")
	if dsn == "" {
		dsn = config.Default().Database.DSN
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	if err := adminPool.Ping(ctx); err != nil {
		adminPool.Close()
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

func TestIdentityConcurrentLastSuperAdminAndSoftDeleteRebuild(t *testing.T) {
	pool := openIsolatedTestPool(t)
	tx := database.NewTxManager(pool)
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle: %v", err)
	}
	_ = bundle.RegisterFS("access", access.LocalesFS, "locales")
	_ = bundle.RegisterFS("identity", identity.LocalesFS, "locales")

	hasher, err := security.NewPasswordHasher(security.DefaultArgon2Params())
	if err != nil {
		t.Fatalf("NewPasswordHasher: %v", err)
	}

	accessSvc := access.NewService(pool, tx, mem, logger.New("test", "error", io.Discard), access.ServiceDeps{I18n: bundle})
	if err := accessSvc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog: %v", err)
	}

	idSvc := identity.NewService(pool, hasher, identity.ServiceDeps{
		Tx:    tx,
		Roles: &accessAdapter{svc: accessSvc},
		I18n:  bundle,
		Log:   logger.New("test", "error", io.Discard),
	})

	u1, err := idSvc.CreateInitialAdmin(t.Context(), identity.CreateInitialAdminInput{
		Username: "admin1",
		Email:    "admin1@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin(1): %v", err)
	}
	if err := accessSvc.AssignRoleByCode(t.Context(), u1.ID, access.RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode(1): %v", err)
	}

	opts, err := accessSvc.ListRoleOptions(t.Context())
	if err != nil {
		t.Fatalf("ListRoleOptions: %v", err)
	}
	var superRoleID uuid.UUID
	for _, o := range opts {
		if o.Code == access.RoleCodeSuperAdmin {
			superRoleID = o.ID
		}
	}

	u2, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "admin2",
		Email:    "admin2@example.com",
		Password: "password123",
		RoleIDs:  []uuid.UUID{superRoleID},
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("CreateUser(admin2): %v", err)
	}

	// Concurrent disable: u2 disables u1, u1 disables u2 -> exactly one succeeds, one gets USERS_LAST_SUPER_ADMIN.
	var wg sync.WaitGroup
	errs := make([]error, 2)
	disabled := identity.StatusDisabled
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = idSvc.UpdateUser(t.Context(), identity.UpdateUserInput{UserID: u1.ID, Status: &disabled, ActorID: &u2.ID})
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = idSvc.UpdateUser(t.Context(), identity.UpdateUserInput{UserID: u2.ID, Status: &disabled, ActorID: &u1.ID})
	}()
	wg.Wait()

	var okCount, lastSuperCount int
	for _, e := range errs {
		if e == nil {
			okCount++
		} else if appErr, ok := apperr.As(e); ok && appErr.Code == identity.ErrCodeLastSuperAdmin {
			lastSuperCount++
		} else {
			t.Fatalf("unexpected concurrent error: %v", e)
		}
	}
	if okCount != 1 || lastSuperCount != 1 {
		t.Fatalf("expected 1 success and 1 %s, got ok=%d lastSuper=%d", identity.ErrCodeLastSuperAdmin, okCount, lastSuperCount)
	}

	// Soft-delete an ordinary user and recreate with the same username & email.
	alice, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("create alice: %v", err)
	}
	if err := idSvc.DeleteUser(t.Context(), identity.DeleteUserInput{UserID: alice.ID, ActorID: &u1.ID}); err != nil {
		t.Fatalf("delete alice: %v", err)
	}
	alice2, err := idSvc.CreateUser(t.Context(), identity.CreateUserInput{
		Username: "alice",
		Email:    "alice@example.com",
		Password: "password123",
		ActorID:  &u1.ID,
	})
	if err != nil {
		t.Fatalf("recreate alice after soft delete: %v", err)
	}
	if alice2.ID == alice.ID {
		t.Fatalf("expected new UUIDv7 for recreated user")
	}
}
