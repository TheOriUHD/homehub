package truenas

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

// ---- upstream shapes (REST v2.0) -------------------------------------------

// SystemInfo is /system/info.
type SystemInfo struct {
	Version       string    `json:"version"`
	Hostname      string    `json:"hostname"`
	Uptime        string    `json:"uptime"`
	UptimeSeconds float64   `json:"uptime_seconds"`
	PhysicalMem   int64     `json:"physmem"`
	Model         string    `json:"system_product"`
	Cores         int       `json:"cores"`
	LoadAvg       []float64 `json:"loadavg"`
}

// Pool is /pool. size/allocated/free are strings or numbers depending on
// version; captured as json.Number-friendly int64 via the topology summary.
type Pool struct {
	ID            int             `json:"id"`
	Name          string          `json:"name"`
	Status        string          `json:"status"`
	Healthy       bool            `json:"healthy"`
	Size          int64           `json:"size"`
	Allocated     int64           `json:"allocated"`
	Free          int64           `json:"free"`
	Fragmentation string          `json:"fragmentation"`
	Topology      TopologySummary `json:"topology"`
}

type TopologySummary struct {
	Data []VDev `json:"data"`
}

type VDev struct {
	Type   string `json:"type"`
	Status string `json:"status"`
}

// Dataset is /pool/dataset. Numeric props come nested as {parsed, rawvalue}.
type Dataset struct {
	ID          string      `json:"id"`
	Name        string      `json:"name"`
	Type        string      `json:"type"`
	Used        ParsedValue `json:"used"`
	Available   ParsedValue `json:"available"`
	Compression ParsedValue `json:"compression"`
	Mountpoint  string      `json:"mountpoint"`
}

// ParsedValue matches TrueNAS's {parsed, rawvalue, value} property shape.
type ParsedValue struct {
	Parsed   any    `json:"parsed"`
	RawValue string `json:"rawvalue"`
	Value    string `json:"value"`
}

func (p ParsedValue) Int64() int64 {
	switch v := p.Parsed.(type) {
	case float64:
		return int64(v)
	case string:
		n, _ := strconv.ParseInt(v, 10, 64)
		return n
	}
	if p.RawValue != "" {
		n, _ := strconv.ParseInt(p.RawValue, 10, 64)
		return n
	}
	return 0
}

func (p ParsedValue) String() string {
	if p.Value != "" {
		return p.Value
	}
	if s, ok := p.Parsed.(string); ok {
		return s
	}
	return p.RawValue
}

// Disk is /disk.
type Disk struct {
	Name        string `json:"name"`
	Serial      string `json:"serial"`
	Model       string `json:"model"`
	Size        int64  `json:"size"`
	Type        string `json:"type"` // HDD | SSD
	Temperature *int   `json:"temperature,omitempty"`
	Pool        string `json:"pool"`
}

