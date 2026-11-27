package httpserver_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
	"tianshu/internal/shared/apperr"
)

func TestRateLimitMiddlewareEnforcesLimitAndRetryAfter(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	r := newTestEngine(t)
	r.POST("/limited", httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:  2,
		Window: time.Minute,
		Now:    func() time.Time { return now },
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for i := range 2 {
		req := httptest.NewRequest(http.MethodPost, "/limited", nil)
		req.RemoteAddr = "203.0.113.10:12345"
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d, want 200", i+1, rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/limited", nil)
	req.RemoteAddr = "203.0.113.10:12345"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("3rd attempt status = %d, want 429", rec.Code)
	}
	if rec.Header().Get("Retry-After") != "60" {
		t.Fatalf("Retry-After = %q, want 60", rec.Header().Get("Retry-After"))
	}
	if rec.Header().Get("X-RateLimit-Limit") != "2" {
		t.Fatalf("X-RateLimit-Limit = %q, want 2", rec.Header().Get("X-RateLimit-Limit"))
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal 429 body: %v", err)
	}
	if body["code"] != apperr.CodeRateLimited {
		t.Fatalf("429 code = %v, want %s", body["code"], apperr.CodeRateLimited)
	}

	// Different IP key stays independent.
	otherReq := httptest.NewRequest(http.MethodPost, "/limited", nil)
	otherReq.RemoteAddr = "203.0.113.11:12345"
	otherRec := httptest.NewRecorder()
	r.ServeHTTP(otherRec, otherReq)
	if otherRec.Code != http.StatusOK {
		t.Fatalf("independent IP status = %d, want 200", otherRec.Code)
	}

	// Advancing clock past window resets counter deterministically.
	now = now.Add(time.Minute)
	resetReq := httptest.NewRequest(http.MethodPost, "/limited", nil)
	resetReq.RemoteAddr = "203.0.113.10:12345"
	resetRec := httptest.NewRecorder()
	r.ServeHTTP(resetRec, resetReq)
	if resetRec.Code != http.StatusOK {
		t.Fatalf("post-window status = %d, want 200", resetRec.Code)
	}
}

func TestRateLimitMiddlewareEmptyKeyAndBoundedBuckets(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	r := newTestEngine(t)

	// Empty key bypasses limiting.
	r.POST("/empty-key", httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:   1,
		Window:  time.Minute,
		KeyFunc: func(*gin.Context) string { return "" },
		Now:     func() time.Time { return now },
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})
	for i := range 3 {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/empty-key", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("empty key attempt %d status = %d, want 200", i+1, rec.Code)
		}
	}

	// MaxBuckets=2 bounds memory by evicting oldest active bucket when key churn exceeds cap.
	r.POST("/bounded", httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:      1,
		Window:     time.Minute,
		MaxBuckets: 2,
		KeyFunc:    func(c *gin.Context) string { return c.GetHeader("X-Key") },
		Now:        func() time.Time { return now },
	}), func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	for _, key := range []string{"k1", "k2", "k3"} {
		req := httptest.NewRequest(http.MethodPost, "/bounded", nil)
		req.Header.Set("X-Key", key)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("first call for %s status = %d, want 200", key, rec.Code)
		}
	}

	// k3 must still be tracked (rate-limited on 2nd call), while k1 was evicted to stay within MaxBuckets=2.
	reqK3 := httptest.NewRequest(http.MethodPost, "/bounded", nil)
	reqK3.Header.Set("X-Key", "k3")
	recK3 := httptest.NewRecorder()
	r.ServeHTTP(recK3, reqK3)
	if recK3.Code != http.StatusTooManyRequests {
		t.Fatalf("2nd call for k3 status = %d, want 429", recK3.Code)
	}

	reqK1 := httptest.NewRequest(http.MethodPost, "/bounded", nil)
	reqK1.Header.Set("X-Key", "k1")
	recK1 := httptest.NewRecorder()
	r.ServeHTTP(recK1, reqK1)
	if recK1.Code != http.StatusOK {
		t.Fatalf("evicted k1 status = %d, want 200 (evicted under MaxBuckets=2)", recK1.Code)
	}

	// Expired sweep frees room before evicting active keys when advancing past Window.
	now = now.Add(2 * time.Minute)
	for i := range 5 {
		req := httptest.NewRequest(http.MethodPost, "/bounded", nil)
		req.Header.Set("X-Key", fmt.Sprintf("fresh-%d", i))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("fresh-%d status = %d, want 200", i, rec.Code)
		}
	}
}
