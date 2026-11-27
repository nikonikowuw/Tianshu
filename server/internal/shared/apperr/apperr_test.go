package apperr_test

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"testing"

	"tianshu/internal/shared/apperr"
)

func TestErrorMethodsAndUnwrap(t *testing.T) {
	sentinel := errors.New("root failure")

	tests := []struct {
		name     string
		err      *apperr.Error
		wantStr  string
		wantWrap error
	}{
		{
			name:     "nil receiver",
			err:      nil,
			wantStr:  "",
			wantWrap: nil,
		},
		{
			name:     "code without cause",
			err:      apperr.New(http.StatusConflict, apperr.CodeConflict),
			wantStr:  apperr.CodeConflict,
			wantWrap: nil,
		},
		{
			name:     "code with cause",
			err:      apperr.Wrap(http.StatusInternalServerError, apperr.CodeInternal, sentinel),
			wantStr:  apperr.CodeInternal + ": root failure",
			wantWrap: sentinel,
		},
		{
			name:     "empty code with cause avoids leading colon",
			err:      apperr.Wrap(http.StatusBadRequest, "", sentinel),
			wantStr:  "root failure",
			wantWrap: sentinel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.wantStr {
				t.Fatalf("Error() = %q, want %q", got, tt.wantStr)
			}
			if got := tt.err.Unwrap(); !errors.Is(got, tt.wantWrap) {
				t.Fatalf("Unwrap() = %v, want %v", got, tt.wantWrap)
			}
			if tt.wantWrap != nil && !errors.Is(tt.err, tt.wantWrap) {
				t.Fatalf("errors.Is(err, sentinel) = false, want true")
			}
		})
	}
}

