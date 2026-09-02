// Package utils holds small leaf helpers — hashing, tokens and time — that
// have no dependency on the rest of the application.
package utils

import "golang.org/x/crypto/bcrypt"

// MaxPasswordBytes is bcrypt's hard input limit. Anything longer is silently
// truncated by the algorithm, so it is rejected at the edge instead.
const MaxPasswordBytes = 72

// HashPassword returns the bcrypt hash of plain at the given cost.
func HashPassword(plain string, cost int) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword reports whether plain is the password behind hash.
//
// bcrypt's comparison is constant-time with respect to the hash, so a wrong
// password cannot be distinguished from a right one by timing.
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
