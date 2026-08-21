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
//  1. Permission — the caller needs control on the host (or the registry) for
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
	}
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
	if !access.Can("system/ssh", "control") && !access.Can(hostRes, "control") {
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
