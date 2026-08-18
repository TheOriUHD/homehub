package truenas

import (
	"context"
	"math"
	"sort"
	"strings"
)

// ---- live performance stats (WebSocket reporting API) -----------------------
//
// Everything here reads the netdata-backed reporting API, which only exists
// on the JSON-RPC transport — verified against a live TrueNAS 26.0 box:
// interface traffic arrives in Kilobits/s with legend [time, received, sent],
// cpu as %CPU with an aggregate "cpu" column, memory.available and
// arcsize.size in bytes. Pre-/api/current systems simply get empty stats.

// StatPoint is one (unix-seconds, value) sample.
type StatPoint struct {
	TS    int64   `json:"ts"`
	Value float64 `json:"value"`
}

// LoadPoint is one load-average sample (1/5/15 minutes).
type LoadPoint struct {
	TS  int64   `json:"ts"`
	L1  float64 `json:"load1"`
	L5  float64 `json:"load5"`
	L15 float64 `json:"load15"`
}

// TrafficPoint is one interface throughput sample, bits/s both directions.
type TrafficPoint struct {
	TS     int64   `json:"ts"`
	InBps  float64 `json:"inBps"`
	OutBps float64 `json:"outBps"`
}

// IfaceTraffic couples one adapter's identity with its recent throughput.
type IfaceTraffic struct {
	Name      string         `json:"name"`
	Type      string         `json:"type"` // PHYSICAL | BRIDGE | VLAN | …
	LinkState string         `json:"linkState"`
	Up        bool           `json:"up"`
	Media     string         `json:"media,omitempty"` // e.g. "1000baseT/Full"
	Addresses []string       `json:"addresses,omitempty"`
	Points    []TrafficPoint `json:"points"`
}

// Stats is the performance snapshot for the dashboard: an hour of history.
type Stats struct {
	CPU           []StatPoint    `json:"cpu"`  // percent, 0..100
	Load          []LoadPoint    `json:"load"` // load averages
	MemAvailable  []StatPoint    `json:"memAvailable"`
	ArcSize       []StatPoint    `json:"arcSize"`
	TotalMemBytes int64          `json:"totalMemBytes"`
	Interfaces    []IfaceTraffic `json:"interfaces"`
}

// ifaceRow is one interface.query result.
type ifaceRow struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	State struct {
		LinkState string `json:"link_state"`
		Media     string `json:"active_media_subtype"`
	} `json:"state"`
	Aliases []struct {
		Type    string `json:"type"`
		Address string `json:"address"`
	} `json:"aliases"`
}

// netdataGraph is one reporting.netdata_graphs row.
type netdataGraph struct {
	Name        string   `json:"name"`
	Identifiers []string `json:"identifiers"`
}

// Interfaces lists the NAS's network adapters.
func (c *Conn) Interfaces(ctx context.Context) ([]ifaceRow, error) {
	var rows []ifaceRow
	if err := c.fetch(ctx, "interface.query", nil, "GET", "/interface", nil, &rows); err != nil {
		return nil, err
	}
	return rows, nil
}

// Stats gathers the hour-long performance history in two socket round-trips:
// the graph catalogue (for interface identifiers), then one batched
// netdata_get_data call for cpu, load, memory, ARC, and every interface.
func (c *Conn) Stats(ctx context.Context) (*Stats, error) {
	var info SystemInfo
	if err := c.fetch(ctx, "system.info", nil, "GET", "/system/info", nil, &info); err != nil {
		return nil, err
	}
	out := &Stats{TotalMemBytes: info.PhysicalMem}

	ifaces, err := c.Interfaces(ctx)
	if err != nil {
		ifaces = nil // adapters are additive; keep the rest of the page alive
	}

	if c.ws == nil {
		return out, nil // reporting needs the socket
	}

	// Which interfaces does netdata actually chart?
	var catalogue []netdataGraph
	graphIDs := map[string]bool{}
	if err := c.ws.call(ctx, "reporting.netdata_graphs", nil, &catalogue); err == nil {
		for _, g := range catalogue {
			if g.Name == "interface" {
				for _, id := range g.Identifiers {
					graphIDs[id] = true
				}
			}
		}
	}

	graphs := []map[string]string{
		{"name": "cpu"}, {"name": "load"}, {"name": "memory"}, {"name": "arcsize"},
	}
	for _, row := range ifaces {
		if graphIDs[row.Name] {
			graphs = append(graphs, map[string]string{"name": "interface", "identifier": row.Name})
		}
	}

	var results []reportingData
	if err := c.ws.call(ctx, "reporting.netdata_get_data",
		[]any{graphs, map[string]any{"unit": "HOUR", "page": 1}}, &results); err != nil {
		return out, nil // stats are best-effort; adapters still listed below
	}

	byIface := map[string][]TrafficPoint{}
	for _, g := range results {
		switch g.Name {
		case "cpu":
			out.CPU = downsampleAvg(pickColumn(g, "cpu"), 240)
		case "load":
			out.Load = downsampleLoad(g, 240)
		case "memory":
			out.MemAvailable = downsampleAvg(pickColumn(g, "available"), 240)
		case "arcsize":
			out.ArcSize = downsampleAvg(pickColumn(g, "size"), 240)
		case "interface":
			byIface[g.Identifier] = downsampleTraffic(g, 240)
		}
	}

	for _, row := range ifaces {
		out.Interfaces = append(out.Interfaces, IfaceTraffic{
			Name:      row.Name,
			Type:      row.Type,
			LinkState: row.State.LinkState,
			Up:        row.State.LinkState == "LINK_STATE_UP",
			Media:     cleanMedia(row.State.Media),
			Addresses: aliasAddrs(row),
			Points:    byIface[row.Name],
		})
	}
	// Physical adapters first, then by name.
	sort.SliceStable(out.Interfaces, func(i, j int) bool {
		pi, pj := out.Interfaces[i].Type == "PHYSICAL", out.Interfaces[j].Type == "PHYSICAL"
		if pi != pj {
			return pi
		}
		return out.Interfaces[i].Name < out.Interfaces[j].Name
	})
	return out, nil
}

