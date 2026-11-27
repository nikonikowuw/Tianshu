package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
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
	svc := newServiceWithStore(st)

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

	assertErrorEnvelope := func(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
		t.Helper()
		if rec.Code != wantStatus {
			t.Fatalf("status = %d, want %d (body=%s)", rec.Code, wantStatus, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatal("missing X-Request-Id header")
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("unmarshal error body: %v (body=%s)", err, rec.Body.String())
		}
		if _, hasReqID := raw["request_id"]; hasReqID {
			t.Fatalf("error body must not include request_id: %s", rec.Body.String())
		}
		if raw["code"] != wantCode {
			t.Fatalf("code = %v, want %s", raw["code"], wantCode)
		}
		msg, _ := raw["message"].(string)
		if msg == "" || msg == wantCode {
			t.Fatalf("expected translated message, got %q", msg)
		}
		return raw
	}

	// 1. Missing token -> 401 with standard error body and X-Request-Id header only.
	rec401 := httptest.NewRecorder()
	engine.ServeHTTP(rec401, httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil))
	raw401 := assertErrorEnvelope(t, rec401, http.StatusUnauthorized, apperr.CodeUnauthorized)
	if _, hasErrors := raw401["errors"]; hasErrors {
		t.Fatalf("401 body must not include errors: %v", raw401)
	}

	// 2. Missing permission -> 403 with standard error body.
	req403 := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	req403.Header.Set("X-Test-User", "forbidden")
	rec403 := httptest.NewRecorder()
	engine.ServeHTTP(rec403, req403)
	raw403 := assertErrorEnvelope(t, rec403, http.StatusForbidden, apperr.CodeForbidden)
	if _, hasErrors := raw403["errors"]; hasErrors {
		t.Fatalf("403 body must not include errors: %v", raw403)
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

	// 4. Empty numeric query params fall back cleanly to pagination defaults.
	for _, query := range []string{"page=&pageSize=", "page=%20&pageSize=%20"} {
		reqFallback := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?"+query, nil)
		reqFallback.Header.Set("X-Test-User", "allowed")
		recFallback := httptest.NewRecorder()
		engine.ServeHTTP(recFallback, reqFallback)
		if recFallback.Code != http.StatusOK {
			t.Fatalf("empty numeric query %q status = %d, want 200 (body=%s)", query, recFallback.Code, recFallback.Body.String())
		}
		var fallbackBody struct {
			Page     int `json:"page"`
			PageSize int `json:"pageSize"`
		}
		if err := json.Unmarshal(recFallback.Body.Bytes(), &fallbackBody); err != nil {
			t.Fatalf("unmarshal fallback body: %v", err)
		}
		if fallbackBody.Page != 1 || fallbackBody.PageSize != 20 {
			t.Fatalf("fallback pagination = %+v, want page=1 pageSize=20", fallbackBody)
		}
	}

	// 5. Empty result serializes items as [] (not null).
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

	// 6. Write methods must NOT be exposed (audit logs are strictly read-only).
	for _, method := range []string{http.MethodPost, http.MethodPatch, http.MethodDelete} {
		reqWrite := httptest.NewRequest(method, "/api/v1/audit-logs", nil)
		reqWrite.Header.Set("X-Test-User", "allowed")
		recWrite := httptest.NewRecorder()
		engine.ServeHTTP(recWrite, reqWrite)
		if recWrite.Code != http.StatusNotFound && recWrite.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s /api/v1/audit-logs status = %d, want 404 or 405", method, recWrite.Code)
		}
	}

	// 7. Invalid query parameters return 422 VALIDATION_FAILED with translated zh-CN / en-US messages.
	tooManyActions := make([]string, 0, maxMultiFilterItems+1)
	for i := range maxMultiFilterItems + 1 {
		tooManyActions = append(tooManyActions, fmt.Sprintf("act.%d", i))
	}
	for _, tc := range []struct {
		name      string
		query     string
		wantField string
		rawKey    string
	}{
		{"invalid page number", "page=abc", "page", ErrCodeInvalidNumber},
		{"page below min", "page=0", "page", ErrCodeInvalidPage},
		{"pageSize out of range", "pageSize=5", "pageSize", ErrCodeInvalidPageSize},
		{"pageSize above max", "pageSize=101", "pageSize", ErrCodeInvalidPageSize},
		{"query too long", "q=" + strings.Repeat("a", 101), "q", ErrCodeQueryTooLong},
		{"actor too long", "actor=" + strings.Repeat("a", 101), "actor", ErrCodeActorTooLong},
		{"invalid actorId format", "actorId=not-a-uuid", "actorId", "VALIDATION_UUID"},
		{"nil actorId uuid", "actorId=" + uuid.Nil.String(), "actorId", "VALIDATION_UUID"},
		{"too many actions", "action=" + strings.Join(tooManyActions, ","), "action", ErrCodeInvalidAction},
		{"invalid status", "status=unknown", "status", ErrCodeInvalidStatus},
		{"invalid from timestamp", "from=not-a-timestamp", "from", ErrCodeInvalidTimeFormat},
		{"inverted time range", "from=2026-11-26T12:00:00Z&to=2026-11-26T11:00:00Z", "from", ErrCodeInvalidTimeRange},
		{"invalid sort field", "sort=password", "sort", "VALIDATION_ONE_OF"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runLocale := func(lang string) (string, string) {
				reqBad := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs?"+tc.query, nil)
				reqBad.Header.Set("X-Test-User", "allowed")
				reqBad.Header.Set("Accept-Language", lang)
				recBad := httptest.NewRecorder()
				engine.ServeHTTP(recBad, reqBad)
				assertErrorEnvelope(t, recBad, http.StatusUnprocessableEntity, apperr.CodeValidationFailed)

				var errBody struct {
					Code    string              `json:"code"`
					Message string              `json:"message"`
					Errors  []apperr.FieldError `json:"errors"`
				}
				if err := json.Unmarshal(recBad.Body.Bytes(), &errBody); err != nil {
					t.Fatalf("unmarshal 422 body: %v", err)
				}
				if len(errBody.Errors) == 0 || errBody.Errors[0].Field != tc.wantField {
					t.Fatalf("errors = %+v, want field %q", errBody.Errors, tc.wantField)
				}
				fieldMsg := errBody.Errors[0].Message
				if fieldMsg == "" || fieldMsg == tc.rawKey {
					t.Fatalf("lang=%s field message not translated: %q", lang, fieldMsg)
				}
				return errBody.Message, fieldMsg
			}

			enTop, enField := runLocale("en-US")
			zhTop, zhField := runLocale("zh-CN")
			if enTop == zhTop || enField == zhField {
				t.Fatalf("expected distinct en-US and zh-CN translations: top=(%q, %q) field=(%q, %q)", enTop, zhTop, enField, zhField)
			}
		})
	}

	// 8. Single-pass validation collects and sorts all field errors together.
	reqMulti := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/audit-logs?page=abc&status=unknown&actorId="+uuid.Nil.String(),
		nil,
	)
	reqMulti.Header.Set("X-Test-User", "allowed")
	recMulti := httptest.NewRecorder()
	engine.ServeHTTP(recMulti, reqMulti)
	assertErrorEnvelope(t, recMulti, http.StatusUnprocessableEntity, apperr.CodeValidationFailed)
	var multiBody struct {
		Errors []apperr.FieldError `json:"errors"`
	}
	if err := json.Unmarshal(recMulti.Body.Bytes(), &multiBody); err != nil {
		t.Fatalf("unmarshal multi-error body: %v", err)
	}
	gotFields := make([]string, 0, len(multiBody.Errors))
	for _, fe := range multiBody.Errors {
		gotFields = append(gotFields, fe.Field)
	}
	if !slices.Equal(gotFields, []string{"actorId", "page", "status"}) {
		t.Fatalf("multi-field errors = %v, want [actorId page status]", gotFields)
	}

	// 9. Store failure maps to 500 INTERNAL_ERROR without leaking internal details.
	st.countErr = errors.New("pq: relation audit_logs locked")
	t.Cleanup(func() { st.countErr = nil })
	req500 := httptest.NewRequest(http.MethodGet, "/api/v1/audit-logs", nil)
	req500.Header.Set("X-Test-User", "allowed")
	rec500 := httptest.NewRecorder()
	engine.ServeHTTP(rec500, req500)
	assertErrorEnvelope(t, rec500, http.StatusInternalServerError, apperr.CodeInternal)
	if strings.Contains(rec500.Body.String(), "relation audit_logs") {
		t.Fatalf("500 response leaked internal store error: %s", rec500.Body.String())
	}
}
