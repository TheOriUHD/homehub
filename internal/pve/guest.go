package pve

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// GuestStatus returns the current runtime status of a VM or container.
// kind is "qemu" or "lxc" for all per-guest methods.
func (c *Client) GuestStatus(ctx context.Context, node, kind string, vmid int) (GuestStatus, error) {
	if err := checkKind(kind); err != nil {
		return GuestStatus{}, err
	}
	var st GuestStatus
	err := c.get(ctx, guestPath(node, kind, vmid, "status", "current"), nil, &st)
	return st, err
}

// GuestConfig returns the guest's config as raw key/value pairs (cores,
// memory, netN, ...). Values are left untyped: PVE mixes strings and numbers
// per key.
func (c *Client) GuestConfig(ctx context.Context, node, kind string, vmid int) (map[string]any, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	var cfg map[string]any
	err := c.get(ctx, guestPath(node, kind, vmid, "config"), nil, &cfg)
	return cfg, err
}

// SetGuestConfig applies config changes to a guest. PVE uses POST for qemu
// and PUT for lxc on the same path.
func (c *Client) SetGuestConfig(ctx context.Context, node, kind string, vmid int, changes map[string]string) error {
	if err := checkKind(kind); err != nil {
		return err
	}
	if len(changes) == 0 {
		return nil
	}
	form := url.Values{}
	for k, v := range changes {
		form.Set(k, v)
	}
	method := http.MethodPost
	if kind == KindLXC {
		method = http.MethodPut
	}
	return c.do(ctx, method, guestPath(node, kind, vmid, "config"), nil, form, nil)
}

// GuestAction starts a power-state transition and returns the task UPID.
// Valid actions: start, stop, shutdown, reboot, suspend, resume.
func (c *Client) GuestAction(ctx context.Context, node, kind string, vmid int, action string) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	switch action {
	case "start", "stop", "shutdown", "reboot", "suspend", "resume":
	default:
		return "", fmt.Errorf("pve: invalid guest action %q", action)
	}
	var upid string
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "status", action), nil, nil, &upid)
	return upid, err
}

// MigrateGuest moves a guest to the target node and returns the task UPID.
// online requests live migration for running guests.
func (c *Client) MigrateGuest(ctx context.Context, node, kind string, vmid int, target string, online bool) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	form := url.Values{"target": {target}}
	if online {
		form.Set("online", "1")
	}
	var upid string
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "migrate"), nil, form, &upid)
	return upid, err
}

// CloneGuest clones a guest to newid and returns the task UPID. full copies
// all disks instead of making a linked clone; target picks the destination
// node; name (qemu) / hostname (lxc) is optional.
func (c *Client) CloneGuest(ctx context.Context, node, kind string, vmid, newid int, name string, full bool, target string) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	form := url.Values{
		"newid": {strconv.Itoa(newid)},
		"full":  {formBool(full)},
	}
	if name != "" {
		// The same concept is spelled differently per guest type.
		key := "name"
		if kind == KindLXC {
			key = "hostname"
		}
		form.Set(key, name)
	}
	if target != "" {
		form.Set("target", target)
	}
	var upid string
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "clone"), nil, form, &upid)
	return upid, err
}

// DeleteGuest destroys a guest and returns the task UPID. purge also removes
// it from backup jobs, replication, and HA config.
func (c *Client) DeleteGuest(ctx context.Context, node, kind string, vmid int, purge bool) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	var q url.Values
	if purge {
		q = url.Values{"purge": {"1"}}
	}
	var upid string
	err := c.do(ctx, http.MethodDelete, guestPath(node, kind, vmid), q, nil, &upid)
	return upid, err
}

// ListSnapshots returns a guest's snapshots, excluding the "current"
// pseudo-entry PVE appends for the live state.
func (c *Client) ListSnapshots(ctx context.Context, node, kind string, vmid int) ([]Snapshot, error) {
	if err := checkKind(kind); err != nil {
		return nil, err
	}
	var all []Snapshot
	if err := c.get(ctx, guestPath(node, kind, vmid, "snapshot"), nil, &all); err != nil {
		return nil, err
	}
	snaps := all[:0]
	for _, s := range all {
		if s.Name != "current" {
			snaps = append(snaps, s)
		}
	}
	return snaps, nil
}

