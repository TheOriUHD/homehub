package truenas

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

// wsClient speaks TrueNAS 25.x's JSON-RPC 2.0 WebSocket API at
// /api/current — the transport TrueNAS itself recommends now that the REST
// shim is deprecated (the NAS raises an alert about REST authentications).
//
// Auth: user-linked API keys (25.04+) log in via auth.login_ex with the
// OWNING USERNAME plus the key; legacy keys via auth.login_with_api_key.
// That is why the integration config carries an optional username.
//
// Calls are single-flight over one socket: a mutex serializes them, and a
// dead socket is redialed once per call. The reads this driver does are
// low-rate (health/collect ticks and page loads), so lockstep is plenty.
type wsClient struct {
	wsURL    string
	username string
	key      string
	insecure bool

	mu     sync.Mutex
	conn   *websocket.Conn
	seq    int64
	authed bool
}

// errWsAuth marks an authentication rejection — callers must NOT fall back
// to REST for these, because the operator explicitly configured the
// WebSocket transport and should see the failure.
var errWsAuth = errors.New("the NAS rejected the API key over the WebSocket API — check that the username matches the account the key belongs to")

func newWSClient(base, username, key string, insecure bool) *wsClient {
	wsURL := strings.Replace(base, "https://", "wss://", 1)
	wsURL = strings.Replace(wsURL, "http://", "ws://", 1)
	// base is scheme://host/api/v2.0 — swap the REST path for the WS one.
	wsURL = strings.TrimSuffix(wsURL, "/api/v2.0") + "/api/current"
	return &wsClient{wsURL: wsURL, username: username, key: key, insecure: insecure}
}

type rpcEnvelope struct {
	ID     json.Number     `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) friendly() error {
	var data struct {
		Reason  string `json:"reason"`
		Errname string `json:"errname"`
	}
	if e.Data != nil && json.Unmarshal(e.Data, &data) == nil && data.Reason != "" {
		return fmt.Errorf("TrueNAS: %s", strings.TrimSpace(data.Reason))
	}
	return fmt.Errorf("TrueNAS: %s", e.Message)
}

// call performs one JSON-RPC request, dialing and authenticating on demand
// and retrying once on a dead socket.
func (c *wsClient) call(ctx context.Context, method string, params []any, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if err := c.ensureLocked(ctx); err != nil {
			return err
		}
		raw, err := c.roundTripLocked(ctx, method, params)
		if err != nil {
			c.dropLocked()
			var rpcErr *rpcError
			if errors.As(err, &rpcErr) || errors.Is(err, errWsAuth) || attempt > 0 {
				return err
			}
			continue // one redial for transport-level failures
		}
		if out != nil && len(raw) > 0 && string(raw) != "null" {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("%s: decode response: %w", method, err)
			}
		}
		return nil
	}
}

func (c *wsClient) dropLocked() {
	if c.conn != nil {
		_ = c.conn.Close(websocket.StatusNormalClosure, "")
		c.conn = nil
	}
	c.authed = false
}

func (c *wsClient) ensureLocked(ctx context.Context) error {
	if c.conn != nil && c.authed {
		return nil
	}
	c.dropLocked()

	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	httpClient := &http.Client{}
	if c.insecure {
		httpClient.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
	}
	conn, _, err := websocket.Dial(dctx, c.wsURL, &websocket.DialOptions{HTTPClient: httpClient})
	if err != nil {
		return fmt.Errorf("dialing the WebSocket API: %w", err)
	}
	conn.SetReadLimit(32 << 20)
	c.conn = conn

	// User-linked keys need the owning username; legacy keys work bare.
	if c.username != "" {
		var res struct {
			ResponseType string `json:"response_type"`
		}
		raw, err := c.roundTripLocked(ctx, "auth.login_ex", []any{map[string]any{
			"mechanism": "API_KEY_PLAIN", "username": c.username, "api_key": c.key,
		}})
		if err == nil {
			err = json.Unmarshal(raw, &res)
		}
		if err != nil {
			c.dropLocked()
			return fmt.Errorf("WebSocket login failed: %w", err)
		}
		if res.ResponseType != "SUCCESS" {
			c.dropLocked()
			return errWsAuth
		}
	} else {
		var ok bool
		raw, err := c.roundTripLocked(ctx, "auth.login_with_api_key", []any{c.key})
		if err == nil {
			err = json.Unmarshal(raw, &ok)
		}
		if err != nil {
			c.dropLocked()
			return fmt.Errorf("WebSocket login failed: %w", err)
		}
		if !ok {
			c.dropLocked()
			return errWsAuth
		}
	}
	c.authed = true
	return nil
}

// roundTripLocked writes one request and reads until its response arrives,
// skipping event notifications.
func (c *wsClient) roundTripLocked(ctx context.Context, method string, params []any) (json.RawMessage, error) {
	c.seq++
	id := c.seq
	if params == nil {
		params = []any{}
	}
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": id, "method": method, "params": params,
	})
	if err != nil {
		return nil, err
	}
	wctx, wcancel := context.WithTimeout(ctx, 15*time.Second)
	defer wcancel()
	if err := c.conn.Write(wctx, websocket.MessageText, req); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		rctx, rcancel := context.WithTimeout(ctx, 30*time.Second)
		_, raw, err := c.conn.Read(rctx)
		rcancel()
		if err != nil {
			return nil, err
		}
		var envelope rpcEnvelope
		if json.Unmarshal(raw, &envelope) != nil {
			continue
		}
		if envelope.ID.String() != fmt.Sprint(id) {
			continue // notification or stale response
		}
		if envelope.Error != nil {
			return nil, envelope.Error.friendly()
		}
		return envelope.Result, nil
	}
	return nil, errors.New("timed out waiting for the WebSocket API response")
}

func (e *rpcError) Error() string { return e.friendly().Error() }
