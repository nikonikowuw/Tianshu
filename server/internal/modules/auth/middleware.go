package auth

import (
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

// RequireAuth validates the Bearer HS256 JWT and populates authctx.Identity on the request context.
func RequireAuth(jwtMgr *security.JWTManager) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := strings.TrimSpace(c.GetHeader("Authorization"))
		token, ok := strings.CutPrefix(raw, "Bearer ")
		if !ok || strings.TrimSpace(token) == "" {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}

		claims, err := jwtMgr.Verify(strings.TrimSpace(token))
		if err != nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}

		ctx := authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    claims.UserID,
			SessionID: claims.SessionID,
		})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// RequireSameOrigin enforces Origin/Referer validation on cookie-authenticated endpoints (refresh/logout).
// Requests carrying the refresh cookie MUST supply a valid same-origin or allowlisted Origin/Referer;
// missing headers or "Origin: null" are rejected with 403 AUTH_ORIGIN_REJECTED.
func RequireSameOrigin(allowedOrigins []string) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		trimmed := strings.TrimSpace(o)
		if trimmed != "" && trimmed != "*" {
			allowed[strings.ToLower(trimmed)] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if strings.EqualFold(origin, "null") {
			_ = c.Error(originRejectedError())
			c.Abort()
			return
		}
		if origin == "" {
			if referer := strings.TrimSpace(c.GetHeader("Referer")); referer != "" {
				if parsed, err := url.Parse(referer); err == nil && parsed.Scheme != "" && parsed.Host != "" {
					origin = parsed.Scheme + "://" + parsed.Host
				}
			}
		}

		if origin == "" {
			// If a refresh cookie is present, missing both Origin and Referer must be rejected to prevent CSRF.
			if cookieVal, err := c.Cookie(refreshCookieName); err == nil && strings.TrimSpace(cookieVal) != "" {
				_ = c.Error(originRejectedError())
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if _, ok := allowed[strings.ToLower(origin)]; ok {
			c.Next()
			return
		}

		parsed, err := url.Parse(origin)
		if err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, c.Request.Host) {
			c.Next()
			return
		}

		_ = c.Error(originRejectedError())
		c.Abort()
	}
}
