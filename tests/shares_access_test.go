// Tests for the sharing endpoints: minting a share link, and the public,
// unauthenticated read through it.
//
// Like notes_access_test.go, these run against fake stores rather than
// MongoDB, so they cover routing, ownership, password and expiry logic, and
// the response shape — with no database required.

package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PalashSinha14/evernote/internal/config"
	"github.com/PalashSinha14/evernote/internal/db"
	"github.com/PalashSinha14/evernote/internal/handlers"
	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/PalashSinha14/evernote/internal/models"
	"github.com/gin-gonic/gin"
	"go.mongodb.org/mongo-driver/v2/bson"
)

// fakeShareStore is an in-memory stand-in for db.ShareRepo.
type fakeShareStore struct {
	byToken map[string]*models.Share
	byID    map[string]*models.Share
}

func newFakeShareStore() *fakeShareStore {
	return &fakeShareStore{byToken: map[string]*models.Share{}, byID: map[string]*models.Share{}}
}

func (f *fakeShareStore) Create(_ context.Context, s *models.Share) error {
	if _, exists := f.byToken[s.Token]; exists {
		return db.ErrDuplicateToken
	}
	s.ID = bson.NewObjectID()
	f.byToken[s.Token] = s
	f.byID[s.ID.Hex()] = s
	return nil
}

func (f *fakeShareStore) FindByToken(_ context.Context, token string) (*models.Share, error) {
	s, ok := f.byToken[token]
	if !ok {
		return nil, db.ErrNotFound
	}
	return s, nil
}

func (f *fakeShareStore) IncrementClicks(_ context.Context, id bson.ObjectID) error {
	s, ok := f.byID[id.Hex()]
	if !ok {
		return db.ErrNotFound
	}
	s.Clicks++
	return nil
}

// shareRouter builds a router exposing both the authenticated share-creation
// route and the public share-access route, exactly as app.go registers them:
// the second carries no auth middleware at all.
func shareRouter(notes handlers.NoteLookup, shares handlers.ShareStore, cfg *config.Config, asUser string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := handlers.NewShareHandler(notes, shares, cfg)

	r.GET("/s/:token", h.Access)

	authed := r.Group("/api/v1/notes", func(c *gin.Context) {
		c.Set(middleware.ContextUserID, asUser)
		c.Next()
	})
	authed.POST("/:id/share", h.Create)
	return r
}

func testCfg() *config.Config {
	return &config.Config{AppBaseURL: "https://evernote.example", BcryptCost: 4}
}

