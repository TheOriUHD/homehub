package aperture

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// ---- upstream wire shapes ---------------------------------------------------
// Timestamps are epoch MILLISECONDS throughout.

type systemStatus struct {
	Cameras []CameraHealth `json:"cameras"`
}

// CameraHealth is one row of /system/status — the headline dashboard data.
type CameraHealth struct {
	CameraID          string `json:"cameraId"`
	Name              string `json:"name"`
	Enabled           bool   `json:"enabled"`
	RecordMode        string `json:"recordMode"`
	RecordingExpected bool   `json:"recordingExpected"`
	RecordingOk       bool   `json:"recordingOk"`
	LiveOk            bool   `json:"liveOk"`
	CoveragePercent   int    `json:"coveragePercent"`
	LastSegmentTs     int64  `json:"lastSegmentTs"`
	RestartsToday     int    `json:"restartsToday"`
	WatchdogStale     bool   `json:"watchdogStale"`
}

// Storage is /system/storage. Permanently at cap by design — the signal is
// the retention window, not "full".
type Storage struct {
	CapBytes int64           `json:"capBytes"`
	AppBytes int64           `json:"appBytes"`
	Cameras  []CameraStorage `json:"cameras"`
}

type CameraStorage struct {
	CameraID string `json:"cameraId"`
	Name     string `json:"name"`
	Bytes    int64  `json:"bytes"`
	Count    int    `json:"count"`
	OldestTs int64  `json:"oldestTs"`
	NewestTs int64  `json:"newestTs"`
}

type bitrateResponse struct {
	Buckets []BitrateBucket `json:"buckets"`
}

type BitrateBucket struct {
	Ts  int64 `json:"ts"`
	Bps int64 `json:"bps"`
}

// Camera is the WHITELISTED subset of /cameras. The upstream objects carry
// rtspMainUrl/rtspSubUrl with plaintext camera credentials; those fields do
// not exist here, so they are dropped at decode and cannot leak.
type Camera struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Enabled         bool   `json:"enabled"`
	RecordMode      string `json:"recordMode"`
	SortOrder       int    `json:"sortOrder"`
	RetentionDays   int    `json:"retentionDays"`
	MotionDetection bool   `json:"motionDetection"`
}

// StreamToken is /cameras/{id}/stream-token: 5 minutes, one camera.
type StreamToken struct {
	Token  string `json:"token"`
	WsURL  string `json:"wsUrl"`
	HasSub bool   `json:"hasSub"`
}

// ---- connection with server-side caches -------------------------------------

// Cache TTLs sized to the doc's recommendation, comfortably inside the
// burst-40 / refill-20-per-minute token bucket.
const (
	statusTTL  = 10 * time.Second
	storageTTL = 60 * time.Second
	bitrateTTL = 60 * time.Second
	camerasTTL = 5 * time.Minute
)

type cached[T any] struct {
	value   T
	fetched time.Time
}

type Conn struct {
	inst   integration.Instance
	client *client
	deps   integration.Deps

	mu      sync.Mutex
	status  cached[[]CameraHealth]
	storage cached[*Storage]
	bitrate cached[[]BitrateBucket]
	cameras cached[[]Camera]
}

func newConn(inst integration.Instance, c *client, deps integration.Deps) *Conn {
	return &Conn{inst: inst, client: c, deps: deps}
}

func (c *Conn) Close() {}

// fetchCached returns the cached value when fresh, otherwise fetches under
// the lock (one upstream call however many HomeHub clients ask).
func fetchCached[T any](ctx context.Context, c *Conn, slot *cached[T], ttl time.Duration, fetch func(ctx context.Context) (T, error)) (T, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !slot.fetched.IsZero() && time.Since(slot.fetched) < ttl {
		return slot.value, nil
	}
	value, err := fetch(ctx)
	if err != nil {
		// Serve stale data over an error when we have it — a blip at
		// Aperture should degrade the tab, not blank it.
		if !slot.fetched.IsZero() && time.Since(slot.fetched) < 5*ttl {
			return slot.value, nil
		}
		return value, err
	}
	slot.value, slot.fetched = value, time.Now()
	return value, nil
}

// Status is the camera health list (cached ~10s).
func (c *Conn) Status(ctx context.Context) ([]CameraHealth, error) {
	return fetchCached(ctx, c, &c.status, statusTTL, func(ctx context.Context) ([]CameraHealth, error) {
		var out systemStatus
		if err := c.client.get(ctx, "/system/status", &out); err != nil {
			return nil, err
		}
		return out.Cameras, nil
	})
}

