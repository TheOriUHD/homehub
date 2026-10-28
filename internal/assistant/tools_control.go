package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/google/uuid"
)

// Controls that act on the physical world or take a system down — unlocking a
// door, opening the garage, disarming the alarm, stopping a VM or a game
// server, making someone a Minecraft operator — wait for the person exactly as
// a mutating SSH command does. The model can be talked into things by what it
// reads (a calendar entry, a container log, a chat line on a game server), and
// "open the garage" must never be one injected sentence away. Benign controls
// — lights, starting things, status — stay immediate.
//
// A held control is rebuilt from its params when approved, by the same
// function the immediate path uses, so the request that runs is the request
// that was described.

// Control action kinds, as they travel in a PendingAction.
const (
	KindHAService    = "ha.call_service"
	KindDockerAction = "docker.container_action"
	KindPVEAction    = "pve.guest_action"
	KindAMPAction    = "amp.instance_action"
	KindMCPower      = "mc.power"
	KindMCCommand    = "mc.command"
)

// IsControlKind reports whether a pending action is one of the controls above.
func IsControlKind(kind string) bool {
	switch kind {
	case KindHAService, KindDockerAction, KindPVEAction, KindAMPAction, KindMCPower, KindMCCommand:
		return true
	}
	return false
}

// controlParams is the union of every control tool's input. The field names
// are the tools' own, so a tool's input decodes straight into it.
type controlParams struct {
	SystemID   string         `json:"systemId"`
	EntityID   string         `json:"entityId,omitempty"`
	Service    string         `json:"service,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
	Name       string         `json:"name,omitempty"`
	Action     string         `json:"action,omitempty"`
	VMID       int            `json:"vmid,omitempty"`
	InstanceID string         `json:"instanceId,omitempty"`
	ServerID   string         `json:"serverId,omitempty"`
	Command    string         `json:"command,omitempty"`
}

// ControlRequest is one control as the API call that performs it.
type ControlRequest struct {
	Method string
	Path   string
	Body   any
	// Label says what it does, for the approval prompt and the outcome.
	Label string
}

// ControlRequestFor rebuilds the API call an approved control stands for,
// validating its params the same way the tool did.
func ControlRequestFor(kind string, params json.RawMessage) (ControlRequest, error) {
	var p controlParams
	if err := json.Unmarshal(params, &p); err != nil {
		return ControlRequest{}, fmt.Errorf("unreadable control parameters")
	}
	return buildControl(kind, p)
}

func buildControl(kind string, p controlParams) (ControlRequest, error) {
	sys, err := uuid.Parse(strings.TrimSpace(p.SystemID))
	if err != nil {
		return ControlRequest{}, fmt.Errorf("systemId must be the uuid from list_systems")
	}
	switch kind {
	case KindHAService:
		if p.EntityID == "" || p.Service == "" {
			return ControlRequest{}, fmt.Errorf("entityId and service are required")
		}
		body := map[string]any{"service": p.Service}
		if len(p.Data) > 0 {
			body["data"] = p.Data
		}
		return ControlRequest{http.MethodPost,
			"/ha/" + sys.String() + "/entities/" + url.PathEscape(p.EntityID) + "/service", body,
			fmt.Sprintf("Home Assistant: %s on %s", p.Service, p.EntityID)}, nil
	case KindDockerAction:
		if p.Name == "" || p.Action == "" {
			return ControlRequest{}, fmt.Errorf("name and action are required")
		}
		return ControlRequest{http.MethodPost,
			"/docker/" + sys.String() + "/containers/" + url.PathEscape(p.Name) + "/action",
			map[string]any{"action": p.Action},
			fmt.Sprintf("%s Docker container %s", verbLabel(p.Action), p.Name)}, nil
	case KindPVEAction:
		if p.VMID <= 0 || p.Action == "" {
			return ControlRequest{}, fmt.Errorf("vmid and action are required")
		}
		return ControlRequest{http.MethodPost,
			fmt.Sprintf("/proxmox/%s/guests/%d/action", sys, p.VMID),
			map[string]any{"action": p.Action},
			fmt.Sprintf("%s Proxmox guest %d", verbLabel(p.Action), p.VMID)}, nil
	case KindAMPAction:
		if p.InstanceID == "" || p.Action == "" {
			return ControlRequest{}, fmt.Errorf("instanceId and action are required")
		}
		return ControlRequest{http.MethodPost,
			"/amp/" + sys.String() + "/instances/" + url.PathEscape(p.InstanceID) + "/lifecycle",
			map[string]any{"action": p.Action},
			fmt.Sprintf("%s AMP instance %s", verbLabel(p.Action), p.InstanceID)}, nil
	case KindMCPower:
		server, err := uuid.Parse(strings.TrimSpace(p.ServerID))
		if err != nil {
			return ControlRequest{}, fmt.Errorf("serverId must be a server uuid from system_details")
		}
		switch p.Action {
		case "start", "stop", "restart":
		default:
			return ControlRequest{}, fmt.Errorf("action must be start, stop, or restart")
		}
		return ControlRequest{http.MethodPost,
			fmt.Sprintf("/mc/%s/servers/%s/%s", sys, server, p.Action), nil,
			fmt.Sprintf("%s Minecraft server %s", verbLabel(p.Action), server)}, nil
	case KindMCCommand:
		server, err := uuid.Parse(strings.TrimSpace(p.ServerID))
		if err != nil {
			return ControlRequest{}, fmt.Errorf("serverId must be a server uuid from system_details")
		}
		command := strings.TrimPrefix(strings.TrimSpace(p.Command), "/")
		if command == "" {
			return ControlRequest{}, fmt.Errorf("command is required")
		}
		return ControlRequest{http.MethodPost,
			fmt.Sprintf("/mc/%s/servers/%s/command", sys, server),
			map[string]any{"command": command},
			fmt.Sprintf("Minecraft console: /%s", command)}, nil
	}
	return ControlRequest{}, fmt.Errorf("unknown control %q", kind)
}

func verbLabel(action string) string {
	if action == "" {
		return action
	}
	return strings.ToUpper(action[:1]) + action[1:]
}

// immediateLifecycle are the lifecycle actions that bring something up rather
// than down; everything else stops, restarts or freezes a running system.
var immediateLifecycle = map[string]bool{"start": true, "unpause": true}

// controlNeedsApproval returns why a control must wait for the person, or ""
// when it may run at once.
func (tb *Toolbox) controlNeedsApproval(ctx context.Context, kind string, p controlParams) string {
	switch kind {
	case KindDockerAction, KindPVEAction, KindAMPAction, KindMCPower:
		if !immediateLifecycle[p.Action] {
			return "takes a running system down"
		}
	case KindMCCommand:
		return mcCommandRisk(p.Command)
	case KindHAService:
		return tb.haServiceRisk(ctx, p)
	}
	return ""
}

// haServiceRisk names Home Assistant services that open the house up. The
// service always runs in the entity's own domain (the API enforces that), so
// the domain is read from the entity id.
func (tb *Toolbox) haServiceRisk(ctx context.Context, p controlParams) string {
	domain, _, _ := strings.Cut(p.EntityID, ".")
	switch domain {
	case "lock":
		if p.Service == "unlock" || p.Service == "open" {
			return "unlocks a lock"
		}
	case "alarm_control_panel":
		if p.Service == "alarm_disarm" || p.Service == "alarm_trigger" {
			return "disarms or triggers the alarm"
		}
	case "cover":
		switch p.Service {
		case "open_cover", "toggle", "set_cover_position":
		default:
			return ""
		}
		// Blinds open freely; doors, gates and windows do not. A cover
		// whose class cannot be read is treated as a door.
		switch tb.haDeviceClass(ctx, p) {
		case "awning", "blind", "curtain", "shade", "shutter":
			return ""
		}
		return "opens a door, gate or window"
	}
	return ""
}

// haDeviceClass reads one entity's device_class, or "" when it cannot.
func (tb *Toolbox) haDeviceClass(ctx context.Context, p controlParams) string {
	domain, _, _ := strings.Cut(p.EntityID, ".")
	raw, err := tb.call(ctx, http.MethodGet, "/ha/"+p.SystemID+"/entities?domain="+url.QueryEscape(domain), nil)
	if err != nil {
		return ""
	}
	var payload struct {
		Entities []struct {
			EntityID string         `json:"entityId"`
			Attrs    map[string]any `json:"attributes"`
		} `json:"entities"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	for _, e := range payload.Entities {
		if e.EntityID == p.EntityID {
			class, _ := e.Attrs["device_class"].(string)
			return class
		}
	}
	return ""
}

