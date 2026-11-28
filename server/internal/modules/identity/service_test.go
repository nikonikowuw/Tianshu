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

	"tianshu/internal/platform/database"
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
	users   map[uuid.UUID]*fakeUserRow
	nowTick int64
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users:   make(map[uuid.UUID]*fakeUserRow),
		nowTick: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}
}

func (f *fakeStore) nextTime() time.Time {
	f.nowTick++
	return time.Unix(f.nowTick, 0).UTC()
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
	now := f.nextTime()
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
		now := f.nextTime()
		r.lastLoginAt = &now
	}
	return nil
}

func (f *fakeStore) UpdateUserPasswordHashIfMatches(_ context.Context, id uuid.UUID, expectedHash, newHash string) (int64, error) {
	if r, ok := f.users[id]; ok && r.deletedAt == nil && r.user.Status == StatusActive && r.passwordHash == expectedHash {
		r.passwordHash = newHash
		return 1, nil
	}
	return 0, nil
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
	r.user.UpdatedAt = f.nextTime()
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
	r.user.UpdatedAt = f.nextTime()
	uid := rec.ID
	r.updatedBy = &uid
	return nil
}

func (f *fakeStore) UpdateUserPasswordWithActor(_ context.Context, id uuid.UUID, hash string, updatedBy *uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		r.passwordHash = hash
		r.updatedBy = cloneUUIDPtr(updatedBy)
		r.user.UpdatedAt = f.nextTime()
	}
	return nil
}

func (f *fakeStore) SoftDeleteUser(_ context.Context, id uuid.UUID, updatedBy *uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		now := f.nextTime()
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
	verifyErr error
}

func (h *fakeHasher) Hash(pw string) (string, error) {
	h.hashCalls++
	if err := security.ValidatePasswordLength(pw); err != nil {
		return "", err
	}
	return "hash:" + pw, nil
}

func (h *fakeHasher) Verify(pw, encodedHash string) (bool, bool, error) {
	if h.verifyErr != nil {
		return false, false, h.verifyErr
	}
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
	valid         map[uuid.UUID]uuid.UUID // fileID -> ownerID
	signErr       error
	validateCalls int
}

func newFakeAvatarFiles() *fakeAvatarFiles {
	return &fakeAvatarFiles{valid: make(map[uuid.UUID]uuid.UUID)}
}

