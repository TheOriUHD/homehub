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

	// Peer inside a trusted CIDR -> X-Real-Ip is honored.
	_, n, _ := net.ParseCIDR("192.168.1.0/24")
	SetTrustedProxies([]*net.IPNet{n})
	if got := ClientIP(mk("192.168.1.50:5000", "1.2.3.4", "5.6.7.8")); got != "5.6.7.8" {
		t.Fatalf("trusted peer X-Real-Ip: got %q, want 5.6.7.8", got)
	}
	// Trusted peer, only X-Forwarded-For -> first token.
	if got := ClientIP(mk("192.168.1.50:5000", "9.9.9.9, 10.0.0.1", "")); got != "9.9.9.9" {
		t.Fatalf("trusted peer XFF: got %q, want 9.9.9.9", got)
	}
	// A peer OUTSIDE the trusted set still can't forge, even with a trusted
	// network configured.
	if got := ClientIP(mk("203.0.113.7:5000", "1.2.3.4", "5.6.7.8")); got != "203.0.113.7" {
		t.Fatalf("non-trusted peer: got %q, want 203.0.113.7", got)
	}
}
