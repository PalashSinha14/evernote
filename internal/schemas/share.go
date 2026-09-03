package schemas

import "time"

// MaxSharePasswordLen mirrors the bcrypt input limit used elsewhere.
const MaxSharePasswordLen = 72

// CreateShareRequest is the body of POST /api/v1/notes/:id/share.
//
// Both fields are optional per api_spec.md. ExpiresIn is a duration in seconds
// rather than an absolute timestamp, so the client does not need to reason
// about clock skew between itself and the server — it says "good for one day"
// rather than computing what "one day from now" means on the server's clock.
type CreateShareRequest struct {
	ExpiresIn *int64  `json:"expires_in,omitempty" binding:"omitempty,gt=0"`
	Password  *string `json:"password,omitempty"   binding:"omitempty,min=1,max=72"`
}

// CreateShareResponse is the 201 body of POST /api/v1/notes/:id/share.
type CreateShareResponse struct {
	ShareID   string     `json:"share_id"`
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// AccessShareRequest is the body a caller may send to GET /s/:token when the
// share is password protected.
//
// It arrives as a body on a GET rather than a query parameter or header
// because a password belongs in neither: query strings end up in server logs
// and browser history, and this keeps it alongside the one existing precedent
// in this API — login — which also takes a password in a JSON body.
type AccessShareRequest struct {
	Password string `json:"password"`
}

// SharedNoteResponse is the 200 body of GET /s/:token — a read-only, owner-
// blind projection of the note. It intentionally has no is_public or owner
// information: a visitor holding a valid link does not need to know anything
// about the account behind it.
type SharedNoteResponse struct {
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Tags      []string  `json:"tags"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
