package patchpilot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// vaultScope namespaces a fleet's secrets (the orchestrator private key).
func vaultScope(integrationID uuid.UUID) string {
	return "patchpilot:" + integrationID.String()
}

const keySecretName = "orchestratorKey"

// Enqueuer is the subset of the jobs engine the registry needs.
type Enqueuer interface {
	Enqueue(ctx context.Context, kind string, payload any) (*store.Job, error)
}

// Registry owns PatchPilot's live state: the per-fleet SSH identity, run
// admission, and the event fan-out. The driver's Connection reads aggregate
// health from it; the API layer drives onboarding and runs through it.
type Registry struct {
	store  *store.Store
	vault  *vault.Vault
	logger *slog.Logger
	jobs   Enqueuer

	// Publish emits an event on the fleet's WS topic (wired to the hub in
	// app wiring, same {event,data} envelope the manager uses).
	Publish func(integrationID uuid.UUID, event string, data any)

	mu      sync.Mutex
	running map[uuid.UUID]bool // integrationID → a run is executing
}

func NewRegistry(st *store.Store, v *vault.Vault, logger *slog.Logger) *Registry {
	return &Registry{
		store:   st,
		vault:   v,
		logger:  logger.With("component", "patchpilot"),
		running: map[uuid.UUID]bool{},
	}
}

func (r *Registry) SetJobs(e Enqueuer) { r.jobs = e }

func (r *Registry) publish(integrationID uuid.UUID, event string, data any) {
	if r.Publish != nil {
		r.Publish(integrationID, event, data)
	}
}

// KeyPairFor returns the fleet's orchestrator keypair, generating and sealing
// one on first use. Idempotent.
func (r *Registry) KeyPairFor(ctx context.Context, integrationID uuid.UUID) (*KeyPair, error) {
	scope := vaultScope(integrationID)
	if raw, err := r.vault.Open(ctx, scope, keySecretName); err == nil && len(raw) > 0 {
		pub, perr := PublicKeyFromPrivate(string(raw))
		if perr != nil {
			return nil, perr
		}
		return &KeyPair{PrivatePEM: string(raw), PublicKey: pub}, nil
	}
	kp, err := GenerateKeyPair()
	if err != nil {
		return nil, err
	}
	if err := r.vault.Seal(ctx, scope, keySecretName, []byte(kp.PrivatePEM)); err != nil {
		return nil, fmt.Errorf("sealing orchestrator key: %w", err)
	}
	return kp, nil
}

// PublicKey returns just the authorized_keys line for a fleet (for the UI /
// manual key installs).
func (r *Registry) PublicKey(ctx context.Context, integrationID uuid.UUID) (string, error) {
	kp, err := r.KeyPairFor(ctx, integrationID)
	if err != nil {
		return "", err
	}
	return kp.PublicKey, nil
}

// connectHost opens a key-authenticated session as the managed account.
func (r *Registry) connectHost(ctx context.Context, h *store.PatchpilotHost) (*SSHConn, error) {
	kp, err := r.KeyPairFor(ctx, h.IntegrationID)
	if err != nil {
		return nil, err
	}
	return ConnectKey(ctx, h.Hostname, h.SSHPort, h.SSHUser, kp.PrivatePEM)
}

// resolvedPaths re-resolves absolute command paths for a host at run time —
// the target may have changed since onboarding.
func (r *Registry) resolvedPaths(ctx context.Context, conn *SSHConn, mgr string) map[string]string {
	paths, err := resolvePaths(ctx, conn, mgr)
	if err != nil {
		return map[string]string{}
	}
	return paths
}
