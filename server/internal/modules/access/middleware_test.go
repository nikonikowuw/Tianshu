package access

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/cache"
	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/authctx"
)

func TestRequirePermissionMiddleware(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, fakeTxRunner{}, mem, logger.New("test", "error", io.Discard))
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	adminID := uuid.Must(uuid.NewV7())
	viewerID := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), adminID, RoleCodeSuperAdmin); err != nil {
		t.Fatalf("AssignRoleByCode() error = %v", err)
	}

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	attachUser := func(userID uuid.UUID) gin.HandlerFunc {
		return func(c *gin.Context) {
			if userID != uuid.Nil {
				ctx := authctx.WithIdentity(c.Request.Context(), authctx.Identity{
					UserID:   userID,
					Username: "tester",
				})
				c.Request = c.Request.WithContext(ctx)
			}
			c.Next()
		}
	}

	engine.GET("/anon", RequirePermission(svc, PermUsersDelete), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET("/forbidden", attachUser(viewerID), RequirePermission(svc, PermUsersDelete), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	engine.GET("/allowed", attachUser(adminID), RequirePermission(svc, PermUsersDelete), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"missing identity returns 401", "/anon", http.StatusUnauthorized},
		{"missing permission returns 403", "/forbidden", http.StatusForbidden},
		{"granted permission returns 200", "/allowed", http.StatusOK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("GET %s status = %d, want %d (body=%s)", tc.path, rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
