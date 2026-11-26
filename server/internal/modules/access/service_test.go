package access

import (
	"context"
	"io"
	"slices"
	"testing"

	"github.com/google/uuid"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
)

type fakeStore struct {
	perms       map[string]PermissionDef
	rolesByCode map[string]uuid.UUID
	rolePerms   map[uuid.UUID]map[string]struct{}
	userRoles   map[uuid.UUID]map[uuid.UUID]struct{}

	listRolesCalls int
	listPermsCalls int
}

type fakeTxRunner struct{}

func (fakeTxRunner) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.WithSimulatedTx(ctx, fn)
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		perms:       make(map[string]PermissionDef),
		rolesByCode: make(map[string]uuid.UUID),
		rolePerms:   make(map[uuid.UUID]map[string]struct{}),
		userRoles:   make(map[uuid.UUID]map[uuid.UUID]struct{}),
	}
}

func (f *fakeStore) UpsertPermission(_ context.Context, def PermissionDef) error {
	f.perms[def.Code] = def
	return nil
}

func (f *fakeStore) EnsureSystemRole(_ context.Context, id uuid.UUID, def SystemRoleDef) (uuid.UUID, error) {
	if existing, ok := f.rolesByCode[def.Code]; ok {
		return existing, nil
	}
	f.rolesByCode[def.Code] = id
	return id, nil
}

func (f *fakeStore) GrantRolePermission(_ context.Context, roleID uuid.UUID, permCode string) error {
	if f.rolePerms[roleID] == nil {
		f.rolePerms[roleID] = make(map[string]struct{})
	}
	f.rolePerms[roleID][permCode] = struct{}{}
	return nil
}

func (f *fakeStore) GetRoleIDByCode(_ context.Context, code string) (uuid.UUID, error) {
	id, ok := f.rolesByCode[code]
	if !ok {
		return uuid.Nil, errRoleNotFound
	}
	return id, nil
}

func (f *fakeStore) AssignUserRole(_ context.Context, userID, roleID uuid.UUID) error {
	if f.userRoles[userID] == nil {
		f.userRoles[userID] = make(map[uuid.UUID]struct{})
	}
	f.userRoles[userID][roleID] = struct{}{}
	return nil
}

func (f *fakeStore) ListUserRoleCodes(_ context.Context, userID uuid.UUID) ([]string, error) {
	f.listRolesCalls++
	var codes []string
	for code, roleID := range f.rolesByCode {
		if _, ok := f.userRoles[userID][roleID]; ok {
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)
	return codes, nil
}

func (f *fakeStore) ListUserPermissions(_ context.Context, userID uuid.UUID) ([]string, error) {
	f.listPermsCalls++
	seen := make(map[string]struct{})
	for roleID := range f.userRoles[userID] {
		for code := range f.rolePerms[roleID] {
			seen[code] = struct{}{}
		}
	}
	perms := make([]string, 0, len(seen))
	for code := range seen {
		perms = append(perms, code)
	}
	slices.Sort(perms)
	return perms, nil
}

func (f *fakeStore) ListRoleUserIDs(_ context.Context, roleID uuid.UUID) ([]uuid.UUID, error) {
	var users []uuid.UUID
	for userID, roles := range f.userRoles {
		if _, ok := roles[roleID]; ok {
			users = append(users, userID)
		}
	}
	return users, nil
}

func TestSyncCatalogIdempotentAndGrantsAllToSuperAdmin(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, fakeTxRunner{}, mem, logger.New("test", "error", io.Discard))

	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("first SyncCatalog() error = %v", err)
	}
	firstRoleID := s.rolesByCode[RoleCodeSuperAdmin]

	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("second SyncCatalog() error = %v", err)
	}
	if got := s.rolesByCode[RoleCodeSuperAdmin]; got != firstRoleID {
		t.Fatalf("super_admin role ID changed across syncs: %s != %s", got, firstRoleID)
	}
	if len(s.perms) != len(Catalog()) {
		t.Fatalf("seeded permissions = %d, want %d", len(s.perms), len(Catalog()))
	}
	if len(s.rolePerms[firstRoleID]) != len(Catalog()) {
		t.Fatalf("super_admin permissions = %d, want %d", len(s.rolePerms[firstRoleID]), len(Catalog()))
	}
}

func TestCatalogHasNoDuplicatePermissionCodes(t *testing.T) {
	seen := make(map[string]struct{}, len(Catalog()))
	for _, p := range Catalog() {
		if _, exists := seen[p.Code]; exists {
			t.Fatalf("duplicate permission code in catalog: %s", p.Code)
		}
		seen[p.Code] = struct{}{}
	}
}

func TestGetUserAccessCachesAndDefersInvalidationUntilCommit(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, fakeTxRunner{}, mem, logger.New("test", "error", io.Discard))
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	userID := uuid.Must(uuid.NewV7())

	// Prime cache with empty permissions.
	ua, err := svc.GetUserAccess(t.Context(), userID)
	if err != nil {
		t.Fatalf("GetUserAccess() error = %v", err)
	}
	if len(ua.Roles) != 0 || len(ua.Permissions) != 0 {
		t.Fatalf("expected empty access before role assignment, got %+v", ua)
	}
	if s.listPermsCalls != 1 {
		t.Fatalf("listPermsCalls = %d, want 1", s.listPermsCalls)
	}

	// Second call hits cache without touching store.
	if _, err := svc.GetUserAccess(t.Context(), userID); err != nil {
		t.Fatalf("cached GetUserAccess() error = %v", err)
	}
	if s.listPermsCalls != 1 {
		t.Fatalf("listPermsCalls = %d after cache hit, want 1", s.listPermsCalls)
	}

	// Inside an outer transaction, cache must stay intact until outer tx commits.
	err = database.WithSimulatedTx(t.Context(), func(outerCtx context.Context) error {
		if assignErr := svc.AssignRoleByCode(outerCtx, userID, RoleCodeSuperAdmin); assignErr != nil {
			return assignErr
		}
		if _, getErr := mem.Get(t.Context(), userCacheKey(userID)); getErr != nil {
			t.Fatalf("cache was evicted before outer transaction committed: %v", getErr)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("outer tx error = %v", err)
	}

	// After outer tx commits, cache is evicted and HasPermission reads fresh permissions.
	ok, err := svc.HasPermission(t.Context(), userID, PermUsersDelete)
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if !ok {
		t.Fatal("expected super_admin user to hold users:delete permission")
	}
	if s.listPermsCalls != 2 {
		t.Fatalf("listPermsCalls = %d after post-commit invalidation, want 2", s.listPermsCalls)
	}
}

func TestAccessLocalesCompleteAndMatchCatalog(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(access) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	for _, perm := range Catalog() {
		if got := bundle.Translate(i18n.LangZhCN, perm.NameKey, nil); got == perm.NameKey {
			t.Errorf("missing zh-CN translation for %s", perm.NameKey)
		}
		if got := bundle.Translate(i18n.LangEnUS, perm.DescriptionKey, nil); got == perm.DescriptionKey {
			t.Errorf("missing en-US translation for %s", perm.DescriptionKey)
		}
	}
}
