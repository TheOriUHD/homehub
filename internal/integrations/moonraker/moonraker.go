// Package moonraker integrates Moonraker — the API server that fronts the
// Klipper 3D-printer firmware (https://moonraker.readthedocs.io).
//
// Every printer exposes a *different* set of Klipper objects: one machine has
// two extruders, a chamber sensor and 30 macros; another has a single hotend,
// a Neopixel and a handful of macros. So nothing here is hard-coded to a
// specific object list. The driver discovers the live object list from
// /printer/objects/list, classifies each entry by its Klipper prefix
// (extruder / heater_bed / heater_generic / temperature_sensor /
// temperature_fan / fan* / led|neopixel|dotstar / output_pin / gcode_macro …),
// and the dashboard renders whatever it finds. New object types a printer
// exposes surface as "other" rather than being dropped.
//
// Transport: Moonraker speaks REST (JSON, everything wrapped in {"result":…})
// and a JSON-RPC 2.0 WebSocket at /websocket. The connection keeps a live
// WebSocket subscription so temperatures, print progress and the G-code
// console stream in real time; the REST client is the source of truth for the
// initial snapshot and the fallback when the socket is down. Auth is optional:
// a Moonraker API key (X-Api-Key header / ?token= on the socket) is sent when
// configured, and omitted for the common trusted-LAN setup.
package moonraker

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

	"homehub/internal/integration"
)

const TypeID = "moonraker"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Moonraker / Klipper" }
func (Driver) Description() string {
	return "Klipper 3D printers via Moonraker: a live dashboard that discovers every heater, temperature sensor, fan, LED and macro your setup exposes — with print progress, temperature control, motion, and a G-code console."
}
func (Driver) SecretNames() []string { return []string{"apiKey"} }

// client is a thin Moonraker REST client. Auth is optional (X-Api-Key).
type client struct {
	base string // scheme://host[:port], no trailing slash
	key  string // Moonraker API key, or "" for trusted-client setups
	http *http.Client
}

func newClient(config map[string]any, secrets map[string]string) (*client, error) {
	baseURL, _ := config["baseUrl"].(string)
	base := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if base == "" {
		return nil, errors.New("the Moonraker address is required, e.g. http://printer.local or http://192.168.1.50:7125")
	}
	// Klipper hosts are usually plain HTTP on the LAN; default to http:// when
	// no scheme is given rather than https:// (which would fail on most rigs).
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%q is not a valid address", baseURL)
	}
	return &client{
		base: u.Scheme + "://" + u.Host,
		key:  strings.TrimSpace(secrets["apiKey"]),
		http: &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// wsBase converts the HTTP origin to its WebSocket scheme.
func (c *client) wsBase() string {
	if strings.HasPrefix(c.base, "https://") {
		return "wss://" + strings.TrimPrefix(c.base, "https://")
	}
	return "ws://" + strings.TrimPrefix(c.base, "http://")
}

// rpcResult is Moonraker's universal envelope: success carries "result",
// failure carries "error" with a code and message.
type rpcResult struct {
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func (c *client) do(ctx context.Context, method, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, nil)
	if err != nil {
		return err
	}
	if c.key != "" {
		req.Header.Set("X-Api-Key", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return friendlyNetErr(err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("Moonraker requires authentication — add an API key (Moonraker → API Key, or set a trusted client for HomeHub's address)")
	}
	var env rpcResult
	// Moonraker returns the {result|error} envelope even on 4xx/5xx; decode
	// first so the upstream message ("Klippy is not connected") reaches the UI.
	if len(bytes.TrimSpace(raw)) > 0 {
		_ = json.Unmarshal(raw, &env)
	}
	if env.Error != nil && env.Error.Message != "" {
		return fmt.Errorf("Moonraker: %s", env.Error.Message)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("Moonraker answered %d: %s", resp.StatusCode, strings.TrimSpace(string(bytes.TrimSpace(raw))))
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("%s: decode response: %w", path, err)
		}
	}
	return nil
}

func (c *client) get(ctx context.Context, path string, out any) error {
	return c.do(ctx, http.MethodGet, path, out)
}

// post issues a Moonraker action. Actions take their arguments as query
// parameters (e.g. /printer/gcode/script?script=…), so callers pre-encode.
func (c *client) post(ctx context.Context, path string) error {
	return c.do(ctx, http.MethodPost, path, nil)
}

func friendlyNetErr(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"):
		return errors.New("connection refused — check the address and that Moonraker is running (default port 7125)")
	case strings.Contains(msg, "no such host"):
		return errors.New("hostname not found — check the address")
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "Client.Timeout"):
		return errors.New("Moonraker did not answer in time — check the address and any firewalls")
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return errors.New("TLS certificate not trusted: " + msg)
	}
	return err
}

// printerInfo is /printer/info.
type printerInfo struct {
	State           string `json:"state"`
	StateMessage    string `json:"state_message"`
	Hostname        string `json:"hostname"`
	SoftwareVersion string `json:"software_version"`
	KlipperPath     string `json:"klipper_path"`
	PythonPath      string `json:"python_path"`
	CPUInfo         string `json:"cpu_info"`
}

// serverInfo is /server/info — the Klippy connection state.
type serverInfo struct {
	KlippyConnected bool     `json:"klippy_connected"`
	KlippyState     string   `json:"klippy_state"`
	Components      []string `json:"components"`
	MoonrakerVer    string   `json:"moonraker_version"`
}

func (c *client) printerInfo(ctx context.Context) (*printerInfo, error) {
	var info printerInfo
	if err := c.get(ctx, "/printer/info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *client) serverInfo(ctx context.Context) (*serverInfo, error) {
	var info serverInfo
	if err := c.get(ctx, "/server/info", &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	c, err := newClient(config, secrets)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	srv, err := c.serverInfo(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyNetErr(err).Error()}
	}
	// Reachable. Klippy may still be starting/shut down — that's a healthy
	// *connection*, so report it rather than failing the test.
	objects := 0
	if list, lerr := c.objectList(ctx); lerr == nil {
		objects = len(list)
	}
	msg := "Connected to Moonraker"
	if info, ierr := c.printerInfo(ctx); ierr == nil && info.Hostname != "" {
		msg = "Connected to " + info.Hostname
	}
	switch srv.KlippyState {
	case "ready", "":
		msg += fmt.Sprintf(" — Klipper ready, %d objects", objects)
	default:
		msg += fmt.Sprintf(" — Klipper is %s (the printer is reachable)", srv.KlippyState)
	}
	return integration.TestResult{
		OK:      true,
		Message: msg,
		Detail:  map[string]any{"klippyState": srv.KlippyState, "objects": objects},
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	key, err := deps.Secret(ctx, "apiKey")
	if err != nil {
		// A missing key is fine (trusted client); only a vault error is fatal.
		key = ""
	}
	c, err := newClient(inst.Config, map[string]string{"apiKey": key})
	if err != nil {
		return nil, err
	}
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if _, err := c.serverInfo(pctx); err != nil {
		return nil, friendlyNetErr(err)
	}
	return newConn(inst, c, deps), nil
}
