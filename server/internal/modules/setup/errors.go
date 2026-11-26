package setup

import (
	"net/http"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the setup module.
const (
	ErrCodeAlreadyInitialized = "SETUP_ALREADY_INITIALIZED"
	ErrCodeTokenInvalid       = "SETUP_TOKEN_INVALID"
	ErrCodeLocalOnly          = "SETUP_LOCAL_ONLY"
)

func alreadyInitializedError() *apperr.Error {
	return apperr.Conflict(ErrCodeAlreadyInitialized)
}

func tokenInvalidError() *apperr.Error {
	return apperr.New(http.StatusForbidden, ErrCodeTokenInvalid)
}

func localOnlyError() *apperr.Error {
	return apperr.New(http.StatusForbidden, ErrCodeLocalOnly)
}
