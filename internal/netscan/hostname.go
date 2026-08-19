package netscan

import (
	"context"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/miekg/dns"
)

// resolveHostname finds a friendly name for an IP the way a LAN scanner
// should: first a normal unicast reverse-DNS lookup (which catches names a
// local resolver like Pi-hole/AdGuard hands out), then a multicast-DNS
// (Bonjour/avahi) reverse query for the ".local" names most home devices
// advertise but that have no unicast PTR record. Returns "" if nothing
// answers within the deadline.
func resolveHostname(ctx context.Context, ip string) string {
	if h := unicastReverse(ctx, ip); h != "" {
		return h
	}
	return mdnsReverse(ctx, ip)
}

func unicastReverse(ctx context.Context, ip string) string {
	rctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	names, err := net.DefaultResolver.LookupAddr(rctx, ip)
	if err != nil || len(names) == 0 {
		return ""
	}
	return cleanHostname(names[0])
}

// mdnsReverse asks the link for the ".local" name of an IPv4 address via a
// one-shot multicast-DNS PTR query with the unicast-response bit set, so
// responders reply straight to our ephemeral socket (no need to bind 5353,
// which the host's own mDNS responder usually holds).
func mdnsReverse(ctx context.Context, ip string) string {
	v4 := net.ParseIP(ip).To4()
	if v4 == nil {
		return ""
	}
	qname := reverseArpa(v4)

	msg := new(dns.Msg)
	msg.Id = 0 // mDNS ignores the ID
	msg.RecursionDesired = false
	msg.Question = []dns.Question{{
		Name:   qname,
		Qtype:  dns.TypePTR,
		Qclass: dns.ClassINET | 0x8000, // QU: request a unicast reply
	}}
	packed, err := msg.Pack()
	if err != nil {
		return ""
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return ""
	}
	defer conn.Close()

	deadline := time.Now().Add(900 * time.Millisecond)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	dst := &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}
	if _, err := conn.WriteToUDP(packed, dst); err != nil {
		return ""
	}

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "" // deadline reached, no answer
		}
		var resp dns.Msg
		if resp.Unpack(buf[:n]) != nil {
			continue
		}
		for _, ans := range resp.Answer {
			if ptr, ok := ans.(*dns.PTR); ok && strings.EqualFold(ptr.Hdr.Name, qname) {
				if h := cleanHostname(ptr.Ptr); h != "" {
					return h
				}
			}
		}
	}
}

// reverseArpa builds "67.20.168.192.in-addr.arpa." for an IPv4 address.
func reverseArpa(v4 net.IP) string {
	return strconv.Itoa(int(v4[3])) + "." + strconv.Itoa(int(v4[2])) + "." +
		strconv.Itoa(int(v4[1])) + "." + strconv.Itoa(int(v4[0])) + ".in-addr.arpa."
}

// cleanHostname trims the trailing dot; the ".local" stays (it's the useful
// part of an mDNS name).
func cleanHostname(name string) string {
	name = strings.TrimSuffix(strings.TrimSpace(name), ".")
	if name == "" {
		return ""
	}
	return name
}
