package nettools

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// Every user-driven tool must refuse the HomeHub host's own loopback, even
// with a live service listening there — that is exactly what an attacker
// with a session would aim at.
func TestToolsRefuseLoopbackTargets(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for _, host := range []string{"127.0.0.1", "localhost", "0.0.0.0", "0.1.2.3"} {
		if r := CheckTCP(ctx, host, port); r.Open || !strings.Contains(r.Error, "blocked") {
			t.Errorf("TCP check of %s:%d = %+v, want blocked", host, port, r)
		}
		if _, err := InspectTLS(ctx, host, port, time.Now()); err == nil || !strings.Contains(err.Error(), "blocked") {
			t.Errorf("SSL check of %s: %v, want blocked", host, err)
		}
	}
	if r := Lookup(ctx, "example.com", "A", net.JoinHostPort("127.0.0.1", "53")); !strings.Contains(r.Error, "blocked") {
		t.Errorf("DNS against a loopback server: %+v, want blocked", r)
	}
	if r := Ping(ctx, "127.0.0.1"); r.Alive || !strings.Contains(r.Error, "blocked") {
		t.Errorf("ping loopback: %+v, want blocked", r)
	}
	if err := SendMagicPacket("00:11:22:33:44:55", "127.0.0.1", 9); err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Errorf("WoL to loopback: %v, want blocked", err)
	}
	if err := SendMagicPacket("00:11:22:33:44:55", "169.254.169.254", 9); err == nil {
		t.Error("WoL to the metadata address was allowed")
	}
}

// LAN targets stay reachable: the guard is about the host itself, not about
// the homelab around it.
func TestToolsStillReachLANAddresses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	// An RFC1918 address that nothing answers on: the result
	// must be an ordinary connection failure, never "blocked".
	r := CheckTCP(ctx, "10.255.255.1", 9)
	if strings.Contains(r.Error, "blocked") {
		t.Fatalf("RFC1918 target was blocked: %+v", r)
	}
}
