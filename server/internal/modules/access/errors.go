package access

import (
	"errors"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the access module.
const (
	ErrCodeRoleNotFound = "ACCESS_ROLE_NOT_FOUND"
	ErrCodeRoleInUse    = "ROLES_IN_USE"
)

// errRoleNotFound is an internal store sentinel translated by Service into apperr.Error.
var errRoleNotFound = errors.New("role not found")

func roleNotFoundError() *apperr.Error {
	return apperr.NotFound(ErrCodeRoleNotFound)
}