type createShareBody struct {
	ShareID   string     `json:"share_id"`
	URL       string     `json:"url"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func TestShareCreateAndAccess(t *testing.T) {
	alice, bob := bson.NewObjectID(), bson.NewObjectID()
	notes := &fakeStore{notes: map[string]*models.Note{}}
	shares := newFakeShareStore()
	cfg := testCfg()

	noteID := bson.NewObjectID()
	notes.notes[noteID.Hex()] = &models.Note{
		ID: noteID, OwnerID: alice, Title: "Recipe", Body: "Flour and water",
		Tags: []string{"cooking"}, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}

	ra := shareRouter(notes, shares, cfg, alice.Hex())
	rb := shareRouter(notes, shares, cfg, bob.Hex())
	anon := shareRouter(notes, shares, cfg, "") // no auth needed for /s/:token

	// A non-owner cannot mint a share for someone else's note.
	if w := do(t, rb, "POST", "/api/v1/notes/"+noteID.Hex()+"/share", ""); w.Code != http.StatusForbidden {
		t.Fatalf("bob create share = %d, want 403: %s", w.Code, w.Body)
	}

	// Sharing a note that does not exist.
	if w := do(t, ra, "POST", "/api/v1/notes/"+bson.NewObjectID().Hex()+"/share", ""); w.Code != http.StatusNotFound {
		t.Fatalf("share unknown note = %d, want 404", w.Code)
	}

	// Owner mints a plain share, no password, no expiry, empty body.
	w := do(t, ra, "POST", "/api/v1/notes/"+noteID.Hex()+"/share", "")
	if w.Code != http.StatusCreated {
		t.Fatalf("create share = %d, want 201: %s", w.Code, w.Body)
	}
	var plain createShareBody
	if err := json.Unmarshal(w.Body.Bytes(), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.ExpiresAt != nil {
		t.Fatalf("expires_at = %v, want nil for a share with no expiry", plain.ExpiresAt)
	}
	wantPrefix := "https://evernote.example/s/"
	if len(plain.URL) <= len(wantPrefix) || plain.URL[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("url = %q, want prefix %q", plain.URL, wantPrefix)
	}
	plainToken := plain.URL[len(wantPrefix):]

	// share_id is the share document's own id, not the token — it must not
	// equal the value embedded in the url.
	if plain.ShareID == plainToken {
		t.Fatal("share_id leaked the token; it should be the share's database id")
	}

	// Anonymous access, no token required by the caller.
	w = do(t, anon, "GET", "/s/"+plainToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("anon access = %d, want 200: %s", w.Code, w.Body)
	}
	var body map[string]any
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["title"] != "Recipe" || body["body"] != "Flour and water" {
		t.Fatalf("shared note content wrong: %v", body)
	}
	for _, leaked := range []string{"id", "owner_id", "is_public"} {
		if _, present := body[leaked]; present {
			t.Fatalf("response leaked field %q: %v", leaked, body)
		}
	}

	// Click counting: only successful reads count, and each one adds exactly one.
	if shares.byToken[plainToken].Clicks != 1 {
		t.Fatalf("clicks after one read = %d, want 1", shares.byToken[plainToken].Clicks)
	}
	do(t, anon, "GET", "/s/"+plainToken, "")
	if shares.byToken[plainToken].Clicks != 2 {
		t.Fatalf("clicks after two reads = %d, want 2", shares.byToken[plainToken].Clicks)
	}

	// An unknown token.
	if w := do(t, anon, "GET", "/s/does-not-exist", ""); w.Code != http.StatusNotFound {
		t.Fatalf("unknown token = %d, want 404", w.Code)
	}

	// A password-protected share.
	w = do(t, ra, "POST", "/api/v1/notes/"+noteID.Hex()+"/share", `{"password":"correct-horse"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create protected share = %d: %s", w.Code, w.Body)
	}
	var protected createShareBody
	json.Unmarshal(w.Body.Bytes(), &protected)
	protectedToken := protected.URL[len(wantPrefix):]

	// No password supplied.
	if w := do(t, anon, "GET", "/s/"+protectedToken, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no password = %d, want 401: %s", w.Code, w.Body)
	}
	// Wrong password.
	if w := doWithHeader(t, anon, "GET", "/s/"+protectedToken, "wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401", w.Code)
	}
	// Failed attempts must not have counted as reads.
	if shares.byToken[protectedToken].Clicks != 0 {
		t.Fatalf("clicks after failed auth = %d, want 0", shares.byToken[protectedToken].Clicks)
	}
	// Correct password.
	if w := doWithHeader(t, anon, "GET", "/s/"+protectedToken, "correct-horse"); w.Code != http.StatusOK {
		t.Fatalf("correct password = %d, want 200: %s", w.Code, w.Body)
	}
	if shares.byToken[protectedToken].Clicks != 1 {
		t.Fatalf("clicks after a genuine read = %d, want 1", shares.byToken[protectedToken].Clicks)
	}

	// Expiry: create with a positive expires_in, then simulate time passing by
	// rewriting the fake store's expiry directly, since the API cannot mint an
	// already-expired share.
	w = do(t, ra, "POST", "/api/v1/notes/"+noteID.Hex()+"/share", `{"expires_in":3600}`)
	var timed createShareBody
	json.Unmarshal(w.Body.Bytes(), &timed)
	if timed.ExpiresAt == nil {
		t.Fatal("expires_at missing for a timed share")
	}
	wantExpiry := time.Now().UTC().Add(3600 * time.Second)
	if diff := timed.ExpiresAt.Sub(wantExpiry); diff < -5*time.Second || diff > 5*time.Second {
		t.Fatalf("expires_at = %v, want close to %v", timed.ExpiresAt, wantExpiry)
	}
	timedToken := timed.URL[len(wantPrefix):]

	if w := do(t, anon, "GET", "/s/"+timedToken, ""); w.Code != http.StatusOK {
		t.Fatalf("unexpired share = %d, want 200", w.Code)
	}
	past := time.Now().UTC().Add(-time.Hour)
	shares.byToken[timedToken].ExpiresAt = &past
	if w := do(t, anon, "GET", "/s/"+timedToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("expired share = %d, want 404", w.Code)
	}

	// Validation on creation.
	for name, body := range map[string]string{
		"zero expiry":     `{"expires_in":0}`,
		"negative expiry": `{"expires_in":-10}`,
		"empty password":  `{"password":""}`,
	} {
		if w := do(t, ra, "POST", "/api/v1/notes/"+noteID.Hex()+"/share", body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: %s", name, w.Code, w.Body)
		}
	}

	// Soft delete on the note cascades: the share document still exists, but
	// the note it points to is gone, so the link stops resolving.
	notes.notes[noteID.Hex()].IsDeleted = true
	if w := do(t, anon, "GET", "/s/"+plainToken, ""); w.Code != http.StatusNotFound {
		t.Fatalf("share to deleted note = %d, want 404", w.Code)
	}
}

// doWithHeader is do() with an X-Share-Password header attached, for the
// password-gated share tests.
func doWithHeader(t *testing.T, r *gin.Engine, method, path, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Share-Password", password)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
