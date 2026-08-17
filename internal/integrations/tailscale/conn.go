package tailscale

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/metrics"
)

// ---- upstream wire shapes ---------------------------------------------------

// Device is one tailnet node (GET devices?fields=all).
type Device struct {
	ID                 string   `json:"id"`
	NodeID             string   `json:"nodeId"`
	Name               string   `json:"name"`
	Hostname           string   `json:"hostname"`
	User               string   `json:"user"`
	Addresses          []string `json:"addresses"`
	OS                 string   `json:"os"`
	ClientVersion      string   `json:"clientVersion"`
	UpdateAvailable    bool     `json:"updateAvailable"`
	Authorized         bool     `json:"authorized"`
	IsExternal         bool     `json:"isExternal"`
	KeyExpiryDisabled  bool     `json:"keyExpiryDisabled"`
	ConnectedToControl bool     `json:"connectedToControl"`
	Created            TsTime   `json:"created"`
	Expires            TsTime   `json:"expires"`
	LastSeen           TsTime   `json:"lastSeen"`
	Tags               []string `json:"tags"`
	AdvertisedRoutes   []string `json:"advertisedRoutes"`
	EnabledRoutes      []string `json:"enabledRoutes"`
}

// Online reports whether the device is currently connected: the control
// flag when present, otherwise a recent lastSeen.
func (d Device) Online() bool {
	if d.ConnectedToControl {
		return true
	}
	return !d.LastSeen.IsZero() && time.Since(d.LastSeen.Time) < 5*time.Minute
}

// TsTime tolerates the API's empty-string timestamps.
type TsTime struct{ time.Time }

func (t *TsTime) UnmarshalJSON(raw []byte) error {
	if string(raw) == `""` || string(raw) == "null" {
		t.Time = time.Time{}
		return nil
	}
	return json.Unmarshal(raw, &t.Time)
}

func (t TsTime) MarshalJSON() ([]byte, error) { return json.Marshal(t.Time) }

// Key is one tailnet auth key. The secret `key` is only present on create.
type Key struct {
	ID           string          `json:"id"`
	Key          string          `json:"key"`
	Description  string          `json:"description"`
	Created      TsTime          `json:"created"`
	Expires      TsTime          `json:"expires"`
	Revoked      TsTime          `json:"revoked"`
	Invalid      bool            `json:"invalid"`
	Capabilities KeyCapabilities `json:"capabilities"`
}

type KeyCapabilities struct {
	Devices struct {
		Create struct {
			Reusable      bool     `json:"reusable"`
			Ephemeral     bool     `json:"ephemeral"`
			Preauthorized bool     `json:"preauthorized"`
			Tags          []string `json:"tags"`
		} `json:"create"`
	} `json:"devices"`
}

// CreateKeySpec is what HomeHub's API exposes for minting a key.
type CreateKeySpec struct {
	Description   string
	ExpirySeconds int64
	Reusable      bool
	Ephemeral     bool
	Preauthorized bool
	Tags          []string
}

// DNSSettings aggregates the three DNS endpoints.
type DNSSettings struct {
	Nameservers []string
	MagicDNS    bool
	SearchPaths []string
}

// ---- connection -------------------------------------------------------------

type Conn struct {
	inst   integration.Instance
	client *client
	deps   integration.Deps

	mu         sync.Mutex
	lastOnline map[string]bool // device id -> online (change detection)
}

func (c *Conn) Close() {}

// Devices fetches the device inventory, sorted by name, publishing a
// change event when any device's connectivity flips.
func (c *Conn) Devices(ctx context.Context) ([]Device, error) {
	devices, err := fetchDevices(ctx, c.client)
	if err != nil {
		return nil, err
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })

	changed := false
	c.mu.Lock()
	if c.lastOnline == nil {
		c.lastOnline = map[string]bool{}
	}
	for _, d := range devices {
		online := d.Online()
		if prev, seen := c.lastOnline[d.ID]; !seen || prev != online {
			c.lastOnline[d.ID] = online
			changed = true
		}
	}
	c.mu.Unlock()
	if changed {
		c.deps.Publish("devices-changed", nil)
	}
	return devices, nil
}

// SetAuthorized approves or revokes a device's tailnet membership.
func (c *Conn) SetAuthorized(ctx context.Context, deviceID string, authorized bool) error {
	return c.client.call(ctx, http.MethodPost, "/api/v2/device/"+url.PathEscape(deviceID)+"/authorized",
		map[string]any{"authorized": authorized}, nil)
}

// SetRoutes replaces the device's enabled subnet routes.
func (c *Conn) SetRoutes(ctx context.Context, deviceID string, routes []string) error {
	if routes == nil {
		routes = []string{}
	}
	return c.client.call(ctx, http.MethodPost, "/api/v2/device/"+url.PathEscape(deviceID)+"/routes",
		map[string]any{"routes": routes}, nil)
}

// SetKeyExpiryDisabled toggles key expiry for a device.
func (c *Conn) SetKeyExpiryDisabled(ctx context.Context, deviceID string, disabled bool) error {
	return c.client.call(ctx, http.MethodPost, "/api/v2/device/"+url.PathEscape(deviceID)+"/key",
		map[string]any{"keyExpiryDisabled": disabled}, nil)
}

// ExpireKey expires the device's node key immediately, forcing re-auth.
func (c *Conn) ExpireKey(ctx context.Context, deviceID string) error {
	return c.client.call(ctx, http.MethodPost, "/api/v2/device/"+url.PathEscape(deviceID)+"/expire", struct{}{}, nil)
}

