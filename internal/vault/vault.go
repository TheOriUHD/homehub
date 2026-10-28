// Package vault implements envelope encryption for everything HomeHub must
// store confidentially: integration credentials, TOTP secrets, API tokens.
//
// Model: a single 32-byte key-encryption key (KEK) lives outside the database
// (HOMEHUB_MASTER_KEY, or an auto-generated keyfile in the data dir). Every
// encrypted value gets its own random data-encryption key (DEK); the DEK
// encrypts the plaintext with AES-256-GCM and is itself wrapped by the KEK.
// The KEK is never written to the database and never included in app backups,
// so a database dump alone cannot reveal a single secret.
//
// Named secrets are additionally bound to their (scope, name) as GCM
// associated data, so a sealed row copied over another one does not open.
// Rows sealed before binding existed still open and are upgraded on their
// next write.
package vault

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	keySize     = 32
	nonceSize   = 12
	blobVersion = 1
	// blobVersionBound marks a blob whose ciphertext carries associated data
	// naming the record it belongs to.
	blobVersionBound = 2
	// wrappedDEKLen is the size of a KEK-wrapped DEK: nonce + DEK + GCM tag.
	wrappedDEKLen = nonceSize + keySize + 16
	// currentKeyVersion identifies the active KEK. Rotation introduces a new
	// version and re-wraps stored DEKs; ciphertexts are untouched.
	currentKeyVersion = 1
)

var ErrNotFound = errors.New("vault: secret not found")

type Vault struct {
	pool *pgxpool.Pool
	kek  []byte
}

// Open loads the KEK from the environment or the data-dir keyfile, generating
// and persisting a new key on first start. A missing keyfile is only treated
// as a first start when the database holds nothing sealed: if secrets already
// exist they were wrapped by a key that has gone missing, and minting a fresh
// one would silently make every one of them unreadable (and get the new key
// backed up in place of the one that matters).
func Open(pool *pgxpool.Pool, dataDir, envKey string, logger *slog.Logger) (*Vault, error) {
	kek, err := loadKEK(dataDir, envKey, logger, func() (bool, error) { return hasSealedData(pool) })
	if err != nil {
		return nil, err
	}
	return &Vault{pool: pool, kek: kek}, nil
}

// hasSealedData reports whether any vault-encrypted value is stored: named
// secrets, and the TOTP seeds that are sealed in place on their own table.
func hasSealedData(pool *pgxpool.Pool) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var sealed bool
	err := pool.QueryRow(ctx, `
		select exists(select 1 from secrets) or exists(select 1 from totp_credentials)`).Scan(&sealed)
	return sealed, err
}

func loadKEK(dataDir, envKey string, logger *slog.Logger, hasSealed func() (bool, error)) ([]byte, error) {
	if envKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey))
		if err != nil || len(decoded) != keySize {
			return nil, fmt.Errorf("HOMEHUB_MASTER_KEY must be %d bytes of base64 (generate one with: openssl rand -base64 32)", keySize)
		}
		return decoded, nil
	}
	keyPath := filepath.Join(dataDir, "master.key")
	raw, err := os.ReadFile(keyPath)
	switch {
	case err == nil:
		kek, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
		if err != nil || len(kek) != keySize {
			return nil, fmt.Errorf("keyfile %s is corrupt: expected %d bytes of base64", keyPath, keySize)
		}
		return kek, nil
	case os.IsNotExist(err):
		sealed, serr := hasSealed()
		if serr != nil {
			return nil, fmt.Errorf("master key %s is missing and the database could not be checked for existing secrets — refusing to generate a new key: %w", keyPath, serr)
		}
		if sealed {
			return nil, fmt.Errorf("master key %s is missing, but the database already holds secrets sealed with it. "+
				"Put the original master.key back into %s (or set HOMEHUB_MASTER_KEY to its contents) and start again; "+
				"a new key could not open any of the stored credentials", keyPath, dataDir)
		}
		kek := make([]byte, keySize)
		if _, err := rand.Read(kek); err != nil {
			return nil, fmt.Errorf("generate master key: %w", err)
		}
		encoded := base64.StdEncoding.EncodeToString(kek) + "\n"
		if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
			return nil, fmt.Errorf("write keyfile: %w", err)
		}
		logger.Warn("generated new encryption master key — back this file up; without it stored credentials are unrecoverable",
			"path", keyPath)
		return kek, nil
	default:
		return nil, fmt.Errorf("read keyfile %s: %w", keyPath, err)
	}
}

