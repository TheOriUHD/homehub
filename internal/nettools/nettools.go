// Package nettools implements the server-backed network utilities: SSL cert
// inspection, DNS lookup, host reachability (TCP + ICMP ping), Wake-on-LAN,
// and an HTTP request tester. Everything here makes OUTBOUND connections from
// the HomeHub host on behalf of an authenticated user — the handlers gate on
// the session; these functions add the bounds (timeouts, size caps, redirect
// caps) so a utility can't become an unbounded resource sink. On a single-
// admin homelab, reaching internal/RFC1918 hosts is the whole point, so that
// is deliberately allowed; the guards are about resource limits, not target
// filtering.
package nettools

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

// ---- SSL certificate checker ------------------------------------------------

type CertInfo struct {
	Subject   string   `json:"subject"`
	Issuer    string   `json:"issuer"`
	NotBefore string   `json:"notBefore"`
	NotAfter  string   `json:"notAfter"`
	DNSNames  []string `json:"dnsNames,omitempty"`
	Serial    string   `json:"serial"`
	SigAlgo   string   `json:"sigAlgo"`
	IsCA      bool     `json:"isCA"`
}

type SSLResult struct {
	Host          string     `json:"host"`
	Port          int        `json:"port"`
	Valid         bool       `json:"valid"`
	ValidationErr string     `json:"validationError,omitempty"`
	Expired       bool       `json:"expired"`
	DaysRemaining int        `json:"daysRemaining"`
	Leaf          CertInfo   `json:"leaf"`
	Chain         []CertInfo `json:"chain"`
	TLSVersion    string     `json:"tlsVersion"`
	Cipher        string     `json:"cipher"`
}

func certInfo(c *x509.Certificate) CertInfo {
	return CertInfo{
		Subject:   c.Subject.String(),
		Issuer:    c.Issuer.String(),
		NotBefore: c.NotBefore.UTC().Format(time.RFC3339),
		NotAfter:  c.NotAfter.UTC().Format(time.RFC3339),
		DNSNames:  c.DNSNames,
		Serial:    hex.EncodeToString(c.SerialNumber.Bytes()),
		SigAlgo:   c.SignatureAlgorithm.String(),
		IsCA:      c.IsCA,
	}
}

// InspectTLS dials host:port, reports the presented certificate chain and
// whether it validates against the host name. It connects with verification
// OFF so it can still report on self-signed / expired certs (the common
// reason someone reaches for this tool), then validates separately.
func InspectTLS(ctx context.Context, host string, port int, now time.Time) (*SSLResult, error) {
	if port == 0 {
		port = 443
	}
	d := &tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 8 * time.Second},
		Config:    &tls.Config{InsecureSkipVerify: true, ServerName: host}, //nolint:gosec // reporting tool; validation done explicitly below
	}
	dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil {
		return nil, fmt.Errorf("could not connect to %s:%d: %w", host, port, err)
	}
	defer conn.Close()
	tconn := conn.(*tls.Conn)
	state := tconn.ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return nil, fmt.Errorf("the server presented no certificate")
	}
	leaf := state.PeerCertificates[0]

	res := &SSLResult{
		Host: host, Port: port,
		Leaf:          certInfo(leaf),
		TLSVersion:    tlsVersionName(state.Version),
		Cipher:        tls.CipherSuiteName(state.CipherSuite),
		DaysRemaining: int(leaf.NotAfter.Sub(now).Hours() / 24),
		Expired:       now.After(leaf.NotAfter) || now.Before(leaf.NotBefore),
	}
	for _, c := range state.PeerCertificates {
		res.Chain = append(res.Chain, certInfo(c))
	}

	// Explicit validation against the host name using the presented chain.
	roots, _ := x509.SystemCertPool()
	inter := x509.NewCertPool()
	for _, c := range state.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, verr := leaf.Verify(x509.VerifyOptions{
		DNSName: host, Roots: roots, Intermediates: inter, CurrentTime: now,
	}); verr != nil {
		res.Valid = false
		res.ValidationErr = verr.Error()
	} else {
		res.Valid = true
	}
	return res, nil
}

func tlsVersionName(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS10:
		return "TLS 1.0"
	}
	return fmt.Sprintf("0x%04x", v)
}

// ---- TCP reachability -------------------------------------------------------

type TCPResult struct {
	Port      int     `json:"port"`
	Open      bool    `json:"open"`
	LatencyMs float64 `json:"latencyMs"`
	Error     string  `json:"error,omitempty"`
}

// CheckTCP attempts a TCP connection and times it.
func CheckTCP(ctx context.Context, host string, port int) TCPResult {
	start := time.Now()
	d := net.Dialer{Timeout: 5 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	lat := float64(time.Since(start).Microseconds()) / 1000.0
	if err != nil {
		msg := err.Error()
		// Trim the verbose "dial tcp a.b.c.d:port: " prefix.
		if i := strings.LastIndex(msg, ": "); i >= 0 {
			msg = msg[i+2:]
		}
		return TCPResult{Port: port, Open: false, LatencyMs: lat, Error: msg}
	}
	_ = conn.Close()
	return TCPResult{Port: port, Open: true, LatencyMs: lat}
}

// ResolveHost resolves a host name to its first IP (so results can show what
// was actually probed). Returns "" on failure.
func ResolveHost(ctx context.Context, host string) string {
	if net.ParseIP(host) != nil {
		return host
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	return ips[0].IP.String()
}
