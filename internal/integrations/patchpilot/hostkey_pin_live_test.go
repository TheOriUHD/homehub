package patchpilot

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Run: PP_LIVE=1 go test -run TestHostKeyPinning ./internal/integrations/patchpilot/

// TestHostKeyPinning_CaptureVerifyReject exercises the TOFU flow end-to-end
// against the live test-bench sshd on 127.0.0.1:2201:
//  1. first connect with no pin captures a SHA256 fingerprint,
//  2. reconnecting with that exact fingerprint succeeds,
//  3. reconnecting with a wrong fingerprint is refused (MITM/rebuild case).
func TestHostKeyPinning_CaptureVerifyReject(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cred := Credential{User: "root", Password: "pp-test-root-pw"}

	// 1) capture on first contact
	c1, err := ConnectPassword(ctx, "127.0.0.1", 2201, cred, "")
	if err != nil {
		t.Fatalf("first connect: %v", err)
	}
	fp := c1.HostKey()
	c1.Close()
	if !strings.HasPrefix(fp, "SHA256:") {
		t.Fatalf("expected SHA256 fingerprint, got %q", fp)
	}

	// 2) same key verifies
	c2, err := ConnectPassword(ctx, "127.0.0.1", 2201, cred, fp)
	if err != nil {
		t.Fatalf("connect with correct pin should succeed: %v", err)
	}
	c2.Close()

	// 3) wrong key is rejected
	_, err = ConnectPassword(ctx, "127.0.0.1", 2201, cred, "SHA256:AAAAwrongkeyAAAAwrongkeyAAAAwrongkeyAAAAwrong")
	if err == nil {
		t.Fatal("connect with mismatched pin must fail")
	}
	if !strings.Contains(err.Error(), "host key changed") {
		t.Fatalf("expected a host-key-changed error, got: %v", err)
	}
}
