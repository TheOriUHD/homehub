// Package tailscale implements the Tailscale integration against the
// official cloud API (api.tailscale.com, v2): the device inventory with
// connectivity and subnet routes, device administration (authorize, routes,
// key expiry, expire, remove), auth keys, and tailnet DNS settings.
//
// No real tailnet is available in the build environment, so this driver is
// verified against an in-repo simulator (internal/tailscalesim) that mirrors
// the documented routing and envelopes. It must be validated against a real
// tailnet (an API access token from the admin console) before relying on it.
package tailscale

import (
	"bytes"
	"context"
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

const TypeID = "tailscale"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Tailscale" }
func (Driver) Description() string {
	return "Your tailnet: every device with connectivity, versions, and subnet routes, device authorization and key management, auth keys, and tailnet DNS settings."
}
func (Driver) SecretNames() []string { return []string{"apiKey"} }

// client is a thin API-v2 client. The API key rides Basic auth (key as
// username, empty password); the tailnet name "-" means the key's default.
type client struct {
	base    string
	tailnet string
	key     string
	http    *http.Client
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	if secrets["apiKey"] == "" {
		return nil, errors.New("an API access token is required — create one under Settings → Keys in the Tailscale admin console")
	}
	tailnet, _ := config["tailnet"].(string)
	tailnet = strings.TrimSpace(tailnet)
	if tailnet == "" {
		tailnet = "-"
	}
	// baseUrl is a hidden config for pointing at the simulator in tests and
	// self-hosted Headscale installs; production defaults to the cloud API.
	base, _ := config["baseUrl"].(string)
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://api.tailscale.com"
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("base URL %q is not a valid address", base)
	}
	return &client{
		base:    u.Scheme + "://" + u.Host,
		tailnet: tailnet,
		key:     secrets["apiKey"],
		http:    &http.Client{Timeout: 20 * time.Second},
	}, nil
}

// tailnetPath builds /api/v2/tailnet/{tailnet}/<suffix>.
func (c *client) tailnetPath(suffix string) string {
	return "/api/v2/tailnet/" + url.PathEscape(c.tailnet) + "/" + suffix
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
	req.SetBasicAuth(c.key, "")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return fmt.Errorf("Tailscale rejected the API token (%d) — check that it is valid, not expired, and scoped to this tailnet", resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		// The API wraps errors as {"message": "..."}.
		var apiErr struct {
			Message string `json:"message"`
		}
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		if json.Unmarshal(raw, &apiErr) == nil && apiErr.Message != "" {
			return fmt.Errorf("Tailscale: %s", apiErr.Message)
		}
		return fmt.Errorf("Tailscale request failed (%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
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
		return "connection refused — check the address and your network"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return "connection timed out — check your network and any firewalls"
	}
	return msg
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	devices, err := fetchDevices(ctx, c)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	online := 0
	for _, d := range devices {
		if d.Online() {
			online++
		}
	}
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected — %d device(s), %d online", len(devices), online),
		Detail:  map[string]any{"devices": len(devices), "online": online},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	key, err := deps.Secret(ctx, "apiKey")
	if err != nil {
		return nil, fmt.Errorf("reading the API token: %w", err)
	}
	c, err := newClient(inst.Config, map[string]string{"apiKey": key})
	if err != nil {
		return nil, err
	}
	conn := &Conn{inst: inst, client: c, deps: deps}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := conn.Devices(pctx); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return conn, nil
}

// fetchDevices pulls the full-field device list.
func fetchDevices(ctx context.Context, c *client) ([]Device, error) {
	var resp struct {
		Devices []Device `json:"devices"`
	}
	if err := c.call(ctx, http.MethodGet, c.tailnetPath("devices")+"?fields=all", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Devices, nil
}
