package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"homehub/internal/sshguard"
)

// SSH tools. The assistant can look at the SSH registry and run commands on
// those machines, under three layers of protection:
//
//  1. Permission — the caller needs the shell level (configure) on the host (or the registry) for
//     anything to run at all; listing needs view.
//  2. Classification — sshguard decides read-only / needs-confirmation /
//     refused. Read-only commands run straight away; anything that mutates
//     (or that the classifier doesn't recognise) becomes a PendingAction the
//     user must approve; catastrophic commands are refused outright.
//  3. Audit — every executed command is logged by the API layer.
//
// The classifier fails closed, so an unrecognised command asks rather than
// assumes.

// SSHHostRef is one registry entry as the model sees it.
type SSHHostRef struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	Username string `json:"username"`
	Notes    string `json:"notes,omitempty"`
}

// SSHRunResult is the outcome of a read-only command.
type SSHRunResult struct {
	Host     string `json:"host"`
	Command  string `json:"command"`
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
}

func (tb *Toolbox) sshTools() []tool {
	return []tool{
		{
			def: ToolDef{
				Name:        "list_ssh_hosts",
				Description: "The SSH machines HomeHub can reach (the central SSH registry): name, address, and login user. Use the name with ssh_run.",
				InputSchema: obj(``),
			},
			run: tb.listSSHHosts,
		},
		{
			def: ToolDef{
				Name:        "ssh_run",
				Description: "Run a shell command on one of the SSH hosts (see list_ssh_hosts). Read-only commands (ls, cat, df, systemctl status, journalctl, docker ps …) run immediately and return their output. Anything that changes the system is NOT executed — it is queued for the user to confirm, and you must tell them what you proposed. Destructive commands (wiping disks, rm -rf /, formatting) are refused outright: tell the user to do it themselves in a real terminal. Never chain a mutating command onto a read-only one to get around this.",
				InputSchema: obj(`"host":{"type":"string","description":"Host name from list_ssh_hosts"},"command":{"type":"string","description":"The shell command to run"}`, "host", "command"),
			},
			run: tb.sshRun,
		},
		{
			def: ToolDef{
				Name:        "ssh_check_host_key",
				Description: "Compare a machine's live SSH host key against the one HomeHub pinned. Use this when an SSH connection fails with \"host key changed\" — it says whether the machine is presenting a different key, and what both fingerprints are. Reads the key without logging in, so it works even when connections are being refused.",
				InputSchema: obj(`"host":{"type":"string","description":"Host name from list_ssh_hosts, or a plain address"},"port":{"type":"integer","description":"SSH port, default 22"}`, "host"),
			},
			run: tb.sshCheckHostKey,
		},
		{
			def: ToolDef{
				Name:        "ssh_trust_host_key",
				Description: "Accept the host key a machine is presenting now, restoring connections to it. Only for a key that changed for a reason the user knows about — a rebuild, a reinstall, or the address now pointing at a different machine. NOT executed on your say-so: it is queued for the user to approve, and you must tell them both fingerprints and that a key can also change because something is intercepting the connection. Always run ssh_check_host_key first and quote what it found.",
				InputSchema: obj(`"host":{"type":"string","description":"Host name from list_ssh_hosts, or a plain address"},"port":{"type":"integer","description":"SSH port, default 22"}`, "host"),
			},
			run: tb.sshTrustHostKey,
		},
	}
}

// resolveSSHAddress turns what the model said into an address. It accepts a
// registry entry name, because that is what the model has been shown, and
// falls back to treating the string as the address itself — a Docker daemon
// or Minecraft host has a pinned key without having a registry entry.
func (tb *Toolbox) resolveSSHAddress(ctx context.Context, name string, port int) (string, int, string) {
	if host, err := tb.Store.GetSSHHostByName(ctx, name); err == nil {
		return host.Hostname, host.Port, host.Name
	}
	if port == 0 {
		port = 22
	}
	return name, port, name
}

func (tb *Toolbox) sshCheckHostKey(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var p struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Host)
	if name == "" {
		return nil, fmt.Errorf("which host?")
	}
	if !access.Can("system/ssh", "view") {
		return nil, fmt.Errorf("you do not have permission to look at pinned host keys")
	}
	if tb.Call == nil {
		return nil, fmt.Errorf("host keys are not reachable from the assistant here")
	}
	host, port, label := tb.resolveSSHAddress(ctx, name, p.Port)
	status, resp := tb.Call(ctx, "POST", "/ssh-known-hosts/check", map[string]any{"host": host, "port": port})
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("could not check that host key: %s", string(resp))
	}
	var out map[string]any
	if err := json.Unmarshal(resp, &out); err != nil {
		return nil, err
	}
	out["name"] = label
	return out, nil
}

