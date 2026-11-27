package access

import (
	"context"
	"errors"
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
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

type failingPermChecker struct{}

func (failingPermChecker) HasPermission(_ context.Context, _ uuid.UUID, _ string) (bool, error) {
	return false, apperr.Internal(errors.New("boom"))
}

func TestRequirePermissionMiddleware(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		Log:   logger.New("test", "error", io.Discard),
	})
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

func TestRequireAnyPermissionMiddleware(t *testing.T) {
	s := newFakeStore()
	mem := cache.NewMemory()
	defer func() { _ = mem.Close() }()

	svc := newServiceWithStore(s, ServiceDeps{
		Tx:    fakeTxRunner{},
		Cache: mem,
		Log:   logger.New("test", "error", io.Discard),
	})
	if err := svc.SyncCatalog(t.Context()); err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}

	usersReaderRole, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "users_reader",
		Name:        "Users Reader",
		Permissions: []string{PermUsersRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(users_reader) error = %v", err)
	}
	rolesReaderRole, err := svc.CreateRole(t.Context(), CreateRoleInput{
		Code:        "roles_reader",
		Name:        "Roles Reader",
		Permissions: []string{PermRolesRead},
	})
	if err != nil {
		t.Fatalf("CreateRole(roles_reader) error = %v", err)
	}

	usersReaderID := uuid.Must(uuid.NewV7())
	rolesReaderID := uuid.Must(uuid.NewV7())
	noPermID := uuid.Must(uuid.NewV7())
	if err := svc.AssignRoleByCode(t.Context(), usersReaderID, usersReaderRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode(usersReader) error = %v", err)
	}
	if err := svc.AssignRoleByCode(t.Context(), rolesReaderID, rolesReaderRole.Code); err != nil {
		t.Fatalf("AssignRoleByCode(rolesReader) error = %v", err)
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
				c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
					UserID:   userID,
					Username: "tester",
				}))
			}
			c.Next()
		}
	}

	guard := RequireAnyPermission(svc, PermUsersRead, PermRolesRead)
	engine.GET("/anon", guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/none", attachUser(noPermID), guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/first", attachUser(usersReaderID), guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/second", attachUser(rolesReaderID), guard, func(c *gin.Context) { c.Status(http.StatusOK) })
	engine.GET("/err", attachUser(usersReaderID), RequireAnyPermission(failingPermChecker{}, PermUsersRead), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	tests := []struct {
		name       string
		path       string
		wantStatus int
	}{
		{"missing identity returns 401", "/anon", http.StatusUnauthorized},
		{"holding neither permission returns 403", "/none", http.StatusForbidden},
		{"holding first permission returns 200", "/first", http.StatusOK},
		{"holding second permission returns 200", "/second", http.StatusOK},
		{"checker failure returns 500", "/err", http.StatusInternalServerError},
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
