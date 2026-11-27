package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
)

func TestAuthHTTPRoutesContract(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("auth", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	apiV1 := engine.Group("/api/v1")
	h := NewHandler(svc, []string{"https://admin.example.com"})
	h.RegisterPublicRoutes(apiV1)
	authed := apiV1.Group("")
	authed.Use(RequireAuth(svc.JWTManager()))
	h.RegisterRoutes(authed)

	// 1. Login returns 200 with accessToken and HttpOnly refresh cookie.
	reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{
		"identifier": "admin",
		"password": "correct-password-123"
	}`))
	reqLogin.Header.Set("Content-Type", "application/json")
	reqLogin.RemoteAddr = "127.0.0.1:20001"
	recLogin := httptest.NewRecorder()
	engine.ServeHTTP(recLogin, reqLogin)
	if recLogin.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200 (body=%s)", recLogin.Code, recLogin.Body.String())
	}

	var loginBody struct {
		AccessToken string   `json:"accessToken"`
		Roles       []string `json:"roles"`
		Permissions []string `json:"permissions"`
	}
	if err := json.Unmarshal(recLogin.Body.Bytes(), &loginBody); err != nil {
		t.Fatalf("unmarshal login response: %v", err)
	}
	if loginBody.AccessToken == "" || len(loginBody.Permissions) == 0 {
		t.Fatalf("unexpected login response: %+v", loginBody)
	}

	var refreshCookie *http.Cookie
	for _, ck := range recLogin.Result().Cookies() {
		if ck.Name == refreshCookieName {
			refreshCookie = ck
		}
	}
	if refreshCookie == nil || !refreshCookie.HttpOnly || refreshCookie.Path != refreshCookiePath {
		t.Fatalf("invalid refresh cookie: %+v", refreshCookie)
	}

	// 2. GET /auth/me without Bearer token returns 401; with Bearer token returns 200.
	recAnon := httptest.NewRecorder()
	engine.ServeHTTP(recAnon, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	if recAnon.Code != http.StatusUnauthorized {
		t.Fatalf("anon /auth/me status = %d, want 401", recAnon.Code)
	}

	reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "Bearer "+loginBody.AccessToken)
	recMe := httptest.NewRecorder()
	engine.ServeHTTP(recMe, reqMe)
	if recMe.Code != http.StatusOK {
		t.Fatalf("authed /auth/me status = %d, want 200 (body=%s)", recMe.Code, recMe.Body.String())
	}

	// 3. POST /auth/refresh from foreign Origin, Origin: null, or missing Origin/Referer with cookie is rejected with 403.
	for _, tc := range []struct {
		name   string
		origin string
	}{
		{"foreign origin", "https://evil.example.net"},
		{"null origin", "null"},
		{"missing origin and referer with cookie", ""},
	} {
		reqBad := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		reqBad.Host = "tianshu.local"
		if tc.origin != "" {
			reqBad.Header.Set("Origin", tc.origin)
		}
		reqBad.AddCookie(refreshCookie)
		recBad := httptest.NewRecorder()
		engine.ServeHTTP(recBad, reqBad)
		if recBad.Code != http.StatusForbidden {
			t.Fatalf("%s refresh status = %d, want 403", tc.name, recBad.Code)
		}
	}

	// 4. POST /auth/refresh from same-origin succeeds and rotates cookie.
	reqRefresh := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqRefresh.Host = "tianshu.local"
	reqRefresh.Header.Set("Origin", "https://tianshu.local")
	reqRefresh.AddCookie(refreshCookie)
	recRefresh := httptest.NewRecorder()
	engine.ServeHTTP(recRefresh, reqRefresh)
	if recRefresh.Code != http.StatusOK {
		t.Fatalf("same-origin refresh status = %d, want 200 (body=%s)", recRefresh.Code, recRefresh.Body.String())
	}
	for _, ck := range recRefresh.Result().Cookies() {
		if ck.Name == refreshCookieName {
			refreshCookie = ck
		}
	}

	// 5. POST /auth/logout with same-origin returns 204 and clears the refresh cookie.
	reqLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqLogout.Host = "tianshu.local"
	reqLogout.Header.Set("Origin", "https://tianshu.local")
	reqLogout.Header.Set("Authorization", "Bearer "+loginBody.AccessToken)
	reqLogout.AddCookie(refreshCookie)
	recLogout := httptest.NewRecorder()
	engine.ServeHTTP(recLogout, reqLogout)
	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", recLogout.Code)
	}
}

// newLoginOnlyTestEngine mounts the login handler without the per-route IP
// limiter, so header-level contracts of the service-level soft cooldown (such as
// Retry-After) can be asserted in isolation.
func newLoginOnlyTestEngine(t *testing.T, svc *Service) *gin.Engine {
	t.Helper()

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("auth", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}

	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	h := NewHandler(svc, nil)
	engine.POST("/api/v1/auth/login", h.login)
	return engine
}

// TestAuthLoginSoftCooldownSetsRetryAfterHeader guards the header contract the
// web client relies on to drive its 429 countdown; losing the header would
// silently degrade the client to a one-second lock while the server still cools
// down for minutes.
func TestAuthLoginSoftCooldownSetsRetryAfterHeader(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)
	engine := newLoginOnlyTestEngine(t, svc)

	const clientAddr = "198.51.100.77:41000"
	postLogin := func(password string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"identifier":"admin","password":%q}`, password)
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientAddr
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	for i := range maxLoginFailures {
		if rec := postLogin("wrong-password-123"); rec.Code != http.StatusUnauthorized {
			t.Fatalf("failure %d status = %d, want 401 (body=%s)", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := postLogin("correct-password-123")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("cooldown status = %d, want 429 (body=%s)", rec.Code, rec.Body.String())
	}

	raw := rec.Header().Get("Retry-After")
	retryAfter, err := strconv.Atoi(raw)
	if err != nil || retryAfter <= 0 {
		t.Fatalf("Retry-After = %q, want positive integer seconds", raw)
	}
}

func TestAuthLoginRateLimitConfigurable(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)

	// Defensive fallback to 5 when unconfigured or non-positive.
	for _, invalid := range []int{-3, 0} {
		svc.cfg.LoginRateLimit = invalid
		if got := svc.LoginRateLimit(); got != 5 {
			t.Fatalf("LoginRateLimit(%d) = %d, want 5", invalid, got)
		}
	}

	// Configured positive threshold is enforced by RegisterPublicRoutes.
	svc.cfg.LoginRateLimit = 2
	if got := svc.LoginRateLimit(); got != 2 {
		t.Fatalf("LoginRateLimit(2) = %d, want 2", got)
	}

	bundle, err := i18n.NewBundle()
	if err != nil {
		t.Fatalf("NewBundle() error = %v", err)
	}
	if err := bundle.RegisterFS("auth", LocalesFS, "locales"); err != nil {
		t.Fatalf("RegisterFS() error = %v", err)
	}
	engine, err := httpserver.NewEngine(httpserver.Options{
		Env:    "test",
		Logger: logger.New("test", "error", io.Discard),
		I18n:   bundle,
	})
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	NewHandler(svc, nil).RegisterPublicRoutes(engine.Group("/api/v1"))

	const clientAddr = "198.51.100.88:42000"
	postLogin := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"admin","password":"wrong-password-123"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = clientAddr
		rec := httptest.NewRecorder()
		engine.ServeHTTP(rec, req)
		return rec
	}

	for i := range 2 {
		if rec := postLogin(); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401 (body=%s)", i+1, rec.Code, rec.Body.String())
		}
	}

	rec := postLogin()
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt status = %d, want 429 (body=%s)", rec.Code, rec.Body.String())
	}
	var errResp struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal 429: %v", err)
	}
	if errResp.Code != "RATE_LIMITED" {
		t.Fatalf("429 code = %q, want RATE_LIMITED", errResp.Code)
	}
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retryAfter <= 0 {
		t.Fatalf("Retry-After = %q, want positive integer seconds", rec.Header().Get("Retry-After"))
	}
}
