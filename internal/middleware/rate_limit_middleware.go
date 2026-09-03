package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/gin-gonic/gin"
)

// rateBucket tracks one caller's request count within the current window.
type rateBucket struct {
	count       int
	windowStart time.Time
}

// RateLimiter is a fixed-window request limiter, keyed by client IP.
//
// It lives in this process's memory. Behind more than one instance of the
// service — the shape a real deployment behind a load balancer would take —
// each instance enforces its own limit independently rather than a shared
// one, so the effective limit scales up with the number of instances. A
// shared limit across instances needs a shared store, such as Redis, which is
// outside this project's scope; the limitation is recorded rather than
// engineered around.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
	limit   int
	window  time.Duration
}

// NewRateLimiter builds a limiter allowing limit requests per window, per
// caller.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{buckets: map[string]*rateBucket{}, limit: limit, window: window}
}

// Middleware returns the gin.HandlerFunc that enforces this limiter.
//
// api_spec.md's security notes name exactly two surfaces for this: the auth
// endpoints and public share access. Both are reachable by a caller who holds
// no token at all, and so both are otherwise unshielded by anything that
// costs an attacker effort to repeat — nothing else in the request is
// expensive to send again immediately. One limiter instance is shared across
// both route groups in app.go, so the two draw from a single combined budget
// per caller rather than each getting its own.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.ClientIP()
		now := time.Now()

		rl.mu.Lock()
		b, ok := rl.buckets[key]
		if !ok || now.Sub(b.windowStart) >= rl.window {
			b = &rateBucket{windowStart: now}
			rl.buckets[key] = b
		}
		b.count++
		over := b.count > rl.limit
		rl.mu.Unlock()

		if over {
			c.AbortWithStatusJSON(http.StatusTooManyRequests,
				schemas.NewError(schemas.CodeRateLimited, "Too many requests, try again later", nil))
			return
		}

		c.Next()
	}
}
