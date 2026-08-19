package patchpilot

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

// DetectResult is what the wizard shows after probing a host.
type DetectResult struct {
	Reachable      bool   `json:"reachable"`
	OSPretty       string `json:"osPretty"`
	Arch           string `json:"arch"`
	Hostname       string `json:"hostname"`
	PackageManager string `json:"packageManager"`
	IsProxmox      bool   `json:"isProxmox"`
	Escalation     string `json:"escalation"`
	NeedsPassword  bool   `json:"needsPassword"` // sudo/su needs a password we don't yet have
	Refusal        string `json:"refusal,omitempty"`
	Blocker        string `json:"blocker,omitempty"` // why bootstrap can't proceed
	Message        string `json:"message"`
}

// Detect probes a host with a login credential and reports what onboarding
// would do, without changing anything. It retries transient failures (a
// dropped SSH channel, or a probe that comes back empty on a busy host) so a
// single click is reliable — a genuinely empty/non-Linux host stays empty
// across all attempts and is reported honestly.
func (r *Registry) Detect(ctx context.Context, host string, port int, cred Credential, rootPassword string) (*DetectResult, error) {
	var conn *SSHConn
	var facts *HostFacts
	var raw, lastErr string
	reachable := false

	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(400 * time.Millisecond)
		}
		c, err := ConnectPassword(ctx, host, port, cred)
		if err != nil {
			lastErr = err.Error()
			continue // connection itself failed — retry
		}
		reachable = true
		out, f, perr := Probe(ctx, c)
		if perr != nil {
			c.Close()
			lastErr = perr.Error()
			continue
		}
		raw, facts = out, f
		// A useful probe has at least an OS or a package manager. Keep the
		// connection open to verify elevation; retry on an empty result.
		if f.PackageManager != "" || f.OSPretty != "" {
			conn = c
			break
		}
		c.Close()
	}
	if conn != nil {
		defer conn.Close()
	}

	if !reachable {
		return &DetectResult{Reachable: false, Message: lastErr}, nil
	}
	if facts == nil {
		return &DetectResult{Reachable: true, Message: "connected, but the detection probe failed: " + lastErr}, nil
	}
	if facts.PackageManager == "" && facts.OSPretty == "" {
		// Log exactly what the host sent so an odd shell / PATH / banner is
		// diagnosable.
		r.logger.Warn("patchpilot detection empty", "host", host,
			"rawLen", len(raw), "raw", truncate(raw, 1200))
	}
	res := &DetectResult{
		Reachable:      true,
		OSPretty:       facts.OSPretty,
		Arch:           facts.Arch,
		Hostname:       facts.Hostname,
		PackageManager: facts.PackageManager,
		IsProxmox:      facts.IsProxmox,
	}
	if refusal := StandardRefusal(facts); refusal != "" {
		res.Refusal = refusal
		res.Message = refusal
		return res, nil
	}
	// Verify elevation actually works (sudo installed ≠ this user can sudo).
	mode, blocker := ResolveEscalation(ctx, conn, facts, cred.Password, rootPassword)
	if blocker != "" {
		res.Blocker = blocker
		res.NeedsPassword = true
		res.Message = blocker
		return res, nil
	}
	res.Escalation = string(mode)
	res.Message = "Ready to onboard via " + string(mode)
	return res, nil
}

// CommitInput names a host once bootstrap succeeds.
type CommitInput struct {
	Host         string
	Port         int
	Cred         Credential
	RootPassword string
	Name         string
	Notes        string
	AutoReboot   bool
}