// mcCommandRisk names console commands that hand out power or take the server
// away from its players. execute … run <cmd> is judged by the command it runs.
func mcCommandRisk(command string) string {
	fields := strings.Fields(strings.ToLower(strings.TrimPrefix(strings.TrimSpace(command), "/")))
	for len(fields) > 0 {
		name := fields[0]
		// minecraft:op and essentials:ban are the same commands.
		if i := strings.LastIndex(name, ":"); i >= 0 {
			name = name[i+1:]
		}
		switch name {
		case "execute":
			run := -1
			for i, f := range fields {
				if f == "run" {
					run = i
					break
				}
			}
			if run < 0 {
				return ""
			}
			fields = fields[run+1:]
			continue
		case "op", "deop":
			return "changes who is a server operator"
		case "ban", "ban-ip", "banip", "ipban", "tempban", "tempbanip", "pardon", "pardon-ip",
			"unban", "unbanip", "eban", "etempban", "eunban", "ebanip", "eunbanip":
			return "bans or unbans players"
		case "stop", "restart", "reload", "rl", "save-off":
			return "stops, reloads or stops saving the server"
		case "whitelist", "ewhitelist":
			if len(fields) > 1 && (fields[1] == "off" || fields[1] == "remove") {
				return "opens the whitelist"
			}
		case "lp", "luckperms", "perm", "perms", "permissions", "pex":
			return "changes permissions"
		case "sudo", "esudo":
			return "runs a command as another player"
		}
		return ""
	}
	return ""
}

