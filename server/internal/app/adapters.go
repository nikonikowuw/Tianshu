package app

import (
	"context"

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
