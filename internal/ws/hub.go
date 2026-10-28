// Package ws implements the single multiplexed WebSocket every client holds.
//
// Protocol (JSON text frames):
//
//	client → server: {"type":"subscribe","topic":"..."}
//	                 {"type":"unsubscribe","topic":"..."}
//	server → client: {"type":"event","topic":"...","data":{...}}
//	                 {"type":"subscribed","topic":"..."} / {"type":"unsubscribed","topic":"..."}
//	                 {"type":"error","topic":"...","message":"..."}
//
// Topics are permission-checked twice: at subscribe time and again on every
// fanout, so a revoked grant stops the stream within the permission cache TTL.
// Binary frames are reserved for terminal/console subchannels (phase 2+);
// their framing is [1B channel kind][16B stream id][payload].
package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// AuthorizeFunc decides whether a principal may receive a topic. Wired to the
// RBAC engine at startup; nil denies everything (fail closed).
type AuthorizeFunc func(ctx context.Context, userID uuid.UUID, viewAsRole *uuid.UUID, topic string) bool

// CanViewFunc reports whether a principal has view on one fine-grained RBAC
// resource path (e.g. "amp/<panel>/instance/<id>"). Used to scope per-child
// live data on a coarse instance topic down to what the client may actually
// see — mirroring the REST layer's per-sub-resource `Can(view)` check. Wired to
// the RBAC engine at startup; nil denies everything (fail closed).
type CanViewFunc func(ctx context.Context, userID uuid.UUID, viewAsRole *uuid.UUID, resource string) bool

type Hub struct {
	logger    *slog.Logger
	authorize AuthorizeFunc
	canView   CanViewFunc

	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub(logger *slog.Logger, authorize AuthorizeFunc, canView CanViewFunc) *Hub {
	return &Hub{
		logger:    logger.With("component", "ws"),
		authorize: authorize,
		canView:   canView,
		clients:   map[*Client]struct{}{},
	}
}

// sendBuffer is how many frames may wait for one client before it counts as
// too slow. A healthy tab drains in milliseconds; a stalled one (asleep,
// backgrounded on a phone, behind a dead network) is cut loose at this depth
// instead of making every publisher wait on it.
const sendBuffer = 256

type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	userID     uuid.UUID
	viewAsRole *uuid.UUID
	send       chan []byte
	cancel     context.CancelFunc
	dropped    atomic.Bool

	mu   sync.Mutex
	subs map[string]struct{}
}

