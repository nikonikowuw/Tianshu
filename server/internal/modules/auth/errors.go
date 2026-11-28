package auth

import (
	"errors"
	"net/http"

	"tianshu/internal/shared/apperr"
)

// Domain error codes owned by the auth module.
const (
	ErrCodeInvalidCredentials = "AUTH_INVALID_CREDENTIALS"
	ErrCodeAccountDisabled    = "AUTH_ACCOUNT_DISABLED"
	ErrCodeTokenExpired       = "AUTH_TOKEN_EXPIRED"
	ErrCodeRefreshReused      = "AUTH_REFRESH_REUSED"
	ErrCodeOriginRejected     = "AUTH_ORIGIN_REJECTED"
)

// authErrorCodes enumerates every module error code so tests can assert each has a
// matching zh-CN and en-US translation. Add new codes here when declaring them above.
var authErrorCodes = []string{
	ErrCodeInvalidCredentials,
	ErrCodeAccountDisabled,
	ErrCodeTokenExpired,
	ErrCodeRefreshReused,
	ErrCodeOriginRejected,
}

// Internal sentinels used to translate storage "not found" conditions. They never escape the package.
var (
	errSessionNotFound = errors.New("session not found")
	errHistoryNotFound = errors.New("refresh token history not found")
)

func invalidCredentialsError() *apperr.Error {
	return apperr.New(http.StatusUnauthorized, ErrCodeInvalidCredentials)
}

func accountDisabledError() *apperr.Error {
	return apperr.New(http.StatusForbidden, ErrCodeAccountDisabled)
}

func tokenExpiredError() *apperr.Error {
	return apperr.New(http.StatusUnauthorized, ErrCodeTokenExpired)
}

func refreshReusedError() *apperr.Error {
	return apperr.New(http.StatusUnauthorized, ErrCodeRefreshReused)
}

func originRejectedError() *apperr.Error {
	return apperr.New(http.StatusForbidden, ErrCodeOriginRejected)
}
