package middleware

import (
	"log"
	"net/http"
	"runtime/debug"

	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/gin-gonic/gin"
)

// Recovery turns a panic in any downstream handler into the API's standard
// error envelope, rather than Gin's own bare 500 with no body.
//
// It must sit inside Logger (registered after it) and outside every route, so
// that every panic in the service is caught here — no handler is trusted to
// protect itself.
func Recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("panic recovered: %v\n%s", r, debug.Stack())
				c.AbortWithStatusJSON(http.StatusInternalServerError,
					schemas.NewError(schemas.CodeInternalError, "Something went wrong", nil))
			}
		}()
		c.Next()
	}
}
