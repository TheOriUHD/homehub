package netscan

import (
	"net"
	"testing"
)

func TestEnumerateCount(t *testing.T) {
	cases := []struct {
		start, end string
		want       int
	}{
		{"192.168.20.1", "192.168.20.254", 254},
		{"192.168.20.1", "192.168.20.1", 1},
		{"10.0.0.0", "10.0.0.255", 256},
		{"192.168.20.254", "192.168.20.1", 0},   // reversed
		{"10.0.0.0", "10.1.0.0", 0},             // 65537 > cap
		{"192.168.20.1", "192.168.20.100", 100}, // partial
	}
	for _, c := range cases {
		got := EnumerateCount(net.ParseIP(c.start), net.ParseIP(c.end))
		if got != c.want {
			t.Errorf("EnumerateCount(%s,%s)=%d want %d", c.start, c.end, got, c.want)
		}
	}
}

func TestEnumerateInclusive(t *testing.T) {
	ips := enumerate(net.ParseIP("192.168.20.10"), net.ParseIP("192.168.20.12"))
	if len(ips) != 3 || ips[0].String() != "192.168.20.10" || ips[2].String() != "192.168.20.12" {
		t.Fatalf("enumerate wrong: %v", ips)
	}
}

func TestVendorFor(t *testing.T) {
	if vendorFor("B8:27:EB:12:34:56") != "Raspberry Pi" {
		t.Error("OUI lookup failed for Raspberry Pi prefix")
	}
	if vendorFor("ff:ff:ff:00:00:00") != "" {
		t.Error("unknown OUI should be empty")
	}
}
