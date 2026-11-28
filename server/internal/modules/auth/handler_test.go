package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/platform/i18n"
	"tianshu/internal/platform/logger"
	"tianshu/internal/platform/security"
	"tianshu/internal/shared/apperr"
)

// testRefreshCookiePath mirrors the cookie Path injected by internal/app in production.
const testRefreshCookiePath = "/api/v1/auth"

func newTestAuthEngine(t *testing.T, svc *Service, allowedOrigins []string) *gin.Engine {
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

	apiV1 := engine.Group("/api/v1")
	h := NewHandler(svc, allowedOrigins, testRefreshCookiePath)
	h.RegisterPublicRoutes(apiV1)
	authed := apiV1.Group("")
	authed.Use(RequireAuth(svc.jwt))
	h.RegisterRoutes(authed)
	return engine
}

func assertErrorContract(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) map[string]any {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body=%s)", rec.Code, wantStatus, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") == "" {
		t.Fatal("missing X-Request-Id response header")
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body %q: %v", rec.Body.String(), err)
	}
	if gotCode, _ := body["code"].(string); gotCode != wantCode {
		t.Fatalf("error code = %q, want %q (body=%s)", gotCode, wantCode, rec.Body.String())
	}
	if msg, _ := body["message"].(string); strings.TrimSpace(msg) == "" {
		t.Fatalf("error message must be non-empty (body=%s)", rec.Body.String())
	}
	if _, hasReqID := body["request_id"]; hasReqID {
		t.Fatalf("error body must not contain request_id (body=%s)", rec.Body.String())
	}
	if _, hasCamelReqID := body["requestId"]; hasCamelReqID {
		t.Fatalf("error body must not contain requestId (body=%s)", rec.Body.String())
	}
	if wantStatus != http.StatusUnprocessableEntity {
		if _, hasErrors := body["errors"]; hasErrors {
			t.Fatalf("non-422 error body must not contain errors field (body=%s)", rec.Body.String())
		}
	}
	return body
}

func findRefreshCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == refreshCookieName {
			return ck
		}
	}
	return nil
}

func assertRefreshCookieCleared(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	ck := findRefreshCookie(rec)
	if ck == nil {
		t.Fatalf("expected clearing Set-Cookie for %s, got none", refreshCookieName)
	}
	if ck.Value != "" || ck.MaxAge >= 0 || ck.Path != testRefreshCookiePath || !ck.HttpOnly {
		t.Fatalf("invalid cleared refresh cookie: %+v", ck)
	}
}

