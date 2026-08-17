package minecraft

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// tokenPrefix versions the agent token format: mcs1.<integration-uuid>.<secret>.
const tokenPrefix = "mcs1"

// Registry tracks live companion sessions and terminates the agent
// WebSocket endpoint. Drivers look their session up here; the API layer
// reads consoles and sends commands through it.
type Registry struct {
	store  *store.Store
	vault  *vault.Vault
	logger *slog.Logger
	// Publish emits an event on the instance's WS topic, wired to the hub in
	// app wiring with the same {event, data} envelope the manager uses.
	Publish func(instanceID uuid.UUID, event string, data any)

	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session
}

func NewRegistry(st *store.Store, v *vault.Vault, logger *slog.Logger) *Registry {
	return &Registry{
		store:    st,
		vault:    v,
		logger:   logger.With("component", "minecraft"),
		sessions: map[uuid.UUID]*Session{},
		Publish:  func(uuid.UUID, string, any) {},
	}
}

// Session returns the live companion session for an instance, if connected.
func (r *Registry) Session(id uuid.UUID) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Session is one connected companion plugin.
type Session struct {
	InstanceID uuid.UUID
	ServerName string
	Loader     string
	McVersion  string
	Companion  string
	RemoteIP   string
	Connected  time.Time

	Console *ConsoleBuffer

	conn    *websocket.Conn
	writeMu sync.Mutex

	mu      sync.RWMutex
	status  *StatusSnapshot
	players []Player

	cmdMu      sync.Mutex
	cmdSeq     int64
	cmdWaiters map[int64]chan Frame

	closed chan struct{}
	once   sync.Once
}

// Status returns the latest performance snapshot, nil before the first one.
func (s *Session) Status() *StatusSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.status
}

// Players returns the current online player list.
func (s *Session) Players() []Player {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]Player(nil), s.players...)
}

// Command executes a console command on the server and waits for the
// companion's acknowledgment.
func (s *Session) Command(ctx context.Context, command string) (bool, string, error) {
	s.cmdMu.Lock()
	s.cmdSeq++
	id := s.cmdSeq
	ch := make(chan Frame, 1)
	s.cmdWaiters[id] = ch
	s.cmdMu.Unlock()
	defer func() {
		s.cmdMu.Lock()
		delete(s.cmdWaiters, id)
		s.cmdMu.Unlock()
	}()

	if err := s.write(ctx, Frame{Type: FrameCmd, ID: id, Command: command}); err != nil {
		return false, "", fmt.Errorf("companion connection lost while sending the command")
	}
	select {
	case res := <-ch:
		return res.OK, res.Message, nil
	case <-ctx.Done():
		return false, "", errors.New("the server did not acknowledge the command in time")
	case <-s.closed:
		return false, "", errors.New("companion disconnected before acknowledging the command")
	}
}

func (s *Session) write(ctx context.Context, f Frame) error {
	raw, err := json.Marshal(f)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.conn.Write(wctx, websocket.MessageText, raw)
}

func (s *Session) close() {
	s.once.Do(func() { close(s.closed) })
	_ = s.conn.Close(websocket.StatusNormalClosure, "")
}

// ---- agent endpoint --------------------------------------------------------

