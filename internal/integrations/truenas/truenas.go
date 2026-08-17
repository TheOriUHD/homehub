// Package truenas implements the TrueNAS integration over the REST v2.0 API
// (the common surface across TrueNAS SCALE and CORE): pools and datasets,
// disks with SMART, shares, snapshots, replication, alerts, and reporting.
//
// No TrueNAS hardware is available in the build environment, so this driver
// is verified against an in-repo API simulator (internal/truenassim) and
// fixture tests. It must be validated against a real system before relying
// on it — the response shapes follow the documented v2.0 API.
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

	"homehub/internal/integration"
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

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	var info SystemInfo
	if err := c.call(ctx, "GET", "/system/info", nil, &info); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	var pools []Pool
	if err := c.call(ctx, "GET", "/pool", nil, &pools); err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	return integration.TestResult{
		OK: true,
		Message: fmt.Sprintf("Connected to %s — TrueNAS %s, %d pool(s)",
			info.Hostname, info.Version, len(pools)),
		Detail: map[string]any{"version": info.Version, "hostname": info.Hostname},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	key, err := deps.Secret(ctx, "apiKey")
	if err != nil {
		return nil, fmt.Errorf("reading the API key: %w", err)
	}
	c, err := newClient(inst.Config, map[string]string{"apiKey": key})
	if err != nil {
		return nil, err
	}
	conn := &Conn{inst: inst, client: c}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var info SystemInfo
	if err := c.call(pctx, "GET", "/system/info", nil, &info); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}
