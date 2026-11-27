package identity

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/pagination"
)

type fakeUserRow struct {
	user         User
	passwordHash string
	lastLoginAt  *time.Time
	createdBy    *uuid.UUID
	updatedBy    *uuid.UUID
	deletedAt    *time.Time
}

type fakeStore struct {
	users map[uuid.UUID]*fakeUserRow
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: make(map[uuid.UUID]*fakeUserRow)}
}

func (f *fakeStore) CountUsers(_ context.Context) (int64, error) {
	var n int64
	for _, r := range f.users {
		if r.deletedAt == nil {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) CreateUser(_ context.Context, rec createUserRecord) (User, error) {
	for _, r := range f.users {
		if r.deletedAt != nil {
			continue
		}
		if strings.EqualFold(r.user.Username, rec.Username) {
			return User{}, errUsernameConflict
		}
		if strings.EqualFold(r.user.Email, rec.Email) {
			return User{}, errEmailConflict
		}
	}
	now := time.Now().UTC()
	u := User{
		ID:          rec.ID,
		Username:    rec.Username,
		Email:       rec.Email,
		DisplayName: rec.DisplayName,
		Status:      rec.Status,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	var cb, ub *uuid.UUID
	if rec.CreatedBy.Valid {
		id := rec.CreatedBy.UUID
		cb = &id
	}
	if rec.UpdatedBy.Valid {
		id := rec.UpdatedBy.UUID
		ub = &id
	}
	f.users[rec.ID] = &fakeUserRow{
		user:         u,
		passwordHash: rec.PasswordHash,
		createdBy:    cb,
		updatedBy:    ub,
	}
	return u, nil
}

func (f *fakeStore) GetUserForAuthByIdentifier(_ context.Context, idf string) (AuthCredentials, error) {
	for _, r := range f.users {
		if r.deletedAt == nil && (strings.EqualFold(r.user.Username, idf) || strings.EqualFold(r.user.Email, idf)) {
			return AuthCredentials{
				ID:           r.user.ID,
				Username:     r.user.Username,
				Email:        r.user.Email,
				DisplayName:  r.user.DisplayName,
				AvatarFileID: cloneUUIDPtr(r.user.AvatarFileID),
				Status:       r.user.Status,
				PasswordHash: r.passwordHash,
			}, nil
		}
	}
	return AuthCredentials{}, errUserNotFound
}

func (f *fakeStore) GetUserByID(_ context.Context, id uuid.UUID) (User, error) {
	r, ok := f.users[id]
	if !ok || r.deletedAt != nil {
		return User{}, errUserNotFound
	}
	return r.user, nil
}

func (f *fakeStore) GetUserPasswordHashByID(_ context.Context, id uuid.UUID) (userPasswordRecord, error) {
	r, ok := f.users[id]
	if !ok || r.deletedAt != nil {
		return userPasswordRecord{}, errUserNotFound
	}
	return userPasswordRecord{
		ID:           r.user.ID,
		Username:     r.user.Username,
		Status:       r.user.Status,
		PasswordHash: r.passwordHash,
	}, nil
}

func (f *fakeStore) GetUserPasswordHashByIDForUpdate(ctx context.Context, id uuid.UUID) (userPasswordRecord, error) {
	return f.GetUserPasswordHashByID(ctx, id)
}

func (f *fakeStore) UpdateUserLastLogin(_ context.Context, id uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		now := time.Now().UTC()
		r.lastLoginAt = &now
	}
	return nil
}

func (f *fakeStore) UpdateUserPasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		r.passwordHash = hash
	}
	return nil
}

func (f *fakeStore) filterRows(q listUsersQuery) []*fakeUserRow {
	out := make([]*fakeUserRow, 0, len(f.users))
	needle := strings.ToLower(q.Q)
	for _, r := range f.users {
		if r.deletedAt != nil {
			continue
		}
		if needle != "" {
			if !strings.Contains(strings.ToLower(r.user.Username), needle) &&
				!strings.Contains(strings.ToLower(r.user.Email), needle) &&
				!strings.Contains(strings.ToLower(r.user.DisplayName), needle) {
				continue
			}
		}
		if len(q.Statuses) > 0 && !slices.Contains(q.Statuses, r.user.Status) {
			continue
		}
		if q.FilterIDs && !slices.Contains(q.IDs, r.user.ID) {
			continue
		}
		out = append(out, r)
	}
	asc := strings.EqualFold(q.Order, "asc")
	slices.SortFunc(out, func(a, b *fakeUserRow) int {
		var c int
		switch q.Sort {
		case "username":
			c = cmp.Compare(a.user.Username, b.user.Username)
		case "updated_at":
			c = cmp.Compare(a.user.UpdatedAt.UnixNano(), b.user.UpdatedAt.UnixNano())
		case "last_login_at":
			switch {
			case a.lastLoginAt == nil && b.lastLoginAt == nil:
				c = 0
			case a.lastLoginAt == nil:
				return 1 // NULLS LAST
			case b.lastLoginAt == nil:
				return -1 // NULLS LAST
			default:
				c = cmp.Compare(a.lastLoginAt.UnixNano(), b.lastLoginAt.UnixNano())
			}
		default:
			c = cmp.Compare(a.user.CreatedAt.UnixNano(), b.user.CreatedAt.UnixNano())
		}
		if c == 0 {
			c = cmp.Compare(a.user.ID.String(), b.user.ID.String())
		}
		if !asc {
			return -c
		}
		return c
	})
	return out
}

func (f *fakeStore) CountUsersFiltered(_ context.Context, q listUsersQuery) (int64, error) {
	return int64(len(f.filterRows(q))), nil
}

func (f *fakeStore) ListUsers(_ context.Context, q listUsersQuery) ([]UserDetail, error) {
	rows := f.filterRows(q)
	start := min(int(q.Offset), len(rows))
	end := min(start+int(q.Limit), len(rows))
	out := make([]UserDetail, 0, end-start)
	for _, r := range rows[start:end] {
		out = append(out, f.toDetail(r))
	}
	return out, nil
}

func (f *fakeStore) GetUserDetailByID(_ context.Context, id uuid.UUID) (UserDetail, error) {
	r, ok := f.users[id]
	if !ok || r.deletedAt != nil {
		return UserDetail{}, errUserNotFound
	}
	return f.toDetail(r), nil
}

func (f *fakeStore) GetUserByIDForUpdate(ctx context.Context, id uuid.UUID) (User, error) {
	return f.GetUserByID(ctx, id)
}

func (f *fakeStore) UpdateUserProfile(_ context.Context, rec updateUserProfileRecord) error {
	r, ok := f.users[rec.ID]
	if !ok || r.deletedAt != nil {
		return errUserNotFound
	}
	for id, other := range f.users {
		if id != rec.ID && other.deletedAt == nil && strings.EqualFold(other.user.Email, rec.Email) {
			return errEmailConflict
		}
	}
	r.user.Email = rec.Email
	r.user.DisplayName = rec.DisplayName
	r.user.Status = rec.Status
	r.user.UpdatedAt = time.Now().UTC()
	r.updatedBy = rec.UpdatedBy
	return nil
}

