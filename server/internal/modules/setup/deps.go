package setup

import (
	"context"

	"github.com/google/uuid"
)

// CreateAdminParams defines the minimal contract required from the identity module.
type CreateAdminParams struct {
	Username    string
	Email       string
	DisplayName string
	Password    string
}

// IdentityManager is the minimal dependency on identity.Service, wired in internal/app.
type IdentityManager interface {
	CountUsers(ctx context.Context) (int64, error)
	CreateInitialAdmin(ctx context.Context, params CreateAdminParams) (CreatedAdmin, error)
}

// RoleAssigner is the minimal dependency on access.Service, wired in internal/app.
type RoleAssigner interface {
	AssignRoleByCode(ctx context.Context, userID uuid.UUID, roleCode string) error
}

// TxRunner executes fn inside a database transaction propagated via ctx.
type TxRunner interface {
	WithinTx(ctx context.Context, fn func(ctx context.Context) error) error
}