func TestAuthHTTPRoutesContract(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)
	engine := newTestAuthEngine(t, svc, []string{"https://admin.example.com/"})

	// 1. Login returns 200 with X-Request-Id, accessToken, and HttpOnly refresh cookie.
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
	if recLogin.Header().Get("X-Request-Id") == "" {
		t.Fatal("login response missing X-Request-Id header")
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

	refreshCookie := findRefreshCookie(recLogin)
	if refreshCookie == nil || !refreshCookie.HttpOnly || refreshCookie.Path != testRefreshCookiePath || refreshCookie.MaxAge <= 0 {
		t.Fatalf("invalid refresh cookie: %+v", refreshCookie)
	}

	// 2. GET /auth/me without Bearer token returns 401; with case-insensitive Bearer token returns 200.
	recAnon := httptest.NewRecorder()
	engine.ServeHTTP(recAnon, httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil))
	assertErrorContract(t, recAnon, http.StatusUnauthorized, apperr.CodeUnauthorized)

	reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	reqMe.Header.Set("Authorization", "bearer "+loginBody.AccessToken)
	recMe := httptest.NewRecorder()
	engine.ServeHTTP(recMe, reqMe)
	if recMe.Code != http.StatusOK || recMe.Header().Get("X-Request-Id") == "" {
		t.Fatalf("authed /auth/me status = %d reqID=%q, want 200 (body=%s)", recMe.Code, recMe.Header().Get("X-Request-Id"), recMe.Body.String())
	}

	// 3. POST /auth/refresh from disallowed/malformed/cross-scheme Origin or missing headers with cookie returns 403 AUTH_ORIGIN_REJECTED.
	for _, tc := range []struct {
		name    string
		origin  string
		referer string
	}{
		{"foreign origin", "https://evil.example.net", ""},
		{"null origin", "null", ""},
		{"missing origin and referer with cookie", "", ""},
		{"cross-scheme https on plain http", "https://tianshu.local", ""},
		{"custom scheme", "evil://tianshu.local", ""},
		{"scheme-relative", "//tianshu.local", ""},
		{"userinfo and path injection", "http://attacker@tianshu.local/path", ""},
		{"malformed referer", "", "://bad-referer"},
	} {
		t.Run("refresh_reject_"+tc.name, func(t *testing.T) {
			reqBad := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
			reqBad.Host = "tianshu.local"
			if tc.origin != "" {
				reqBad.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				reqBad.Header.Set("Referer", tc.referer)
			}
			reqBad.AddCookie(refreshCookie)
			recBad := httptest.NewRecorder()
			engine.ServeHTTP(recBad, reqBad)
			assertErrorContract(t, recBad, http.StatusForbidden, ErrCodeOriginRejected)
		})
	}

	// 4. POST /auth/refresh from same-origin (matching HTTP scheme), allowlisted origin, and Referer fallback succeed.
	reqRefresh := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqRefresh.Host = "tianshu.local"
	reqRefresh.Header.Set("Origin", "http://tianshu.local")
	reqRefresh.AddCookie(refreshCookie)
	recRefresh := httptest.NewRecorder()
	engine.ServeHTTP(recRefresh, reqRefresh)
	if recRefresh.Code != http.StatusOK {
		t.Fatalf("same-origin refresh status = %d, want 200 (body=%s)", recRefresh.Code, recRefresh.Body.String())
	}
	oldCookie := refreshCookie
	refreshCookie = findRefreshCookie(recRefresh)
	if refreshCookie == nil || refreshCookie.Value == oldCookie.Value {
		t.Fatalf("expected rotated refresh cookie, got %+v", refreshCookie)
	}

	// Grace-window refresh via allowlisted Origin succeeds and must NOT emit Set-Cookie.
	reqGrace := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqGrace.Host = "tianshu.local"
	reqGrace.Header.Set("Origin", "https://admin.example.com")
	reqGrace.AddCookie(oldCookie)
	recGrace := httptest.NewRecorder()
	engine.ServeHTTP(recGrace, reqGrace)
	if recGrace.Code != http.StatusOK {
		t.Fatalf("allowlisted grace refresh status = %d, want 200 (body=%s)", recGrace.Code, recGrace.Body.String())
	}
	if ck := findRefreshCookie(recGrace); ck != nil {
		t.Fatalf("grace-window refresh must not emit Set-Cookie, got %+v", ck)
	}

	// Referer fallback (without Origin) succeeds on same-origin.
	reqRefFallback := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqRefFallback.Host = "tianshu.local"
	reqRefFallback.Header.Set("Referer", "http://tianshu.local/settings/sessions")
	reqRefFallback.AddCookie(refreshCookie)
	recRefFallback := httptest.NewRecorder()
	engine.ServeHTTP(recRefFallback, reqRefFallback)
	if recRefFallback.Code != http.StatusOK {
		t.Fatalf("referer fallback refresh status = %d, want 200 (body=%s)", recRefFallback.Code, recRefFallback.Body.String())
	}
	refreshCookie = findRefreshCookie(recRefFallback)

	// 5. POST /auth/logout rejects foreign Origin, and on same-origin returns 204 and clears the refresh cookie.
	reqBadLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqBadLogout.Host = "tianshu.local"
	reqBadLogout.Header.Set("Origin", "https://evil.example.net")
	reqBadLogout.AddCookie(refreshCookie)
	recBadLogout := httptest.NewRecorder()
	engine.ServeHTTP(recBadLogout, reqBadLogout)
	assertErrorContract(t, recBadLogout, http.StatusForbidden, ErrCodeOriginRejected)

	reqLogout := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	reqLogout.Host = "tianshu.local"
	reqLogout.Header.Set("Origin", "http://tianshu.local")
	reqLogout.Header.Set("Authorization", "Bearer "+loginBody.AccessToken)
	reqLogout.AddCookie(refreshCookie)
	recLogout := httptest.NewRecorder()
	engine.ServeHTTP(recLogout, reqLogout)
	if recLogout.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", recLogout.Code)
	}
	assertRefreshCookieCleared(t, recLogout)

	// Missing Origin/Referer WITHOUT cookie passes middleware and returns 401 on refresh / 204 on logout.
	recNoCookieRef := httptest.NewRecorder()
	engine.ServeHTTP(recNoCookieRef, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil))
	assertErrorContract(t, recNoCookieRef, http.StatusUnauthorized, apperr.CodeUnauthorized)

	recNoCookieLogout := httptest.NewRecorder()
	engine.ServeHTTP(recNoCookieLogout, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))
	if recNoCookieLogout.Code != http.StatusNoContent {
		t.Fatalf("cookieless logout status = %d, want 204", recNoCookieLogout.Code)
	}
}