func (f *fakeStore) UpdateOwnProfile(_ context.Context, rec updateOwnProfileRecord) error {
	r, ok := f.users[rec.ID]
	if !ok || r.deletedAt != nil {
		return errUserNotFound
	}
	for id, other := range f.users {
		if id != rec.ID && other.deletedAt == nil && strings.EqualFold(other.user.Email, rec.Email) {
			return errEmailConflict
		}
	}
	r.user.Email = rec.Email
	r.user.DisplayName = rec.DisplayName
	r.user.AvatarFileID = cloneUUIDPtr(rec.AvatarFileID)
	r.user.UpdatedAt = time.Now().UTC()
	uid := rec.ID
	r.updatedBy = &uid
	return nil
}

func (f *fakeStore) UpdateUserPasswordWithActor(_ context.Context, id uuid.UUID, hash string, updatedBy *uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		r.passwordHash = hash
		r.updatedBy = cloneUUIDPtr(updatedBy)
		r.user.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (f *fakeStore) SoftDeleteUser(_ context.Context, id uuid.UUID, updatedBy *uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		now := time.Now().UTC()
		r.deletedAt = &now
		r.updatedBy = updatedBy
	}
	return nil
}

func (f *fakeStore) CountActiveUsersByIDs(_ context.Context, ids []uuid.UUID) (int64, error) {
	var n int64
	for _, id := range ids {
		if r, ok := f.users[id]; ok && r.deletedAt == nil && r.user.Status == StatusActive {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) ListUsernamesByIDs(_ context.Context, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	out := make(map[uuid.UUID]string, len(ids))
	for _, id := range ids {
		if r, ok := f.users[id]; ok {
			out[id] = r.user.Username
		}
	}
	return out, nil
}

func (f *fakeStore) CountActiveUsersByAvatarFileID(_ context.Context, fileID uuid.UUID) (int64, error) {
	var n int64
	for _, r := range f.users {
		if r.deletedAt == nil && r.user.AvatarFileID != nil && *r.user.AvatarFileID == fileID {
			n++
		}
	}
	return n, nil
}

func (f *fakeStore) toDetail(r *fakeUserRow) UserDetail {
	return UserDetail{
		ID:           r.user.ID,
		Username:     r.user.Username,
		Email:        r.user.Email,
		DisplayName:  r.user.DisplayName,
		AvatarFileID: cloneUUIDPtr(r.user.AvatarFileID),
		Status:       r.user.Status,
		LastLoginAt:  r.lastLoginAt,
		Roles:        make([]RoleRef, 0),
		CreatedBy:    f.actorRef(r.createdBy),
		UpdatedBy:    f.actorRef(r.updatedBy),
		CreatedAt:    r.user.CreatedAt,
		UpdatedAt:    r.user.UpdatedAt,
	}
}

func (f *fakeStore) actorRef(id *uuid.UUID) *ActorRef {
	if id == nil {
		return nil
	}
	if r, ok := f.users[*id]; ok {
		return &ActorRef{ID: r.user.ID, Username: r.user.Username}
	}
	return nil
}

type fakeHasher struct {
	hashCalls int
}

func (h *fakeHasher) Hash(pw string) (string, error) {
	h.hashCalls++
	if err := security.ValidatePasswordLength(pw); err != nil {
		return "", err
	}
	return "hash:" + pw, nil
}

func (*fakeHasher) Verify(pw, encodedHash string) (bool, bool, error) {
	return encodedHash == "hash:"+pw, false, nil
}

type fakeRoles struct {
	roles        map[uuid.UUID]RoleRef
	userRoles    map[uuid.UUID][]uuid.UUID
	replaceCalls int
	listErr      error
}

func newFakeRoles() *fakeRoles {
	superID := uuid.Must(uuid.NewV7())
	viewerID := uuid.Must(uuid.NewV7())
	editorID := uuid.Must(uuid.NewV7())
	return &fakeRoles{
		roles: map[uuid.UUID]RoleRef{
			superID:  {ID: superID, Code: "super_admin", NameKey: "ACCESS_ROLE_SUPER_ADMIN_NAME", IsSystem: true},
			viewerID: {ID: viewerID, Code: "viewer", Name: "Viewer", IsSystem: false},
			editorID: {ID: editorID, Code: "editor", Name: "Editor", IsSystem: false},
		},
		userRoles: make(map[uuid.UUID][]uuid.UUID),
	}
}

func (f *fakeRoles) roleID(code string) uuid.UUID {
	for id, r := range f.roles {
		if r.Code == code {
			return id
		}
	}
	return uuid.Nil
}

func (f *fakeRoles) ResolveRoles(_ context.Context, ids []uuid.UUID) ([]RoleRef, error) {
	out := make([]RoleRef, 0, len(ids))
	for _, id := range ids {
		if r, ok := f.roles[id]; ok {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRoles) ListRolesByUserIDs(_ context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]RoleRef, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make(map[uuid.UUID][]RoleRef, len(userIDs))
	for _, uid := range userIDs {
		for _, rid := range f.userRoles[uid] {
			if r, ok := f.roles[rid]; ok {
				out[uid] = append(out[uid], r)
			}
		}
		out[uid] = sortRoleRefs(out[uid])
	}
	return out, nil
}

func (f *fakeRoles) ListUserIDsByRoleIDs(_ context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	var out []uuid.UUID
	for uid, rids := range f.userRoles {
		for _, rid := range roleIDs {
			if slices.Contains(rids, rid) {
				out = append(out, uid)
				break
			}
		}
	}
	return out, nil
}

func (f *fakeRoles) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	rid := f.roleID(code)
	if rid == uuid.Nil {
		return nil, nil
	}
	return f.ListUserIDsByRoleIDs(ctx, []uuid.UUID{rid})
}

func (f *fakeRoles) ReplaceUserRoles(_ context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	f.replaceCalls++
	if len(roleIDs) == 0 {
		delete(f.userRoles, userID)
		return nil
	}
	f.userRoles[userID] = slices.Clone(roleIDs)
	return nil
}

type fakeSessions struct {
	revoked      []uuid.UUID
	activeByUser map[uuid.UUID][]UserSession
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{activeByUser: make(map[uuid.UUID][]UserSession)}
}

func (f *fakeSessions) seedActive(userID uuid.UUID) uuid.UUID {
	sid := uuid.Must(uuid.NewV7())
	now := time.Now().UTC()
	f.activeByUser[userID] = append(f.activeByUser[userID], UserSession{
		ID:         sid,
		IPAddress:  "127.0.0.1",
		UserAgent:  "TestUA",
		LastUsedAt: now,
		CreatedAt:  now,
		ExpiresAt:  now.Add(time.Hour),
	})
	return sid
}

func (f *fakeSessions) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	f.revoked = append(f.revoked, userID)
	delete(f.activeByUser, userID)
	return nil
}

func (f *fakeSessions) ListActiveUserSessions(_ context.Context, userID uuid.UUID) ([]UserSession, error) {
	return slices.Clone(f.activeByUser[userID]), nil
}

func (f *fakeSessions) IsSessionActive(_ context.Context, userID, sessionID uuid.UUID) (bool, error) {
	for _, s := range f.activeByUser[userID] {
		if s.ID == sessionID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSessions) LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	return f.IsSessionActive(ctx, userID, sessionID)
}

func (f *fakeSessions) RevokeUserSession(_ context.Context, userID, sessionID uuid.UUID) (bool, error) {
	items := f.activeByUser[userID]
	for i, s := range items {
		if s.ID == sessionID {
			f.activeByUser[userID] = append(items[:i], items[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeSessions) RevokeOtherUserSessions(_ context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	items := f.activeByUser[userID]
	kept := make([]UserSession, 0, 1)
	var removed int64
	for _, s := range items {
		if s.ID == keepSessionID {
			kept = append(kept, s)
		} else {
			removed++
		}
	}
	f.activeByUser[userID] = kept
	return removed, nil
}

type fakeAvatarFiles struct {
	valid   map[uuid.UUID]uuid.UUID // fileID -> ownerID
	signErr error
}

func newFakeAvatarFiles() *fakeAvatarFiles {
	return &fakeAvatarFiles{valid: make(map[uuid.UUID]uuid.UUID)}
}

func (f *fakeAvatarFiles) ValidateAndLockAvatarFile(_ context.Context, fileID, ownerID uuid.UUID) error {
	owner, ok := f.valid[fileID]
	if !ok || owner != ownerID {
		return apperr.NotFound("FILES_NOT_FOUND")
	}
	return nil
}

func (f *fakeAvatarFiles) SignAvatarURL(_ context.Context, fileID uuid.UUID) (string, error) {
	if f.signErr != nil {
		return "", f.signErr
	}
	if _, ok := f.valid[fileID]; !ok {
		return "", apperr.NotFound("FILES_NOT_FOUND")
	}
	return "/api/v1/files/signed/" + fileID.String(), nil
}

type fakeAudit struct {
	entries []AuditEntry
}

func (f *fakeAudit) Record(_ context.Context, entry AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

type testFixture struct {
	svc         *Service
	store       *fakeStore
	hasher      *fakeHasher
	roles       *fakeRoles
	sessions    *fakeSessions
	avatarFiles *fakeAvatarFiles
	audit       *fakeAudit
	superID     uuid.UUID
	superSessID uuid.UUID
	mgrID       uuid.UUID
	mgrSessID   uuid.UUID
}

func newTestFixture(t *testing.T) *testFixture {
	t.Helper()

	st := newFakeStore()
	hasher := &fakeHasher{}
	roles := newFakeRoles()
	sess := newFakeSessions()
	avatarFiles := newFakeAvatarFiles()
	aud := &fakeAudit{}
	svc := newServiceWithStore(st, hasher, ServiceDeps{
		Roles:    roles,
		Sessions: sess,
		Files:    avatarFiles,
		Audit:    aud,
	})

	superUser, err := svc.CreateInitialAdmin(t.Context(), CreateInitialAdminInput{
		Username: "root",
		Email:    "root@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("CreateInitialAdmin: %v", err)
	}
	roles.userRoles[superUser.ID] = []uuid.UUID{roles.roleID("super_admin")}
	superSessID := sess.seedActive(superUser.ID)

	mgr, err := svc.CreateUser(t.Context(), CreateUserInput{
		Username:       "manager",
		Email:          "manager@example.com",
		Password:       "password123",
		RoleIDs:        []uuid.UUID{roles.roleID("viewer")},
		ActorID:        &superUser.ID,
		ActorSessionID: superSessID,
	})
	if err != nil {
		t.Fatalf("CreateUser(manager): %v", err)
	}
	mgrSessID := sess.seedActive(mgr.ID)
	hasher.hashCalls = 0
	roles.replaceCalls = 0

	return &testFixture{
		svc:         svc,
		store:       st,
		hasher:      hasher,
		roles:       roles,
		sessions:    sess,
		avatarFiles: avatarFiles,
		audit:       aud,
		superID:     superUser.ID,
		superSessID: superSessID,
		mgrID:       mgr.ID,
		mgrSessID:   mgrSessID,
	}
}

func TestIdentityLocalesComplete(t *testing.T) {
	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("identity", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("bundle.Validate() error = %v", err)
	}

	keys := []string{
		ErrCodeUsernameConflict,
		ErrCodeEmailConflict,
		ErrCodeInvalidPassword,
		ErrCodeUserNotFound,
		ErrCodeLastSuperAdmin,
		ErrCodeSelfOperation,
		ErrCodeSuperAdminProtect,
		ErrCodeInvalidRole,
		ErrCodeInvalidUsername,
		ErrCodeInvalidEmail,
		ErrCodeInvalidDisplay,
		ErrCodeInvalidStatus,
		ErrCodeInvalidPage,
		ErrCodeInvalidPageSize,
		ErrCodeQueryTooLong,
		ErrCodeTooManyRoleFilters,
		ErrCodeProfileInvalidCurrentPassword,
		ErrCodeProfilePasswordUnchanged,
		ErrCodeProfileInvalidAvatar,
		ErrCodeProfileSessionNotFound,
		ErrCodeProfileCurrentSessionRevoke,
	}
	for _, key := range keys {
		zh := bundle.Translate(i18n.LangZhCN, key, nil)
		en := bundle.Translate(i18n.LangEnUS, key, nil)
		if zh == "" || zh == key {
			t.Errorf("missing zh-CN translation for %s", key)
		}
		if en == "" || en == key {
			t.Errorf("missing en-US translation for %s", key)
		}
		if zh == en {
			t.Errorf("expected distinct zh-CN and en-US messages for %s, got %q", key, zh)
		}
	}
}

func TestCreateUserValidationAndProtection(t *testing.T) {
	t.Run("rejects invalid fields with 422 without invoking Argon2id", func(t *testing.T) {
		fx := newTestFixture(t)
		_, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "a",
			Email:          "bad-email",
			Password:       "short",
			Status:         "bogus",
			RoleIDs:        []uuid.UUID{uuid.Must(uuid.NewV7())},
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %v", err)
		}
		if len(appErr.Fields) != 5 {
			t.Fatalf("expected 5 field errors, got %+v", appErr.Fields)
		}
		if fx.hasher.hashCalls != 0 {
			t.Fatalf("expected 0 Argon2id hash calls on 422 validation failure, got %d", fx.hasher.hashCalls)
		}
	})

	t.Run("rejects NUL byte in displayName with 422", func(t *testing.T) {
		fx := newTestFixture(t)
		_, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "validuser",
			Email:          "valid@example.com",
			DisplayName:    "Bad\x00Name",
			Password:       "password123",
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "displayName" {
			t.Fatalf("expected 422 displayName error, got %v", err)
		}
	})

	t.Run("forbids non-super-admin from granting super_admin without hashing", func(t *testing.T) {
		fx := newTestFixture(t)
		_, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "escalated",
			Email:          "esc@example.com",
			Password:       "password123",
			RoleIDs:        []uuid.UUID{fx.roles.roleID("super_admin")},
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusForbidden || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected 403 %s, got %v", ErrCodeSuperAdminProtect, err)
		}
		if fx.hasher.hashCalls != 0 {
			t.Fatalf("expected 0 Argon2id hash calls on 403 pre-check, got %d", fx.hasher.hashCalls)
		}
	})

	t.Run("blocks concurrent super_admin demotion TOCTOU inside CreateUser tx", func(t *testing.T) {
		fx := newTestFixture(t)
		racedSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				// Simulate another super_admin demoting root between pre-check and tx acquisition.
				fx.roles.userRoles[fx.superID] = []uuid.UUID{fx.roles.roleID("viewer")}
			}},
			Roles:    fx.roles,
			Sessions: fx.sessions,
			Audit:    fx.audit,
		})
		_, err := racedSvc.CreateUser(t.Context(), CreateUserInput{
			Username:       "backdoor",
			Email:          "backdoor@example.com",
			Password:       "password123",
			RoleIDs:        []uuid.UUID{fx.roles.roleID("super_admin")},
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusForbidden || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected 403 %s after concurrent demotion, got %v", ErrCodeSuperAdminProtect, err)
		}
	})

	t.Run("blocks disabled, deleted, or revoked-session admin on CreateUser", func(t *testing.T) {
		fx := newTestFixture(t)
		fx.store.users[fx.superID].user.Status = StatusDisabled
		_, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "blocked1",
			Email:          "blocked1@example.com",
			Password:       "password123",
			RoleIDs:        []uuid.UUID{fx.roles.roleID("super_admin")},
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("expected 403 %s for disabled actor, got %v", errCodeAccountDisabled, err)
		}

		fx.store.users[fx.superID].user.Status = StatusActive
		_, err = fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "blocked2",
			Email:          "blocked2@example.com",
			Password:       "password123",
			ActorID:        &fx.superID,
			ActorSessionID: uuid.Must(uuid.NewV7()),
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("expected 401 for revoked actor session, got %v", err)
		}
	})

	t.Run("conflict username and email return 409", func(t *testing.T) {
		fx := newTestFixture(t)
		_, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "MANAGER",
			Email:          "other@example.com",
			Password:       "password123",
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeUsernameConflict {
			t.Fatalf("expected %s, got %v", ErrCodeUsernameConflict, err)
		}

		_, err = fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "unique_user",
			Email:          "MANAGER@example.com",
			Password:       "password123",
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeEmailConflict {
			t.Fatalf("expected %s, got %v", ErrCodeEmailConflict, err)
		}
	})

	t.Run("returns deterministic role ordering and records clean audit metadata", func(t *testing.T) {
		fx := newTestFixture(t)
		created, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "ordered",
			Email:          "ordered@example.com",
			Password:       "password123",
			RoleIDs:        []uuid.UUID{fx.roles.roleID("viewer"), fx.roles.roleID("editor"), fx.roles.roleID("super_admin")},
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		gotCodes := []string{created.Roles[0].Code, created.Roles[1].Code, created.Roles[2].Code}
		wantCodes := []string{"super_admin", "editor", "viewer"}
		if !slices.Equal(gotCodes, wantCodes) {
			t.Fatalf("roles order = %v, want %v", gotCodes, wantCodes)
		}
		last := fx.audit.entries[len(fx.audit.entries)-1]
		if last.Action != auditActionCreate || last.ActorUsername != "root" {
			t.Fatalf("unexpected audit entry: %+v", last)
		}
		if _, hasPw := last.Metadata["password"]; hasPw {
			t.Fatalf("audit metadata must not contain password")
		}
	})
}

