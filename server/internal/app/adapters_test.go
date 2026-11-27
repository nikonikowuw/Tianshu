package app

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
)

func TestIdentityAccessAdapterMapsMissingRoleToValidationError(t *testing.T) {
	svc := access.NewService(nil, access.ServiceDeps{
		Log: logger.New("test", "error", io.Discard),
	})
	_ = svc

	// Verify the adapter maps ACCESS_ROLE_NOT_FOUND into 422 roleIds: USERS_INVALID_ROLE.
	mapped := mapReplaceUserRolesErr(apperr.NotFound(access.ErrCodeRoleNotFound))
	appErr, ok := apperr.As(mapped)
	if !ok || appErr.Status != http.StatusUnprocessableEntity {
		t.Fatalf("mapped error = %+v, want 422 VALIDATION_FAILED", mapped)
	}
	if len(appErr.Fields) != 1 || appErr.Fields[0].Field != "roleIds" || appErr.Fields[0].Message != identity.ErrCodeInvalidRole {
		t.Fatalf("unexpected field errors: %+v", appErr.Fields)
	}

	other := apperr.Internal(context.Canceled)
	if got := mapReplaceUserRolesErr(other); got != other {
		t.Fatalf("expected non-role-not-found error to pass through unchanged, got %v", got)
	}
	_ = uuid.Nil
}
