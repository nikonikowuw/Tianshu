package identity

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

func TestIdentityHTTPRoutesContract(t *testing.T) {
	svc, _, roles, _, _, superID, mgrID := setupTestService()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("identity", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(identity) error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	var currentActor uuid.UUID
	api := engine.Group("/api/v1")
	api.Use(func(c *gin.Context) {
		if currentActor == uuid.Nil {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
			UserID:    currentActor,
			SessionID: uuid.New(),
		}))
		c.Next()
	})

	handler := NewHandler(svc, bundle)
	allow := func(c *gin.Context) { c.Next() }
	handler.RegisterRoutes(api, RoutePermissions{
		RequireRead:   allow,
		RequireCreate: allow,
		RequireUpdate: allow,
		RequireDelete: allow,
	})

	doReq := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf io.Reader
		if body != nil {
			raw, _ := json.Marshal(body)
			buf = bytes.NewReader(raw)
		}
		req := httptest.NewRequest(method, path, buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept-Language", "en-US")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	currentActor = uuid.Nil
	if rec := doReq(http.MethodGet, "/api/v1/users", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated GET /users = %d, want 401", rec.Code)
	}

	currentActor = superID
	t.Run("GET /users returns paginated envelope and X-Request-Id", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/users?page=1&pageSize=10", nil)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /users = %d, body = %s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatalf("missing X-Request-Id header")
		}
		var body struct {
			Items []struct {
				ID    string `json:"id"`
				Roles []any  `json:"roles"`
			} `json:"items"`
			Total int64 `json:"total"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode /users: %v", err)
		}
		if body.Total < 2 || len(body.Items) < 2 {
			t.Fatalf("expected >= 2 users, got %+v", body)
		}
	})

	t.Run("GET /users rejects invalid status and roleId with 422", func(t *testing.T) {
		rec := doReq(http.MethodGet, "/api/v1/users?status=bad", nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid status = %d, want 422", rec.Code)
		}
		rec = doReq(http.MethodGet, "/api/v1/users?roleId=not-a-uuid", nil)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("invalid roleId = %d, want 422", rec.Code)
		}
	})

	t.Run("POST /users creates 201 and empty roles serialize as []", func(t *testing.T) {
		rec := doReq(http.MethodPost, "/api/v1/users", map[string]any{
			"username": "alice",
			"email":    "alice@example.com",
			"password": "password123",
			"roleIds":  []string{},
		})
		if rec.Code != http.StatusCreated {
			t.Fatalf("POST /users = %d, body = %s", rec.Code, rec.Body.String())
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte(`"roles":[]`)) {
			t.Fatalf("expected empty roles array [], got %s", rec.Body.String())
		}

		// Zero UUID in roleIds must be rejected with 422, never silently ignored.
		rec = doReq(http.MethodPost, "/api/v1/users", map[string]any{
			"username": "badrole",
			"email":    "badrole@example.com",
			"password": "password123",
			"roleIds":  []string{uuid.Nil.String()},
		})
		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("POST /users with nil roleId = %d, want 422", rec.Code)
		}
	})

	t.Run("PATCH /users/:id and DELETE /users/:id", func(t *testing.T) {
		rec := doReq(http.MethodPatch, "/api/v1/users/"+mgrID.String(), map[string]any{
			"displayName": "Updated Manager",
			"roleIds":     []string{roles.roleID("viewer").String()},
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("PATCH /users = %d, body = %s", rec.Code, rec.Body.String())
		}

		rec = doReq(http.MethodDelete, "/api/v1/users/"+mgrID.String(), nil)
		if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
			t.Fatalf("DELETE /users = %d (len %d), want 204 empty", rec.Code, rec.Body.Len())
		}

		rec = doReq(http.MethodGet, "/api/v1/users/"+mgrID.String(), nil)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET deleted user = %d, want 404", rec.Code)
		}
	})
}