// DeleteDevice removes the device from the tailnet.
func (c *Conn) DeleteDevice(ctx context.Context, deviceID string) error {
	return c.client.call(ctx, http.MethodDelete, "/api/v2/device/"+url.PathEscape(deviceID), nil, nil)
}

// Keys lists the tailnet's auth keys (secrets are never included).
func (c *Conn) Keys(ctx context.Context) ([]Key, error) {
	var resp struct {
		Keys []Key `json:"keys"`
	}
	if err := c.client.call(ctx, http.MethodGet, c.client.tailnetPath("keys"), nil, &resp); err != nil {
		return nil, err
	}
	// The list endpoint returns skeletons; fetch details so the UI can show
	// capabilities and expiry without a second click.
	out := make([]Key, 0, len(resp.Keys))
	for _, k := range resp.Keys {
		var full Key
		if err := c.client.call(ctx, http.MethodGet, c.client.tailnetPath("keys/"+url.PathEscape(k.ID)), nil, &full); err != nil {
			out = append(out, k) // keep the skeleton over dropping the key
			continue
		}
		full.Key = "" // defense in depth — never expose secrets after create
		out = append(out, full)
	}
	return out, nil
}

// CreateKey mints an auth key; the returned Key carries the secret ONCE.
func (c *Conn) CreateKey(ctx context.Context, spec CreateKeySpec) (*Key, error) {
	if spec.ExpirySeconds <= 0 {
		spec.ExpirySeconds = int64((90 * 24 * time.Hour).Seconds())
	}
	var caps KeyCapabilities
	caps.Devices.Create.Reusable = spec.Reusable
	caps.Devices.Create.Ephemeral = spec.Ephemeral
	caps.Devices.Create.Preauthorized = spec.Preauthorized
	caps.Devices.Create.Tags = spec.Tags
	body := map[string]any{
		"capabilities":  caps,
		"expirySeconds": spec.ExpirySeconds,
		"description":   spec.Description,
	}
	var key Key
	if err := c.client.call(ctx, http.MethodPost, c.client.tailnetPath("keys"), body, &key); err != nil {
		return nil, err
	}
	return &key, nil
}

// RevokeKey revokes an auth key.
func (c *Conn) RevokeKey(ctx context.Context, keyID string) error {
	return c.client.call(ctx, http.MethodDelete, c.client.tailnetPath("keys/"+url.PathEscape(keyID)), nil, nil)
}

// DNS reads the tailnet's DNS configuration from its three endpoints.
func (c *Conn) DNS(ctx context.Context) (*DNSSettings, error) {
	var ns struct {
		DNS []string `json:"dns"`
	}
	if err := c.client.call(ctx, http.MethodGet, c.client.tailnetPath("dns/nameservers"), nil, &ns); err != nil {
		return nil, err
	}
	var prefs struct {
		MagicDNS bool `json:"magicDNS"`
	}
	if err := c.client.call(ctx, http.MethodGet, c.client.tailnetPath("dns/preferences"), nil, &prefs); err != nil {
		return nil, err
	}
	var paths struct {
		SearchPaths []string `json:"searchPaths"`
	}
	if err := c.client.call(ctx, http.MethodGet, c.client.tailnetPath("dns/searchpaths"), nil, &paths); err != nil {
		return nil, err
	}
	return &DNSSettings{Nameservers: ns.DNS, MagicDNS: prefs.MagicDNS, SearchPaths: paths.SearchPaths}, nil
}

// SetDNS writes the tailnet DNS configuration. Order matters upstream:
// nameservers first — turning MagicDNS on requires at least one nameserver.
func (c *Conn) SetDNS(ctx context.Context, settings DNSSettings) error {
	if settings.Nameservers == nil {
		settings.Nameservers = []string{}
	}
	if settings.SearchPaths == nil {
		settings.SearchPaths = []string{}
	}
	if err := c.client.call(ctx, http.MethodPost, c.client.tailnetPath("dns/nameservers"),
		map[string]any{"dns": settings.Nameservers}, nil); err != nil {
		return err
	}
	if err := c.client.call(ctx, http.MethodPost, c.client.tailnetPath("dns/preferences"),
		map[string]any{"magicDNS": settings.MagicDNS}, nil); err != nil {
		return err
	}
	return c.client.call(ctx, http.MethodPost, c.client.tailnetPath("dns/searchpaths"),
		map[string]any{"searchPaths": settings.SearchPaths}, nil)
}

// ---- platform hooks ---------------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, err := c.Devices(ctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	devices, err := c.Devices(ctx)
	if err != nil {
		return nil, err
	}
	base := TypeID + "/" + c.inst.ID.String()
	out := make([]integration.Resource, 0, len(devices))
	for _, d := range devices {
		name := d.Name
		if i := strings.IndexByte(name, '.'); i > 0 {
			name = name[:i]
		}
		out = append(out, integration.Resource{
			Path: base + "/device/" + d.ID,
			Kind: "device",
			Name: name,
		})
	}
	return out, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	devices, err := c.Devices(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	res := TypeID + "/" + c.inst.ID.String()
	online := 0
	for _, d := range devices {
		if d.Online() {
			online++
		}
	}
	return []metrics.Sample{
		{Resource: res, Metric: "devices_online", Unit: "count", TS: now, Value: float64(online)},
		{Resource: res, Metric: "devices_total", Unit: "count", TS: now, Value: float64(len(devices))},
	}, nil
}

