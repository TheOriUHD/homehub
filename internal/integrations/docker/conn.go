package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	dockerclient "github.com/docker/docker/client"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/metrics"
)

// Conn is a live Docker host connection. API handlers type-assert to reach
// the SDK client.
type Conn struct {
	Client *dockerclient.Client
	inst   integration.Instance
	deps   integration.Deps
	stop   chan struct{}

	mu       sync.Mutex
	lastSeen map[string]string // container id → state, for change detection fallback
}

func (c *Conn) Close() {
	close(c.stop)
	_ = c.Client.Close()
}

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, err := c.Client.Ping(ctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: FriendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

// ResourceName returns the stable RBAC segment for a container: its name
// (stable across recreations), falling back to the short id.
func ResourceName(names []string, id string) string {
	if len(names) > 0 {
		return strings.TrimPrefix(names[0], "/")
	}
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	list, err := c.Client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return nil, err
	}
	base := c.inst.ResourcePath()
	out := []integration.Resource{{Path: base, Kind: "instance", Name: c.inst.Name}}
	for _, item := range list {
		name := ResourceName(item.Names, item.ID)
		out = append(out, integration.Resource{
			Path: base + "/container/" + name,
			Kind: "container",
			Name: name,
		})
	}
	return out, nil
}

// Collect samples per-container CPU and memory. Each running container costs
// one stats call (the daemon samples twice, ~1s apart); containers are
// sampled concurrently to keep the tick fast.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	list, err := c.Client.ContainerList(ctx, container.ListOptions{})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	base := c.inst.ResourcePath()

	var mu sync.Mutex
	samples := make([]metrics.Sample, 0, len(list)*3)
	var wg sync.WaitGroup
	for _, item := range list {
		wg.Add(1)
		go func(item types.Container) {
			defer wg.Done()
			stats, err := c.Client.ContainerStats(ctx, item.ID, false)
			if err != nil {
				return
			}
			defer stats.Body.Close()
			var payload types.StatsJSON
			if json.NewDecoder(stats.Body).Decode(&payload) != nil {
				return
			}
			cpuDelta := float64(payload.CPUStats.CPUUsage.TotalUsage - payload.PreCPUStats.CPUUsage.TotalUsage)
			sysDelta := float64(payload.CPUStats.SystemUsage - payload.PreCPUStats.SystemUsage)
			cpu := 0.0
			if sysDelta > 0 && cpuDelta >= 0 {
				cpu = cpuDelta / sysDelta
			}
			memUsed := float64(payload.MemoryStats.Usage)
			memLimit := float64(payload.MemoryStats.Limit)

			resource := base + "/container/" + ResourceName(item.Names, item.ID)
			mu.Lock()
			samples = append(samples,
				metrics.Sample{Resource: resource, Metric: "cpu", Unit: "fraction", TS: now, Value: cpu},
				metrics.Sample{Resource: resource, Metric: "mem_used", Unit: "bytes", TS: now, Value: memUsed},
				metrics.Sample{Resource: resource, Metric: "mem_total", Unit: "bytes", TS: now, Value: memLimit},
			)
			mu.Unlock()
		}(item)
	}
	wg.Wait()
	return samples, nil
}

// watchEvents streams daemon events and publishes debounced change
// notifications so open UIs refresh instantly.
func (c *Conn) watchEvents() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-c.stop
		cancel()
	}()

	debounce := time.NewTimer(time.Hour)
	debounce.Stop()
	pending := false

	for ctx.Err() == nil {
		msgs, errs := c.Client.Events(ctx, types.EventsOptions{
			Filters: filters.NewArgs(
				filters.Arg("type", "container"),
				filters.Arg("type", "image"),
			),
		})
	stream:
		for {
			select {
			case <-ctx.Done():
				return
			case <-msgs:
				if !pending {
					pending = true
					debounce.Reset(400 * time.Millisecond)
				}
			case <-debounce.C:
				pending = false
				c.deps.Publish("containers-changed", nil)
			case <-errs:
				break stream
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// Locate resolves a container reference (RBAC name or id) to its full id.
func (c *Conn) Locate(ctx context.Context, ref string) (types.Container, error) {
	list, err := c.Client.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		return types.Container{}, err
	}
	for _, item := range list {
		if ResourceName(item.Names, item.ID) == ref || item.ID == ref || strings.HasPrefix(item.ID, ref) {
			return item, nil
		}
	}
	return types.Container{}, fmt.Errorf("container %q not found on this host", ref)
}
