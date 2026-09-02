package utils

import "strings"

// NormaliseEmail lowercases and trims an address so that the unique index on
// users.email treats "Alice@Example.com" and "alice@example.com" as the same
// account rather than as two.
func NormaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// NormaliseTags lowercases, trims and de-duplicates a tag list while keeping
// the caller's ordering.
//
// Normalising on the way in is what makes the ?tag= filter and the tag
// aggregation in Phase 3 behave: without it "Work", "work" and " work " would
// be three separate tags, and a filter for one would miss notes carrying the
// others. Empty tags are dropped rather than rejected — a stray comma in a
// client's input is not worth a 400.
func NormaliseTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, dup := seen[t]; dup {
			continue
		}
		seen[t] = struct{}{}
		out = append(out, t)
	}
	return out
}

// NormaliseTag applies the same normalisation as NormaliseTags to a single
// value. The ?tag= filter runs through it so that a query for "Work" matches
// notes stored with "work" — the filter and the stored values have to be
// normalised the same way or the index lookup silently returns nothing.
func NormaliseTag(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}
