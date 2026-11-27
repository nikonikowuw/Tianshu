package apperr

import (
	"cmp"
	"errors"
	"maps"
	"net/http"
	"slices"
)

// Common baseline error codes shared across modules and HTTP middleware.
const (
	CodeBadRequest       = "BAD_REQUEST"
	CodeValidationFailed = "VALIDATION_FAILED"
	CodeUnauthorized     = "AUTH_UNAUTHORIZED"
	CodeForbidden        = "AUTH_FORBIDDEN"
	CodeNotFound         = "RESOURCE_NOT_FOUND"
	CodeConflict         = "RESOURCE_CONFLICT"
	CodeTooLarge         = "PAYLOAD_TOO_LARGE"
	CodeRateLimited      = "RATE_LIMITED"
	CodeInternal         = "INTERNAL_ERROR"
	CodeUnavailable      = "SERVICE_UNAVAILABLE"
)

// FieldError describes a single validation failure mapped to a JSON field path.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error is the domain error contract returned by service layers and handled by HTTP ErrorHandler.
type Error struct {
	Status int
	Code   string
	Params map[string]any
	Fields []FieldError
	Cause  error
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		if e.Code == "" {
			return e.Cause.Error()
		}
		return e.Code + ": " + e.Cause.Error()
	}
	return e.Code
}

// Unwrap exposes the underlying error for errors.Is / errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// New constructs a domain error with explicit HTTP status and stable code.
func New(status int, code string) *Error {
	return &Error{
		Status: status,
		Code:   code,
	}
}

// Wrap constructs a domain error wrapping an underlying cause for internal logging.
func Wrap(status int, code string, cause error) *Error {
	return &Error{
		Status: status,
		Code:   code,
		Cause:  cause,
	}
}

// WithParams attaches template parameters used by the i18n renderer.
func (e *Error) WithParams(params map[string]any) *Error {
	if e == nil {
		return nil
	}
	clone := *e
	clone.Params = maps.Clone(params)
	clone.Fields = slices.Clone(e.Fields)
	return &clone
}

// BadRequest creates a 400 error for malformed syntax or unsupported content.
func BadRequest(code string, cause error) *Error {
	if code == "" {
		code = CodeBadRequest
	}
	return Wrap(http.StatusBadRequest, code, cause)
}

// Validation creates a 422 error carrying field-level validation issues sorted and deduplicated by field.
func Validation(fields []FieldError) *Error {
	return &Error{
		Status: http.StatusUnprocessableEntity,
		Code:   CodeValidationFailed,
		Fields: normalizeFieldErrors(fields),
	}
}

func normalizeFieldErrors(in []FieldError) []FieldError {
	if len(in) == 0 {
		return nil
	}
	byField := make(map[string]FieldError, len(in))
	for _, fe := range in {
		if _, exists := byField[fe.Field]; !exists {
			byField[fe.Field] = fe
		}
	}
	out := make([]FieldError, 0, len(byField))
	for _, fe := range byField {
		out = append(out, fe)
	}
	slices.SortFunc(out, func(a, b FieldError) int {
		return cmp.Compare(a.Field, b.Field)
	})
	return out
}

// Unauthorized creates a 401 authentication error.
func Unauthorized(code string) *Error {
	if code == "" {
		code = CodeUnauthorized
	}
	return New(http.StatusUnauthorized, code)
}

// Forbidden creates a 403 authorization or disabled-account error.
func Forbidden(code string) *Error {
	if code == "" {
		code = CodeForbidden
	}
	return New(http.StatusForbidden, code)
}

// NotFound creates a 404 missing resource error.
func NotFound(code string) *Error {
	if code == "" {
		code = CodeNotFound
	}
	return New(http.StatusNotFound, code)
}

// Conflict creates a 409 uniqueness or state conflict error.
func Conflict(code string) *Error {
	if code == "" {
		code = CodeConflict
	}
	return New(http.StatusConflict, code)
}

// TooLarge creates a 413 payload-too-large error.
func TooLarge(code string) *Error {
	if code == "" {
		code = CodeTooLarge
	}
	return New(http.StatusRequestEntityTooLarge, code)
}

// RateLimited creates a 429 rate-limit error.
func RateLimited() *Error {
	return New(http.StatusTooManyRequests, CodeRateLimited)
}

// Internal creates a 500 internal error without leaking cause to clients.
func Internal(cause error) *Error {
	return Wrap(http.StatusInternalServerError, CodeInternal, cause)
}

// Unavailable creates a 503 service-unavailable error wrapping an optional cause.
func Unavailable(code string, cause error) *Error {
	if code == "" {
		code = CodeUnavailable
	}
	return Wrap(http.StatusServiceUnavailable, code, cause)
}

// As extracts a non-nil *apperr.Error from any error chain.
func As(err error) (*Error, bool) {
	var target *Error
	if errors.As(err, &target) && target != nil {
		return target, true
	}
	return nil, false
}
