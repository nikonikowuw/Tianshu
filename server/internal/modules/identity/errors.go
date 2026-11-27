package identity

import (
	"errors"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the identity module.
const (
	ErrCodeUsernameConflict  = "USERS_USERNAME_CONFLICT"
	ErrCodeEmailConflict     = "USERS_EMAIL_CONFLICT"
	ErrCodeInvalidPassword   = "USERS_INVALID_PASSWORD"
	ErrCodeUserNotFound      = "USERS_NOT_FOUND"
	ErrCodeLastSuperAdmin    = "USERS_LAST_SUPER_ADMIN"
	ErrCodeSelfOperation     = "USERS_SELF_OPERATION_FORBIDDEN"
	ErrCodeSuperAdminProtect = "USERS_SUPER_ADMIN_PROTECTED"
	ErrCodeInvalidRole       = "USERS_INVALID_ROLE"
	ErrCodeInvalidUsername   = "USERS_INVALID_USERNAME"
	ErrCodeInvalidEmail      = "USERS_INVALID_EMAIL"
	ErrCodeInvalidDisplay    = "USERS_INVALID_DISPLAY_NAME"
	ErrCodeInvalidStatus     = "USERS_INVALID_STATUS"

	ErrCodeProfileInvalidCurrentPassword = "PROFILE_INVALID_CURRENT_PASSWORD"
	ErrCodeProfilePasswordUnchanged      = "PROFILE_PASSWORD_UNCHANGED"
	ErrCodeProfileInvalidAvatar          = "PROFILE_INVALID_AVATAR"
	ErrCodeProfileSessionNotFound        = "PROFILE_SESSION_NOT_FOUND"
	ErrCodeProfileCurrentSessionRevoke   = "PROFILE_CURRENT_SESSION_REVOKE_FORBIDDEN"

	// Canonical cross-module code translated by the auth locale catalog so disabled accounts
	// trigger uniform client-side session clearing across all authenticated endpoints.
	errCodeAccountDisabled = "AUTH_ACCOUNT_DISABLED"
)

var (
	errUsernameConflict = errors.New("username conflict")
	errEmailConflict    = errors.New("email conflict")
	errUserNotFound     = errors.New("user not found")
)

func usernameExistsError() *apperr.Error {
	return apperr.Conflict(ErrCodeUsernameConflict)
}

func emailExistsError() *apperr.Error {
	return apperr.Conflict(ErrCodeEmailConflict)
}

func invalidPasswordError() *apperr.Error {
	return apperr.New(422, ErrCodeInvalidPassword)
}

func userNotFoundError() *apperr.Error {
	return apperr.NotFound(ErrCodeUserNotFound)
}

func lastSuperAdminError() *apperr.Error {
	return apperr.Conflict(ErrCodeLastSuperAdmin)
}

func selfOperationError() *apperr.Error {
	return apperr.Conflict(ErrCodeSelfOperation)
}

func superAdminProtectedError() *apperr.Error {
	return apperr.Forbidden(ErrCodeSuperAdminProtect)
}

func invalidCurrentPasswordError() *apperr.Error {
	return apperr.BadRequest(ErrCodeProfileInvalidCurrentPassword, nil)
}

func profileSessionNotFoundError() *apperr.Error {
	return apperr.NotFound(ErrCodeProfileSessionNotFound)
}

func currentSessionRevokeError() *apperr.Error {
	return apperr.Conflict(ErrCodeProfileCurrentSessionRevoke)
}

func accountDisabledError() *apperr.Error {
	return apperr.Forbidden(errCodeAccountDisabled)
}