func TestRequireSameOriginHTTPSProtection(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)
	svc.cfg.CookieSecure = true
	engine := newTestAuthEngine(t, svc, nil)

	loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	cookie := &http.Cookie{Name: refreshCookieName, Value: loginRes.RefreshToken, Path: testRefreshCookiePath}

	// Downgraded http Origin must be rejected when CookieSecure=true.
	reqHTTP := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqHTTP.Host = "tianshu.local"
	reqHTTP.Header.Set("Origin", "http://tianshu.local")
	reqHTTP.AddCookie(cookie)
	recHTTP := httptest.NewRecorder()
	engine.ServeHTTP(recHTTP, reqHTTP)
	assertErrorContract(t, recHTTP, http.StatusForbidden, ErrCodeOriginRejected)

	// Matching https Origin succeeds.
	reqHTTPS := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	reqHTTPS.Host = "tianshu.local"
	reqHTTPS.TLS = &tls.ConnectionState{}
	reqHTTPS.Header.Set("Origin", "https://tianshu.local")
	reqHTTPS.AddCookie(cookie)
	recHTTPS := httptest.NewRecorder()
	engine.ServeHTTP(recHTTPS, reqHTTPS)
	if recHTTPS.Code != http.StatusOK {
		t.Fatalf("https refresh status = %d, want 200 (body=%s)", recHTTPS.Code, recHTTPS.Body.String())
	}
	if ck := findRefreshCookie(recHTTPS); ck == nil || !ck.Secure {
		t.Fatalf("expected Secure cookie on CookieSecure=true, got %+v", ck)
	}
}

func TestRequireAuthTokenErrorsAndLocaleContract(t *testing.T) {
	svc, _, _, _ := newTestAuthHarness(t)
	engine := newTestAuthEngine(t, svc, nil)

	expiredToken := mintExpiredTestJWT(t, "test-secret-for-auth-service-32-bytes!!")

	// Mint a token signed with a different secret.
	wrongMgr, err := security.NewJWTManager("another-secret-for-auth-service-32bytes!", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewJWTManager(wrong): %v", err)
	}
	wrongSecretToken, _, err := wrongMgr.Sign(uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()))
	if err != nil {
		t.Fatalf("Sign(wrong): %v", err)
	}

	// Mint an alg=none token.
	algNoneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	algNonePayload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + uuid.NewString() + `","sid":"` + uuid.NewString() + `","iss":"tianshu","aud":"tianshu-web","iat":1700000000,"exp":2000000000}`))
	algNoneToken := algNoneHeader + "." + algNonePayload + "."

	cases := []struct {
		name     string
		header   string
		wantCode string
	}{
		{"missing header", "", apperr.CodeUnauthorized},
		{"basic scheme", "Basic dXNlcjpwYXNz", apperr.CodeUnauthorized},
		{"empty bearer", "Bearer   ", apperr.CodeUnauthorized},
		{"malformed token", "Bearer not-a-jwt", apperr.CodeUnauthorized},
		{"wrong secret", "Bearer " + wrongSecretToken, apperr.CodeUnauthorized},
		{"alg none", "Bearer " + algNoneToken, apperr.CodeUnauthorized},
		{"expired token", "Bearer " + expiredToken, ErrCodeTokenExpired},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqZH := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tc.header != "" {
				reqZH.Header.Set("Authorization", tc.header)
			}
			reqZH.Header.Set("Accept-Language", "zh-CN")
			recZH := httptest.NewRecorder()
			engine.ServeHTTP(recZH, reqZH)
			bodyZH := assertErrorContract(t, recZH, http.StatusUnauthorized, tc.wantCode)

			reqEN := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
			if tc.header != "" {
				reqEN.Header.Set("Authorization", tc.header)
			}
			reqEN.Header.Set("Accept-Language", "en-US")
			recEN := httptest.NewRecorder()
			engine.ServeHTTP(recEN, reqEN)
			bodyEN := assertErrorContract(t, recEN, http.StatusUnauthorized, tc.wantCode)

			if bodyZH["message"] == bodyEN["message"] {
				t.Fatalf("expected localized zh-CN vs en-US messages to differ, both got %q", bodyZH["message"])
			}
		})
	}
}

