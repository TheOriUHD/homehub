package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. 64 MiB / 3 passes / 4 lanes sits comfortably above the
// OWASP minimum while keeping login latency well under 200ms on modest
// hardware.
const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 3
	argonThreads   = 4
	argonKeyLen    = 32
	argonSaltLen   = 16
)

// hashSlots bounds how many argon2id computations run at once. Each one
// allocates argonMemoryKiB (64 MiB), so a flood of concurrent login attempts
// — which the per-IP limiter only slows, and which can come from many IPs —
// would otherwise allocate 64 MiB per request in flight and could exhaust
// memory. Waiters hold no hash memory; they just queue.
var hashSlots = make(chan struct{}, hashConcurrency())

func hashConcurrency() int {
	n := runtime.NumCPU()
	// More parallel hashes than cores buys no throughput, only memory; keep a
	// floor of 2 so one slow hash never serializes every login.
	return min(max(n, 2), 4)
}

// argon2Key runs one argon2id derivation inside a hashSlots slot.
func argon2Key(pw, salt []byte, time, mem uint32, threads uint8, keyLen uint32) []byte {
	hashSlots <- struct{}{}
	defer func() { <-hashSlots }()
	return argon2.IDKey(pw, salt, time, mem, threads, keyLen)
}

// MinPasswordLength applies to every password the system accepts.
const MinPasswordLength = 12

var ErrPasswordTooWeak = fmt.Errorf("password must be at least %d characters", MinPasswordLength)

// ValidatePassword enforces the password policy. Length-first per NIST
// guidance; no composition rules that push users toward Password1!.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return ErrPasswordTooWeak
	}
	if len(pw) > 512 {
		return errors.New("password too long (max 512 characters)")
	}
	return nil
}

// HashPassword produces a PHC-format argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2Key([]byte(pw), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks a password against a PHC-format argon2id hash in
// constant time. The stored parameters are honored, so parameter upgrades
// remain compatible with existing hashes.
func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, err
	}
	var mem, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &time, &threads); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2Key([]byte(pw), salt, time, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
