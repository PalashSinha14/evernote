package schemas

import "time"

// Note field limits. db_schema.md warns against unbounded note bodies, so the
// ceiling is set at the edge rather than left to MongoDB's 16 MB document cap.
const (
	MaxTitleLen = 200
	MaxBodyLen  = 100000
	MaxTags     = 20
	MaxTagLen   = 50
)

// CreateNoteRequest is the body of POST /api/v1/notes.
type CreateNoteRequest struct {
	Title    string   `json:"title"     binding:"required,min=1,max=200"`
	Body     string   `json:"body"      binding:"max=100000"`
	Tags     []string `json:"tags"      binding:"max=20,dive,min=1,max=50"`
	IsPublic bool     `json:"is_public"`
}

// CreateNoteResponse is the 201 body of POST /api/v1/notes, which api_spec.md
// defines as the new id and its creation time rather than the whole resource.
type CreateNoteResponse struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// UpdateNoteRequest is the body of PUT /api/v1/notes/:id.
//
// api_spec.md marks every field optional, so each one is a pointer: a nil
// pointer means the client did not mention the field, while a non-nil pointer
// to the zero value means they explicitly sent it. Plain values could not tell
// those apart, and an update sending only a title would then also reset
// is_public to false.
type UpdateNoteRequest struct {
	Title    *string   `json:"title"     binding:"omitempty,min=1,max=200"`
	Body     *string   `json:"body"      binding:"omitempty,max=100000"`
	Tags     *[]string `json:"tags"      binding:"omitempty,max=20,dive,min=1,max=50"`
	IsPublic *bool     `json:"is_public"`
}

// NoteResponse is a note as returned by the API.
type NoteResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Tags      []string  `json:"tags"`
	IsPublic  bool      `json:"is_public"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Pagination bounds for GET /api/v1/notes, from api_spec.md section 3.
const (
	DefaultPage  = 1
	DefaultLimit = 20
	MaxLimit     = 100
)

// Sort fields the listing endpoint accepts. api_spec.md names exactly these
// two, and the value is used to build a MongoDB sort document — so it is
// matched against this list rather than passed through, and anything else is
// a 400.
const (
	SortCreatedAt = "created_at"
	SortUpdatedAt = "updated_at"
)

// ListNotesQuery is the query string of GET /api/v1/notes.
//
// Page and Limit are left at zero when absent so the handler can apply the
// documented defaults; binding only enforces the bounds on values that were
// actually supplied.
type ListNotesQuery struct {
	Q     string `form:"q"     binding:"omitempty,max=200"`
	Tag   string `form:"tag"   binding:"omitempty,max=50"`
	Page  int    `form:"page"  binding:"omitempty,min=1"`
	Limit int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Sort  string `form:"sort"  binding:"omitempty,max=32"`
}

// PageMeta is the meta block of a paginated response.
type PageMeta struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

// ListNotesResponse is the 200 body of GET /api/v1/notes.
type ListNotesResponse struct {
	Data []NoteResponse `json:"data"`
	Meta PageMeta       `json:"meta"`
}

// TagsResponse is the 200 body of GET /api/v1/tags.
type TagsResponse struct {
	Data []string `json:"data"`
}
