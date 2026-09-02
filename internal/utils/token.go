package utils

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// ErrInvalidToken is returned for any token that fails to parse, fails
// signature verification, or has expired. The caller is told no more than
// that, deliberately.
var ErrInvalidToken = errors.New("invalid or expired token")

// Claims is the payload carried by an access token.
//
// RegisteredClaims supplies the standard fields this service relies on:
// Subject holds the user's ID, ExpiresAt drives expiry, and ID holds the jti —
// a unique identifier per token, which is what makes logout possible. Without
// a jti there would be nothing specific to revoke.
type Claims struct {
	jwt.RegisteredClaims
}

// UserID returns the user this token was issued to.
func (c *Claims) UserID() string { return c.Subject }

// JTI returns the token's unique identifier.
func (c *Claims) JTI() string { return c.ID }

// RandomToken returns n cryptographically random bytes as a hex string. It
// backs both jti generation and, later, public share tokens.
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generating random token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// MintToken signs an access token for userID and returns it alongside its
// lifetime in seconds, which is what the login response reports as expires_in.
func MintToken(secret, userID string, ttl time.Duration) (string, int, error) {
	jti, err := RandomToken(16)
	if err != nil {
		return "", 0, err
	}

	now := time.Now().UTC()
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        jti,
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
	if err != nil {
		return "", 0, fmt.Errorf("signing token: %w", err)
	}
	return signed, int(ttl.Seconds()), nil
}

// ParseToken verifies a token's signature and expiry and returns its claims.
func ParseToken(secret, token string) (*Claims, error) {
	claims := &Claims{}

	// The keyfunc pins the algorithm. Without this check a token could arrive
	// claiming alg "none", or an asymmetric algorithm, and be accepted.
	_, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method %v", t.Header["alg"])
		}
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
	if err != nil {
		return nil, ErrInvalidToken
	}

	if claims.Subject == "" || claims.ID == "" || claims.ExpiresAt == nil {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
