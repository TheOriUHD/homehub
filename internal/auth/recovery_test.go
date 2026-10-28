package auth

import (
	"bytes"
	"math"
	"regexp"
	"strings"
	"testing"
)

func TestNewRecoveryCode_AtLeast80Bits(t *testing.T) {
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{4}(-[0-9a-hjkmnp-tv-z]{4}){3}$`).MatchString(code) {
		t.Fatalf("unexpected format %q", code)
	}
	bits := float64(recoveryCodeLen) * math.Log2(float64(len(recoveryAlphabet)))
	if bits < 80 {
		t.Fatalf("only %.0f bits of entropy", bits)
	}
	seen := map[string]bool{}
	for range 1000 {
		c, _ := NewRecoveryCode()
		if seen[c] {
			t.Fatalf("duplicate code %q in 1000 draws", c)
		}
		seen[c] = true
	}
}

func TestRecoveryCode_SaltedHashMatchesForgivingly(t *testing.T) {
	code, _ := NewRecoveryCode()
	h1, err := hashRecoveryCode(code)
	if err != nil {
		t.Fatal(err)
	}
	h2, _ := hashRecoveryCode(code)
	if bytes.Equal(h1, h2) {
		t.Fatal("the same code hashed twice gives the same value — not salted")
	}
	for _, typed := range []string{
		code,
		strings.ToUpper(code),
		strings.ReplaceAll(code, "-", ""),
		" " + strings.ReplaceAll(code, "-", " ") + " ",
	} {
		if !recoveryCodeMatches(h1, typed) {
			t.Fatalf("%q did not match its own code %q", typed, code)
		}
	}
	other, _ := NewRecoveryCode()
	if recoveryCodeMatches(h1, other) {
		t.Fatal("a different code matched")
	}
	if recoveryCodeMatches(h1, "") {
		t.Fatal("empty input matched")
	}
}

func TestRecoveryCode_LookalikesReadAsDigits(t *testing.T) {
	h, _ := hashRecoveryCode("0001-1111-abcd-efgh")
	if !recoveryCodeMatches(h, "OOO1-iIlL-ABCD-EFGH") {
		t.Fatal("o/i/l were not read as 0/1")
	}
}

// Codes issued before the upgrade (10 digits, bare SHA-256) must keep
// working until the user regenerates them.
func TestRecoveryCode_LegacyCodesStillVerify(t *testing.T) {
	legacy := HashToken("1234567890")
	if !recoveryCodeMatches(legacy, "12345-67890") {
		t.Fatal("legacy code no longer verifies")
	}
	if recoveryCodeMatches(legacy, "12345-67891") {
		t.Fatal("wrong legacy code matched")
	}
	if recoveryCodeMatches(legacy, "abcd-efgh") {
		t.Fatal("non-digit input matched a legacy code")
	}
	// Unknown stored formats never match.
	if recoveryCodeMatches([]byte{0x09, 1, 2}, "1234567890") {
		t.Fatal("garbage stored value matched")
	}
}
