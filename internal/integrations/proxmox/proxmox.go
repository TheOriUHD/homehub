// Package proxmox implements the Proxmox VE integration driver.
package proxmox

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"homehub/internal/integration"
	"homehub/internal/metrics"
	"homehub/internal/pve"
)

const TypeID = "proxmox"

type Driver struct{}

func (Driver) TypeID() string      { return TypeID }
func (Driver) DisplayName() string { return "Proxmox VE" }
func (Driver) Description() string {
	return "Manage a Proxmox VE node or cluster: virtual machines, containers, storage, backups, and consoles."
}
func (Driver) SecretNames() []string { return []string{"tokenSecret"} }

// buildConfig normalizes the instance config into a pve.Config.
func buildConfig(config map[string]any, tokenSecret string) (pve.Config, error) {
	host, _ := config["host"].(string)
	host = strings.TrimSpace(strings.TrimRight(host, "/"))
	if host == "" {
		return pve.Config{}, errors.New("host is required, e.g. pve.example.com or https://192.168.1.10:8006")
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		return pve.Config{}, fmt.Errorf("host %q is not a valid address", host)
	}
	if u.Port() == "" && strings.HasPrefix(host, "https://") {
		u.Host += ":8006"
	}

	tokenID, _ := config["tokenId"].(string)
	if strings.TrimSpace(tokenID) == "" {
		return pve.Config{}, errors.New("API token id is required, e.g. root@pam!homehub")
	}
	if tokenSecret == "" {
		return pve.Config{}, errors.New("API token secret is required")
	}
	insecure, _ := config["insecureTls"].(bool)
	fingerprint, _ := config["fingerprint"].(string)

	return pve.Config{
		BaseURL:     u.Scheme + "://" + u.Host,
		TokenID:     strings.TrimSpace(tokenID),
		Secret:      strings.TrimSpace(tokenSecret),
		InsecureTLS: insecure,
		Fingerprint: strings.TrimSpace(fingerprint),
		Timeout:     15 * time.Second,
	}, nil
}