func TestUpdateAndDeleteUserGuards(t *testing.T) {
	t.Run("self disable, self demote, self delete forbidden", func(t *testing.T) {
		fx := newTestFixture(t)
		disabled := StatusDisabled
		_, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.superID,
			Status:         &disabled,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self disable, got %v", ErrCodeSelfOperation, err)
		}

		emptyRoles := []uuid.UUID{}
		_, err = fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.superID,
			RoleIDs:        &emptyRoles,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self super_admin removal, got %v", ErrCodeSelfOperation, err)
		}

		err = fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.superID,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self delete, got %v", ErrCodeSelfOperation, err)
		}
	})

	t.Run("disabled admin cannot self-re-enable, update others, or delete others", func(t *testing.T) {
		fx := newTestFixture(t)
		fx.store.users[fx.mgrID].user.Status = StatusDisabled

		active := StatusActive
		_, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Status:         &active,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled self-re-enable = %v, want 403 %s", err, errCodeAccountDisabled)
		}

		err = fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.mgrID,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled self-delete = %v, want 403 %s", err, errCodeAccountDisabled)
		}

		fx.store.users[fx.superID].user.Status = StatusDisabled
		pw := "hijacked123"
		_, err = fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Password:       &pw,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled super_admin resetting password = %v, want 403 %s", err, errCodeAccountDisabled)
		}
		if fx.hasher.hashCalls != 0 {
			t.Fatalf("expected 0 hash calls when actor is disabled, got %d", fx.hasher.hashCalls)
		}
	})

	t.Run("non-super-admin cannot edit, grant super_admin, or delete super_admin", func(t *testing.T) {
		fx := newTestFixture(t)
		newDisplay := "Hacked"
		_, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.superID,
			DisplayName:    &newDisplay,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected %s, got %v", ErrCodeSuperAdminProtect, err)
		}

		grantSuper := []uuid.UUID{fx.roles.roleID("super_admin")}
		_, err = fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			RoleIDs:        &grantSuper,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected %s when non-super grants super_admin, got %v", ErrCodeSuperAdminProtect, err)
		}

		err = fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.superID,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected %s, got %v", ErrCodeSuperAdminProtect, err)
		}
	})

	t.Run("last super admin invariant and disabling second super admin", func(t *testing.T) {
		fx := newTestFixture(t)
		// Sole active super_admin cannot be removed.
		if err := fx.svc.ensureOtherActiveSuperAdmin(t.Context(), fx.superID); err == nil {
			t.Fatalf("expected %s for sole super_admin", ErrCodeLastSuperAdmin)
		} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeLastSuperAdmin {
			t.Fatalf("expected %s, got %v", ErrCodeLastSuperAdmin, err)
		}

		secondSuper, err := fx.svc.CreateUser(t.Context(), CreateUserInput{
			Username:       "super2",
			Email:          "super2@example.com",
			Password:       "password123",
			RoleIDs:        []uuid.UUID{fx.roles.roleID("super_admin")},
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if err != nil {
			t.Fatalf("create second super admin: %v", err)
		}

		// With two active super admins, disabling secondSuper succeeds and revokes its sessions.
		disabled := StatusDisabled
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         secondSuper.ID,
			Status:         &disabled,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("expected disabling second super to succeed, got %v", err)
		}
		if !slices.Contains(fx.sessions.revoked, secondSuper.ID) {
			t.Fatalf("expected sessions revoked on disable")
		}

		// Even though secondSuper still holds the super_admin role while disabled, root is the sole ACTIVE super_admin.
		if err := fx.svc.ensureOtherActiveSuperAdmin(t.Context(), fx.superID); err == nil {
			t.Fatalf("expected %s when only other super_admin is disabled", ErrCodeLastSuperAdmin)
		} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeLastSuperAdmin {
			t.Fatalf("expected %s, got %v", ErrCodeLastSuperAdmin, err)
		}
	})

	t.Run("update validation skips Argon2id, handles email conflict, 404, empty displayName fallback, and no-op roles", func(t *testing.T) {
		fx := newTestFixture(t)

		badEmail := "not-an-email"
		validPw := "password123"
		_, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Email:          &badEmail,
			Password:       &validPw,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %v", err)
		}
		if fx.hasher.hashCalls != 0 {
			t.Fatalf("expected 0 Argon2id calls on invalid UpdateUser, got %d", fx.hasher.hashCalls)
		}

		dupEmail := "ROOT@example.com"
		_, err = fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Email:          &dupEmail,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeEmailConflict {
			t.Fatalf("expected %s, got %v", ErrCodeEmailConflict, err)
		}

		_, err = fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         uuid.Must(uuid.NewV7()),
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeUserNotFound {
			t.Fatalf("expected %s, got %v", ErrCodeUserNotFound, err)
		}

		// Empty displayName falls back to username; submitting unchanged roleIds skips ReplaceUserRoles & "roles" audit diff.
		emptyDisplay := "   "
		sameRoles := []uuid.UUID{fx.roles.roleID("viewer")}
		updated, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			DisplayName:    &emptyDisplay,
			RoleIDs:        &sameRoles,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		})
		if err != nil {
			t.Fatalf("UpdateUser: %v", err)
		}
		if updated.DisplayName != "manager" {
			t.Fatalf("DisplayName = %q, want fallback username manager", updated.DisplayName)
		}
		if fx.roles.replaceCalls != 0 {
			t.Fatalf("expected 0 ReplaceUserRoles calls on unchanged role set, got %d", fx.roles.replaceCalls)
		}
		lastAudit := fx.audit.entries[len(fx.audit.entries)-1]
		changedFields, _ := lastAudit.Metadata["changedFields"].([]string)
		if slices.Contains(changedFields, "roles") {
			t.Fatalf("unchanged roles must not appear in audit changedFields: %v", changedFields)
		}
	})

	t.Run("reset password revokes sessions and delete clears roles and hides user with 404", func(t *testing.T) {
		fx := newTestFixture(t)
		newPw := "newpassword123"
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Password:       &newPw,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("reset password: %v", err)
		}
		if !slices.Contains(fx.sessions.revoked, fx.mgrID) {
			t.Fatalf("expected sessions revoked on password reset")
		}
		lastUpdateAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if lastUpdateAudit.Metadata["passwordReset"] != true || lastUpdateAudit.ActorUsername != "root" {
			t.Fatalf("unexpected password reset audit metadata: %+v", lastUpdateAudit)
		}
		if _, hasPw := lastUpdateAudit.Metadata["password"]; hasPw {
			t.Fatalf("update audit must never contain password")
		}

		fx.sessions.revoked = nil
		if err := fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.mgrID,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("delete manager: %v", err)
		}
		if !slices.Contains(fx.sessions.revoked, fx.mgrID) {
			t.Fatalf("expected sessions revoked on delete")
		}
		if len(fx.roles.userRoles[fx.mgrID]) != 0 {
			t.Fatalf("expected user roles cleared on soft delete")
		}
		_, err := fx.svc.GetUser(t.Context(), fx.mgrID)
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusNotFound || appErr.Code != ErrCodeUserNotFound {
			t.Fatalf("expected 404 %s after delete, got %v", ErrCodeUserNotFound, err)
		}
		lastDeleteAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if lastDeleteAudit.Action != auditActionDelete || lastDeleteAudit.ActorUsername != "root" {
			t.Fatalf("unexpected delete audit: %+v", lastDeleteAudit)
		}
	})
}

