package store_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// Recovering from a changed SSH host key. Database-backed: run with
// HOMEHUB_DATABASE_URL set. Test addresses are private and cleaned up.

// Pinning on first contact must never overwrite: a changed key is a decision
// for a person, and silently re-pinning it would make the whole check
// theatre. Trusting is the deliberate act that does overwrite.
func TestOnlyTrustingReplacesAPinnedHostKey(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	host := "test-probe-" + uuid.NewString()[:8]
	t.Cleanup(func() { _, _ = st.ForgetSSHHostKey(ctx, host, 22) })

	if err := st.PinSSHHostKey(ctx, host, 22, "SHA256:first"); err != nil {
		t.Fatalf("first pin: %v", err)
	}
	// A second sighting of a different key must not take effect.
	if err := st.PinSSHHostKey(ctx, host, 22, "SHA256:impostor"); err != nil {
		t.Fatalf("second pin: %v", err)
	}
	if got, _ := st.GetSSHHostKey(ctx, host, 22); got != "SHA256:first" {
		t.Fatalf("pin is now %q — first-contact pinning overwrote an existing key", got)
	}

	if err := st.TrustSSHHostKey(ctx, host, 22, "SHA256:rebuilt"); err != nil {
		t.Fatalf("trust: %v", err)
	}
	if got, _ := st.GetSSHHostKey(ctx, host, 22); got != "SHA256:rebuilt" {
		t.Fatalf("pin is %q — trusting must replace it", got)
	}

	removed, err := st.ForgetSSHHostKey(ctx, host, 22)
	if err != nil || !removed {
		t.Fatalf("forget: %v %v", removed, err)
	}
	if got, _ := st.GetSSHHostKey(ctx, host, 22); got != "" {
		t.Fatalf("pin survived being forgotten: %q", got)
	}
	// Forgetting nothing is not an error, but it reports that it found nothing.
	if removed, err := st.ForgetSSHHostKey(ctx, host, 22); err != nil || removed {
		t.Fatalf("forgetting an absent pin: removed=%v err=%v", removed, err)
	}
}

// The pin is keyed by address, which is what lets one decision restore the
// terminal, a Docker daemon and the agent at once. Ports must not collide.
func TestPinsAreKeyedByAddressAndPort(t *testing.T) {
	st := dbStore(t)
	ctx := context.Background()
	host := "test-probe-" + uuid.NewString()[:8]
	t.Cleanup(func() {
		_, _ = st.ForgetSSHHostKey(ctx, host, 22)
		_, _ = st.ForgetSSHHostKey(ctx, host, 2222)
	})

	if err := st.TrustSSHHostKey(ctx, host, 22, "SHA256:twentytwo"); err != nil {
		t.Fatal(err)
	}
	if err := st.TrustSSHHostKey(ctx, host, 2222, "SHA256:twentytwotwentytwo"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.GetSSHHostKey(ctx, host, 22); got != "SHA256:twentytwo" {
		t.Fatalf("port 22 pin was clobbered by the 2222 one: %q", got)
	}
}
