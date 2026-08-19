package patchpilot

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// These tests drive the real bootstrap protocol against disposable sshd
// containers stood up for verification (pp-target1 root@:2201,
// pp-target2 sudo-user phil@:2202). They run only when PP_LIVE=1.

func liveGate(t *testing.T) {
	if os.Getenv("PP_LIVE") != "1" {
		t.Skip("set PP_LIVE=1 with the sshd test containers running")
	}
}

func TestLiveBootstrapRootHost(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := ConnectPassword(ctx, "127.0.0.1", 2201, Credential{User: "root", Password: "pp-test-root-pw"}, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	facts, err := Detect(ctx, conn)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	if facts.PackageManager != MgrApt {
		t.Fatalf("expected apt, got %q", facts.PackageManager)
	}
	if facts.UID != 0 {
		t.Fatalf("expected root uid, got %d", facts.UID)
	}
	mode, blocker := ResolveEscalation(ctx, conn, facts, "pp-test-root-pw", "")
	if blocker != "" || mode != EscDirectRoot {
		t.Fatalf("escalation: mode=%s blocker=%s", mode, blocker)
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	var steps []string
	res, berr := BootstrapStandard(ctx, conn, facts, kp.PublicKey, func(s BootstrapStep) {
		steps = append(steps, s.Step+":"+s.Status)
	})
	if berr != nil {
		t.Fatalf("bootstrap: %v (steps: %v)", berr, steps)
	}
	if !strings.HasPrefix(res.SudoersEntry, "patchpilot ALL=(ALL) NOPASSWD:") {
		t.Fatalf("unexpected sudoers: %q", res.SudoersEntry)
	}

	// Reconnect as the managed account with the key and prove NOPASSWD sudo.
	time.Sleep(500 * time.Millisecond)
	keyConn, err := ConnectKey(ctx, "127.0.0.1", 2201, ManagedUser, kp.PrivatePEM, "")
	if err != nil {
		t.Fatalf("key reconnect: %v", err)
	}
	defer keyConn.Close()
	if err := VerifyManagedAccess(ctx, keyConn, MgrApt, res.Paths); err != nil {
		t.Fatalf("verify managed access: %v", err)
	}

	// A dry-run must work over the managed account.
	out, code, err := keyConn.Run(ctx, "sh -c "+shellSingleQuote(DryRunCommand(MgrApt, res.Paths, "sudo -n ")))
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	dr := ParseDryRun(MgrApt, out)
	t.Logf("dry-run exit=%d, %d upgrades, summary=%q", code, dr.PackagesUpgraded, dr.Summary)
}

func TestLiveBootstrapSudoUserHost(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	conn, err := ConnectPassword(ctx, "127.0.0.1", 2202, Credential{User: "phil", Password: "pp-test-user-pw"}, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	facts, err := Detect(ctx, conn)
	if err != nil {
		t.Fatalf("detect: %v", err)
	}
	mode, blocker := ResolveEscalation(ctx, conn, facts, "pp-test-user-pw", "")
	if blocker != "" {
		t.Fatalf("blocker: %s", blocker)
	}
	if mode != EscSudoPassword && mode != EscSudoNoPasswd {
		t.Fatalf("unexpected escalation %s", mode)
	}

	kp, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	res, berr := BootstrapStandard(ctx, conn, facts, kp.PublicKey, func(BootstrapStep) {})
	if berr != nil {
		t.Fatalf("bootstrap: %v", berr)
	}

	time.Sleep(500 * time.Millisecond)
	keyConn, err := ConnectKey(ctx, "127.0.0.1", 2202, ManagedUser, kp.PrivatePEM, "")
	if err != nil {
		t.Fatalf("key reconnect: %v", err)
	}
	defer keyConn.Close()
	if err := VerifyManagedAccess(ctx, keyConn, facts.PackageManager, res.Paths); err != nil {
		t.Fatalf("verify: %v", err)
	}
}
