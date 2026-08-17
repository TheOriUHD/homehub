// Package pve is a typed client for the Proxmox VE HTTP API.
//
// Authentication uses API tokens (Authorization: PVEAPIToken=<id>=<secret>).
// Self-signed clusters can pin the server certificate by SHA-256 fingerprint
// instead of disabling TLS verification entirely.
//
// Proxmox wraps every payload in {"data": ...} and is loose about scalar
// types (numbers as strings, booleans as 0/1); the Flex* types absorb that
// so callers see ordinary Go values.
package pve

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Guest kinds accepted by the per-guest methods.
const (
	KindQEMU = "qemu"
	KindLXC  = "lxc"
)

// Config describes how to reach and authenticate against a Proxmox cluster.
type Config struct {
	// BaseURL is the API root, e.g. "https://pve.example:8006".
	BaseURL string
	// TokenID is the API token id, "user@realm!tokenname".
	TokenID string
	// Secret is the API token secret (a UUID).
	Secret string
	// InsecureTLS disables certificate verification entirely.
	InsecureTLS bool
	// Fingerprint pins the server certificate by hex SHA-256 fingerprint
	// ("AA:BB:..."). Takes precedence over InsecureTLS.
	Fingerprint string
	// Timeout applies per HTTP request; zero means 30 seconds.
	Timeout time.Duration
}

// Client talks to a single Proxmox VE cluster endpoint. It is safe for
// concurrent use.
type Client struct {
	baseURL string // normalized, no trailing slash
	authz   string
	hc      *http.Client
	// wsHC has no Timeout: http.Client.Timeout would tear down long-lived
	// websocket connections mid-stream.
	wsHC *http.Client
}

// New validates cfg and returns a ready client. No network calls are made.
func New(cfg Config) (*Client, error) {
	if cfg.BaseURL == "" {
		return nil, errors.New("pve: BaseURL is required")
	}
	if cfg.TokenID == "" || cfg.Secret == "" {
		return nil, errors.New("pve: TokenID and Secret are required")
	}
	base := strings.TrimRight(cfg.BaseURL, "/")
	u, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("pve: invalid BaseURL %q: %w", cfg.BaseURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("pve: BaseURL must be an http(s) URL, got %q", cfg.BaseURL)
	}
	tlsCfg, err := buildTLSConfig(cfg)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsCfg
	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		baseURL: base,
		authz:   "PVEAPIToken=" + cfg.TokenID + "=" + cfg.Secret,
		hc:      &http.Client{Transport: transport, Timeout: timeout},
		wsHC:    &http.Client{Transport: transport},
	}, nil
}

func buildTLSConfig(cfg Config) (*tls.Config, error) {
	if cfg.Fingerprint != "" {
		want, err := parseFingerprint(cfg.Fingerprint)
		if err != nil {
			return nil, err
		}
		// PVE self-signed setups pin the leaf certificate: skip chain/CA
		// verification and compare the presented cert's SHA-256 instead.
		return &tls.Config{
			InsecureSkipVerify: true,
			VerifyPeerCertificate: func(rawCerts [][]byte, _ [][]*x509.Certificate) error {
				if len(rawCerts) == 0 {
					return errors.New("pve: server presented no certificate")
				}
				got := sha256.Sum256(rawCerts[0])
				if !bytes.Equal(got[:], want) {
					return fmt.Errorf("pve: server certificate fingerprint %s does not match pinned %s",
						formatFingerprint(got[:]), formatFingerprint(want))
				}
				return nil
			},
		}, nil
	}
	if cfg.InsecureTLS {
		return &tls.Config{InsecureSkipVerify: true}, nil
	}
	return nil, nil
}

func parseFingerprint(fp string) ([]byte, error) {
	clean := strings.Map(func(r rune) rune {
		if r == ':' || r == ' ' {
			return -1
		}
		return r
	}, fp)
	raw, err := hex.DecodeString(clean)
	if err != nil || len(raw) != sha256.Size {
		return nil, fmt.Errorf("pve: invalid certificate fingerprint %q (want hex SHA-256 like \"AA:BB:...\")", fp)
	}
	return raw, nil
}

func formatFingerprint(b []byte) string {
	parts := make([]string, len(b))
	for i, c := range b {
		parts[i] = fmt.Sprintf("%02X", c)
	}
	return strings.Join(parts, ":")
}

// joinPath builds "/seg/seg/..." with each segment path-escaped.
func joinPath(segs ...string) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}

func guestPath(node, kind string, vmid int, extra ...string) string {
	segs := append([]string{"nodes", node, kind, strconv.Itoa(vmid)}, extra...)
	return joinPath(segs...)
}

func checkKind(kind string) error {
	if kind != KindQEMU && kind != KindLXC {
		return fmt.Errorf("pve: invalid guest kind %q (want %q or %q)", kind, KindQEMU, KindLXC)
	}
	return nil
}

func formBool(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

// do issues one API request. Non-nil form is sent x-www-form-urlencoded;
// out (if non-nil) receives the unwrapped "data" field.
func (c *Client) do(ctx context.Context, method, path string, query, form url.Values, out any) error {
	rawURL := c.baseURL + "/api2/json" + path
	if len(query) > 0 {
		rawURL += "?" + query.Encode()
	}
	var body io.Reader
	if len(form) > 0 {
		body = strings.NewReader(form.Encode())
	}
	req, err := http.NewRequestWithContext(ctx, method, rawURL, body)
	if err != nil {
		return fmt.Errorf("pve: build request: %w", err)
	}
	req.Header.Set("Authorization", c.authz)
	if body != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("pve: %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("pve: %s %s: read response: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newError(resp.StatusCode, resp.Status, raw)
	}
	if out == nil {
		return nil
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return fmt.Errorf("pve: %s %s: decode envelope: %w", method, path, err)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("pve: %s %s: decode data: %w", method, path, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, nil, out)
}