func (f *fakeAvatarFiles) ValidateAndLockAvatarFile(_ context.Context, fileID, ownerID uuid.UUID) error {
	f.validateCalls++
	owner, ok := f.valid[fileID]
	if !ok || owner != ownerID {
		return ErrInvalidAvatarFile
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
		Tx:       simTx{},
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
		if !ok || appErr.Status != http.StatusUnprocessableEntity || appErr.Code != apperr.CodeValidationFailed {
			t.Fatalf("expected 422 VALIDATION_FAILED, got %v", err)
		}
		if len(appErr.Fields) != 5 {
			t.Fatalf("expected 5 field errors, got %+v", appErr.Fields)
		}
		gotFields := make(map[string]string, len(appErr.Fields))
		for _, f := range appErr.Fields {
			gotFields[f.Field] = f.Message
		}
		for field, wantMsg := range map[string]string{
			"username": ErrCodeInvalidUsername,
			"email":    ErrCodeInvalidEmail,
			"password": ErrCodeInvalidPassword,
			"status":   ErrCodeInvalidStatus,
			"roleIds":  ErrCodeInvalidRole,
		} {
			if gotFields[field] != wantMsg {
				t.Fatalf("field %q message = %q, want %q", field, gotFields[field], wantMsg)
			}
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
		// Sole active super_admin cannot be removed (defense-in-depth invariant).
		if err := fx.svc.ensureOtherActiveSuperAdmin(t.Context(), fx.superID); !assertAppErrorCode(err, http.StatusConflict, ErrCodeLastSuperAdmin) {
			t.Fatalf("expected 409 %s for sole super_admin, got %v", ErrCodeLastSuperAdmin, err)
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

		// Even though secondSuper still holds super_admin while disabled, root is the sole ACTIVE super_admin.
		if err := fx.svc.ensureOtherActiveSuperAdmin(t.Context(), fx.superID); !assertAppErrorCode(err, http.StatusConflict, ErrCodeLastSuperAdmin) {
			t.Fatalf("expected 409 %s when only other super_admin is disabled, got %v", ErrCodeLastSuperAdmin, err)
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

		// Resetting password against unknown 404 or protected super_admin 403 skips Argon2id hashing.
		fx.hasher.hashCalls = 0
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         uuid.Must(uuid.NewV7()),
			Password:       &validPw,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); !assertAppErrorCode(err, http.StatusNotFound, ErrCodeUserNotFound) || fx.hasher.hashCalls != 0 {
			t.Fatalf("404 password reset precheck failed: err=%v hashCalls=%d", err, fx.hasher.hashCalls)
		}
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.superID,
			Password:       &validPw,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		}); !assertAppErrorCode(err, http.StatusForbidden, ErrCodeSuperAdminProtect) || fx.hasher.hashCalls != 0 {
			t.Fatalf("403 password reset precheck failed: err=%v hashCalls=%d", err, fx.hasher.hashCalls)
		}

		// Role & status changes record diff metadata (roles: before/after, status: from/to) and re-enable works.
		disabled := StatusDisabled
		active := StatusActive
		editorRoles := []uuid.UUID{fx.roles.roleID("editor")}
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Status:         &disabled,
			RoleIDs:        &editorRoles,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("UpdateUser status+roles: %v", err)
		}
		diffAudit := fx.audit.entries[len(fx.audit.entries)-1]
		statusDiff, _ := diffAudit.Metadata["status"].(map[string]string)
		rolesDiff, _ := diffAudit.Metadata["roles"].(map[string]any)
		if statusDiff["from"] != StatusActive || statusDiff["to"] != StatusDisabled || rolesDiff == nil {
			t.Fatalf("expected status and roles diff metadata, got %+v", diffAudit.Metadata)
		}
		if reEnabled, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.mgrID,
			Status:         &active,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil || reEnabled.Status != StatusActive {
			t.Fatalf("re-enable user = (%+v, %v)", reEnabled, err)
		}
		fx.mgrSessID = fx.sessions.seedActive(fx.mgrID)

		// Non-super-admin cannot revoke super_admin role or delete disabled super_admin; hookTx catches concurrent demotion.
		nonSuperRoles := []uuid.UUID{fx.roles.roleID("viewer")}
		if _, err := fx.svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:         fx.superID,
			RoleIDs:        &nonSuperRoles,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		}); !assertAppErrorCode(err, http.StatusForbidden, ErrCodeSuperAdminProtect) {
			t.Fatalf("non-super revoke super_admin err = %v", err)
		}
		fx.store.users[fx.superID].user.Status = StatusDisabled
		if err := fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.superID,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		}); !assertAppErrorCode(err, http.StatusForbidden, ErrCodeSuperAdminProtect) {
			t.Fatalf("non-super delete disabled super_admin err = %v", err)
		}
		fx.store.users[fx.superID].user.Status = StatusActive

		racedDeleteSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				fx.roles.userRoles[fx.mgrID] = []uuid.UUID{fx.roles.roleID("viewer")}
			}},
			Roles:    fx.roles,
			Sessions: fx.sessions,
			Audit:    fx.audit,
		})
		fx.roles.userRoles[fx.mgrID] = []uuid.UUID{fx.roles.roleID("super_admin")}
		if err := racedDeleteSvc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.superID,
			ActorID:        &fx.mgrID,
			ActorSessionID: fx.mgrSessID,
		}); !assertAppErrorCode(err, http.StatusForbidden, ErrCodeSuperAdminProtect) {
			t.Fatalf("concurrent demotion before DeleteUser tx err = %v", err)
		}
		fx.roles.userRoles[fx.mgrID] = []uuid.UUID{fx.roles.roleID("editor")}
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
		if len(appErr.Fields) != 3 ||
			appErr.Fields[0].Field != "email" || appErr.Fields[0].Message != ErrCodeInvalidEmail ||
			appErr.Fields[1].Field != "password" || appErr.Fields[1].Message != ErrCodeInvalidPassword ||
			appErr.Fields[2].Field != "username" || appErr.Fields[2].Message != ErrCodeInvalidUsername {
			t.Fatalf("unexpected CreateInitialAdmin field errors: %+v", appErr.Fields)
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

	t.Run("CountUsers, GetAuthCredentials, GetByID, CompleteLogin, ListUsernamesByIDs", func(t *testing.T) {
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
		if _, err := fx.svc.GetByID(t.Context(), uuid.Must(uuid.NewV7())); !assertAppErrorCode(err, http.StatusNotFound, ErrCodeUserNotFound) {
			t.Fatalf("expected 404 %s on unknown GetByID, got %v", ErrCodeUserNotFound, err)
		}

		// CompleteLogin without rehash updates last_login_at while keeping passwordHash intact.
		if err := fx.svc.CompleteLogin(t.Context(), CompleteLoginInput{
			UserID:               fx.mgrID,
			ExpectedPasswordHash: "hash:password123",
		}); err != nil || fx.store.users[fx.mgrID].lastLoginAt == nil || fx.store.users[fx.mgrID].passwordHash != "hash:password123" {
			t.Fatalf("CompleteLogin without rehash failed: err=%v lastLoginAt=%v", err, fx.store.users[fx.mgrID].lastLoginAt)
		}
		if err := fx.svc.CompleteLogin(t.Context(), CompleteLoginInput{
			UserID:               fx.mgrID,
			ExpectedPasswordHash: "hash:password123",
			UpgradedPasswordHash: "hash:rehashed-v2",
		}); err != nil || fx.store.users[fx.mgrID].passwordHash != "hash:rehashed-v2" {
			t.Fatalf("CompleteLogin CAS rehash failed: err=%v hash=%q", err, fx.store.users[fx.mgrID].passwordHash)
		}
		if err := fx.svc.CompleteLogin(t.Context(), CompleteLoginInput{
			UserID:               fx.mgrID,
			ExpectedPasswordHash: "hash:stale",
			UpgradedPasswordHash: "hash:should-not-overwrite",
		}); !assertAppErrorCode(err, http.StatusUnauthorized, apperr.CodeUnauthorized) || fx.store.users[fx.mgrID].passwordHash != "hash:rehashed-v2" {
			t.Fatalf("CompleteLogin stale hash = (%v, %q), want 401 without clobbering", err, fx.store.users[fx.mgrID].passwordHash)
		}
		if err := fx.svc.CompleteLogin(t.Context(), CompleteLoginInput{
			UserID:               uuid.Must(uuid.NewV7()),
			ExpectedPasswordHash: "hash:any",
		}); !assertAppErrorCode(err, http.StatusNotFound, ErrCodeUserNotFound) {
			t.Fatalf("CompleteLogin missing user = %v, want 404 %s", err, ErrCodeUserNotFound)
		}
		fx.store.users[fx.mgrID].user.Status = StatusDisabled
		if err := fx.svc.CompleteLogin(t.Context(), CompleteLoginInput{
			UserID:               fx.mgrID,
			ExpectedPasswordHash: "hash:rehashed-v2",
		}); !assertAppErrorCode(err, http.StatusForbidden, errCodeAccountDisabled) {
			t.Fatalf("CompleteLogin disabled user = %v, want 403 %s", err, errCodeAccountDisabled)
		}
		fx.store.users[fx.mgrID].user.Status = StatusActive

		// Soft-delete mgrID and confirm ListUsernamesByIDs preserves historical username while GetAuthCredentials hides it.
		if err := fx.svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:         fx.mgrID,
			ActorID:        &fx.superID,
			ActorSessionID: fx.superSessID,
		}); err != nil {
			t.Fatalf("DeleteUser: %v", err)
		}
		if _, err := fx.svc.GetAuthCredentials(t.Context(), "manager"); !assertAppErrorCode(err, http.StatusNotFound, ErrCodeUserNotFound) {
			t.Fatalf("expected 404 %s for soft-deleted GetAuthCredentials, got %v", ErrCodeUserNotFound, err)
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

		noTxSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{
			Roles: fx.roles,
		})
		if err := noTxSvc.DeleteUser(t.Context(), DeleteUserInput{UserID: fx.mgrID}); !assertAppErrorCode(err, http.StatusInternalServerError, apperr.CodeInternal) {
			t.Fatalf("expected fail-closed 500 when s.tx is nil, got %v", err)
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

		// Re-submitting the same avatarFileId is a no-op and must not call ValidateAndLockAvatarFile again.
		fx.avatarFiles.validateCalls = 0
		if _, err := fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			AvatarFileID:     &avatarA,
		}); err != nil || fx.avatarFiles.validateCalls != 0 {
			t.Fatalf("idempotent avatar update = (err=%v, calls=%d), want (nil, 0)", err, fx.avatarFiles.validateCalls)
		}

		// ClearAvatar explicitly removes avatar reference, and blank displayName falls back to username.
		blankDisplay := "   "
		cleared, err := fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			DisplayName:      &blankDisplay,
			ClearAvatar:      true,
		})
		if err != nil {
			t.Fatalf("UpdateProfile(ClearAvatar): %v", err)
		}
		if cleared.AvatarFileID != nil || cleared.AvatarURL != nil || cleared.DisplayName != "manager" {
			t.Fatalf("expected cleared avatar and fallback displayName, got id=%v url=%v display=%q", cleared.AvatarFileID, cleared.AvatarURL, cleared.DisplayName)
		}
		clearAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if clearAudit.Action != auditActionProfileUpdate {
			t.Fatalf("expected %s audit, got %+v", auditActionProfileUpdate, clearAudit)
		}

		// ClearAvatar when already empty does not mark avatarFileId as changed; invalid email/displayName return 422; s.files==nil returns 422.
		if _, err := fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			ClearAvatar:      true,
		}); err != nil {
			t.Fatalf("idempotent ClearAvatar: %v", err)
		}
		noopAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if cf, _ := noopAudit.Metadata["changedFields"].([]string); len(cf) != 0 {
			t.Fatalf("expected empty changedFields on noop ClearAvatar, got %v", cf)
		}
		badProfileEmail := "not-an-email"
		badProfileDisplay := "bad\x00name"
		_, err = fx.svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			Email:            &badProfileEmail,
			DisplayName:      &badProfileDisplay,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) != 2 {
			t.Fatalf("expected 422 with 2 field errors on UpdateProfile, got %v", err)
		}
		noFilesSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{Tx: simTx{}, Sessions: fx.sessions})
		if _, err := noFilesSvc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			AvatarFileID:     &avatarA,
		}); !assertAppErrorCode(err, http.StatusUnprocessableEntity, apperr.CodeValidationFailed) {
			t.Fatalf("expected 422 when s.files is nil, got %v", err)
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
		if _, err := fx.svc.GetProfile(t.Context(), fx.mgrID, fx.mgrSessID); !assertAppErrorCode(err, http.StatusUnauthorized, apperr.CodeUnauthorized) {
			t.Fatalf("expected 401 on soft-deleted GetProfile, got %v", err)
		}
		if _, err := fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			CurrentPassword:  "password123",
			NewPassword:      "new-password-456",
		}); !assertAppErrorCode(err, http.StatusUnauthorized, apperr.CodeUnauthorized) {
			t.Fatalf("expected 401 on soft-deleted ChangePassword, got %v", err)
		}
		if err := fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: fx.mgrSessID,
			SessionID:        uuid.Must(uuid.NewV7()),
		}); !assertAppErrorCode(err, http.StatusUnauthorized, apperr.CodeUnauthorized) {
			t.Fatalf("expected 401 on soft-deleted RevokeProfileSession, got %v", err)
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

		// Empty currentPassword or invalid newPassword length fails 422 validation before verifying hash.
		_, err := fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "",
			NewPassword:      "short",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity || len(appErr.Fields) != 2 ||
			appErr.Fields[0].Field != "currentPassword" || appErr.Fields[1].Field != "newPassword" {
			t.Fatalf("expected 422 currentPassword & newPassword errors, got %v", err)
		}

		// Corrupted stored hash returns 500 INTERNAL_ERROR without writing invalid_current_password audit.
		auditCountBefore := len(fx.audit.entries)
		fx.hasher.verifyErr = errors.New("corrupt argon2id encoding")
		_, err = fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "password123",
			NewPassword:      "new-password-456",
		})
		fx.hasher.verifyErr = nil
		if !assertAppErrorCode(err, http.StatusInternalServerError, apperr.CodeInternal) || len(fx.audit.entries) != auditCountBefore {
			t.Fatalf("expected 500 INTERNAL_ERROR without password failure audit, got err=%v auditDelta=%d", err, len(fx.audit.entries)-auditCountBefore)
		}

		// Wrong current password returns 400 PROFILE_INVALID_CURRENT_PASSWORD + failure audit.
		_, err = fx.svc.ChangePassword(t.Context(), ChangePasswordInput{
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
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusUnprocessableEntity ||
			len(appErr.Fields) != 1 || appErr.Fields[0].Field != "newPassword" || appErr.Fields[0].Message != ErrCodeProfilePasswordUnchanged {
			t.Fatalf("expected 422 newPassword %s, got %v", ErrCodeProfilePasswordUnchanged, err)
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
		okAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if okAudit.Action != auditActionProfileChangePassword || okAudit.Status != auditStatusSuccess ||
			okAudit.Metadata["revokeOtherSessions"] != true || okAudit.Metadata["revokedSessionCount"] != int64(2) {
			t.Fatalf("unexpected ChangePassword success audit: %+v", okAudit)
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

		// Revoking nil session ID or unknown/foreign session returns 404.
		for _, targetSess := range []uuid.UUID{uuid.Nil, uuid.Must(uuid.NewV7())} {
			err = fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
				UserID:           fx.mgrID,
				CurrentSessionID: curSess,
				SessionID:        targetSess,
			})
			if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusNotFound || appErr.Code != ErrCodeProfileSessionNotFound {
				t.Fatalf("expected 404 %s for session %s, got %v", ErrCodeProfileSessionNotFound, targetSess, err)
			}
		}

		// Revoking another own session succeeds and records audit.
		if err := fx.svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
			SessionID:        other1,
		}); err != nil {
			t.Fatalf("RevokeProfileSession(other1): %v", err)
		}
		revAudit := fx.audit.entries[len(fx.audit.entries)-1]
		if revAudit.Action != auditActionProfileSessionRevoke || revAudit.ResourceID != other1.String() {
			t.Fatalf("expected %s audit for %s, got %+v", auditActionProfileSessionRevoke, other1, revAudit)
		}

		// RevokeOtherProfileSessions revokes remaining non-current session (other2), and a second call with 0 remaining writes no audit.
		revoked, err := fx.svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
		})
		if err != nil || revoked != 1 {
			t.Fatalf("RevokeOtherProfileSessions = (%d, %v), want (1, nil)", revoked, err)
		}
		auditCountBeforeZero := len(fx.audit.entries)
		if revokedZero, err := fx.svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           fx.mgrID,
			CurrentSessionID: curSess,
		}); err != nil || revokedZero != 0 || len(fx.audit.entries) != auditCountBeforeZero {
			t.Fatalf("RevokeOtherProfileSessions(0) = (revoked=%d, err=%v, auditDelta=%d), want (0, nil, 0)", revokedZero, err, len(fx.audit.entries)-auditCountBeforeZero)
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

func TestEnsureActiveCaller(t *testing.T) {
	t.Run("accepts active user with active session", func(t *testing.T) {
		fx := newTestFixture(t)
		if err := fx.svc.EnsureActiveCaller(t.Context(), fx.mgrID, fx.mgrSessID); err != nil {
			t.Fatalf("EnsureActiveCaller() error = %v", err)
		}
	})

	t.Run("rejects missing user, nil session, inactive session, and disabled user", func(t *testing.T) {
		fx := newTestFixture(t)

		if err := fx.svc.EnsureActiveCaller(t.Context(), uuid.Nil, fx.mgrSessID); !assertAppErrorStatus(err, http.StatusUnauthorized) {
			t.Fatalf("nil user err = %v, want 401", err)
		}
		if err := fx.svc.EnsureActiveCaller(t.Context(), uuid.New(), fx.mgrSessID); !assertAppErrorStatus(err, http.StatusUnauthorized) {
			t.Fatalf("missing user err = %v, want 401", err)
		}
		noSessSvc := newServiceWithStore(fx.store, fx.hasher, ServiceDeps{Tx: simTx{}})
		if err := noSessSvc.EnsureActiveCaller(t.Context(), fx.mgrID, fx.mgrSessID); !assertAppErrorCode(err, http.StatusInternalServerError, apperr.CodeInternal) {
			t.Fatalf("nil sessions manager err = %v, want 500", err)
		}
		if err := fx.svc.EnsureActiveCaller(t.Context(), fx.mgrID, uuid.Nil); !assertAppErrorStatus(err, http.StatusUnauthorized) {
			t.Fatalf("nil session err = %v, want 401", err)
		}
		if err := fx.svc.EnsureActiveCaller(t.Context(), fx.mgrID, uuid.New()); !assertAppErrorStatus(err, http.StatusUnauthorized) {
			t.Fatalf("revoked/unknown session err = %v, want 401", err)
		}

		fx.store.users[fx.mgrID].user.Status = StatusDisabled
		err := fx.svc.EnsureActiveCaller(t.Context(), fx.mgrID, fx.mgrSessID)
		if appErr, ok := apperr.As(err); !ok || appErr.Status != http.StatusForbidden || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled user err = %v, want 403 %s", err, errCodeAccountDisabled)
		}
	})
}

func assertAppErrorStatus(err error, wantStatus int) bool {
	appErr, ok := apperr.As(err)
	return ok && appErr.Status == wantStatus
}

func assertAppErrorCode(err error, wantStatus int, wantCode string) bool {
	appErr, ok := apperr.As(err)
	return ok && appErr.Status == wantStatus && appErr.Code == wantCode
}

type simTx struct{}

func (simTx) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return database.WithSimulatedTx(ctx, fn)
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