// prepareControl validates a control tool's input and decides its fate: a
// non-nil PendingAction when it must wait for the person, otherwise the
// request to run now.
func (tb *Toolbox) prepareControl(ctx context.Context, kind string, input json.RawMessage) (*PendingAction, ControlRequest, error) {
	var p controlParams
	if err := json.Unmarshal(input, &p); err != nil {
		return nil, ControlRequest{}, fmt.Errorf("unreadable input: %v", err)
	}
	p.SystemID = strings.TrimSpace(p.SystemID)
	req, err := buildControl(kind, p)
	if err != nil {
		return nil, ControlRequest{}, err
	}
	if reason := tb.controlNeedsApproval(ctx, kind, p); reason != "" {
		raw, _ := json.Marshal(p)
		summary := req.Label
		if name := tb.systemName(ctx, p.SystemID); name != "" {
			summary += " on " + name
		}
		return &PendingAction{Kind: kind, Summary: summary + " — " + reason, Params: raw}, req, nil
	}
	return nil, req, nil
}

// control is the whole of a plain control tool: hold it, or run it.
func (tb *Toolbox) control(ctx context.Context, kind string, input json.RawMessage) (any, error) {
	held, req, err := tb.prepareControl(ctx, kind, input)
	if err != nil {
		return nil, err
	}
	if held != nil {
		return held, nil
	}
	return tb.call(ctx, req.Method, req.Path, req.Body)
}

// systemName is the system's display name for a proposal, when it can be read.
func (tb *Toolbox) systemName(ctx context.Context, systemID string) string {
	id, err := uuid.Parse(systemID)
	if err != nil || tb.Store == nil {
		return ""
	}
	row, err := tb.Store.GetIntegration(ctx, id)
	if err != nil {
		return ""
	}
	return row.Name
}
