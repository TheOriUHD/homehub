package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testVault(t *testing.T) *Vault {
	t.Helper()
	kek := make([]byte, keySize)
	if _, err := rand.Read(kek); err != nil {
		t.Fatal(err)
	}
	return &Vault{kek: kek}
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// A missing keyfile on a database that already holds sealed values is a lost
// key, not a first start: generating one would strand every stored secret.
func TestMissingKeyWithSealedDataRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	_, err := loadKEK(dir, "", quiet, func() (bool, error) { return true, nil })
	if err == nil || !strings.Contains(err.Error(), "master.key") {
		t.Fatalf("want a refusal naming master.key, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "master.key")); !os.IsNotExist(statErr) {
		t.Fatalf("a replacement keyfile was written: %v", statErr)
	}
}

func TestMissingKeyWhenSealedCheckFailsRefusesToStart(t *testing.T) {
	dir := t.TempDir()
	_, err := loadKEK(dir, "", quiet, func() (bool, error) { return false, errors.New("db down") })
	if err == nil {
		t.Fatal("generated a key without knowing whether secrets exist")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "master.key")); !os.IsNotExist(statErr) {
		t.Fatalf("a replacement keyfile was written: %v", statErr)
	}
}

func TestFreshInstallGeneratesAndReloadsKey(t *testing.T) {
	dir := t.TempDir()
	first, err := loadKEK(dir, "", quiet, func() (bool, error) { return false, nil })
	if err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	// Once the file exists the sealed-data check is irrelevant.
	second, err := loadKEK(dir, "", quiet, func() (bool, error) { return true, nil })
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("reload returned a different key (err %v)", err)
	}
}

func TestEnvKeyNeedsNoKeyfile(t *testing.T) {
	key := make([]byte, keySize)
	got, err := loadKEK(t.TempDir(), base64.StdEncoding.EncodeToString(key), quiet,
		func() (bool, error) { t.Fatal("sealed check consulted with an env key"); return false, nil })
	if err != nil || !bytes.Equal(got, key) {
		t.Fatalf("env key: %v", err)
	}
}

func TestBoundBlobOnlyOpensUnderItsBinding(t *testing.T) {
	v := testVault(t)
	blob, err := v.EncryptBound([]byte("seed"), "totp:alice")
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := v.DecryptBound(blob, "totp:alice"); err != nil || string(pt) != "seed" {
		t.Fatalf("own binding: %q %v", pt, err)
	}
	if _, err := v.DecryptBound(blob, "totp:bob"); err == nil {
		t.Fatal("bound blob opened under another record's binding")
	}
	if _, err := v.Decrypt(blob); err == nil {
		t.Fatal("bound blob opened without its binding")
	}
}

// Values sealed before binding existed must keep opening.
func TestLegacyBlobStillOpens(t *testing.T) {
	v := testVault(t)
	legacy, err := v.Encrypt([]byte("old"))
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := v.DecryptBound(legacy, "anything"); err != nil || string(pt) != "old" {
		t.Fatalf("legacy via DecryptBound: %q %v", pt, err)
	}
	if pt, err := v.Decrypt(legacy); err != nil || string(pt) != "old" {
		t.Fatalf("legacy via Decrypt: %q %v", pt, err)
	}
}

// legacySecretColumns is how Seal laid out a row before binding: the plain
// blob split straight into its columns.
func legacySecretColumns(t *testing.T, v *Vault, plaintext string) (wrapped, nonce, ct []byte) {
	t.Helper()
	blob, err := v.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatal(err)
	}
	n := wrappedDEKLen
	return blob[7 : 7+n], blob[7+n : 7+n+nonceSize], blob[7+n+nonceSize:]
}

func TestSecretRowsAreBoundToScopeAndName(t *testing.T) {
	v := testVault(t)
	w, n, c, err := v.sealSecret("integration:a", "password", []byte("hunter2"))
	if err != nil {
		t.Fatal(err)
	}
	if pt, err := v.openSecret("integration:a", "password", w, n, c); err != nil || string(pt) != "hunter2" {
		t.Fatalf("own row: %q %v", pt, err)
	}
	// The same columns pasted under another scope or name must not open.
	if _, err := v.openSecret("integration:b", "password", w, n, c); err == nil {
		t.Fatal("sealed row opened under another scope")
	}
	if _, err := v.openSecret("integration:a", "token", w, n, c); err == nil {
		t.Fatal("sealed row opened under another name")
	}
}

func TestLegacySecretRowsStillOpen(t *testing.T) {
	v := testVault(t)
	w, n, c := legacySecretColumns(t, v, "kept")
	blob, _ := v.Encrypt([]byte("x"))
	if got := int(blob[5])<<8 | int(blob[6]); got != wrappedDEKLen {
		t.Fatalf("legacy wrapped DEK is %d bytes, layout assumption broken", got)
	}
	if pt, err := v.openSecret("any", "row", w, n, c); err != nil || string(pt) != "kept" {
		t.Fatalf("legacy row: %q %v", pt, err)
	}
}