type frame struct {
	Type    string          `json:"type"`
	Topic   string          `json:"topic,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Message string          `json:"message,omitempty"`
}

// ClientCount reports connected clients for the health page.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// subscribers snapshots the clients subscribed to topic. Publishers work from
// the copy so that per-client permission checks (database lookups) and sends
// never run while the hub lock is held — a held lock would also block every
// tab connecting or disconnecting.
func (h *Hub) subscribers(topic string, match func(*Client) bool) []*Client {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*Client, 0, len(h.clients))
	for c := range h.clients {
		if c.subscribed(topic) && (match == nil || match(c)) {
			out = append(out, c)
		}
	}
	return out
}

// Publish fans an event out to every subscribed, still-authorized client.
func (h *Hub) Publish(topic string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.logger.Error("marshal event", "topic", topic, "error", err)
		return
	}
	msg, _ := json.Marshal(frame{Type: "event", Topic: topic, Data: raw})

	for _, c := range h.subscribers(topic, nil) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ok := h.authorize != nil && h.authorize(ctx, c.userID, c.viewAsRole, topic)
		cancel()
		if !ok {
			continue
		}
		c.trySend(msg)
	}
}

// canViewResource authorizes one client against a fine-grained resource path.
func (h *Hub) canViewResource(c *Client, resource string) bool {
	if h.canView == nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return h.canView(ctx, c.userID, c.viewAsRole, resource)
}

// PublishScoped fans an event out to subscribers of the coarse `topic`, but
// delivers only to clients who also have view on `resource` — the specific
// sub-resource this event concerns (one AMP instance, one Minecraft server).
// Clients still SUBSCRIBE to the whole-instance topic (so the frontend is
// unchanged), but a member granted only a sibling sub-resource never receives
// this one. Closes the per-child console/status leak on instance topics.
func (h *Hub) PublishScoped(topic, resource string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.logger.Error("marshal event", "topic", topic, "error", err)
		return
	}
	msg, _ := json.Marshal(frame{Type: "event", Topic: topic, Data: raw})

	for _, c := range h.subscribers(topic, nil) {
		if !h.canViewResource(c, resource) {
			continue
		}
		c.trySend(msg)
	}
}

// ScopedRow is one payload row tagged with the RBAC resource it belongs to, so
// PublishRows can deliver each client only the rows it may see.
type ScopedRow struct {
	Resource string
	Data     any
}

// PublishRows delivers, to each subscriber of `topic`, a `{key: [...]}` event
// containing only the rows they have view on. A client with no visible rows is
// skipped entirely. Used for batched per-resource fanout (metrics samples)
// where one event carries data for many sibling sub-resources.
func (h *Hub) PublishRows(topic, key string, rows []ScopedRow) {
	if len(rows) == 0 {
		return
	}
	for _, c := range h.subscribers(topic, nil) {
		visible := make([]any, 0, len(rows))
		for _, row := range rows {
			if h.canViewResource(c, row.Resource) {
				visible = append(visible, row.Data)
			}
		}
		if len(visible) == 0 {
			continue
		}
		raw, err := json.Marshal(map[string]any{key: visible})
		if err != nil {
			continue
		}
		msg, _ := json.Marshal(frame{Type: "event", Topic: topic, Data: raw})
		c.trySend(msg)
	}
}

// PublishUser delivers an event to one user's connections only (their inbox,
// their session revocations). No topic authorization beyond identity.
func (h *Hub) PublishUser(userID uuid.UUID, topic string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		return
	}
	msg, _ := json.Marshal(frame{Type: "event", Topic: topic, Data: raw})
	for _, c := range h.subscribers(topic, func(c *Client) bool { return c.userID == userID }) {
		c.trySend(msg)
	}
}

// Serve owns a connection for its lifetime: registers the client, pumps
// outbound messages, and processes inbound control frames.
func (h *Hub) Serve(ctx context.Context, conn *websocket.Conn, userID uuid.UUID, viewAsRole *uuid.UUID) {
	client := &Client{
		hub:        h,
		conn:       conn,
		userID:     userID,
		viewAsRole: viewAsRole,
		send:       make(chan []byte, sendBuffer),
		subs:       map[string]struct{}{},
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client.cancel = cancel

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
	}()

	go client.writePump(ctx, cancel)
	client.readPump(ctx)
}

func (c *Client) subscribed(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.subs[topic]
	return ok
}

// trySend queues a message without ever blocking the publisher; a client too
// slow to drain its buffer is disconnected rather than allowed to stall the
// hub. The browser reconnects and resubscribes, landing on fresh state.
func (c *Client) trySend(msg []byte) {
	if c.dropped.Load() {
		return
	}
	select {
	case c.send <- msg:
	default:
		c.drop()
	}
}

// drop disconnects a slow consumer, once. Close runs a close handshake that
// waits on the very peer that has stopped reading (and on a writer stuck in
// its own write timeout), so it goes to its own goroutine; cancelling the
// client's context stops its pumps straight away.
func (c *Client) drop() {
	if !c.dropped.CompareAndSwap(false, true) {
		return
	}
	c.hub.logger.Warn("dropping slow websocket client", "user", c.userID)
	go func() { _ = c.conn.Close(websocket.StatusPolicyViolation, "client too slow") }()
	if c.cancel != nil {
		c.cancel()
	}
}

func (c *Client) writePump(ctx context.Context, cancel context.CancelFunc) {
	defer cancel()
	ping := time.NewTicker(30 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-c.send:
			wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Write(wctx, websocket.MessageText, msg)
			wcancel()
			if err != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.conn.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		}
	}
}

func (c *Client) readPump(ctx context.Context) {
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		var f frame
		if err := json.Unmarshal(data, &f); err != nil || f.Topic == "" {
			continue
		}
		switch f.Type {
		case "subscribe":
			actx, acancel := context.WithTimeout(ctx, 2*time.Second)
			ok := c.hub.authorize != nil && c.hub.authorize(actx, c.userID, c.viewAsRole, f.Topic)
			acancel()
			if !ok {
				c.reply(frame{Type: "error", Topic: f.Topic, Message: "not authorized for this topic"})
				continue
			}
			c.mu.Lock()
			c.subs[f.Topic] = struct{}{}
			c.mu.Unlock()
			c.reply(frame{Type: "subscribed", Topic: f.Topic})
		case "unsubscribe":
			c.mu.Lock()
			delete(c.subs, f.Topic)
			c.mu.Unlock()
			c.reply(frame{Type: "unsubscribed", Topic: f.Topic})
		}
	}
}

func (c *Client) reply(f frame) {
	msg, _ := json.Marshal(f)
	c.trySend(msg)
}
