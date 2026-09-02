// Package handlers holds the HTTP layer, grouped by feature. A handler binds
// the request, enforces access rules, calls the data layer and shapes the
// response. No MongoDB query is issued from this package.
package handlers

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/PalashSinha14/evernote/internal/schemas"
	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// respondError writes the standard error envelope and stops the handler chain.
func respondError(c *gin.Context, status int, code, message string, details any) {
	c.AbortWithStatusJSON(status, schemas.NewError(code, message, details))
}

// respondBindError turns a failed request binding into a 400.
//
// When the failure is a validation error the response names the offending
// fields in details, because "invalid input" alone gives a client no way to
// fix its request. A malformed JSON body carries no field information, so it
// gets the plain message.
func respondBindError(c *gin.Context, err error) {
	var verrs validator.ValidationErrors
	if errors.As(err, &verrs) {
		details := make(map[string]string, len(verrs))
		for _, fe := range verrs {
			details[fe.Field()] = describeRule(fe)
		}
		respondError(c, http.StatusBadRequest, schemas.CodeInvalidInput, "Request validation failed", details)
		return
	}
	respondError(c, http.StatusBadRequest, schemas.CodeInvalidInput, "Request body is not valid JSON", nil)
}

// describeRule renders a single failed validation rule in plain English.
func describeRule(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "is required"
	case "email":
		return "must be a valid email address"
	case "min":
		return fmt.Sprintf("must be at least %s characters", fe.Param())
	case "max":
		return fmt.Sprintf("must be at most %s characters", fe.Param())
	default:
		return fmt.Sprintf("failed the %q rule", fe.Tag())
	}
}

// respondInternal writes a 500 without leaking the underlying error to the
// client. The detail belongs in the logs, not in the response body.
func respondInternal(c *gin.Context) {
	respondError(c, http.StatusInternalServerError, schemas.CodeInternalError, "Something went wrong", nil)
}
