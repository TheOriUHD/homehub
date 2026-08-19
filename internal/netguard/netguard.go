// Package netguard supplies a net.Dialer Control hook that blocks server-side
// outbound connections to loopback, link-local (incl. cloud metadata), and
// other non-routable targets, while still permitting normal LAN (RFC1918/ULA)
// and public destinations. It is the SSRF egress guard shared by the user-
// driven HTTP tester and the calendar feed/CalDAV fetcher.
//
// Because the hook inspects the RESOLVED ip:port right before connect — and
// runs again on every redirect dial — it also closes DNS-rebinding: a hostname
// that resolves (or re-resolves) to a blocked address is rejected at connect
// time, not merely at URL-parse time.
package netguard

import (
	"fmt"
	"net"
	"syscall"
	"time"
)

// ErrBlocked is returned (wrapped) when a dial targets a disallowed address.
type ErrBlocked struct{ Addr string }

func (e *ErrBlocked) Error() string {
	return fmt.Sprintf("blocked outbound address %s (loopback/link-local/metadata are not allowed)", e.Addr)
}

// blocked reports whether an IP must not be dialed by a user-driven fetch.
// RFC1918 private ranges and unique-local IPv6 are intentionally allowed —
// reaching LAN services is a legitimate homelab use — so only loopback,
// link-local (which includes 169.254.169.254 cloud metadata), multicast, and
// the unspecified address are rejected.
func blocked(ip net.IP) bool {
	return ip == nil ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified()
}

// Control is a net.Dialer.Control hook: it rejects the connection when the
// resolved address is in a blocked range. Wire it onto the dialer used by any
// http.Client that fetches user-supplied URLs.
func Control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("netguard: malformed dial address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if blocked(ip) {
		return &ErrBlocked{Addr: host}
	}
	return nil
}

// Dialer returns a net.Dialer with the Control guard installed and the given
// timeout, ready to hang off an http.Transport's DialContext.
func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: Control}
}
