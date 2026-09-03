// Tests for the cross-cutting middleware added in Phase 5: rate limiting,
// panic recovery, and CORS. Each is built as a small standalone router rather
// than going through app.go, since none of this depends on the note or share
// domain.

package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PalashSinha14/evernote/internal/middleware"
	"github.com/gin-gonic/gin"
)

func TestRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	limiter := middleware.NewRateLimiter(3, time.Minute)
	r.GET("/x", limiter.Middleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = "203.0.113.7:5555" // fixed caller, so all requests share one bucket
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	for i := 1; i <= 3; i++ {
		if w := get(); w.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 (within the limit)", i, w.Code)
		}
	}

	w := get()
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("request 4 = %d, want 429: %s", w.Code, w.Body)
	}
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	json.Unmarshal(w.Body.Bytes(), &env)
	if env.Error.Code != "RATE_LIMITED" {
		t.Fatalf("error code = %q, want RATE_LIMITED: %s", env.Error.Code, w.Body)
	}

	// A different caller has an independent budget.
	req := httptest.NewRequest("GET", "/x", nil)
	req.RemoteAddr = "198.51.100.9:5555"
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)
	if w2.Code != http.StatusOK {
		t.Fatalf("a different caller = %d, want 200", w2.Code)
	}
}

func TestRateLimiterWindowResets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	limiter := middleware.NewRateLimiter(1, 20*time.Millisecond)
	r.GET("/x", limiter.Middleware(), func(c *gin.Context) { c.Status(http.StatusOK) })

	get := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/x", nil)
		req.RemoteAddr = "203.0.113.7:5555"
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", w.Code)
	}
	if w := get(); w.Code != http.StatusTooManyRequests {
		t.Fatalf("second request within the window = %d, want 429", w.Code)
	}

	time.Sleep(25 * time.Millisecond)
	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("request after the window elapsed = %d, want 200", w.Code)
	}
}

func TestRecoveryReturnsStandardEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Recovery())
	r.GET("/boom", func(c *gin.Context) { panic("something went sideways") })

	req := httptest.NewRequest("GET", "/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	var env struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("panic response was not the standard envelope: %s", w.Body)
	}
	if env.Error.Code != "INTERNAL_ERROR" {
		t.Fatalf("error code = %q, want INTERNAL_ERROR", env.Error.Code)
	}
	if env.Error.Message == "" || env.Error.Message == "something went sideways" {
		// The panic value must never reach the client directly — only the
		// generic message this API uses for every other internal failure.
		t.Fatalf("panic detail leaked into the response: %q", env.Error.Message)
	}
}

func TestRecoveryDoesNotSwallowNormalRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.Recovery())
	r.GET("/ok", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"fine": true}) })

	req := httptest.NewRequest("GET", "/ok", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
}

func TestCORS(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wildcard := gin.New()
	wildcard.Use(middleware.CORS([]string{"*"}))
	hit := false
	wildcard.GET("/x", func(c *gin.Context) { hit = true; c.Status(http.StatusOK) })

	// A normal request from any origin gets the wildcard header.
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Origin", "https://anywhere.example")
	w := httptest.NewRecorder()
	wildcard.ServeHTTP(w, req)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
	if !hit {
		t.Fatal("request never reached the handler")
	}

	// A preflight OPTIONS request is answered directly and never reaches the
	// handler.
	hit = false
	preflight := httptest.NewRequest(http.MethodOptions, "/x", nil)
	preflight.Header.Set("Origin", "https://anywhere.example")
	w = httptest.NewRecorder()
	wildcard.ServeHTTP(w, preflight)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want 204", w.Code)
	}
	if hit {
		t.Fatal("preflight request reached the downstream handler")
	}

	// A restricted allowlist echoes back only an origin it recognises, and
	// varies the response on Origin so a shared cache cannot mix callers up.
	restricted := gin.New()
	restricted.Use(middleware.CORS([]string{"https://trusted.example"}))
	restricted.GET("/x", func(c *gin.Context) { c.Status(http.StatusOK) })

	reqTrusted := httptest.NewRequest("GET", "/x", nil)
	reqTrusted.Header.Set("Origin", "https://trusted.example")
	w = httptest.NewRecorder()
	restricted.ServeHTTP(w, reqTrusted)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "https://trusted.example" {
		t.Fatalf("trusted origin = %q, want https://trusted.example", got)
	}
	if got := w.Header().Get("Vary"); got != "Origin" {
		t.Fatalf("Vary header = %q, want Origin", got)
	}

	reqUntrusted := httptest.NewRequest("GET", "/x", nil)
	reqUntrusted.Header.Set("Origin", "https://untrusted.example")
	w = httptest.NewRecorder()
	restricted.ServeHTTP(w, reqUntrusted)
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("untrusted origin got a CORS header: %q", got)
	}
}
