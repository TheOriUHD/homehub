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
	"encoding/json"
	"net/http"
	"strings"
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
	return mux
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
		{"level": "INFO", "formatted": "Scrub of pool 'tank' finished.", "dismissed": false, "datetime": map[string]any{"$date": int64(1755390000000)}},
	}
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
