package api

import (
	"net"
	"net/http"
	"testing"
)

func TestClientIP_TrustedProxyGating(t *testing.T) {
	mk := func(remote, xff, xreal string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		if xff != "" {
			r.Header.Set("X-Forwarded-For", xff)
		}
		if xreal != "" {
			r.Header.Set("X-Real-Ip", xreal)
		}
		return r
	}
	t.Cleanup(func() { SetTrustedProxies(nil) })

	// Default: no trusted proxies -> forwarding headers are ignored, the client
	// IP is always the direct socket peer (a LAN caller can't forge it).
	SetTrustedProxies(nil)
	if got := ClientIP(mk("192.168.1.50:5000", "1.2.3.4", "5.6.7.8")); got != "192.168.1.50" {
		t.Fatalf("untrusted peer: got %q, want 192.168.1.50", got)
	}

	// Peer inside a trusted CIDR -> X-Forwarded-For wins over X-Real-Ip
	// (which many proxies pass through from the client untouched); X-Real-Ip
	// is only the fallback when there is no XFF.
	_, n, _ := net.ParseCIDR("192.168.1.0/24")
	SetTrustedProxies([]*net.IPNet{n})
	if got := ClientIP(mk("192.168.1.50:5000", "1.2.3.4", "5.6.7.8")); got != "1.2.3.4" {
		t.Fatalf("trusted peer XFF over X-Real-Ip: got %q, want 1.2.3.4", got)
	}
	if got := ClientIP(mk("192.168.1.50:5000", "", "5.6.7.8")); got != "5.6.7.8" {
		t.Fatalf("trusted peer X-Real-Ip fallback: got %q, want 5.6.7.8", got)
	}
	// Trusted peer, only X-Forwarded-For -> the rightmost untrusted hop.
	if got := ClientIP(mk("192.168.1.50:5000", "9.9.9.9, 10.0.0.1", "")); got != "10.0.0.1" {
		t.Fatalf("trusted peer XFF: got %q, want 10.0.0.1", got)
	}
	// A peer OUTSIDE the trusted set still can't forge, even with a trusted
	// network configured.
	if got := ClientIP(mk("203.0.113.7:5000", "1.2.3.4", "5.6.7.8")); got != "203.0.113.7" {
		t.Fatalf("non-trusted peer: got %q, want 203.0.113.7", got)
	}
}

// TestClientIP_XFFSpoofing pins the right-to-left walk: a client that sends
// its own X-Forwarded-For can only add entries on the LEFT of what our
// proxies append, so it must never be able to choose the resolved IP.
func TestClientIP_XFFSpoofing(t *testing.T) {
	t.Cleanup(func() { SetTrustedProxies(nil) })
	_, lan, _ := net.ParseCIDR("10.0.0.0/8")
	SetTrustedProxies([]*net.IPNet{lan})

	req := func(remote string, xff ...string) *http.Request {
		r := &http.Request{RemoteAddr: remote, Header: http.Header{}}
		for _, v := range xff {
			r.Header.Add("X-Forwarded-For", v)
		}
		return r
	}
	cases := []struct {
		name   string
		remote string
		xff    []string
		want   string
	}{
		// Attacker 203.0.113.9 sends "XFF: 1.1.1.1"; the proxy appends the
		// real peer. The forged left entry must lose.
		{"forged left entry", "10.0.0.2:1", []string{"1.1.1.1, 203.0.113.9"}, "203.0.113.9"},
		// Two proxy hops (10.0.0.3 -> 10.0.0.2): both are skipped.
		{"chained trusted proxies", "10.0.0.2:1", []string{"6.6.6.6, 198.51.100.4, 10.0.0.3"}, "198.51.100.4"},
		// Several XFF header lines are one list, in order.
		{"multiple header lines", "10.0.0.2:1", []string{"6.6.6.6", "198.51.100.4"}, "198.51.100.4"},
		// LAN client behind a LAN proxy: every hop is trusted, the leftmost
		// (the original client) is the answer.
		{"all hops trusted", "10.0.0.2:1", []string{"10.0.0.77"}, "10.0.0.77"},
		// Unparseable hop: stop at the last address we can vouch for.
		{"garbage hop", "10.0.0.2:1", []string{"1.1.1.1, not-an-ip"}, "10.0.0.2"},
		{"ipv6 client", "10.0.0.2:1", []string{"2001:db8::1"}, "2001:db8::1"},
		// Untrusted peer: headers ignored entirely.
		{"untrusted peer", "203.0.113.9:1", []string{"10.0.0.5"}, "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClientIP(req(tc.remote, tc.xff...)); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
