package httpserver

import (
	"container/list"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"tianshu/internal/shared/apperr"
)

const defaultMaxRateLimitBuckets = 4096

// RateLimitConfig configures a fixed-window in-process rate limiter middleware.
type RateLimitConfig struct {
	Limit      int
	Window     time.Duration
	MaxBuckets int
	KeyFunc    func(c *gin.Context) string
	Now        func() time.Time
}

type rateWindow struct {
	key     string
	count   int
	resetAt time.Time
	elem    *list.Element
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
	maxBuckets := cfg.MaxBuckets
	if maxBuckets <= 0 {
		maxBuckets = defaultMaxRateLimitBuckets
	}
	keyFn := cfg.KeyFunc
	if keyFn == nil {
		keyFn = func(c *gin.Context) string {
			return c.ClientIP()
		}
	}
	nowFn := cfg.Now
	if nowFn == nil {
		nowFn = time.Now
	}

	var (
		mu        sync.Mutex
		buckets   = make(map[string]*rateWindow)
		order     = list.New()
		lastSweep time.Time
	)

	return func(c *gin.Context) {
		key := keyFn(c)
		if key == "" {
			c.Next()
			return
		}

		now := nowFn()
		mu.Lock()

		bucket, ok := buckets[key]
		if ok && now.Before(bucket.resetAt) {
			order.MoveToBack(bucket.elem)
			if bucket.count >= cfg.Limit {
				retrySeconds := max(int(math.Ceil(bucket.resetAt.Sub(now).Seconds())), 1)
				mu.Unlock()
				c.Header("Retry-After", strconv.Itoa(retrySeconds))
				c.Header("X-RateLimit-Limit", strconv.Itoa(cfg.Limit))
				_ = c.Error(apperr.RateLimited())
				c.Abort()
				return
			}
			bucket.count++
			mu.Unlock()
			c.Next()
			return
		}

		if ok {
			// Window expired for existing key: reset in place without reallocating map entry.
			bucket.count = 1
			bucket.resetAt = now.Add(cfg.Window)
			order.MoveToBack(bucket.elem)
			mu.Unlock()
			c.Next()
			return
		}

		// Evict expired entries at most once per Window when hitting capacity,
		// then fall back to O(1) oldest-bucket eviction to keep memory strictly bounded.
		if len(buckets) >= maxBuckets {
			if lastSweep.IsZero() || !now.Before(lastSweep.Add(cfg.Window)) {
				for k, b := range buckets {
					if !now.Before(b.resetAt) {
						delete(buckets, k)
						order.Remove(b.elem)
					}
				}
				lastSweep = now
			}
			for len(buckets) >= maxBuckets {
				oldest := order.Front()
				if oldest == nil {
					break
				}
				evicted, _ := oldest.Value.(*rateWindow)
				order.Remove(oldest)
				if evicted != nil {
					delete(buckets, evicted.key)
				}
			}
		}

		bucket = &rateWindow{
			key:     key,
			count:   1,
			resetAt: now.Add(cfg.Window),
		}
		bucket.elem = order.PushBack(bucket)
		buckets[key] = bucket
		mu.Unlock()
		c.Next()
	}
}
