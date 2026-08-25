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
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// tokenPrefix versions the agent token format: mcs1.<server-uuid>.<secret>.
// (Pre-network tokens carried the integration uuid; the wrap migration
// gave those servers id == integration id, so they parse identically.)
const tokenPrefix = "mcs1"

// Registry tracks live companion sessions and terminates the agent
// WebSocket endpoint. Drivers look their session up here; the API layer
// reads consoles and sends commands through it.
type Registry struct {
	store  *store.Store
	vault  *vault.Vault
	logger *slog.Logger
	// Publish emits a per-server event on the network's WS topic, wired to the
	// hub in app wiring with the same {event, data} envelope the manager uses.
	// resource is the server's fine-grained RBAC path so delivery is scoped to
	// clients who may see that server (not every sibling on the network).
	Publish func(instanceID uuid.UUID, resource string, event string, data any)
	// StatusChanged fires when a companion binds or drops, wired to the
	// integration manager's Kick so the instance status follows within a
	// second instead of the next poll cycle.
	StatusChanged func(instanceID uuid.UUID)

	mu       sync.RWMutex
	sessions map[uuid.UUID]*Session // keyed by SERVER id
}

// serverResource is a Minecraft server's fine-grained RBAC path, used to scope
// WS delivery to clients who may see that specific server.
func serverResource(networkID, serverID uuid.UUID) string {
	return TypeID + "/" + networkID.String() + "/server/" + serverID.String()
}

func NewRegistry(st *store.Store, v *vault.Vault, logger *slog.Logger) *Registry {
	return &Registry{
		store:         st,
		vault:         v,
		logger:        logger.With("component", "minecraft"),
		sessions:      map[uuid.UUID]*Session{},
		Publish:       func(uuid.UUID, string, string, any) {},
		StatusChanged: func(uuid.UUID) {},
	}
}

// Session returns the live companion session for one SERVER, if connected.
func (r *Registry) Session(serverID uuid.UUID) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[serverID]
	return s, ok
}

// NetworkSessions returns the connected sessions of one network.
func (r *Registry) NetworkSessions(networkID uuid.UUID) []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []*Session{}
	for _, s := range r.sessions {
		if s.NetworkID == networkID {
			out = append(out, s)
		}
	}
	return out
}