func TestAs(t *testing.T) {
	base := apperr.NotFound("USERS_NOT_FOUND")
	wrapped := fmt.Errorf("load user: %w", base)
	var typedNil *apperr.Error
	wrappedTypedNil := fmt.Errorf("wrapped typed nil: %w", typedNil)

	tests := []struct {
		name   string
		err    error
		want   *apperr.Error
		wantOK bool
	}{
		{
			name:   "nil error",
			err:    nil,
			want:   nil,
			wantOK: false,
		},
		{
			name:   "plain error",
			err:    errors.New("plain"),
			want:   nil,
			wantOK: false,
		},
		{
			name:   "typed nil apperr pointer",
			err:    typedNil,
			want:   nil,
			wantOK: false,
		},
		{
			name:   "wrapped typed nil apperr pointer",
			err:    wrappedTypedNil,
			want:   nil,
			wantOK: false,
		},
		{
			name:   "direct apperr",
			err:    base,
			want:   base,
			wantOK: true,
		},
		{
			name:   "wrapped apperr",
			err:    wrapped,
			want:   base,
			wantOK: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := apperr.As(tt.err)
			if ok != tt.wantOK {
				t.Fatalf("As() ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.want {
				t.Fatalf("As() err = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWithParamsIsolationAndNilSafety(t *testing.T) {
	var nilErr *apperr.Error
	if got := nilErr.WithParams(map[string]any{"retry_after": 10}); got != nil {
		t.Fatalf("nil.WithParams() = %v, want nil", got)
	}

	orig := apperr.Validation([]apperr.FieldError{
		{Field: "email", Message: "VALIDATION_EMAIL"},
	})
	inputParams := map[string]any{"retry_after": 30}
	cloned := orig.WithParams(inputParams)

	if cloned == orig {
		t.Fatal("WithParams() must return a new instance")
	}
	if orig.Params != nil {
		t.Fatalf("WithParams() mutated original Params: %v", orig.Params)
	}

	// Mutating caller map or cloned Fields must not leak across instances.
	inputParams["retry_after"] = 999
	if cloned.Params["retry_after"] != 30 {
		t.Fatalf("WithParams() aliased caller map: got %v, want 30", cloned.Params["retry_after"])
	}

	cloned.Fields[0].Message = "MUTATED"
	if orig.Fields[0].Message != "VALIDATION_EMAIL" {
		t.Fatalf("WithParams() aliased Fields slice: orig=%+v", orig.Fields)
	}
}

func TestValidationSortDedupeAndIsolation(t *testing.T) {
	t.Run("empty fields", func(t *testing.T) {
		err := apperr.Validation(nil)
		if err.Status != http.StatusUnprocessableEntity || err.Code != apperr.CodeValidationFailed {
			t.Fatalf("got status=%d code=%q, want 422 %s", err.Status, err.Code, apperr.CodeValidationFailed)
		}
		if len(err.Fields) != 0 {
			t.Fatalf("expected empty fields, got %+v", err.Fields)
		}
	})

	t.Run("sorts deduplicates preserving first and isolates caller slice", func(t *testing.T) {
		input := []apperr.FieldError{
			{Field: "username", Message: "FIRST_USERNAME"},
			{Field: "email", Message: "FIRST_EMAIL"},
			{Field: "username", Message: "SECOND_USERNAME"},
			{Field: "age", Message: "FIRST_AGE"},
		}
		err := apperr.Validation(input)

		// Mutate caller slice after construction.
		input[0].Message = "MUTATED_BY_CALLER"

		want := []apperr.FieldError{
			{Field: "age", Message: "FIRST_AGE"},
			{Field: "email", Message: "FIRST_EMAIL"},
			{Field: "username", Message: "FIRST_USERNAME"},
		}
		if !reflect.DeepEqual(err.Fields, want) {
			t.Fatalf("Fields = %+v, want %+v", err.Fields, want)
		}
	})
}

func TestConstructorsAndFallbacks(t *testing.T) {
	cause := errors.New("underlying cause")

	tests := []struct {
		name       string
		err        *apperr.Error
		wantStatus int
		wantCode   string
		wantCause  error
	}{
		{
			name:       "BadRequest empty code fallback",
			err:        apperr.BadRequest("", cause),
			wantStatus: http.StatusBadRequest,
			wantCode:   apperr.CodeBadRequest,
			wantCause:  cause,
		},
		{
			name:       "BadRequest explicit code",
			err:        apperr.BadRequest("FILES_EMPTY_FILE", nil),
			wantStatus: http.StatusBadRequest,
			wantCode:   "FILES_EMPTY_FILE",
		},
		{
			name:       "Unauthorized empty code fallback",
			err:        apperr.Unauthorized(""),
			wantStatus: http.StatusUnauthorized,
			wantCode:   apperr.CodeUnauthorized,
		},
		{
			name:       "Unauthorized explicit code",
			err:        apperr.Unauthorized("AUTH_TOKEN_EXPIRED"),
			wantStatus: http.StatusUnauthorized,
			wantCode:   "AUTH_TOKEN_EXPIRED",
		},
		{
			name:       "Forbidden empty code fallback",
			err:        apperr.Forbidden(""),
			wantStatus: http.StatusForbidden,
			wantCode:   apperr.CodeForbidden,
		},
		{
			name:       "Forbidden explicit code",
			err:        apperr.Forbidden("AUTH_ACCOUNT_DISABLED"),
			wantStatus: http.StatusForbidden,
			wantCode:   "AUTH_ACCOUNT_DISABLED",
		},
		{
			name:       "NotFound empty code fallback",
			err:        apperr.NotFound(""),
			wantStatus: http.StatusNotFound,
			wantCode:   apperr.CodeNotFound,
		},
		{
			name:       "NotFound explicit code",
			err:        apperr.NotFound("USERS_NOT_FOUND"),
			wantStatus: http.StatusNotFound,
			wantCode:   "USERS_NOT_FOUND",
		},
		{
			name:       "Conflict empty code fallback",
			err:        apperr.Conflict(""),
			wantStatus: http.StatusConflict,
			wantCode:   apperr.CodeConflict,
		},
		{
			name:       "Conflict explicit code",
			err:        apperr.Conflict("ROLES_CODE_CONFLICT"),
			wantStatus: http.StatusConflict,
			wantCode:   "ROLES_CODE_CONFLICT",
		},
		{
			name:       "TooLarge empty code fallback",
			err:        apperr.TooLarge(""),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   apperr.CodeTooLarge,
		},
		{
			name:       "TooLarge explicit code",
			err:        apperr.TooLarge("FILES_TOO_LARGE"),
			wantStatus: http.StatusRequestEntityTooLarge,
			wantCode:   "FILES_TOO_LARGE",
		},
		{
			name:       "RateLimited",
			err:        apperr.RateLimited(),
			wantStatus: http.StatusTooManyRequests,
			wantCode:   apperr.CodeRateLimited,
		},
		{
			name:       "Internal",
			err:        apperr.Internal(cause),
			wantStatus: http.StatusInternalServerError,
			wantCode:   apperr.CodeInternal,
			wantCause:  cause,
		},
		{
			name:       "Unavailable empty code fallback",
			err:        apperr.Unavailable("", cause),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   apperr.CodeUnavailable,
			wantCause:  cause,
		},
		{
			name:       "Unavailable explicit code",
			err:        apperr.Unavailable("FILES_STORAGE_UNAVAILABLE", cause),
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "FILES_STORAGE_UNAVAILABLE",
			wantCause:  cause,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err.Status != tt.wantStatus {
				t.Fatalf("Status = %d, want %d", tt.err.Status, tt.wantStatus)
			}
			if tt.err.Code != tt.wantCode {
				t.Fatalf("Code = %q, want %q", tt.err.Code, tt.wantCode)
			}
			if !errors.Is(tt.err.Cause, tt.wantCause) {
				t.Fatalf("Cause = %v, want %v", tt.err.Cause, tt.wantCause)
			}
		})
	}
}
