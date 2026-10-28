package sshpin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"golang.org/x/crypto/ssh"
)

type fakeBackend struct {
	pinned    map[string]string
	readErr   error
	committed []string // fingerprints Commit tried to pin
}

func (f *fakeBackend) GetSSHHostKey(_ context.Context, host string, _ int) (string, error) {
	if f.readErr != nil {
		return "", f.readErr
	}
	return f.pinned[host], nil
}

func (f *fakeBackend) PinSSHHostKey(_ context.Context, _ string, _ int, fp string) error {
	f.committed = append(f.committed, fp)
	return nil
}

func hostKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func withBackend(t *testing.T, b Backend) {
	t.Helper()
	prev := current()
	Configure(b)
	t.Cleanup(func() { Configure(prev) })
}

// A pin store that cannot be read must not look like "nothing pinned yet":
// that would accept whatever key answers and then pin it.
func TestUnreadablePinStoreRefusesTheConnection(t *testing.T) {
	b := &fakeBackend{readErr: errors.New("connection refused")}
	withBackend(t, b)
	ctx := context.Background()

	pin := For(ctx, "10.0.0.5", 22)
	if err := pin.Verify("", nil, hostKey(t)); err == nil {
		t.Fatal("accepted a host key without being able to check the pin")
	}
	pin.Commit(ctx)
	if len(b.committed) != 0 {
		t.Fatalf("pinned %v after a failed lookup", b.committed)
	}
}

func TestFirstContactPinsAndLaterMismatchIsRefused(t *testing.T) {
	b := &fakeBackend{pinned: map[string]string{}}
	withBackend(t, b)
	ctx := context.Background()
	key := hostKey(t)

	pin := For(ctx, "10.0.0.6", 22)
	if err := pin.Verify("", nil, key); err != nil {
		t.Fatalf("first contact refused: %v", err)
	}
	pin.Commit(ctx)
	if len(b.committed) != 1 || b.committed[0] != ssh.FingerprintSHA256(key) {
		t.Fatalf("first contact pinned %v", b.committed)
	}

	b.pinned["10.0.0.6"] = ssh.FingerprintSHA256(key)
	if err := For(ctx, "10.0.0.6", 22).Verify("", nil, key); err != nil {
		t.Fatalf("pinned key refused: %v", err)
	}
	if err := For(ctx, "10.0.0.6", 22).Verify("", nil, hostKey(t)); err == nil {
		t.Fatal("a different key was accepted for a pinned host")
	}
}
