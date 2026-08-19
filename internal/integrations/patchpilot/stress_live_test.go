package patchpilot

import (
	"context"
	"testing"
	"time"
)

// TestLiveProbeReliability runs the probe many times to surface any race /
// flakiness in RunScript (empty facts on some runs). Gated behind PP_LIVE.
func TestLiveProbeReliability(t *testing.T) {
	liveGate(t)
	empties, errs := 0, 0
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		conn, err := ConnectPassword(ctx, "127.0.0.1", 2201, Credential{User: "root", Password: "pp-test-root-pw"}, "")
		if err != nil {
			errs++
			cancel()
			continue
		}
		_, facts, perr := Probe(ctx, conn)
		if perr != nil {
			errs++
		} else if facts.PackageManager == "" {
			empties++
		}
		conn.Close()
		cancel()
	}
	t.Logf("40 probes: empties=%d errors=%d", empties, errs)
	if empties > 0 || errs > 0 {
		t.Fatalf("flaky: %d empty, %d errors out of 40", empties, errs)
	}
}
