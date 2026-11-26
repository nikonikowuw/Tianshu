package setup

import "tianshu/internal/shared/apperr"

// Domain error codes owned by the setup module.
const (
	ErrCodeAlreadyInitialized = "SETUP_ALREADY_INITIALIZED"
)

func alreadyInitializedError() *apperr.Error {
	return apperr.Conflict(ErrCodeAlreadyInitialized)
}
