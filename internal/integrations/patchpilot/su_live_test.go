package patchpilot

import (
	"context"
	"testing"
	"time"
)

// TestLiveSuRootBootstrap proves the su-root path: a non-sudoer login + root's
// password → PatchPilot becomes root via su and completes the full bootstrap.
// This is the path a host like the user's 'database' account takes.
func TestLiveSuRootBootstrap(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	mode, blocker := ResolveEscalation(ctx, conn, facts, "nosudo-pw", "pp-root-pw-2")
	if blocker != "" {
		t.Fatalf("blocker with root pw: %s", blocker)
	}
	if mode != EscSuRoot {
		t.Fatalf("expected su-root, got %s", mode)
	}
	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	res, berr := BootstrapStandard(ctx, conn, facts, kp.PublicKey, func(BootstrapStep) {})
	if berr != nil {
		t.Fatalf("su bootstrap: %v", berr)
	}
	time.Sleep(500 * time.Millisecond)
	keyConn, err := ConnectKey(ctx, "127.0.0.1", 2202, ManagedUser, kp.PrivatePEM, "")
	if err != nil {
		t.Fatalf("key reconnect: %v", err)
	}
	defer keyConn.Close()
	if verr := VerifyManagedAccess(ctx, keyConn, facts.PackageManager, res.Paths); verr != nil {
		t.Fatalf("verify: %v", verr)
	}
	t.Log("su-root bootstrap + verify OK")
}