// Session is one connected companion plugin.
type Session struct {
	ServerID   uuid.UUID
	NetworkID  uuid.UUID
	Label      string
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

	detailMu      sync.Mutex
	detailSeq     int64
	detailWaiters map[int64]chan Frame

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

// PlayerDetail asks the companion for one player's full live detail and
// waits for the answer. The returned document is opaque (built by the
// plugin); an empty result means the player is offline or unsupported.
func (s *Session) PlayerDetail(ctx context.Context, uuid string) (json.RawMessage, error) {
	s.detailMu.Lock()
	s.detailSeq++
	id := s.detailSeq
	ch := make(chan Frame, 1)
	s.detailWaiters[id] = ch
	s.detailMu.Unlock()
	defer func() {
		s.detailMu.Lock()
		delete(s.detailWaiters, id)
		s.detailMu.Unlock()
	}()

	if err := s.write(ctx, Frame{Type: FramePlayerDetail, ID: id, UUID: uuid}); err != nil {
		return nil, fmt.Errorf("companion connection lost while requesting player detail")
	}
	select {
	case res := <-ch:
		if !res.OK && res.Message != "" {
			return nil, errors.New(res.Message)
		}
		return res.Detail, nil
	case <-ctx.Done():
		return nil, errors.New("the companion did not return player detail in time")
	case <-s.closed:
		return nil, errors.New("companion disconnected before returning player detail")
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

	var server *store.MinecraftServer
	paired := false
	switch first.Type {
	case FramePair:
		server, err = r.pair(ctx, conn, first)
		paired = true
	case FrameHello:
		server, err = r.hello(ctx, first)
	default:
		err = errors.New("first frame must be pair or hello")
	}
	if err != nil {
		writeFrame(ctx, conn, Frame{Type: FrameErr, Message: err.Error()})
		_ = conn.Close(websocket.StatusPolicyViolation, "auth failed")
		return
	}

	first.McVersion = gameVersion(first.McVersion)
	if err := r.store.TouchMinecraftServer(ctx, server.ID,
		first.ServerName, first.Loader, first.McVersion, first.Companion, clientIP); err != nil {
		r.logger.Error("recording companion metadata", "error", err)
	}

	sess := &Session{
		ServerID:      server.ID,
		NetworkID:     server.IntegrationID,
		Label:         server.Label,
		ServerName:    first.ServerName,
		Loader:        first.Loader,
		McVersion:     first.McVersion,
		Companion:     first.Companion,
		RemoteIP:      clientIP,
		Connected:     time.Now(),
		Console:       NewConsoleBuffer(),
		conn:          conn,
		cmdWaiters:    map[int64]chan Frame{},
		detailWaiters: map[int64]chan Frame{},
		closed:        make(chan struct{}),
	}

	// One session per server: a reconnecting companion replaces its
	// predecessor (its console backfill restores continuity).
	r.mu.Lock()
	if old := r.sessions[server.ID]; old != nil {
		old.close()
	}
	r.sessions[server.ID] = sess
	r.mu.Unlock()

	if !paired {
		writeFrame(ctx, conn, Frame{Type: FrameWelcome})
	}
	r.logger.Info("companion connected",
		"network", server.IntegrationID, "serverId", server.ID, "server", first.ServerName, "loader", first.Loader, "mc", first.McVersion)
	r.Publish(server.IntegrationID, serverResource(server.IntegrationID, server.ID), "companion-changed", map[string]any{"serverId": server.ID.String()})
	r.StatusChanged(server.IntegrationID)

	r.pump(ctx, sess)

	r.mu.Lock()
	if r.sessions[server.ID] == sess {
		delete(r.sessions, server.ID)
	}
	r.mu.Unlock()
	sess.close()
	r.logger.Info("companion disconnected", "network", server.IntegrationID, "serverId", server.ID, "server", first.ServerName)
	r.Publish(server.IntegrationID, serverResource(server.IntegrationID, server.ID), "companion-changed", map[string]any{"serverId": server.ID.String()})
	r.StatusChanged(server.IntegrationID)
}

// buildSuffix is what Paper's Bukkit version string carries after the game
// version since 26.x ("26.2.build.2622"). Companions from before the fix
// send it verbatim; the game version is what the UI shows.
var buildSuffix = regexp.MustCompile(`\.build\.\d+$`)

func gameVersion(reported string) string {
	return buildSuffix.ReplaceAllString(strings.TrimSpace(reported), "")
}

// pair redeems a 6-digit code, creates the server row, and issues its
// permanent token. Tokens are sealed per server in the NETWORK's vault
// scope so deleting the network sweeps them all.
func (r *Registry) pair(ctx context.Context, conn *websocket.Conn, f *Frame) (*store.MinecraftServer, error) {
	code := strings.TrimSpace(f.Code)
	if len(code) != 6 {
		return nil, errors.New("pairing code must be 6 digits")
	}
	networkID, label, err := r.store.ConsumeMinecraftPairing(ctx, code)
	if err != nil {
		return nil, errors.New("pairing code is wrong or expired — generate a fresh one in HomeHub")
	}
	if label == "" {
		label = f.ServerName
	}
	if label == "" {
		label = "Server"
	}
	server, err := r.store.CreateMinecraftServer(ctx, networkID, label)
	if err != nil {
		return nil, fmt.Errorf("creating the server record: %w", err)
	}
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, err
	}
	secret := base64.RawURLEncoding.EncodeToString(secretBytes)
	if err := r.vault.Seal(ctx, "integration:"+networkID.String(), "agent-token-"+server.ID.String(), []byte(secret)); err != nil {
		return nil, fmt.Errorf("storing the agent token: %w", err)
	}
	token := tokenPrefix + "." + server.ID.String() + "." + secret
	writeFrame(ctx, conn, Frame{Type: FramePaired, Token: token})
	return server, nil
}

// hello validates a previously issued token. The uuid inside is the
// SERVER id; wrapped pre-network servers (id == network id) fall back to
// the legacy vault slot their token was sealed under.
func (r *Registry) hello(ctx context.Context, f *Frame) (*store.MinecraftServer, error) {
	parts := strings.Split(f.Token, ".")
	if len(parts) != 3 || parts[0] != tokenPrefix {
		return nil, errors.New("malformed agent token — re-pair the companion")
	}
	serverID, err := uuid.Parse(parts[1])
	if err != nil {
		return nil, errors.New("malformed agent token — re-pair the companion")
	}
	server, err := r.store.GetMinecraftServer(ctx, serverID)
	if err != nil {
		return nil, errors.New("this server was removed from its network — generate a new code in HomeHub")
	}
	scope := "integration:" + server.IntegrationID.String()
	stored, err := r.vault.Open(ctx, scope, "agent-token-"+server.ID.String())
	if err != nil && server.ID == server.IntegrationID {
		stored, err = r.vault.Open(ctx, scope, "agent-token")
	}
	if err != nil {
		return nil, errors.New("this server is no longer paired — generate a new code in HomeHub")
	}
	if subtle.ConstantTimeCompare(stored, []byte(parts[2])) != 1 {
		return nil, errors.New("agent token rejected — re-pair the companion")
	}
	return server, nil
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
				r.Publish(s.NetworkID, serverResource(s.NetworkID, s.ServerID), "console", map[string]any{"serverId": s.ServerID.String(), "lines": stored})
			}
		case FrameStatus:
			if f.Status != nil {
				s.mu.Lock()
				s.status = f.Status
				s.mu.Unlock()
				r.Publish(s.NetworkID, serverResource(s.NetworkID, s.ServerID), "status", map[string]any{"serverId": s.ServerID.String(), "status": f.Status})
			}
		case FramePlayers:
			s.mu.Lock()
			s.players = f.Players
			s.mu.Unlock()
			r.Publish(s.NetworkID, serverResource(s.NetworkID, s.ServerID), "players-changed", map[string]any{"serverId": s.ServerID.String(), "players": f.Players})
		case FrameCmdResult:
			s.cmdMu.Lock()
			ch := s.cmdWaiters[f.ID]
			s.cmdMu.Unlock()
			if ch != nil {
				ch <- *f
			}
		case FramePlayerDetailResult:
			s.detailMu.Lock()
			ch := s.detailWaiters[f.ID]
			s.detailMu.Unlock()
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
