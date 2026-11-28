package access

import (
	"cmp"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/database"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type fakeStore struct {
	perms       map[string]PermissionDef
	rolesByID   map[uuid.UUID]Role
	rolesByCode map[string]uuid.UUID
	rolePerms   map[uuid.UUID]map[string]struct{}
	userRoles   map[uuid.UUID]map[uuid.UUID]struct{}
	menusByID   map[uuid.UUID]Menu
	menusByCode map[string]uuid.UUID

	listRolesCalls int
	listPermsCalls int
	onListPerms    func()
}

type fakeAuditRecorder struct {
	entries []AuditEntry
}

func (r *fakeAuditRecorder) Record(_ context.Context, entry AuditEntry) error {
	r.entries = append(r.entries, entry)
	return nil
}

type fakeActorResolver struct {
	names map[uuid.UUID]string
}

func (r *fakeActorResolver) GetUsername(_ context.Context, userID uuid.UUID) (string, error) {
	return r.names[userID], nil
}

type fakeTxRunner struct{}

func (fakeTxRunner) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.WithSimulatedTx(ctx, fn)
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		perms:       make(map[string]PermissionDef),
		rolesByID:   make(map[uuid.UUID]Role),
		rolesByCode: make(map[string]uuid.UUID),
		rolePerms:   make(map[uuid.UUID]map[string]struct{}),
		userRoles:   make(map[uuid.UUID]map[uuid.UUID]struct{}),
		menusByID:   make(map[uuid.UUID]Menu),
		menusByCode: make(map[string]uuid.UUID),
	}
}

func (f *fakeStore) UpsertPermission(_ context.Context, def PermissionDef) error {
	f.perms[def.Code] = def
	return nil
}

