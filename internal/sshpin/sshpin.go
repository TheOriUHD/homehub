// Package sshpin provides trust-on-first-use SSH host-key pinning for the
// interactive SSH bridges (Proxmox guest terminal, remote Docker daemon,
// Minecraft RCON-over-SSH). Those bridges are low-level helpers with no store
// handle of their own, so the persistence backend is registered once at
// startup via Configure and reached through a process-wide reference — the same
// way a logger or metrics sink is wired.
//
// PatchPilot does NOT use this package: it pins per managed-host row, tied to
// that host's onboarding lifecycle. sshpin covers the bridges that have no such
// row, keyed by (host, port).
package sshpin

import (
	"context"
	"fmt"
	"net"
	"sync"

	"golang.org/x/crypto/ssh"
)

// Backend persists pinned fingerprints. *store.Store satisfies it.
type Backend interface {
	GetSSHHostKey(ctx context.Context, host string, port int) (string, error)
	PinSSHHostKey(ctx context.Context, host string, port int, fingerprint string) error
}

var (
	mu      sync.RWMutex
	backend Backend
)

// Configure registers the persistence backend. Called once during server
// wiring; until then pinning degrades to capture-only (no worse than the old
// InsecureIgnoreHostKey, and production always configures it).
func Configure(b Backend) {
	mu.Lock()
	backend = b
	mu.Unlock()
}

func current() Backend {
	mu.RLock()
	defer mu.RUnlock()
	return backend
}

// Pin verifies one connection's host key against the stored fingerprint and
// captures it for pinning on first contact.
type Pin struct {
	host     string
	port     int
	expected string // pinned fingerprint, "" if none yet
	observed string // fingerprint seen during the handshake
}

// For loads any pinned fingerprint for host:port ahead of dialing. Use the
// returned Pin's Verify as the ssh.ClientConfig HostKeyCallback, then call
// Commit after a successful handshake to pin the key on first contact.
func For(ctx context.Context, host string, port int) *Pin {
	p := &Pin{host: host, port: port}
	if b := current(); b != nil {
		if fp, err := b.GetSSHHostKey(ctx, host, port); err == nil {
			p.expected = fp
		}
	}
	return p
}

// Verify is the HostKeyCallback: it records the presented key's fingerprint and
// rejects the connection when a pinned key no longer matches (host rebuilt or a
// LAN man-in-the-middle). On first contact (no pin yet) it accepts and records.
func (p *Pin) Verify(_ string, _ net.Addr, key ssh.PublicKey) error {
	p.observed = ssh.FingerprintSHA256(key)
	if p.expected != "" && p.observed != p.expected {
		return fmt.Errorf(
			"host key changed for %s — refusing to connect (pinned %s, now %s). "+
				"If this machine was rebuilt or the address now points at a different one, "+
				"accept the new key under SSH Hosts; nothing needs re-adding",
			net.JoinHostPort(p.host, fmt.Sprint(p.port)), p.expected, p.observed)
	}
	return nil
}

// Commit pins the observed fingerprint when nothing was pinned before. A no-op
// once a key is pinned (PinSSHHostKey won't overwrite). Safe to call after any
// successful dial.
func (p *Pin) Commit(ctx context.Context) {
	if p.expected != "" || p.observed == "" {
		return
	}
	if b := current(); b != nil {
		_ = b.PinSSHHostKey(ctx, p.host, p.port, p.observed)
	}
}