// Encrypt seals plaintext into a self-contained blob:
//
//	[1B blob version][4B key version][2B wrapped-DEK length][wrapped DEK][12B nonce][ciphertext]
//
// The blob can be stored in any column and decrypted with Decrypt. It is not
// tied to where it is stored; EncryptBound is the variant that is.
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
	return v.encrypt(plaintext, blobVersion, nil)
}

// EncryptBound seals plaintext like Encrypt, but binds the ciphertext to
// binding (e.g. "totp:<user id>") as GCM associated data. A bound blob copied
// into another row fails to open there, so someone who can write the database
// but not read the key cannot swap one user's secret for another's.
func (v *Vault) EncryptBound(plaintext []byte, binding string) ([]byte, error) {
	return v.encrypt(plaintext, blobVersionBound, []byte(binding))
}

func (v *Vault) encrypt(plaintext []byte, version byte, aad []byte) ([]byte, error) {
	dek := make([]byte, keySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, err
	}
	wrapped, err := gcmSeal(v.kek, dek)
	if err != nil {
		return nil, fmt.Errorf("wrap dek: %w", err)
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, plaintext, aad)

	blob := make([]byte, 0, 1+4+2+len(wrapped)+nonceSize+len(ct))
	blob = append(blob, version)
	blob = binary.BigEndian.AppendUint32(blob, currentKeyVersion)
	blob = binary.BigEndian.AppendUint16(blob, uint16(len(wrapped)))
	blob = append(blob, wrapped...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return blob, nil
}

// Decrypt opens a blob produced by Encrypt. Bound blobs need DecryptBound.
func (v *Vault) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) > 0 && blob[0] == blobVersionBound {
		return nil, errors.New("vault: blob is bound to a record; open it with DecryptBound")
	}
	return v.decrypt(blob, nil)
}

// DecryptBound opens a blob produced by EncryptBound with the same binding.
// Blobs from before binding existed (plain Encrypt) still open, so callers can
// switch over without rewriting stored data; they become bound the next time
// the value is written.
func (v *Vault) DecryptBound(blob []byte, binding string) ([]byte, error) {
	return v.decrypt(blob, []byte(binding))
}

func (v *Vault) decrypt(blob []byte, aad []byte) ([]byte, error) {
	if len(blob) < 1+4+2 || (blob[0] != blobVersion && blob[0] != blobVersionBound) {
		return nil, errors.New("vault: malformed blob")
	}
	if blob[0] == blobVersion {
		// Legacy layout: sealed without associated data.
		aad = nil
	}
	wrappedLen := int(binary.BigEndian.Uint16(blob[5:7]))
	rest := blob[7:]
	if len(rest) < wrappedLen+nonceSize {
		return nil, errors.New("vault: truncated blob")
	}
	wrapped := rest[:wrappedLen]
	nonce := rest[wrappedLen : wrappedLen+nonceSize]
	ct := rest[wrappedLen+nonceSize:]

	dek, err := gcmOpen(v.kek, wrapped)
	if err != nil {
		return nil, fmt.Errorf("unwrap dek (wrong master key?): %w", err)
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt failed: %w", err)
	}
	return pt, nil
}

// secretBinding is the associated data a named secret is sealed under. A
// NUL cannot occur in a PostgreSQL text value, so scope and name cannot be
// shifted against each other to collide.
func secretBinding(scope, name string) string {
	return "secret\x00" + scope + "\x00" + name
}

