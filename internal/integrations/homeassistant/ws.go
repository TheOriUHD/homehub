package homeassistant

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// socket is one authenticated Home Assistant WebSocket connection with
// correlated request/response calls and event subscriptions. It does not
// reconnect itself: when the read pump dies every pending call fails and
// Health reports unreachable, and the platform manager reopens the
// connection with backoff.
type socket struct {
	conn      *websocket.Conn
	haVersion string

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan wsResult
	subs    map[int64]func(event json.RawMessage)
	err     error
	done    chan struct{}
}

type wsResult struct {
	result json.RawMessage
	err    error
}

// wsError is HA's structured call failure.
type wsError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// dialSocket connects, authenticates, and starts the read pump.
func dialSocket(ctx context.Context, baseURL string, insecure bool, token string) (*socket, error) {
	wsURL := strings.Replace(baseURL, "http", "ws", 1) + "/api/websocket"
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure},
		},
	}
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(32 << 20) // get_states on a large install is megabytes

	s := &socket{
		conn:    conn,
		pending: map[int64]chan wsResult{},
		subs:    map[int64]func(json.RawMessage){},
		done:    make(chan struct{}),
	}

	// Auth handshake: auth_required → auth → auth_ok | auth_invalid.
	var hello struct {
		Type      string `json:"type"`
		HAVersion string `json:"ha_version"`
	}
	if err := s.readJSON(ctx, &hello); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}
	if hello.Type != "auth_required" {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, fmt.Errorf("unexpected handshake message %q — is this a Home Assistant WebSocket endpoint?", hello.Type)
	}
	if err := s.writeJSON(ctx, map[string]any{"type": "auth", "access_token": token}); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}
	var verdict struct {
		Type      string `json:"type"`
		HAVersion string `json:"ha_version"`
		Message   string `json:"message"`
	}
	if err := s.readJSON(ctx, &verdict); err != nil {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, err
	}
	if verdict.Type != "auth_ok" {
		conn.Close(websocket.StatusNormalClosure, "")
		return nil, errors.New("Home Assistant rejected the access token — create a long-lived token under your profile's Security tab")
	}
	s.haVersion = verdict.HAVersion

	go s.pump()
	return s, nil
}

func (s *socket) readJSON(ctx context.Context, out any) error {
	_, data, err := s.conn.Read(ctx)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func (s *socket) writeJSON(ctx context.Context, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, data)
}

// pump routes incoming frames to pending calls and event subscribers until
// the connection dies.
func (s *socket) pump() {
	ctx := context.Background()
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			s.fail(err)
			return
		}
		var msg struct {
			ID      int64           `json:"id"`
			Type    string          `json:"type"`
			Success bool            `json:"success"`
			Result  json.RawMessage `json:"result"`
			Error   *wsError        `json:"error"`
			Event   json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "result", "pong":
			s.mu.Lock()
			ch := s.pending[msg.ID]
			delete(s.pending, msg.ID)
			s.mu.Unlock()
			if ch == nil {
				continue
			}
			if msg.Type == "pong" || msg.Success {
				ch <- wsResult{result: msg.Result}
			} else if msg.Error != nil {
				ch <- wsResult{err: fmt.Errorf("%s: %s", msg.Error.Code, msg.Error.Message)}
			} else {
				ch <- wsResult{err: errors.New("call failed")}
			}
		case "event":
			s.mu.Lock()
			fn := s.subs[msg.ID]
			s.mu.Unlock()
			if fn != nil {
				fn(msg.Event)
			}
		}
	}
}

// fail marks the socket dead and unblocks every waiter.
func (s *socket) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return
	}
	s.err = err
	close(s.done)
	for id, ch := range s.pending {
		ch <- wsResult{err: err}
		delete(s.pending, id)
	}
}

// dead reports the pump error, or nil while the connection is live.
func (s *socket) dead() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

// call sends one correlated request and waits for its result.
func (s *socket) call(ctx context.Context, msg map[string]any) (json.RawMessage, error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return nil, s.err
	}
	s.nextID++
	id := s.nextID
	ch := make(chan wsResult, 1)
	s.pending[id] = ch
	s.mu.Unlock()

	msg["id"] = id
	if err := s.writeJSON(ctx, msg); err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, err
	}
	select {
	case r := <-ch:
		return r.result, r.err
	case <-ctx.Done():
		s.mu.Lock()
		delete(s.pending, id)
		s.mu.Unlock()
		return nil, ctx.Err()
	case <-s.done:
		return nil, s.dead()
	}
}

// callInto is call with JSON decoding of the result.
func (s *socket) callInto(ctx context.Context, msg map[string]any, out any) error {
	res, err := s.call(ctx, msg)
	if err != nil {
		return err
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(res, out)
}

// subscribe registers for an event type; fn runs on the pump goroutine, so
// it must not block.
func (s *socket) subscribe(ctx context.Context, eventType string, fn func(event json.RawMessage)) error {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return s.err
	}
	s.nextID++
	id := s.nextID
	ch := make(chan wsResult, 1)
	s.pending[id] = ch
	s.subs[id] = fn
	s.mu.Unlock()

	err := s.writeJSON(ctx, map[string]any{"id": id, "type": "subscribe_events", "event_type": eventType})
	if err == nil {
		select {
		case r := <-ch:
			err = r.err
		case <-ctx.Done():
			err = ctx.Err()
		case <-s.done:
			err = s.dead()
		}
	}
	if err != nil {
		s.mu.Lock()
		delete(s.pending, id)
		delete(s.subs, id)
		s.mu.Unlock()
	}
	return err
}

// ping round-trips a heartbeat.
func (s *socket) ping(ctx context.Context) error {
	_, err := s.call(ctx, map[string]any{"type": "ping"})
	return err
}

func (s *socket) close() {
	s.conn.Close(websocket.StatusNormalClosure, "")
	s.fail(errors.New("connection closed"))
}
