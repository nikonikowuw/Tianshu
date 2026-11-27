package identity

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

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
	slices.SortFunc(out, func(a, b *fakeUserRow) int {
		return cmp.Compare(b.user.CreatedAt.UnixNano(), a.user.CreatedAt.UnixNano())
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

func (f *fakeStore) UpdateOwnPasswordHash(_ context.Context, id uuid.UUID, hash string) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		r.passwordHash = hash
		uid := id
		r.updatedBy = &uid
		r.user.UpdatedAt = time.Now().UTC()
	}
	return nil
}

func (f *fakeStore) UpdateUserPasswordByAdmin(_ context.Context, id uuid.UUID, hash string, updatedBy *uuid.UUID) error {
	if r, ok := f.users[id]; ok && r.deletedAt == nil {
		r.passwordHash = hash
		r.updatedBy = updatedBy
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

type fakeHasher struct{}

func (fakeHasher) Hash(pw string) (string, error) {
	if len(pw) < 8 || len(pw) > 128 {
		return "", security.ErrInvalidPasswordLength
	}
	return "hash:" + pw, nil
}

func (fakeHasher) Verify(pw, encodedHash string) (bool, bool, error) {
	return encodedHash == "hash:"+pw, false, nil
}

type fakeRoles struct {
	roles     map[uuid.UUID]RoleRef
	userRoles map[uuid.UUID][]uuid.UUID
}

func newFakeRoles() *fakeRoles {
	superID := uuid.Must(uuid.NewV7())
	viewerID := uuid.Must(uuid.NewV7())
	return &fakeRoles{
		roles: map[uuid.UUID]RoleRef{
			superID:  {ID: superID, Code: "super_admin", NameKey: "ACCESS_ROLE_SUPER_ADMIN_NAME", IsSystem: true},
			viewerID: {ID: viewerID, Code: "viewer", Name: "Viewer", IsSystem: false},
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
	out := make(map[uuid.UUID][]RoleRef, len(userIDs))
	for _, uid := range userIDs {
		for _, rid := range f.userRoles[uid] {
			if r, ok := f.roles[rid]; ok {
				out[uid] = append(out[uid], r)
			}
		}
	}
	return out, nil
}

func (f *fakeRoles) ListUserIDsByRoleIDs(_ context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
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

func (f *fakeRoles) ListUserIDsByRoleCode(_ context.Context, code string) ([]uuid.UUID, error) {
	rid := f.roleID(code)
	if rid == uuid.Nil {
		return nil, nil
	}
	return f.ListUserIDsByRoleIDs(context.Background(), []uuid.UUID{rid})
}

func (f *fakeRoles) ReplaceUserRoles(_ context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
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
	valid map[uuid.UUID]uuid.UUID // fileID -> ownerID
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

func setupTestServiceWithFiles() (*Service, *fakeStore, *fakeRoles, *fakeSessions, *fakeAvatarFiles, *fakeAudit, uuid.UUID, uuid.UUID) {
	st := newFakeStore()
	roles := newFakeRoles()
	sess := newFakeSessions()
	avatarFiles := newFakeAvatarFiles()
	aud := &fakeAudit{}
	svc := newServiceWithStore(st, fakeHasher{}, ServiceDeps{
		Roles:    roles,
		Sessions: sess,
		Files:    avatarFiles,
		Audit:    aud,
	})
	superUser, err := svc.CreateInitialAdmin(context.Background(), CreateInitialAdminInput{
		Username: "root",
		Email:    "root@example.com",
		Password: "password123",
	})
	if err != nil {
		panic(err)
	}
	roles.userRoles[superUser.ID] = []uuid.UUID{roles.roleID("super_admin")}

	mgr, err := svc.CreateUser(context.Background(), CreateUserInput{
		Username: "manager",
		Email:    "manager@example.com",
		Password: "password123",
		RoleIDs:  []uuid.UUID{roles.roleID("viewer")},
		ActorID:  &superUser.ID,
	})
	if err != nil {
		panic(err)
	}
	return svc, st, roles, sess, avatarFiles, aud, superUser.ID, mgr.ID
}

func setupTestService() (*Service, *fakeStore, *fakeRoles, *fakeSessions, *fakeAudit, uuid.UUID, uuid.UUID) {
	svc, st, roles, sess, _, aud, superID, mgrID := setupTestServiceWithFiles()
	return svc, st, roles, sess, aud, superID, mgrID
}

func TestCreateUserValidationAndProtection(t *testing.T) {
	svc, _, roles, _, aud, superID, mgrID := setupTestService()

	t.Run("rejects invalid fields with 422", func(t *testing.T) {
		_, err := svc.CreateUser(t.Context(), CreateUserInput{
			Username: "a",
			Email:    "bad-email",
			Password: "short",
			Status:   "bogus",
			RoleIDs:  []uuid.UUID{uuid.Must(uuid.NewV7())},
			ActorID:  &superID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != 422 {
			t.Fatalf("expected 422, got %v", err)
		}
		if len(appErr.Fields) != 5 {
			t.Fatalf("expected 5 field errors, got %+v", appErr.Fields)
		}
	})

	t.Run("forbids non-super-admin from granting super_admin", func(t *testing.T) {
		_, err := svc.CreateUser(t.Context(), CreateUserInput{
			Username: "escalated",
			Email:    "esc@example.com",
			Password: "password123",
			RoleIDs:  []uuid.UUID{roles.roleID("super_admin")},
			ActorID:  &mgrID,
		})
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != 403 || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected 403 %s, got %v", ErrCodeSuperAdminProtect, err)
		}
	})

	t.Run("conflict username and email return 409", func(t *testing.T) {
		_, err := svc.CreateUser(t.Context(), CreateUserInput{
			Username: "MANAGER",
			Email:    "other@example.com",
			Password: "password123",
			ActorID:  &superID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeUsernameConflict {
			t.Fatalf("expected %s, got %v", ErrCodeUsernameConflict, err)
		}

		_, err = svc.CreateUser(t.Context(), CreateUserInput{
			Username: "unique_user",
			Email:    "MANAGER@example.com",
			Password: "password123",
			ActorID:  &superID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeEmailConflict {
			t.Fatalf("expected %s, got %v", ErrCodeEmailConflict, err)
		}
	})

	t.Run("audit metadata never contains password", func(t *testing.T) {
		last := aud.entries[len(aud.entries)-1]
		if _, hasPw := last.Metadata["password"]; hasPw {
			t.Fatalf("audit metadata must not contain password")
		}
	})
}

func TestUpdateAndDeleteUserGuards(t *testing.T) {
	svc, _, roles, sess, _, superID, mgrID := setupTestService()

	t.Run("self disable, self demote, self delete forbidden", func(t *testing.T) {
		disabled := StatusDisabled
		_, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:  superID,
			Status:  &disabled,
			ActorID: &superID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self disable, got %v", ErrCodeSelfOperation, err)
		}

		emptyRoles := []uuid.UUID{}
		_, err = svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:  superID,
			RoleIDs: &emptyRoles,
			ActorID: &superID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self super_admin removal, got %v", ErrCodeSelfOperation, err)
		}

		err = svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:  superID,
			ActorID: &superID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSelfOperation {
			t.Fatalf("expected %s on self delete, got %v", ErrCodeSelfOperation, err)
		}
	})

	t.Run("non-super-admin cannot edit or delete super_admin", func(t *testing.T) {
		newDisplay := "Hacked"
		_, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:      superID,
			DisplayName: &newDisplay,
			ActorID:     &mgrID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected %s, got %v", ErrCodeSuperAdminProtect, err)
		}

		err = svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:  superID,
			ActorID: &mgrID,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeSuperAdminProtect {
			t.Fatalf("expected %s, got %v", ErrCodeSuperAdminProtect, err)
		}
	})

	t.Run("last super admin guard and second super admin allow", func(t *testing.T) {
		secondSuper, err := svc.CreateUser(t.Context(), CreateUserInput{
			Username: "super2",
			Email:    "super2@example.com",
			Password: "password123",
			RoleIDs:  []uuid.UUID{roles.roleID("super_admin")},
			ActorID:  &superID,
		})
		if err != nil {
			t.Fatalf("create second super admin: %v", err)
		}

		// Disabling secondSuper is allowed because root is still active.
		disabled := StatusDisabled
		if _, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:  secondSuper.ID,
			Status:  &disabled,
			ActorID: &superID,
		}); err != nil {
			t.Fatalf("expected disabling second super to succeed, got %v", err)
		}
		if !slices.Contains(sess.revoked, secondSuper.ID) {
			t.Fatalf("expected sessions revoked on disable")
		}

		// Now root is the only active super_admin; another super_admin cannot disable/delete root.
		// (Use a third disabled super or test via actor = third active super -> let's enable secondSuper, then test removing root's super role when secondSuper is disabled)
		// Specifically: if secondSuper is disabled, and we have a non-self super_admin? No, only active super_admin can act.
		// So if root is the ONLY active super_admin, even if another user has super_admin but is disabled, root cannot disable itself anyway (self guard).
		// To test USERS_LAST_SUPER_ADMIN directly: create a non-self super_admin that is disabled? No, let's test DeleteUser on secondSuper when secondSuper is active vs when root is disabled.
		// Even simpler: re-enable secondSuper, disable root via secondSuper? Wait: root is active, secondSuper is active -> secondSuper disables root (ok), then root is disabled, secondSuper is the ONLY active super_admin.
		active := StatusActive
		if _, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:  secondSuper.ID,
			Status:  &active,
			ActorID: &superID,
		}); err != nil {
			t.Fatalf("re-enable secondSuper: %v", err)
		}
		if _, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:  superID,
			Status:  &disabled,
			ActorID: &secondSuper.ID,
		}); err != nil {
			t.Fatalf("secondSuper disables root: %v", err)
		}
		// Now secondSuper is the sole active super_admin; if root (still holding role, though disabled) tries to delete secondSuper -> self guard doesn't trigger, last super admin guard DOES!
		if err := svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:  secondSuper.ID,
			ActorID: &superID,
		}); err == nil {
			t.Fatalf("expected last super admin error")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeLastSuperAdmin {
			t.Fatalf("expected %s, got %v", ErrCodeLastSuperAdmin, err)
		}
	})

	t.Run("reset password revokes sessions and delete clears roles", func(t *testing.T) {
		sess.revoked = nil
		newPw := "newpassword123"
		if _, err := svc.UpdateUser(t.Context(), UpdateUserInput{
			UserID:   mgrID,
			Password: &newPw,
			ActorID:  &superID,
		}); err != nil {
			t.Fatalf("reset password: %v", err)
		}
		if !slices.Contains(sess.revoked, mgrID) {
			t.Fatalf("expected sessions revoked on password reset")
		}

		sess.revoked = nil
		if err := svc.DeleteUser(t.Context(), DeleteUserInput{
			UserID:  mgrID,
			ActorID: &superID,
		}); err != nil {
			t.Fatalf("delete manager: %v", err)
		}
		if !slices.Contains(sess.revoked, mgrID) {
			t.Fatalf("expected sessions revoked on delete")
		}
		if len(roles.userRoles[mgrID]) != 0 {
			t.Fatalf("expected user roles cleared on soft delete")
		}
		if _, err := svc.GetUser(t.Context(), mgrID); !errors.Is(err, err) {
			appErr, ok := apperr.As(err)
			if !ok || appErr.Status != 404 {
				t.Fatalf("expected 404 after delete, got %v", err)
			}
		}
	})

	t.Run("list users pagination", func(t *testing.T) {
		res, err := svc.ListUsers(t.Context(), UserListFilter{
			Pagination: pagination.Normalize(1, 10),
		})
		if err != nil {
			t.Fatalf("ListUsers: %v", err)
		}
		if res.Total == 0 || len(res.Items) == 0 {
			t.Fatalf("expected non-empty users list")
		}
	})

	t.Run("list usernames by ids and avatar reference check", func(t *testing.T) {
		names, err := svc.ListUsernamesByIDs(t.Context(), []uuid.UUID{superID, superID, mgrID})
		if err != nil {
			t.Fatalf("ListUsernamesByIDs: %v", err)
		}
		if names[superID] == "" || names[mgrID] == "" {
			t.Fatalf("expected active and soft-deleted usernames preserved, got %+v", names)
		}

		inUse, err := svc.IsAvatarFileInUse(t.Context(), uuid.Must(uuid.NewV7()))
		if err != nil || inUse {
			t.Fatalf("IsAvatarFileInUse = (%v, %v), want (false, nil)", inUse, err)
		}
	})
}

func TestProfileUpdateChangePasswordAndSessions(t *testing.T) {
	svc, st, _, sess, avatarFiles, aud, superID, mgrID := setupTestServiceWithFiles()

	avatarA := uuid.Must(uuid.NewV7())
	avatarForeign := uuid.Must(uuid.NewV7())
	avatarFiles.valid[avatarA] = mgrID
	avatarFiles.valid[avatarForeign] = superID

	t.Run("update profile fields, bind avatar, clear avatar, and enforce uniqueness/ownership", func(t *testing.T) {
		curSess := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		sess.activeByUser[mgrID] = []UserSession{
			{ID: curSess, IPAddress: "10.0.0.1", UserAgent: "MgrUA", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		}

		// Revoked/unknown session is rejected with 401 on GetProfile and UpdateProfile.
		if _, err := svc.GetProfile(t.Context(), mgrID, uuid.Must(uuid.NewV7())); err == nil {
			t.Fatal("expected 401 on GetProfile with inactive session")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
			t.Fatalf("expected 401, got %v", err)
		}

		newDisplay := "Manager Display"
		newEmail := "manager.new@example.com"
		updated, err := svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
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
		inUse, err := svc.IsAvatarFileInUse(t.Context(), avatarA)
		if err != nil || !inUse {
			t.Fatalf("IsAvatarFileInUse = (%v, %v), want (true, nil)", inUse, err)
		}

		// Binding another user's avatar must fail with 422 on avatarFileId.
		_, err = svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			AvatarFileID:     &avatarForeign,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 || len(appErr.Fields) != 1 || appErr.Fields[0].Field != "avatarFileId" {
			t.Fatalf("expected 422 avatarFileId for foreign avatar, got %v", err)
		}

		// Duplicate email must fail with 409 USERS_EMAIL_CONFLICT.
		dupEmail := "ROOT@example.com"
		_, err = svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			Email:            &dupEmail,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Code != ErrCodeEmailConflict {
			t.Fatalf("expected %s, got %v", ErrCodeEmailConflict, err)
		}

		// ClearAvatar explicitly removes avatar reference.
		cleared, err := svc.UpdateProfile(t.Context(), UpdateProfileInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			ClearAvatar:      true,
		})
		if err != nil {
			t.Fatalf("UpdateProfile(ClearAvatar): %v", err)
		}
		if cleared.AvatarFileID != nil || cleared.AvatarURL != nil {
			t.Fatalf("expected cleared avatar, got id=%v url=%v", cleared.AvatarFileID, cleared.AvatarURL)
		}
	})

	t.Run("change password verifies old password, forbids unchanged, and revokes other sessions", func(t *testing.T) {
		curSess := uuid.Must(uuid.NewV7())
		otherSess1 := uuid.Must(uuid.NewV7())
		otherSess2 := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		sess.activeByUser[mgrID] = []UserSession{
			{ID: otherSess1, IPAddress: "10.0.0.2", UserAgent: "Other1", LastUsedAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: curSess, IPAddress: "10.0.0.1", UserAgent: "Current", LastUsedAt: now.Add(-2 * time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
			{ID: otherSess2, IPAddress: "10.0.0.3", UserAgent: "Other2", LastUsedAt: now.Add(-3 * time.Minute), CreatedAt: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)},
		}

		// Wrong current password returns 400 PROFILE_INVALID_CURRENT_PASSWORD + failure audit.
		_, err := svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "wrong-password",
			NewPassword:      "new-password-456",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 400 || appErr.Code != ErrCodeProfileInvalidCurrentPassword {
			t.Fatalf("expected 400 %s, got %v", ErrCodeProfileInvalidCurrentPassword, err)
		}
		lastAudit := aud.entries[len(aud.entries)-1]
		if lastAudit.Action != auditActionProfileChangePassword || lastAudit.Status != auditStatusFailure {
			t.Fatalf("expected failure audit for bad password, got %+v", lastAudit)
		}

		// Identical new password rejected with 422 PROFILE_PASSWORD_UNCHANGED.
		_, err = svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "password123",
			NewPassword:      "password123",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 422 || len(appErr.Fields) != 1 || appErr.Fields[0].Message != ErrCodeProfilePasswordUnchanged {
			t.Fatalf("expected 422 %s, got %v", ErrCodeProfilePasswordUnchanged, err)
		}

		// Valid change with default revokeOtherSessions=true revokes 2 other sessions.
		res, err := svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
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
		if st.users[mgrID].passwordHash != "hash:new-password-456" {
			t.Fatalf("passwordHash not updated: %q", st.users[mgrID].passwordHash)
		}
		if len(sess.activeByUser[mgrID]) != 1 || sess.activeByUser[mgrID][0].ID != curSess {
			t.Fatalf("expected only current session retained, got %+v", sess.activeByUser[mgrID])
		}

		// revokeOtherSessions=false keeps other sessions intact.
		keepFalse := false
		extraSess := uuid.Must(uuid.NewV7())
		sess.activeByUser[mgrID] = append(sess.activeByUser[mgrID], UserSession{
			ID: extraSess, IPAddress: "10.0.0.4", UserAgent: "Extra", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		})
		res2, err := svc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:              mgrID,
			CurrentSessionID:    curSess,
			CurrentPassword:     "new-password-456",
			NewPassword:         "another-password-789",
			RevokeOtherSessions: &keepFalse,
		})
		if err != nil || res2.RevokedSessions != 0 {
			t.Fatalf("ChangePassword(revoke=false) = (%+v, %v), want 0 revoked", res2, err)
		}
		if len(sess.activeByUser[mgrID]) != 2 {
			t.Fatalf("expected 2 sessions preserved, got %d", len(sess.activeByUser[mgrID]))
		}
	})

	t.Run("sessions list pins current first and guards self/foreign revocation", func(t *testing.T) {
		curSess := uuid.Must(uuid.NewV7())
		other1 := uuid.Must(uuid.NewV7())
		other2 := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		sess.activeByUser[mgrID] = []UserSession{
			{ID: other1, IPAddress: "10.0.0.10", UserAgent: "Other1", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			{ID: curSess, IPAddress: "10.0.0.11", UserAgent: "Cur", LastUsedAt: now.Add(-time.Minute), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
			{ID: other2, IPAddress: "10.0.0.12", UserAgent: "Other2", LastUsedAt: now.Add(-2 * time.Minute), CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		}

		list, err := svc.ListProfileSessions(t.Context(), mgrID, curSess)
		if err != nil {
			t.Fatalf("ListProfileSessions: %v", err)
		}
		if len(list) != 3 || !list[0].IsCurrent || list[0].ID != curSess {
			t.Fatalf("expected current session pinned first, got %+v", list)
		}

		// Revoking current session is forbidden (409).
		err = svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			SessionID:        curSess,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 409 || appErr.Code != ErrCodeProfileCurrentSessionRevoke {
			t.Fatalf("expected 409 %s, got %v", ErrCodeProfileCurrentSessionRevoke, err)
		}

		// Revoking unknown/foreign session returns 404.
		err = svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			SessionID:        uuid.Must(uuid.NewV7()),
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 404 || appErr.Code != ErrCodeProfileSessionNotFound {
			t.Fatalf("expected 404 %s, got %v", ErrCodeProfileSessionNotFound, err)
		}

		// Revoking another own session succeeds.
		if err := svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			SessionID:        other1,
		}); err != nil {
			t.Fatalf("RevokeProfileSession(other1): %v", err)
		}

		// RevokeOtherProfileSessions revokes remaining non-current session (other2).
		revoked, err := svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
		})
		if err != nil || revoked != 1 {
			t.Fatalf("RevokeOtherProfileSessions = (%d, %v), want (1, nil)", revoked, err)
		}

		// A revoked session must receive 401 and cannot revoke the remaining active session.
		if err := svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           mgrID,
			CurrentSessionID: other1,
			SessionID:        curSess,
		}); err == nil {
			t.Fatal("expected 401 when revoked session tries to revoke active session")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
			t.Fatalf("expected 401, got %v", err)
		}
		if _, err := svc.RevokeOtherProfileSessions(t.Context(), RevokeOtherSessionsInput{
			UserID:           mgrID,
			CurrentSessionID: other1,
		}); err == nil {
			t.Fatal("expected 401 when revoked session tries bulk revoke")
		} else if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
			t.Fatalf("expected 401, got %v", err)
		}
		if len(sess.activeByUser[mgrID]) != 1 || sess.activeByUser[mgrID][0].ID != curSess {
			t.Fatalf("legitimate session must remain untouched, got %+v", sess.activeByUser[mgrID])
		}

		// Disabled account receives 403 AUTH_ACCOUNT_DISABLED even when targeting its own session ID (check order).
		st.users[mgrID].user.Status = StatusDisabled
		err = svc.RevokeProfileSession(t.Context(), RevokeSessionInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			SessionID:        curSess,
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 403 || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("disabled account self-revoke = %v, want 403 %s", err, errCodeAccountDisabled)
		}
		st.users[mgrID].user.Status = StatusActive
	})

	t.Run("change password rejects concurrent password mutation, disable, and soft-delete inside tx lock", func(t *testing.T) {
		curSess := uuid.Must(uuid.NewV7())
		now := time.Now().UTC()
		sess.activeByUser[mgrID] = []UserSession{
			{ID: curSess, IPAddress: "10.0.0.20", UserAgent: "Cur", LastUsedAt: now, CreatedAt: now, ExpiresAt: now.Add(time.Hour)},
		}

		// Simulate an admin resetting password hash between Verify and WithinTx lock.
		origHash := st.users[mgrID].passwordHash
		racedSvc := newServiceWithStore(st, fakeHasher{}, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				st.users[mgrID].passwordHash = "hash:admin-reset-password"
			}},
			Sessions: sess,
			Audit:    aud,
		})
		_, err := racedSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  strings.TrimPrefix(origHash, "hash:"),
			NewPassword:      "attacker-new-password",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 400 || appErr.Code != ErrCodeProfileInvalidCurrentPassword {
			t.Fatalf("concurrent password reset race = %v, want 400 %s", err, ErrCodeProfileInvalidCurrentPassword)
		}
		if st.users[mgrID].passwordHash != "hash:admin-reset-password" {
			t.Fatalf("admin reset hash was overwritten: %q", st.users[mgrID].passwordHash)
		}
		lastAudit := aud.entries[len(aud.entries)-1]
		if lastAudit.Status != auditStatusFailure || lastAudit.Metadata["reason"] != "password_changed_concurrently" {
			t.Fatalf("expected concurrent failure audit, got %+v", lastAudit)
		}

		// Simulate an admin disabling the account during Argon2id.
		disabledSvc := newServiceWithStore(st, fakeHasher{}, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				st.users[mgrID].user.Status = StatusDisabled
			}},
			Sessions: sess,
			Audit:    aud,
		})
		_, err = disabledSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "admin-reset-password",
			NewPassword:      "disabled-user-pw-1",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 403 || appErr.Code != errCodeAccountDisabled {
			t.Fatalf("concurrent disable race = %v, want 403 %s", err, errCodeAccountDisabled)
		}
		st.users[mgrID].user.Status = StatusActive

		// Simulate an admin soft-deleting the account during Argon2id.
		deletedSvc := newServiceWithStore(st, fakeHasher{}, ServiceDeps{
			Tx: &hookTx{beforeTx: func() {
				ts := time.Now().UTC()
				st.users[mgrID].deletedAt = &ts
			}},
			Sessions: sess,
			Audit:    aud,
		})
		_, err = deletedSvc.ChangePassword(t.Context(), ChangePasswordInput{
			UserID:           mgrID,
			CurrentSessionID: curSess,
			CurrentPassword:  "admin-reset-password",
			NewPassword:      "deleted-user-pw-1",
		})
		if appErr, ok := apperr.As(err); !ok || appErr.Status != 401 {
			t.Fatalf("concurrent soft-delete race = %v, want 401", err)
		}
		st.users[mgrID].deletedAt = nil
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