// Onboard runs the full bootstrap for one host and persists it, streaming
// step events to the fleet topic. On success it returns the created host.
func (r *Registry) Onboard(ctx context.Context, integrationID uuid.UUID, in CommitInput, emit func(BootstrapStep)) (*store.PatchpilotHost, error) {
	step := func(s BootstrapStep) {
		emit(s)
		r.publish(integrationID, "bootstrap.step", s)
	}

	conn, err := ConnectPassword(ctx, in.Host, in.Port, in.Cred)
	if err != nil {
		return nil, bootErr("ssh_connection", "could not connect to the host", err.Error(), "")
	}
	defer conn.Close()

	step(BootstrapStep{Step: "detect", Status: "running", Message: "Inspecting the host"})
	facts, err := Detect(ctx, conn)
	if err != nil {
		return nil, bootErr("detection_probe", "the detection probe failed", err.Error(), "")
	}
	if refusal := StandardRefusal(facts); refusal != "" {
		return nil, bootErr("unknown", refusal, "use the matching native integration instead", "")
	}
	// ResolveEscalation verifies elevation works and leaves conn configured
	// with the mode/password that succeeded (sudo→su fallback included).
	mode, blocker := ResolveEscalation(ctx, conn, facts, in.Cred.Password, in.RootPassword)
	if blocker != "" {
		return nil, bootErr("permission_denied", blocker, "supply a login with sudo rights, or root's password", "")
	}
	_ = mode
	step(BootstrapStep{Step: "detect", Status: "ok", Message: facts.OSPretty + " · " + ManagerDisplayName(facts.PackageManager)})

	kp, err := r.KeyPairFor(ctx, integrationID)
	if err != nil {
		return nil, err
	}

	result, berr := BootstrapStandard(ctx, conn, facts, kp.PublicKey, step)
	if berr != nil {
		if be, ok := berr.(*BootstrapError); ok {
			r.logger.Warn("patchpilot bootstrap failed", "host", in.Host, "code", be.Code,
				"summary", be.Summary, "output", truncate(be.Output, 800))
			step(BootstrapStep{Step: "error", Status: "error", Message: be.Summary + " — " + be.Guidance, Code: be.Code})
		}
		return nil, berr
	}

	// Verify the key + NOPASSWD sudoers over a fresh managed-account session,
	// with a short backoff — sshd needs a moment after the account is made.
	step(BootstrapStep{Step: "verify", Status: "running", Message: "Verifying passwordless access"})
	var keyConn *SSHConn
	for i, delay := range []time.Duration{500, 1000, 1500, 2000, 3000} {
		time.Sleep(delay * time.Millisecond)
		keyConn, err = ConnectKey(ctx, in.Host, in.Port, ManagedUser, kp.PrivatePEM)
		if err == nil {
			break
		}
		if i == 4 {
			return nil, bootErr("patchpilot_reconnect_failed", "could not reconnect as the patchpilot account", err.Error(), "")
		}
	}
	defer keyConn.Close()
	if verr := VerifyManagedAccess(ctx, keyConn, facts.PackageManager, result.Paths); verr != nil {
		return nil, verr
	}
	step(BootstrapStep{Step: "verify", Status: "ok", Message: "Passwordless updates confirmed"})

	// Persist the host.
	host, err := r.store.CreatePatchpilotHost(ctx, store.PatchpilotHost{
		IntegrationID:  integrationID,
		Name:           in.Name,
		Hostname:       in.Host,
		SSHPort:        in.Port,
		SSHUser:        ManagedUser,
		HostType:       "standard",
		PackageManager: facts.PackageManager,
		OSPretty:       facts.OSPretty,
		Arch:           facts.Arch,
		IsProxmox:      facts.IsProxmox,
		Enabled:        true,
		AutoReboot:     in.AutoReboot,
		Notes:          in.Notes,
	})
	if err != nil {
		return nil, err
	}
	step(BootstrapStep{Step: "complete", Status: "ok", Message: "Host onboarded"})
	r.publish(integrationID, "hosts-changed", map[string]any{"hostId": host.ID})
	return host, nil
}

// RepairSudoers rewrites the sudoers drop-in with the current whitelist, over
// the managed account (which can already sudo the package manager). Useful
// after a package-manager path change.
func (r *Registry) RepairSudoers(ctx context.Context, h *store.PatchpilotHost) error {
	conn, err := r.connectHost(ctx, h)
	if err != nil {
		return err
	}
	defer conn.Close()
	paths := r.resolvedPaths(ctx, conn, h.PackageManager)
	body := SudoersContent(ManagedUser, h.PackageManager, paths)
	// Write via a temp file + validate + move, all through sudo.
	cmd := "printf '%s' " + shellSingleQuote(body) + " | sudo -n tee /etc/sudoers.d/" + ManagedUser + " >/dev/null && " +
		"sudo -n chmod 0440 /etc/sudoers.d/" + ManagedUser + " && sudo -n " + path(paths, "visudo") + " -cf /etc/sudoers.d/" + ManagedUser
	out, code, err := conn.Run(ctx, cmd)
	if err != nil || code != 0 {
		return bootErr("sudoers_write_failed", "could not repair the sudoers rule", out, out)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
