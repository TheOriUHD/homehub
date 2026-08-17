package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"math/big"
)

// NewToken returns a URL-safe opaque token with 256 bits of entropy, and the
// SHA-256 digest under which it is stored. Only the digest ever touches the
// database, so a database leak cannot be replayed as a live credential.
func NewToken() (token string, hash []byte, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(raw)
	return token, HashToken(token), nil
}

// HashToken digests a presented token for lookup.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// NewTempPassword generates a readable one-time password for admin-created
// accounts: grouped base32-like alphabet without ambiguous characters,
// ~62 bits of entropy. The receiving user is forced to change it on first
// login.
func NewTempPassword() (string, error) {
	const alphabet = "abcdefghjkmnpqrstuvwxyz23456789"
	const groups, groupLen = 4, 4
	out := make([]byte, 0, groups*groupLen+groups-1)
	for g := range groups {
		if g > 0 {
			out = append(out, '-')
		}
		for range groupLen {
			n, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
			if err != nil {
				return "", err
			}
			out = append(out, alphabet[n.Int64()])
		}
	}
	return string(out), nil
}

// NewRecoveryCode generates one MFA recovery code (10 digits, grouped).
func NewRecoveryCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_00000_00000))
	if err != nil {
		return "", err
	}
	s := fmt.Sprintf("%010d", n.Int64())
	return s[:5] + "-" + s[5:], nil
}
