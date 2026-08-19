package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"homehub/internal/httpctx"
	"homehub/internal/netscan"
	"homehub/internal/rbac"
)

// IP scanner: sweep a LAN range from the HomeHub server and report per host
// alive state, hostname, MAC/vendor, and open ports — an Angry-IP-Scanner-
// style tool. The scan streams NDJSON so the UI fills rows in live; the
// interfaces list is a plain typed endpoint.

func (s *Server) NetscanInterfaces(ctx context.Context, _ NetscanInterfacesRequestObject) (NetscanInterfacesResponseObject, error) {
	if err := s.requireToolsControl(ctx); err != nil {
		return nil, err
	}
	nets := netscan.LocalNetworks()
	out := make(NetscanInterfaces200JSONResponse, 0, len(nets))
	for _, n := range nets {
		out = append(out, NetscanInterface{
			Label:   n.Label,
			Cidr:    n.CIDR,
			Start:   n.Start,
			End:     n.End,
			Primary: n.Primary,
		})
	}
	return out, nil
}

// HandleNetscan streams a scan as NDJSON: POST /api/netscan with
// {start, end, ports?}. Each line is one JSON object — a "meta" with the
// total, then one "host" per address as it completes, then "done". The
// client's disconnect cancels the request context, which stops the scan.
// netscanSem bounds concurrently running scans across all users. A full-range
// scan peaks at ~1.5k goroutines plus that many in-flight sockets; without a
// cap a user firing many concurrent POSTs could exhaust the host's FDs and
// starve the service. Excess requests get 429; normal single-scan use never
// touches the limit.
var netscanSem = make(chan struct{}, 3)

func (s *Server) HandleNetscan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := httpctx.IdentityFrom(ctx)
	if id == nil {
		http.Error(w, `{"type":"error","message":"sign in first"}`, http.StatusUnauthorized)
		return
	}
	// The IP scanner drives the server to sweep/probe arbitrary LAN ranges, so
	// it is a control-level utility, not an all-users tool (see ResTools).
	if !s.can(ctx, id, ResTools, rbac.LevelControl) {
		s.deniedAudit(ctx, id, "netscan", ResTools)
		http.Error(w, `{"type":"error","message":"you do not have permission for this"}`, http.StatusForbidden)
		return
	}
	select {
	case netscanSem <- struct{}{}:
		defer func() { <-netscanSem }()
	default:
		http.Error(w, `{"type":"error","message":"too many scans in progress — try again in a moment"}`, http.StatusTooManyRequests)
		return
	}
	var body struct {
		Start string `json:"start"`
		End   string `json:"end"`
		Ports []int  `json:"ports"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, `{"type":"error","message":"bad request"}`, http.StatusBadRequest)
		return
	}
	start := netscan.ParseIP4(body.Start)
	end := netscan.ParseIP4(body.End)
	if start == nil || end == nil {
		http.Error(w, `{"type":"error","message":"start and end must be IPv4 addresses"}`, http.StatusBadRequest)
		return
	}
	total := netscan.EnumerateCount(start, end)
	if total == 0 {
		http.Error(w, `{"type":"error","message":"invalid range (reversed, or larger than 65536 addresses)"}`, http.StatusBadRequest)
		return
	}
	// Guard the port list: bounded count, valid range.
	ports := body.Ports
	if len(ports) > 500 {
		ports = ports[:500]
	}
	clean := ports[:0]
	for _, p := range ports {
		if p >= 1 && p <= 65535 {
			clean = append(clean, p)
		}
	}
	ports = clean

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, `{"type":"error","message":"streaming unsupported"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)

	enc := json.NewEncoder(w)
	var mu sync.Mutex
	writeLine := func(v any) {
		mu.Lock()
		defer mu.Unlock()
		_ = enc.Encode(v)
		flusher.Flush()
	}

	writeLine(map[string]any{"type": "meta", "total": total, "ports": len(portsOrDefault(ports))})

	netscan.Scan(ctx, netscan.Options{Start: start, End: end, Ports: ports}, func(res netscan.HostResult) {
		writeLine(map[string]any{
			"type":      "host",
			"ip":        res.IP,
			"hostname":  res.Hostname,
			"mac":       res.MAC,
			"vendor":    res.Vendor,
			"alive":     res.Alive,
			"rttMs":     res.RttMs,
			"openPorts": res.OpenPorts,
		})
	})

	if ctx.Err() != nil {
		writeLine(map[string]any{"type": "aborted"})
		return
	}
	writeLine(map[string]any{"type": "done"})
}

func portsOrDefault(p []int) []int {
	if len(p) == 0 {
		return netscan.DefaultPorts
	}
	return p
}
