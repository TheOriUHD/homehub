package patchpilot

import (
	"context"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
)

const TypeID = "patchpilot"

// Driver is a local-only integration (like Minecraft): there is nothing to
// dial at create time — a fleet is a container for hosts you onboard later.
type Driver struct {
	Registry *Registry
}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "PatchPilot" }
func (Driver) Description() string {
	return "Agentless update manager for your Linux fleet: onboard hosts over SSH (HomeHub installs a dedicated least-privilege account, nothing else), see pending updates, patch on demand or on a schedule, watch live logs. Never reboots on its own."
}

// No pre-create secrets — the orchestrator key is generated per fleet, and
// host login passwords are used once during onboarding and never stored.
func (Driver) SecretNames() []string { return nil }

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	return integration.TestResult{
		OK:      true,
		Message: "Ready — create the fleet, then add each host and HomeHub will set up passwordless access over SSH.",
	}
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	return &Conn{registry: d.Registry, inst: inst}, nil
}

// Conn aggregates fleet health from stored host state — no live connection is
// held open between actions.
type Conn struct {
	registry *Registry
	inst     integration.Instance
}

func (c *Conn) Health(ctx context.Context) integration.Health {
	hosts, err := c.registry.store.ListPatchpilotHosts(ctx, c.inst.ID)
	if err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: err.Error()}
	}
	if len(hosts) == 0 {
		return integration.Health{Status: integration.StatusPending, Message: "no hosts added yet"}
	}
	// A failed run is not a degraded fleet: the run signal already carries it,
	// and marking the integration degraded on top of that reported the same
	// fault twice — a second message that survived dismissing the first.
	return integration.Health{Status: integration.StatusConnected}
}

// Resources publishes the fleet plus one node per host, so hosts can be
// granted individually.
func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	hosts, err := c.registry.store.ListPatchpilotHosts(ctx, c.inst.ID)
	if err != nil {
		return nil, err
	}
	root := c.inst.ResourcePath()
	out := []integration.Resource{{Path: root, Kind: "fleet", Name: c.inst.Name}}
	for _, h := range hosts {
		out = append(out, integration.Resource{Path: root + "/host/" + h.ID.String(), Kind: "host", Name: h.Name})
	}
	return out, nil
}

// Collect emits per-host pending-update counts so history/alerts can watch
// them.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	hosts, err := c.registry.store.ListPatchpilotHosts(ctx, c.inst.ID)
	if err != nil {
		return nil, err
	}
	root := c.inst.ResourcePath()
	now := time.Now()
	var samples []metrics.Sample
	for _, h := range hosts {
		samples = append(samples, metrics.Sample{
			Resource: root + "/host/" + h.ID.String(), Metric: "updates_pending", Unit: "count",
			TS: now, Value: float64(h.UpdatesPending),
		})
	}
	return samples, nil
}

func (c *Conn) Close() {}
