package patchpilot

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveNonSudoerBlocked proves a user who has sudo INSTALLED but isn't a
// sudoer is caught at detection (blocker), not greenlit as sudo-password.
// Uses a throwaway user 'nosudo' on pp-target2. Gated behind PP_LIVE.
func TestLiveNonSudoerBlocked(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := ConnectPassword(ctx, "127.0.0.1", 2202, Credential{User: "nosudo", Password: "nosudo-pw"}, "")
	if err != nil {
		t.Skipf("nosudo user not set up: %v", err)
	}
	defer conn.Close()
	facts, err := Detect(ctx, conn)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	// No root password → must be blocked (can't verify elevation).
	mode, blocker := ResolveEscalation(ctx, conn, facts, "nosudo-pw", "")
	if blocker == "" {
		t.Fatalf("expected a blocker for a non-sudoer, got mode=%s", mode)
	}
	if !strings.Contains(blocker, "elevate") && !strings.Contains(blocker, "sudo") {
		t.Fatalf("blocker not descriptive: %q", blocker)
	}
	t.Logf("correctly blocked: %s", blocker)
}
