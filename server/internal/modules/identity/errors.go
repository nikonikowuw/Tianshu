package identity

import (
	"errors"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the identity module.
const (
	ErrCodeUsernameExists  = "IDENTITY_USERNAME_EXISTS"
	ErrCodeEmailExists     = "IDENTITY_EMAIL_EXISTS"
	ErrCodeInvalidPassword = "IDENTITY_INVALID_PASSWORD"
	ErrCodeUserNotFound    = "IDENTITY_USER_NOT_FOUND"
)

var (
	errUsernameConflict = errors.New("username conflict")
	errEmailConflict    = errors.New("email conflict")
)

func usernameExistsError() *apperr.Error {
	return apperr.Conflict(ErrCodeUsernameExists)
}

func emailExistsError() *apperr.Error {
	return apperr.Conflict(ErrCodeEmailExists)
}

func invalidPasswordError() *apperr.Error {
	return apperr.New(422, ErrCodeInvalidPassword)
}