func aliasAddrs(row ifaceRow) []string {
	var out []string
	for _, a := range row.Aliases {
		if a.Type == "INET" || a.Type == "INET6" {
			out = append(out, a.Address)
		}
	}
	return out
}

// cleanMedia hides netdata's "Unknown Other" placeholder.
func cleanMedia(s string) string {
	if strings.HasPrefix(strings.ToLower(s), "unknown") {
		return ""
	}
	return s
}

// pickColumn extracts one legend column as (ts, value) points. The first
// legend entry is the timestamp column.
func pickColumn(g reportingData, name string) []StatPoint {
	col := -1
	for i, l := range g.Legend {
		if l == name {
			col = i
			break
		}
	}
	if col < 1 {
		return nil
	}
	out := make([]StatPoint, 0, len(g.Data))
	for _, row := range g.Data {
		if len(row) <= col {
			continue
		}
		out = append(out, StatPoint{TS: int64(row[0]), Value: row[col]})
	}
	return out
}

// downsampleAvg buckets points to at most n, averaging within each bucket.
func downsampleAvg(points []StatPoint, n int) []StatPoint {
	if len(points) <= n {
		return points
	}
	size := (len(points) + n - 1) / n
	out := make([]StatPoint, 0, n)
	for i := 0; i < len(points); i += size {
		end := min(i+size, len(points))
		var sum float64
		for _, p := range points[i:end] {
			sum += p.Value
		}
		out = append(out, StatPoint{TS: points[i].TS, Value: sum / float64(end-i)})
	}
	return out
}

// downsampleLoad buckets load rows to at most n, averaging each series.
func downsampleLoad(g reportingData, n int) []LoadPoint {
	c1, c5, c15 := legendIndex(g, "shortterm"), legendIndex(g, "midterm"), legendIndex(g, "longterm")
	if c1 < 1 {
		return nil
	}
	var rows []LoadPoint
	for _, row := range g.Data {
		if len(row) <= c1 {
			continue
		}
		p := LoadPoint{TS: int64(row[0]), L1: row[c1]}
		if c5 >= 1 && len(row) > c5 {
			p.L5 = row[c5]
		}
		if c15 >= 1 && len(row) > c15 {
			p.L15 = row[c15]
		}
		rows = append(rows, p)
	}
	if len(rows) <= n {
		return rows
	}
	size := (len(rows) + n - 1) / n
	out := make([]LoadPoint, 0, n)
	for i := 0; i < len(rows); i += size {
		end := min(i+size, len(rows))
		var s1, s5, s15 float64
		for _, p := range rows[i:end] {
			s1 += p.L1
			s5 += p.L5
			s15 += p.L15
		}
		cnt := float64(end - i)
		out = append(out, LoadPoint{TS: rows[i].TS, L1: s1 / cnt, L5: s5 / cnt, L15: s15 / cnt})
	}
	return out
}

// downsampleTraffic converts an interface graph (Kilobits/s, legend
// [time, received, sent]) into bits/s points, bucketing with MAX so spikes
// survive — a traffic chart that hides bursts is lying.
func downsampleTraffic(g reportingData, n int) []TrafficPoint {
	rx, tx := legendIndex(g, "received"), legendIndex(g, "sent")
	if rx < 1 {
		return nil
	}
	var rows []TrafficPoint
	for _, row := range g.Data {
		if len(row) <= rx {
			continue
		}
		p := TrafficPoint{TS: int64(row[0]), InBps: math.Abs(row[rx]) * 1000}
		if tx >= 1 && len(row) > tx {
			p.OutBps = math.Abs(row[tx]) * 1000
		}
		rows = append(rows, p)
	}
	if len(rows) <= n {
		return rows
	}
	size := (len(rows) + n - 1) / n
	out := make([]TrafficPoint, 0, n)
	for i := 0; i < len(rows); i += size {
		end := min(i+size, len(rows))
		p := TrafficPoint{TS: rows[i].TS}
		for _, q := range rows[i:end] {
			p.InBps = math.Max(p.InBps, q.InBps)
			p.OutBps = math.Max(p.OutBps, q.OutBps)
		}
		out = append(out, p)
	}
	return out
}

func legendIndex(g reportingData, name string) int {
	for i, l := range g.Legend {
		if l == name {
			return i
		}
	}
	return -1
}

// ---- alert actions ----------------------------------------------------------

// DismissAlert acknowledges one alert by uuid.
func (c *Conn) DismissAlert(ctx context.Context, uuid string) error {
	return c.fetch(ctx, "alert.dismiss", []any{uuid}, "POST", "/alert/dismiss", uuid, nil)
}