// sealSecret produces the secrets-table columns for one value. The table has
// no format column, so a bound row marks itself with a leading version byte
// on dek_wrapped: legacy rows always hold exactly wrappedDEKLen bytes there
// (nonce + DEK + tag), which keeps the two unambiguous without a migration.
func (v *Vault) sealSecret(scope, name string, plaintext []byte) (wrapped, nonce, ct []byte, err error) {
	blob, err := v.EncryptBound(plaintext, secretBinding(scope, name))
	if err != nil {
		return nil, nil, nil, err
	}
	wrappedLen := int(binary.BigEndian.Uint16(blob[5:7]))
	wrapped = append([]byte{blobVersionBound}, blob[7:7+wrappedLen]...)
	nonce = blob[7+wrappedLen : 7+wrappedLen+nonceSize]
	ct = blob[7+wrappedLen+nonceSize:]
	return wrapped, nonce, ct, nil
}

// openSecret reassembles and decrypts stored secrets-table columns.
func (v *Vault) openSecret(scope, name string, wrapped, nonce, ct []byte) ([]byte, error) {
	version := byte(blobVersion)
	if len(wrapped) == wrappedDEKLen+1 && wrapped[0] == blobVersionBound {
		version = blobVersionBound
		wrapped = wrapped[1:]
	}
	blob := make([]byte, 0, 1+4+2+len(wrapped)+len(nonce)+len(ct))
	blob = append(blob, version)
	blob = binary.BigEndian.AppendUint32(blob, currentKeyVersion)
	blob = binary.BigEndian.AppendUint16(blob, uint16(len(wrapped)))
	blob = append(blob, wrapped...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return v.decrypt(blob, []byte(secretBinding(scope, name)))
}

// Seal stores a named secret in the secrets table (upsert by scope+name). The
// value is always written in the bound format, so rows sealed before binding
// existed are upgraded the next time they are saved.
func (v *Vault) Seal(ctx context.Context, scope, name string, plaintext []byte) error {
	wrapped, nonce, ct, err := v.sealSecret(scope, name, plaintext)
	if err != nil {
		return err
	}
	_, err = v.pool.Exec(ctx, `
		insert into secrets (scope, name, key_version, dek_wrapped, nonce, ciphertext)
		values ($1, $2, $3, $4, $5, $6)
		on conflict (scope, name) do update
		set key_version = excluded.key_version,
		    dek_wrapped = excluded.dek_wrapped,
		    nonce       = excluded.nonce,
		    ciphertext  = excluded.ciphertext,
		    updated_at  = now()`,
		scope, name, currentKeyVersion, wrapped, nonce, ct)
	return err
}

// Open retrieves and decrypts a named secret. ErrNotFound means no such
// secret exists; any other error (database, wrong key, tampered row) means it
// may exist but could not be read, and callers must not treat it as absent.
func (v *Vault) Open(ctx context.Context, scope, name string) ([]byte, error) {
	var wrapped, nonce, ct []byte
	err := v.pool.QueryRow(ctx,
		`select dek_wrapped, nonce, ciphertext from secrets where scope = $1 and name = $2`,
		scope, name).Scan(&wrapped, &nonce, &ct)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return v.openSecret(scope, name, wrapped, nonce, ct)
}

// Delete removes a named secret. Missing secrets are not an error.
func (v *Vault) Delete(ctx context.Context, scope, name string) error {
	_, err := v.pool.Exec(ctx, `delete from secrets where scope = $1 and name = $2`, scope, name)
	return err
}

// DeleteScope removes every secret under a scope (e.g. when an integration
// instance is deleted).
func (v *Vault) DeleteScope(ctx context.Context, scope string) error {
	_, err := v.pool.Exec(ctx, `delete from secrets where scope = $1`, scope)
	return err
}

func gcmSeal(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return append(nonce, gcm.Seal(nil, nonce, plaintext, nil)...), nil
}

func gcmOpen(key, sealed []byte) ([]byte, error) {
	if len(sealed) < nonceSize {
		return nil, errors.New("vault: sealed value too short")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, sealed[:nonceSize], sealed[nonceSize:], nil)
}
