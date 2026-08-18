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
	// Modern keys are user-linked: a bare key on a box that speaks the
	// WebSocket API must fail with guidance, never silently downgrade to
	// REST (which no longer exists on 25.10+).
	bare := Driver{}.Test(context.Background(), map[string]any{"host": srv.URL}, map[string]string{"apiKey": "good-key"})
	if bare.OK || !strings.Contains(bare.Message, "username") {
		t.Fatalf("bare key on a WS-speaking box must ask for the username, got ok=%v %q", bare.OK, bare.Message)
	}
	ok := Driver{}.Test(context.Background(),
		map[string]any{"host": srv.URL, "username": truenassim.WSUsername},
		map[string]string{"apiKey": "good-key"})
	if !ok.OK || !strings.Contains(ok.Message, "WebSocket API") {
		t.Fatalf("expected WS success, got ok=%v %q", ok.OK, ok.Message)
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
	// v26: temperature comes from disk.temperatures, not disk.query — the
	// merge must repopulate it.
	disks, err := conn.Disks(ctx)
	if err != nil || len(disks) == 0 {
		t.Fatalf("ws disks: %v (%d)", err, len(disks))
	}
	for _, d := range disks {
		if d.Temperature == nil {
			t.Fatalf("disk %s missing temperature after disk.temperatures merge", d.Name)
		}
		if d.Pool == "" {
			t.Fatalf("disk %s missing pool name", d.Name)
		}
	}
	shares, err := conn.Shares(ctx)
	if err != nil || len(shares) == 0 {
		t.Fatalf("ws shares: %v (%d)", err, len(shares))
	}
	// v26: used/created come back as requested ZFS properties, not fields.
	snaps, err := conn.Snapshots(ctx, 0)
	if err != nil || len(snaps) == 0 {
		t.Fatalf("ws snapshots: %v (%d)", err, len(snaps))
	}
	for _, sn := range snaps {
		if sn.UsedBytes() == 0 || sn.CreatedMs() == 0 {
			t.Fatalf("snapshot %s: used=%d createdMs=%d — v26 properties not decoded", sn.Name, sn.UsedBytes(), sn.CreatedMs())
		}
	}
	if snaps[0].CreatedMs() < snaps[1].CreatedMs() {
		t.Fatal("snapshots not sorted newest-first")
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
		// v26 sends ISO strings — FlexTime must still yield a timestamp.
		if a.Datetime.Ms == 0 {
			t.Fatalf("alert datetime not decoded from ISO string: %+v", a)
		}
		if a.UUID == "" {
			t.Fatalf("alert uuid missing: %+v", a)
		}
	}

	// Performance stats: cpu/load/memory plus per-adapter traffic in bits/s
	// (the sim serves Kilobits/s like real netdata; 950 kb/s → 950000 b/s).
	stats, err := conn.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if len(stats.CPU) == 0 || len(stats.Load) == 0 || len(stats.MemAvailable) == 0 {
		t.Fatalf("stats series missing: %+v", stats)
	}
	if len(stats.Interfaces) != 1 || stats.Interfaces[0].Name != "eno1" || !stats.Interfaces[0].Up {
		t.Fatalf("interfaces: %+v", stats.Interfaces)
	}
	pts := stats.Interfaces[0].Points
	if len(pts) == 0 || pts[len(pts)-1].InBps != 950000 {
		t.Fatalf("traffic points not converted to bits/s: %+v", pts)
	}

	// Dismissing an alert must stick.
	if err := conn.DismissAlert(ctx, alerts[0].UUID); err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	after, err := conn.Alerts(ctx)
	if err != nil {
		t.Fatalf("alerts after dismiss: %v", err)
	}
	for _, a := range after {
		if a.UUID == alerts[0].UUID && !a.Dismissed {
			t.Fatalf("alert %s still active after dismiss", a.UUID)
		}
	}
}
