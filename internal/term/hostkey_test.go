package term

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The probe has to work on a machine HomeHub cannot log into — that is the
// whole point, since it runs while connections are being refused. It stops at
// the key exchange, so it needs no credential and leaves no auth attempt.
func TestProbeReadsAHostKeyWithoutLoggingIn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	fp, err := ProbeHostKey(ctx, "127.0.0.1", 22)
	if err != nil {
		t.Skipf("no sshd on this machine to probe: %v", err)
	}
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("got %q, want an SHA256 fingerprint", fp)
	}
}

// An address that does not answer must report an error, never an empty
// fingerprint that a caller could mistake for a key.
func TestProbeReportsAnUnreachableAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	fp, err := ProbeHostKey(ctx, "127.0.0.1", 59999)
	if err == nil {
		t.Fatal("a closed port reported success")
	}
	if fp != "" {
		t.Fatalf("a failed probe returned a fingerprint: %q", fp)
	}
}
