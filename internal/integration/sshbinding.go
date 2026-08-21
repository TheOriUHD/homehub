package integration

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
	"github.com/TheOriUHD/homehub/internal/vault"
)

// SSHHostIDKey is the config field an instance sets to dial a host from the
// central SSH registry instead of credentials typed into its own form.
const SSHHostIDKey = "sshHostId"

// SSHSecretNames are the per-driver names for "the SSH secret". All of them
// resolve to the bound registry host's secret, so a driver keeps asking for
// the name it always asked for.
var SSHSecretNames = map[string]bool{
	"sshSecret":   true, // docker
	"sshPassword": true,
	"sshKey":      true,
}

// SSHBinding is a resolved registry host, ready to be written into the plain
// config keys drivers read.
type SSHBinding struct {
	Hostname string
	Port     int
	Username string
	AuthType string
	Secret   string
}

// ResolveSSHBinding looks up the registry host a config points at. ok is false
// when the config has no binding at all, which is not an error — that instance
// simply uses its own inline fields.
func ResolveSSHBinding(
	ctx context.Context,
	st *store.Store,
	v *vault.Vault,
	config map[string]any,
) (SSHBinding, bool, error) {
	raw, _ := config[SSHHostIDKey].(string)
	if raw == "" {
		return SSHBinding{}, false, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return SSHBinding{}, true, errors.New("the SSH host binding is malformed — pick a host again")
	}
	h, err := st.GetSSHHost(ctx, id)
	if err != nil {
		return SSHBinding{}, true, errors.New("the SSH host this system points at no longer exists — pick another")
	}
	secret, err := v.Open(ctx, store.SSHHostVaultScope, h.ID.String())
	if err != nil {
		return SSHBinding{}, true, errors.New("the stored secret for SSH host " + h.Name + " is unreadable — re-enter it")
	}
	return SSHBinding{
		Hostname: h.Hostname, Port: h.Port, Username: h.Username,
		AuthType: h.AuthType, Secret: string(secret),
	}, true, nil
}

// ApplySSHBinding writes a resolved host into the generic config keys, on a
// copy: the caller's map may be the shared instance config.
func ApplySSHBinding(config map[string]any, b SSHBinding) map[string]any {
	out := make(map[string]any, len(config)+4)
	for k, v := range config {
		out[k] = v
	}
	out["host"] = b.Hostname
	// Ports arrive from JSON as float64 everywhere else, and driver config
	// parsers type-assert for exactly that — match it.
	out["port"] = float64(b.Port)
	out["username"] = b.Username
	out["authType"] = b.AuthType
	return out
}
