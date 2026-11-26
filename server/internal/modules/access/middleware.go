package access

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

// PermissionChecker is the minimal contract required by RequirePermission middleware.
type PermissionChecker interface {
	HasPermission(ctx context.Context, userID uuid.UUID, permissionCode string) (bool, error)
}

var _ PermissionChecker = (*Service)(nil)

// RequirePermission enforces that the authenticated caller holds permissionCode.
func RequirePermission(checker PermissionChecker, permissionCode string) gin.HandlerFunc {
	return func(c *gin.Context) {
		identity, ok := authctx.IdentityFromContext(c.Request.Context())
		if !ok {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}

		allowed, err := checker.HasPermission(c.Request.Context(), identity.UserID, permissionCode)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}
		if !allowed {
			_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
			c.Abort()
			return
		}

		c.Next()
	}
}