func (f *fakeStore) EnsureSystemRole(_ context.Context, id uuid.UUID, def SystemRoleDef) (uuid.UUID, error) {
	if existingID, ok := f.rolesByCode[def.Code]; ok {
		r := f.rolesByID[existingID]
		r.NameKey = def.NameKey
		r.DescriptionKey = def.DescriptionKey
		r.IsSystem = true
		f.rolesByID[existingID] = r
		return existingID, nil
	}
	now := time.Now().UTC()
	f.rolesByCode[def.Code] = id
	f.rolesByID[id] = Role{
		ID:             id,
		Code:           def.Code,
		NameKey:        def.NameKey,
		DescriptionKey: def.DescriptionKey,
		IsSystem:       true,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
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

func (f *fakeStore) GetUserAccessData(_ context.Context, userID uuid.UUID) (UserAccess, error) {
	f.listRolesCalls++
	f.listPermsCalls++
	if f.onListPerms != nil {
		f.onListPerms()
	}
	var codes []string
	for code, roleID := range f.rolesByCode {
		if _, ok := f.userRoles[userID][roleID]; ok {
			codes = append(codes, code)
		}
	}
	slices.Sort(codes)

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
	return UserAccess{
		Roles:       nonNilStrings(codes),
		Permissions: perms,
	}, nil
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

func (f *fakeStore) filterRoles(q listRolesQuery) []Role {
	needle := strings.ToLower(q.Q)
	out := make([]Role, 0, len(f.rolesByID))
	for _, r := range f.rolesByID {
		if q.IsSystem != nil && r.IsSystem != *q.IsSystem {
			continue
		}
		if needle != "" {
			matched := strings.Contains(strings.ToLower(r.Code), needle) ||
				strings.Contains(strings.ToLower(r.Name), needle) ||
				strings.Contains(strings.ToLower(r.Description), needle) ||
				slices.Contains(q.MatchedSystemCodes, r.Code)
			if !matched {
				continue
			}
		}
		var userCount int64
		for _, roles := range f.userRoles {
			if _, ok := roles[r.ID]; ok {
				userCount++
			}
		}
		r.UserCount = userCount
		r.PermissionCount = int64(len(f.rolePerms[r.ID]))
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Role) int {
		if q.Sort == "" {
			if a.IsSystem != b.IsSystem {
				if a.IsSystem {
					return -1
				}
				return 1
			}
		}
		dir := -1
		if q.Order == "asc" {
			dir = 1
		}
		var res int
		switch q.Sort {
		case "code":
			res = cmp.Compare(a.Code, b.Code)
		case "name":
			nameA := a.Name
			if nameA == "" {
				if a.Code == RoleCodeSuperAdmin && q.SuperAdminName != "" {
					nameA = q.SuperAdminName
				} else {
					nameA = a.Code
				}
			}
			nameB := b.Name
			if nameB == "" {
				if b.Code == RoleCodeSuperAdmin && q.SuperAdminName != "" {
					nameB = q.SuperAdminName
				} else {
					nameB = b.Code
				}
			}
			res = cmp.Compare(nameA, nameB)
		case "updated_at":
			res = a.UpdatedAt.Compare(b.UpdatedAt)
		default:
			res = a.CreatedAt.Compare(b.CreatedAt)
		}
		if res != 0 {
			return res * dir
		}
		return cmp.Compare(b.ID.String(), a.ID.String())
	})
	return out
}

func (f *fakeStore) CountRoles(_ context.Context, q listRolesQuery) (int64, error) {
	return int64(len(f.filterRoles(q))), nil
}

func (f *fakeStore) ListRoles(_ context.Context, q listRolesQuery) ([]Role, error) {
	all := f.filterRoles(q)
	start := int(q.Offset)
	if start >= len(all) {
		return make([]Role, 0), nil
	}
	end := min(start+int(q.Limit), len(all))
	return all[start:end], nil
}

func (f *fakeStore) GetRoleByID(_ context.Context, id uuid.UUID) (Role, error) {
	r, ok := f.rolesByID[id]
	if !ok {
		return Role{}, errRoleNotFound
	}
	var userCount int64
	for _, roles := range f.userRoles {
		if _, has := roles[id]; has {
			userCount++
		}
	}
	r.UserCount = userCount
	r.PermissionCount = int64(len(f.rolePerms[id]))
	return r, nil
}

func (f *fakeStore) GetRoleByIDForUpdate(ctx context.Context, id uuid.UUID) (Role, error) {
	return f.GetRoleByID(ctx, id)
}

func (f *fakeStore) ListPermissionCodesByRoleIDs(_ context.Context, roleIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	res := make(map[uuid.UUID][]string, len(roleIDs))
	for _, roleID := range roleIDs {
		perms := make([]string, 0, len(f.rolePerms[roleID]))
		for code := range f.rolePerms[roleID] {
			perms = append(perms, code)
		}
		slices.Sort(perms)
		res[roleID] = perms
	}
	return res, nil
}

func (f *fakeStore) CreateRole(_ context.Context, rec createRoleRecord) (Role, error) {
	if _, exists := f.rolesByCode[rec.Code]; exists {
		return Role{}, errRoleCodeConflict
	}
	now := time.Now().UTC()
	r := Role{
		ID:          rec.ID,
		Code:        rec.Code,
		Name:        rec.Name,
		Description: rec.Description,
		IsSystem:    false,
		CreatedBy:   rec.ActorID,
		UpdatedBy:   rec.ActorID,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	f.rolesByID[r.ID] = r
	f.rolesByCode[r.Code] = r.ID
	return r, nil
}

func (f *fakeStore) UpdateRole(_ context.Context, rec updateRoleRecord) (Role, error) {
	r, ok := f.rolesByID[rec.ID]
	if !ok {
		return Role{}, errRoleNotFound
	}
	r.Name = rec.Name
	r.Description = rec.Description
	r.UpdatedBy = rec.ActorID
	r.UpdatedAt = time.Now().UTC()
	f.rolesByID[r.ID] = r
	return r, nil
}

func (f *fakeStore) DeleteRolePermissions(_ context.Context, roleID uuid.UUID) error {
	delete(f.rolePerms, roleID)
	return nil
}

func (f *fakeStore) CountRoleUsers(_ context.Context, roleID uuid.UUID) (int64, error) {
	var count int64
	for _, roles := range f.userRoles {
		if _, ok := roles[roleID]; ok {
			count++
		}
	}
	return count, nil
}

func (f *fakeStore) DeleteRole(_ context.Context, id uuid.UUID) error {
	r, ok := f.rolesByID[id]
	if ok {
		delete(f.rolesByCode, r.Code)
		delete(f.rolesByID, id)
		delete(f.rolePerms, id)
	}
	return nil
}

func (f *fakeStore) ListRoleOptions(_ context.Context) ([]RoleRef, error) {
	out := make([]RoleRef, 0, len(f.rolesByID))
	for _, r := range f.rolesByID {
		out = append(out, RoleRef{
			ID:       r.ID,
			Code:     r.Code,
			Name:     r.Name,
			NameKey:  r.NameKey,
			IsSystem: r.IsSystem,
		})
	}
	slices.SortFunc(out, func(a, b RoleRef) int {
		if a.IsSystem != b.IsSystem {
			if a.IsSystem {
				return -1
			}
			return 1
		}
		return cmp.Compare(a.Code, b.Code)
	})
	return out, nil
}

func (f *fakeStore) ListRolesByIDs(_ context.Context, ids []uuid.UUID) ([]RoleRef, error) {
	out := make([]RoleRef, 0, len(ids))
	for _, id := range ids {
		if r, ok := f.rolesByID[id]; ok {
			out = append(out, RoleRef{
				ID:       r.ID,
				Code:     r.Code,
				Name:     r.Name,
				NameKey:  r.NameKey,
				IsSystem: r.IsSystem,
			})
		}
	}
	return out, nil
}

func (f *fakeStore) ListRolesByUserIDs(_ context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error) {
	out := make(map[uuid.UUID][]RoleRef, len(userIDs))
	for _, uid := range userIDs {
		for rid := range f.userRoles[uid] {
			if r, ok := f.rolesByID[rid]; ok {
				out[uid] = append(out[uid], RoleRef{
					ID:       r.ID,
					Code:     r.Code,
					Name:     r.Name,
					NameKey:  r.NameKey,
					IsSystem: r.IsSystem,
				})
			}
		}
	}
	return out, nil
}

func (f *fakeStore) DeleteUserRoles(_ context.Context, userID uuid.UUID) error {
	delete(f.userRoles, userID)
	return nil
}

func (f *fakeStore) ListUserIDsByRoleIDs(_ context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	set := make(map[uuid.UUID]struct{})
	for _, rid := range roleIDs {
		for uid, rids := range f.userRoles {
			if _, has := rids[rid]; has {
				set[uid] = struct{}{}
			}
		}
	}
	out := make([]uuid.UUID, 0, len(set))
	for uid := range set {
		out = append(out, uid)
	}
	return out, nil
}

func (f *fakeStore) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	roleID, ok := f.rolesByCode[code]
	if !ok {
		return make([]uuid.UUID, 0), nil
	}
	return f.ListRoleUserIDs(ctx, roleID)
}

func (f *fakeStore) EnsureSystemMenu(_ context.Context, id uuid.UUID, parentID *uuid.UUID, def SystemMenuDef) (uuid.UUID, error) {
	if existingID, ok := f.menusByCode[def.Code]; ok {
		m := f.menusByID[existingID]
		m.TitleKey = def.TitleKey
		m.IsSystem = true
		f.menusByID[existingID] = m
		return existingID, nil
	}
	now := time.Now().UTC()
	m := Menu{
		ID:             id,
		ParentID:       parentID,
		Type:           def.Type,
		Code:           def.Code,
		TitleKey:       def.TitleKey,
		Path:           def.Path,
		Icon:           def.Icon,
		PermissionCode: def.PermissionCode,
		SortOrder:      def.SortOrder,
		Visible:        true,
		Status:         MenuStatusActive,
		IsSystem:       true,
		Children:       make([]Menu, 0),
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.menusByID[id] = m
	f.menusByCode[def.Code] = id
	return id, nil
}

func (f *fakeStore) ListMenus(_ context.Context) ([]Menu, error) {
	out := make([]Menu, 0, len(f.menusByID))
	for _, m := range f.menusByID {
		out = append(out, m)
	}
	slices.SortFunc(out, func(a, b Menu) int {
		if c := cmp.Compare(a.SortOrder, b.SortOrder); c != 0 {
			return c
		}
		return cmp.Compare(a.CreatedAt.UnixNano(), b.CreatedAt.UnixNano())
	})
	return out, nil
}

func (f *fakeStore) GetMenuByID(_ context.Context, id uuid.UUID) (Menu, error) {
	m, ok := f.menusByID[id]
	if !ok {
		return Menu{}, errMenuNotFound
	}
	return m, nil
}

func (f *fakeStore) GetMenuByIDForUpdate(ctx context.Context, id uuid.UUID) (Menu, error) {
	return f.GetMenuByID(ctx, id)
}

func (f *fakeStore) CreateMenu(_ context.Context, rec createMenuRecord) (Menu, error) {
	if _, exists := f.menusByCode[rec.Code]; exists {
		return Menu{}, errMenuCodeConflict
	}
	now := time.Now().UTC()
	m := Menu{
		ID:             rec.ID,
		ParentID:       rec.ParentID,
		Type:           rec.Type,
		Code:           rec.Code,
		Title:          rec.Title,
		Path:           rec.Path,
		Icon:           rec.Icon,
		PermissionCode: rec.PermissionCode,
		SortOrder:      rec.SortOrder,
		Visible:        rec.Visible,
		Status:         rec.Status,
		IsSystem:       false,
		Children:       make([]Menu, 0),
		CreatedBy:      rec.ActorID,
		UpdatedBy:      rec.ActorID,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	f.menusByID[rec.ID] = m
	f.menusByCode[rec.Code] = rec.ID
	return m, nil
}

func (f *fakeStore) UpdateMenu(_ context.Context, rec updateMenuRecord) (Menu, error) {
	m, ok := f.menusByID[rec.ID]
	if !ok {
		return Menu{}, errMenuNotFound
	}
	m.ParentID = rec.ParentID
	m.Type = rec.Type
	m.Title = rec.Title
	m.Path = rec.Path
	m.Icon = rec.Icon
	m.PermissionCode = rec.PermissionCode
	m.SortOrder = rec.SortOrder
	m.Visible = rec.Visible
	m.Status = rec.Status
	m.UpdatedBy = rec.ActorID
	m.UpdatedAt = time.Now().UTC()
	f.menusByID[rec.ID] = m
	return m, nil
}

func (f *fakeStore) UpdateMenuOrder(_ context.Context, id uuid.UUID, parentID *uuid.UUID, sortOrder int32, actorID *uuid.UUID) error {
	m, ok := f.menusByID[id]
	if !ok {
		return errMenuNotFound
	}
	m.ParentID = parentID
	m.SortOrder = sortOrder
	m.UpdatedBy = actorID
	m.UpdatedAt = time.Now().UTC()
	f.menusByID[id] = m
	return nil
}

func (f *fakeStore) CountMenuChildren(_ context.Context, id uuid.UUID) (int64, error) {
	var n int64
	for _, m := range f.menusByID {
		if m.ParentID != nil && *m.ParentID == id {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) DeleteMenu(_ context.Context, id uuid.UUID) error {
	if m, ok := f.menusByID[id]; ok {
		delete(f.menusByCode, m.Code)
		delete(f.menusByID, id)
	}
	return nil
}

func TestSyncCatalogIdempotentAndGrantsAllToSuperAdmin(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		Log:   logger.New("test", "error", io.Discard),
	})

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

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		Log:   logger.New("test", "error", io.Discard),
	})
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

func TestRoleCRUDInvariantsAndCacheInvalidation(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	actorID := uuid.Must(uuid.NewV7())
	auditRec := &fakeAuditRecorder{}
	svc := newServiceWithStore(s, ServiceDeps{
		Tx:     fakeTxRunner{},
		Cache:  mem,
		I18n:   bundle,
		Audit:  auditRec,
		Actors: &fakeActorResolver{names: map[uuid.UUID]string{actorID: "root_admin"}},
		Log:    logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	// Create role deduplicates permissions and records audit.
	created, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "Ops_Viewer",
		Name:        "Operations Viewer",
		Description: "Read-only operations access",
		Permissions: []string{PermUsersRead, PermUsersRead, PermDashboardRead},
		ActorID:     &actorID,
	})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}
	if created.Code != "ops_viewer" {
		t.Fatalf("code = %q, want ops_viewer", created.Code)
	}
	if !slices.Equal(created.Permissions, []string{PermDashboardRead, PermUsersRead}) {
		t.Fatalf("permissions = %v, want [dashboard:read users:read]", created.Permissions)
	}
	if len(auditRec.entries) != 1 || auditRec.entries[0].Action != auditActionCreate || auditRec.entries[0].ActorUsername != "root_admin" {
		t.Fatalf("unexpected audit entry: %+v", auditRec.entries)
	}

	// Duplicate role code returns 409 ROLES_CODE_CONFLICT.
	_, err = svc.CreateRole(t.Context(), CreateRoleInput{
		Code: "ops_viewer",
		Name: "Duplicate",
	})
	assertAppErrCode(t, err, ErrCodeRoleCodeConflict)

	// Unknown permission code returns 422 VALIDATION_FAILED.
	_, err = svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "bad_perm_role",
		Name:        "Bad Perm",
		Permissions: []string{"nonexistent:read"},
	})
	assertAppErrCode(t, err, apperr.CodeValidationFailed)

	// Assign custom role to a user and warm permission cache.
	memberID := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), memberID, "ops_viewer"); err != nil {
		t.Fatalf("AssignRoleByCode() error = %v", err)
	}
	hasUpdate, err := svc.HasPermission(t.Context(), memberID, PermUsersUpdate)
	if err != nil || hasUpdate {
		t.Fatalf("before update HasPermission(users:update) = %v, err = %v; want false", hasUpdate, err)
	}

	// Updating permissions invalidates affected user's cache after commit.
	nextPerms := []string{PermDashboardRead, PermUsersRead, PermUsersUpdate}
	nextName := "Operations Editor"
	updated, err := svc.UpdateRole(t.Context(), UpdateRoleInput{
		RoleID:      created.ID,
		Name:        &nextName,
		Permissions: &nextPerms,
		ActorID:     &actorID,
	})
	if err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	if updated.Name != nextName || len(updated.Permissions) != 3 {
		t.Fatalf("unexpected updated role: %+v", updated)
	}
	hasUpdate, err = svc.HasPermission(t.Context(), memberID, PermUsersUpdate)
	if err != nil || !hasUpdate {
		t.Fatalf("after update HasPermission(users:update) = %v, err = %v; want true", hasUpdate, err)
	}

	// Role in use cannot be deleted.
	err = svc.DeleteRole(t.Context(), DeleteRoleInput{RoleID: created.ID, ActorID: &actorID})
	assertAppErrCode(t, err, ErrCodeRoleInUse)

	// Built-in system role cannot be modified or deleted.
	superAdminID := s.rolesByCode[RoleCodeSuperAdmin]
	_, err = svc.UpdateRole(t.Context(), UpdateRoleInput{RoleID: superAdminID, Name: &nextName})
	assertAppErrCode(t, err, ErrCodeRoleSystemImmutable)
	err = svc.DeleteRole(t.Context(), DeleteRoleInput{RoleID: superAdminID})
	assertAppErrCode(t, err, ErrCodeRoleSystemImmutable)

	// Localized search matches super_admin by translated Chinese name.
	res, err := svc.ListRoles(t.Context(), RoleListFilter{
		Pagination: pagination.Normalize(1, 20),
		Q:          bundle.Translate(i18n.LangZhCN, "ACCESS_ROLE_SUPER_ADMIN_NAME", nil),
	})
	if err != nil {
		t.Fatalf("ListRoles() error = %v", err)
	}
	if res.Total != 1 || len(res.Items) != 1 || res.Items[0].Code != RoleCodeSuperAdmin {
		t.Fatalf("expected super_admin match for zh-CN query, got %+v", res)
	}

	// Unused custom role can be deleted.
	unused, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code: "temp_auditor",
		Name: "Temp Auditor",
	})
	if err != nil {
		t.Fatalf("CreateRole(temp_auditor) error = %v", err)
	}
	if err := svc.DeleteRole(t.Context(), DeleteRoleInput{RoleID: unused.ID, ActorID: &actorID}); err != nil {
		t.Fatalf("DeleteRole(temp_auditor) error = %v", err)
	}
	_, err = svc.GetRole(t.Context(), unused.ID)
	assertAppErrCode(t, err, ErrCodeRoleNotFound)
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
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			if got := bundle.Translate(lang, perm.NameKey, nil); got == "" || got == perm.NameKey {
				t.Errorf("missing %s translation for %s", lang, perm.NameKey)
			}
			if got := bundle.Translate(lang, perm.DescriptionKey, nil); got == "" || got == perm.DescriptionKey {
				t.Errorf("missing %s translation for %s", lang, perm.DescriptionKey)
			}
		}
	}
	for _, group := range PermissionGroups() {
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			if got := bundle.Translate(lang, group.LabelKey, nil); got == "" || got == group.LabelKey {
				t.Errorf("missing %s module label for %s", lang, group.LabelKey)
			}
		}
	}
	for _, role := range SystemRoles() {
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			if got := bundle.Translate(lang, role.NameKey, nil); got == "" || got == role.NameKey {
				t.Errorf("missing %s system role name for %s", lang, role.NameKey)
			}
			if got := bundle.Translate(lang, role.DescriptionKey, nil); got == "" || got == role.DescriptionKey {
				t.Errorf("missing %s system role description for %s", lang, role.DescriptionKey)
			}
		}
	}

	allErrCodes := []string{
		ErrCodeRoleNotFound,
		ErrCodeRoleInUse,
		ErrCodeRoleSystemImmutable,
		ErrCodeRoleCodeConflict,
		ErrCodeInvalidRoleCode,
		ErrCodeInvalidRoleName,
		ErrCodeInvalidRoleDesc,
		ErrCodeInvalidPermissionCode,
		ErrCodeInvalidNumber,
		ErrCodeInvalidBool,
		ErrCodeMenuNotFound,
		ErrCodeMenuCodeConflict,
		ErrCodeMenuSystemImmutable,
		ErrCodeMenuHasChildren,
		ErrCodeMenuCycleDetected,
		ErrCodeMenuInvalidParent,
		ErrCodeInvalidMenuCode,
		ErrCodeMenuCodeImmutable,
		ErrCodeInvalidMenuType,
		ErrCodeInvalidMenuTitle,
		ErrCodeInvalidMenuPath,
		ErrCodeInvalidMenuIcon,
		ErrCodeInvalidMenuStatus,
		ErrCodeDuplicateMenuReorder,
	}
	for _, code := range allErrCodes {
		for _, lang := range []string{i18n.LangZhCN, i18n.LangEnUS} {
			if got := bundle.Translate(lang, code, nil); got == "" || got == code {
				t.Errorf("missing %s translation for error code %s", lang, code)
			}
		}
	}
}

