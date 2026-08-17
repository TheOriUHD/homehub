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
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

// AuthorizeFunc decides whether a principal may receive a topic. Wired to the
// RBAC engine at startup; nil denies everything (fail closed).
type AuthorizeFunc func(ctx context.Context, userID uuid.UUID, viewAsRole *uuid.UUID, topic string) bool

type Hub struct {
	logger    *slog.Logger
	authorize AuthorizeFunc

	mu      sync.RWMutex
	clients map[*Client]struct{}
}

func NewHub(logger *slog.Logger, authorize AuthorizeFunc) *Hub {
	return &Hub{
		logger:    logger.With("component", "ws"),
		authorize: authorize,
		clients:   map[*Client]struct{}{},
	}
}

type Client struct {
	hub        *Hub
	conn       *websocket.Conn
	userID     uuid.UUID
	viewAsRole *uuid.UUID
	send       chan []byte

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

// Publish fans an event out to every subscribed, still-authorized client.
func (h *Hub) Publish(topic string, data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.logger.Error("marshal event", "topic", topic, "error", err)
		return
	}
	msg, _ := json.Marshal(frame{Type: "event", Topic: topic, Data: raw})

	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if !c.subscribed(topic) {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		ok := h.authorize != nil && h.authorize(ctx, c.userID, c.viewAsRole, topic)
		cancel()
		if !ok {
			continue
		}
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
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if c.userID == userID && c.subscribed(topic) {
			c.trySend(msg)
		}
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
		send:       make(chan []byte, 256),
		subs:       map[string]struct{}{},
	}
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
	}()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go client.writePump(ctx, cancel)
	client.readPump(ctx)
}

func (c *Client) subscribed(topic string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.subs[topic]
	return ok
}

// trySend queues a message; a client too slow to drain its buffer is
// disconnected rather than allowed to stall the hub.
func (c *Client) trySend(msg []byte) {
	select {
	case c.send <- msg:
	default:
		c.conn.Close(websocket.StatusPolicyViolation, "client too slow")
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
