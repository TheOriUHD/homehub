package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeHA speaks just enough of the Home Assistant WebSocket protocol to
// exercise the client: auth handshake, get_config/get_states, one
// subscription, and a pushed state_changed event.
func fakeHA(t *testing.T, wrongToken bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/api/websocket") {
			http.NotFound(w, r)
			return
		}
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		ctx := r.Context()
		send := func(v any) {
			raw, _ := json.Marshal(v)
			_ = c.Write(ctx, websocket.MessageText, raw)
		}
		read := func() map[string]json.RawMessage {
			_, data, err := c.Read(ctx)
			if err != nil {
				return nil
			}
			var m map[string]json.RawMessage
			_ = json.Unmarshal(data, &m)
			return m
		}

		send(map[string]any{"type": "auth_required", "ha_version": "2026.8.2"})
		auth := read()
		var token string
		_ = json.Unmarshal(auth["access_token"], &token)
		if wrongToken || token != "good-token" {
			send(map[string]any{"type": "auth_invalid", "message": "Invalid access token"})
			c.Close(websocket.StatusNormalClosure, "")
			return
		}
		send(map[string]any{"type": "auth_ok", "ha_version": "2026.8.2"})

		var subID int64
		for {
			msg := read()
			if msg == nil {
				return
			}
			var id int64
			var typ string
			_ = json.Unmarshal(msg["id"], &id)
			_ = json.Unmarshal(msg["type"], &typ)
			switch typ {
			case "ping":
				send(map[string]any{"id": id, "type": "pong"})
			case "get_config":
				send(map[string]any{"id": id, "type": "result", "success": true,
					"result": map[string]any{"location_name": "Test Home", "version": "2026.8.2"}})
			case "get_states":
				send(map[string]any{"id": id, "type": "result", "success": true,
					"result": []map[string]any{{
						"entity_id": "light.demo", "state": "off",
						"attributes":   map[string]any{"friendly_name": "Demo Light"},
						"last_changed": time.Now().UTC(), "last_updated": time.Now().UTC(),
					}}})
			case "subscribe_events":
				var evType string
				_ = json.Unmarshal(msg["event_type"], &evType)
				send(map[string]any{"id": id, "type": "result", "success": true})
				if evType == "state_changed" {
					subID = id
					// Push one state change right after the subscription.
					send(map[string]any{"id": subID, "type": "event", "event": map[string]any{
						"event_type": "state_changed",
						"data": map[string]any{
							"entity_id": "light.demo",
							"new_state": map[string]any{
								"entity_id": "light.demo", "state": "on",
								"attributes":   map[string]any{"friendly_name": "Demo Light", "brightness": 128},
								"last_changed": time.Now().UTC(), "last_updated": time.Now().UTC(),
							},
						},
					}})
				}
			case "config/area_registry/list", "config/device_registry/list", "config/entity_registry/list":
				send(map[string]any{"id": id, "type": "result", "success": true, "result": []any{}})
			case "call_service":
				var domain string
				_ = json.Unmarshal(msg["domain"], &domain)
				if domain == "broken" {
					send(map[string]any{"id": id, "type": "result", "success": false,
						"error": map[string]any{"code": "not_found", "message": "Service broken.explode not found."}})
				} else {
					send(map[string]any{"id": id, "type": "result", "success": true, "result": map[string]any{}})
				}
			}
		}
	}))
}

func TestDialSocketAuth(t *testing.T) {
	srv := fakeHA(t, false)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := dialSocket(ctx, srv.URL, false, "bad-token"); err == nil {
		t.Fatal("expected auth failure with wrong token")
	}
	sock, err := dialSocket(ctx, srv.URL, false, "good-token")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sock.close()
	if sock.haVersion != "2026.8.2" {
		t.Fatalf("ha version = %q", sock.haVersion)
	}
	if err := sock.ping(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
}

func TestConnStateFlow(t *testing.T) {
	srv := fakeHA(t, false)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sock, err := dialSocket(ctx, srv.URL, false, "good-token")
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	var events atomic.Int64
	conn := &Conn{sock: sock}
	conn.deps.Publish = func(event string, data any) {
		if event == "state" {
			events.Add(1)
		}
	}
	if err := conn.bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	defer conn.Close()

	if got := conn.Info().LocationName; got != "Test Home" {
		t.Fatalf("location = %q", got)
	}
	// The fake pushes one state_changed immediately after subscribing.
	deadline := time.Now().Add(3 * time.Second)
	for events.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if events.Load() == 0 {
		t.Fatal("no state event published")
	}
	e, ok := conn.Entity("light.demo")
	if !ok {
		t.Fatal("light.demo missing")
	}
	if e.State != "on" {
		t.Fatalf("state after push = %q, want on", e.State)
	}
	if b, _ := numericAttr(e.Attributes, "brightness"); b != 128 {
		t.Fatalf("brightness = %v", b)
	}

	if err := conn.CallService(ctx, "light", "turn_off", "light.demo", nil); err != nil {
		t.Fatalf("call_service: %v", err)
	}
	if err := conn.CallService(ctx, "broken", "explode", "light.demo", nil); err == nil {
		t.Fatal("expected upstream service error to surface")
	}
}