// StorageInfo is the storage/retention snapshot (cached ~60s).
func (c *Conn) StorageInfo(ctx context.Context) (*Storage, error) {
	return fetchCached(ctx, c, &c.storage, storageTTL, func(ctx context.Context) (*Storage, error) {
		var out Storage
		if err := c.client.get(ctx, "/system/storage", &out); err != nil {
			return nil, err
		}
		return &out, nil
	})
}

// Bitrate is the last 30 minutes of throughput (cached ~60s).
func (c *Conn) Bitrate(ctx context.Context) ([]BitrateBucket, error) {
	return fetchCached(ctx, c, &c.bitrate, bitrateTTL, func(ctx context.Context) ([]BitrateBucket, error) {
		var out bitrateResponse
		if err := c.client.get(ctx, "/system/bitrate?minutes=30", &out); err != nil {
			return nil, err
		}
		return out.Buckets, nil
	})
}

// Cameras is the whitelisted camera list (cached ~5min), in Aperture's own
// sort order so both apps agree.
func (c *Conn) Cameras(ctx context.Context) ([]Camera, error) {
	return fetchCached(ctx, c, &c.cameras, camerasTTL, func(ctx context.Context) ([]Camera, error) {
		var out []Camera
		if err := c.client.get(ctx, "/cameras", &out); err != nil {
			return nil, err
		}
		sort.SliceStable(out, func(i, j int) bool { return out[i].SortOrder < out[j].SortOrder })
		return out, nil
	})
}

// MintStreamToken exchanges the long-lived token for a 5-minute,
// single-camera token and returns the ABSOLUTE WebSocket URL the browser
// connects to. Requires the raw camera ID — handles 404 on this route.
func (c *Conn) MintStreamToken(ctx context.Context, cameraID string) (*StreamToken, error) {
	var token StreamToken
	if err := c.client.get(ctx, "/cameras/"+url.PathEscape(cameraID)+"/stream-token", &token); err != nil {
		return nil, err
	}
	if token.WsURL != "" && strings.HasPrefix(token.WsURL, "/") {
		token.WsURL = c.client.wsBase() + token.WsURL
	}
	return &token, nil
}

// BaseURL is the Aperture origin, for the "Open Aperture" link.
func (c *Conn) BaseURL() string { return c.client.base }

// ---- platform hooks ---------------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	cameras, err := c.Status(ctx)
	if err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: err.Error()}
	}
	stale := 0
	for _, cam := range cameras {
		if cam.Enabled && (cam.WatchdogStale || (cam.RecordingExpected && !cam.RecordingOk)) {
			stale++
		}
	}
	if stale > 0 {
		return integration.Health{
			Status:  integration.StatusDegraded,
			Message: pluralCameras(stale) + " not recording",
		}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func pluralCameras(n int) string {
	if n == 1 {
		return "1 camera is"
	}
	return fmt.Sprintf("%d cameras are", n)
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	cameras, err := c.Cameras(ctx)
	if err != nil {
		return nil, err
	}
	base := TypeID + "/" + c.inst.ID.String()
	out := make([]integration.Resource, 0, len(cameras))
	for _, cam := range cameras {
		out = append(out, integration.Resource{
			Path: base + "/camera/" + cam.ID,
			Kind: "camera",
			Name: cam.Name,
		})
	}
	return out, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	now := time.Now()
	base := TypeID + "/" + c.inst.ID.String()
	var samples []metrics.Sample

	if cameras, err := c.Status(ctx); err == nil {
		for _, cam := range cameras {
			res := base + "/camera/" + cam.CameraID
			recording := 0.0
			if cam.RecordingOk {
				recording = 1
			}
			samples = append(samples,
				metrics.Sample{Resource: res, Metric: "coverage_pct", Unit: "percent", TS: now, Value: float64(cam.CoveragePercent)},
				metrics.Sample{Resource: res, Metric: "recording", Unit: "bool", TS: now, Value: recording},
			)
		}
	}
	if storage, err := c.StorageInfo(ctx); err == nil {
		for _, cam := range storage.Cameras {
			res := base + "/camera/" + cam.CameraID
			samples = append(samples,
				metrics.Sample{Resource: res, Metric: "storage_bytes", Unit: "bytes", TS: now, Value: float64(cam.Bytes)},
			)
			if cam.NewestTs > cam.OldestTs {
				samples = append(samples, metrics.Sample{
					Resource: res, Metric: "retention_hours", Unit: "hours", TS: now,
					Value: float64(cam.NewestTs-cam.OldestTs) / 3_600_000,
				})
			}
		}
	}
	if buckets, err := c.Bitrate(ctx); err == nil && len(buckets) > 0 {
		samples = append(samples, metrics.Sample{
			Resource: base, Metric: "bitrate_bps", Unit: "bps", TS: now,
			Value: float64(buckets[len(buckets)-1].Bps),
		})
	}
	return samples, nil
}