func TestGetUserAccessPreventsStaleWriteBackOnConcurrentInvalidate(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	role, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "temp_editor",
		Name:        "Temp Editor",
		Permissions: []string{PermUsersRead, PermUsersDelete},
	})
	if err != nil {
		t.Fatalf("CreateRole() error = %v", err)
	}

	userID := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), userID, role.Code); err != nil {
		t.Fatalf("AssignRoleByCode() error = %v", err)
	}

	readEntered := make(chan struct{})
	resumeRead := make(chan struct{})
	s.onListPerms = func() {
		s.onListPerms = nil
		close(readEntered)
		<-resumeRead
	}

	readDone := make(chan error, 1)
	go func() {
		_, gErr := svc.GetUserAccess(t.Context(), userID)
		readDone <- gErr
	}()

	<-readEntered
	// While the slow reader already observed old permissions, revoke users:delete and commit.
	nextPerms := []string{PermUsersRead}
	if _, err := svc.UpdateRole(t.Context(), UpdateRoleInput{
		RoleID:      role.ID,
		Permissions: &nextPerms,
	}); err != nil {
		t.Fatalf("UpdateRole() error = %v", err)
	}
	close(resumeRead)

	if rErr := <-readDone; rErr != nil {
		t.Fatalf("slow GetUserAccess() error = %v", rErr)
	}

	// Stale write-back must have been skipped, so subsequent HasPermission reflects revoked users:delete.
	canDelete, err := svc.HasPermission(t.Context(), userID, PermUsersDelete)
	if err != nil {
		t.Fatalf("HasPermission() error = %v", err)
	}
	if canDelete {
		t.Fatal("stale cache write-back resurrected revoked users:delete permission")
	}
}

