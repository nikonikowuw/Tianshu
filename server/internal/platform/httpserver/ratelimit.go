package httpserver

import (
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/shared/apperr"
)

// RateLimitConfig configures a fixed-window in-process rate limiter middleware.
type RateLimitConfig struct {
	Limit   int
	Window  time.Duration
	KeyFunc func(c *gin.Context) string
}

type rateWindow struct {
	count   int
	resetAt time.Time
}

// RateLimitMiddleware returns a Gin middleware enforcing Limit requests per Window per key.
// Exceeded requests receive 429 RATE_LIMITED with a Retry-After header.
func RateLimitMiddleware(cfg RateLimitConfig) gin.HandlerFunc {
	if cfg.Limit <= 0 {
		cfg.Limit = 60
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	keyFn := cfg.KeyFunc
	if keyFn == nil {
		keyFn = func(c *gin.Context) string {
			return c.ClientIP()
		}
	}

	var (
		mu      sync.Mutex
		buckets = make(map[string]*rateWindow)
	)

	return func(c *gin.Context) {
		key := keyFn(c)
		if key == "" {
			c.Next()
			return
		}

		now := time.Now()
		mu.Lock()
		// Periodic opportunistic sweep to keep memory bounded under high key churn.
		if len(buckets) > 4096 {
			for k, b := range buckets {
				if !now.Before(b.resetAt) {
					delete(buckets, k)
				}
			}
		}

		bucket, ok := buckets[key]
		if !ok || !now.Before(bucket.resetAt) {
			bucket = &rateWindow{
				count:   1,
				resetAt: now.Add(cfg.Window),
			}
			buckets[key] = bucket
			mu.Unlock()
			c.Next()
			return
		}

		if bucket.count >= cfg.Limit {
			retrySeconds := max(int(math.Ceil(time.Until(bucket.resetAt).Seconds())), 1)
			mu.Unlock()
			c.Header("Retry-After", strconv.Itoa(retrySeconds))
			c.Header("X-RateLimit-Limit", fmt.Sprint(cfg.Limit))
			_ = c.Error(apperr.RateLimited())
			c.Abort()
			return
		}

		bucket.count++
		mu.Unlock()
		c.Next()
	}
}
