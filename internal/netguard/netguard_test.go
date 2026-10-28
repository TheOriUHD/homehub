package netguard

import "testing"

func TestControl_BlocksInternalTargets(t *testing.T) {
	blocked := []string{
		"127.0.0.1:80",          // loopback admin services
		"[::1]:80",              // IPv6 loopback
		"169.254.169.254:80",    // cloud metadata (link-local)
		"169.254.10.20:443",     // link-local
		"[fe80::1]:80",          // IPv6 link-local
		"0.0.0.0:80",            // unspecified
		"[::]:80",               // IPv6 unspecified
		"224.0.0.1:80",          // multicast
		"0.1.2.3:5432",          // 0/8 is "this host" on Linux
		"[::ffff:127.0.0.1]:80", // IPv4-mapped loopback
		"not-an-ip:80",          // unresolved / malformed host
	}
	for _, addr := range blocked {
		if err := Control("tcp", addr, nil); err == nil {
			t.Errorf("expected %s to be blocked, but it was allowed", addr)
		}
	}
}

func TestControl_AllowsLANAndPublic(t *testing.T) {
	// RFC1918 LAN targets are intentionally reachable (homelab), as are public
	// addresses; only non-routable/internal ranges are blocked.
	allowed := []string{
		"192.168.1.10:80",
		"10.0.0.5:443",
		"172.16.5.5:8080",
		"8.8.8.8:53",       // public
		"[2606:4700::1]:443", // public IPv6
	}
	for _, addr := range allowed {
		if err := Control("tcp", addr, nil); err != nil {
			t.Errorf("expected %s to be allowed, got: %v", addr, err)
		}
	}
}
