package dashboard

import (
	"encoding/json"
	"errors"
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

func TestDashboardHTTPRoutesContract(t *testing.T) {
	fixedNow := time.Date(2026, 11, 27, 8, 0, 0, 0, time.UTC)
	st := &fakeStore{
		userStats:  UserStats{Total: 8, Active: 7, Disabled: 1, NewLast7Days: 2},
		roleStats:  RoleStats{Total: 3, System: 1, Custom: 2},
		fileStats:  FileStats{Total: 5, TotalSizeBytes: 102400, Images: 3, Documents: 2},
		activeSess: 4,
		trend: []DailyActivityPoint{
			{Date: "2026-11-27", Logins: 2, Operations: 3, Failures: 1},
		},
	}
	svc := newServiceWithStore(st)
	svc.now = func() time.Time { return fixedNow }

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("dashboard", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS(dashboard) error = %v", err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
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

	mockRequireRead := func(c *gin.Context) {
		identity, ok := authctx.IdentityFromContext(c.Request.Context())
		if !ok || identity.UserID != allowedUser {
			_ = c.Error(apperr.Forbidden(apperr.CodeForbidden))
			c.Abort()
			return
		}
		c.Next()
	}

	authed := engine.Group("/api/v1")
	authed.Use(mockAuth)
	NewHandler(svc).RegisterRoutes(authed, mockRequireRead)

	assertErrorBody := func(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode, lang string) map[string]any {
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
			t.Fatalf("error body must not duplicate request_id: %s", rec.Body.String())
		}
		if raw["code"] != wantCode {
			t.Fatalf("code = %v, want %s", raw["code"], wantCode)
		}
		msg, _ := raw["message"].(string)
		wantMsg := bundle.Translate(lang, wantCode, nil)
		if msg == "" || msg == wantCode || msg != wantMsg {
			t.Fatalf("lang=%s message = %q, want %q", lang, msg, wantMsg)
		}
		if wantStatus != http.StatusUnprocessableEntity {
			if _, hasErrors := raw["errors"]; hasErrors {
				t.Fatalf("non-422 error body must not include errors: %v", raw)
			}
		}
		return raw
	}

	t.Run("rejects unauthenticated with 401 and unauthorized with 403", func(t *testing.T) {
		for _, lang := range []string{"en-US", "zh-CN"} {
			req401 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
			req401.Header.Set("Accept-Language", lang)
			rec401 := httptest.NewRecorder()
			engine.ServeHTTP(rec401, req401)
			assertErrorBody(t, rec401, http.StatusUnauthorized, apperr.CodeUnauthorized, lang)

			req403 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
			req403.Header.Set("X-Test-User", "forbidden")
			req403.Header.Set("Accept-Language", lang)
			rec403 := httptest.NewRecorder()
			engine.ServeHTTP(rec403, req403)
			assertErrorBody(t, rec403, http.StatusForbidden, apperr.CodeForbidden, lang)
		}
	})

	t.Run("returns bare summary DTO with empty recentActivity as []", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
		req.Header.Set("X-Test-User", "allowed")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-Request-Id") == "" {
			t.Fatal("missing X-Request-Id header")
		}
		if strings.Contains(rec.Body.String(), `"recentActivity":null`) {
			t.Fatalf("recentActivity must serialize as [], got %s", rec.Body.String())
		}

		var body struct {
			GeneratedAt    string                  `json:"generatedAt"`
			RangeDays      int                     `json:"rangeDays"`
			Users          userStatsResponse       `json:"users"`
			Roles          roleStatsResponse       `json:"roles"`
			Files          fileStatsResponse       `json:"files"`
			Sessions       sessionStatsResponse    `json:"sessions"`
			Audit          auditStatsResponse      `json:"audit"`
			ActivityTrend  []dailyActivityResponse `json:"activityTrend"`
			RecentActivity []any                   `json:"recentActivity"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if body.RangeDays != 7 || len(body.ActivityTrend) != 7 {
			t.Fatalf("rangeDays=%d len(trend)=%d, want 7/7", body.RangeDays, len(body.ActivityTrend))
		}
		if body.Users.Total != 8 || body.Roles.Total != 3 || body.Files.TotalSizeBytes != 102400 || body.Sessions.Active != 4 {
			t.Fatalf("unexpected KPI payload: %+v", body)
		}
		if body.Audit.TotalInRange != 6 || body.Audit.LoginsInRange != 2 || body.Audit.FailuresInRange != 1 {
			t.Fatalf("unexpected audit stats: %+v", body.Audit)
		}
		if len(body.RecentActivity) != 0 {
			t.Fatalf("len(recentActivity) = %d, want 0", len(body.RecentActivity))
		}
	})

	t.Run("accepts empty days default, days=14 and days=30, and localizes 422 validation errors including days=0", func(t *testing.T) {
		reqEmpty := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary?days=", nil)
		reqEmpty.Header.Set("X-Test-User", "allowed")
		recEmpty := httptest.NewRecorder()
		engine.ServeHTTP(recEmpty, reqEmpty)
		if recEmpty.Code != http.StatusOK {
			t.Fatalf("days= empty status = %d, want 200; body=%s", recEmpty.Code, recEmpty.Body.String())
		}
		var emptyBody struct {
			RangeDays int `json:"rangeDays"`
		}
		if err := json.Unmarshal(recEmpty.Body.Bytes(), &emptyBody); err != nil || emptyBody.RangeDays != 7 {
			t.Fatalf("days= empty rangeDays = %d (err=%v), want 7", emptyBody.RangeDays, err)
		}

		for _, days := range []struct {
			query string
			want  int
		}{
			{query: "14", want: 14},
			{query: "30", want: 30},
		} {
			reqRange := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary?days="+days.query, nil)
			reqRange.Header.Set("X-Test-User", "allowed")
			recRange := httptest.NewRecorder()
			engine.ServeHTTP(recRange, reqRange)
			if recRange.Code != http.StatusOK {
				t.Fatalf("days=%s status = %d, want 200", days.query, recRange.Code)
			}
			if recRange.Header().Get("X-Request-Id") == "" {
				t.Fatalf("missing X-Request-Id for days=%s", days.query)
			}
			var rangeBody struct {
				RangeDays     int                     `json:"rangeDays"`
				ActivityTrend []dailyActivityResponse `json:"activityTrend"`
			}
			if err := json.Unmarshal(recRange.Body.Bytes(), &rangeBody); err != nil {
				t.Fatalf("unmarshal days=%s: %v", days.query, err)
			}
			if rangeBody.RangeDays != days.want || len(rangeBody.ActivityTrend) != days.want {
				t.Fatalf("days=%s got rangeDays=%d len(trend)=%d, want %d", days.query, rangeBody.RangeDays, len(rangeBody.ActivityTrend), days.want)
			}
		}

		for _, tc := range []struct {
			query string
			lang  string
		}{
			{query: "?days=0", lang: "en-US"},
			{query: "?days=0", lang: "zh-CN"},
			{query: "?days=abc", lang: "en-US"},
			{query: "?days=10", lang: "zh-CN"},
		} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary"+tc.query, nil)
			req.Header.Set("X-Test-User", "allowed")
			req.Header.Set("Accept-Language", tc.lang)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			assertErrorBody(t, rec, http.StatusUnprocessableEntity, apperr.CodeValidationFailed, tc.lang)
			var errResp struct {
				Code   string `json:"code"`
				Errors []struct {
					Field   string `json:"field"`
					Message string `json:"message"`
				} `json:"errors"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
				t.Fatalf("unmarshal 422: %v", err)
			}
			if len(errResp.Errors) != 1 || errResp.Errors[0].Field != "days" {
				t.Fatalf("unexpected 422 errors payload: %+v", errResp.Errors)
			}
			wantMsg := bundle.Translate(tc.lang, ErrCodeInvalidDays, nil)
			if errResp.Errors[0].Message != wantMsg {
				t.Fatalf("lang=%s field message = %q, want %q", tc.lang, errResp.Errors[0].Message, wantMsg)
			}
		}
	})

	t.Run("serializes non-empty recentActivity DTO with RFC3339 UTC timestamp and redacted fields", func(t *testing.T) {
		eventID := uuid.Must(uuid.NewV7())
		eventTime := time.Date(2026, 11, 27, 7, 45, 0, 0, time.UTC)
		st.recent = []RecentActivityItem{
			{
				ID:            eventID,
				ActorUsername: "alice",
				Action:        "role.update",
				Resource:      "role",
				ResourceID:    "role-42",
				Status:        "success",
				IPAddress:     "10.0.0.5",
				CreatedAt:     eventTime,
			},
		}
		t.Cleanup(func() { st.recent = nil })

		req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
		req.Header.Set("X-Test-User", "allowed")
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		raw := rec.Body.String()
		if strings.Contains(raw, `"metadata"`) || strings.Contains(raw, `"userAgent"`) {
			t.Fatalf("recentActivity must not leak sensitive audit fields: %s", raw)
		}

		var body struct {
			RecentActivity []recentActivityResponse `json:"recentActivity"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal recentActivity: %v", err)
		}
		if len(body.RecentActivity) != 1 {
			t.Fatalf("len(recentActivity) = %d, want 1", len(body.RecentActivity))
		}
		got := body.RecentActivity[0]
		if got.ID != eventID.String() || got.ActorUsername != "alice" || got.Action != "role.update" ||
			got.Resource != "role" || got.ResourceID != "role-42" || got.Status != "success" ||
			got.IPAddress != "10.0.0.5" || got.CreatedAt != "2026-11-27T07:45:00Z" {
			t.Fatalf("unexpected recentActivity DTO: %+v", got)
		}
	})

	t.Run("masks internal store errors behind 500 INTERNAL_ERROR without leaking cause", func(t *testing.T) {
		st.userErr = errors.New("sensitive postgres credential detail")
		t.Cleanup(func() { st.userErr = nil })

		for _, lang := range []string{"en-US", "zh-CN"} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
			req.Header.Set("X-Test-User", "allowed")
			req.Header.Set("Accept-Language", lang)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if strings.Contains(rec.Body.String(), "sensitive postgres credential detail") {
				t.Fatalf("500 response leaked internal error cause: %s", rec.Body.String())
			}
			assertErrorBody(t, rec, http.StatusInternalServerError, apperr.CodeInternal, lang)
		}
	})
}
