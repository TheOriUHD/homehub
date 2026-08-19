package moonraker

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// runWS keeps a live JSON-RPC subscription to Moonraker so temperatures, print
// progress, motion and the G-code console stream in real time. It reconnects
// with backoff forever until the connection is closed. Every failure just
// drops `live` to false — the REST fallback in Snapshot keeps the dashboard
// working meanwhile.
func (c *Conn) runWS(ctx context.Context) {
	backoff := 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := c.wsSession(ctx)
		c.setLive(false)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			// quiet: transient upstream churn is normal on a printer host
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = 2 * time.Second
	}
}

func (c *Conn) setLive(v bool) {
	c.mu.Lock()
	c.live = v
	c.mu.Unlock()
}

// wsSession runs one connection: discover objects, open the socket, subscribe,
// then pump notifications until the socket dies.
func (c *Conn) wsSession(ctx context.Context) error {
	// Ensure we know the object set before subscribing.
	c.mu.Lock()
	haveObjects := len(c.subNames) > 0 && !c.discAt.IsZero()
	c.mu.Unlock()
	if !haveObjects {
		dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := c.discover(dctx)
		cancel()
		if err != nil {
			return err
		}
	}

	wsURL := c.client.wsBase() + "/websocket"
	// Authenticated Moonraker needs a one-shot token on the socket URL; a
	// trusted-client setup connects bare.
	if c.client.key != "" {
		if tok := c.oneshotToken(ctx); tok != "" {
			wsURL += "?token=" + tok
		}
	}

	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, _, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{HTTPClient: &http.Client{}})
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	conn.SetReadLimit(16 << 20)

	// Subscribe to every stateful object (nil = all fields).
	c.mu.Lock()
	names := append([]string(nil), c.subNames...)
	c.mu.Unlock()
	objects := make(map[string]any, len(names))
	for _, n := range names {
		objects[n] = nil
	}
	req, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "printer.objects.subscribe",
		"params": map[string]any{"objects": objects},
	})
	wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
	err = conn.Write(wctx, websocket.MessageText, req)
	wcancel()
	if err != nil {
		return err
	}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Minute)
		_, raw, err := conn.Read(rctx)
		rcancel()
		if err != nil {
			return err
		}
		c.handleWSMessage(raw)
	}
}

// oneshotToken mints a single-use websocket token using the API key.
func (c *Conn) oneshotToken(ctx context.Context) string {
	tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var tok string
	if err := c.client.get(tctx, "/access/oneshot_token", &tok); err != nil {
		return ""
	}
	return tok
}

type wsMessage struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Params json.RawMessage `json:"params"`
}

func (c *Conn) handleWSMessage(raw []byte) {
	var msg wsMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return
	}

	// Subscribe response: {"result":{"eventtime":…,"status":{…}}} — seed state.
	if msg.ID != nil && len(msg.Result) > 0 {
		var res struct {
			Status map[string]objFields `json:"status"`
		}
		if json.Unmarshal(msg.Result, &res) == nil && res.Status != nil {
			c.mergeState(res.Status)
			c.setLive(true)
			c.publishSnapshot(true)
		}
		return
	}

	switch msg.Method {
	case "notify_status_update":
		// params: [ {statusDelta}, eventtime ]
		var params []json.RawMessage
		if json.Unmarshal(msg.Params, &params) != nil || len(params) == 0 {
			return
		}
		var delta map[string]objFields
		if json.Unmarshal(params[0], &delta) != nil {
			return
		}
		c.mergeState(delta)
		c.setLive(true)
		c.publishSnapshot(false)
	case "notify_gcode_response":
		var params []string
		if json.Unmarshal(msg.Params, &params) == nil && len(params) > 0 {
			c.deps.Publish("gcode", map[string]any{"line": params[0]})
		}
	case "notify_klippy_ready", "notify_klippy_shutdown", "notify_klippy_disconnected":
		state := strings.TrimPrefix(msg.Method, "notify_klippy_")
		c.deps.Publish("klippy", map[string]any{"state": state})
		c.publishSnapshot(true)
	}
}

// mergeState folds a status delta into the live cache at field granularity —
// notify_status_update carries only changed fields, so a shallow object
// replace would lose the rest.
func (c *Conn) mergeState(delta map[string]objFields) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == nil {
		c.state = map[string]objFields{}
	}
	for obj, fields := range delta {
		cur := c.state[obj]
		if cur == nil {
			cur = objFields{}
			c.state[obj] = cur
		}
		for k, v := range fields {
			cur[k] = v
		}
	}
	c.stateAt = time.Now()
}

// publishSnapshot rebuilds the dashboard snapshot from the live cache and
// pushes it to subscribers. High-rate deltas are coalesced to ~3/sec unless
// `force` (initial seed / klippy transition), which always emits.
func (c *Conn) publishSnapshot(force bool) {
	c.mu.Lock()
	if !force && time.Since(c.lastPublish) < 350*time.Millisecond {
		c.mu.Unlock()
		return
	}
	c.lastPublish = time.Now()
	state := cloneState(c.state)
	objects := append([]string(nil), c.objects...)
	macros := append([]Macro(nil), c.macros...)
	meta := c.meta
	c.mu.Unlock()

	snap := buildSnapshot(objects, state, macros)
	snap.Live = true
	snap.Printer = meta
	if cams := c.camerasCached(); len(cams) > 0 {
		snap.Cameras = cams
	}
	if wh, ok := state["webhooks"]; ok {
		if s := wh.str("state"); s != "" {
			snap.Klippy.State = s
		}
		if m := wh.str("state_message"); m != "" {
			snap.Klippy.Message = strings.TrimSpace(m)
		}
	}
	c.deps.Publish("snapshot", snap)
}
