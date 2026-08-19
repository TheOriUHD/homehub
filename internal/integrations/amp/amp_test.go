package amp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/ampsim"
	"github.com/TheOriUHD/homehub/internal/integration"
)

func newTestConn(t *testing.T) (*Conn, *ampsim.Sim) {
	t.Helper()
	sim := ampsim.New()
	srv := httptest.NewServer(sim)
	t.Cleanup(srv.Close)

	c, err := newClient(
		map[string]any{"host": srv.URL, "username": "admin"},
		map[string]string{"password": "test123"},
	)
	if err != nil {
		t.Fatalf("newClient: %v", err)
	}
	inst := integration.Instance{ID: uuid.New(), Type: TypeID, Name: "sim"}
	conn := newConn(inst, c, integration.Deps{Publish: func(string, any) {}, PublishScoped: func(string, string, any) {}})
	t.Cleanup(conn.Close)
	return conn, sim
}

func TestTestVerdicts(t *testing.T) {
	sim := ampsim.New()
	srv := httptest.NewServer(sim)
	defer srv.Close()

	res := Driver{}.Test(context.Background(),
		map[string]any{"host": srv.URL, "username": "admin"},
		map[string]string{"password": "test123"})
	if !res.OK {
		t.Fatalf("expected success, got %q", res.Message)
	}
	if !strings.Contains(res.Message, "4 instance(s)") {
		t.Fatalf("unexpected message %q", res.Message)
	}

	bad := Driver{}.Test(context.Background(),
		map[string]any{"host": srv.URL, "username": "admin"},
		map[string]string{"password": "wrong"})
	if bad.OK || !strings.Contains(bad.Message, "Invalid username or password") {
		t.Fatalf("expected login rejection, got ok=%v %q", bad.OK, bad.Message)
	}
}

func TestTargetsAndMetrics(t *testing.T) {
	conn, _ := newTestConn(t)
	targets, err := conn.Targets(context.Background())
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) != 1 || len(targets[0].Instances) != 4 {
		t.Fatalf("expected 1 target with 4 instances, got %+v", targets)
	}
	target := targets[0]
	if target.CPUModel != "Ryzen 7 5800X" || target.RAMMB != 32768 {
		t.Fatalf("platform not mapped: %+v", target)
	}
	var lobby *NormInstance
	for i := range target.Instances {
		if target.Instances[i].ID == ampsim.LobbyID {
			lobby = &target.Instances[i]
		}
	}
	if lobby == nil {
		t.Fatal("lobby instance missing")
	}
	if lobby.State != "running" || !lobby.Running {
		t.Fatalf("lobby should be running, got %+v", lobby)
	}
	if lobby.CPUPercent == nil || lobby.MemUsedMB == nil || lobby.Players == nil {
		t.Fatalf("lobby metrics missing: %+v", lobby)
	}
	if *lobby.MaxPlayers != 20 {
		t.Fatalf("max players = %d", *lobby.MaxPlayers)
	}
}

func TestInstanceStatusThroughProxy(t *testing.T) {
	conn, _ := newTestConn(t)
	status, err := conn.InstanceStatus(context.Background(), ampsim.LobbyID)
	if err != nil {
		t.Fatalf("InstanceStatus: %v", err)
	}
	if stateName(status.State) != "running" || status.Uptime == "0:00:00:00" {
		t.Fatalf("unexpected status %+v", status)
	}
}