func (Driver) Test(ctx context.Context, config map[string]any, secrets map[string]string) integration.TestResult {
	cfg, err := buildConfig(config, secrets["tokenSecret"])
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	client, err := pve.New(cfg)
	if err != nil {
		return integration.TestResult{OK: false, Message: err.Error()}
	}
	version, err := client.Version(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	status, err := client.ClusterStatus(ctx)
	if err != nil {
		return integration.TestResult{OK: false, Message: friendlyError(err)}
	}
	clusterName := ""
	nodesOnline, nodesTotal := 0, 0
	for _, entry := range status {
		switch entry.Type {
		case "cluster":
			clusterName = entry.Name
		case "node":
			nodesTotal++
			if bool(entry.Online) {
				nodesOnline++
			}
		}
	}
	msg := fmt.Sprintf("Connected to Proxmox VE %s", version.Version)
	if clusterName != "" {
		msg += fmt.Sprintf(" — cluster %q, %d/%d nodes online", clusterName, nodesOnline, nodesTotal)
	} else if nodesTotal > 0 {
		msg += fmt.Sprintf(" — standalone node, %d/%d online", nodesOnline, nodesTotal)
	}
	return integration.TestResult{
		OK:      true,
		Message: msg,
		Detail:  map[string]any{"version": version.Version, "cluster": clusterName, "nodes": nodesTotal},
	}
}

// friendlyError turns transport/auth failures into messages that say what to
// fix, not just what broke.
func friendlyError(err error) string {
	var pveErr *pve.Error
	if errors.As(err, &pveErr) {
		switch pveErr.Status {
		case 401:
			return "Proxmox rejected the API token — check the token id and secret, and that the token is not expired (401)"
		case 403:
			return "the API token lacks permission — give it at least PVEAuditor on / plus PVEVMAdmin where you want control (403)"
		}
		return pveErr.Message
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate"):
		return "TLS certificate not trusted — pin the certificate fingerprint or enable \"skip TLS verification\" (" + msg + ")"
	case strings.Contains(msg, "connection refused"):
		return "connection refused — is the address right and the Proxmox web service (port 8006) reachable from HomeHub?"
	case strings.Contains(msg, "no such host"):
		return "hostname not found — check the address and that HomeHub can resolve it"
	case strings.Contains(msg, "context deadline exceeded") || strings.Contains(msg, "timeout"):
		return "connection timed out — check the address, port, and any firewalls between HomeHub and Proxmox"
	}
	return msg
}

func (d Driver) Open(ctx context.Context, inst integration.Instance, deps integration.Deps) (integration.Connection, error) {
	secret, err := deps.Secret(ctx, "tokenSecret")
	if err != nil {
		return nil, fmt.Errorf("stored token secret unreadable: %w", err)
	}
	cfg, err := buildConfig(inst.Config, secret)
	if err != nil {
		return nil, err
	}
	client, err := pve.New(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := client.Version(ctx); err != nil {
		return nil, errors.New(friendlyError(err))
	}
	return &Conn{
		Client: client,
		inst:   inst,
		deps:   deps,
		guests: map[int]guestLoc{},
	}, nil
}

type guestLoc struct {
	Node string
	Kind string
}

// Conn is a live Proxmox connection. API handlers retrieve it from the
// integration manager and type-assert to reach the underlying client.
type Conn struct {
	Client *pve.Client
	inst   integration.Instance
	deps   integration.Deps

	mu         sync.Mutex
	guests     map[int]guestLoc
	guestState map[int]string
	taskState  map[string]string
}

func (c *Conn) Close() {}

func (c *Conn) Health(ctx context.Context) integration.Health {
	status, err := c.Client.ClusterStatus(ctx)
	if err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	offline := []string{}
	for _, entry := range status {
		if entry.Type == "node" && !bool(entry.Online) {
			offline = append(offline, entry.Name)
		}
	}
	if len(offline) > 0 {
		return integration.Health{
			Status:  integration.StatusDegraded,
			Message: "node(s) offline: " + strings.Join(offline, ", "),
		}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	rows, err := c.Client.ClusterResources(ctx)
	if err != nil {
		return nil, err
	}
	base := c.inst.ResourcePath()
	out := []integration.Resource{{Path: base, Kind: "instance", Name: c.inst.Name}}
	seenStorage := map[string]bool{}
	for _, r := range rows {
		switch r.Type {
		case "node":
			out = append(out, integration.Resource{Path: base + "/node/" + r.Node, Kind: "node", Name: r.Node})
		case "qemu", "lxc":
			out = append(out, integration.Resource{
				Path: fmt.Sprintf("%s/guest/%d", base, int(r.VMID)),
				Kind: "guest",
				Name: r.Name,
			})
		case "storage":
			if !seenStorage[r.Storage] {
				seenStorage[r.Storage] = true
				out = append(out, integration.Resource{Path: base + "/storage/" + r.Storage, Kind: "storage", Name: r.Storage})
			}
		}
	}
	return out, nil
}

// Collect samples node and guest metrics, refreshes the guest index, and
// publishes change events that drive live UI updates.
func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	rows, err := c.Client.ClusterResources(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	base := c.inst.ResourcePath()
	samples := make([]metrics.Sample, 0, len(rows)*3)

	newIndex := map[int]guestLoc{}
	newState := map[int]string{}
	for _, r := range rows {
		switch r.Type {
		case "node":
			res := base + "/node/" + r.Node
			samples = append(samples,
				metrics.Sample{Resource: res, Metric: "cpu", Unit: "fraction", TS: now, Value: float64(r.CPU)},
				metrics.Sample{Resource: res, Metric: "mem_used", Unit: "bytes", TS: now, Value: float64(r.Mem)},
				metrics.Sample{Resource: res, Metric: "mem_total", Unit: "bytes", TS: now, Value: float64(r.MaxMem)},
			)
		case "qemu", "lxc":
			vmid := int(r.VMID)
			newIndex[vmid] = guestLoc{Node: r.Node, Kind: r.Type}
			newState[vmid] = r.Status
			if bool(r.Template) {
				continue
			}
			res := fmt.Sprintf("%s/guest/%d", base, vmid)
			samples = append(samples,
				metrics.Sample{Resource: res, Metric: "cpu", Unit: "fraction", TS: now, Value: float64(r.CPU)},
				metrics.Sample{Resource: res, Metric: "mem_used", Unit: "bytes", TS: now, Value: float64(r.Mem)},
				metrics.Sample{Resource: res, Metric: "mem_total", Unit: "bytes", TS: now, Value: float64(r.MaxMem)},
			)
		}
	}

	c.mu.Lock()
	stateChanged := len(c.guestState) != len(newState)
	if !stateChanged {
		for vmid, status := range newState {
			if c.guestState[vmid] != status {
				stateChanged = true
				break
			}
		}
	}
	c.guests = newIndex
	c.guestState = newState
	c.mu.Unlock()

	if stateChanged {
		c.deps.Publish("guests-changed", nil)
	}
	c.pollTasks(ctx)
	return samples, nil
}

// pollTasks watches recent tasks across nodes and publishes on transitions,
// so open task views update without client polling.
func (c *Conn) pollTasks(ctx context.Context) {
	status, err := c.Client.ClusterStatus(ctx)
	if err != nil {
		return
	}
	changed := false
	seen := map[string]string{}
	for _, entry := range status {
		if entry.Type != "node" || !bool(entry.Online) {
			continue
		}
		tasks, err := c.Client.NodeTasks(ctx, entry.Name, 25)
		if err != nil {
			continue
		}
		for _, task := range tasks {
			seen[task.UPID] = task.Status + "/" + task.ExitStatus
		}
	}
	c.mu.Lock()
	if len(seen) != len(c.taskState) {
		changed = true
	} else {
		for upid, s := range seen {
			if c.taskState[upid] != s {
				changed = true
				break
			}
		}
	}
	c.taskState = seen
	c.mu.Unlock()
	if changed {
		c.deps.Publish("tasks-changed", nil)
	}
}

// Locate resolves a vmid to its node and kind, refreshing the index on miss
// (e.g. right after a migration or clone).
func (c *Conn) Locate(ctx context.Context, vmid int) (node, kind string, err error) {
	c.mu.Lock()
	loc, ok := c.guests[vmid]
	c.mu.Unlock()
	if ok {
		return loc.Node, loc.Kind, nil
	}
	rows, err := c.Client.ClusterResources(ctx)
	if err != nil {
		return "", "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, r := range rows {
		if r.Type == "qemu" || r.Type == "lxc" {
			c.guests[int(r.VMID)] = guestLoc{Node: r.Node, Kind: r.Type}
		}
	}
	if loc, ok = c.guests[vmid]; ok {
		return loc.Node, loc.Kind, nil
	}
	return "", "", fmt.Errorf("guest %d not found on this Proxmox instance", vmid)
}

// FriendlyError is exported for API handlers mapping upstream failures.
func FriendlyError(err error) string { return friendlyError(err) }
