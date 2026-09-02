package utils

import "strings"

// NormaliseEmail lowercases and trims an address so that the unique index on
// users.email treats "Alice@Example.com" and "alice@example.com" as the same
// account rather than as two.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