func TestConsoleAndCommands(t *testing.T) {
	conn, _ := newTestConn(t)
	ctx := context.Background()

	lines, _, next, err := conn.Console(ctx, ampsim.LobbyID, -1, 500)
	if err != nil {
		t.Fatalf("Console: %v", err)
	}
	if len(lines) == 0 || next == 0 {
		t.Fatalf("expected backfill lines, got %d", len(lines))
	}

	if err := conn.SendCommand(ctx, ampsim.LobbyID, "say hello"); err != nil {
		t.Fatalf("SendCommand: %v", err)
	}
	if err := conn.SendCommand(ctx, ampsim.LobbyID, "zzznotreal"); err != nil {
		t.Fatalf("SendCommand must not fail on unknown commands: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var all []ConsoleLine
	for time.Now().Before(deadline) {
		fresh, _, n, err := conn.Console(ctx, ampsim.LobbyID, next-1, 500)
		if err != nil {
			t.Fatalf("Console poll: %v", err)
		}
		all, next = append(all, fresh...), n
		if containsLine(all, "[Server] hello") && containsLine(all, "Unknown command 'zzznotreal'") {
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatalf("echo lines never arrived; got %+v", all)
}

func containsLine(lines []ConsoleLine, want string) bool {
	for _, l := range lines {
		if strings.Contains(l.Contents, want) {
			return true
		}
	}
	return false
}

func TestSessionExpiryRelogin(t *testing.T) {
	conn, sim := newTestConn(t)
	ctx := context.Background()
	if _, err := conn.Targets(ctx); err != nil {
		t.Fatalf("first Targets: %v", err)
	}
	sim.ExpireSessions()
	if _, err := conn.Targets(ctx); err != nil {
		t.Fatalf("Targets after expiry should re-login: %v", err)
	}
	if _, err := conn.InstanceStatus(ctx, ampsim.LobbyID); err != nil {
		t.Fatalf("InstanceStatus after expiry should re-login: %v", err)
	}
}

func TestAppLifecycle(t *testing.T) {
	conn, _ := newTestConn(t)
	ctx := context.Background()

	if err := conn.AppAction(ctx, ampsim.LobbyID, "stop"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	status, err := conn.InstanceStatus(ctx, ampsim.LobbyID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := stateName(status.State); got != "stopping" && got != "stopped" {
		t.Fatalf("expected stopping/stopped, got %s", got)
	}

	time.Sleep(1700 * time.Millisecond)
	if err := conn.AppAction(ctx, ampsim.LobbyID, "start"); err != nil {
		t.Fatalf("start: %v", err)
	}
	time.Sleep(1700 * time.Millisecond)
	status, err = conn.InstanceStatus(ctx, ampsim.LobbyID)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if got := stateName(status.State); got != "running" {
		t.Fatalf("expected running after start settles, got %s", got)
	}

	if err := conn.AppAction(ctx, ampsim.LobbyID, "flomp"); err == nil {
		t.Fatal("unknown action must error")
	}
}

func TestInstanceLifecycleViaController(t *testing.T) {
	conn, _ := newTestConn(t)
	ctx := context.Background()

	if err := conn.InstanceAction(ctx, ampsim.ValheimID, "start"); err != nil {
		t.Fatalf("InstanceAction start: %v", err)
	}
	time.Sleep(1700 * time.Millisecond)
	targets, err := conn.Targets(ctx)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, inst := range targets[0].Instances {
		if inst.ID == ampsim.ValheimID && inst.State != "running" {
			t.Fatalf("valheim should be running, got %s", inst.State)
		}
	}

	if err := conn.InstanceAction(ctx, uuid.NewString(), "start"); err == nil {
		t.Fatal("unknown instance must error")
	}
}

func TestCollectSamples(t *testing.T) {
	conn, _ := newTestConn(t)
	samples, err := conn.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	var haveCPU, havePlayers bool
	for _, s := range samples {
		if s.Metric == "cpu" && strings.Contains(s.Resource, ampsim.LobbyID) {
			haveCPU = true
			if s.Value < 0 || s.Value > 1 {
				t.Fatalf("cpu fraction out of range: %f", s.Value)
			}
		}
		if s.Metric == "players" {
			havePlayers = true
		}
	}
	if !haveCPU || !havePlayers {
		t.Fatalf("expected cpu+players samples, got %+v", samples)
	}
}

func TestAmpTimeFormats(t *testing.T) {
	var ts AmpTime
	if err := ts.UnmarshalJSON([]byte(`"/Date(1723910400000)/"`)); err != nil {
		t.Fatalf(".NET date: %v", err)
	}
	if ts.Year() != 2024 {
		t.Fatalf(".NET date parsed to %v", ts.Time)
	}
	if err := ts.UnmarshalJSON([]byte(`"2026-08-17T10:00:00Z"`)); err != nil {
		t.Fatalf("ISO date: %v", err)
	}
	if err := ts.UnmarshalJSON([]byte(`"2026-08-17T10:00:00"`)); err != nil {
		t.Fatalf("zoneless ISO date: %v", err)
	}
}

func TestOfflineInstance(t *testing.T) {
	conn, _ := newTestConn(t)
	ctx := context.Background()

	targets, err := conn.Targets(ctx)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	var rust *NormInstance
	for i := range targets[0].Instances {
		if targets[0].Instances[i].ID == ampsim.RustID {
			rust = &targets[0].Instances[i]
		}
	}
	if rust == nil || rust.State != "offline" || rust.Running {
		t.Fatalf("stopped container must report offline, got %+v", rust)
	}

	_, err = conn.InstanceStatus(ctx, ampsim.RustID)
	if err == nil || !strings.Contains(err.Error(), "not running — start it first") {
		t.Fatalf("expected the not-running sentence, got %v", err)
	}
}