func (tb *Toolbox) sshTrustHostKey(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var p struct {
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Host)
	if name == "" {
		return nil, fmt.Errorf("which host?")
	}
	// Trusting a key is a configuration decision, and the approval below does
	// not grant permission — the API re-checks this when the action runs.
	if !access.Can("system/ssh", "configure") {
		return nil, fmt.Errorf("you do not have permission to change pinned host keys")
	}
	if tb.Call == nil {
		return nil, fmt.Errorf("host keys are not reachable from the assistant here")
	}
	host, port, label := tb.resolveSSHAddress(ctx, name, p.Port)

	// Read the live key first, so the proposal names the exact fingerprint
	// being accepted rather than "whatever answers when you say yes".
	status, resp := tb.Call(ctx, "POST", "/ssh-known-hosts/check", map[string]any{"host": host, "port": port})
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("could not read that host key: %s", string(resp))
	}
	var check struct {
		Status   string `json:"status"`
		Pinned   string `json:"pinned"`
		Observed string `json:"observed"`
		Message  string `json:"message"`
	}
	if err := json.Unmarshal(resp, &check); err != nil {
		return nil, err
	}
	switch check.Status {
	case "unreachable":
		return nil, fmt.Errorf("%s is not answering on port %d, so there is no key to accept: %s", label, port, check.Message)
	case "match":
		return map[string]any{
			"host": label, "status": "match", "pinned": check.Pinned,
			"note": "Nothing to do — the pinned key still matches, so whatever is failing is not the host key.",
		}, nil
	}

	raw, _ := json.Marshal(map[string]any{
		"host": host, "port": port, "name": label,
		"fingerprint": check.Observed, "previous": check.Pinned,
	})
	summary := fmt.Sprintf("Trust the new SSH host key for %s (%s:%d): %s", label, host, port, check.Observed)
	if check.Pinned != "" {
		summary += fmt.Sprintf(", replacing %s", check.Pinned)
	}
	return &PendingAction{Kind: "ssh.trust_host_key", Summary: summary, Params: raw}, nil
}

func (tb *Toolbox) listSSHHosts(ctx context.Context, access Access, _ json.RawMessage) (any, error) {
	rows, err := tb.Store.ListSSHHosts(ctx)
	if err != nil {
		return nil, err
	}
	out := []SSHHostRef{}
	for _, h := range rows {
		if !access.Can("system/ssh", "view") && !access.Can("ssh/"+h.ID.String(), "view") {
			continue
		}
		out = append(out, SSHHostRef{
			ID: h.ID.String(), Name: h.Name, Hostname: h.Hostname,
			Username: h.Username, Notes: h.Notes,
		})
	}
	if len(out) == 0 {
		return "No SSH hosts are configured (or you cannot see any). They are managed under Systems → SSH Hosts.", nil
	}
	return out, nil
}

func (tb *Toolbox) sshRun(ctx context.Context, access Access, input json.RawMessage) (any, error) {
	var p struct {
		Host    string `json:"host"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(p.Host)
	command := strings.TrimSpace(p.Command)
	if name == "" || command == "" {
		return nil, fmt.Errorf("both host and command are required")
	}
	if tb.SSHRun == nil {
		return nil, fmt.Errorf("SSH is not available in this context")
	}
	host, err := tb.Store.GetSSHHostByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("no SSH host named %q — use list_ssh_hosts to see the available ones", name)
	}
	hostRes := "ssh/" + host.ID.String()
	// The same bar as a terminal: a command is a shell, one line at a time.
	if !access.Can("system/ssh", "configure") && !access.Can(hostRes, "configure") {
		return nil, fmt.Errorf("you do not have permission to run commands on %s", host.Name)
	}

	switch v := sshguard.Classify(command); v.Risk {
	case sshguard.RiskBlocked:
		// Refused at every level — the operator can still do it by hand.
		return nil, fmt.Errorf("refused: that command %s and is never run by the assistant. If you really intend it, do it in a terminal yourself", v.Reason)
	case sshguard.RiskConfirm:
		raw, _ := json.Marshal(map[string]any{"hostId": host.ID.String(), "hostName": host.Name, "command": command})
		return &PendingAction{
			Kind:    "ssh.run",
			Summary: fmt.Sprintf("Run on %s: %s", host.Name, command),
			Params:  raw,
		}, nil
	}

	out, exit, rerr := tb.SSHRun(ctx, host.ID.String(), command)
	if rerr != nil {
		return nil, fmt.Errorf("running on %s: %w", host.Name, rerr)
	}
	return SSHRunResult{Host: host.Name, Command: command, ExitCode: exit, Output: out}, nil
}
