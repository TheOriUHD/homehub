package backup

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"homehub/internal/vault"
)

type fakeSecrets struct {
	stored  map[string][]byte
	openErr error
	seals   int
}

func (f *fakeSecrets) Open(_ context.Context, scope, name string) ([]byte, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	raw, ok := f.stored[scope+"/"+name]
	if !ok {
		return nil, vault.ErrNotFound
	}
	return raw, nil
}

func (f *fakeSecrets) Seal(_ context.Context, scope, name string, plaintext []byte) error {
	f.seals++
	f.stored[scope+"/"+name] = plaintext
	return nil
}

func testService(secrets secretStore) *Service {
	return &Service{vault: secrets, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// The backup key is generated only when none is stored. A key that exists but
// cannot be read right now must never be replaced: every archive encrypted
// with it would become undecryptable.
func TestBackupKeyIsNeverReplacedOnReadFailure(t *testing.T) {
	ctx := context.Background()
	for _, readErr := range []error{
		errors.New("vault: decrypt failed: cipher: message authentication failed"),
		errors.New("conn closed"),
	} {
		secrets := &fakeSecrets{stored: map[string][]byte{}, openErr: readErr}
		if _, err := testService(secrets).encryptionIdentity(ctx); err == nil {
			t.Errorf("%v: got a key, want the error", readErr)
		}
		if secrets.seals != 0 {
			t.Errorf("%v: a replacement key was sealed", readErr)
		}
	}
}

func TestBackupKeyIsCreatedOnceAndReused(t *testing.T) {
	ctx := context.Background()
	secrets := &fakeSecrets{stored: map[string][]byte{}}
	svc := testService(secrets)

	// Reading an archive never mints a key: there is nothing it could open.
	if _, err := svc.loadIdentity(ctx, false); !errors.Is(err, errNoBackupKey) {
		t.Fatalf("read without a key: %v", err)
	}
	first, err := svc.encryptionIdentity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.encryptionIdentity(ctx)
	if err != nil || second.String() != first.String() || secrets.seals != 1 {
		t.Fatalf("key changed or resealed (seals=%d, err=%v)", secrets.seals, err)
	}
}

func TestDamagedBackupKeyIsReportedNotReplaced(t *testing.T) {
	secrets := &fakeSecrets{stored: map[string][]byte{"backup/age-identity": []byte("garbage")}}
	if _, err := testService(secrets).encryptionIdentity(context.Background()); err == nil {
		t.Fatal("damaged key accepted")
	}
	if secrets.seals != 0 {
		t.Fatal("damaged key was replaced")
	}
}
