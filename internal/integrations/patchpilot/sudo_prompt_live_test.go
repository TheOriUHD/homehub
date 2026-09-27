package patchpilot

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// Live against the bench's pp-target2 (sudo user phil@:2202), PP_LIVE=1.

// The reported hang. A wrong sudo password made sudo prompt a second time,
// nothing answered it, and detection — riding a browser request with no
// deadline — waited for as long as the tab stayed open. The wizard's spinner
// never stopped. It must come back quickly, and say it was the password.
func TestLiveWrongSudoPasswordFailsFast(t *testing.T) {
	if os.Getenv("PP_LIVE") != "1" {
		t.Skip("set PP_LIVE=1 to run against the bench containers")
	}
	// No deadline, exactly like the request from the browser.
	ctx := context.Background()
	conn, err := ConnectPassword(ctx, "127.0.0.1", 2202, Credential{User: "phil", Password: "pp-test-user-pw"}, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	_, facts, err := Probe(ctx, conn)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}

	done := make(chan string, 1)
	start := time.Now()
	go func() {
		_, blocker := ResolveEscalation(ctx, conn, facts, "definitely-not-the-password", "")
		done <- blocker
	}()
	select {
	case blocker := <-done:
		if took := time.Since(start); took > 10*time.Second {
			t.Errorf("a wrong password took %s to report; it should be seconds", took)
		}
		if !strings.Contains(blocker, "did not accept that password") {
			t.Errorf("the reason should name the password, got %q", blocker)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("still waiting after 45s — a wrong sudo password hangs detection again")
	}
}

// Answering the prompt instead of writing blind must not cost the case that
// works: the right password still elevates, and a command's output comes
// back as its own, without the prompt text mixed into it.
func TestLiveRightSudoPasswordStillElevates(t *testing.T) {
	if os.Getenv("PP_LIVE") != "1" {
		t.Skip("set PP_LIVE=1 to run against the bench containers")
	}
	ctx := context.Background()
	conn, err := ConnectPassword(ctx, "127.0.0.1", 2202, Credential{User: "phil", Password: "pp-test-user-pw"}, "")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	_, facts, err := Probe(ctx, conn)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	mode, blocker := ResolveEscalation(ctx, conn, facts, "pp-test-user-pw", "")
	if mode != EscSudoPassword || blocker != "" {
		t.Fatalf("the right password no longer elevates: mode=%q blocker=%q", mode, blocker)
	}
	out, code, err := conn.RunEscalated(ctx, "id -u; echo after")
	if err != nil || code != 0 {
		t.Fatalf("escalated run failed: code=%d err=%v out=%q", code, err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "0") || !strings.Contains(out, "after") {
		t.Errorf("escalated output is not the command's: %q", out)
	}
	if strings.Contains(out, sudoPromptMarker) {
		t.Errorf("the prompt leaked into the output: %q", out)
	}
}
