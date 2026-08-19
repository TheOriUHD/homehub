package moonraker

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Moonraker owns the webcam registry (name, stream/snapshot URLs, and the
// per-camera flip_horizontal / flip_vertical / rotation display flags). We read
// it from /server/webcams/list, resolve the (usually relative) stream URLs to
// absolute printer URLs the BACKEND can reach, and expose flip toggles that
// persist through Moonraker so the setting sticks and stays in sync with
// Fluidd/Mainsail. The browser never touches the printer's plain-HTTP stream
// directly — it goes through the backend proxy (see api_moonraker webcam
// handlers), which is what makes cameras work over HomeHub's HTTPS origin and
// from off-LAN.

type webcamWire struct {
	Name           string `json:"name"`
	Location       string `json:"location"`
	Service        string `json:"service"`
	Enabled        *bool  `json:"enabled"`
	Icon           string `json:"icon"`
	TargetFPS      int    `json:"target_fps"`
	TargetFPSIdle  int    `json:"target_fps_idle"`
	StreamURL      string `json:"stream_url"`
	SnapshotURL    string `json:"snapshot_url"`
	FlipHorizontal bool   `json:"flip_horizontal"`
	FlipVertical   bool   `json:"flip_vertical"`
	Rotation       int    `json:"rotation"`
	AspectRatio    string `json:"aspect_ratio"`
}

// Camera is the dashboard-facing view (no raw printer URLs — the browser uses
// the backend proxy addressed by name).
type Camera struct {
	Name           string `json:"name"`
	Service        string `json:"service"`
	FlipHorizontal bool   `json:"flipHorizontal"`
	FlipVertical   bool   `json:"flipVertical"`
	Rotation       int    `json:"rotation"`
	TargetFPS      int    `json:"targetFps"`
	AspectRatio    string `json:"aspectRatio"`
	Snapshot       bool   `json:"snapshot"` // a still-image URL is available
}

const webcamTTL = 12 * time.Second

// webcams returns the cached webcam list, refreshing past the TTL.
func (c *Conn) webcams(ctx context.Context) ([]webcamWire, error) {
	c.mu.Lock()
	if !c.webcamsAt.IsZero() && time.Since(c.webcamsAt) < webcamTTL {
		cached := append([]webcamWire(nil), c.webcamsCache...)
		c.mu.Unlock()
		return cached, nil
	}
	c.mu.Unlock()

	var out struct {
		Webcams []webcamWire `json:"webcams"`
	}
	if err := c.client.get(ctx, "/server/webcams/list", &out); err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.webcamsCache, c.webcamsAt = out.Webcams, time.Now()
	c.mu.Unlock()
	return out.Webcams, nil
}

// invalidateWebcams forces the next webcams() to refetch (after a flip write).
func (c *Conn) invalidateWebcams() {
	c.mu.Lock()
	c.webcamsAt = time.Time{}
	c.mu.Unlock()
}

// Cameras builds the dashboard camera list from the cache (best-effort; a
// webcam fetch error just yields no cameras rather than failing the snapshot).
func (c *Conn) Cameras(ctx context.Context) []Camera {
	cams, err := c.webcams(ctx)
	if err != nil {
		return nil
	}
	out := make([]Camera, 0, len(cams))
	for _, w := range cams {
		if w.Enabled != nil && !*w.Enabled {
			continue
		}
		out = append(out, Camera{
			Name: w.Name, Service: w.Service,
			FlipHorizontal: w.FlipHorizontal, FlipVertical: w.FlipVertical,
			Rotation: w.Rotation, TargetFPS: w.TargetFPS, AspectRatio: w.AspectRatio,
			Snapshot: strings.TrimSpace(w.SnapshotURL) != "",
		})
	}
	return out
}

// camerasCached is the no-network variant for live WS snapshots.
func (c *Conn) camerasCached() []Camera {
	c.mu.Lock()
	cams := append([]webcamWire(nil), c.webcamsCache...)
	c.mu.Unlock()
	out := make([]Camera, 0, len(cams))
	for _, w := range cams {
		if w.Enabled != nil && !*w.Enabled {
			continue
		}
		out = append(out, Camera{
			Name: w.Name, Service: w.Service,
			FlipHorizontal: w.FlipHorizontal, FlipVertical: w.FlipVertical,
			Rotation: w.Rotation, TargetFPS: w.TargetFPS, AspectRatio: w.AspectRatio,
			Snapshot: strings.TrimSpace(w.SnapshotURL) != "",
		})
	}
	return out
}

// WebcamStreamURL resolves a named camera's live-stream URL to an absolute
// printer address the backend can fetch and proxy.
func (c *Conn) WebcamStreamURL(ctx context.Context, name string) (string, bool) {
	return c.resolveNamed(ctx, name, func(w webcamWire) string { return w.StreamURL })
}

// WebcamSnapshotURL resolves a named camera's still-image URL.
func (c *Conn) WebcamSnapshotURL(ctx context.Context, name string) (string, bool) {
	return c.resolveNamed(ctx, name, func(w webcamWire) string {
		if w.SnapshotURL != "" {
			return w.SnapshotURL
		}
		return w.StreamURL // fall back to the stream if no dedicated snapshot
	})
}

func (c *Conn) resolveNamed(ctx context.Context, name string, pick func(webcamWire) string) (string, bool) {
	cams, err := c.webcams(ctx)
	if err != nil {
		return "", false
	}
	for _, w := range cams {
		if w.Name == name {
			resolved := c.resolveWebcamURL(pick(w))
			return resolved, resolved != ""
		}
	}
	return "", false
}

// resolveWebcamURL turns Moonraker's stored URL (usually a host-relative path
// like "/webcam/?action=stream") into an absolute URL. Relative paths resolve
// against the printer host on its DEFAULT web port — the webcam is served by
// the machine's reverse proxy (nginx) on 80/443, not on Moonraker's own port
// (7125) — while an already-absolute URL (a direct streamer port) is used
// verbatim.
func (c *Conn) resolveWebcamURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		return raw
	}
	u, err := url.Parse(c.client.base)
	if err != nil {
		return ""
	}
	if !strings.HasPrefix(raw, "/") {
		raw = "/" + raw
	}
	// Drop Moonraker's port; the webcam lives behind the host's web server.
	return u.Scheme + "://" + u.Hostname() + raw
}

// SetWebcam persists per-camera display settings (flip H/V, rotation) through
// Moonraker via a read-modify-write, so the change sticks and is shared with
// the printer's other UIs. Nil fields are left unchanged.
func (c *Conn) SetWebcam(ctx context.Context, name string, flipH, flipV *bool, rotation *int) error {
	// Read the current item so we repost a complete, valid object.
	var item map[string]any
	{
		var res struct {
			Webcam map[string]any `json:"webcam"`
		}
		if err := c.client.get(ctx, "/server/webcams/item?name="+url.QueryEscape(name), &res); err != nil {
			return err
		}
		item = res.Webcam
	}
	if item == nil {
		return fmt.Errorf("camera %q not found", name)
	}
	if flipH != nil {
		item["flip_horizontal"] = *flipH
	}
	if flipV != nil {
		item["flip_vertical"] = *flipV
	}
	if rotation != nil {
		item["rotation"] = *rotation
	}
	// Server-managed fields must not be echoed back.
	delete(item, "source")
	delete(item, "uid")

	body, err := json.Marshal(item)
	if err != nil {
		return err
	}
	if err := c.client.postJSON(ctx, "/server/webcams/item", body); err != nil {
		return err
	}
	c.invalidateWebcams()
	return nil
}
