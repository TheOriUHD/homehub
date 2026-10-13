// Package aperturesim is an in-memory Aperture NVR used to verify the
// aperture driver and UI without the real (production) server. It mirrors
// the documented surface (Aperture's REST and WebSocket API): Bearer-token
// REST under /api/v1 with {"code","error"} failures, the public health
// endpoint, /cameras responses that INCLUDE plaintext RTSP credentials
// (so tests can prove the driver's whitelist strips them), stream-token
// minting that 404s on handles, and the live WebSocket speaking the JSON
// signalling protocol — answering every offer with a stated error, since
// the sim has no media pipeline; the UI must surface that reason instead
// of a silent black tile.
package aperturesim

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

const (
	APIKey = "apt_homehub_sim_token"

	Cam3DPrinter = "b2a26f0042e85ef0685168eab03f4283"
	CamA1Mini    = "4dc6bf3f3699967fcfd2abc16cfee84c"
	CamZimmer    = "e9f1fc8e89ca92e22a653c3bb540ea67"
	CamSchlaf    = "03274ba04e9b42c6755e4376ddd17caa"
)

type camera struct {
	id        string
	name      string
	handle    string
	sortOrder int
	healthy   bool
	coverage  int
}

// Sim is one fake Aperture server.
type Sim struct {
	mu           sync.Mutex
	cameras      []camera
	streamTokens map[string]string // token -> camera id
}

func New() *Sim {
	return &Sim{
		cameras: []camera{
			{id: Cam3DPrinter, name: "3D-Drucker", handle: "3d_drucker", sortOrder: 1, healthy: true, coverage: 97},
			{id: CamA1Mini, name: "A1 Mini", handle: "a1_mini", sortOrder: 2, healthy: true, coverage: 95},
			{id: CamZimmer, name: "Zimmer PH", handle: "zimmer_ph", sortOrder: 3, healthy: true, coverage: 93},
			{id: CamSchlaf, name: "Schlafzimmer", handle: "schlafzimmer", sortOrder: 4, healthy: false, coverage: 61},
		},
		streamTokens: map[string]string{},
	}
}

// SetHealthy flips one camera's recording state (for degraded-health tests).
func (s *Sim) SetHealthy(cameraID string, healthy bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cameras {
		if s.cameras[i].id == cameraID {
			s.cameras[i].healthy = healthy
		}
	}
}

