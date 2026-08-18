// Package truenassim is an in-memory TrueNAS REST v2.0 API simulator, used to
// verify the truenas driver end to end without real hardware. It serves a
// realistic set of pools, datasets, disks, shares, snapshots, and alerts, and
// authenticates the Bearer API key the driver sends.
//
// It intentionally mirrors the documented v2.0 response shapes; it is a
// stand-in for verification, not a spec. The real system remains the source
// of truth and the driver must be validated against it.
package truenassim

import (
	"github.com/coder/websocket"

	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// Sim is the simulator state.
type Sim struct {
	APIKey string
	mux    *http.Server
}

// Handler returns an http.Handler serving the simulated API. apiKey is the
// Bearer token the driver must present.
func Handler(apiKey string) http.Handler {
	mux := http.NewServeMux()
	s := &state{apiKey: apiKey}
	s.seed()

	auth := func(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if got != apiKey {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"message": "Not authenticated"})
				return
			}
			next(w, r)
		}
	}

	mux.HandleFunc("/api/v2.0/system/info", auth(s.systemInfo))
	mux.HandleFunc("/api/v2.0/pool", auth(s.pools))
	mux.HandleFunc("/api/v2.0/pool/dataset", auth(s.datasets))
	mux.HandleFunc("/api/v2.0/pool/snapshot", auth(s.snapshots))
	mux.HandleFunc("/api/v2.0/disk", auth(s.disks))
	mux.HandleFunc("/api/v2.0/sharing/smb", auth(s.smb))
	mux.HandleFunc("/api/v2.0/sharing/nfs", auth(s.nfs))
	mux.HandleFunc("/api/v2.0/iscsi/target", auth(s.iscsi))
	mux.HandleFunc("/api/v2.0/replication", auth(s.replication))
	mux.HandleFunc("/api/v2.0/alert/list", auth(s.alerts))
	mux.HandleFunc("/api/v2.0/reporting/get_data", auth(s.reporting))
	// The 25.x JSON-RPC WebSocket API — user-linked keys log in with the
	// owning username ("simuser") via auth.login_ex.
	mux.HandleFunc("/api/current", s.wsAPI)
	return mux
}

// WSUsername is the account the sim's API key belongs to.
const WSUsername = "simuser"

