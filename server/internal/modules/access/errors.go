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

	ErrCodeMenuNotFound        = "MENUS_NOT_FOUND"
	ErrCodeMenuCodeConflict    = "MENUS_CODE_CONFLICT"
	ErrCodeMenuSystemImmutable = "MENUS_SYSTEM_IMMUTABLE"
	ErrCodeMenuHasChildren     = "MENUS_HAS_CHILDREN"
	ErrCodeMenuCycleDetected   = "MENUS_CYCLE_DETECTED"
	ErrCodeMenuInvalidParent   = "MENUS_INVALID_PARENT"
	ErrCodeInvalidMenuType     = "ACCESS_INVALID_MENU_TYPE"
	ErrCodeInvalidMenuTitle    = "ACCESS_INVALID_MENU_TITLE"
	ErrCodeInvalidMenuPath     = "ACCESS_INVALID_MENU_PATH"
	ErrCodeInvalidMenuStatus   = "ACCESS_INVALID_MENU_STATUS"
)

// Internal store sentinels translated by Service into apperr.Error.
var (
	errRoleNotFound     = errors.New("role not found")
	errRoleCodeConflict = errors.New("role code already exists")
	errMenuNotFound     = errors.New("menu not found")
	errMenuCodeConflict = errors.New("menu code already exists")
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

func menuNotFoundError() *apperr.Error {
	return apperr.NotFound(ErrCodeMenuNotFound)
}

func menuCodeConflictError() *apperr.Error {
	return apperr.Conflict(ErrCodeMenuCodeConflict)
}

func menuSystemImmutableError() *apperr.Error {
	return apperr.Conflict(ErrCodeMenuSystemImmutable)
}

func menuHasChildrenError() *apperr.Error {
	return apperr.Conflict(ErrCodeMenuHasChildren)
}

func menuCycleDetectedError() *apperr.Error {
	return apperr.Conflict(ErrCodeMenuCycleDetected)
}

func roleCodeConflictError() *apperr.Error {
	return apperr.Conflict(ErrCodeRoleCodeConflict)
}
