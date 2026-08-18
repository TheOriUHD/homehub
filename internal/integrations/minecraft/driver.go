package minecraft

import (
	"context"
	"fmt"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

const TypeID = "minecraft"

// Driver adapts the inbound companion model to the integration platform.
// One instance is a NETWORK — a proxy and its subservers (or any grouping
// the operator likes); health aggregates over the member servers, and the
// optional SSH credential gives the network host-level reach for full
// restarts.
type Driver struct {
	Registry *Registry
}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Minecraft" }
func (Driver) Description() string {
	return "A Minecraft network: proxies and subservers under one roof via the HomeHub companion plugin — live consoles with full scrollback, commands, players, TPS/memory history, and optional SSH host access. Paper, Spigot, Velocity, Fabric, and Forge."
}

// SecretNames: the agent tokens are created during pairing; the SSH
// credential is the administrator's.
func (Driver) SecretNames() []string { return []string{"sshPassword", "sshKey"} }

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	return integration.TestResult{
		OK: true,
		Message: "Ready — create the network, then install the companion plugin on each server " +
			"and pair every one with its own 6-digit code from the network page.",
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
	servers, err := c.registry.store.ListMinecraftServers(ctx, c.inst.ID)
	if err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: err.Error()}
	}
	if len(servers) == 0 {
		// No members yet: waiting for setup, not suffering an outage.
		return integration.Health{
			Status:  integration.StatusPending,
			Message: "no servers paired yet — install the companion plugin and pair each server",
		}
	}
	connected := 0
	for _, server := range servers {
		if _, ok := c.registry.Session(server.ID); ok {
			connected++
		}
	}
	switch {
	case connected == len(servers):
		return integration.Health{Status: integration.StatusConnected}
	case connected > 0:
		return integration.Health{
			Status:  integration.StatusDegraded,
			Message: fmt.Sprintf("%d of %d servers connected", connected, len(servers)),
		}
	default:
		return integration.Health{
			Status:  integration.StatusUnreachable,
			Message: "no companions connected — they reconnect automatically when the servers are up",
		}
	}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	out := []integration.Resource{
		{Path: c.inst.ResourcePath(), Kind: "network", Name: c.inst.Name},
	}
	servers, err := c.registry.store.ListMinecraftServers(ctx, c.inst.ID)
	if err != nil {
		return out, nil
	}
	for _, server := range servers {
		name := server.Label
		if name == "" {
			name = server.ServerName
		}
		out = append(out, integration.Resource{
			Path: c.inst.ResourcePath() + "/server/" + server.ID.String(),
			Kind: "server",
			Name: name,
		})
	}
	return out, nil
}

// Collect samples every connected member's latest snapshot, one resource
// per server.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	now := time.Now()
	var samples []metrics.Sample
	for _, sess := range c.registry.NetworkSessions(c.inst.ID) {
		st := sess.Status()
		if st == nil {
			continue
		}
		res := c.inst.ResourcePath() + "/server/" + sess.ServerID.String()
		samples = append(samples,
			metrics.Sample{Resource: res, Metric: "players_online", Unit: "count", TS: now, Value: float64(st.PlayersOnline)},
			metrics.Sample{Resource: res, Metric: "heap_used_mb", Unit: "MB", TS: now, Value: st.HeapUsedMB},
		)
		if st.TPS > 0 {
			samples = append(samples,
				metrics.Sample{Resource: res, Metric: "tps", Unit: "count", TS: now, Value: st.TPS},
				metrics.Sample{Resource: res, Metric: "mspt", Unit: "ms", TS: now, Value: st.MSPT},
			)
		}
	}
	return samples, nil
}

// Close is a no-op: the companions own their sockets, and the registry
// keeps them across driver reopen cycles.
func (c *Conn) Close() {}
