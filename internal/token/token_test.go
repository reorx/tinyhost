package token

import (
	"regexp"
	"testing"
)

// Behavior: tokens contain 32 random bytes and only their SHA-256 hashes are retained.
func TestTokensAreUniqueAndHashesCompare(t *testing.T) {
	a, b := New(), New()
	if a == b || !regexp.MustCompile(`^th_[A-Za-z0-9_-]{43}$`).MatchString(a) {
		t.Fatal(a, b)
	}
	if Hash("abc") != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatal("incorrect SHA-256")
	}
	if !Equal(Hash(a), Hash(a)) || Equal(Hash(a), Hash(b)) || Equal("a", "aa") {
		t.Fatal("hash comparison")
	}
}
