package webui_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"tianshu/internal/webui"
)

func TestNewHandler(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":          {Data: []byte("<html><body>tianshu</body></html>")},
		"assets/app-12345.js": {Data: []byte("console.log('ok')")},
		"favicon.ico":         {Data: []byte("ico")},
	}

	h := webui.NewHandler(fsys)

	tests := []struct {
		name             string
		method           string
		path             string
		wantStatus       int
		wantCacheControl string
		wantBodyContains string
		wantContentType  string
	}{
		{
			name:             "root serves index without strong cache",
			method:           http.MethodGet,
			path:             "/",
			wantStatus:       http.StatusOK,
			wantCacheControl: "no-cache",
			wantBodyContains: "tianshu",
			wantContentType:  "text/html",
		},
		{
			name:             "spa deep link falls back to index",
			method:           http.MethodGet,
			path:             "/users/123",
			wantStatus:       http.StatusOK,
			wantCacheControl: "no-cache",
			wantBodyContains: "tianshu",
			wantContentType:  "text/html",
		},
		{
			name:             "hashed asset gets immutable long cache",
			method:           http.MethodGet,
			path:             "/assets/app-12345.js",
			wantStatus:       http.StatusOK,
			wantCacheControl: "public, max-age=31536000, immutable",
			wantBodyContains: "console.log",
		},
		{
			name:             "root static file gets short cache",
			method:           http.MethodGet,
			path:             "/favicon.ico",
			wantStatus:       http.StatusOK,
			wantCacheControl: "public, max-age=3600",
		},
		{
			name:       "missing asset with extension returns 404",
			method:     http.MethodGet,
			path:       "/assets/missing.js",
			wantStatus: http.StatusNotFound,
		},
		{
			name:             "unmatched api path returns json 404",
			method:           http.MethodGet,
			path:             "/api/v1/not-found",
			wantStatus:       http.StatusNotFound,
			wantContentType:  "application/json",
			wantBodyContains: `"code":"RESOURCE_NOT_FOUND"`,
		},
		{
			name:       "non get or head method returns 405",
			method:     http.MethodPost,
			path:       "/users",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
			}
			if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
				t.Fatalf("X-Frame-Options = %q, want DENY", got)
			}
			if tc.wantCacheControl != "" && rec.Header().Get("Cache-Control") != tc.wantCacheControl {
				t.Fatalf("Cache-Control = %q, want %q", rec.Header().Get("Cache-Control"), tc.wantCacheControl)
			}
			if tc.wantContentType != "" && !strings.Contains(rec.Header().Get("Content-Type"), tc.wantContentType) {
				t.Fatalf("Content-Type = %q, want containing %q", rec.Header().Get("Content-Type"), tc.wantContentType)
			}
			if tc.wantBodyContains != "" && !strings.Contains(rec.Body.String(), tc.wantBodyContains) {
				t.Fatalf("body = %q, want containing %q", rec.Body.String(), tc.wantBodyContains)
			}
		})
	}
}

func TestNewHandlerMissingIndexReturns503(t *testing.T) {
	h := webui.NewHandler(fstest.MapFS{})
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}
