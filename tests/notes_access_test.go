// Access-control tests for the note endpoints.
//
// These run against a fake store rather than MongoDB, so they exercise the
// full Gin stack — routing, binding, validation, ownership and the error
// envelope — with no database. Ownership is the most security-sensitive logic
// in the project, which is why it is tested first, ahead of the wider suite
// planned for Phase 6.

// Package tests holds the integration-level tests for the HTTP layer.
package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/handlers"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/PalashSinha14/evernote/internal/models"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

type fakeStore struct{ notes map[string]*models.Note }

func (f *fakeStore) Create(_ context.Context, n *models.Note) error {
	n.ID = bson.NewObjectID()
	if n.Tags == nil {
		n.Tags = []string{}
	}
	f.notes[n.ID.Hex()] = n
	return nil
}
func (f *fakeStore) GetByID(_ context.Context, id string) (*models.Note, error) {
	n, ok := f.notes[id]
	if !ok || n.IsDeleted {
		return nil, db.ErrNotFound
	}
	return n, nil
}
func (f *fakeStore) Update(_ context.Context, id string, owner bson.ObjectID, u db.NoteUpdate) (*models.Note, error) {
	n, ok := f.notes[id]
	if !ok || n.IsDeleted || n.OwnerID != owner {
		return nil, db.ErrNotFound
	}
	if u.Title != nil {
		n.Title = *u.Title
	}
	if u.Body != nil {
		n.Body = *u.Body
	}
	if u.Tags != nil {
		n.Tags = *u.Tags
	}
	if u.IsPublic != nil {
		n.IsPublic = *u.IsPublic
	}
	return n, nil
}

// List mirrors the repository's filtering, sorting and paging in memory. The
// q filter is a substring match standing in for MongoDB's text index, which is
// enough to prove the handler wires the parameter through.
func (f *fakeStore) List(_ context.Context, flt db.NoteFilter) ([]models.Note, int64, error) {
	var matched []models.Note
	for _, n := range f.notes {
		if n.IsDeleted || n.OwnerID != flt.OwnerID {
			continue
		}
		if flt.Tag != "" && !slices.Contains(n.Tags, flt.Tag) {
			continue
		}
		if flt.Query != "" &&
			!strings.Contains(strings.ToLower(n.Title), strings.ToLower(flt.Query)) &&
			!strings.Contains(strings.ToLower(n.Body), strings.ToLower(flt.Query)) {
			continue
		}
		matched = append(matched, *n)
	}

	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]
		if flt.SortField == "created_at" {
			if flt.SortDesc {
				return a.CreatedAt.After(b.CreatedAt)
			}
			return a.CreatedAt.Before(b.CreatedAt)
		}
		if flt.SortDesc {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
		return a.UpdatedAt.Before(b.UpdatedAt)
	})

	total := int64(len(matched))
	start := (flt.Page - 1) * flt.Limit
	if start >= len(matched) {
		return []models.Note{}, total, nil
	}
	end := min(start+flt.Limit, len(matched))
	return matched[start:end], total, nil
}

