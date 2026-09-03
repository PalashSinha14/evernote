package utils

import (
	"testing"
	"time"
)

func TestMintTokenAndParseToken(t *testing.T) {
	token, expiresIn, err := MintToken("test-secret", "user-123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if expiresIn != 3600 {
		t.Fatalf("expires_in = %d, want 3600", expiresIn)
	}

	claims, err := ParseToken("test-secret", token)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID() != "user-123" {
		t.Fatalf("subject = %q, want user-123", claims.UserID())
	}
	if claims.JTI() == "" {
		t.Fatal("jti is empty — logout would have nothing to revoke")
	}
}

func TestMintTokenJTIsAreUnique(t *testing.T) {
	// Two tokens minted for the same user must carry different jtis, or
	// logging out of one session would revoke every session at once.
	a, _, err := MintToken("test-secret", "user-123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := MintToken("test-secret", "user-123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	claimsA, _ := ParseToken("test-secret", a)
	claimsB, _ := ParseToken("test-secret", b)
	if claimsA.JTI() == claimsB.JTI() {
		t.Fatal("two tokens for the same user share a jti")
	}
}

func TestParseTokenRejectsWrongSecret(t *testing.T) {
	token, _, err := MintToken("real-secret", "user-123", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken("wrong-secret", token); err != ErrInvalidToken {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseTokenRejectsExpiredToken(t *testing.T) {
	token, _, err := MintToken("test-secret", "user-123", -time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseToken("test-secret", token); err != ErrInvalidToken {
		t.Fatalf("err = %v, want ErrInvalidToken", err)
	}
}

func TestParseTokenRejectsGarbage(t *testing.T) {
	for _, bad := range []string{"", "not.a.jwt", "a.b.c", "Bearer abc123"} {
		if _, err := ParseToken("test-secret", bad); err != ErrInvalidToken {
			t.Fatalf("ParseToken(%q) err = %v, want ErrInvalidToken", bad, err)
		}
	}
}

func TestRandomToken(t *testing.T) {
	a, err := RandomToken(16)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RandomToken(16)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) != 32 { // 16 bytes, hex-encoded, is 32 characters
		t.Fatalf("len(RandomToken(16)) = %d, want 32", len(a))
	}
	if a == b {
		t.Fatal("two calls to RandomToken produced the same value")
	}
}