func TestListRolesSortByNameUsesLocalizedSystemRoleName(t *testing.T) {
	s := newFakeStore()
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("access", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:   fakeTxRunner{},
		I18n: bundle,
		Log:  logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}
	if _, err := svc.CreateRole(t.Context(), CreateRoleInput{Code: "alpha", Name: "Alpha Role"}); err != nil {
		t.Fatalf("CreateRole(alpha) error = %v", err)
	}
	if _, err := svc.CreateRole(t.Context(), CreateRoleInput{Code: "zeta", Name: "Zeta Role"}); err != nil {
		t.Fatalf("CreateRole(zeta) error = %v", err)
	}

	enCtx := i18n.WithLocale(t.Context(), i18n.LangEnUS)
	resAsc, err := svc.ListRoles(enCtx, RoleListFilter{
		Pagination: pagination.Normalize(1, 20),
		Sort:       "name",
		Order:      "asc",
	})
	if err != nil {
		t.Fatalf("ListRoles(sort=name asc) error = %v", err)
	}
	gotCodes := []string{resAsc.Items[0].Code, resAsc.Items[1].Code, resAsc.Items[2].Code}
	wantCodes := []string{"alpha", RoleCodeSuperAdmin, "zeta"}
	if !slices.Equal(gotCodes, wantCodes) {
		t.Fatalf("sort=name asc codes = %v, want %v", gotCodes, wantCodes)
	}
}

