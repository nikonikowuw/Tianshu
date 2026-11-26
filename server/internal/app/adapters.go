package app

import (
	"context"

	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/audit"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/identity"
	"tianshu/internal/modules/setup"
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
			ID:          creds.ID,
			Username:    creds.Username,
			Email:       creds.Email,
			DisplayName: creds.DisplayName,
			Status:      creds.Status,
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
		ID:          u.ID,
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		Status:      u.Status,
	}, nil
}

func (a *authIdentityAdapter) RecordLogin(ctx context.Context, userID uuid.UUID) error {
	return a.svc.RecordLogin(ctx, userID)
}

func (a *authIdentityAdapter) UpdatePasswordHash(ctx context.Context, userID uuid.UUID, newHash string) error {
	return a.svc.UpdatePasswordHash(ctx, userID, newHash)
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
	refs, err := a.svc.ResolveRoles(ctx, ids)
	if err != nil {
		return nil, err
	}
	return toIdentityRoleRefs(refs), nil
}

func (a *identityAccessAdapter) ListRolesByUserIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]identity.RoleRef, error) {
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
	return a.svc.ListUserIDsByRoleIDs(ctx, roleIDs)
}

func (a *identityAccessAdapter) ListUserIDsByRoleCode(ctx context.Context, code string) ([]uuid.UUID, error) {
	return a.svc.ListUserIDsByRoleCode(ctx, code)
}

func (a *identityAccessAdapter) ReplaceUserRoles(ctx context.Context, userID uuid.UUID, roleIDs []uuid.UUID) error {
	return a.svc.ReplaceUserRoles(ctx, userID, roleIDs)
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

// identitySessionAdapter bridges auth.Service into identity.SessionRevoker using late binding to break construction order cycles.
type identitySessionAdapter struct {
	svc *auth.Service
}

var _ identity.SessionRevoker = (*identitySessionAdapter)(nil)

func (a *identitySessionAdapter) RevokeAllUserSessions(ctx context.Context, userID uuid.UUID) error {
	if a.svc == nil {
		return nil
	}
	return a.svc.RevokeAllUserSessions(ctx, userID)
}

// identityAuditAdapter bridges audit.Service into identity.AuditRecorder.
type identityAuditAdapter struct {
	svc *audit.Service
}

var _ identity.AuditRecorder = (*identityAuditAdapter)(nil)

func (a *identityAuditAdapter) Record(ctx context.Context, entry identity.AuditEntry) error {
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
