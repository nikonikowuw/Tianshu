package access

import (
	"errors"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the access module.
const (
	ErrCodeRoleNotFound          = "ACCESS_ROLE_NOT_FOUND"
	ErrCodeRoleInUse             = "ROLES_IN_USE"
	ErrCodeRoleSystemImmutable   = "ROLES_SYSTEM_IMMUTABLE"
	ErrCodeRoleCodeConflict      = "ROLES_CODE_CONFLICT"
	ErrCodeInvalidRoleCode       = "ACCESS_INVALID_ROLE_CODE"
	ErrCodeInvalidRoleName       = "ACCESS_INVALID_ROLE_NAME"
	ErrCodeInvalidRoleDesc       = "ACCESS_INVALID_ROLE_DESCRIPTION"
	ErrCodeInvalidPermissionCode = "ACCESS_INVALID_PERMISSION_CODE"
	ErrCodeInvalidNumber         = "ACCESS_INVALID_NUMBER"
	ErrCodeInvalidBool           = "ACCESS_INVALID_BOOL"
)

// Internal store sentinels translated by Service into apperr.Error.
var (
	errRoleNotFound     = errors.New("role not found")
	errRoleCodeConflict = errors.New("role code already exists")
)

func roleNotFoundError() *apperr.Error {
	return apperr.NotFound(ErrCodeRoleNotFound)
}

func roleInUseError() *apperr.Error {
	return apperr.Conflict(ErrCodeRoleInUse)
}

func roleSystemImmutableError() *apperr.Error {
	return apperr.Conflict(ErrCodeRoleSystemImmutable)
}

func roleCodeConflictError() *apperr.Error {
	return apperr.Conflict(ErrCodeRoleCodeConflict)
}
