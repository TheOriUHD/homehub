package integration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"homehub/internal/metrics"
	"homehub/internal/store"
	"homehub/internal/vault"
	"homehub/internal/ws"
)

// Manager supervises every configured instance: connect with backoff, watch
// health, collect metrics, and publish status transitions. Instances start,
// stop, and reload at runtime — nothing here ever requires an app restart.
type Manager struct {
	store   *store.Store
	vault   *vault.Vault
	metrics *metrics.Service
	hub     *ws.Hub
	logger  *slog.Logger

	// StatusChanged, when set, is called on every status edge (for
	// notifications). Wired by the app to avoid a package cycle.
	StatusChanged func(inst Instance, from, to Status, message string)

	mu      sync.Mutex
	drivers map[string]Driver
	runners map[uuid.UUID]*runner
}

type runner struct {
	cancel context.CancelFunc
	done   chan struct{}
	// kick wakes the runner out of a reconnect backoff or straight into a
	// health check — used when an inbound upstream (companion-style) binds
	// or drops, so status follows within a second instead of a poll cycle.
	kick chan struct{}

	mu   sync.Mutex
	conn Connection
	last Health
}

func NewManager(st *store.Store, v *vault.Vault, m *metrics.Service, hub *ws.Hub, logger *slog.Logger) *Manager {
	return &Manager{
		store:   st,
		vault:   v,
		metrics: m,
		hub:     hub,
		logger:  logger.With("component", "integrations"),
		drivers: map[string]Driver{},
		runners: map[uuid.UUID]*runner{},
	}
}

// Register adds a driver; called once per type at startup.
func (m *Manager) Register(d Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[d.TypeID()] = d
}

// Drivers lists registered types for the "add integration" UI.
func (m *Manager) Drivers() []Driver {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Driver, 0, len(m.drivers))
	for _, d := range m.drivers {
		out = append(out, d)
	}
	return out
}

func (m *Manager) Driver(typeID string) (Driver, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.drivers[typeID]
	return d, ok
}

// Start launches runners for every enabled instance.
func (m *Manager) Start(ctx context.Context) error {
	rows, err := m.store.ListIntegrations(ctx)
	if err != nil {
		return fmt.Errorf("load integrations: %w", err)
	}
	for _, row := range rows {
		if row.Enabled {
			m.launch(instanceFromRow(row))
		}
	}
	return nil
}

// Apply starts or restarts the runner for an instance after create/update,
// or stops it when disabled.
func (m *Manager) Apply(ctx context.Context, id uuid.UUID) {
	m.stop(id)
	row, err := m.store.GetIntegration(ctx, id)
	if err != nil {
		return
	}
	if !row.Enabled {
		if _, err := m.store.SetIntegrationStatus(ctx, id, string(StatusDisabled), ""); err == nil {
			m.publishStatus(instanceFromRow(*row), StatusDisabled, "")
		}
		return
	}
	m.launch(instanceFromRow(*row))
}

// Remove stops the runner and deletes the instance's secrets.
func (m *Manager) Remove(ctx context.Context, id uuid.UUID, typ string) {
	m.stop(id)
	if err := m.vault.DeleteScope(ctx, "integration:"+id.String()); err != nil {
		m.logger.Error("delete integration secrets", "integration", id, "error", err)
	}
}

