package patchpilot

import (
	"context"
	"testing"
	"time"
)

// TestLiveProbeNonPosixShell proves detection survives a non-POSIX login
// shell (root's shell set to tcsh on pp-target1). Gated behind PP_LIVE.
func TestLiveProbeNonPosixShell(t *testing.T) {
	liveGate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := ConnectPassword(ctx, "127.0.0.1", 2201, Credential{User: "root", Password: "pp-test-root-pw"})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()

	// The OLD path (login shell = tcsh) would choke on sh syntax.
	rawLogin, _, _ := conn.Run(ctx, detectionProbe)
	factsLogin := ParseHostFacts(rawLogin)
	t.Logf("via login shell (tcsh): pkgmgr=%q os=%q", factsLogin.PackageManager, factsLogin.OSPretty)

	// The NEW path must detect apt regardless of login shell.
	_, facts, err := Probe(ctx, conn)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if facts.PackageManager != MgrApt {
		t.Fatalf("RunScript probe failed under tcsh: pkgmgr=%q os=%q", facts.PackageManager, facts.OSPretty)
	}
	t.Logf("via /bin/sh: pkgmgr=%q os=%q — FIX CONFIRMED", facts.PackageManager, facts.OSPretty)
}
