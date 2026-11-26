package audit

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/shared/apperr"
	"tianshu/internal/shared/authctx"
)

func TestAuditHTTPRoutesContract(t *testing.T) {
	st := &fakeStore{}
	svc := newServiceWithStore(st, logger.New("test", "error", io.Discard))

	actorID := uuid.Must(uuid.NewV7())
	st.entries = []Entry{
		{
			ID:            uuid.Must(uuid.NewV7()),
			ActorID:       &actorID,
			ActorUsername: "admin",
			Action:        "auth.login",
			Resource:      "session",
			ResourceID:    "sess-1",
			Status:        StatusSuccess,
			IPAddress:     "127.0.0.1",
			UserAgent:     "Chrome",
			RequestID:     "req_12345678",
			Metadata:      map[string]any{"method": "password"},
			CreatedAt:     time.Date(2026, 11, 26, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:            uuid.Must(uuid.NewV7()),
			ActorUsername: "intruder",
			Action:        "auth.login",
			Resource:      "session",
			Status:        StatusFailure,
			IPAddress:     "198.51.100.2",
			UserAgent:     "curl",
			RequestID:     "req_87654321",
			Metadata:      map[string]any{},
			CreatedAt:     time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC),
		},
	}

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("audit", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(audit) error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	allowedUser := uuid.Must(uuid.NewV7())
	forbiddenUser := uuid.Must(uuid.NewV7())

	mockAuth := func(c *gin.Context) {
		switch c.GetHeader("X-Test-User") {
		case "allowed":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
				UserID: allowedUser,
			}))
		case "forbidden":
			c.Request = c.Request.WithContext(authctx.WithIdentity(c.Request.Context(), authctx.Identity{
				UserID: forbiddenUser,
			}))
		default:
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		c.Next()
	}

	requireAuditRead := func(c *gin.Context) {
		id, ok := authctx.IdentityFromContext(c.Request.Context())
		if !ok {
			_ = c.Error(apperr.Unauthorized(apperr.CodeUnauthorized))
			c.Abort()
			return
		}
		if id.UserID != allowedUser {
			_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
			c.Abort()
			return
		}
		c.Next()
	}

	authed := engine.Group("/api/v1")
	authed.Use(mockAuth)
	NewHandler(svc).RegisterRoutes(authed, requireAuditRead)

	// 1. Missing token -> 401.
	rec401 := httptest.NewRecorder()
	engine.ServeHTTP(rec401, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil))
	if rec401.Code != http.StatusUnauthorized {
		t.Fatalf("anon status = %d, want 401", rec401.Code)
	}

	// 2. Missing permission -> 403.
	req403 := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	req403.Header.Set("X-Test-User", "forbidden")
	rec403 := httptest.NewRecorder()
	engine.ServeHTTP(rec403, req403)
	if rec403.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d, want 403", rec403.Code)
	}

	// 3. Valid paginated list without envelope and with X-Request-Id header.
	reqOK := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?status=failure&page=1&pageSize=10", nil)
	reqOK.Header.Set("X-Test-User", "allowed")
	recOK := httptest.NewRecorder()
	engine.ServeHTTP(recOK, reqOK)
	if recOK.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200 (body=%s)", recOK.Code, recOK.Body.String())
	}
	if recOK.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id header")
	}

	var body struct {
		Items    []auditLogResponse `json:"items"`
		Total    int64              `json:"total"`
		Page     int                `json:"page"`
		PageSize int                `json:"pageSize"`
	}
	if err := json.Unmarshal(recOK.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal list response: %v", err)
	}
	if body.Total != 1 || len(body.Items) != 1 || body.Page != 1 || body.PageSize != 10 {
		t.Fatalf("unexpected paginated body: %+v", body)
	}
	if body.Items[0].ActorUsername != "intruder" || body.Items[0].ActorID != nil {
		t.Fatalf("unexpected audit item: %+v", body.Items[0])
	}

	// 4. Empty result serializes items as [] (not null).
	reqEmpty := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?q=nonexistent", nil)
	reqEmpty.Header.Set("X-Test-User", "allowed")
	recEmpty := httptest.NewRecorder()
	engine.ServeHTTP(recEmpty, reqEmpty)
	if recEmpty.Code != http.StatusOK {
		t.Fatalf("empty list status = %d, want 200", recEmpty.Code)
	}
	if !strings.Contains(recEmpty.Body.String(), `"items":[]`) {
		t.Fatalf("expected items:[], got %s", recEmpty.Body.String())
	}

	// 5. Invalid query parameters return 422 VALIDATION_FAILED with translated field messages.
	for _, tc := range []struct {
		name      string
		query     string
		wantField string
	}{
		{"invalid page number", "page=abc", "page"},
		{"pageSize out of range", "pageSize=5", "pageSize"},
		{"invalid status", "status=unknown", "status"},
		{"invalid from timestamp", "from=not-a-timestamp", "from"},
		{"inverted time range", "from=2026-11-26T12:00:00Z&to=2026-11-26T11:00:00Z", "from"},
		{"invalid sort field", "sort=password", "sort"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?"+tc.query, nil)
			reqBad.Header.Set("X-Test-User", "allowed")
			reqBad.Header.Set("Accept-Language", "en-US")
			recBad := httptest.NewRecorder()
			engine.ServeHTTP(recBad, reqBad)
			if recBad.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body=%s)", recBad.Code, recBad.Body.String())
			}
			var errBody struct {
				Code   string              `json:"code"`
				Errors []apperr.FieldError `json:"errors"`
			}
			if err := json.Unmarshal(recBad.Body.Bytes(), &errBody); err != nil {
				t.Fatalf("unmarshal 422 body: %v", err)
			}
			if errBody.Code != apperr.CodeValidationFailed {
				t.Fatalf("code = %q, want %s", errBody.Code, apperr.CodeValidationFailed)
			}
			if len(errBody.Errors) == 0 || errBody.Errors[0].Field != tc.wantField {
				t.Fatalf("errors = %+v, want field %q", errBody.Errors, tc.wantField)
			}
		})
	}
}
