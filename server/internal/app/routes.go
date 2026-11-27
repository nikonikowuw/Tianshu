package app

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

	"tianshu/internal/modules/access"
	"tianshu/internal/modules/auth"
	"tianshu/internal/modules/files"
	"tianshu/internal/modules/identity"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/webui"
)

func (a *App) registerRoutes() {
	checks := map[string]httpserver.ReadinessCheck{}
	if a.db != nil {
		checks["postgres"] = func(ctx context.Context) error {
			return a.db.Ping(ctx)
		}
	}
	httpserver.RegisterHealthRoutes(a.engine, checks)

	apiV1 := a.engine.Group("/api/v1")
	if a.setupHandler != nil {
		a.setupHandler.RegisterPublicRoutes(apiV1)
	}
	if a.filesHandler != nil {
		a.filesHandler.RegisterPublicRoutes(apiV1)
	}
	if a.authHandler != nil {
		a.authHandler.RegisterPublicRoutes(apiV1)

		authedV1 := apiV1.Group("")
		authedV1.Use(auth.RequireAuth(a.jwt))
		a.authHandler.RegisterRoutes(authedV1)
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
		path := c.Request.URL.Path
		if path == "/api" || strings.HasPrefix(path, "/api/") {
			_ = c.Error(apperr.NotFound(apperr.CodeNotFound))
			return
		}
		spaHandler.ServeHTTP(c.Writer, c.Request)
	})
}