func TestRoleValidationRejectsNulAndInvalidUTF8AndNonNilEmptySlices(t *testing.T) {
	s := newFakeStore()
	svc := newServiceWithStore(s, ServiceDeps{
		Tx:  fakeTxRunner{},
		Log: logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	if _, err := svc.ListRoles(t.Context(), RoleListFilter{Q: "bad\x00query"}); err == nil {
		t.Fatal("expected 422 for NUL byte in ListRoles.Q")
	} else {
		assertAppErrCode(t, err, apperr.CodeValidationFailed)
	}

	if _, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "valid_code",
		Name:        "Bad\x00Name",
		Description: "Bad\xffDesc",
	}); err == nil {
		t.Fatal("expected 422 for NUL/invalid UTF-8 in CreateRole")
	} else {
		assertAppErrCode(t, err, apperr.CodeValidationFailed)
	}

	created, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code: "clean_role",
		Name: "Clean Role",
	})
	if err != nil {
		t.Fatalf("CreateRole(clean_role) error = %v", err)
	}
	badDesc := "bad\x00desc"
	if _, err := svc.UpdateRole(t.Context(), UpdateRoleInput{
		RoleID:      created.ID,
		Description: &badDesc,
	}); err == nil {
		t.Fatal("expected 422 for NUL in UpdateRole description")
	} else {
		assertAppErrCode(t, err, apperr.CodeValidationFailed)
	}

	idsByIDs, err := svc.ListUserIDsByRoleIDs(t.Context(), []uuid.UUID{created.ID})
	if err != nil || idsByIDs == nil || len(idsByIDs) != 0 {
		t.Fatalf("ListUserIDsByRoleIDs empty = (%#v, %v), want non-nil empty slice", idsByIDs, err)
	}
	idsByCode, err := svc.ListUserIDsByRoleCode(t.Context(), created.Code)
	if err != nil || idsByCode == nil || len(idsByCode) != 0 {
		t.Fatalf("ListUserIDsByRoleCode empty = (%#v, %v), want non-nil empty slice", idsByCode, err)
	}
}

func assertAppErrCode(t *testing.T, err error, wantCode string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error %s, got nil", wantCode)
	}
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("expected apperr.Error %s, got %T: %v", wantCode, err, err)
	}
	if appErr.Code != wantCode {
		t.Fatalf("error code = %s, want %s", appErr.Code, wantCode)
	}
}
