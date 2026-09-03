// Tests for the auth endpoints: signup, login, logout and the profile route.
// Like the other handler tests, these run against fake stores through the
// real Gin router and RequireAuth middleware, with no MongoDB required.

package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// fakeUserStore is an in-memory stand-in for db.UserRepo.
type fakeUserStore struct {
	byID    map[string]*models.User
	byEmail map[string]*models.User
}

func newFakeUserStore() *fakeUserStore {
	return &fakeUserStore{byID: map[string]*models.User{}, byEmail: map[string]*models.User{}}
}

func (f *fakeUserStore) Create(_ context.Context, u *models.User) error {
	if _, exists := f.byEmail[u.Email]; exists {
		return db.ErrDuplicateEmail
	}
	u.ID = bson.NewObjectID()
	u.CreatedAt, u.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	f.byID[u.ID.Hex()] = u
	f.byEmail[u.Email] = u
	return nil
}

func (f *fakeUserStore) FindByEmail(_ context.Context, email string) (*models.User, error) {
	u, ok := f.byEmail[email]
	if !ok {
		return nil, db.ErrNotFound
	}
	return u, nil
}

func (f *fakeUserStore) FindByID(_ context.Context, id string) (*models.User, error) {
	u, ok := f.byID[id]
	if !ok {
		return nil, db.ErrNotFound
	}
	return u, nil
}

func (f *fakeUserStore) TouchLastLogin(_ context.Context, id bson.ObjectID) error {
	u, ok := f.byID[id.Hex()]
	if !ok {
		return db.ErrNotFound
	}
	now := time.Now().UTC()
	u.LastLogin = &now
	return nil
}

// fakeRevocationStore is an in-memory stand-in for db.RevokedTokenRepo,
// shared between AuthHandler (which writes to it) and RequireAuth (which
// reads from it) — exactly as the real repository is shared in app.go.
type fakeRevocationStore struct {
	revoked map[string]bool
}

func newFakeRevocationStore() *fakeRevocationStore {
	return &fakeRevocationStore{revoked: map[string]bool{}}
}

func (f *fakeRevocationStore) Revoke(_ context.Context, jti string, _ bson.ObjectID, _ time.Time) error {
	f.revoked[jti] = true
	return nil
}

func (f *fakeRevocationStore) IsRevoked(_ context.Context, jti string) (bool, error) {
	return f.revoked[jti], nil
}

// authRouter builds a router wired exactly as app.go wires the auth routes:
// signup and login open, logout and me behind the real RequireAuth
// middleware — so these tests exercise actual JWT verification, not a
// stand-in for it.
func authRouter(t *testing.T, users handlers.UserStore, revoked *fakeRevocationStore, secret string) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	cfg := &config.Config{JWTSecret: secret, JWTExpiry: time.Hour, BcryptCost: 4}
	h := handlers.NewAuthHandler(users, revoked, cfg)
	requireAuth := middleware.RequireAuth(secret, revoked)

	authGroup := r.Group("/api/v1/auth")
	authGroup.POST("/signup", h.Signup)
	authGroup.POST("/login", h.Login)
	authGroup.POST("/logout", requireAuth, h.Logout)
	r.GET("/api/v1/me", requireAuth, h.Me)
	return r
}

