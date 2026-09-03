// Package config loads and validates every runtime setting from the
// environment. Nothing in this service is configured any other way, so that no
// secret ever has to live in the repository.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// MinBcryptCost is the floor set by the API specification's security notes.
const MinBcryptCost = 10

// Config holds every setting the service needs to run.
type Config struct {
	Port       string
	MongoURI   string
	MongoDB    string
	JWTSecret  string
	JWTExpiry  time.Duration
	BcryptCost int
	AppBaseURL string

	// AllowedOrigins is the set of origins the CORS middleware permits. "*"
	// means any origin, which is a safe default here specifically because
	// authentication is a bearer token in an Authorization header rather than
	// a cookie — unlike cookie-based auth, a wildcard origin combined with
	// token auth does not expose the API to cross-site request forgery.
	AllowedOrigins []string

	// RateLimitRequests and RateLimitWindow bound how many requests one
	// caller may make in one window against the endpoints api_spec.md's
	// security notes name explicitly: auth and public share access.
	RateLimitRequests int
	RateLimitWindow   time.Duration
}

// Load reads configuration from the environment, applying defaults where the
// specification allows one and returning an error where it does not.
func Load() (*Config, error) {
	cfg := &Config{
		Port:       env("PORT", "8080"),
		MongoURI:   env("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:    env("MONGO_DB", "evernote_lite"),
		JWTSecret:  os.Getenv("JWT_SECRET"),
		AppBaseURL: env("APP_BASE_URL", "http://localhost:8080"),
	}

	// The signing key has no safe default. A service that invents one would
	// happily issue tokens that anybody could forge.
	if cfg.JWTSecret == "" {
		return nil, errors.New("JWT_SECRET is required and has no default")
	}

	expiry, err := time.ParseDuration(env("JWT_EXPIRY", "1h"))
	if err != nil {
		return nil, fmt.Errorf("JWT_EXPIRY is not a valid duration: %w", err)
	}
	if expiry <= 0 {
		return nil, errors.New("JWT_EXPIRY must be positive")
	}
	cfg.JWTExpiry = expiry

	cost, err := strconv.Atoi(env("BCRYPT_COST", "12"))
	if err != nil {
		return nil, fmt.Errorf("BCRYPT_COST is not a number: %w", err)
	}
	if cost < MinBcryptCost {
		return nil, fmt.Errorf("BCRYPT_COST must be at least %d", MinBcryptCost)
	}
	cfg.BcryptCost = cost

	cfg.AllowedOrigins = splitCSV(env("CORS_ALLOWED_ORIGINS", "*"))

	rateLimit, err := strconv.Atoi(env("RATE_LIMIT_REQUESTS", "20"))
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_REQUESTS is not a number: %w", err)
	}
	if rateLimit <= 0 {
		return nil, errors.New("RATE_LIMIT_REQUESTS must be positive")
	}
	cfg.RateLimitRequests = rateLimit

	rateWindow, err := time.ParseDuration(env("RATE_LIMIT_WINDOW", "1m"))
	if err != nil {
		return nil, fmt.Errorf("RATE_LIMIT_WINDOW is not a valid duration: %w", err)
	}
	if rateWindow <= 0 {
		return nil, errors.New("RATE_LIMIT_WINDOW must be positive")
	}
	cfg.RateLimitWindow = rateWindow

	return cfg, nil
}

// splitCSV parses a comma-separated environment value into a trimmed,
// non-empty slice.
func splitCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// env returns the environment variable named by key, or fallback when it is
// unset or empty.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
