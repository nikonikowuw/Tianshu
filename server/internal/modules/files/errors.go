package files

import (
	"net/http"

	"tianshu/internal/shared/apperr"
)

// Stable error codes owned by the files module.
const (
	ErrCodeNotFound           = "FILES_NOT_FOUND"
	ErrCodeTypeNotAllowed     = "FILES_TYPE_NOT_ALLOWED"
	ErrCodeTooLarge           = "FILES_TOO_LARGE"
	ErrCodeEmptyFile          = "FILES_EMPTY_FILE"
	ErrCodeInUse              = "FILES_IN_USE"
	ErrCodeInvalidImage       = "FILES_INVALID_IMAGE"
	ErrCodeInvalidSignedURL   = "FILES_INVALID_SIGNED_URL"
	ErrCodeStorageUnavailable = "FILES_STORAGE_UNAVAILABLE"
	ErrCodeMissingFile        = "FILES_MISSING_FILE"
	ErrCodeInvalidDisposition = "FILES_INVALID_DISPOSITION"
	ErrCodeInvalidCategory    = "FILES_INVALID_CATEGORY"
	ErrCodeInvalidDriver      = "FILES_INVALID_DRIVER"
	ErrCodeInvalidPage        = "FILES_INVALID_PAGE"
	ErrCodeInvalidPageSize    = "FILES_INVALID_PAGE_SIZE"
	ErrCodeQueryTooLong       = "FILES_QUERY_TOO_LONG"
	ErrCodeTooManyMIMEFilters = "FILES_TOO_MANY_MIME_FILTERS"
)

func errFileNotFound() *apperr.Error {
	return apperr.NotFound(ErrCodeNotFound)
}

func errFileTypeNotAllowed() *apperr.Error {
	return apperr.BadRequest(ErrCodeTypeNotAllowed, nil)
}

func errFileTooLarge() *apperr.Error {
	return apperr.New(http.StatusRequestEntityTooLarge, ErrCodeTooLarge)
}

func errEmptyFile() *apperr.Error {
	return apperr.BadRequest(ErrCodeEmptyFile, nil)
}

func errFileInUse() *apperr.Error {
	return apperr.Conflict(ErrCodeInUse)
}

func errInvalidImage(cause error) *apperr.Error {
	return apperr.BadRequest(ErrCodeInvalidImage, cause)
}

func errInvalidSignedURL() *apperr.Error {
	return apperr.Unauthorized(ErrCodeInvalidSignedURL)
}

func errStorageUnavailable(cause error) *apperr.Error {
	return apperr.Wrap(http.StatusServiceUnavailable, ErrCodeStorageUnavailable, cause)
}

func errMissingFile() *apperr.Error {
	return apperr.BadRequest(ErrCodeMissingFile, nil)
}
