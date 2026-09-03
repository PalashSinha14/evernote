package utils

import "testing"

func TestNormaliseEmail(t *testing.T) {
	cases := map[string]string{
		"Ada@Example.com":  "ada@example.com",
		"  bob@x.com  ":    "bob@x.com",
		"ALREADY@LOWER.co": "already@lower.co",
	}
	for in, want := range cases {
		if got := NormaliseEmail(in); got != want {
			t.Errorf("NormaliseEmail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormaliseTags(t *testing.T) {
	got := NormaliseTags([]string{"Work", " work ", "IDEA", "", "  ", "idea"})
	want := []string{"work", "idea"}
	if len(got) != len(want) {
		t.Fatalf("NormaliseTags = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormaliseTags = %v, want %v", got, want)
		}
	}
}

func TestNormaliseTag(t *testing.T) {
	if got := NormaliseTag("  Shopping  "); got != "shopping" {
		t.Errorf("NormaliseTag = %q, want shopping", got)
	}
}