func TestSignupAndLogin(t *testing.T) {
	users := newFakeUserStore()
	revoked := newFakeRevocationStore()
	r := authRouter(t, users, revoked, "test-secret")

	// Signup.
	w := do(t, r, "POST", "/api/v1/auth/signup",
		`{"name":"Ada","email":"Ada@Example.com","password":"correct-horse"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("signup = %d, want 201: %s", w.Code, w.Body)
	}
	var signup struct {
		ID    string `json:"id"`
		Email string `json:"email"`
	}
	json.Unmarshal(w.Body.Bytes(), &signup)
	if signup.Email != "ada@example.com" {
		t.Fatalf("email = %q, want lowercased ada@example.com", signup.Email)
	}
	// The password hash must never appear in the response.
	if strings.Contains(w.Body.String(), "password") {
		t.Fatalf("signup response leaked a password field: %s", w.Body)
	}

	// Duplicate email, different case, is still a duplicate.
	w = do(t, r, "POST", "/api/v1/auth/signup",
		`{"name":"Ada 2","email":"ADA@EXAMPLE.COM","password":"another-password"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("duplicate signup = %d, want 400: %s", w.Code, w.Body)
	}

	// Validation: short password, missing fields, bad email.
	for name, body := range map[string]string{
		"short password": `{"name":"X","email":"x@example.com","password":"short"}`,
		"missing name":   `{"email":"y@example.com","password":"correct-horse"}`,
		"bad email":      `{"name":"X","email":"not-an-email","password":"correct-horse"}`,
	} {
		if w := do(t, r, "POST", "/api/v1/auth/signup", body); w.Code != http.StatusBadRequest {
			t.Fatalf("%s = %d, want 400: %s", name, w.Code, w.Body)
		}
	}

	// Wrong password.
	w = do(t, r, "POST", "/api/v1/auth/login", `{"email":"ada@example.com","password":"wrong"}`)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password = %d, want 401: %s", w.Code, w.Body)
	}

	// Unknown email gets the identical response to a wrong password — this is
	// asserted byte for byte, since the whole point is that the two must be
	// indistinguishable to the caller.
	wUnknown := do(t, r, "POST", "/api/v1/auth/login", `{"email":"nobody@example.com","password":"whatever"}`)
	if wUnknown.Code != w.Code || wUnknown.Body.String() != w.Body.String() {
		t.Fatalf("unknown email response differs from wrong password response:\n%s\nvs\n%s",
			wUnknown.Body, w.Body)
	}

	// Correct login, case-insensitive email.
	w = do(t, r, "POST", "/api/v1/auth/login", `{"email":"ADA@Example.com","password":"correct-horse"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200: %s", w.Code, w.Body)
	}
	var login struct {
		Token     string `json:"token"`
		ExpiresIn int    `json:"expires_in"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)
	if login.Token == "" || login.ExpiresIn != 3600 {
		t.Fatalf("login body = %+v", login)
	}
	if users.byEmail["ada@example.com"].LastLogin == nil {
		t.Fatal("last_login was not stamped")
	}

	// The token actually authenticates on a protected route.
	wMe := doAuthed(t, r, "GET", "/api/v1/me", "", login.Token)
	if wMe.Code != http.StatusOK {
		t.Fatalf("me = %d, want 200: %s", wMe.Code, wMe.Body)
	}
	var me struct {
		Email string `json:"email"`
	}
	json.Unmarshal(wMe.Body.Bytes(), &me)
	if me.Email != "ada@example.com" {
		t.Fatalf("me email = %q", me.Email)
	}

	// No token, and a garbage token, are both refused.
	if w := do(t, r, "GET", "/api/v1/me", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", w.Code)
	}
	if w := doAuthed(t, r, "GET", "/api/v1/me", "", "not-a-real-token"); w.Code != http.StatusUnauthorized {
		t.Fatalf("garbage token = %d, want 401", w.Code)
	}
}

func TestLogoutRevokesTheToken(t *testing.T) {
	users := newFakeUserStore()
	revoked := newFakeRevocationStore()
	r := authRouter(t, users, revoked, "test-secret")

	do(t, r, "POST", "/api/v1/auth/signup", `{"name":"Bob","email":"bob@example.com","password":"correct-horse"}`)
	w := do(t, r, "POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"correct-horse"}`)
	var login struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)

	// Works before logout.
	if w := doAuthed(t, r, "GET", "/api/v1/me", "", login.Token); w.Code != http.StatusOK {
		t.Fatalf("me before logout = %d, want 200", w.Code)
	}

	// Logout.
	if w := doAuthed(t, r, "POST", "/api/v1/auth/logout", "", login.Token); w.Code != http.StatusNoContent {
		t.Fatalf("logout = %d, want 204: %s", w.Code, w.Body)
	}

	// The exact same token is now refused everywhere it would have worked.
	if w := doAuthed(t, r, "GET", "/api/v1/me", "", login.Token); w.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout = %d, want 401", w.Code)
	}
	if w := doAuthed(t, r, "POST", "/api/v1/auth/logout", "", login.Token); w.Code != http.StatusUnauthorized {
		t.Fatalf("second logout with the same token = %d, want 401", w.Code)
	}

	// A fresh login issues a different token, unaffected by the earlier
	// token's revocation.
	w = do(t, r, "POST", "/api/v1/auth/login", `{"email":"bob@example.com","password":"correct-horse"}`)
	var second struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &second)
	if second.Token == login.Token {
		t.Fatal("second login returned the same token as the first")
	}
	if w := doAuthed(t, r, "GET", "/api/v1/me", "", second.Token); w.Code != http.StatusOK {
		t.Fatalf("me with the fresh token = %d, want 200", w.Code)
	}
}

func TestMeForADeletedUser(t *testing.T) {
	// A token can outlive the account it names, if the account is removed
	// after the token was issued. Me must answer 404, not 500 or a stale 200.
	users := newFakeUserStore()
	revoked := newFakeRevocationStore()
	r := authRouter(t, users, revoked, "test-secret")

	do(t, r, "POST", "/api/v1/auth/signup", `{"name":"Carl","email":"carl@example.com","password":"correct-horse"}`)
	w := do(t, r, "POST", "/api/v1/auth/login", `{"email":"carl@example.com","password":"correct-horse"}`)
	var login struct {
		Token string `json:"token"`
	}
	json.Unmarshal(w.Body.Bytes(), &login)

	delete(users.byID, users.byEmail["carl@example.com"].ID.Hex())

	if w := doAuthed(t, r, "GET", "/api/v1/me", "", login.Token); w.Code != http.StatusNotFound {
		t.Fatalf("me for a deleted user = %d, want 404: %s", w.Code, w.Body)
	}
}

// doAuthed is do() with a Bearer token attached.
func doAuthed(t *testing.T, r *gin.Engine, method, path, body, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
