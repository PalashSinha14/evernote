package utils

import "testing"

func TestHashPasswordAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple", 10)
	if err != nil {
		t.Fatal(err)
	}
	if hash == "correct horse battery staple" {
		t.Fatal("HashPassword returned the plaintext unchanged")
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("the correct password was rejected")
	}
	if CheckPassword(hash, "wrong password") {
		t.Fatal("an incorrect password was accepted")
	}
}

func TestHashPasswordIsSalted(t *testing.T) {
	// Two hashes of the same password must differ, or a shared salt would
	// let identical passwords be spotted by comparing hashes directly.
	a, err := HashPassword("same-password", 10)
	if err != nil {
		t.Fatal(err)
	}
	b, err := HashPassword("same-password", 10)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("hashing the same password twice produced identical hashes")
	}
	if !CheckPassword(a, "same-password") || !CheckPassword(b, "same-password") {
		t.Fatal("both salted hashes must still verify the original password")
	}
}

func TestCheckPasswordAgainstAMalformedHash(t *testing.T) {
	if CheckPassword("not-a-real-bcrypt-hash", "anything") {
		t.Fatal("a malformed hash was reported as matching")
	}
}
