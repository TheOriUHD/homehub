package pvesim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestVNCWebsocketEcho(t *testing.T) {
	s := New(Options{})
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	req, _ := http.NewRequest("POST", ts.URL+"/api2/json/nodes/pve1/qemu/101/vncproxy", strings.NewReader("websocket=1"))
	req.Header.Set("Authorization", "PVEAPIToken="+s.Token())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data struct {
			Ticket string `json:"ticket"`
			Port   string `json:"port"`
			User   string `json:"user"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if env.Data.Ticket == "" || env.Data.Port == "" {
		t.Fatalf("vncproxy response incomplete: %+v", env.Data)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	wsURL := strings.Replace(ts.URL, "http://", "ws://", 1) +
		"/api2/json/nodes/pve1/vncwebsocket?port=" + env.Data.Port +
		"&vncticket=" + url.QueryEscape(env.Data.Ticket)
	conn, _, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"PVEAPIToken=" + s.Token()}},
	})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.CloseNow()

	// VNC tickets skip the termproxy preamble (real PVE semantics): the
	// stream echoes from the first frame.
	payload := []byte{0x01, 0x02, 0xff, 0x00}
	if err := conn.Write(ctx, websocket.MessageBinary, payload); err != nil {
		t.Fatal(err)
	}
	typ, data, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || string(data) != string(payload) {
		t.Fatalf("echo = %v %q (%v), want binary payload back", typ, data, err)
	}

	// The ticket is single-use: a second connection must be refused.
	_, hresp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"PVEAPIToken=" + s.Token()}},
	})
	if err == nil {
		t.Fatal("second dial with used ticket succeeded, want 401")
	}
	if hresp != nil && hresp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("second dial status = %d, want 401", hresp.StatusCode)
	}
}
