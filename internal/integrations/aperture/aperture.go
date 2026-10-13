// Package aperture integrates the Aperture NVR. Aperture has NO CORS, so every JSON read goes through this
// backend; only the live-video WebSocket runs browser→Aperture directly,
// authenticated by short-lived stream tokens this backend mints.
//
// Two rules shape everything here:
//
//  1. The rate budget is burst 40, refill 20/min PER TOKEN — so responses
//     are cached server-side (status ~10s, storage/bitrate ~60s, cameras
//     ~5min) and many HomeHub clients share one cached copy. Browsers are
//     never fanned out to Aperture.
//  2. GET /cameras leaks each camera's RTSP credentials in plaintext. The
//     wire structs below simply do not have those fields, so the secrets
//     are dropped at decode and can never reach a handler or the frontend.
package aperture

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

const TypeID = "aperture"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Aperture" }
func (Driver) Description() string {
	return "The Aperture NVR: every camera live in a grid with health pills, recording coverage, storage and retention, and throughput — video streams directly from Aperture, never proxied."
}
func (Driver) SecretNames() []string { return []string{"apiKey"} }

type client struct {
	base string // scheme://host, no trailing slash
	key  string
	http *http.Client
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	baseURL, _ := config["baseUrl"].(string)
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, errors.New("the Aperture address is required, e.g. https://aperture.example.com")
	}
	if !strings.Contains(base, "://") {
		base = "https://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not a valid address", baseURL)
	}
	if secrets["apiKey"] == "" {
		return nil, errors.New("an API token is required — create one under Settings → Integrations → API tokens in Aperture (scopes: media:live, system:read, storage:read, cameras:read)")
	}
	return &client{
		base: u.Scheme + "://" + u.Host,
		key:  secrets["apiKey"],
		http: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// wsBase converts the HTTP base to its WebSocket scheme — HTTPS must pair
// with WSS or the browser silently blocks the stream as mixed content.
func (c *client) wsBase() string {
	if strings.HasPrefix(c.base, "https://") {
		return "wss://" + strings.TrimPrefix(c.base, "https://")
	}
	return "ws://" + strings.TrimPrefix(c.base, "http://")
}

func (c *client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v1"+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	resp, err := c.http.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		return apiError(resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("%s: decode response: %w", path, err)
		}
	}
	return nil
}

// apiError translates Aperture's {"code","error"} envelope into a sentence
// that says what to fix.
func apiError(status int, raw []byte) error {
	var body struct {
		Code  string `json:"code"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &body)
	switch {
	case status == http.StatusUnauthorized:
		return errors.New("Aperture rejected the API token — check that it is valid and not revoked")
	case status == http.StatusForbidden && body.Code == "forbidden.scope":
		return errors.New("the API token is missing a scope — recreate it with media:live, system:read, storage:read, and cameras:read")
	case status == http.StatusForbidden && body.Code == "forbidden.camera":
		return errors.New("the API token is restricted to specific cameras — recreate it without camera restrictions")
	case status == http.StatusTooManyRequests:
		return errors.New("Aperture rate-limited the token — HomeHub will back off; avoid other heavy users of this token")
	}
	if body.Error != "" {
		return fmt.Errorf("Aperture: %s", body.Error)
	}
	return fmt.Errorf("Aperture answered %d: %s", status, strings.TrimSpace(string(bytes.TrimSpace(raw))))
}

func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused — check the address and that Aperture is running")
	case strings.Contains(msg, "no such host"):
		return errors.New("hostname not found — check the address")
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return errors.New("Aperture did not answer in time — check the address and any firewalls")
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return errors.New("TLS certificate not trusted: " + msg)
	}
	return err
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	// The health probe proves reachability; status proves the token+scopes.
	if err := c.get(ctx, "/system/health", nil); err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	var status systemStatus
	if err := c.get(ctx, "/system/status", &status); err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	ok := 0
	for _, cam := range status.Cameras {
		if cam.RecordingOk {
			ok++
		}
	}
	return integration.TestResult{
		OK:      true,
		Message: fmt.Sprintf("Connected — %d camera(s), %d recording", len(status.Cameras), ok),
		Detail:  map[string]any{"cameras": len(status.Cameras)},
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
	conn := newConn(inst, c, deps)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := c.get(pctx, "/system/health", nil); err != nil {
		return nil, err
	}
	return conn, nil
}
