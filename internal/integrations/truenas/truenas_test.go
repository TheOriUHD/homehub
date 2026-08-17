package truenas

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/truenassim"
)

func testConn(t *testing.T) (*Conn, func()) {
	t.Helper()
	srv := httptest.NewServer(truenassim.Handler("test-api-key"))
	conn := &Conn{
		inst:   integration.Instance{ID: uuid.New(), Type: TypeID, Name: "nas"},
		client: mustClient(t, srv.URL, "test-api-key"),
	}
	return conn, srv.Close
}

func mustClient(t *testing.T, host, key string) *client {
	t.Helper()
	c, err := newClient(map[string]any{"host": host}, map[string]string{"apiKey": key})
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	return c
}

func TestDriverTest(t *testing.T) {
	srv := httptest.NewServer(truenassim.Handler("good-key"))
	defer srv.Close()

	bad := Driver{}.Test(context.Background(), map[string]any{"host": srv.URL}, map[string]string{"apiKey": "wrong"})
	if bad.OK {
		t.Fatal("expected auth failure with wrong key")
	}
	ok := Driver{}.Test(context.Background(), map[string]any{"host": srv.URL}, map[string]string{"apiKey": "good-key"})
	if !ok.OK {
		t.Fatalf("expected success, got %q", ok.Message)
	}
}

func TestPoolsAndCapacity(t *testing.T) {
	conn, closeFn := testConn(t)
	defer closeFn()
	pools, err := conn.Pools(context.Background())
	if err != nil {
		t.Fatalf("pools: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("pools = %d, want 2", len(pools))
	}
	var tank NormPool
	for _, p := range pools {
		if p.Name == "tank" {
			tank = p
		}
	}
	if !tank.Healthy || tank.Status != "ONLINE" {
		t.Fatalf("tank health: %+v", tank)
	}
	// 9 TiB allocated of 16 TiB → ~56%.
	if tank.UsedPct < 55 || tank.UsedPct > 57 {
		t.Fatalf("tank usedPct = %.1f, want ~56", tank.UsedPct)
	}
}

func TestDatasetsParsedValues(t *testing.T) {
	conn, closeFn := testConn(t)
	defer closeFn()
	ds, err := conn.Datasets(context.Background())
	if err != nil {
		t.Fatalf("datasets: %v", err)
	}
	if len(ds) != 4 {
		t.Fatalf("datasets = %d, want 4", len(ds))
	}
	for _, d := range ds {
		if d.Name == "tank/media" {
			if d.UsedBytes != 6*(int64(1)<<40) {
				t.Fatalf("tank/media used = %d", d.UsedBytes)
			}
			if d.Compression != "LZ4" {
				t.Fatalf("tank/media compression = %q", d.Compression)
			}
		}
	}
}

func TestDisksAndShares(t *testing.T) {
	conn, closeFn := testConn(t)
	defer closeFn()
	disks, err := conn.Disks(context.Background())
	if err != nil {
		t.Fatalf("disks: %v", err)
	}
	if len(disks) != 6 {
		t.Fatalf("disks = %d, want 6", len(disks))
	}
	withTemp := 0
	for _, d := range disks {
		if d.Temperature != nil {
			withTemp++
		}
	}
	if withTemp != 6 {
		t.Fatalf("disks with temp = %d, want 6", withTemp)
	}
	shares, err := conn.Shares(context.Background())
	if err != nil {
		t.Fatalf("shares: %v", err)
	}
	kinds := map[string]int{}
	for _, s := range shares {
		kinds[s.Type]++
	}
	if kinds["smb"] != 2 || kinds["nfs"] != 1 || kinds["iscsi"] != 1 {
		t.Fatalf("share kinds = %v", kinds)
	}
}

func TestCollectMetrics(t *testing.T) {
	conn, closeFn := testConn(t)
	defer closeFn()
	samples, err := conn.Collect(context.Background())
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	// 2 pools × 2 metrics + 6 disk temps = 10.
	if len(samples) != 10 {
		t.Fatalf("samples = %d, want 10", len(samples))
	}
}

func TestHealthDegradedOnBadPool(t *testing.T) {
	conn, closeFn := testConn(t)
	defer closeFn()
	h := conn.Health(context.Background())
	if h.Status != integration.StatusConnected {
		t.Fatalf("health = %s, want connected", h.Status)
	}
}

func TestWebSocketTransport(t *testing.T) {
	const simKey = "ws-test-key"
	srv := httptest.NewServer(truenassim.Handler(simKey))
	defer srv.Close()
	ctx := context.Background()

	// Wrong username: the auth failure must surface, never silently REST.
	badRes := Driver{}.Test(ctx,
		map[string]any{"host": srv.URL, "username": "wronguser"},
		map[string]string{"apiKey": simKey})
	if badRes.OK || !strings.Contains(badRes.Message, "username") {
		t.Fatalf("wrong ws username must fail loudly, got ok=%v %q", badRes.OK, badRes.Message)
	}

	// Right username: everything reads over the socket.
	res := Driver{}.Test(ctx,
		map[string]any{"host": srv.URL, "username": truenassim.WSUsername},
		map[string]string{"apiKey": simKey})
	if !res.OK || !strings.Contains(res.Message, "WebSocket API") {
		t.Fatalf("ws transport expected, got ok=%v %q", res.OK, res.Message)
	}

	c, err := newClient(map[string]any{"host": srv.URL}, map[string]string{"apiKey": simKey})
	if err != nil {
		t.Fatal(err)
	}
	conn := &Conn{client: c, ws: newWSClient(c.base, truenassim.WSUsername, simKey, false)}

	pools, err := conn.Pools(ctx)
	if err != nil || len(pools) == 0 {
		t.Fatalf("ws pools: %v (%d)", err, len(pools))
	}
	shares, err := conn.Shares(ctx)
	if err != nil || len(shares) == 0 {
		t.Fatalf("ws shares: %v (%d)", err, len(shares))
	}
	mem, err := conn.Memory(ctx)
	if err != nil || mem.ArcBytes == 0 {
		t.Fatalf("ws memory: %v %+v", err, mem)
	}
	alerts, err := conn.Alerts(ctx)
	if err != nil {
		t.Fatalf("ws alerts: %v", err)
	}
	for _, a := range alerts {
		if strings.Contains(a.Formatted, "<") {
			t.Fatalf("alert HTML not stripped: %q", a.Formatted)
		}
	}
}
