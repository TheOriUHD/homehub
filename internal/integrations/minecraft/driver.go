package minecraft

import (
	"context"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

const TypeID = "minecraft"

// Driver adapts the inbound companion model to the integration platform:
// the "connection" is a handle onto the registry, and health reflects
// whether the companion is currently connected.
type Driver struct {
	Registry *Registry
}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Minecraft" }
func (Driver) Description() string {
	return "Minecraft servers via the HomeHub companion plugin: live console with full scrollback, command execution, players, and TPS/memory history. Paper, Spigot, Velocity, Fabric, and Forge."
}

// SecretNames is empty: the agent token is created during pairing, not
// entered by the administrator.
func (Driver) SecretNames() []string { return nil }

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	return integration.TestResult{
		OK: true,
		Message: "Ready — create the instance, then install the companion plugin on the server " +
			"and pair it with the 6-digit code from the instance page.",
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	return &Conn{registry: d.Registry, inst: inst}, nil
}

type Conn struct {
	registry *Registry
	inst     integration.Instance
}

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, ok := c.registry.Session(c.inst.ID); ok {
		return integration.Health{Status: integration.StatusConnected}
	}
	// Never paired: this instance is waiting for setup, not suffering an
	// outage — pending, so it neither alarms nor alerts.
	if _, err := c.registry.store.GetMinecraftServer(ctx, c.inst.ID); err != nil {
		return integration.Health{
			Status:  integration.StatusPending,
			Message: "waiting for the companion plugin — install it on the server and pair with the 6-digit code",
		}
	}
	return integration.Health{
		Status:  integration.StatusUnreachable,
		Message: "companion not connected — it reconnects automatically when the server is up",
	}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	name := c.inst.Name
	if s, ok := c.registry.Session(c.inst.ID); ok && s.ServerName != "" {
		name = s.ServerName
	}
	return []integration.Resource{
		{Path: c.inst.ResourcePath(), Kind: "server", Name: name},
	}, nil
}

// Collect samples the latest performance snapshot into the metrics engine.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	s, ok := c.registry.Session(c.inst.ID)
	if !ok {
		return nil, nil
	}
	st := s.Status()
	if st == nil {
		return nil, nil
	}
	now := time.Now()
	res := c.inst.ResourcePath()
	samples := []metrics.Sample{
		{Resource: res, Metric: "players_online", Unit: "count", TS: now, Value: float64(st.PlayersOnline)},
		{Resource: res, Metric: "heap_used_mb", Unit: "MB", TS: now, Value: st.HeapUsedMB},
	}
	if st.TPS > 0 {
		samples = append(samples,
			metrics.Sample{Resource: res, Metric: "tps", Unit: "count", TS: now, Value: st.TPS},
			metrics.Sample{Resource: res, Metric: "mspt", Unit: "ms", TS: now, Value: st.MSPT},
		)
	}
	return samples, nil
}

// Close is a no-op: the companion owns the socket, and the registry keeps
// it across driver reopen cycles.
func (c *Conn) Close() {}
