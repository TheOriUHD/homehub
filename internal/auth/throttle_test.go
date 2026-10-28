package auth

import (
	"fmt"
	"testing"
	"time"
)

func newTestThrottle() (*loginThrottle, *time.Time) {
	t := newLoginThrottle()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t.now = func() time.Time { return now }
	return t, &now
}

// The regression this guards: failing against the admin's username from one
// address must not lock the admin out when they sign in from another.
func TestLoginThrottle_AttackerLocksOnlyItself(t *testing.T) {
	th, _ := newTestThrottle()
	const attacker, owner = "203.0.113.9", "192.168.1.20"

	for i := range pairLockThreshold {
		until := th.fail("Admin", attacker)
		if i < pairLockThreshold-1 && !until.IsZero() {
			t.Fatalf("locked after %d failures, threshold is %d", i+1, pairLockThreshold)
		}
	}
	if until, _ := th.check("admin", attacker); until.IsZero() {
		t.Fatal("attacker IP is not locked after reaching the threshold")
	}
	if until, _ := th.check("admin", owner); !until.IsZero() {
		t.Fatal("the owner's IP got locked by someone else's failures")
	}
}

func TestLoginThrottle_PairLockBacksOffAndCaps(t *testing.T) {
	th, now := newTestThrottle()
	var prev time.Duration
	for i := range pairLockThreshold + 6 {
		until := th.fail("admin", "10.0.0.1")
		if i < pairLockThreshold-1 {
			continue
		}
		d := until.Sub(*now)
		if d < prev {
			t.Fatalf("lock shrank from %v to %v", prev, d)
		}
		if d > lockoutMax {
			t.Fatalf("lock %v exceeds the %v cap", d, lockoutMax)
		}
		prev = d
	}
	if prev != lockoutMax {
		t.Fatalf("lock never reached the cap: %v", prev)
	}
	// It expires on its own.
	*now = now.Add(lockoutMax + time.Second)
	if until, _ := th.check("admin", "10.0.0.1"); !until.IsZero() {
		t.Fatal("lock did not expire")
	}
}

// A distributed guesser (many IPs, each under the pair threshold) is slowed
// down for that username — for everyone, but only by a bounded delay, never
// a refusal.
func TestLoginThrottle_CrossIPFailuresDelayNotBlock(t *testing.T) {
	th, _ := newTestThrottle()
	for i := range userDelayThreshold - 1 {
		th.fail("admin", fmt.Sprintf("198.51.100.%d", i))
	}
	if _, d := th.check("admin", "192.168.1.20"); d != 0 {
		t.Fatalf("delay %v below the threshold", d)
	}
	for i := range 100 {
		th.fail("admin", fmt.Sprintf("203.0.113.%d", i))
	}
	until, d := th.check("admin", "192.168.1.20")
	if !until.IsZero() {
		t.Fatal("cross-IP failures locked out a fresh IP")
	}
	if d <= 0 || d > userDelayMax {
		t.Fatalf("delay %v, want (0, %v]", d, userDelayMax)
	}
	// Other usernames are unaffected.
	if _, d := th.check("bob", "192.168.1.20"); d != 0 {
		t.Fatalf("another account is slowed by %v", d)
	}
}

func TestLoginThrottle_SuccessClearsOnlyThatIP(t *testing.T) {
	th, _ := newTestThrottle()
	for range pairLockThreshold - 1 {
		th.fail("admin", "10.0.0.1")
		th.fail("admin", "10.0.0.2")
	}
	th.succeed("admin", "10.0.0.1")
	if until := th.fail("admin", "10.0.0.1"); !until.IsZero() {
		t.Fatal("success did not reset the pair")
	}
	if until := th.fail("admin", "10.0.0.2"); until.IsZero() {
		t.Fatal("success from one IP reset another IP's counter")
	}
}

func TestLoginThrottle_ForgetsStaleRecords(t *testing.T) {
	th, now := newTestThrottle()
	th.fail("admin", "10.0.0.1")
	*now = now.Add(failureWindow + 2*time.Minute)
	th.check("someone", "10.0.0.9") // triggers gc
	if len(th.pairs) != 0 || len(th.users) != 0 {
		t.Fatalf("stale records kept: %d pairs, %d users", len(th.pairs), len(th.users))
	}
}
