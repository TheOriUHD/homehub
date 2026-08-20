// Package term brokers interactive terminal and console sessions.
//
// Flow: an authenticated, permission-checked REST call creates a Session —
// a one-time ticket binding a user to an opener function. The client then
// upgrades GET /api/term/{sessionId} to a WebSocket; the opener bridges that
// socket to the target (SSH, Proxmox VNC, Proxmox serial).
//
// Client protocol on the bridged socket:
//   - binary frames both ways carry terminal/console bytes verbatim
//   - client text frames: {"type":"resize","cols":N,"rows":N}
//   - server text frames: {"type":"exit","message":…} | {"type":"error","message":…}
package term

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// OpenFunc bridges an accepted WebSocket to the session target; it returns
// when either side closes.
type OpenFunc func(ctx context.Context, conn *websocket.Conn) error

const sessionTTL = 30 * time.Second

type Session struct {
	ID      string
	UserID  uuid.UUID
	Open    OpenFunc
	created time.Time
}

// Manager holds pending one-time sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
}

func NewManager() *Manager {
	m := &Manager{sessions: map[string]*Session{}}
	go func() {
		for range time.Tick(15 * time.Second) {
			m.mu.Lock()
			now := time.Now()
			for id, s := range m.sessions {
				if now.Sub(s.created) > sessionTTL {
					delete(m.sessions, id)
				}
			}
			m.mu.Unlock()
		}
	}()
	return m
}

// Create registers a pending session and returns its id (the WS path suffix).
func (m *Manager) Create(userID uuid.UUID, open OpenFunc) string {
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	id := base64.RawURLEncoding.EncodeToString(raw)
	m.mu.Lock()
	m.sessions[id] = &Session{ID: id, UserID: userID, Open: open, created: time.Now()}
	m.mu.Unlock()
	return id
}

// Take consumes a pending session for the given user; single use.
func (m *Manager) Take(id string, userID uuid.UUID) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || s.UserID != userID || time.Since(s.created) > sessionTTL {
		delete(m.sessions, id)
		return nil, false
	}
	delete(m.sessions, id)
	return s, true
}

// KeepAlive pings the given websockets every interval until ctx ends.
// WS ping/pong are control frames — invisible to the bridged byte streams
// (safe even inside a raw RFB pipe) — and they count as traffic for every
// idle-timeout along the path: the browser, any reverse proxy in front of
// HomeHub, and the upstream host. Pongs are processed by each side's active
// read loop, which every bridge here has.
func KeepAlive(ctx context.Context, interval time.Duration, conns ...*websocket.Conn) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				for _, c := range conns {
					pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
					_ = c.Ping(pctx) // a dead conn fails its read loop on its own
					cancel()
				}
			}
		}
	}()
}

// ControlFrame is the JSON text-frame envelope on bridged sockets.
type ControlFrame struct {
	Type    string `json:"type"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
	Message string `json:"message,omitempty"`
}

// SendControl writes a server→client control frame; best effort.
func SendControl(ctx context.Context, conn *websocket.Conn, frame ControlFrame) {
	raw, _ := json.Marshal(frame)
	wctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, raw)
}
