package netscan

import (
	"encoding/binary"
	"net"
	"sort"
	"strings"
)

// LocalNetwork describes an interface's IPv4 network and a sensible default
// scan window (its /24, hosts .1–.254).
type LocalNetwork struct {
	Label    string `json:"label"`    // interface name
	CIDR     string `json:"cidr"`     // the interface's actual network, e.g. 192.168.10.42/24
	Start    string `json:"start"`    // suggested scan start
	End      string `json:"end"`      // suggested scan end
	Primary  bool   `json:"primary"`  // best guess for "your subnet"
}

func isPrivate(ip net.IP) bool {
	ip4 := ip.To4()
	if ip4 == nil {
		return false
	}
	return ip4[0] == 10 ||
		(ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31) ||
		(ip4[0] == 192 && ip4[1] == 168)
}

// LocalNetworks lists the server's up, non-loopback IPv4 networks. The most
// likely LAN (private, not the Docker default bridge) sorts first and is
// marked Primary — the UI defaults its range to that.
func LocalNetworks() []LocalNetwork {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []LocalNetwork
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := iface.Name
		// Skip virtual/container plumbing that isn't a real LAN.
		if strings.HasPrefix(name, "veth") || strings.HasPrefix(name, "br-") ||
			name == "docker0" || strings.HasPrefix(name, "tailscale") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLinkLocalUnicast() {
				continue
			}
			ones, _ := ipnet.Mask.Size()
			// Default scan window = the /24 the interface IP sits in, hosts
			// .1–.254 — matches "your subnet" without proposing a huge sweep
			// when the real mask is wider than /24.
			base := make(net.IP, 4)
			copy(base, ip4)
			start := make(net.IP, 4)
			copy(start, base)
			start[3] = 1
			end := make(net.IP, 4)
			copy(end, base)
			end[3] = 254
			cidr := &net.IPNet{IP: ip4.Mask(ipnet.Mask), Mask: ipnet.Mask}
			out = append(out, LocalNetwork{
				Label:   name,
				CIDR:    cidr.String(),
				Start:   start.String(),
				End:     end.String(),
				Primary: isPrivate(ip4) && ones >= 16,
			})
		}
	}
	// Primary (real private LAN) first, then by address for stability.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Primary != out[j].Primary {
			return out[i].Primary
		}
		return out[i].Start < out[j].Start
	})
	// Only the first primary stays flagged, so the UI has one clear default.
	seenPrimary := false
	for i := range out {
		if out[i].Primary {
			if seenPrimary {
				out[i].Primary = false
			}
			seenPrimary = true
		}
	}
	return out
}

// ParseIP4 parses a dotted-quad, returning nil for anything non-IPv4.
func ParseIP4(s string) net.IP {
	ip := net.ParseIP(strings.TrimSpace(s))
	if ip == nil {
		return nil
	}
	return ip.To4()
}

// ip4ToU32 / u32ToIP4 are exported-free helpers used by tests.
func ip4ToU32(ip net.IP) uint32 { return binary.BigEndian.Uint32(ip.To4()) }