// Share shapes for SMB, NFS, iSCSI.
type SMBShare struct {
	ID      int    `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
	Comment string `json:"comment"`
}

type NFSShare struct {
	ID      int      `json:"id"`
	Paths   []string `json:"paths"`
	Path    string   `json:"path"`
	Enabled bool     `json:"enabled"`
	Comment string   `json:"comment"`
}

type ISCSITarget struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Alias string `json:"alias"`
}

// Snapshot is one pool.snapshot.query row. Modern TrueNAS (26.x) strips
// used/created from the default payload — they come back only as ZFS
// properties requested via extra.properties — while the legacy REST shape
// carried them at the top level. Both are decoded; use the accessors.
type Snapshot struct {
	Name     string      `json:"name"`
	Dataset  string      `json:"dataset"`
	Snapshot string      `json:"snapshot_name"`
	Used     ParsedValue `json:"used"` // legacy REST shape
	Created  struct {
		Date int64 `json:"$date"`
	} `json:"created"` // legacy REST shape
	CreateTXG  string `json:"createtxg"`
	Properties struct {
		Used     ParsedValue `json:"used"`
		Creation ParsedValue `json:"creation"` // rawvalue = Unix seconds
	} `json:"properties"`
}

// UsedBytes returns the snapshot's space usage from whichever shape arrived.
func (s Snapshot) UsedBytes() int64 {
	if v := s.Properties.Used.Int64(); v > 0 {
		return v
	}
	return s.Used.Int64()
}

// CreatedMs returns the creation time in Unix milliseconds, from the legacy
// $date field or the ZFS creation property (whose rawvalue is seconds).
func (s Snapshot) CreatedMs() int64 {
	if s.Created.Date > 0 {
		return s.Created.Date
	}
	if v := s.Properties.Creation.Int64(); v > 0 {
		return v * 1000
	}
	return 0
}

// Replication is /replication.
type Replication struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	State struct {
		State string `json:"state"`
	} `json:"state"`
	Enabled bool `json:"enabled"`
}

// FlexTime tolerates both timestamp encodings TrueNAS has used: the legacy
// extended-JSON object {"$date": ms} and the ISO date-time string modern
// releases emit. An unrecognized shape decodes to zero rather than failing
// the whole payload — the timestamp is cosmetic.
type FlexTime struct {
	Ms int64
}

func (f *FlexTime) UnmarshalJSON(b []byte) error {
	var obj struct {
		Date int64 `json:"$date"`
	}
	if err := json.Unmarshal(b, &obj); err == nil && obj.Date > 0 {
		f.Ms = obj.Date
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil && s != "" {
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05.999999", "2006-01-02 15:04:05"} {
			if t, perr := time.Parse(layout, s); perr == nil {
				f.Ms = t.UnixMilli()
				return nil
			}
		}
	}
	return nil
}

// Alert is one alert.list row.
type Alert struct {
	UUID      string   `json:"uuid"`
	Level     string   `json:"level"`
	Formatted string   `json:"formatted"`
	Dismissed bool     `json:"dismissed"`
	Datetime  FlexTime `json:"datetime"`
	OneShot   bool     `json:"one_shot"`
}

// ---- normalized model ------------------------------------------------------

type NormPool struct {
	Name           string  `json:"name"`
	Status         string  `json:"status"`
	Healthy        bool    `json:"healthy"`
	SizeBytes      int64   `json:"sizeBytes"`
	AllocatedBytes int64   `json:"allocatedBytes"`
	FreeBytes      int64   `json:"freeBytes"`
	UsedPct        float64 `json:"usedPct"`
	Fragmentation  string  `json:"fragmentation,omitempty"`
}

type NormDataset struct {
	Name           string `json:"name"`
	Type           string `json:"type"`
	UsedBytes      int64  `json:"usedBytes"`
	AvailableBytes int64  `json:"availableBytes"`
	Compression    string `json:"compression,omitempty"`
	Mountpoint     string `json:"mountpoint,omitempty"`
}

type NormShare struct {
	Type    string `json:"type"` // smb | nfs | iscsi
	Name    string `json:"name"`
	Path    string `json:"path,omitempty"`
	Enabled bool   `json:"enabled"`
	Comment string `json:"comment,omitempty"`
}

// ---- connection ------------------------------------------------------------

type Conn struct {
	inst   integration.Instance
	client *client
	// ws is the primary transport (always armed): the JSON-RPC WebSocket
	// API modern TrueNAS ships. client is the REST shim kept for systems
	// that predate /api/current.
	ws *wsClient
}

// fetch reads one resource, WebSocket JSON-RPC first — the only API modern
// TrueNAS ships (REST v2.0 was deprecated in 25.04 and removed after). The
// REST shim survives solely as the fallback for systems that predate
// /api/current (CORE, SCALE ≤ 24.x), reached only when the socket fails at
// the transport level. Auth rejections and API-level errors are surfaced,
// never downgraded: the request reached TrueNAS, REST can't do better.
func (c *Conn) fetch(ctx context.Context, wsMethod string, wsParams []any, restVerb, restPath string, restBody, out any) error {
	if c.ws == nil {
		return c.client.call(ctx, restVerb, restPath, restBody, out)
	}
	wsErr := c.ws.call(ctx, wsMethod, wsParams, out)
	if wsErr == nil || errors.Is(wsErr, errWsAuth) {
		return wsErr
	}
	var rpcErr *rpcError
	if errors.As(wsErr, &rpcErr) {
		return wsErr
	}
	// Transport-level trouble (no /api/current, dead socket): try REST.
	restErr := c.client.call(ctx, restVerb, restPath, restBody, out)
	if restErr == nil {
		return nil
	}
	if strings.Contains(restErr.Error(), "(404)") {
		// A 404 from /api/v2.0 means the REST API no longer exists — that
		// noise would mask the real problem, so report the socket's error.
		return wsErr
	}
	return restErr
}

func (c *Conn) Info(ctx context.Context) (*SystemInfo, error) {
	var info SystemInfo
	if err := c.fetch(ctx, "system.info", nil, "GET", "/system/info", nil, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Conn) Pools(ctx context.Context) ([]NormPool, error) {
	var pools []Pool
	if err := c.fetch(ctx, "pool.query", nil, "GET", "/pool", nil, &pools); err != nil {
		return nil, err
	}
	out := make([]NormPool, 0, len(pools))
	for _, p := range pools {
		np := NormPool{
			Name: p.Name, Status: p.Status, Healthy: p.Healthy,
			SizeBytes: p.Size, AllocatedBytes: p.Allocated, FreeBytes: p.Free,
			Fragmentation: p.Fragmentation,
		}
		if p.Size > 0 {
			np.UsedPct = float64(p.Allocated) / float64(p.Size) * 100
		}
		out = append(out, np)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Conn) Datasets(ctx context.Context) ([]NormDataset, error) {
	var datasets []Dataset
	if err := c.fetch(ctx, "pool.dataset.query", nil, "GET", "/pool/dataset", nil, &datasets); err != nil {
		return nil, err
	}
	out := make([]NormDataset, 0, len(datasets))
	for _, d := range datasets {
		out = append(out, NormDataset{
			Name: d.Name, Type: d.Type,
			UsedBytes: d.Used.Int64(), AvailableBytes: d.Available.Int64(),
			Compression: d.Compression.String(), Mountpoint: d.Mountpoint,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (c *Conn) Disks(ctx context.Context) ([]Disk, error) {
	var disks []Disk
	// Modern TrueNAS only joins pool names on request, and v26 moved
	// temperature out of disk.query entirely — ask for the join here and
	// batch-read temperatures separately.
	wsParams := []any{[]any{}, map[string]any{"extra": map[string]any{"pools": true}}}
	if err := c.fetch(ctx, "disk.query", wsParams, "GET", "/disk", nil, &disks); err != nil {
		return nil, err
	}
	c.fillTemperatures(ctx, disks)
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks, nil
}

// fillTemperatures merges disk.temperatures (name → °C) into the list.
// Best-effort: TrueNAS serves cached values at most once per 5 minutes,
// old systems lack the method, and a disk with no reading maps to null.
func (c *Conn) fillTemperatures(ctx context.Context, disks []Disk) {
	if c.ws == nil || len(disks) == 0 {
		return
	}
	names := make([]string, 0, len(disks))
	for _, d := range disks {
		if d.Name != "" {
			names = append(names, d.Name)
		}
	}
	var temps map[string]any
	if err := c.ws.call(ctx, "disk.temperatures", []any{names}, &temps); err != nil {
		// Best-effort, but leave a trace: silent loss of temperatures made
		// an earlier regression invisible.
		slog.Warn("truenas: disk.temperatures failed", "error", err)
		return
	}
	for i := range disks {
		if v, ok := temps[disks[i].Name].(float64); ok {
			t := int(v)
			disks[i].Temperature = &t
		}
	}
}

func (c *Conn) Shares(ctx context.Context) ([]NormShare, error) {
	out := []NormShare{}
	var smb []SMBShare
	if err := c.fetch(ctx, "sharing.smb.query", nil, "GET", "/sharing/smb", nil, &smb); err == nil {
		for _, s := range smb {
			out = append(out, NormShare{Type: "smb", Name: s.Name, Path: s.Path, Enabled: s.Enabled, Comment: s.Comment})
		}
	}
	var nfs []NFSShare
	if err := c.fetch(ctx, "sharing.nfs.query", nil, "GET", "/sharing/nfs", nil, &nfs); err == nil {
		for _, s := range nfs {
			path := s.Path
			if path == "" && len(s.Paths) > 0 {
				path = strings.Join(s.Paths, ", ")
			}
			name := path
			out = append(out, NormShare{Type: "nfs", Name: name, Path: path, Enabled: s.Enabled, Comment: s.Comment})
		}
	}
	var targets []ISCSITarget
	if err := c.fetch(ctx, "iscsi.target.query", nil, "GET", "/iscsi/target", nil, &targets); err == nil {
		for _, t := range targets {
			name := t.Name
			if t.Alias != "" {
				name = t.Alias
			}
			out = append(out, NormShare{Type: "iscsi", Name: name, Enabled: true})
		}
	}
	return out, nil
}

func (c *Conn) Snapshots(ctx context.Context, limit int) ([]Snapshot, error) {
	var snaps []Snapshot
	// used/created only arrive when asked for as ZFS properties (26.x).
	wsParams := []any{[]any{}, map[string]any{
		"extra": map[string]any{"properties": []string{"used", "creation"}},
	}}
	if err := c.fetch(ctx, "pool.snapshot.query", wsParams, "GET", "/pool/snapshot", nil, &snaps); err != nil {
		return nil, err
	}
	sort.Slice(snaps, func(i, j int) bool {
		ci, cj := snaps[i].CreatedMs(), snaps[j].CreatedMs()
		if ci != cj {
			return ci > cj
		}
		// No timestamps at all: fall back to the transaction group counter.
		ti, _ := strconv.ParseInt(snaps[i].CreateTXG, 10, 64)
		tj, _ := strconv.ParseInt(snaps[j].CreateTXG, 10, 64)
		return ti > tj
	})
	if limit > 0 && len(snaps) > limit {
		snaps = snaps[:limit]
	}
	return snaps, nil
}

func (c *Conn) Alerts(ctx context.Context) ([]Alert, error) {
	var alerts []Alert
	if err := c.fetch(ctx, "alert.list", nil, "GET", "/alert/list", nil, &alerts); err != nil {
		return nil, err
	}
	// TrueNAS embeds HTML (<br>, links) in alert texts; the UI wants prose.
	for i := range alerts {
		alerts[i].Formatted = stripHTML(alerts[i].Formatted)
	}
	return alerts, nil
}

// stripHTML flattens the markup TrueNAS puts into alert messages.
func stripHTML(s string) string {
	s = strings.ReplaceAll(s, "<br>", " ")
	s = strings.ReplaceAll(s, "<br/>", " ")
	s = strings.ReplaceAll(s, "<br />", " ")
	var b strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func (c *Conn) Replications(ctx context.Context) ([]Replication, error) {
	var reps []Replication
	if err := c.fetch(ctx, "replication.query", nil, "GET", "/replication", nil, &reps); err != nil {
		return nil, err
	}
	return reps, nil
}

// MemoryStats is the RAM allocation breakdown for the dashboard dial.
type MemoryStats struct {
	TotalBytes    int64 `json:"totalBytes"`
	ArcBytes      int64 `json:"arcBytes"`      // ZFS cache
	ServicesBytes int64 `json:"servicesBytes"` // everything else in use
	FreeBytes     int64 `json:"freeBytes"`
}

// reportingData is one reporting graph result.
type reportingData struct {
	Name       string      `json:"name"`
	Identifier string      `json:"identifier"`
	Legend     []string    `json:"legend"`
	Data       [][]float64 `json:"data"`
	Latest     []float64   `json:"latest"`
	// aggregations is deliberately not decoded: its shape changed between
	// TrueNAS releases (array pre-25.x, object after) and nothing here
	// needs it.
}

// lastPoint returns the newest non-empty row of a graph.
func (r reportingData) lastPoint() []float64 {
	if len(r.Latest) > 0 {
		return r.Latest
	}
	for i := len(r.Data) - 1; i >= 0; i-- {
		if len(r.Data[i]) > 0 {
			return r.Data[i]
		}
	}
	return nil
}

// legendValue finds a column by legend name in a data row (skipping the
// leading timestamp column when present).
func (r reportingData) legendValue(point []float64, name string) (float64, bool) {
	offset := len(point) - len(r.Legend)
	if offset < 0 {
		return 0, false
	}
	for i, l := range r.Legend {
		if l == name {
			return point[offset+i], true
		}
	}
	return 0, false
}

// Memory fetches the RAM breakdown via the reporting API: the memory graph
// (free/used) plus the arcsize graph (ZFS cache). Values arrive in bytes.
func (c *Conn) Memory(ctx context.Context) (*MemoryStats, error) {
	var info SystemInfo
	if err := c.fetch(ctx, "system.info", nil, "GET", "/system/info", nil, &info); err != nil {
		return nil, err
	}
	graphsParam := []map[string]string{{"name": "memory"}, {"name": "arcsize"}}
	var graphs []reportingData
	var wsErr error
	fetched := false
	if c.ws != nil {
		wsErr = c.ws.call(ctx, "reporting.netdata_get_data",
			[]any{graphsParam, map[string]any{"unit": "HOUR", "page": 1}}, &graphs)
		fetched = wsErr == nil
	}
	if !fetched {
		// REST fallback for pre-/api/current systems. 25.x renamed the query
		// field and requires page >= 1; older releases only accept the
		// reporting_query form.
		newBody := map[string]any{
			"graphs": graphsParam,
			"query":  map[string]any{"unit": "HOUR", "page": 1},
		}
		if err := c.client.call(ctx, "POST", "/reporting/get_data", newBody, &graphs); err != nil {
			legacyBody := map[string]any{
				"graphs":          graphsParam,
				"reporting_query": map[string]any{"unit": "HOUR", "page": 0},
			}
			if lerr := c.client.call(ctx, "POST", "/reporting/get_data", legacyBody, &graphs); lerr != nil {
				if wsErr != nil && strings.Contains(err.Error(), "(404)") {
					return nil, wsErr // REST is gone — the socket error is the real one
				}
				return nil, err
			}
		}
	}
	stats := &MemoryStats{TotalBytes: info.PhysicalMem}
	for _, g := range graphs {
		point := g.lastPoint()
		if point == nil {
			continue
		}
		switch g.Name {
		case "memory":
			if v, ok := g.legendValue(point, "free"); ok {
				stats.FreeBytes = int64(v)
			}
			if v, ok := g.legendValue(point, "available"); ok && stats.FreeBytes == 0 {
				stats.FreeBytes = int64(v)
			}
		case "arcsize":
			// Legend is "size" on modern netdata reporting, "arc_size" on
			// older releases.
			if v, ok := g.legendValue(point, "size"); ok {
				stats.ArcBytes = int64(v)
			} else if v, ok := g.legendValue(point, "arc_size"); ok {
				stats.ArcBytes = int64(v)
			} else if len(point) > 0 {
				stats.ArcBytes = int64(point[len(point)-1])
			}
		}
	}
	if used := stats.TotalBytes - stats.FreeBytes - stats.ArcBytes; used > 0 {
		stats.ServicesBytes = used
	}
	return stats, nil
}

// ---- integration.Connection ------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var info SystemInfo
	if err := c.fetch(hctx, "system.info", nil, "GET", "/system/info", nil, &info); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	// A pool in a degraded/faulted state is reachable but unhealthy.
	if pools, err := c.Pools(hctx); err == nil {
		for _, p := range pools {
			if !p.Healthy {
				return integration.Health{Status: integration.StatusDegraded, Message: "pool " + p.Name + " is " + p.Status}
			}
		}
	}
	return integration.Health{Status: integration.StatusConnected}
}

// Resources publishes one node per pool for per-pool grants.
func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	pools, err := c.Pools(ctx)
	if err != nil {
		return nil, err
	}
	root := c.inst.ResourcePath()
	out := make([]integration.Resource, 0, len(pools))
	for _, p := range pools {
		out = append(out, integration.Resource{Path: root + "/pool/" + p.Name, Kind: "pool", Name: p.Name})
	}
	return out, nil
}

// Collect samples per-pool capacity and per-disk temperature.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	now := time.Now()
	root := c.inst.ResourcePath()
	var samples []metrics.Sample

	pools, err := c.Pools(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pools {
		samples = append(samples,
			metrics.Sample{Resource: root + "/pool/" + p.Name, Metric: "used_pct", Unit: "%", TS: now, Value: p.UsedPct},
			metrics.Sample{Resource: root + "/pool/" + p.Name, Metric: "free_bytes", Unit: "bytes", TS: now, Value: float64(p.FreeBytes)},
		)
	}
	if disks, err := c.Disks(ctx); err == nil {
		for _, d := range disks {
			if d.Temperature != nil {
				samples = append(samples, metrics.Sample{
					Resource: root + "/disk/" + d.Name, Metric: "temperature", Unit: "°C", TS: now, Value: float64(*d.Temperature),
				})
			}
		}
	}
	return samples, nil
}

func (c *Conn) Close() {}