// wsAPI is the JSON-RPC 2.0 endpoint mirroring TrueNAS 25.x behavior:
// every method except auth.* answers ENOTAUTHENTICATED until login_ex
// succeeds with the right username + key.
func (s *state) wsAPI(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	ctx := r.Context()
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(8 << 20)
	authed := false

	reply := func(id json.RawMessage, result any) {
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "result": result})
		_ = conn.Write(ctx, websocket.MessageText, raw)
	}
	replyErr := func(id json.RawMessage, errname, reason string) {
		raw, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": json.RawMessage(id),
			"error": map[string]any{"code": -32001, "message": "Method call error",
				"data": map[string]any{"errname": errname, "reason": reason}},
		})
		_ = conn.Write(ctx, websocket.MessageText, raw)
	}

	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var req struct {
			ID     json.RawMessage   `json:"id"`
			Method string            `json:"method"`
			Params []json.RawMessage `json:"params"`
		}
		if json.Unmarshal(raw, &req) != nil {
			continue
		}
		switch req.Method {
		case "auth.login_ex":
			var arg struct {
				Mechanism string `json:"mechanism"`
				Username  string `json:"username"`
				APIKey    string `json:"api_key"`
			}
			if len(req.Params) > 0 {
				_ = json.Unmarshal(req.Params[0], &arg)
			}
			if arg.Mechanism == "API_KEY_PLAIN" && arg.Username == WSUsername && arg.APIKey == s.apiKey {
				authed = true
				reply(req.ID, map[string]any{"response_type": "SUCCESS"})
			} else {
				reply(req.ID, map[string]any{"response_type": "AUTH_ERR"})
			}
		case "auth.login_with_api_key":
			// User-linked keys refuse the bare login, like real 25.x.
			reply(req.ID, false)
		default:
			if !authed {
				replyErr(req.ID, "ENOTAUTHENTICATED", "[ENOTAUTHENTICATED] Not authenticated")
				continue
			}
			switch req.Method {
			case "system.info":
				reply(req.ID, s.info)
			case "pool.query":
				reply(req.ID, s.poolList)
			case "pool.dataset.query":
				reply(req.ID, s.dsList)
			case "disk.query":
				// v26-faithful: disk.query carries no temperature — that
				// moved to the separate disk.temperatures call.
				stripped := make([]map[string]any, 0, len(s.diskList))
				for _, d := range s.diskList {
					copyD := map[string]any{}
					for k, v := range d {
						if k != "temperature" {
							copyD[k] = v
						}
					}
					stripped = append(stripped, copyD)
				}
				reply(req.ID, stripped)
			case "disk.temperatures":
				temps := map[string]any{}
				for _, d := range s.diskList {
					if name, ok := d["name"].(string); ok {
						temps[name] = d["temperature"]
					}
				}
				reply(req.ID, temps)
			case "sharing.smb.query":
				reply(req.ID, s.smbList)
			case "sharing.nfs.query":
				reply(req.ID, s.nfsList)
			case "iscsi.target.query":
				reply(req.ID, s.iscsiList)
			case "pool.snapshot.query":
				// v26-faithful: no top-level used/created — ZFS properties
				// arrive under "properties" when requested via extra.
				v26 := make([]map[string]any, 0, len(s.snapList))
				for _, sn := range s.snapList {
					copySn := map[string]any{}
					for k, v := range sn {
						if k != "used" && k != "created" {
							copySn[k] = v
						}
					}
					var creationSec int64
					if created, ok := sn["created"].(map[string]any); ok {
						if ms, ok := created["$date"].(int64); ok {
							creationSec = ms / 1000
						}
					}
					copySn["createtxg"] = itoa(creationSec)
					copySn["properties"] = map[string]any{
						"used":     sn["used"],
						"creation": map[string]any{"parsed": nil, "rawvalue": itoa(creationSec)},
					}
					v26 = append(v26, copySn)
				}
				reply(req.ID, v26)
			case "alert.list":
				// v26-faithful: datetime is an ISO string, not {"$date": ms}.
				v26Alerts := make([]map[string]any, 0, len(s.alertList))
				for _, a := range s.alertList {
					copyA := map[string]any{}
					for k, v := range a {
						copyA[k] = v
					}
					if dt, ok := a["datetime"].(map[string]any); ok {
						if ms, ok := dt["$date"].(int64); ok {
							copyA["datetime"] = time.UnixMilli(ms).UTC().Format(time.RFC3339)
						}
					}
					v26Alerts = append(v26Alerts, copyA)
				}
				reply(req.ID, v26Alerts)
			case "replication.query":
				reply(req.ID, s.repList)
			case "interface.query":
				reply(req.ID, []map[string]any{
					{
						"name": "eno1", "type": "PHYSICAL",
						"state":   map[string]any{"link_state": "LINK_STATE_UP", "active_media_subtype": "1000baseT/Full"},
						"aliases": []map[string]any{{"type": "INET", "address": "192.168.1.8"}},
					},
				})
			case "reporting.netdata_graphs":
				reply(req.ID, []map[string]any{
					{"name": "cpu"}, {"name": "load"}, {"name": "memory"}, {"name": "arcsize"},
					{"name": "interface", "identifiers": []string{"eno1"}},
				})
			case "reporting.netdata_get_data":
				// Serve whichever graphs were requested, v26 shapes: cpu in
				// %, memory/arcsize in bytes, interface in Kilobits/s.
				const gib = float64(int64(1) << 30)
				var reqGraphs []struct {
					Name       string `json:"name"`
					Identifier string `json:"identifier"`
				}
				if len(req.Params) > 0 {
					_ = json.Unmarshal(req.Params[0], &reqGraphs)
				}
				if len(reqGraphs) == 0 {
					reqGraphs = []struct {
						Name       string `json:"name"`
						Identifier string `json:"identifier"`
					}{{Name: "memory"}, {Name: "arcsize"}}
				}
				results := []map[string]any{}
				for _, g := range reqGraphs {
					switch g.Name {
					case "cpu":
						results = append(results, map[string]any{
							"name": "cpu", "legend": []string{"time", "cpu"},
							"data": [][]float64{{1755399900, 12}, {1755400000, 17}},
						})
					case "load":
						results = append(results, map[string]any{
							"name": "load", "legend": []string{"time", "shortterm", "midterm", "longterm"},
							"data": [][]float64{{1755399900, 0.5, 0.4, 0.3}, {1755400000, 0.7, 0.5, 0.35}},
						})
					case "memory":
						results = append(results, map[string]any{
							"name": "memory", "legend": []string{"time", "available"},
							"data": [][]float64{{1755399900, 22 * gib}, {1755400000, 21 * gib}},
						})
					case "arcsize":
						results = append(results, map[string]any{
							"name": "arcsize", "legend": []string{"time", "size"},
							"data": [][]float64{{1755399900, 23 * gib}, {1755400000, 24 * gib}},
						})
					case "interface":
						results = append(results, map[string]any{
							"name": "interface", "identifier": g.Identifier,
							"legend": []string{"time", "received", "sent"},
							"data":   [][]float64{{1755399900, 120.5, 30.25}, {1755400000, 950.0, 12.5}},
						})
					}
				}
				reply(req.ID, results)
			case "alert.dismiss":
				var uuid string
				if len(req.Params) > 0 {
					_ = json.Unmarshal(req.Params[0], &uuid)
				}
				dismissed := false
				for _, a := range s.alertList {
					if a["uuid"] == uuid {
						a["dismissed"] = true
						dismissed = true
					}
				}
				reply(req.ID, dismissed)
			default:
				replyErr(req.ID, "ENOMETHOD", "Method does not exist")
			}
		}
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type state struct {
	apiKey    string
	info      map[string]any
	poolList  []map[string]any
	dsList    []map[string]any
	diskList  []map[string]any
	smbList   []map[string]any
	nfsList   []map[string]any
	iscsiList []map[string]any
	snapList  []map[string]any
	repList   []map[string]any
	alertList []map[string]any
}