// HandleAgent terminates ws(s)://…/api/mc/agent. The first frame must be
// pair (6-digit code) or hello (token); everything after is the companion
// stream. Browser sessions never hit this path — token auth only.
func (r *Registry) HandleAgent(w http.ResponseWriter, req *http.Request, clientIP string) {
	conn, err := websocket.Accept(w, req, &websocket.AcceptOptions{
		// Companions are not browsers; origin checks do not apply.
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	conn.SetReadLimit(4 << 20)
	ctx := context.Background()

	first, err := readFrame(ctx, conn, 10*time.Second)
	if err != nil {
		_ = conn.Close(websocket.StatusPolicyViolation, "expected pair or hello")
		return
	}

	var instanceID uuid.UUID
	paired := false
	switch first.Type {
	case FramePair:
		instanceID, err = r.pair(ctx, conn, first)
		paired = true
	case FrameHello:
		instanceID, err = r.hello(ctx, first)
	default:
		err = errors.New("first frame must be pair or hello")
	}
	if err != nil {
		writeFrame(ctx, conn, Frame{Type: FrameErr, Message: err.Error()})
		_ = conn.Close(websocket.StatusPolicyViolation, "auth failed")
		return
	}

	if err := r.store.UpsertMinecraftServer(ctx, instanceID,
		first.ServerName, first.Loader, first.McVersion, first.Companion, clientIP, paired); err != nil {
		r.logger.Error("recording companion metadata", "error", err)
	}

	sess := &Session{
		InstanceID: instanceID,
		ServerName: first.ServerName,
		Loader:     first.Loader,
		McVersion:  first.McVersion,
		Companion:  first.Companion,
		RemoteIP:   clientIP,
		Connected:  time.Now(),
		Console:    NewConsoleBuffer(),
		conn:       conn,
		cmdWaiters: map[int64]chan Frame{},
		closed:     make(chan struct{}),
	}

	// One session per instance: a reconnecting companion replaces its
	// predecessor (its console backfill restores continuity).
	r.mu.Lock()
	if old := r.sessions[instanceID]; old != nil {
		old.close()
	}
	r.sessions[instanceID] = sess
	r.mu.Unlock()

	if !paired {
		writeFrame(ctx, conn, Frame{Type: FrameWelcome})
	}
	r.logger.Info("companion connected",
		"instance", instanceID, "server", first.ServerName, "loader", first.Loader, "mc", first.McVersion)
	r.Publish(instanceID, "companion-changed", nil)

	r.pump(ctx, sess)

	r.mu.Lock()
	if r.sessions[instanceID] == sess {
		delete(r.sessions, instanceID)
	}
	r.mu.Unlock()
	sess.close()
	r.logger.Info("companion disconnected", "instance", instanceID, "server", first.ServerName)
	r.Publish(instanceID, "companion-changed", nil)
}

// pair redeems a 6-digit code and issues the permanent token.
func (r *Registry) pair(ctx context.Context, conn *websocket.Conn, f *Frame) (uuid.UUID, error) {
	code := strings.TrimSpace(f.Code)
	if len(code) != 6 {
		return uuid.Nil, errors.New("pairing code must be 6 digits")
	}
	id, err := r.store.ConsumeMinecraftPairing(ctx, code)
	if err != nil {
		return uuid.Nil, errors.New("pairing code is wrong or expired — generate a fresh one in HomeHub")
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return uuid.Nil, err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	if err := r.vault.Seal(ctx, "integration:"+id.String(), "agent-token", []byte(secret)); err != nil {
		return uuid.Nil, fmt.Errorf("storing the agent token: %w", err)
	}
	token := tokenPrefix + "." + id.String() + "." + secret
	writeFrame(ctx, conn, Frame{Type: FramePaired, Token: token})
	return id, nil
}

// hello validates a previously issued token.
func (r *Registry) hello(ctx context.Context, f *Frame) (uuid.UUID, error) {
	parts := strings.Split(f.Token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return uuid.Nil, errors.New("malformed agent token — re-pair the companion")
	}
	id, err := uuid.Parse(parts[1])
	if err != nil {
		return uuid.Nil, errors.New("malformed agent token — re-pair the companion")
	}
	stored, err := r.vault.Open(ctx, "integration:"+id.String(), "agent-token")
	if err != nil {
		return uuid.Nil, errors.New("this server is no longer paired — generate a new code in HomeHub")
	}
	if subtle.ConstantTimeCompare(stored, []byte(parts[2])) != 1 {
		return uuid.Nil, errors.New("agent token rejected — re-pair the companion")
	}
	return id, nil
}

// pump processes companion frames until the connection dies.
func (r *Registry) pump(ctx context.Context, s *Session) {
	pingTicker := time.NewTicker(30 * time.Second)
	defer pingTicker.Stop()
	go func() {
		for {
			select {
			case <-pingTicker.C:
				_ = s.write(ctx, Frame{Type: FramePing})
			case <-s.closed:
				return
			}
		}
	}()

	for {
		f, err := readFrame(ctx, s.conn, 90*time.Second)
		if err != nil {
			return
		}
		switch f.Type {
		case FrameConsole, FrameBackfill:
			stored := s.Console.Append(f.Lines)
			// Backfill is history — clients pull it via the console API; only
			// live lines push through the hub to keep events light.
			if f.Type == FrameConsole && len(stored) > 0 {
				r.Publish(s.InstanceID, "console", map[string]any{"lines": stored})
			}
		case FrameStatus:
			if f.Status != nil {
				s.mu.Lock()
				s.status = f.Status
				s.mu.Unlock()
				r.Publish(s.InstanceID, "status", f.Status)
			}
		case FramePlayers:
			s.mu.Lock()
			s.players = f.Players
			s.mu.Unlock()
			r.Publish(s.InstanceID, "players-changed", map[string]any{"players": f.Players})
		case FrameCmdResult:
			s.cmdMu.Lock()
			ch := s.cmdWaiters[f.ID]
			s.cmdMu.Unlock()
			if ch != nil {
				ch <- *f
			}
		case FramePong:
			// liveness satisfied by the read itself
		}
	}
}

func readFrame(ctx context.Context, conn *websocket.Conn, timeout time.Duration) (*Frame, error) {
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	_, raw, err := conn.Read(rctx)
	if err != nil {
		return nil, err
	}
	return Decode(raw)
}

func writeFrame(ctx context.Context, conn *websocket.Conn, f Frame) {
	raw, _ := json.Marshal(f)
	wctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = conn.Write(wctx, websocket.MessageText, raw)
}
