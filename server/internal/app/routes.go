package app

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"

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
