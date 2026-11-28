package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/modules/setup"
	"tianshu/internal/shared/apperr"
)

// setupAuditAdapter bridges audit.Service into setup.AuditRecorder.
type setupAuditAdapter struct {
	svc *audit.Service
}

var _ setup.AuditRecorder = (*setupAuditAdapter)(nil)

func (a *setupAuditAdapter) Record(ctx context.Context, entry setup.AuditEntry) error {
	return a.svc.Record(ctx, audit.RecordInput{
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

// authAuditAdapter bridges audit.Service into auth.AuditRecorder.
type authAuditAdapter struct {
	svc *audit.Service
}

var _ auth.AuditRecorder = (*authAuditAdapter)(nil)

func (a *authAuditAdapter) Record(ctx context.Context, entry auth.AuditEntry) error {
	return a.svc.Record(ctx, audit.RecordInput{
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

// setupIdentityAdapter bridges identity.Service into setup.IdentityManager without coupling modules.
type setupIdentityAdapter struct {
	svc *identity.Service
}

var _ setup.IdentityManager = (*setupIdentityAdapter)(nil)

func (a *setupIdentityAdapter) CountUsers(ctx context.Context) (int64, error) {
	return a.svc.CountUsers(ctx)
}

func (a *setupIdentityAdapter) CreateInitialAdmin(ctx context.Context, p setup.CreateAdminParams) (setup.CreatedAdmin, error) {
	user, err := a.svc.CreateInitialAdmin(ctx, identity.CreateInitialAdminInput{
		Username:    p.Username,
		Email:       p.Email,
		DisplayName: p.DisplayName,
		Password:    p.Password,
	})
	if err != nil {
		return setup.CreatedAdmin{}, err
	}
	return setup.CreatedAdmin{
		ID:          user.ID,
		Username:    user.Username,
		Email:       user.Email,
		DisplayName: user.DisplayName,
	}, nil
}

// authIdentityAdapter bridges identity.Service into auth.IdentityProvider.
type authIdentityAdapter struct {
	svc *identity.Service
}

var _ auth.IdentityProvider = (*authIdentityAdapter)(nil)

func (a *authIdentityAdapter) GetAuthCredentials(ctx context.Context, identifier string) (auth.UserCredentials, error) {
	creds, err := a.svc.GetAuthCredentials(ctx, identifier)
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

func (a *authIdentityAdapter) GetByID(ctx context.Context, userID uuid.UUID) (auth.User, error) {
	u, err := a.svc.GetByID(ctx, userID)
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

func (a *authIdentityAdapter) CompleteLogin(ctx context.Context, input auth.CompleteLoginInput) error {
	return a.svc.CompleteLogin(ctx, identity.CompleteLoginInput{
		UserID:               input.UserID,
		ExpectedPasswordHash: input.ExpectedPasswordHash,
		UpgradedPasswordHash: input.UpgradedPasswordHash,
	})
}

// authAccessAdapter bridges access.Service into auth.AccessProvider.
type authAccessAdapter struct {
	svc *access.Service
}

var _ auth.AccessProvider = (*authAccessAdapter)(nil)

func (a *authAccessAdapter) GetUserAccess(ctx context.Context, userID uuid.UUID) (auth.UserAccess, error) {
	ua, err := a.svc.GetUserAccess(ctx, userID)
	if err != nil {
		return auth.UserAccess{}, err
	}
	return auth.UserAccess{
		Roles:       ua.Roles,
		Permissions: ua.Permissions,
	}, nil
}

// accessAuditAdapter bridges audit.Service into access.AuditRecorder.
type accessAuditAdapter struct {
	svc *audit.Service
}

var _ access.AuditRecorder = (*accessAuditAdapter)(nil)

func (a *accessAuditAdapter) Record(ctx context.Context, entry access.AuditEntry) error {
	return a.svc.Record(ctx, audit.RecordInput{
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

// accessActorAdapter bridges identity.Service into access.ActorResolver.
type accessActorAdapter struct {
	svc *identity.Service
}

var _ access.ActorResolver = (*accessActorAdapter)(nil)

func (a *accessActorAdapter) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	user, err := a.svc.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

// identityAccessAdapter bridges access.Service into identity.RoleManager.
type identityAccessAdapter struct {
	svc *access.Service
}

var _ identity.RoleManager = (*identityAccessAdapter)(nil)

func (a *identityAccessAdapter) ResolveRoles(ctx context.Context, ids []uuid.UUID) ([]identity.RoleRef, error) {
	if a.svc == nil {
		return nil, apperr.Internal(errors.New("identity access adapter: access service not initialized"))
	}
	refs, err := a.svc.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	return toIdentityRoleRefs(refs), nil
}

func (a *identityAccessAdapter) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]identity.RoleRef, error) {
	if a.svc == nil {
		return nil, apperr.Internal(errors.New("identity access adapter: access service not initialized"))
	}
	m, err := a.svc.ListRolesByUserIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]identity.RoleRef, len(m))
	for uid, refs := range m {
		out[uid] = toIdentityRoleRefs(refs)
	}
	return out, nil
}

func (a *identityAccessAdapter) ListUserIDsByRoleIDs(ctx context.Context, roleIDs []uuid.UUID) ([]uuid.UUID, error) {
	if a.svc == nil {
		return nil, apperr.Internal(errors.New("identity access adapter: access service not initialized"))
	}
	return a.svc.ListUserIDsByRoleIDs(ctx, roleIDs)
}

func (a *identityAccessAdapter) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	if a.svc == nil {
		return nil, apperr.Internal(errors.New("identity access adapter: access service not initialized"))
	}
	return a.svc.ListUserIDsByRoleCode(ctx, code)
}

func (a *identityAccessAdapter) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	if a.svc == nil {
		return apperr.Internal(errors.New("identity access adapter: access service not initialized"))
	}
	return mapReplaceUserRolesErr(a.svc.ReplaceUserRoles(ctx, userID, roleIDs))
}

func mapReplaceUserRolesErr(err error) error {
	if appErr, ok := apperr.As(err); ok && appErr.Code == access.ErrCodeRoleNotFound {
		return apperr.Validation([]apperr.FieldError{{
			Field:   "roleIds",
			Message: identity.ErrCodeInvalidRole,
		}})
	}
	return err
}

func toIdentityRoleRefs(in []access.RoleRef) []identity.RoleRef {
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

// identitySessionAdapter bridges auth.Service into identity.SessionManager using late binding to break construction order cycles.
type identitySessionAdapter struct {
	svc *auth.Service
}

var _ identity.SessionManager = (*identitySessionAdapter)(nil)

func (a *identitySessionAdapter) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	if a.svc == nil {
		return apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	return a.svc.RevokeAllUserSessions(ctx, userID)
}

func (a *identitySessionAdapter) ListActiveUserSessions(ctx context.Context, userID uuid.UUID) ([]identity.UserSession, error) {
	if a.svc == nil {
		return nil, apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	sessions, err := a.svc.ListActiveUserSessions(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]identity.UserSession, 0, len(sessions))
	for _, s := range sessions {
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

func (a *identitySessionAdapter) IsSessionActive(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	if a.svc == nil {
		return false, apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	return a.svc.IsSessionActive(ctx, userID, sessionID)
}

func (a *identitySessionAdapter) LockActiveSessionForShare(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	if a.svc == nil {
		return false, apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	return a.svc.LockActiveSessionForShare(ctx, userID, sessionID)
}

func (a *identitySessionAdapter) RevokeUserSession(ctx context.Context, userID, sessionID uuid.UUID) (bool, error) {
	if a.svc == nil {
		return false, apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	return a.svc.RevokeUserSession(ctx, userID, sessionID)
}

func (a *identitySessionAdapter) RevokeOtherUserSessions(ctx context.Context, userID, keepSessionID uuid.UUID) (int64, error) {
	if a.svc == nil {
		return 0, apperr.Internal(errors.New("identity session adapter: auth service not initialized"))
	}
	return a.svc.RevokeOtherUserSessions(ctx, userID, keepSessionID)
}

// identityFilesAdapter bridges files.Service into identity.AvatarFileManager using late binding.
type identityFilesAdapter struct {
	svc *files.Service
}

var _ identity.AvatarFileManager = (*identityFilesAdapter)(nil)

func (a *identityFilesAdapter) ValidateAndLockAvatarFile(ctx context.Context, fileID, ownerID uuid.UUID) error {
	if a.svc == nil {
		return apperr.Internal(errors.New("identity files adapter: files service not initialized"))
	}
	return mapValidateAvatarFileErr(a.svc.ValidateAndLockAvatarFile(ctx, fileID, ownerID))
}

func mapValidateAvatarFileErr(err error) error {
	if appErr, ok := apperr.As(err); ok && appErr.Code == files.ErrCodeNotFound {
		return identity.ErrInvalidAvatarFile
	}
	return err
}

func (a *identityFilesAdapter) SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error) {
	if a.svc == nil {
		return "", apperr.Internal(errors.New("identity files adapter: files service not initialized"))
	}
	return a.svc.SignAvatarURL(ctx, fileID)
}

// authAvatarSignerAdapter bridges files.Service into auth.AvatarURLSigner.
type authAvatarSignerAdapter struct {
	svc *files.Service
}

var _ auth.AvatarURLSigner = (*authAvatarSignerAdapter)(nil)

func (a *authAvatarSignerAdapter) SignAvatarURL(ctx context.Context, fileID uuid.UUID) (string, error) {
	if a.svc == nil {
		return "", apperr.Internal(errors.New("auth avatar signer adapter: files service not initialized"))
	}
	return a.svc.SignAvatarURL(ctx, fileID)
}

// identityAuditAdapter bridges audit.Service into identity.AuditRecorder.
type identityAuditAdapter struct {
	svc *audit.Service
}

var _ identity.AuditRecorder = (*identityAuditAdapter)(nil)

func (a *identityAuditAdapter) Record(ctx context.Context, entry identity.AuditEntry) error {
	if a.svc == nil {
		return apperr.Internal(errors.New("identity audit adapter: audit service not initialized"))
	}
	return a.svc.Record(ctx, audit.RecordInput{
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

// filesActorAdapter bridges identity.Service into files.ActorResolver.
type filesActorAdapter struct {
	svc *identity.Service
}

var _ files.ActorResolver = (*filesActorAdapter)(nil)

func (a *filesActorAdapter) GetUsername(ctx context.Context, userID uuid.UUID) (string, error) {
	user, err := a.svc.GetByID(ctx, userID)
	if err != nil {
		return "", err
	}
	return user.Username, nil
}

func (a *filesActorAdapter) ListUsernamesByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]string, error) {
	return a.svc.ListUsernamesByIDs(ctx, userIDs)
}

// filesAvatarRefAdapter bridges identity.Service into files.AvatarRefChecker.
type filesAvatarRefAdapter struct {
	svc *identity.Service
}

var _ files.AvatarRefChecker = (*filesAvatarRefAdapter)(nil)

func (a *filesAvatarRefAdapter) IsAvatarFileInUse(ctx context.Context, fileID uuid.UUID) (bool, error) {
	return a.svc.IsAvatarFileInUse(ctx, fileID)
}

// filesAvatarCallerAdapter bridges identity.Service into files.AvatarCallerValidator.
type filesAvatarCallerAdapter struct {
	svc *identity.Service
}

var _ files.AvatarCallerValidator = (*filesAvatarCallerAdapter)(nil)

func (a *filesAvatarCallerAdapter) EnsureActiveAvatarCaller(ctx context.Context, userID, sessionID uuid.UUID) error {
	if a.svc == nil {
		return apperr.Internal(errors.New("files avatar caller adapter: identity service not initialized"))
	}
	return a.svc.EnsureActiveCaller(ctx, userID, sessionID)
}

// filesAuditAdapter bridges audit.Service into files.AuditRecorder.
type filesAuditAdapter struct {
	svc *audit.Service
}

var _ files.AuditRecorder = (*filesAuditAdapter)(nil)

func (a *filesAuditAdapter) Record(ctx context.Context, entry files.AuditEntry) error {
	return a.svc.Record(ctx, audit.RecordInput{
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
