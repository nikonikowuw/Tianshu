package httpserver_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/platform/httpserver"
)

func TestRateLimitMiddlewareEnforcesLimitAndRetryAfter(t *testing.T) {
	r := newTestEngine(t)
	r.POST("/limited", httpserver.RateLimitMiddleware(httpserver.RateLimitConfig{
		Limit:  2,
		Window: time.Minute,
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
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("missing Retry-After header on 429 response")
	}
}
