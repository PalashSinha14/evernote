package middleware

import (
	"log"
	"time"

	"github.com/gin-gonic/gin"
)

// Logger writes one line per request: method, path, status, latency and the
// caller's IP.
//
// It must be registered before Recovery in the middleware chain, not after.
// A panic unwinds the Go call stack looking for the nearest recover, skipping
// any plain code — including a log line — sitting between the panic and
// whichever middleware actually recovers it. Registering Logger first makes
// it the outermost frame: Recovery converts the panic into a normal return
// before control passes back up to Logger, so the request still gets logged,
// with the 500 status Recovery wrote.
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		if raw := c.Request.URL.RawQuery; raw != "" {
			path += "?" + raw
		}

		c.Next()

		log.Printf("%s %s %d %s %s",
			c.Request.Method, path, c.Writer.Status(), time.Since(start), c.ClientIP())
	}
}