// CreateSnapshot snapshots a guest and returns the task UPID. vmstate (qemu
// only) also saves RAM.
func (c *Client) CreateSnapshot(ctx context.Context, node, kind string, vmid int, name, description string, vmstate bool) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	form := url.Values{"snapname": {name}}
	if description != "" {
		form.Set("description", description)
	}
	if vmstate {
		form.Set("vmstate", "1")
	}
	var upid string
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "snapshot"), nil, form, &upid)
	return upid, err
}

// RollbackSnapshot rolls a guest back to a snapshot and returns the task UPID.
func (c *Client) RollbackSnapshot(ctx context.Context, node, kind string, vmid int, name string) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	var upid string
	err := c.do(ctx, http.MethodPost, guestPath(node, kind, vmid, "snapshot", name, "rollback"), nil, nil, &upid)
	return upid, err
}

// DeleteSnapshot removes a snapshot and returns the task UPID.
func (c *Client) DeleteSnapshot(ctx context.Context, node, kind string, vmid int, name string) (string, error) {
	if err := checkKind(kind); err != nil {
		return "", err
	}
	var upid string
	err := c.do(ctx, http.MethodDelete, guestPath(node, kind, vmid, "snapshot", name), nil, nil, &upid)
	return upid, err
}

// Vzdump starts an ad-hoc backup of one guest on the node and returns the
// task UPID. mode and compress are optional ("" uses server defaults).
func (c *Client) Vzdump(ctx context.Context, node string, vmid int, storage, mode, compress string) (string, error) {
	form := url.Values{"vmid": {strconv.Itoa(vmid)}}
	if storage != "" {
		form.Set("storage", storage)
	}
	if mode != "" {
		form.Set("mode", mode)
	}
	if compress != "" {
		form.Set("compress", compress)
	}
	var upid string
	err := c.do(ctx, http.MethodPost, joinPath("nodes", node, "vzdump"), nil, form, &upid)
	return upid, err
}

// AgentInterface is one NIC as the QEMU guest agent reports it.
type AgentInterface struct {
	Name            string `json:"name"`
	HardwareAddress string `json:"hardware-address"`
	IPAddresses     []struct {
		Address string `json:"ip-address"`
		Type    string `json:"ip-address-type"` // ipv4 | ipv6
		Prefix  int    `json:"prefix"`
	} `json:"ip-addresses"`
}

// agentUnavailable matches the errors PVE returns when the guest agent is
// not installed, not running, or the guest is stopped.
func agentUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "guest agent is not running") ||
		strings.Contains(msg, "No QEMU guest agent") ||
		strings.Contains(msg, "not running") ||
		strings.Contains(msg, "guest-ping")
}

// ErrAgentUnavailable reports that the QEMU guest agent cannot answer —
// callers render "agent not available" instead of an upstream error.
var ErrAgentUnavailable = errors.New("qemu guest agent unavailable")

// AgentNetworkInterfaces queries the guest agent for NICs and addresses.
// QEMU guests only.
func (c *Client) AgentNetworkInterfaces(ctx context.Context, node string, vmid int) ([]AgentInterface, error) {
	var out struct {
		Result []AgentInterface `json:"result"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/network-get-interfaces", node, vmid), nil, &out)
	if err != nil {
		if agentUnavailable(err) {
			return nil, ErrAgentUnavailable
		}
		return nil, err
	}
	return out.Result, nil
}

// AgentHostname asks the guest agent for the guest's own hostname.
func (c *Client) AgentHostname(ctx context.Context, node string, vmid int) (string, error) {
	var out struct {
		Result struct {
			HostName string `json:"host-name"`
		} `json:"result"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-host-name", node, vmid), nil, &out)
	if err != nil {
		if agentUnavailable(err) {
			return "", ErrAgentUnavailable
		}
		return "", err
	}
	return out.Result.HostName, nil
}

// AgentOSInfo asks the guest agent for the guest OS description.
func (c *Client) AgentOSInfo(ctx context.Context, node string, vmid int) (string, error) {
	var out struct {
		Result struct {
			PrettyName string `json:"pretty-name"`
			Name       string `json:"name"`
		} `json:"result"`
	}
	err := c.get(ctx, fmt.Sprintf("/nodes/%s/qemu/%d/agent/get-osinfo", node, vmid), nil, &out)
	if err != nil {
		if agentUnavailable(err) {
			return "", ErrAgentUnavailable
		}
		return "", err
	}
	if out.Result.PrettyName != "" {
		return out.Result.PrettyName, nil
	}
	return out.Result.Name, nil
}
