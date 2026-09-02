// Package schemas holds the request and response shapes of the HTTP API. They
// are kept separate from the database models so that a change to a document
// never silently changes the public contract.
package schemas

// Error codes used across the API.
const (
	CodeInvalidInput  = "INVALID_INPUT"
	CodeUnauthorized  = "UNAUTHORIZED"
	CodeForbidden     = "FORBIDDEN"
	CodeNotFound      = "NOT_FOUND"
	CodeRateLimited   = "RATE_LIMITED"
	CodeInternalError = "INTERNAL_ERROR"
)

// ErrorEnvelope is the single shape every failure in this API is returned in,
// exactly as api_spec.md defines it:
//
//	{ "error": { "code": "...", "message": "...", "details": null } }
type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody carries the machine-readable code, the human-readable message, and
// optional structured detail such as a per-field validation breakdown.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details"`
}

// NewError builds an error envelope.
func NewError(code, message string, details any) ErrorEnvelope {
	return ErrorEnvelope{Error: ErrorBody{Code: code, Message: message, Details: details}}
}
