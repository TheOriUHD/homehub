package netscan

import (
	"context"
	"net"
	"sync"
	"testing"
)

func TestEnumerateCount(t *testing.T) {
	cases := []struct {
		start, end string
		want       int
	}{
		{"192.168.10.1", "192.168.10.254", 254},
		{"192.168.10.1", "192.168.10.1", 1},
		{"10.0.0.0", "10.0.0.255", 256},
		{"192.168.10.254", "192.168.10.1", 0},   // reversed
		{"10.0.0.0", "10.1.0.0", 0},             // 65537 > cap
		{"192.168.10.1", "192.168.10.100", 100}, // partial
	}
	for _, c := range cases {
		got := EnumerateCount(net.ParseIP(c.start), net.ParseIP(c.end))
		if got != c.want {
			t.Errorf("EnumerateCount(%s,%s)=%d want %d", c.start, c.end, got, c.want)
		}
	}
}

func TestEnumerateInclusive(t *testing.T) {
	ips := enumerate(net.ParseIP("192.168.10.10"), net.ParseIP("192.168.10.12"))
	if len(ips) != 3 || ips[0].String() != "192.168.10.10" || ips[2].String() != "192.168.10.12" {
		t.Fatalf("enumerate wrong: %v", ips)
	}
}

func TestVendorFor(t *testing.T) {
	// Full IEEE DB: MA-L /24 hit, and an unassigned prefix misses.
	if got := vendorFor("B8:27:EB:12:34:56"); got == "" {
		t.Error("OUI lookup failed for a known Raspberry Pi prefix")
	}
	if vendorFor("ff:ff:ff:00:00:00") != "" {
		t.Error("unknown OUI should be empty")
	}
	// MA-S /36 specificity: a 9-hex assignment resolves.
	if got := vendorFor("8C:1F:64:AF:A0:00"); got == "" {
		t.Error("MA-S /36 lookup failed")
	}
}

func TestScanConcurrencyNoRace(t *testing.T) {
	// Small loopback range with one likely-closed port — exercises the worker
	// pool, per-host port goroutines, and concurrent emit under -race. Results
	// don't matter here; we assert emit fires once per address without a race.
	var mu sync.Mutex
	seen := map[string]int{}
	Scan(context.Background(), Options{
		Start:       net.ParseIP("127.0.0.1"),
		End:         net.ParseIP("127.0.0.4"),
		Ports:       []int{1},
		Concurrency: 4,
	}, func(r HostResult) {
		mu.Lock()
		seen[r.IP]++
		mu.Unlock()
	})
	if len(seen) != 4 {
		t.Fatalf("expected 4 emits, got %d: %v", len(seen), seen)
	}
	for ip, n := range seen {
		if n != 1 {
			t.Fatalf("%s emitted %d times, want 1", ip, n)
		}
	}
}
