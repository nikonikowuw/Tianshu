package app

import (
	"context"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
	"tianshu/internal/webui"
)

const dbReadinessTimeout = 2 * time.Second

func (a *App) registerRoutes() {
	checks := map[string]httpserver.ReadinessCheck{}
	if a.db != nil {
		checks["postgres"] = func(ctx context.Context) error {
			pingCtx, cancel := context.WithTimeout(ctx, dbReadinessTimeout)
			defer cancel()
			return a.db.Ping(pingCtx)
		}
	}
	httpserver.RegisterHealthRoutes(a.engine, checks)

	apiV1 := a.engine.Group(apiV1Prefix)
	if a.cfg.Server.MaxRequestBodyBytes > 0 {
		apiV1.Use(maxRequestBodyMiddleware(a.cfg.Server.MaxRequestBodyBytes))
	}

	publicV1 := apiV1.Group("")
	if a.cfg.Server.APIRateLimit > 0 {
		publicV1.Use(httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
			Limit:  a.cfg.Server.APIRateLimit,
			Window: time.Minute,
			KeyFunc: func(c *gin.Context) string {
				return "api-ip:" + c.ClientIP()
			},
		}))
	}
	if a.setupHandler != nil {
		a.setupHandler.RegisterPublicRoutes(publicV1)
	}
	if a.filesHandler != nil {
		a.filesHandler.RegisterPublicRoutes(publicV1)
	}
	if a.authHandler != nil {
		a.authHandler.RegisterPublicRoutes(publicV1)
	}

	if a.jwt != nil {
		authedV1 := apiV1.Group("")
		authedV1.Use(auth.RequireAuth(a.jwt))
		if a.cfg.Server.APIRateLimit > 0 {
			authedV1.Use(httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
				Limit:  a.cfg.Server.APIRateLimit,
				Window: time.Minute,
				KeyFunc: func(c *gin.Context) string {
					if id, ok := authctx.IdentityFromContext(c.Request.Context()); ok {
						return "api-user:" + id.UserID.String()
					}
					return "api-ip:" + c.ClientIP()
				},
			}))
		}
		if a.authHandler != nil {
			a.authHandler.RegisterRoutes(authedV1)
		}
		if a.accessHandler != nil && a.access != nil {
			a.accessHandler.RegisterRoutes(authedV1, access.RoutePermissions{
				RequireRead:       access.RequirePermission(a.access, access.PermRolesRead),
				RequireOptions:    access.RequireAnyPermission(a.access, access.PermUsersRead, access.PermRolesRead),
				RequireCreate:     access.RequirePermission(a.access, access.PermRolesCreate),
				RequireUpdate:     access.RequirePermission(a.access, access.PermRolesUpdate),
				RequireDelete:     access.RequirePermission(a.access, access.PermRolesDelete),
				RequireMenuRead:   access.RequireAnyPermission(a.access, access.PermMenusRead, access.PermRolesRead),
				RequireMenuCreate: access.RequirePermission(a.access, access.PermMenusCreate),
				RequireMenuUpdate: access.RequirePermission(a.access, access.PermMenusUpdate),
				RequireMenuDelete: access.RequirePermission(a.access, access.PermMenusDelete),
			})
		}
		if a.identityHandler != nil && a.access != nil {
			a.identityHandler.RegisterRoutes(authedV1, identity.RoutePermissions{
				RequireRead:   access.RequirePermission(a.access, access.PermUsersRead),
				RequireCreate: access.RequirePermission(a.access, access.PermUsersCreate),
				RequireUpdate: access.RequirePermission(a.access, access.PermUsersUpdate),
				RequireDelete: access.RequirePermission(a.access, access.PermUsersDelete),
			})
		}
		if a.auditHandler != nil && a.access != nil {
			a.auditHandler.RegisterRoutes(
				authedV1,
				access.RequirePermission(a.access, access.PermAuditRead),
			)
		}
		if a.filesHandler != nil && a.access != nil {
			a.filesHandler.RegisterRoutes(authedV1, files.RoutePermissions{
				RequireRead:   access.RequirePermission(a.access, access.PermFilesRead),
				RequireUpload: access.RequirePermission(a.access, access.PermFilesUpload),
				RequireDelete: access.RequirePermission(a.access, access.PermFilesDelete),
			})
		}
		if a.dashboardHandler != nil && a.access != nil {
			a.dashboardHandler.RegisterRoutes(
				authedV1,
				access.RequirePermission(a.access, access.PermDashboardRead),
			)
		}
	}

	spaHandler := webui.Handler()
	a.engine.NoRoute(func(c *gin.Context) {
		cleaned := path.Clean("/" + c.Request.URL.Path)
		if cleaned == "/api" || strings.HasPrefix(cleaned, "/api/") {
			_ = c.Error(apperr.NotFound(apperr.CodeNotFound))
			return
		}
		spaHandler.ServeHTTP(c.Writer, c.Request)
	})
}

func maxRequestBodyMiddleware(limitBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Multipart upload endpoints enforce dedicated MaxFileSizeBytes / MaxAvatarSizeBytes caps.
		fullPath := c.FullPath()
		if c.Request.Body != nil && c.Request.Body != http.NoBody &&
			fullPath != "/api/v1/files" && fullPath != "/api/v1/files/avatar" {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limitBytes)
		}
		c.Next()
	}
}
