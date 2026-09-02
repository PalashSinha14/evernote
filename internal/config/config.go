// Package config loads and validates every runtime setting from the
// environment. Nothing in this service is configured any other way, so that no
// secret ever has to live in the repository.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
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

	return cfg, nil
}

// env returns the environment variable named by key, or fallback when it is
// unset or empty.
func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
