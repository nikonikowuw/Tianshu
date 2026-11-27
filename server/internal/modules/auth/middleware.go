package auth

import (
	"errors"
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
		token, ok := extractBearerToken(c.GetHeader("Authorization"))
		if !ok {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}

		claims, err := jwtMgr.Verify(token)
		if err != nil {
			if errors.Is(err, security.ErrExpiredToken) {
				_ = c.Error(tokenExpiredError())
			} else {
				_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			}
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

// extractBearerToken extracts a Bearer token case-insensitively per RFC 9110 §11.1.
func extractBearerToken(header string) (string, bool) {
	raw := strings.TrimSpace(header)
	const scheme = "Bearer"
	if len(raw) <= len(scheme) || !strings.EqualFold(raw[:len(scheme)], scheme) {
		return "", false
	}
	sep := raw[len(scheme)]
	if sep != ' ' && sep != '\t' {
		return "", false
	}
	token := strings.TrimSpace(raw[len(scheme)+1:])
	if token == "" {
		return "", false
	}
	return token, true
}

// requireSameOrigin enforces strict Origin/Referer validation on cookie-authenticated endpoints (refresh/logout).
// Requests carrying the refresh cookie MUST supply a valid same-origin (matching scheme and host) or
// allowlisted Origin/Referer; missing headers, malformed headers, or "Origin: null" are rejected with 403 AUTH_ORIGIN_REJECTED.
func requireSameOrigin(allowedOrigins []string, cookieSecure bool) gin.HandlerFunc {
	allowed := make(map[string]struct{}, len(allowedOrigins))
	for _, o := range allowedOrigins {
		trimmed := strings.TrimRight(strings.TrimSpace(o), "/")
		if trimmed == "" || trimmed == "*" {
			continue
		}
		if _, _, canonical, ok := parseStrictOrigin(trimmed); ok {
			allowed[canonical] = struct{}{}
		}
	}

	return func(c *gin.Context) {
		var (
			scheme    string
			host      string
			canonical string
			hasOrigin bool
		)

		if rawOrigin := strings.TrimSpace(c.GetHeader("Origin")); rawOrigin != "" {
			var ok bool
			scheme, host, canonical, ok = parseStrictOrigin(rawOrigin)
			if !ok {
				_ = c.Error(originRejectedError())
				c.Abort()
				return
			}
			hasOrigin = true
		} else if rawRef := strings.TrimSpace(c.GetHeader("Referer")); rawRef != "" {
			var ok bool
			scheme, host, canonical, ok = parseRefererOrigin(rawRef)
			if !ok {
				_ = c.Error(originRejectedError())
				c.Abort()
				return
			}
			hasOrigin = true
		}

		if !hasOrigin {
			// If a refresh cookie is present, missing both Origin and Referer must be rejected to prevent CSRF.
			if cookieVal, err := c.Cookie(refreshCookieName); err == nil && strings.TrimSpace(cookieVal) != "" {
				_ = c.Error(originRejectedError())
				c.Abort()
				return
			}
			c.Next()
			return
		}

		if _, ok := allowed[canonical]; ok {
			c.Next()
			return
		}

		expectedScheme := expectedRequestScheme(c, cookieSecure)
		if c.Request.Host != "" &&
			strings.EqualFold(scheme, expectedScheme) &&
			strings.EqualFold(host, c.Request.Host) {
			c.Next()
			return
		}

		_ = c.Error(originRejectedError())
		c.Abort()
	}
}

func parseStrictOrigin(raw string) (scheme, host, canonical string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", "", "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Path != "" || parsed.RawPath != "" ||
		parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" ||
		parsed.Host == "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", false
	}
	host = strings.ToLower(parsed.Host)
	return scheme, host, scheme + "://" + host, true
}

func parseRefererOrigin(raw string) (scheme, host, canonical string, ok bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", "", "", false
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" {
		return "", "", "", false
	}
	scheme = strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", "", false
	}
	host = strings.ToLower(parsed.Host)
	return scheme, host, scheme + "://" + host, true
}

func expectedRequestScheme(c *gin.Context, cookieSecure bool) string {
	if c.Request.TLS != nil || cookieSecure {
		return "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		if strings.EqualFold(strings.TrimSpace(first), "https") {
			return "https"
		}
	}
	return "http"
}