func TestServiceReadMethodsHelpersAndErrorNormalization(t *testing.T) {
	t.Run("CreateInitialAdmin returns 422 VALIDATION_FAILED with field errors and handles conflicts", func(t *testing.T) {
		fx := newTestFixture(t)
		_, err := fx.svc.CreateInitialAdmin(t.Context(), CreateInitialAdminInput{
			Username: "a",
			Email:    "bad",
			Password: "short",
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Code != apperr.CodeValidationFailed {
			t.Fatalf("expected 422 VALIDATION_FAILED, got %v", err)
		}
		if len(appErr.Fields) != 3 {
			t.Fatalf("expected 3 field errors including password, got %+v", appErr.Fields)
		}
		if fx.hasher.hashCalls != 0 {
			t.Fatalf("expected 0 Argon2id calls on invalid CreateInitialAdmin, got %d", fx.hasher.hashCalls)
		}

		_, err = fx.svc.CreateInitialAdmin(t.Context(), CreateInitialAdminInput{
			Username: "ROOT",
			Email:    "fresh@example.com",
			Password: "password123",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeUsernameConflict {
			t.Fatalf("expected %s, got %v", ErrCodeUsernameConflict, err)
		}
	})

	t.Run("CountUsers, GetAuthCredentials, GetByID, RecordLogin, UpdatePasswordHash, ListUsernamesByIDs", func(t *testing.T) {
		fx := newTestFixture(t)
		count, err := fx.svc.CountUsers(t.Context())
		if err != nil || count != 2 {
			t.Fatalf("CountUsers = (%d, %v), want (2, nil)", count, err)
		}

		avatarID := uuid.Must(uuid.NewV7())
		fx.store.users[fx.mgrID].user.AvatarFileID = &avatarID

		creds, err := fx.svc.GetAuthCredentials(t.Context(), "  MANAGER@EXAMPLE.COM ")
		if err != nil || creds.ID != fx.mgrID || creds.AvatarFileID == nil || *creds.AvatarFileID != avatarID {
			t.Fatalf("GetAuthCredentials = (%+v, %v)", creds, err)
		}
		if _, err := fx.svc.GetAuthCredentials(t.Context(), "bad\x00user"); err == nil {
			t.Fatal("expected not found for NUL byte identifier")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeUserNotFound {
			t.Fatalf("expected %s, got %v", ErrCodeUserNotFound, err)
		}

		user, err := fx.svc.GetByID(t.Context(), fx.mgrID)
		if err != nil || user.Username != "manager" {
			t.Fatalf("GetByID = (%+v, %v)", user, err)
		}
		if _, err := fx.svc.GetByID(t.Context(), uuid.Must(uuid.NewV7())); err == nil {
			t.Fatal("expected 404 on unknown GetByID")
		}

		if err := fx.svc.RecordLogin(t.Context(), fx.mgrID); err != nil || fx.store.users[fx.mgrID].lastLoginAt == nil {
			t.Fatalf("RecordLogin failed: err=%v lastLoginAt=%v", err, fx.store.users[fx.mgrID].lastLoginAt)
		}
		if err := fx.svc.UpdatePasswordHash(t.Context(), fx.mgrID, "hash:rehashed"); err != nil || fx.store.users[fx.mgrID].passwordHash != "hash:rehashed" {
			t.Fatalf("UpdatePasswordHash failed: err=%v", err)
		}

		// Soft-delete mgrID and confirm ListUsernamesByIDs preserves historical username while GetAuthCredentials hides it.
		if err := fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.mgrID,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		if _, err := fx.svc.GetAuthCredentials(t.Context(), "manager"); err == nil {
			t.Fatal("expected soft-deleted user hidden from GetAuthCredentials")
		}
		names, err := fx.svc.ListUsernamesByIDs(t.Context(), []uuid.UUID{fx.superID, fx.superID, fx.mgrID})
		if err != nil || names[fx.superID] != "root" || names[fx.mgrID] != "manager" {
			t.Fatalf("ListUsernamesByIDs = (%+v, %v), want both active and soft-deleted usernames", names, err)
		}
		inUse, err := fx.svc.IsAvatarFileInUse(t.Context(), avatarID)
		if err != nil || inUse {
			t.Fatalf("IsAvatarFileInUse after soft-delete = (%v, %v), want (false, nil)", inUse, err)
		}
	})

	t.Run("ListUsers filters, sort/order, zero-match role early return, and NUL rejection", func(t *testing.T) {
		fx := newTestFixture(t)
		res, err := fx.svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(1, 10),
			Q:          "manager",
			Statuses:   []string{StatusActive},
			RoleIDs:    []uuid.UUID{fx.roles.roleID("viewer")},
			Sort:       "username",
			Order:      "asc",
		})
		if err != nil || res.Total != 1 || len(res.Items) != 1 || res.Items[0].Username != "manager" {
			t.Fatalf("ListUsers filtered = (%+v, %v)", res, err)
		}

		// Role with zero assigned users returns empty slice early.
		emptyRes, err := fx.svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(1, 10),
			RoleIDs:    []uuid.UUID{fx.roles.roleID("editor")},
		})
		if err != nil || emptyRes.Total != 0 || emptyRes.Items == nil || len(emptyRes.Items) != 0 {
			t.Fatalf("ListUsers zero-match role = (%+v, %v)", emptyRes, err)
		}

		// Out-of-range page returns empty items with accurate total.
		page2, err := fx.svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(2, 10),
		})
		if err != nil || page2.Total != 2 || len(page2.Items) != 0 {
			t.Fatalf("ListUsers page 2 = (%+v, %v)", page2, err)
		}

		if _, err := fx.svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(1, 10),
			Q:          "bad\x00query",
		}); err == nil {
			t.Fatal("expected 422 on NUL byte query")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity {
			t.Fatalf("expected 422, got %v", err)
		}
	})

	t.Run("withinTx and collaborator raw errors are normalized to *apperr.Error", func(t *testing.T) {
		fx := newTestFixture(t)
		txFailSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &failingTx{err: errors.New("commit failed")},
		})
		err := txFailSvc.DeleteUser(t.Context(), DeleteUserInput{
			UserID: fx.mgrID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError || appErr.Code != apperr.CodeInternal {
			t.Fatalf("expected normalized 500 *apperr.Error from withinTx, got %v", err)
		}

		fx.roles.listErr = errors.New("role db timeout")
		_, err = fx.svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(1, 10),
			RoleIDs:    []uuid.UUID{fx.roles.roleID("viewer")},
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusInternalServerError || appErr.Code != apperr.CodeInternal {
			t.Fatalf("expected normalized 500 *apperr.Error from RoleManager, got %v", err)
		}
	})

	t.Run("pure store and request helpers", func(t *testing.T) {
		if got := escapeLikePattern(`100%_a\b`); got != `100\%\_a\\b` {
			t.Fatalf("escapeLikePattern = %q", got)
		}
		if got := mapUserUniqueConflict(&pgconn.PgError{Code: "23505", ConstraintName: "uq_users_username_lower"}); !errors.Is(got, errUsernameConflict) {
			t.Fatalf("mapUserUniqueConflict(username) = %v", got)
		}
		if got := mapUserUniqueConflict(&pgconn.PgError{Code: "23505", ConstraintName: "uq_users_email_lower"}); !errors.Is(got, errEmailConflict) {
			t.Fatalf("mapUserUniqueConflict(email) = %v", got)
		}
		if got := mapUserUniqueConflict(&pgconn.PgError{Code: "23505", ConstraintName: "other"}); got != nil {
			t.Fatalf("mapUserUniqueConflict(other) = %v, want nil", got)
		}
		if got := normalizeUserSort(""); got != "created_at" {
			t.Fatalf("normalizeUserSort('') = %q, want created_at", got)
		}

		var optNull optionalNullableString
		if err := json.Unmarshal([]byte(`null`), &optNull); err != nil || !optNull.Set || optNull.Value != nil {
			t.Fatalf("UnmarshalJSON(null) = (%+v, %v)", optNull, err)
		}
		var optStr optionalNullableString
		if err := json.Unmarshal([]byte(`"abc"`), &optStr); err != nil || !optStr.Set || optStr.Value == nil || *optStr.Value != "abc" {
			t.Fatalf("UnmarshalJSON(string) = (%+v, %v)", optStr, err)
		}
		var optBad optionalNullableString
		if err := json.Unmarshal([]byte(`123`), &optBad); err == nil {
			t.Fatal("expected error unmarshaling number into optionalNullableString")
		}
	})
}

