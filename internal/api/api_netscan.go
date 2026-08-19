package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"homehub/internal/httpctx"
	"homehub/internal/netscan"
)

// IP scanner: sweep a LAN range from the HomeHub server and report per host
// alive state, hostname, MAC/vendor, and open ports — an Angry-IP-Scanner-
// style tool. The scan streams NDJSON so the UI fills rows in live; the
// interfaces list is a plain typed endpoint.

func (s *Server) NetscanInterfaces(ctx context.Context, _ NetscanInterfacesRequestObject) (NetscanInterfacesResponseObject, error) {
	if httpctx.IdentityFrom(ctx) == nil {
		return nil, ErrUnauthenticated
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
func (s *Server) HandleNetscan(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if httpctx.IdentityFrom(ctx) == nil {
		http.Error(w, `{"type":"error","message":"sign in first"}`, http.StatusUnauthorized)
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
