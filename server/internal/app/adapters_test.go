package app

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/shared/apperr"
)

func TestIdentityFilesAdapterMapsNotFoundToDomainErr(t *testing.T) {
	if got := mapValidateAvatarFileErr(apperr.NotFound(files.ErrCodeNotFound)); got != identity.ErrInvalidAvatarFile {
		t.Fatalf("mapValidateAvatarFileErr(FILES_NOT_FOUND) = %v, want identity.ErrInvalidAvatarFile", got)
	}
	other := apperr.Internal(context.Canceled)
	if got := mapValidateAvatarFileErr(other); got != other {
		t.Fatalf("expected non-not-found error to pass through unchanged, got %v", got)
	}
}

func TestIdentityAccessAdapterMapsMissingRoleToValidationError(t *testing.T) {
	// Verify the adapter maps ACCESS_ROLE_NOT_FOUND into 422 roleIds: USERS_INVALID_ROLE.
	mapped := mapReplaceUserRolesErr(apperr.NotFound(access.ErrCodeRoleNotFound))
	appErr, ok := apperr.As(mapped)
	if !ok || appErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("mapped error = %+v, want 422 VALIDATION_FAILED", mapped)
	}
	if len(appErr.Fields) != 1 || appErr.Fields[0].Field != "roleIds" || appErr.Fields[0].Message != identity.ErrCodeInvalidRole {
		t.Fatalf("unexpected field errors: %+v", appErr.Fields)
	}

	other := apperr.Internal(context.Canceled)
	if got := mapReplaceUserRolesErr(other); got != other {
		t.Fatalf("expected non-role-not-found error to pass through unchanged, got %v", got)
	}
}

func TestToIdentityRoleRefsPreservesFields(t *testing.T) {
	id := uuid.New()
	got := toIdentityRoleRefs([]access.RoleRef{{
		ID:       id,
		Code:     "admin",
		Name:     "Admin",
		NameKey:  "roles.admin",
		IsSystem: true,
	}})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].ID != id || got[0].Code != "admin" || got[0].Name != "Admin" || got[0].NameKey != "roles.admin" || !got[0].IsSystem {
		t.Fatalf("unexpected converted RoleRef: %+v", got[0])
	}
}

func TestLateBoundAdaptersRejectNilServices(t *testing.T) {
	ctx := t.Context()
	uid := uuid.New()
	sid := uuid.New()

	assertInternal := func(t *testing.T, name string, err error) {
		t.Helper()
		appErr, ok := apperr.As(err)
		if !ok || appErr.Status != http.StatusInternalServerError || appErr.Code != apperr.CodeInternal {
			t.Fatalf("%s err = %v, want 500 INTERNAL_ERROR", name, err)
		}
	}

	accessAdapter := &identityAccessAdapter{}
	_, err := accessAdapter.ResolveRoles(ctx, []uuid.UUID{uid})
	assertInternal(t, "ResolveRoles", err)
	_, err = accessAdapter.ListRolesByUserIDs(ctx, []uuid.UUID{uid})
	assertInternal(t, "ListRolesByUserIDs", err)
	_, err = accessAdapter.ListUserIDsByRoleIDs(ctx, []uuid.UUID{uid})
	assertInternal(t, "ListUserIDsByRoleIDs", err)
	_, err = accessAdapter.ListUserIDsByRoleCode(ctx, "admin")
	assertInternal(t, "ListUserIDsByRoleCode", err)
	assertInternal(t, "ReplaceUserRoles", accessAdapter.ReplaceUserRoles(ctx, uid, []uuid.UUID{uid}))

	sessionAdapter := &identitySessionAdapter{}
	assertInternal(t, "RevokeAllUserSessions", sessionAdapter.RevokeAllUserSessions(ctx, uid))
	_, err = sessionAdapter.ListActiveUserSessions(ctx, uid)
	assertInternal(t, "ListActiveUserSessions", err)
	_, err = sessionAdapter.IsSessionActive(ctx, uid, sid)
	assertInternal(t, "IsSessionActive", err)
	_, err = sessionAdapter.LockActiveSessionForShare(ctx, uid, sid)
	assertInternal(t, "LockActiveSessionForShare", err)
	_, err = sessionAdapter.RevokeUserSession(ctx, uid, sid)
	assertInternal(t, "RevokeUserSession", err)
	_, err = sessionAdapter.RevokeOtherUserSessions(ctx, uid, sid)
	assertInternal(t, "RevokeOtherUserSessions", err)

	filesAdapter := &identityFilesAdapter{}
	assertInternal(t, "ValidateAndLockAvatarFile", filesAdapter.ValidateAndLockAvatarFile(ctx, uid, uid))
	_, err = filesAdapter.SignAvatarURL(ctx, uid)
	assertInternal(t, "identityFilesAdapter.SignAvatarURL", err)

	signerAdapter := &authAvatarSignerAdapter{}
	_, err = signerAdapter.SignAvatarURL(ctx, uid)
	assertInternal(t, "authAvatarSignerAdapter.SignAvatarURL", err)

	callerAdapter := &filesAvatarCallerAdapter{}
	assertInternal(t, "EnsureActiveAvatarCaller", callerAdapter.EnsureActiveAvatarCaller(ctx, uid, sid))

	auditAdapter := &identityAuditAdapter{}
	assertInternal(t, "identityAuditAdapter.Record", auditAdapter.Record(ctx, identity.AuditEntry{}))
}

func TestFilesAvatarCallerAdapterDelegatesValidation(t *testing.T) {
	// With nil identity userID, EnsureActiveCaller rejects with 401 before touching DB.
	svc := identity.NewService(nil, nil)
	adapter := &filesAvatarCallerAdapter{svc: svc}

	err := adapter.EnsureActiveAvatarCaller(t.Context(), uuid.Nil, uuid.New())
	appErr, ok := apperr.As(err)
	if !ok || appErr.Status != http.StatusUnauthorized {
		t.Fatalf("EnsureActiveAvatarCaller(nil user) err = %v, want 401", err)
	}

	// With non-nil userID and missing SessionManager, EnsureActiveCaller fails closed with 500.
	err = adapter.EnsureActiveAvatarCaller(t.Context(), uuid.New(), uuid.New())
	appErr, ok = apperr.As(err)
	if !ok || appErr.Status != http.StatusInternalServerError {
		t.Fatalf("EnsureActiveAvatarCaller(unwired sessions) err = %v, want 500", err)
	}
}