func (s *Sim) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	if path == r.URL.Path {
		http.NotFound(w, r)
		return
	}

	// Live WebSocket: token in the query, not a Bearer header.
	if strings.HasPrefix(path, "/cameras/") && strings.HasSuffix(path, "/ws") {
		s.serveWS(w, r, strings.TrimSuffix(strings.TrimPrefix(path, "/cameras/"), "/ws"))
		return
	}

	switch {
	case path == "/system/health":
		s.reply(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	case path == "/system/version":
		s.reply(w, http.StatusOK, map[string]any{"version": "sim-1.0.0"})
		return
	}

	// Everything else needs the Bearer token.
	if strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ") != APIKey {
		s.fail(w, http.StatusUnauthorized, "unauthorized", "missing or invalid token")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()

	switch {
	case path == "/system/status":
		out := []map[string]any{}
		for _, cam := range s.cameras {
			out = append(out, map[string]any{
				"cameraId": cam.id, "name": cam.name, "enabled": true,
				"recordMode": "continuous", "recordingExpected": true,
				"recordingOk": cam.healthy, "liveOk": cam.healthy,
				"coveragePercent":  cam.coverage,
				"lastSegmentTs":    now - 9000,
				"restartsToday":    map[bool]int{true: 0, false: 17}[cam.healthy],
				"watchdogStale":    !cam.healthy,
				"watchdogLastSeen": now - 20000,
			})
		}
		s.reply(w, http.StatusOK, map[string]any{"cameras": out})

	case path == "/system/storage":
		cams := []map[string]any{}
		for i, cam := range s.cameras {
			cams = append(cams, map[string]any{
				"cameraId": cam.id, "name": cam.name,
				"bytes":    int64(9_000_000_000) + int64(i)*1_500_000_000,
				"count":    5000 + i*500,
				"oldestTs": now - int64(64+i)*3_600_000,
				"newestTs": now - 9000,
			})
		}
		s.reply(w, http.StatusOK, map[string]any{
			"capBytes": int64(53687091200), "allocatedBytes": int64(53687091200),
			"appBytes": int64(1403524452), "dataDir": "/data", "defaultPath": "/recordings",
			"cameras": cams,
		})

	case strings.HasPrefix(path, "/system/bitrate"):
		buckets := []map[string]any{}
		base := now - 30*60_000
		for i := 0; i < 30; i++ {
			buckets = append(buckets, map[string]any{
				"ts":  base + int64(i)*60_000,
				"bps": 5_500_000 + rand.Int63n(800_000),
			})
		}
		s.reply(w, http.StatusOK, map[string]any{"buckets": buckets})

	case path == "/cameras":
		out := []map[string]any{}
		for _, cam := range s.cameras {
			out = append(out, map[string]any{
				"id": cam.id, "name": cam.name, "enabled": true,
				"recordMode": "continuous", "sortOrder": cam.sortOrder,
				"retentionDays": 3, "motionDetection": true,
				// The real endpoint leaks these — the driver's whitelist
				// must make sure they never travel further.
				"rtspMainUrl": "rtsp://admin:SUPERSECRET@10.0.30." + cam.id[:2] + ":554/stream1",
				"rtspSubUrl":  "rtsp://admin:SUPERSECRET@10.0.30." + cam.id[:2] + ":554/stream2",
			})
		}
		s.reply(w, http.StatusOK, out)

	case strings.HasPrefix(path, "/cameras/") && strings.HasSuffix(path, "/stream-token"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/cameras/"), "/stream-token")
		cam := s.byID(id)
		if cam == nil {
			// Handles 404 here — only raw IDs work on this route.
			s.fail(w, http.StatusNotFound, "not_found", "camera not found (stream-token requires the raw camera ID)")
			return
		}
		token := "st_" + uuid.NewString()
		s.streamTokens[token] = cam.id
		s.reply(w, http.StatusOK, map[string]any{
			"token":  token,
			"wsUrl":  "/api/v1/cameras/" + cam.id + "/ws?token=" + token,
			"hasSub": true,
		})

	default:
		s.fail(w, http.StatusNotFound, "not_found", "unknown route")
	}
}

func (s *Sim) byID(id string) *camera {
	for i := range s.cameras {
		if s.cameras[i].id == id {
			return &s.cameras[i]
		}
	}
	return nil
}

func (s *Sim) byIDOrHandle(key string) *camera {
	for i := range s.cameras {
		if s.cameras[i].id == key || s.cameras[i].handle == key {
			return &s.cameras[i]
		}
	}
	return nil
}

// serveWS accepts the live-view socket and speaks the JSON protocol. The
// sim has no media pipeline, so every negotiation ends in a stated error —
// which is exactly what the UI's failure path needs for verification.
func (s *Sim) serveWS(w http.ResponseWriter, r *http.Request, key string) {
	s.mu.Lock()
	cam := s.byIDOrHandle(key)
	tokenCam, tokenOK := s.streamTokens[r.URL.Query().Get("token")]
	s.mu.Unlock()
	if cam == nil {
		http.Error(w, "camera not found", http.StatusNotFound)
		return
	}
	if !tokenOK || tokenCam != cam.id {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
	if err != nil {
		return
	}
	ctx := context.Background()
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var frame struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(raw, &frame) != nil {
			continue
		}
		switch frame.Type {
		case "webrtc/offer", "mse":
			payload, _ := json.Marshal(map[string]string{
				"type":  "error",
				"value": "sim: no media pipeline behind this camera",
			})
			if conn.Write(ctx, websocket.MessageText, payload) != nil {
				return
			}
		}
	}
}

func (s *Sim) reply(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		fmt.Fprintln(w, "{}")
	}
}

func (s *Sim) fail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"code":%q,"error":%q}`, code, msg)
}
