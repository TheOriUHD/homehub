// Package truenas implements the TrueNAS integration: pools and datasets,
// disks with SMART, shares, snapshots, replication, alerts, and reporting.
//
// The primary transport is the JSON-RPC 2.0 WebSocket API at /api/current —
// the only API modern TrueNAS ships (REST v2.0 was deprecated in 25.04 and
// removed afterwards; on 26.x it 404s). The REST client survives solely as
// a fallback for systems that predate /api/current (CORE, SCALE ≤ 24.x),
// used only when the WebSocket dial itself fails.
//
// Verified against the in-repo simulator (internal/truenassim) and a real
// TrueNAS 26.0 system. Method names and response shapes were checked against
// the live /api/docs/current method list.
package truenas

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/TheOriUHD/homehub/internal/integration"
)

const TypeID = "truenas"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "TrueNAS" }
func (Driver) Description() string {
	return "TrueNAS storage: pools and datasets with capacity and health, disks with SMART and temperature, SMB/NFS/iSCSI shares, snapshots, replication, and alerts."
}
func (Driver) SecretNames() []string { return []string{"apiKey"} }

// client is a thin REST v2.0 client with Bearer API-key auth.
type client struct {
	base string
	key  string
	http *http.Client
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	host, _ := config["host"].(string)
	host = strings.TrimRight(strings.TrimSpace(host), "/")
	if host == "" {
		return nil, errors.New("host is required, e.g. truenas.lan or https://192.168.1.8")
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("host %q is not a valid address", host)
	}
	if secrets["apiKey"] == "" {
		return nil, errors.New("an API key is required — create one under Credentials → API Keys in TrueNAS")
	}
	insecure, _ := config["insecureTls"].(bool)
	return &client{
		base: u.Scheme + "://" + u.Host + "/api/v2.0",
		key:  secrets["apiKey"],
		http: &http.Client{
			Timeout: 20 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
			},
		},
	}, nil
}

func (c *client) call(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("TrueNAS rejected the API key (%d) — check that it is valid and not revoked", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("TrueNAS request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

func friendlyError(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return "connection refused — check the address and that the TrueNAS web UI is reachable"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — enable \"skip TLS verification\" for the self-signed default (" + msg + ")"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "connection timed out — check the address and any firewalls"
	}
	return msg
}

// newConn builds a connection with the WebSocket transport always armed —
// it is the only API modern TrueNAS has. fetch() falls back to REST when
// the socket can't be dialed (older systems without /api/current).
func newConn(config map[string]any, key string) (*Conn, error) {
	c, err := newClient(config, map[string]string{"apiKey": key})
	if err != nil {
		return nil, err
	}
	username, _ := config["username"].(string)
	insecure, _ := config["insecureTls"].(bool)
	return &Conn{
		client: c,
		ws:     newWSClient(c.base, strings.TrimSpace(username), key, insecure),
	}, nil
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	conn, err := newConn(config, secrets["apiKey"])
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	info, err := conn.Info(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	pools, err := conn.Pools(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	transport := "the REST API (legacy — pre-25.04 systems only)"
	if conn.ws.isAuthed() {
		transport = "the WebSocket API"
	}
	return integration.TestResult{
		OK: true,
		Message: fmt.Sprintf("Connected to %s via %s — TrueNAS %s, %d pool(s)",
			info.Hostname, transport, info.Version, len(pools)),
		Detail: map[string]any{"version": info.Version, "hostname": info.Hostname},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	key, err := deps.Secret(ctx, "apiKey")
	if err != nil {
		return nil, fmt.Errorf("reading the API key: %w", err)
	}
	conn, err := newConn(inst.Config, key)
	if err != nil {
		return nil, err
	}
	conn.inst = inst
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if _, err := conn.Info(pctx); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}