func mintExpiredTestJWT(t *testing.T, secret string) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	past := time.Now().UTC().Add(-10 * time.Minute).Unix()
	payloadJSON := fmt.Sprintf(
		`{"sub":"%s","sid":"%s","iss":"tianshu","aud":"tianshu-web","iat":%d,"exp":%d}`,
		uuid.Must(uuid.NewV7()).String(),
		uuid.Must(uuid.NewV7()).String(),
		past-600,
		past,
	)
	payload := base64.RawURLEncoding.EncodeToString([]byte(payloadJSON))
	signingInput := header + "." + payload
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func TestAuthHTTPValidationDisabledCookieAndEmptySlices(t *testing.T) {
	t.Run("400 malformed JSON and 422 validation errors with i18n", func(t *testing.T) {
		svc, _, _, _ := newTestAuthHarness(t)
		engine := newTestAuthEngine(t, svc, nil)

		reqBadJSON := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":`))
		reqBadJSON.Header.Set("Content-Type", "application/json")
		reqBadJSON.RemoteAddr = "198.51.100.1:1000"
		recBadJSON := httptest.NewRecorder()
		engine.ServeHTTP(recBadJSON, reqBadJSON)
		assertErrorContract(t, recBadJSON, http.StatusBadRequest, apperr.CodeBadRequest)

		postVal := func(lang string) map[string]any {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"","password":""}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept-Language", lang)
			req.RemoteAddr = "198.51.100.2:1000"
			rec := httptest.NewRecorder()
			engine.ServeHTTP(rec, req)
			return assertErrorContract(t, rec, http.StatusUnprocessableEntity, apperr.CodeValidationFailed)
		}
		zhBody := postVal("zh-CN")
		enBody := postVal("en-US")
		if zhBody["message"] == enBody["message"] {
			t.Fatalf("expected 422 messages to differ across locales: %q", zhBody["message"])
		}
		errs, ok := enBody["errors"].([]any)
		if !ok || len(errs) != 2 {
			t.Fatalf("expected 2 field errors, got %#v", enBody["errors"])
		}
	})

	t.Run("403 AUTH_ACCOUNT_DISABLED on login, refresh (clears cookie), and me", func(t *testing.T) {
		svc, _, idProv, _ := newTestAuthHarness(t)
		engine := newTestAuthEngine(t, svc, nil)

		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		// Disable the user after login.
		for id, u := range idProv.users {
			u.User.Status = userStatusDisabled
			idProv.users[id] = u
		}

		// /login -> 403 AUTH_ACCOUNT_DISABLED
		reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"admin","password":"correct-password-123"}`))
		reqLogin.Header.Set("Content-Type", "application/json")
		reqLogin.RemoteAddr = "198.51.100.3:1000"
		recLogin := httptest.NewRecorder()
		engine.ServeHTTP(recLogin, reqLogin)
		assertErrorContract(t, recLogin, http.StatusForbidden, ErrCodeAccountDisabled)

		// /me -> 403 AUTH_ACCOUNT_DISABLED
		reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		reqMe.Header.Set("Authorization", "Bearer "+loginRes.AccessToken)
		recMe := httptest.NewRecorder()
		engine.ServeHTTP(recMe, reqMe)
		assertErrorContract(t, recMe, http.StatusForbidden, ErrCodeAccountDisabled)

		// /refresh -> 403 AUTH_ACCOUNT_DISABLED and clears cookie
		reqRef := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		reqRef.Host = "tianshu.local"
		reqRef.Header.Set("Origin", "http://tianshu.local")
		reqRef.AddCookie(&http.Cookie{Name: refreshCookieName, Value: loginRes.RefreshToken, Path: testRefreshCookiePath})
		recRef := httptest.NewRecorder()
		engine.ServeHTTP(recRef, reqRef)
		assertErrorContract(t, recRef, http.StatusForbidden, ErrCodeAccountDisabled)
		assertRefreshCookieCleared(t, recRef)
	})

	t.Run("refresh 500 preserves cookie while 401 clears cookie", func(t *testing.T) {
		svc, _, idProv, now := newTestAuthHarness(t)
		engine := newTestAuthEngine(t, svc, nil)

		loginRes, err := svc.Login(t.Context(), LoginInput{Identifier: "admin", Password: "correct-password-123"})
		if err != nil {
			t.Fatalf("Login: %v", err)
		}
		if _, err := svc.Refresh(t.Context(), RefreshInput{RefreshToken: loginRes.RefreshToken}); err != nil {
			t.Fatalf("Refresh: %v", err)
		}

		// Simulate transient DB error during grace window -> 500 MUST NOT clear cookie.
		*now = now.Add(10 * time.Second)
		idProv.getByIDErr = apperr.Internal(errors.New("transient db timeout"))
		req500 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		req500.Host = "tianshu.local"
		req500.Header.Set("Origin", "http://tianshu.local")
		req500.AddCookie(&http.Cookie{Name: refreshCookieName, Value: loginRes.RefreshToken, Path: testRefreshCookiePath})
		rec500 := httptest.NewRecorder()
		engine.ServeHTTP(rec500, req500)
		assertErrorContract(t, rec500, http.StatusInternalServerError, apperr.CodeInternal)
		if ck := findRefreshCookie(rec500); ck != nil {
			t.Fatalf("500 response must not clear refresh cookie, got %+v", ck)
		}

		// Invalid token -> 401 MUST clear cookie.
		idProv.getByIDErr = nil
		req401 := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		req401.Host = "tianshu.local"
		req401.Header.Set("Origin", "http://tianshu.local")
		req401.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "unknown-token", Path: testRefreshCookiePath})
		rec401 := httptest.NewRecorder()
		engine.ServeHTTP(rec401, req401)
		assertErrorContract(t, rec401, http.StatusUnauthorized, apperr.CodeUnauthorized)
		assertRefreshCookieCleared(t, rec401)
	})

	t.Run("empty roles and permissions serialize as JSON [] on login and me", func(t *testing.T) {
		svc, _, _, _ := newTestAuthHarness(t)
		svc.access = &fakeAccessProvider{access: UserAccess{Roles: nil, Permissions: nil}}
		engine := newTestAuthEngine(t, svc, nil)

		reqLogin := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"identifier":"admin","password":"correct-password-123"}`))
		reqLogin.Header.Set("Content-Type", "application/json")
		reqLogin.RemoteAddr = "198.51.100.4:1000"
		recLogin := httptest.NewRecorder()
		engine.ServeHTTP(recLogin, reqLogin)
		if recLogin.Code != http.StatusOK {
			t.Fatalf("login status = %d, want 200", recLogin.Code)
		}
		if !strings.Contains(recLogin.Body.String(), `"roles":[]`) || !strings.Contains(recLogin.Body.String(), `"permissions":[]`) {
			t.Fatalf("expected empty JSON arrays in login response, got %s", recLogin.Body.String())
		}

		var body struct {
			AccessToken string `json:"accessToken"`
		}
		if err := json.Unmarshal(recLogin.Body.Bytes(), &body); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		reqMe := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		reqMe.Header.Set("Authorization", "Bearer "+body.AccessToken)
		recMe := httptest.NewRecorder()
		engine.ServeHTTP(recMe, reqMe)
		if recMe.Code != http.StatusOK {
			t.Fatalf("me status = %d, want 200", recMe.Code)
		}
		if !strings.Contains(recMe.Body.String(), `"roles":[]`) || !strings.Contains(recMe.Body.String(), `"permissions":[]`) {
			t.Fatalf("expected empty JSON arrays in me response, got %s", recMe.Body.String())
		}
	})
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

	h := NewHandler(svc, nil, testRefreshCookiePath)
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
	assertErrorContract(t, rec, http.StatusTooManyRequests, apperr.CodeRateLimited)

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
		if got := svc.loginRateLimit(); got != 5 {
			t.Fatalf("loginRateLimit(%d) = %d, want 5", invalid, got)
		}
	}

	// Configured positive threshold is enforced by RegisterPublicRoutes.
	svc.cfg.LoginRateLimit = 2
	if got := svc.loginRateLimit(); got != 2 {
		t.Fatalf("loginRateLimit(2) = %d, want 2", got)
	}

	engine := newTestAuthEngine(t, svc, nil)

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
	assertErrorContract(t, rec, http.StatusTooManyRequests, apperr.CodeRateLimited)
	retryAfter, err := strconv.Atoi(rec.Header().Get("Retry-After"))
	if err != nil || retryAfter <= 0 {
		t.Fatalf("Retry-After = %q, want positive integer seconds", rec.Header().Get("Retry-After"))
	}
}
