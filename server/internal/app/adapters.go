package app

import (
	"context"

	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/identity"
	"tianshu/internal/modules/setup"
)

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
