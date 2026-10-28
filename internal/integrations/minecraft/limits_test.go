package minecraft

import (
	"fmt"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func TestPairLimiterBudgetsPerIPAndGlobally(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	l := newPairLimiter()
	l.now = clk.now

	for i := range pairFailuresPerIP {
		if !l.allow("10.0.0.1") {
			t.Fatalf("attempt %d refused before the per-IP budget was spent", i)
		}
		l.fail("10.0.0.1")
	}
	if l.allow("10.0.0.1") {
		t.Fatal("an IP past its budget may still guess")
	}
	if !l.allow("10.0.0.2") {
		t.Fatal("one IP's failures locked out another")
	}

	// Many addresses, a few guesses each: the global budget stops them.
	for i := 0; len(l.global) < pairFailuresGlobal; i++ {
		ip := fmt.Sprintf("192.0.2.%d", i)
		if l.allow(ip) {
			l.fail(ip)
		}
	}
	if l.allow("198.51.100.7") {
		t.Fatal("a fresh IP may guess after the global budget is spent")
	}
	// Nothing more is recorded while locked, so memory stays bounded.
	before := len(l.byIP)
	l.fail("203.0.113.9")
	if len(l.byIP) != before {
		t.Fatal("failures are still recorded while globally locked")
	}

	clk.t = clk.t.Add(pairWindow + time.Second)
	if !l.allow("10.0.0.1") || !l.allow("198.51.100.7") {
		t.Fatal("budgets did not refill after the window")
	}
	if len(l.byIP) != 0 || len(l.global) != 0 {
		t.Fatalf("expired failures kept: %d ips, %d global", len(l.byIP), len(l.global))
	}
}

func TestSkinCacheIsBoundedBySizeAndTTL(t *testing.T) {
	clk := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	c := NewSkinCache(3)
	c.now = clk.now

	for i := range 5 {
		c.Put(fmt.Sprint(i), []byte{byte(i)}, time.Hour)
	}
	if c.Len() != 3 {
		t.Fatalf("cache holds %d entries, max 3", c.Len())
	}
	if _, ok := c.Get("0"); ok {
		t.Fatal("least recently used entry was not evicted")
	}
	// Touching "2" makes "3" the oldest.
	if _, ok := c.Get("2"); !ok {
		t.Fatal("recent entry missing")
	}
	c.Put("5", nil, time.Hour)
	if _, ok := c.Get("3"); ok {
		t.Fatal("eviction ignored recency")
	}
	if _, ok := c.Get("2"); !ok {
		t.Fatal("recently used entry was evicted")
	}

	c.Put("short", []byte{1}, time.Minute)
	clk.t = clk.t.Add(2 * time.Minute)
	if _, ok := c.Get("short"); ok {
		t.Fatal("expired entry served")
	}
}

func TestNormalizePlayerUUID(t *testing.T) {
	for in, want := range map[string]string{
		"069A79F4-44E9-4726-A5BE-FCA90E38AAF5": "069a79f444e94726a5befca90e38aaf5",
		"069a79f444e94726a5befca90e38aaf5":     "069a79f444e94726a5befca90e38aaf5",
		"not-a-uuid":                           "",
		"zz9a79f444e94726a5befca90e38aaf5":     "",
		"":                                     "",
	} {
		if got := NormalizePlayerUUID(in); got != want {
			t.Errorf("NormalizePlayerUUID(%q) = %q, want %q", in, got, want)
		}
	}
}
