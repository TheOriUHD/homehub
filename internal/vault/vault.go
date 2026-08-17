// Package vault implements envelope encryption for everything HomeHub must
// store confidentially: integration credentials, TOTP secrets, API tokens.
//
// Model: a single 32-byte key-encryption key (KEK) lives outside the database
// (HOMEHUB_MASTER_KEY, or an auto-generated keyfile in the data dir). Every
// encrypted value gets its own random data-encryption key (DEK); the DEK
// encrypts the plaintext with AES-256-GCM and is itself wrapped by the KEK.
// The KEK is never written to the database and never included in app backups,
// so a database dump alone cannot reveal a single secret.
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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	keySize     = 32
	nonceSize   = 12
	blobVersion = 1
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
// and persisting a new key on first start.
func Open(pool *pgxpool.Pool, dataDir, envKey string, logger *slog.Logger) (*Vault, error) {
	var kek []byte
	switch {
	case envKey != "":
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(envKey))
		if err != nil || len(decoded) != keySize {
			return nil, fmt.Errorf("HOMEHUB_MASTER_KEY must be %d bytes of base64 (generate one with: openssl rand -base64 32)", keySize)
		}
		kek = decoded
	default:
		keyPath := filepath.Join(dataDir, "master.key")
		raw, err := os.ReadFile(keyPath)
		switch {
		case err == nil:
			kek, err = base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
			if err != nil || len(kek) != keySize {
				return nil, fmt.Errorf("keyfile %s is corrupt: expected %d bytes of base64", keyPath, keySize)
			}
		case os.IsNotExist(err):
			kek = make([]byte, keySize)
			if _, err := rand.Read(kek); err != nil {
				return nil, fmt.Errorf("generate master key: %w", err)
			}
			encoded := base64.StdEncoding.EncodeToString(kek) + "\n"
			if err := os.WriteFile(keyPath, []byte(encoded), 0o600); err != nil {
				return nil, fmt.Errorf("write keyfile: %w", err)
			}
			logger.Warn("generated new encryption master key — back this file up; without it stored credentials are unrecoverable",
				"path", keyPath)
		default:
			return nil, fmt.Errorf("read keyfile %s: %w", keyPath, err)
		}
	}
	return &Vault{pool: pool, kek: kek}, nil
}

// Encrypt seals plaintext into a self-contained blob:
//
//	[1B blob version][4B key version][2B wrapped-DEK length][wrapped DEK][12B nonce][ciphertext]
//
// The blob can be stored in any column and decrypted with Decrypt.
func (v *Vault) Encrypt(plaintext []byte) ([]byte, error) {
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
	ct := gcm.Seal(nil, nonce, plaintext, nil)

	blob := make([]byte, 0, 1+4+2+len(wrapped)+nonceSize+len(ct))
	blob = append(blob, blobVersion)
	blob = binary.BigEndian.AppendUint32(blob, currentKeyVersion)
	blob = binary.BigEndian.AppendUint16(blob, uint16(len(wrapped)))
	blob = append(blob, wrapped...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return blob, nil
}

// Decrypt opens a blob produced by Encrypt.
func (v *Vault) Decrypt(blob []byte) ([]byte, error) {
	if len(blob) < 1+4+2 || blob[0] != blobVersion {
		return nil, errors.New("vault: malformed blob")
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
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("vault: decrypt failed: %w", err)
	}
	return pt, nil
}

// Seal stores a named secret in the secrets table (upsert by scope+name).
func (v *Vault) Seal(ctx context.Context, scope, name string, plaintext []byte) error {
	blob, err := v.Encrypt(plaintext)
	if err != nil {
		return err
	}
	// The blob layout is decomposed into columns for observability; Decrypt
	// reassembles it. wrapped length is fixed per key version in practice.
	wrappedLen := int(binary.BigEndian.Uint16(blob[5:7]))
	wrapped := blob[7 : 7+wrappedLen]
	nonce := blob[7+wrappedLen : 7+wrappedLen+nonceSize]
	ct := blob[7+wrappedLen+nonceSize:]
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

// Open retrieves and decrypts a named secret.
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
	blob := make([]byte, 0, 1+4+2+len(wrapped)+len(nonce)+len(ct))
	blob = append(blob, blobVersion)
	blob = binary.BigEndian.AppendUint32(blob, currentKeyVersion)
	blob = binary.BigEndian.AppendUint16(blob, uint16(len(wrapped)))
	blob = append(blob, wrapped...)
	blob = append(blob, nonce...)
	blob = append(blob, ct...)
	return v.Decrypt(blob)
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
