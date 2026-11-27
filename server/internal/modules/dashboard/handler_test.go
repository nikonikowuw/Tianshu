package dashboard

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

	t.Run("rejects unauthenticated with 401 and unauthorized with 403", func(t *testing.T) {
		req401 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
		rec401 := httptest.NewRecorder()
		engine.ServeHTTP(rec401, req401)
		if rec401.Code != http.StatusUnauthorized {
			t.Fatalf("401 status = %d, want 401", rec401.Code)
		}
		if rec401.Header().Get("X-Request-Id") == "" {
			t.Fatal("missing X-Request-Id on 401")
		}

		req403 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary", nil)
		req403.Header.Set("X-Test-User", "forbidden")
		rec403 := httptest.NewRecorder()
		engine.ServeHTTP(rec403, req403)
		if rec403.Code != http.StatusForbidden {
			t.Fatalf("403 status = %d, want 403", rec403.Code)
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

	t.Run("accepts days=14 and localizes 422 validation errors on invalid days", func(t *testing.T) {
		req14 := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary?days=14", nil)
		req14.Header.Set("X-Test-User", "allowed")
		rec14 := httptest.NewRecorder()
		engine.ServeHTTP(rec14, req14)
		if rec14.Code != http.StatusOK {
			t.Fatalf("days=14 status = %d, want 200", rec14.Code)
		}

		for _, tc := range []struct {
			query string
			lang  string
		}{
			{query: "?days=abc", lang: "en-US"},
			{query: "?days=10", lang: "zh-CN"},
		} {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard/summary"+tc.query, nil)
			req.Header.Set("X-Test-User", "allowed")
			req.Header.Set("Accept-Language", tc.lang)
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("query=%s status = %d, want 422; body=%s", tc.query, rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), `"request_id"`) {
				t.Fatalf("error body must not duplicate request_id: %s", rec.Body.String())
			}
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
			if errResp.Code != apperr.CodeValidationFailed || len(errResp.Errors) != 1 || errResp.Errors[0].Field != "days" {
				t.Fatalf("unexpected 422 payload: %+v", errResp)
			}
			wantMsg := bundle.Translate(tc.lang, ErrCodeInvalidDays, nil)
			if errResp.Errors[0].Message != wantMsg {
				t.Fatalf("lang=%s message = %q, want %q", tc.lang, errResp.Errors[0].Message, wantMsg)
			}
		}
	})
}
