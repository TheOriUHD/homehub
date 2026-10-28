package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"strings"
)

// Recovery codes.
//
// Current codes are 16 Crockford-base32 characters (80 bits), stored as
// recoveryHashV2 || salt || HMAC-SHA256(salt, code). The per-code salt means
// equal codes never share a stored value and a leaked table can't be
// attacked with one precomputation; 80 bits makes a fast keyed hash
// sufficient (a GPU still can't search 2^80), which matters because a login
// attempt compares against every unused code of the account.
//
// Legacy codes (10 digits, ~33 bits) were stored as a bare SHA-256 of the
// digits. They keep working until the user regenerates their codes, so
// nobody is locked out by the upgrade.

const (
	recoveryHashV2   = 0x02
	recoverySaltLen  = 16
	recoveryCodeLen  = 16
	recoveryAlphabet = "0123456789abcdefghjkmnpqrstvwxyz" // Crockford: no i, l, o, u
)

// NewRecoveryCode generates one MFA recovery code, grouped for reading:
// xxxx-xxxx-xxxx-xxxx.
func NewRecoveryCode() (string, error) {
	raw := make([]byte, recoveryCodeLen)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	var b strings.Builder
	for i, c := range raw {
		if i > 0 && i%4 == 0 {
			b.WriteByte('-')
		}
		// 256 is a multiple of 32, so the low 5 bits are uniform.
		b.WriteByte(recoveryAlphabet[c&31])
	}
	return b.String(), nil
}

// hashRecoveryCode produces the salted stored form of a new code.
func hashRecoveryCode(code string) ([]byte, error) {
	salt := make([]byte, recoverySaltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	out := make([]byte, 0, 1+recoverySaltLen+sha256.Size)
	out = append(out, recoveryHashV2)
	out = append(out, salt...)
	return append(out, recoveryMAC(salt, normalizeRecoveryCode(code))...), nil
}

func recoveryMAC(salt []byte, normalized string) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write([]byte(normalized))
	return m.Sum(nil)
}

// recoveryCodeMatches reports whether a presented code matches one stored
// value, of either generation, in constant time.
func recoveryCodeMatches(stored []byte, presented string) bool {
	switch {
	case len(stored) == 1+recoverySaltLen+sha256.Size && stored[0] == recoveryHashV2:
		salt := stored[1 : 1+recoverySaltLen]
		return hmac.Equal(stored[1+recoverySaltLen:], recoveryMAC(salt, normalizeRecoveryCode(presented)))
	case len(stored) == sha256.Size:
		digits := legacyRecoveryDigits(presented)
		if digits == "" {
			return false
		}
		return subtle.ConstantTimeCompare(stored, HashToken(digits)) == 1
	}
	return false
}

// normalizeRecoveryCode makes typing forgiving: case, separators and
// whitespace are ignored, and the look-alikes Crockford base32 leaves out
// (o, i, l) are read as the digits they resemble.
func normalizeRecoveryCode(code string) string {
	out := make([]byte, 0, len(code))
	for _, c := range strings.ToLower(code) {
		switch {
		case c == 'o':
			out = append(out, '0')
		case c == 'i' || c == 'l':
			out = append(out, '1')
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z':
			out = append(out, byte(c))
		}
	}
	return string(out)
}

// legacyRecoveryDigits is the normalization the 10-digit codes were hashed
// under: digits only.
func legacyRecoveryDigits(code string) string {
	out := make([]byte, 0, len(code))
	for _, c := range code {
		if c >= '0' && c <= '9' {
			out = append(out, byte(c))
		}
	}
	return string(out)
}