func parsed(raw int64, human string) map[string]any {
	return map[string]any{"parsed": float64(raw), "rawvalue": itoa(raw), "value": human}
}

func itoa(n int64) string {
	// small helper avoiding strconv import churn
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (s *state) seed() {
	s.info = map[string]any{
		"version":        "TrueNAS-SCALE-24.10.2",
		"hostname":       "truenas.local",
		"uptime":         "6 days, 4:12:33",
		"uptime_seconds": 534753.0,
		"physmem":        68719476736,
		"system_product": "Mini X+",
		"cores":          8,
		"loadavg":        []float64{0.31, 0.28, 0.22},
	}
	const tb = int64(1) << 40
	s.poolList = []map[string]any{
		{
			"id": 1, "name": "tank", "status": "ONLINE", "healthy": true,
			"size": 16 * tb, "allocated": 9 * tb, "free": 7 * tb, "fragmentation": "4",
			"topology": map[string]any{"data": []map[string]any{{"type": "RAIDZ2", "status": "ONLINE"}}},
		},
		{
			"id": 2, "name": "flash", "status": "ONLINE", "healthy": true,
			"size": 2 * tb, "allocated": 512 * (int64(1) << 30), "free": int64(1536) * (int64(1) << 30), "fragmentation": "1",
			"topology": map[string]any{"data": []map[string]any{{"type": "MIRROR", "status": "ONLINE"}}},
		},
	}
	const gb = int64(1) << 30
	s.dsList = []map[string]any{
		{"id": "tank", "name": "tank", "type": "FILESYSTEM", "mountpoint": "/mnt/tank",
			"used": parsed(9*tb, "9 TiB"), "available": parsed(7*tb, "7 TiB"), "compression": map[string]any{"value": "LZ4", "parsed": "lz4"}},
		{"id": "tank/media", "name": "tank/media", "type": "FILESYSTEM", "mountpoint": "/mnt/tank/media",
			"used": parsed(6*tb, "6 TiB"), "available": parsed(7*tb, "7 TiB"), "compression": map[string]any{"value": "LZ4", "parsed": "lz4"}},
		{"id": "tank/backups", "name": "tank/backups", "type": "FILESYSTEM", "mountpoint": "/mnt/tank/backups",
			"used": parsed(3*tb, "3 TiB"), "available": parsed(7*tb, "7 TiB"), "compression": map[string]any{"value": "ZSTD", "parsed": "zstd"}},
		{"id": "flash/vms", "name": "flash/vms", "type": "VOLUME",
			"used": parsed(512*gb, "512 GiB"), "available": parsed(1536*gb, "1.5 TiB"), "compression": map[string]any{"value": "LZ4", "parsed": "lz4"}},
	}
	temp := func(t int) *int { return &t }
	_ = temp
	s.diskList = []map[string]any{
		{"name": "sda", "serial": "WD-A1B2C3", "model": "WDC WD40EFRX", "size": 4 * tb, "type": "HDD", "temperature": 34, "pool": "tank"},
		{"name": "sdb", "serial": "WD-D4E5F6", "model": "WDC WD40EFRX", "size": 4 * tb, "type": "HDD", "temperature": 35, "pool": "tank"},
		{"name": "sdc", "serial": "WD-G7H8I9", "model": "WDC WD40EFRX", "size": 4 * tb, "type": "HDD", "temperature": 33, "pool": "tank"},
		{"name": "sdd", "serial": "WD-J1K2L3", "model": "WDC WD40EFRX", "size": 4 * tb, "type": "HDD", "temperature": 36, "pool": "tank"},
		{"name": "nvme0n1", "serial": "S4EVNF0M", "model": "Samsung 980 PRO", "size": 1 * tb, "type": "SSD", "temperature": 41, "pool": "flash"},
		{"name": "nvme1n1", "serial": "S4EVNF0N", "model": "Samsung 980 PRO", "size": 1 * tb, "type": "SSD", "temperature": 42, "pool": "flash"},
	}
	s.smbList = []map[string]any{
		{"id": 1, "name": "media", "path": "/mnt/tank/media", "enabled": true, "comment": "Movies and music"},
		{"id": 2, "name": "backups", "path": "/mnt/tank/backups", "enabled": true, "comment": "Time Machine + PC backups"},
	}
	s.nfsList = []map[string]any{
		{"id": 1, "paths": []string{"/mnt/tank/media"}, "enabled": true, "comment": "Kodi library"},
	}
	s.iscsiList = []map[string]any{
		{"id": 1, "name": "iqn.2005-10.org.freenas.ctl:vmstore", "alias": "vmstore"},
	}
	s.snapList = []map[string]any{
		{"name": "tank/backups@auto-2026-08-17", "dataset": "tank/backups", "snapshot_name": "auto-2026-08-17",
			"used": parsed(12*gb, "12 GiB"), "created": map[string]any{"$date": int64(1755400000000)}},
		{"name": "tank/media@auto-2026-08-16", "dataset": "tank/media", "snapshot_name": "auto-2026-08-16",
			"used": parsed(3*gb, "3 GiB"), "created": map[string]any{"$date": int64(1755313600000)}},
	}
	s.repList = []map[string]any{
		{"id": 1, "name": "backups-to-offsite", "enabled": true, "state": map[string]any{"state": "FINISHED"}},
	}
	s.alertList = []map[string]any{
		{"uuid": "alert-scrub-1", "level": "INFO", "formatted": "Scrub of pool 'tank' finished.", "dismissed": false, "datetime": map[string]any{"$date": int64(1755390000000)}},
		{"uuid": "alert-degraded-1", "level": "WARNING", "formatted": "Pool 'tank' has one <b>offline</b> disk.", "dismissed": false, "datetime": map[string]any{"$date": int64(1755391000000)}},
	}
}

// reporting serves the memory + arcsize graphs the RAM dial consumes:
// 64 GiB total, 21 GiB free, 24 GiB ZFS ARC (services = the remainder).
func (s *state) reporting(w http.ResponseWriter, r *http.Request) {
	// Mirrors TrueNAS 25.x: the legacy reporting_query field is rejected
	// outright, and query.page must be >= 1 when present.
	var body struct {
		ReportingQuery map[string]any `json:"reporting_query"`
		Query          map[string]any `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
		if body.ReportingQuery != nil {
			writeJSON(w, 400, map[string]any{"message": "The following attributes are not expected: reporting_query"})
			return
		}
		if page, ok := body.Query["page"].(float64); ok && page < 1 {
			writeJSON(w, 422, map[string]any{"query.page": []map[string]any{{"message": "Input should be greater than or equal to 1", "errno": 22}}})
			return
		}
	}
	const gib = float64(int64(1) << 30)
	writeJSON(w, 200, []map[string]any{
		{
			"name":   "memory",
			"legend": []string{"time", "available"},
			"data":   [][]float64{{1755400000, 21 * gib}},
		},
		{
			"name":   "arcsize",
			"legend": []string{"time", "arc_size"},
			"data":   [][]float64{{1755400000, 24 * gib}},
		},
	})
}

func (s *state) systemInfo(w http.ResponseWriter, _ *http.Request)  { writeJSON(w, 200, s.info) }
func (s *state) pools(w http.ResponseWriter, _ *http.Request)       { writeJSON(w, 200, s.poolList) }
func (s *state) datasets(w http.ResponseWriter, _ *http.Request)    { writeJSON(w, 200, s.dsList) }
func (s *state) disks(w http.ResponseWriter, _ *http.Request)       { writeJSON(w, 200, s.diskList) }
func (s *state) smb(w http.ResponseWriter, _ *http.Request)         { writeJSON(w, 200, s.smbList) }
func (s *state) nfs(w http.ResponseWriter, _ *http.Request)         { writeJSON(w, 200, s.nfsList) }
func (s *state) iscsi(w http.ResponseWriter, _ *http.Request)       { writeJSON(w, 200, s.iscsiList) }
func (s *state) snapshots(w http.ResponseWriter, _ *http.Request)   { writeJSON(w, 200, s.snapList) }
func (s *state) replication(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.repList) }
func (s *state) alerts(w http.ResponseWriter, _ *http.Request)      { writeJSON(w, 200, s.alertList) }
