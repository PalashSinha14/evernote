// Package middleware holds the handlers that run before a route's own handler:
// authentication, logging, CORS and rate limiting.
package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/PalashSinha14/evernote/internal/utils"
	"github.com/gin-gonic/gin"
)

// Context keys under which the authenticated identity is published to
// downstream handlers.
const (
	ContextUserID    = "auth_user_id"
	ContextTokenJTI  = "auth_token_jti"
	ContextTokenExp  = "auth_token_exp"
	authHeader       = "Authorization"
	bearerPrefix     = "Bearer "
	revocationBudget = 2 * time.Second
)

// RevocationChecker reports whether a token has been logged out. The middleware
// depends on this narrow interface rather than on the repository itself, so the
// HTTP layer can be tested without a database.
type RevocationChecker interface {
	IsRevoked(ctx context.Context, jti string) (bool, error)
}

// RequireAuth verifies the bearer token on a request and publishes the caller's
// identity into the Gin context.
//
// Four things have to hold before the request continues: the header is present
// and well formed, the signature verifies, the token has not expired, and the
// token has not been revoked by a logout. The first three are decided from the
// token alone; the fourth needs the database, which is the price of being able
// to revoke a stateless credential.
//
// Every failure returns the same 401 and the same message. Telling a caller
// which of the four checks failed would let them probe for valid token IDs.
func RequireAuth(secret string, revoked RevocationChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := c.GetHeader(authHeader)
		if !strings.HasPrefix(raw, bearerPrefix) {
			unauthorized(c)
			return
		}

		token := strings.TrimSpace(strings.TrimPrefix(raw, bearerPrefix))
		if token == "" {
			unauthorized(c)
			return
		}

		claims, err := utils.ParseToken(secret, token)
		if err != nil {
			unauthorized(c)
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), revocationBudget)
		defer cancel()

		isRevoked, err := revoked.IsRevoked(ctx, claims.JTI())
		if err != nil {
			// The database could not answer, so whether this token is still
			// valid is unknown. Refusing is the only safe response: treating
			// "unknown" as "valid" would make logout fail open.
			c.AbortWithStatusJSON(http.StatusInternalServerError,
				schemas.NewError(schemas.CodeInternalError, "Something went wrong", nil))
			return
		}
		if isRevoked {
			unauthorized(c)
			return
		}

		c.Set(ContextUserID, claims.UserID())
		c.Set(ContextTokenJTI, claims.JTI())
		c.Set(ContextTokenExp, claims.ExpiresAt.Time)
		c.Next()
	}
}

func unauthorized(c *gin.Context) {
	c.AbortWithStatusJSON(http.StatusUnauthorized,
		schemas.NewError(schemas.CodeUnauthorized, "Missing or invalid authentication token", nil))
}

// UserID returns the authenticated caller's ID. The second result is false when
// the route was not behind RequireAuth.
func UserID(c *gin.Context) (string, bool) {
	v, ok := c.Get(ContextUserID)
	if !ok {
		return "", false
	}
	id, ok := v.(string)
	return id, ok
}

// TokenIdentity returns the current token's jti and expiry, which logout needs
// in order to revoke it.
func TokenIdentity(c *gin.Context) (string, time.Time, bool) {
	rawJTI, ok := c.Get(ContextTokenJTI)
	if !ok {
		return "", time.Time{}, false
	}
	rawExp, ok := c.Get(ContextTokenExp)
	if !ok {
		return "", time.Time{}, false
	}
	jti, okJTI := rawJTI.(string)
	exp, okExp := rawExp.(time.Time)
	return jti, exp, okJTI && okExp
}
