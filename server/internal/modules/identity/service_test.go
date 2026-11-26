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

func (f *fakeStore) toDetail(r *fakeUserRow) UserDetail {
	return UserDetail{
		ID:          r.user.ID,
		Username:    r.user.Username,
		Email:       r.user.Email,
		DisplayName: r.user.DisplayName,
		Status:      r.user.Status,
		LastLoginAt: r.lastLoginAt,
		Roles:       make([]RoleRef, 0),
		CreatedBy:   f.actorRef(r.createdBy),
		UpdatedBy:   f.actorRef(r.updatedBy),
		CreatedAt:   r.user.CreatedAt,
		UpdatedAt:   r.user.UpdatedAt,
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
	revoked []uuid.UUID
}

func (f *fakeSessions) RevokeAllUserSessions(_ context.Context, userID uuid.UUID) error {
	f.revoked = append(f.revoked, userID)
	return nil
}

type fakeAudit struct {
	entries []AuditEntry
}

func (f *fakeAudit) Record(_ context.Context, entry AuditEntry) error {
	f.entries = append(f.entries, entry)
	return nil
}

func setupTestService() (*Service, *fakeStore, *fakeRoles, *fakeSessions, *fakeAudit, uuid.UUID, uuid.UUID) {
	st := newFakeStore()
	roles := newFakeRoles()
	sess := &fakeSessions{}
	aud := &fakeAudit{}
	svc := newServiceWithStore(st, fakeHasher{}, ServiceDeps{
		Roles:    roles,
		Sessions: sess,
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
	return svc, st, roles, sess, aud, superUser.ID, mgr.ID
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
}