func TestProfileUpdateChangePasswordAndSessions(t *testing.T) {
	t.Run("update profile fields, bind avatar, clear avatar, graceful sign error, and enforce uniqueness/ownership", func(t *testing.T) {
		fx := newTestFixture(t)
		avatarA := uuid.Must(uuid.NewV7())
		avatarForeign := uuid.Must(uuid.NewV7())
		fx.avatarFiles.valid[avatarA] = fx.mgrID
		fx.avatarFiles.valid[avatarForeign] = fx.superID

		// Revoked/unknown session is rejected with 401 on GetProfile.
		if _, err := fx.svc.GetProfile(t.Context(), fx.mgrID, uuid.Must(uuid.NewV7())); err == nil {
			t.Fatal("expected 401 on GetProfile with inactive session")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %v", err)
		}

		newDisplay := "Manager Display"
		newEmail := "manager.new@example.com"
		updated, err := fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			DisplayName:      &newDisplay,
			Email:            &newEmail,
			AvatarFileID:     &avatarA,
		})
		if err != nil {
			t.Fatalf("UpdateProfile: %v", err)
		}
		if updated.DisplayName != newDisplay || updated.Email != newEmail {
			t.Fatalf("unexpected updated profile: %+v", updated)
		}
		if updated.AvatarFileID == nil || *updated.AvatarFileID != avatarA {
			t.Fatalf("expected avatar %s, got %v", avatarA, updated.AvatarFileID)
		}
		if updated.AvatarURL == nil || !strings.Contains(*updated.AvatarURL, avatarA.String()) {
			t.Fatalf("expected signed avatar URL, got %v", updated.AvatarURL)
		}
		inUse, err := fx.svc.IsAvatarFileInUse(t.Context(), avatarA)
		if err != nil || !inUse {
			t.Fatalf("IsAvatarFileInUse = (%v, %v), want (true, nil)", inUse, err)
		}

		// SignAvatarURL failure degrades gracefully (returns AvatarURL: nil without failing GetProfile).
		fx.avatarFiles.signErr = errors.New("signer unavailable")
		degraded, err := fx.svc.GetProfile(t.Context(), fx.mgrID, fx.mgrSessID)
		if err != nil || degraded.AvatarFileID == nil || degraded.AvatarURL != nil {
			t.Fatalf("degraded GetProfile = (%+v, %v), want nil AvatarURL with nil error", degraded, err)
		}
		fx.avatarFiles.signErr = nil

		// Binding another user's avatar must fail with 422 on avatarFileId.
		_, err = fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			AvatarFileID:     &avatarForeign,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "avatarFileId" {
			t.Fatalf("expected 422 avatarFileId for foreign avatar, got %v", err)
		}

		// Duplicate email must fail with 409 USERS_EMAIL_CONFLICT.
		dupEmail := "ROOT@example.com"
		_, err = fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			Email:            &dupEmail,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeEmailConflict {
			t.Fatalf("expected %s, got %v", ErrCodeEmailConflict, err)
		}

		// ClearAvatar explicitly removes avatar reference.
		cleared, err := fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			ClearAvatar:      true,
		})
		if err != nil {
			t.Fatalf("UpdateProfile(ClearAvatar): %v", err)
		}
		if cleared.AvatarFileID != nil || cleared.AvatarURL != nil {
			t.Fatalf("expected cleared avatar, got id=%v url=%v", cleared.AvatarFileID, cleared.AvatarURL)
		}
	})

	t.Run("disabled and deleted accounts are rejected on all profile endpoints", func(t *testing.T) {
		fx := newTestFixture(t)
		fx.store.users[fx.mgrID].user.Status = StatusDisabled

		assert403 := func(op string, err error) {
			t.Helper()
			if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
				t.Fatalf("%s disabled = %v, want 403 %s", op, err, errCodeAccountDisabled)
			}
		}
		_, err := fx.svc.GetProfile(t.Context(), fx.mgrID, fx.mgrSessID)
		assert403("GetProfile", err)
		_, err = fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{UserID: fx.mgrID, CurrentSessionID: fx.mgrSessID})
		assert403("UpdateProfile", err)
		_, err = fx.svc.ListProfileSessions(t.Context(), fx.mgrID, fx.mgrSessID)
		assert403("ListProfileSessions", err)
		_, err = fx.svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{UserID: fx.mgrID, CurrentSessionID: fx.mgrSessID})
		assert403("RevokeOtherProfileSessions", err)

		ts := time.Now().UTC()
		fx.store.users[fx.mgrID].user.Status = StatusActive
		fx.store.users[fx.mgrID].deletedAt = &ts
		if _, err := fx.svc.GetProfile(t.Context(), fx.mgrID, fx.mgrSessID); err == nil {
			t.Fatal("expected 401 on soft-deleted GetProfile")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %v", err)
		}
	})

	t.Run("change password verifies old password, forbids unchanged, and revokes other sessions", func(t *testing.T) {
		fx := newTestFixture(t)
		curSess := fx.mgrSessID
		otherSess1 := uuid.Must(uuid.NewV7())
		otherSess2 := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		fx.sessions.activeByUser[fx.mgrID] = []UserSession{
			{ID: otherSess1, IPAddress: "10.0.0.2", UserAgent: "Other1", LastUsedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: curSess, IPAddress: "10.0.0.1", UserAgent: "Current", LastUsedAt: now.Add(-2 * time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: otherSess2, IPAddress: "10.0.0.3", UserAgent: "Other2", LastUsedAt: now.Add(-3 * time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		}

		// Wrong current password returns 400 PROFILE_INVALID_CURRENT_PASSWORD + failure audit.
		_, err := fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "wrong-password",
			NewPassword:      "new-password-456",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusBadRequest || appErr.Code != ErrCodeProfileInvalidCurrentPassword {
			t.Fatalf("expected 400 %s, got %v", ErrCodeProfileInvalidCurrentPassword, err)
		}
		lastAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if lastAudit.Action != auditActionProfileChangePassword || lastAudit.Status != auditStatusFailure {
			t.Fatalf("expected failure audit for bad password, got %+v", lastAudit)
		}

		// Identical new password rejected with 422 PROFILE_PASSWORD_UNCHANGED.
		_, err = fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "password123",
			NewPassword:      "password123",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) != 1 || appErr.Fields[0].Message != ErrCodeProfilePasswordUnchanged {
			t.Fatalf("expected 422 %s, got %v", ErrCodeProfilePasswordUnchanged, err)
		}

		// Valid change with default revokeOtherSessions=true revokes 2 other sessions.
		res, err := fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "password123",
			NewPassword:      "new-password-456",
		})
		if err != nil {
			t.Fatalf("ChangePassword: %v", err)
		}
		if res.RevokedSessions != 2 {
			t.Fatalf("RevokedSessions = %d, want 2", res.RevokedSessions)
		}
		if fx.store.users[fx.mgrID].passwordHash != "hash:new-password-456" {
			t.Fatalf("passwordHash not updated: %q", fx.store.users[fx.mgrID].passwordHash)
		}
		if len(fx.sessions.activeByUser[fx.mgrID]) != 1 || fx.sessions.activeByUser[fx.mgrID][0].ID != curSess {
			t.Fatalf("expected only current session retained, got %+v", fx.sessions.activeByUser[fx.mgrID])
		}

		// revokeOtherSessions=false keeps other sessions intact.
		keepFalse := false
		extraSess := uuid.Must(uuid.NewV7())
		fx.sessions.activeByUser[fx.mgrID] = append(fx.sessions.activeByUser[fx.mgrID], UserSession{
			ID: extraSess, IPAddress: "10.0.0.4", UserAgent: "Extra", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		res2, err := fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:              fx.mgrID,
			CurrentSessionID:    curSess,
			CurrentPassword:     "new-password-456",
			NewPassword:         "another-password-789",
			RevokeOtherSessions: &keepFalse,
		})
		if err != nil || res2.RevokedSessions != 0 {
			t.Fatalf("ChangePassword(revoke=false) = (%+v, %v), want 0 revoked", res2, err)
		}
		if len(fx.sessions.activeByUser[fx.mgrID]) != 2 {
			t.Fatalf("expected 2 sessions preserved, got %d", len(fx.sessions.activeByUser[fx.mgrID]))
		}
	})

	t.Run("sessions list pins current first and guards self/foreign revocation", func(t *testing.T) {
		fx := newTestFixture(t)
		curSess := fx.mgrSessID
		other1 := uuid.Must(uuid.NewV7())
		other2 := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		fx.sessions.activeByUser[fx.mgrID] = []UserSession{
			{ID: other1, IPAddress: "10.0.0.10", UserAgent: "Other1", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			{ID: curSess, IPAddress: "10.0.0.11", UserAgent: "Cur", LastUsedAt: now.Add(-time.Minute), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			{ID: other2, IPAddress: "10.0.0.12", UserAgent: "Other2", LastUsedAt: now.Add(-2 * time.Minute), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		}

		list, err := fx.svc.ListProfileSessions(t.Context(), fx.mgrID, curSess)
		if err != nil {
			t.Fatalf("ListProfileSessions: %v", err)
		}
		if len(list) != 3 || !list[0].IsCurrent || list[0].ID != curSess {
			t.Fatalf("expected current session pinned first, got %+v", list)
		}

		// Revoking current session is forbidden (409).
		err = fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			SessionID:        curSess,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusConflict || appErr.Code != ErrCodeProfileCurrentSessionRevoke {
			t.Fatalf("expected 409 %s, got %v", ErrCodeProfileCurrentSessionRevoke, err)
		}

		// Revoking unknown/foreign session returns 404.
		err = fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			SessionID:        uuid.Must(uuid.NewV7()),
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusNotFound || appErr.Code != ErrCodeProfileSessionNotFound {
			t.Fatalf("expected 404 %s, got %v", ErrCodeProfileSessionNotFound, err)
		}

		// Revoking another own session succeeds.
		if err := fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			SessionID:        other1,
		}); err != nil {
			t.Fatalf("RevokeProfileSession(other1): %v", err)
		}

		// RevokeOtherProfileSessions revokes remaining non-current session (other2).
		revoked, err := fx.svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
		})
		if err != nil || revoked != 1 {
			t.Fatalf("RevokeOtherProfileSessions = (%d, %v), want (1, nil)", revoked, err)
		}

		// A revoked session must receive 401 and cannot revoke the remaining active session.
		if err := fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: other1,
			SessionID:        curSess,
		}); err == nil {
			t.Fatal("expected 401 when revoked session tries to revoke active session")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %v", err)
		}
		if _, err := fx.svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           fx.mgrID,
			CurrentSessionID: other1,
		}); err == nil {
			t.Fatal("expected 401 when revoked session tries bulk revoke")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("expected 401, got %v", err)
		}
		if len(fx.sessions.activeByUser[fx.mgrID]) != 1 || fx.sessions.activeByUser[fx.mgrID][0].ID != curSess {
			t.Fatalf("legitimate session must remain untouched, got %+v", fx.sessions.activeByUser[fx.mgrID])
		}

		// Disabled account receives 403 AUTH_ACCOUNT_DISABLED even when targeting its own session ID.
		fx.store.users[fx.mgrID].user.Status = StatusDisabled
		err = fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			SessionID:        curSess,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled account self-revoke = %v, want 403 %s", err, errCodeAccountDisabled)
		}
	})

	t.Run("change password rejects concurrent password mutation, disable, and soft-delete inside tx lock", func(t *testing.T) {
		fx := newTestFixture(t)
		curSess := fx.mgrSessID

		// Simulate an admin resetting password hash between Verify and WithinTx lock.
		origHash := fx.store.users[fx.mgrID].passwordHash
		racedSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				fx.store.users[fx.mgrID].passwordHash = "hash:admin-reset-password"
			}},
			Sessions: fx.sessions,
			Audit:    fx.audit,
		})
		_, err := racedSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  strings.TrimPrefix(origHash, "hash:"),
			NewPassword:      "attacker-new-password",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusBadRequest || appErr.Code != ErrCodeProfileInvalidCurrentPassword {
			t.Fatalf("concurrent password reset race = %v, want 400 %s", err, ErrCodeProfileInvalidCurrentPassword)
		}
		if fx.store.users[fx.mgrID].passwordHash != "hash:admin-reset-password" {
			t.Fatalf("admin reset hash was overwritten: %q", fx.store.users[fx.mgrID].passwordHash)
		}
		lastAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if lastAudit.Status != auditStatusFailure || lastAudit.Metadata["reason"] != "password_changed_concurrently" {
			t.Fatalf("expected concurrent failure audit, got %+v", lastAudit)
		}

		// Simulate an admin disabling the account during Argon2id.
		disabledSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				fx.store.users[fx.mgrID].user.Status = StatusDisabled
			}},
			Sessions: fx.sessions,
			Audit:    fx.audit,
		})
		_, err = disabledSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "admin-reset-password",
			NewPassword:      "disabled-user-pw-1",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("concurrent disable race = %v, want 403 %s", err, errCodeAccountDisabled)
		}
		fx.store.users[fx.mgrID].user.Status = StatusActive

		// Simulate an admin soft-deleting the account during Argon2id.
		deletedSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				ts := time.Now().UTC()
				fx.store.users[fx.mgrID].deletedAt = &ts
			}},
			Sessions: fx.sessions,
			Audit:    fx.audit,
		})
		_, err = deletedSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "admin-reset-password",
			NewPassword:      "deleted-user-pw-1",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnauthorized {
			t.Fatalf("concurrent soft-delete race = %v, want 401", err)
		}
	})
}

type hookTx struct {
	beforeTx func()
}

func (h *hookTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if h.beforeTx != nil {
		h.beforeTx()
	}
	return fn(ctx)
}

type failingTx struct {
	err error
}

func (f *failingTx) WithinTx(_ context.Context, _ func(ctx context.Context) error) error {
	return f.err
}
