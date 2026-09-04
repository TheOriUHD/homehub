package patchpilot

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"homehub/internal/store"
	"homehub/internal/vault"
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
	running map[uuid.UUID]bool               // integrationID → a run is executing
	cancels map[uuid.UUID]context.CancelFunc // runID → cancel its execution
	// Per run: the hosts an operator asked to skip, and a cancel for the host
	// whose command is in flight, so a skip can end that one host's work
	// without touching the rest of the run.
	skips       map[uuid.UUID]map[uuid.UUID]bool
	hostCancels map[uuid.UUID]map[uuid.UUID]context.CancelFunc
}

func NewRegistry(st *store.Store, v *vault.Vault, logger *slog.Logger) *Registry {
	return &Registry{
		store:       st,
		vault:       v,
		logger:      logger.With("component", "patchpilot"),
		running:     map[uuid.UUID]bool{},
		cancels:     map[uuid.UUID]context.CancelFunc{},
		skips:       map[uuid.UUID]map[uuid.UUID]bool{},
		hostCancels: map[uuid.UUID]map[uuid.UUID]context.CancelFunc{},
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

// connectHost opens a key-authenticated session as the managed account,
// verifying the host key against the pinned fingerprint. Hosts onboarded
// before pinning existed have an empty key; the first connection after upgrade
// captures and pins it (trust-on-first-use).
func (r *Registry) connectHost(ctx context.Context, h *store.PatchpilotHost) (*SSHConn, error) {
	kp, err := r.KeyPairFor(ctx, h.IntegrationID)
	if err != nil {
		return nil, err
	}
	host, port := r.Endpoint(ctx, h)
	conn, err := ConnectKey(ctx, host, port, h.SSHUser, kp.PrivatePEM, h.HostKey)
	if err != nil {
		return nil, err
	}
	if h.HostKey == "" && conn.HostKey() != "" {
		if perr := r.store.SetPatchpilotHostKey(ctx, h.ID, conn.HostKey()); perr != nil {
			r.logger.Warn("patchpilot host-key pin failed", "host", h.Hostname, "err", perr)
		} else {
			h.HostKey = conn.HostKey()
		}
	}
	return conn, nil
}

// Endpoint is where a host is dialed: the bound registry entry when there is
// one, so an address edited in the registry applies on the very next
// connection, else the snapshot taken at onboarding. A registry address that
// moved is written back into the snapshot, so lists and a later unbind show
// the current one rather than the one from onboarding day.
func (r *Registry) Endpoint(ctx context.Context, h *store.PatchpilotHost) (string, int) {
	if h.SSHHostID != nil {
		if sh, err := r.store.GetSSHHost(ctx, *h.SSHHostID); err == nil {
			if sh.Hostname != h.Hostname || sh.Port != h.SSHPort {
				if perr := r.store.SetPatchpilotHostEndpoint(ctx, h.ID, sh.Hostname, sh.Port); perr == nil {
					h.Hostname, h.SSHPort = sh.Hostname, sh.Port
				}
			}
			return sh.Hostname, sh.Port
		}
	}
	return h.Hostname, h.SSHPort
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
