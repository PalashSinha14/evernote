package config

import (
	"os"
	"testing"
	"time"
)

// withEnv sets the given environment variables for the duration of the test
// and restores whatever was there before, so tests never leak state into one
// another regardless of run order.
func withEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	for k, v := range vars {
		t.Setenv(k, v)
	}
}

// clearAll unsets every variable Load reads, so a test can build up exactly
// the environment it wants to check without inheriting the host's own.
func clearAll(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"PORT", "MONGO_URI", "MONGO_DB", "JWT_SECRET", "JWT_EXPIRY",
		"BCRYPT_COST", "APP_BASE_URL", "CORS_ALLOWED_ORIGINS",
		"RATE_LIMIT_REQUESTS", "RATE_LIMIT_WINDOW",
	} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	clearAll(t)
	withEnv(t, map[string]string{"JWT_SECRET": "test-secret"})

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.MongoURI != "mongodb://localhost:27017" {
		t.Errorf("MongoURI = %q", cfg.MongoURI)
	}
	if cfg.MongoDB != "evernote_lite" {
		t.Errorf("MongoDB = %q", cfg.MongoDB)
	}
	if cfg.JWTExpiry != time.Hour {
		t.Errorf("JWTExpiry = %v, want 1h", cfg.JWTExpiry)
	}
	if cfg.BcryptCost != 12 {
		t.Errorf("BcryptCost = %d, want 12", cfg.BcryptCost)
	}
	if cfg.AppBaseURL != "http://localhost:8080" {
		t.Errorf("AppBaseURL = %q", cfg.AppBaseURL)
	}
	if len(cfg.AllowedOrigins) != 1 || cfg.AllowedOrigins[0] != "*" {
		t.Errorf("AllowedOrigins = %v, want [*]", cfg.AllowedOrigins)
	}
	if cfg.RateLimitRequests != 20 {
		t.Errorf("RateLimitRequests = %d, want 20", cfg.RateLimitRequests)
	}
	if cfg.RateLimitWindow != time.Minute {
		t.Errorf("RateLimitWindow = %v, want 1m", cfg.RateLimitWindow)
	}
}

func TestLoadOverridesFromEnv(t *testing.T) {
	clearAll(t)
	withEnv(t, map[string]string{
		"JWT_SECRET":           "test-secret",
		"PORT":                 "9090",
		"BCRYPT_COST":          "14",
		"CORS_ALLOWED_ORIGINS": "https://a.example, https://b.example",
		"RATE_LIMIT_REQUESTS":  "5",
		"RATE_LIMIT_WINDOW":    "10s",
	})

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "9090" {
		t.Errorf("Port = %q, want 9090", cfg.Port)
	}
	if cfg.BcryptCost != 14 {
		t.Errorf("BcryptCost = %d, want 14", cfg.BcryptCost)
	}
	want := []string{"https://a.example", "https://b.example"}
	if len(cfg.AllowedOrigins) != 2 || cfg.AllowedOrigins[0] != want[0] || cfg.AllowedOrigins[1] != want[1] {
		t.Errorf("AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
	}
	if cfg.RateLimitRequests != 5 {
		t.Errorf("RateLimitRequests = %d, want 5", cfg.RateLimitRequests)
	}
	if cfg.RateLimitWindow != 10*time.Second {
		t.Errorf("RateLimitWindow = %v, want 10s", cfg.RateLimitWindow)
	}
}

func TestLoadRejectsMissingSecret(t *testing.T) {
	clearAll(t)
	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with no JWT_SECRET")
	}
}

func TestLoadRejectsWeakBcryptCost(t *testing.T) {
	clearAll(t)
	withEnv(t, map[string]string{"JWT_SECRET": "x", "BCRYPT_COST": "9"})
	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with BCRYPT_COST below the floor of 10")
	}
}

func TestLoadRejectsInvalidDurations(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  map[string]string
	}{
		{"bad JWT_EXPIRY", map[string]string{"JWT_SECRET": "x", "JWT_EXPIRY": "banana"}},
		{"zero JWT_EXPIRY", map[string]string{"JWT_SECRET": "x", "JWT_EXPIRY": "0s"}},
		{"negative JWT_EXPIRY", map[string]string{"JWT_SECRET": "x", "JWT_EXPIRY": "-1h"}},
		{"bad RATE_LIMIT_WINDOW", map[string]string{"JWT_SECRET": "x", "RATE_LIMIT_WINDOW": "nope"}},
		{"zero RATE_LIMIT_WINDOW", map[string]string{"JWT_SECRET": "x", "RATE_LIMIT_WINDOW": "0s"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearAll(t)
			withEnv(t, tc.env)
			if _, err := Load(); err == nil {
				t.Fatalf("Load succeeded with %s", tc.name)
			}
		})
	}
}

func TestLoadRejectsInvalidRateLimitRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"not a number", "abc"},
		{"zero", "0"},
		{"negative", "-5"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearAll(t)
			withEnv(t, map[string]string{"JWT_SECRET": "x", "RATE_LIMIT_REQUESTS": tc.value})
			if _, err := Load(); err == nil {
				t.Fatalf("Load succeeded with RATE_LIMIT_REQUESTS=%s", tc.value)
			}
		})
	}
}

func TestSplitCSVTrimsAndDropsEmpties(t *testing.T) {
	got := splitCSV(" a ,, b,c ,")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitCSV = %v, want %v", got, want)
		}
	}
}
