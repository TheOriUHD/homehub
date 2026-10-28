package nettools

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"

	"homehub/internal/netguard"
)

// PingResult is the outcome of an ICMP echo. Alive with an RTT, or a reason.
type PingResult struct {
	Alive     bool    `json:"alive"`
	RttMs     float64 `json:"rttMs"`
	IP        string  `json:"ip,omitempty"`
	Error     string  `json:"error,omitempty"`
	Available bool    `json:"available"` // false if ICMP couldn't be used at all
}

// Ping sends a single ICMP echo. HomeHub runs as root, so a raw ICMP socket
// is available; if it isn't (unprivileged dev), Available is false and the
// caller falls back to TCP timing.
func Ping(ctx context.Context, host string) PingResult {
	ipStr := ResolveHost(ctx, host)
	if ipStr == "" {
		return PingResult{Available: true, Error: "could not resolve host"}
	}
	dst := &net.IPAddr{IP: net.ParseIP(ipStr)}
	if err := netguard.CheckIP(dst.IP); err != nil {
		return PingResult{Available: true, IP: ipStr, Error: err.Error()}
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		// No raw-socket privilege — not an error to the user, just unavailable.
		return PingResult{Available: false, IP: ipStr}
	}
	defer conn.Close()

	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{ID: os.Getpid() & 0xffff, Seq: 1, Data: []byte("homehub-ping")},
	}
	wb, err := msg.Marshal(nil)
	if err != nil {
		return PingResult{Available: true, IP: ipStr, Error: err.Error()}
	}

	deadline := time.Now().Add(4 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	start := time.Now()
	if _, err := conn.WriteTo(wb, dst); err != nil {
		return PingResult{Available: true, IP: ipStr, Error: err.Error()}
	}

	rb := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(rb)
		if err != nil {
			return PingResult{Available: true, IP: ipStr, Alive: false, Error: "no reply (timeout)"}
		}
		rm, perr := icmp.ParseMessage(1, rb[:n]) // 1 = ICMPv4 protocol
		if perr != nil {
			continue
		}
		if rm.Type == ipv4.ICMPTypeEchoReply {
			// Accept the reply from our destination.
			if peer.String() == dst.IP.String() || peer.String() == ipStr {
				return PingResult{
					Available: true, Alive: true, IP: ipStr,
					RttMs: float64(time.Since(start).Microseconds()) / 1000.0,
				}
			}
		}
	}
}

// ---- Wake-on-LAN ------------------------------------------------------------

// SendMagicPacket sends a WoL magic packet (6×0xFF + 16×MAC) as a UDP
// broadcast to broadcast:port. mac may use ':' or '-' separators.
func SendMagicPacket(mac, broadcast string, port int) error {
	hw, err := net.ParseMAC(mac)
	if err != nil {
		return fmt.Errorf("invalid MAC address")
	}
	if len(hw) != 6 {
		return fmt.Errorf("MAC address must be 6 bytes (EUI-48)")
	}
	if broadcast == "" {
		broadcast = "255.255.255.255"
	}
	if port == 0 {
		port = 9
	}

	packet := make([]byte, 0, 102)
	for i := 0; i < 6; i++ {
		packet = append(packet, 0xFF)
	}
	for i := 0; i < 16; i++ {
		packet = append(packet, hw...)
	}

	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(broadcast, fmt.Sprint(port)))
	if err != nil {
		return fmt.Errorf("invalid broadcast address")
	}
	// Guarded like every other tool: the "broadcast" address is free text, and
	// aimed at 127.0.0.1 it would drop a datagram on any local UDP service.
	// Limited (255.255.255.255) and subnet broadcasts are not in a blocked
	// range, so real wake-ups are unaffected.
	conn, err := netguard.Dialer(5*time.Second).Dial("udp", addr.String())
	if err != nil {
		var blocked *netguard.ErrBlocked
		if errors.As(err, &blocked) {
			return blocked
		}
		return fmt.Errorf("could not open UDP socket: %w", err)
	}
	defer conn.Close()
	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("could not send packet: %w", err)
	}
	return nil
}
