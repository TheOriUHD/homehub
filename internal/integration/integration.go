// Package integration defines the contract every integration type implements
// and the manager that supervises configured instances.
//
// The platform owns instance CRUD, credential storage, health supervision,
// reconnection, status publication, and metric forwarding; a driver
// contributes only what is specific to its upstream system.
package integration

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/metrics"
)

// Status is the health state machine every instance moves through.
type Status string

const (
	StatusPending     Status = "pending"
	StatusConnecting  Status = "connecting"
	StatusConnected   Status = "connected"
	StatusDegraded    Status = "degraded"
	StatusUnreachable Status = "unreachable"
	StatusDisabled    Status = "disabled"
)

// Instance is a configured integration as the driver sees it.
type Instance struct {
	ID     uuid.UUID
	Type   string
	Name   string
	Config map[string]any
}

// ResourcePath returns the RBAC root for this instance, e.g. "proxmox/<id>".
func (i Instance) ResourcePath() string { return i.Type + "/" + i.ID.String() }

// Health is a point-in-time verdict from a connection.
type Health struct {
	Status  Status
	Message string
}

// TestResult reports a connection test with a message specific enough to fix
// the problem ("TLS certificate not trusted — enable 'skip verification' or
// pin the fingerprint", not "connection failed").
type TestResult struct {
	OK      bool           `json:"ok"`
	Message string         `json:"message"`
	Detail  map[string]any `json:"detail,omitempty"`
}

// Resource is one node of the tree an instance publishes: permission
// targets, search entries, and dashboard listings all derive from it.
type Resource struct {
	Path string // full RBAC path, e.g. "proxmox/<id>/guest/101"
	Kind string // driver-scoped kind: "node", "guest", "storage", …
	Name string // display name
}

// Deps is what the platform hands a driver when opening a connection.
type Deps struct {
	// Secret reads a named secret for this instance from the vault.
	Secret func(ctx context.Context, name string) (string, error)
	// Publish emits an event on the instance's WS topic
	// ("integration:<type>/<id>"); payloads must be JSON-marshalable.
	Publish func(event string, data any)
	// PublishScoped emits a per-child event on the instance topic but delivers
	// it only to clients with view on `resource` — the specific sub-resource
	// (e.g. "amp/<panel>/instance/<id>") the event concerns. Use for live
	// console/status/player data so a sibling-only grant can't receive another
	// child's stream. Payloads must be JSON-marshalable.
	PublishScoped func(event string, resource string, data any)
}

// Connection is a live, supervised link to the upstream system.
type Connection interface {
	// Health probes the upstream; called periodically and after actions.
	Health(ctx context.Context) Health
	// Resources enumerates the current resource tree.
	Resources(ctx context.Context) ([]Resource, error)
	// Collect gathers one round of metric samples.
	Collect(ctx context.Context) ([]metrics.Sample, error)
	Close()
}

// Driver is one integration type.
type Driver interface {
	// TypeID is the stable identifier ("proxmox") used in resource paths.
	TypeID() string
	DisplayName() string
	Description() string
	// SecretNames lists the vault entries this type stores ("token", …), so
	// the platform can manage them generically.
	SecretNames() []string
	// Test verifies connectivity with the given config and secrets without
	// creating anything.
	Test(ctx context.Context, config map[string]any, secrets map[string]string) TestResult
	// Open establishes a supervised connection.
	Open(ctx context.Context, inst Instance, deps Deps) (Connection, error)
}

// InstanceFromRow converts a store row's JSON config.
func ConfigFromJSON(raw json.RawMessage) map[string]any {
	cfg := map[string]any{}
	_ = json.Unmarshal(raw, &cfg)
	return cfg
}

// Supervision cadence.
const (
	healthInterval   = 15 * time.Second
	collectInterval  = 30 * time.Second
	reconnectMin     = 5 * time.Second
	reconnectMax     = 2 * time.Minute
	upstreamCallTime = 10 * time.Second
)