// Connection returns the live typed connection for an instance, or an error
// explaining the current state — handlers surface this directly.
func (m *Manager) Connection(id uuid.UUID) (Connection, error) {
	m.mu.Lock()
	r, ok := m.runners[id]
	m.mu.Unlock()
	if !ok {
		return nil, errors.New("integration is disabled or not configured")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conn == nil {
		msg := r.last.Message
		if msg == "" {
			msg = "still connecting"
		}
		return nil, fmt.Errorf("integration unreachable: %s", msg)
	}
	return r.conn, nil
}

// Kick nudges an instance's runner to re-evaluate immediately — out of a
// reconnect backoff or into an instant health check. Safe on unknown ids.
func (m *Manager) Kick(id uuid.UUID) {
	m.mu.Lock()
	r, ok := m.runners[id]
	m.mu.Unlock()
	if !ok {
		return
	}
	select {
	case r.kick <- struct{}{}:
	default:
	}
}

// Stop terminates all runners (app shutdown).
func (m *Manager) Stop() {
	m.mu.Lock()
	ids := make([]uuid.UUID, 0, len(m.runners))
	for id := range m.runners {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		m.stop(id)
	}
}

func (m *Manager) stop(id uuid.UUID) {
	m.mu.Lock()
	r, ok := m.runners[id]
	if ok {
		delete(m.runners, id)
	}
	m.mu.Unlock()
	if ok {
		r.cancel()
		<-r.done
	}
}

func (m *Manager) launch(inst Instance) {
	m.mu.Lock()
	d, ok := m.drivers[inst.Type]
	if !ok {
		m.mu.Unlock()
		m.logger.Error("no driver for integration type", "type", inst.Type, "integration", inst.ID)
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r := &runner{cancel: cancel, done: make(chan struct{}), kick: make(chan struct{}, 1)}
	m.runners[inst.ID] = r
	m.mu.Unlock()

	go m.run(ctx, r, d, inst)
}

// run is the supervision loop for one instance.
func (m *Manager) run(ctx context.Context, r *runner, d Driver, inst Instance) {
	defer close(r.done)
	logger := m.logger.With("integration", inst.Name, "type", inst.Type)
	backoff := reconnectMin

	deps := Deps{
		Secret: func(ctx context.Context, name string) (string, error) {
			raw, err := m.vault.Open(ctx, "integration:"+inst.ID.String(), name)
			if err != nil {
				return "", err
			}
			return string(raw), nil
		},
		Publish: func(event string, data any) {
			// Topic carries the resource path ("integration:<type>/<id>") so
			// subscription authorization maps straight onto RBAC visibility.
			m.hub.Publish("integration:"+inst.ResourcePath(), map[string]any{"event": event, "data": data})
		},
	}

	for ctx.Err() == nil {
		m.transition(ctx, r, inst, StatusConnecting, "")

		openCtx, cancel := context.WithTimeout(ctx, upstreamCallTime)
		conn, err := d.Open(openCtx, inst, deps)
		cancel()
		if err != nil {
			m.transition(ctx, r, inst, StatusUnreachable, err.Error())
			logger.Warn("connect failed", "error", err, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			case <-r.kick:
			}
			backoff = min(backoff*2, reconnectMax)
			continue
		}
		// Open built a connection, but only its own health verdict decides the
		// status: inbound drivers (companion-style) open successfully while the
		// upstream still has to call in, and report pending/unreachable here.
		hctx, hcancel := context.WithTimeout(ctx, upstreamCallTime)
		h := conn.Health(hctx)
		hcancel()
		if h.Status == StatusUnreachable {
			conn.Close()
			m.transition(ctx, r, inst, StatusUnreachable, h.Message)
			logger.Warn("unhealthy after connect", "message", h.Message, "retry_in", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			case <-r.kick:
			}
			backoff = min(backoff*2, reconnectMax)
			continue
		}
		backoff = reconnectMin

		r.mu.Lock()
		r.conn = conn
		r.mu.Unlock()
		m.transition(ctx, r, inst, h.Status, h.Message)
		logger.Info("connected", "status", string(h.Status))

		m.watch(ctx, r, inst, conn)

		r.mu.Lock()
		r.conn = nil
		r.mu.Unlock()
		conn.Close()
	}
}

// watch runs health and collection ticks until the connection goes
// unreachable or ctx ends.
func (m *Manager) watch(ctx context.Context, r *runner, inst Instance, conn Connection) {
	healthTick := time.NewTicker(healthInterval)
	collectTick := time.NewTicker(collectInterval)
	defer healthTick.Stop()
	defer collectTick.Stop()

	m.collect(ctx, inst, conn)

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.kick:
			// An inbound upstream just bound or dropped — re-check now.
			hctx, cancel := context.WithTimeout(ctx, upstreamCallTime)
			h := conn.Health(hctx)
			cancel()
			switch h.Status {
			case StatusUnreachable:
				m.transition(ctx, r, inst, StatusUnreachable, h.Message)
				return // reconnect from scratch
			default:
				m.transition(ctx, r, inst, h.Status, h.Message)
			}
		case <-healthTick.C:
			hctx, cancel := context.WithTimeout(ctx, upstreamCallTime)
			h := conn.Health(hctx)
			cancel()
			switch h.Status {
			case StatusUnreachable:
				m.transition(ctx, r, inst, StatusUnreachable, h.Message)
				return // reconnect from scratch
			default:
				m.transition(ctx, r, inst, h.Status, h.Message)
			}
		case <-collectTick.C:
			m.collect(ctx, inst, conn)
		}
	}
}

func (m *Manager) collect(ctx context.Context, inst Instance, conn Connection) {
	cctx, cancel := context.WithTimeout(ctx, upstreamCallTime)
	samples, err := conn.Collect(cctx)
	cancel()
	if err != nil {
		m.logger.Warn("metric collection failed", "integration", inst.Name, "error", err)
		return
	}
	if len(samples) == 0 {
		return
	}
	if err := m.metrics.Record(ctx, samples); err != nil {
		m.logger.Error("record metrics", "integration", inst.Name, "error", err)
	}
	// Live graphs ride the instance topic; clients filter by resource.
	m.hub.Publish("metrics:"+inst.Type+"/"+inst.ID.String(), metrics.WirePayload(samples))
}

// transition persists and publishes a status change; no-ops when unchanged.
func (m *Manager) transition(ctx context.Context, r *runner, inst Instance, to Status, message string) {
	r.mu.Lock()
	from := r.last.Status
	if from == to && r.last.Message == message {
		r.mu.Unlock()
		return
	}
	r.last = Health{Status: to, Message: message}
	r.mu.Unlock()

	changed, err := m.store.SetIntegrationStatus(ctx, inst.ID, string(to), message)
	if err != nil && ctx.Err() == nil {
		m.logger.Error("persist status", "integration", inst.Name, "error", err)
	}
	if changed {
		m.publishStatus(inst, to, message)
		if m.StatusChanged != nil && from != "" {
			m.StatusChanged(inst, from, to, message)
		}
	}
}

func (m *Manager) publishStatus(inst Instance, status Status, message string) {
	m.hub.Publish("integrations", map[string]any{
		"id":      inst.ID,
		"type":    inst.Type,
		"name":    inst.Name,
		"status":  status,
		"message": message,
	})
}

func instanceFromRow(row store.Integration) Instance {
	return Instance{
		ID:     row.ID,
		Type:   row.Type,
		Name:   row.Name,
		Config: ConfigFromJSON(row.Config),
	}
}
