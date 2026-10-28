// Package netguard supplies a net.Dialer Control hook that blocks server-side
// outbound connections to loopback, link-local (incl. cloud metadata), and
// other non-routable targets, while still permitting normal LAN (RFC1918/ULA)
// and public destinations. It is the SSRF egress guard shared by every
// user-driven network tool (HTTP tester, SSL check, TCP/port check, ping,
// DNS-against-a-server, Wake-on-LAN, the IP scanner) and the calendar
// feed/CalDAV fetcher. The point is that a signed-in user's "check this host"
// cannot be aimed at the services only the HomeHub machine itself can reach
// (its Postgres on 127.0.0.1, a Docker API on a loopback port, the cloud
// metadata endpoint).
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
	if ip4 := ip.To4(); ip4 != nil && ip4[0] == 0 {
		// 0.0.0.0/8 means "this host" to the Linux stack: 0.1.2.3:5432
		// reaches a loopback-bound Postgres just like 127.0.0.1 does.
		return true
	}
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

// Blocked reports whether ip is outside what user-driven tools may reach.
func Blocked(ip net.IP) bool { return blocked(ip) }

// CheckIP is Blocked as an error, for tools that do not dial through a
// net.Dialer (raw ICMP) and must refuse before sending anything.
func CheckIP(ip net.IP) error {
	if blocked(ip) {
		s := "<nil>"
		if ip != nil {
			s = ip.String()
		}
		return &ErrBlocked{Addr: s}
	}
	return nil
}

// Dialer returns a net.Dialer with the Control guard installed and the given
// timeout, ready to hang off an http.Transport's DialContext.
func Dialer(timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, Control: Control}
}
