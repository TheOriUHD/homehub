package ws

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"
)

func testHub(t *testing.T) (*Hub, string) {
	t.Helper()
	allow := func(context.Context, uuid.UUID, *uuid.UUID, string) bool { return true }
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)), allow,
		func(context.Context, uuid.UUID, *uuid.UUID, string) bool { return true })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		conn.SetReadLimit(1 << 20)
		hub.Serve(r.Context(), conn, uuid.New(), nil)
	}))
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http")
}

// dialSubscribed connects and subscribes to topic, waiting for the ack.
func dialSubscribed(t *testing.T, url, topic string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadLimit(1 << 20)
	t.Cleanup(func() { conn.CloseNow() })
	sub, _ := json.Marshal(frame{Type: "subscribe", Topic: topic})
	if err := conn.Write(ctx, websocket.MessageText, sub); err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	_, data, err := conn.Read(ctx)
	if err != nil || !strings.Contains(string(data), `"subscribed"`) {
		t.Fatalf("subscribe ack: %s %v", data, err)
	}
	return conn
}

// A tab that stops reading must not hold up anyone else's live updates: the
// publisher never blocks on it, a healthy client keeps receiving, and the
// stalled one is disconnected once its buffer is full.
func TestSlowClientNeverStallsPublishers(t *testing.T) {
	hub, url := testHub(t)
	_ = dialSubscribed(t, url, "metrics") // subscribes, then never reads again
	healthy := dialSubscribed(t, url, "metrics")

	got := make(chan int, 1)
	go func() {
		n := 0
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			_, _, err := healthy.Read(ctx)
			cancel()
			if err != nil {
				got <- n
				return
			}
			n++
			if n == 3*sendBuffer {
				got <- n
				return
			}
		}
	}()

	// Big frames fill the stalled peer's socket buffers fast, so its writer
	// blocks and its queue backs up — the situation that used to stall Publish.
	payload := map[string]string{"blob": strings.Repeat("x", 64<<10)}
	start := time.Now()
	for i := 0; i < 3*sendBuffer; i++ {
		before := time.Now()
		hub.Publish("metrics", payload)
		if d := time.Since(before); d > 500*time.Millisecond {
			t.Fatalf("publish %d blocked for %s", i, d)
		}
		// Let the healthy reader keep pace; the stalled one cannot.
		time.Sleep(200 * time.Microsecond)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("publishing took %s", d)
	}

	if n := <-got; n != 3*sendBuffer {
		t.Fatalf("healthy client got %d of %d events", n, 3*sendBuffer)
	}
	deadline := time.Now().Add(10 * time.Second)
	for hub.ClientCount() != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("stalled client still connected (%d clients)", hub.ClientCount())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