func (f *fakeStore) DistinctTags(_ context.Context, owner bson.ObjectID) ([]string, error) {
	seen := map[string]struct{}{}
	for _, n := range f.notes {
		if n.IsDeleted || n.OwnerID != owner {
			continue
		}
		for _, t := range n.Tags {
			seen[t] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

func (f *fakeStore) SoftDelete(_ context.Context, id string, owner bson.ObjectID) error {
	n, ok := f.notes[id]
	if !ok || n.IsDeleted || n.OwnerID != owner {
		return db.ErrNotFound
	}
	n.IsDeleted = true
	return nil
}

func router(store handlers.NoteStore, asUser string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewNoteHandler(store)
	r.GET("/api/v1/tags", func(c *gin.Context) { c.Set(middleware.ContextUserID, asUser); c.Next() }, h.Tags)
	g := r.Group("/api/v1/notes", func(c *gin.Context) { c.Set(middleware.ContextUserID, asUser); c.Next() })
	g.GET("", h.List)
	g.POST("", h.Create)
	g.GET("/:id", h.Get)
	g.PUT("/:id", h.Update)
	g.DELETE("/:id", h.Delete)
	return r
}

func do(t *testing.T, r *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestNotesAccessControl(t *testing.T) {
	alice, bob := bson.NewObjectID(), bson.NewObjectID()
	store := &fakeStore{notes: map[string]*models.Note{}}
	ra := router(store, alice.Hex())
	rb := router(store, bob.Hex())

	// CREATE — 201, tags normalised and de-duplicated.
	w := do(t, ra, "POST", "/api/v1/notes", `{"title":"Shopping","body":"Milk","tags":["Work"," work ","IDEA"],"is_public":true}`)
	if w.Code != 201 {
		t.Fatalf("create = %d, want 201: %s", w.Code, w.Body)
	}
	var created struct {
		ID string `json:"id"`
	}
	json.Unmarshal(w.Body.Bytes(), &created)
	if got := store.notes[created.ID].Tags; len(got) != 2 || got[0] != "work" || got[1] != "idea" {
		t.Fatalf("tags = %v, want [work idea]", got)
	}

	// GET — owner sees it.
	if w := do(t, ra, "GET", "/api/v1/notes/"+created.ID, ""); w.Code != 200 {
		t.Fatalf("owner get = %d, want 200", w.Code)
	}

	// ACCESS CONTROL — Bob is refused on all three verbs.
	for _, tc := range []struct{ method, body string }{
		{"GET", ""}, {"PUT", `{"title":"hacked"}`}, {"DELETE", ""},
	} {
		if w := do(t, rb, tc.method, "/api/v1/notes/"+created.ID, tc.body); w.Code != 403 {
			t.Fatalf("bob %s = %d, want 403", tc.method, w.Code)
		}
	}
	if store.notes[created.ID].Title != "Shopping" {
		t.Fatal("bob's forbidden PUT still mutated the note")
	}

	// 404 for a well-formed but unknown id, and for a malformed one.
	for _, id := range []string{bson.NewObjectID().Hex(), "not-an-objectid"} {
		if w := do(t, ra, "GET", "/api/v1/notes/"+id, ""); w.Code != 404 {
			t.Fatalf("get %q = %d, want 404", id, w.Code)
		}
	}

	// PARTIAL UPDATE — omitting is_public must not reset it to false.
	w = do(t, ra, "PUT", "/api/v1/notes/"+created.ID, `{"title":"Groceries"}`)
	if w.Code != 200 {
		t.Fatalf("update = %d, want 200: %s", w.Code, w.Body)
	}
	n := store.notes[created.ID]
	if n.Title != "Groceries" || !n.IsPublic || n.Body != "Milk" {
		t.Fatalf("partial update clobbered fields: %+v", n)
	}

	// Explicit false must still be applied.
	do(t, ra, "PUT", "/api/v1/notes/"+created.ID, `{"is_public":false}`)
	if store.notes[created.ID].IsPublic {
		t.Fatal("explicit is_public:false was ignored")
	}

	// VALIDATION — 400s.
	for name, body := range map[string]string{
		"empty object":  `{}`,
		"missing title": `{"body":"x"}`,
		"blank title":   `{"title":""}`,
		"broken json":   `{"title":`,
	} {
		verb, path := "PUT", "/api/v1/notes/"+created.ID
		if name == "missing title" || name == "blank title" {
			verb, path = "POST", "/api/v1/notes"
		}
		if w := do(t, ra, verb, path, body); w.Code != 400 {
			t.Fatalf("%s = %d, want 400: %s", name, w.Code, w.Body)
		}
	}

	// Error envelope shape.
	w = do(t, ra, "POST", "/api/v1/notes", `{"body":"x"}`)
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(w.Body.Bytes(), &env); env.Error.Code != "INVALID_INPUT" {
		t.Fatalf("error envelope = %s", w.Body)
	}

	// DELETE — 204, then gone (404), and a second delete is 404 not 204.
	if w := do(t, ra, "DELETE", "/api/v1/notes/"+created.ID, ""); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", w.Code)
	}
	if !store.notes[created.ID].IsDeleted {
		t.Fatal("delete was hard, not soft — document should still exist")
	}
	for _, m := range []string{"GET", "DELETE"} {
		if w := do(t, ra, m, "/api/v1/notes/"+created.ID, ""); w.Code != 404 {
			t.Fatalf("%s after delete = %d, want 404", m, w.Code)
		}
	}
}

// listBody is the { data, meta } envelope from GET /api/v1/notes.
type listBody struct {
	Data []struct {
		ID    string   `json:"id"`
		Title string   `json:"title"`
		Tags  []string `json:"tags"`
	} `json:"data"`
	Meta struct {
		Page  int   `json:"page"`
		Limit int   `json:"limit"`
		Total int64 `json:"total"`
	} `json:"meta"`
}

func TestNotesListing(t *testing.T) {
	alice, bob := bson.NewObjectID(), bson.NewObjectID()
	store := &fakeStore{notes: map[string]*models.Note{}}

	base := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	seed := func(owner bson.ObjectID, title, body string, tags []string, ageDays int, deleted bool) {
		id := bson.NewObjectID()
		ts := base.AddDate(0, 0, ageDays)
		store.notes[id.Hex()] = &models.Note{
			ID: id, OwnerID: owner, Title: title, Body: body, Tags: tags,
			IsDeleted: deleted, CreatedAt: ts, UpdatedAt: ts,
		}
	}
	seed(alice, "Alpha", "milk and eggs", []string{"shopping"}, 1, false)
	seed(alice, "Beta", "quarterly report", []string{"work"}, 2, false)
	seed(alice, "Gamma", "milk run", []string{"work", "shopping"}, 3, false)
	seed(alice, "Deleted", "gone", []string{"work"}, 4, true)
	seed(bob, "Bob's note", "private", []string{"work"}, 5, false)

	ra := router(store, alice.Hex())
	get := func(t *testing.T, path string) listBody {
		t.Helper()
		w := do(t, ra, "GET", path, "")
		if w.Code != 200 {
			t.Fatalf("%s = %d, want 200: %s", path, w.Code, w.Body)
		}
		var b listBody
		if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}

	// Defaults: page 1, limit 20, newest updated first. Bob's note and the
	// deleted one are both absent.
	b := get(t, "/api/v1/notes")
	if b.Meta.Page != 1 || b.Meta.Limit != 20 || b.Meta.Total != 3 {
		t.Fatalf("meta = %+v, want page 1 limit 20 total 3", b.Meta)
	}
	if len(b.Data) != 3 || b.Data[0].Title != "Gamma" {
		t.Fatalf("default sort wrong: %+v", b.Data)
	}

	// Ascending by created_at flips the order.
	if b := get(t, "/api/v1/notes?sort=created_at"); b.Data[0].Title != "Alpha" {
		t.Fatalf("ascending sort wrong: %+v", b.Data)
	}

	// Pagination: total stays the size of the whole result set, not the page.
	b = get(t, "/api/v1/notes?page=2&limit=2")
	if len(b.Data) != 1 || b.Meta.Total != 3 || b.Meta.Page != 2 {
		t.Fatalf("page 2 = %+v meta %+v", b.Data, b.Meta)
	}
	// A page past the end is an empty list, not an error, and never null.
	if b := get(t, "/api/v1/notes?page=99"); b.Data == nil || len(b.Data) != 0 {
		t.Fatalf("page past end = %+v, want []", b.Data)
	}

	// Tag filter, including case normalisation of the query parameter.
	for _, q := range []string{"tag=shopping", "tag=SHOPPING", "tag=+shopping+"} {
		if b := get(t, "/api/v1/notes?"+strings.ReplaceAll(q, "+", "%20")); b.Meta.Total != 2 {
			t.Fatalf("%s total = %d, want 2", q, b.Meta.Total)
		}
	}

	// Text search reaches both title and body.
	if b := get(t, "/api/v1/notes?q=milk"); b.Meta.Total != 2 {
		t.Fatalf("q=milk total = %d, want 2", b.Meta.Total)
	}
	// Combined filters intersect.
	if b := get(t, "/api/v1/notes?q=milk&tag=work"); b.Meta.Total != 1 {
		t.Fatalf("q+tag total = %d, want 1", b.Meta.Total)
	}

	// Rejected query strings.
	for _, bad := range []string{
		"sort=password_hash", "sort=owner_id", "sort=-title",
		"limit=101", "limit=0&page=-1",
	} {
		if w := do(t, ra, "GET", "/api/v1/notes?"+bad, ""); w.Code != 400 {
			t.Fatalf("?%s = %d, want 400: %s", bad, w.Code, w.Body)
		}
	}

	// Tags aggregation is scoped to the caller and excludes deleted notes.
	w := do(t, ra, "GET", "/api/v1/tags", "")
	if w.Code != 200 {
		t.Fatalf("tags = %d", w.Code)
	}
	var tags struct {
		Data []string `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &tags)
	if !slices.Equal(tags.Data, []string{"shopping", "work"}) {
		t.Fatalf("tags = %v, want [shopping work]", tags.Data)
	}
}
