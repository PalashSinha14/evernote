package middleware

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
)

// CORS allows cross-origin requests from the configured origins, and answers
// a browser's preflight OPTIONS request without forwarding it to any handler.
//
// A wildcard origin ("*", the default — see config.Config.AllowedOrigins) is
// safe here specifically because this API authenticates with a bearer token
// carried in an Authorization header, never a cookie. Wildcard CORS is
// dangerous alongside cookie-based auth, where a browser attaches credentials
// to a cross-origin request automatically; a bearer token has to be attached
// deliberately by the calling code, so a malicious page on another origin
// gains nothing by getting a victim's browser to hit this API.
func CORS(allowedOrigins []string) gin.HandlerFunc {
	allowAll := slices.Contains(allowedOrigins, "*")

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && (allowAll || slices.Contains(allowedOrigins, origin)) {
			if allowAll {
				c.Header("Access-Control-Allow-Origin", "*")
			} else {
				// Echoing back one specific allowed origin, rather than
				// always sending the same value, is why the response must
				// vary by the Origin request header — otherwise a cache
				// sitting in front of this service could serve one caller's
				// CORS headers to a different caller with a different origin.
				c.Header("Access-Control-Allow-Origin", origin)
				c.Header("Vary", "Origin")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Share-Password")
		}

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
