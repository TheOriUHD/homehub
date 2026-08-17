package amp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/metrics"
)

// ---- upstream wire shapes ---------------------------------------------------

// LoginResult is Core/Login. Uniquely, its keys are camelCase — everything
// else AMP returns is PascalCase.
type LoginResult struct {
	Success      bool   `json:"success"`
	Result       int    `json:"result"`
	ResultReason string `json:"resultReason"`
	SessionID    string `json:"sessionID"`
}

// Target is one entry of ADSModule/GetInstances — an ADS/controller with its
// instances (IADSInstance upstream).
type Target struct {
	ID                 int          `json:"Id"`
	InstanceID         string       `json:"InstanceId"`
	FriendlyName       string       `json:"FriendlyName"`
	Description        string       `json:"Description"`
	Disabled           bool         `json:"Disabled"`
	IsRemote           bool         `json:"isRemote"`
	Platform           PlatformInfo `json:"Platform"`
	State              int          `json:"State"`
	StateReason        string       `json:"StateReason"`
	LastUpdated        AmpTime      `json:"LastUpdated"`
	AvailableInstances []Instance   `json:"AvailableInstances"`
	AvailableIPs       []string     `json:"AvailableIPs"`
	URL                string       `json:"URL"`
}

type PlatformInfo struct {
	CPUInfo        CPUInfo `json:"CPUInfo"`
	InstalledRAMMB int     `json:"InstalledRAMMB"`
	PlatformName   string  `json:"PlatformName"`
}

type CPUInfo struct {
	Sockets      int    `json:"Sockets"`
	Cores        int    `json:"Cores"`
	Threads      int    `json:"Threads"`
	ModelName    string `json:"ModelName"`
	TotalCores   int    `json:"TotalCores"`
	TotalThreads int    `json:"TotalThreads"`
}

// Instance is one AMP instance inside a target.
type Instance struct {
	InstanceID         string            `json:"InstanceID"`
	TargetID           string            `json:"TargetID"`
	InstanceName       string            `json:"InstanceName"`
	FriendlyName       string            `json:"FriendlyName"`
	Description        string            `json:"Description"`
	Module             string            `json:"Module"`
	ModuleDisplayName  string            `json:"ModuleDisplayName"`
	AMPVersion         json.RawMessage   `json:"AMPVersion"` // string pre-2.6, object after
	IP                 string            `json:"IP"`
	Port               int               `json:"Port"`
	Daemon             bool              `json:"Daemon"`
	Running            bool              `json:"Running"`
	AppState           int               `json:"AppState"`
	DiskUsageMB        int64             `json:"DiskUsageMB"`
	Suspended          bool              `json:"Suspended"`
	Metrics            map[string]Metric `json:"Metrics"`
	DisplayImageSource string            `json:"DisplayImageSource"`
}

type Metric struct {
	RawValue float64 `json:"RawValue"`
	MaxValue float64 `json:"MaxValue"`
	Percent  float64 `json:"Percent"`
	Units    string  `json:"Units"`
}

// Status is Core/GetStatus and Updates.Status.
type Status struct {
	State   int               `json:"State"`
	Uptime  string            `json:"Uptime"`
	Metrics map[string]Metric `json:"Metrics"`
}

// Updates is Core/GetUpdates: console lines since this session's last call.
type Updates struct {
	Status         Status         `json:"Status"`
	ConsoleEntries []ConsoleEntry `json:"ConsoleEntries"`
}

type ConsoleEntry struct {
	Timestamp AmpTime `json:"Timestamp"`
	Source    string  `json:"Source"`
	Type      string  `json:"Type"`
	Contents  string  `json:"Contents"`
}

// ActionResult is the generic verdict envelope for lifecycle calls.
type ActionResult struct {
	Status bool   `json:"Status"`
	Reason string `json:"Reason"`
}

// AmpTime accepts both of AMP's date encodings: ".NET" /Date(ms)/ strings
// (pre-2.6) and ISO timestamps (2.6+).
type AmpTime struct{ time.Time }

func (t *AmpTime) UnmarshalJSON(raw []byte) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return err
	}
	if strings.HasPrefix(s, "/Date(") {
		ms, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(s, "/Date("), ")/"), 10, 64)
		if err != nil {
			return fmt.Errorf("bad /Date()/ timestamp %q", s)
		}
		t.Time = time.UnixMilli(ms)
		return nil
	}
	if s == "" {
		t.Time = time.Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		// Some builds emit ISO without a zone.
		parsed, err = time.ParseInLocation("2006-01-02T15:04:05", s, time.Local)
		if err != nil {
			return fmt.Errorf("bad timestamp %q", s)
		}
	}
	t.Time = parsed
	return nil
}

// StateNames maps AMP's application state enum to stable slugs the UI
// translates and colors.
var StateNames = map[int]string{
	-1: "undefined", 0: "stopped", 5: "preStart", 7: "configuring",
	10: "starting", 20: "running", 30: "restarting", 40: "stopping",
	45: "preparingForSleep", 50: "sleeping", 60: "waiting", 70: "installing",
	75: "updating", 80: "awaitingInput", 100: "failed", 200: "suspended",
	250: "maintenance", 999: "indeterminate",
}

func stateName(code int) string {
	if name, ok := StateNames[code]; ok {
		return name
	}
	return "undefined"
}

// ---- normalized shapes ------------------------------------------------------

type NormTarget struct {
	InstanceID   string
	FriendlyName string
	Description  string
	State        string
	Online       bool
	Platform     string
	CPUModel     string
	Cores        int
	RAMMB        int
	Instances    []NormInstance
}

type NormInstance struct {
	ID           string
	Name         string
	FriendlyName string
	Description  string
	Module       string
	ModuleName   string
	Running      bool
	Suspended    bool
	State        string
	IP           string
	Port         int
	DiskUsageMB  int64
	CPUPercent   *float64
	MemUsedMB    *float64
	MemMaxMB     *float64
	Players      *int
	MaxPlayers   *int
}

func normalizeTarget(t Target) NormTarget {
	out := NormTarget{
		InstanceID:   t.InstanceID,
		FriendlyName: t.FriendlyName,
		Description:  t.Description,
		State:        stateName(t.State),
		Online:       !t.Disabled && t.State != 100,
		Platform:     t.Platform.PlatformName,
		CPUModel:     t.Platform.CPUInfo.ModelName,
		Cores:        t.Platform.CPUInfo.TotalCores,
		RAMMB:        t.Platform.InstalledRAMMB,
	}
	for _, inst := range t.AvailableInstances {
		out.Instances = append(out.Instances, normalizeInstance(inst))
	}
	sort.Slice(out.Instances, func(i, j int) bool {
		return out.Instances[i].FriendlyName < out.Instances[j].FriendlyName
	})
	return out
}

func normalizeInstance(i Instance) NormInstance {
	out := NormInstance{
		ID:           i.InstanceID,
		Name:         i.InstanceName,
		FriendlyName: i.FriendlyName,
		Description:  i.Description,
		Module:       i.Module,
		ModuleName:   i.ModuleDisplayName,
		Running:      i.Running,
		Suspended:    i.Suspended,
		State:        stateName(i.AppState),
		IP:           i.IP,
		Port:         i.Port,
		DiskUsageMB:  i.DiskUsageMB,
	}
	// A stopped instance container reports a stale AppState — the container
	// being down IS the state, and nothing behind the proxy is reachable.
	if !i.Running {
		out.State = "offline"
	}
	if out.ModuleName == "" {
		out.ModuleName = i.Module
	}
	if m, ok := i.Metrics["CPU Usage"]; ok {
		v := m.Percent
		out.CPUPercent = &v
	}
	if m, ok := i.Metrics["Memory Usage"]; ok {
		used, maxV := m.RawValue, m.MaxValue
		out.MemUsedMB, out.MemMaxMB = &used, &maxV
	}
	if m, ok := i.Metrics["Active Users"]; ok {
		players, maxP := int(m.RawValue), int(m.MaxValue)
		out.Players, out.MaxPlayers = &players, &maxP
	}
	return out
}

// ---- connection -------------------------------------------------------------

const (
	consolePollInterval = 2 * time.Second
	consoleIdleStop     = 3 * time.Minute
	consoleMaxLines     = 5000
)

type Conn struct {
	inst   integration.Instance
	client *client
	deps   integration.Deps
	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	targets  []Target
	states   map[string]int // instanceID -> last seen AppState (change detection)
	consoles map[string]*consoleState
}

func newConn(inst integration.Instance, c *client, deps integration.Deps) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	return &Conn{
		inst:     inst,
		client:   c,
		deps:     deps,
		ctx:      ctx,
		cancel:   cancel,
		states:   map[string]int{},
		consoles: map[string]*consoleState{},
	}
}

func (c *Conn) Close() { c.cancel() }

// Targets fetches, caches, and normalizes the instance inventory.
func (c *Conn) Targets(ctx context.Context) ([]NormTarget, error) {
	targets, err := fetchTargets(ctx, c.client)
	if err != nil {
		return nil, err
	}
	changed := false
	c.mu.Lock()
	c.targets = targets
	for _, t := range targets {
		for _, inst := range t.AvailableInstances {
			if prev, seen := c.states[inst.InstanceID]; !seen || prev != inst.AppState {
				c.states[inst.InstanceID] = inst.AppState
				changed = true
			}
		}
	}
	c.mu.Unlock()
	if changed {
		c.deps.Publish("instances-changed", nil)
	}
	out := make([]NormTarget, 0, len(targets))
	for _, t := range targets {
		out = append(out, normalizeTarget(t))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FriendlyName < out[j].FriendlyName })
	return out, nil
}

// instanceName resolves an instance ID to its InstanceName (what the ADS
// lifecycle calls take) from the cached inventory.
func (c *Conn) instanceName(ctx context.Context, instanceID string) (string, error) {
	c.mu.Lock()
	name := findInstanceName(c.targets, instanceID)
	c.mu.Unlock()
	if name != "" {
		return name, nil
	}
	if _, err := c.Targets(ctx); err != nil {
		return "", err
	}
	c.mu.Lock()
	name = findInstanceName(c.targets, instanceID)
	c.mu.Unlock()
	if name == "" {
		return "", errors.New("unknown instance — it may have been deleted")
	}
	return name, nil
}

func findInstanceName(targets []Target, instanceID string) string {
	for _, t := range targets {
		for _, inst := range t.AvailableInstances {
			if inst.InstanceID == instanceID {
				return inst.InstanceName
			}
		}
	}
	return ""
}

// InstanceStatus reads live state, uptime, and metrics from one instance.
func (c *Conn) InstanceStatus(ctx context.Context, instanceID string) (*Status, error) {
	var status Status
	if err := c.client.Call(ctx, instanceID, "Core/GetStatus", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}

// AppAction drives the application inside an instance.
func (c *Conn) AppAction(ctx context.Context, instanceID, action string) error {
	var api string
	switch action {
	case "start":
		api = "Core/Start"
	case "stop":
		api = "Core/Stop"
	case "restart":
		api = "Core/Restart"
	case "kill":
		api = "Core/Kill"
	case "update":
		api = "Core/UpdateApplication"
	default:
		return fmt.Errorf("unknown application action %q", action)
	}
	// Some verdicts come back as ActionResult, others as no body at all.
	var raw json.RawMessage
	if err := c.client.Call(ctx, instanceID, api, nil, &raw); err != nil {
		return err
	}
	return actionVerdict(raw)
}

// InstanceAction drives the instance itself through the controller.
func (c *Conn) InstanceAction(ctx context.Context, instanceID, action string) error {
	var api string
	switch action {
	case "start":
		api = "ADSModule/StartInstance"
	case "stop":
		api = "ADSModule/StopInstance"
	case "restart":
		api = "ADSModule/RestartInstance"
	default:
		return fmt.Errorf("unknown instance action %q", action)
	}
	name, err := c.instanceName(ctx, instanceID)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	if err := c.client.Call(ctx, "", api, map[string]any{"InstanceName": name}, &raw); err != nil {
		return err
	}
	return actionVerdict(raw)
}

func actionVerdict(raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var res ActionResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return nil // not an ActionResult envelope; treat as fire-and-forget
	}
	// Zero-value decode of a non-envelope object also lands here; only a
	// present-and-false Status with a reason is a real failure signal.
	if !res.Status && res.Reason != "" {
		return errors.New(res.Reason)
	}
	return nil
}

// SendCommand dispatches a console command. AMP gives no per-command
// verdict — success or failure lands in the console stream, which is
// exactly how a real console behaves.
func (c *Conn) SendCommand(ctx context.Context, instanceID, command string) error {
	return c.client.Call(ctx, instanceID, "Core/SendConsoleMessage",
		map[string]any{"message": command}, nil)
}

// ---- console streaming ------------------------------------------------------

// ConsoleLine is one sequenced console entry for the API.
type ConsoleLine struct {
	Seq      int64  `json:"seq"`
	TSMillis int64  `json:"ts"`
	Type     string `json:"type"`
	Source   string `json:"source"`
	Contents string `json:"contents"`
}

type consoleState struct {
	mu       sync.Mutex
	lines    []ConsoleLine
	nextSeq  int64
	lastRead time.Time
	running  bool
}

// Console returns buffered lines after a sequence cursor, starting the
// instance's poller on first use. Pollers stop themselves once nobody has
// read for a while.
func (c *Conn) Console(ctx context.Context, instanceID string, after int64, limit int) ([]ConsoleLine, int64, int64, error) {
	state := c.consoleFor(instanceID)

	state.mu.Lock()
	state.lastRead = time.Now()
	needsStart := !state.running
	if needsStart {
		state.running = true
	}
	state.mu.Unlock()

	if needsStart {
		// Prime synchronously so the first page isn't empty, then poll.
		if err := c.pollConsole(ctx, instanceID, state); err != nil {
			state.mu.Lock()
			state.running = false
			state.mu.Unlock()
			return nil, 0, 0, err
		}
		go c.consoleLoop(instanceID, state)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	oldest := state.nextSeq - int64(len(state.lines))
	out := []ConsoleLine{}
	for _, l := range state.lines {
		if l.Seq > after {
			out = append(out, l)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, oldest, state.nextSeq, nil
}

func (c *Conn) consoleFor(instanceID string) *consoleState {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.consoles[instanceID]
	if !ok {
		state = &consoleState{}
		c.consoles[instanceID] = state
	}
	return state
}

func (c *Conn) consoleLoop(instanceID string, state *consoleState) {
	ticker := time.NewTicker(consolePollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			state.mu.Lock()
			state.running = false
			state.mu.Unlock()
			return
		case <-ticker.C:
			state.mu.Lock()
			idle := time.Since(state.lastRead) > consoleIdleStop
			if idle {
				state.running = false
			}
			state.mu.Unlock()
			if idle {
				return
			}
			pctx, cancel := context.WithTimeout(c.ctx, consolePollInterval*4)
			err := c.pollConsole(pctx, instanceID, state)
			cancel()
			if err != nil && c.ctx.Err() == nil {
				// Keep polling; the next GetUpdates rides a fresh session if
				// the old one expired. Persistent failures surface on read.
				continue
			}
		}
	}
}

// pollConsole runs one GetUpdates cycle: append new lines, publish them and
// the fresh status snapshot on the instance topic.
func (c *Conn) pollConsole(ctx context.Context, instanceID string, state *consoleState) error {
	var updates Updates
	if err := c.client.Call(ctx, instanceID, "Core/GetUpdates", nil, &updates); err != nil {
		return err
	}

	var fresh []ConsoleLine
	state.mu.Lock()
	for _, entry := range updates.ConsoleEntries {
		line := ConsoleLine{
			Seq:      state.nextSeq,
			TSMillis: entry.Timestamp.UnixMilli(),
			Type:     entry.Type,
			Source:   entry.Source,
			Contents: entry.Contents,
		}
		state.nextSeq++
		state.lines = append(state.lines, line)
		fresh = append(fresh, line)
	}
	if excess := len(state.lines) - consoleMaxLines; excess > 0 {
		state.lines = state.lines[excess:]
	}
	state.mu.Unlock()

	if len(fresh) > 0 {
		c.deps.Publish("console", map[string]any{"instanceId": instanceID, "lines": fresh})
	}
	c.deps.Publish("instance-status", map[string]any{
		"instanceId": instanceID,
		"state":      stateName(updates.Status.State),
		"uptime":     updates.Status.Uptime,
	})
	return nil
}

// ---- platform hooks ---------------------------------------------------------

func (c *Conn) Health(ctx context.Context) integration.Health {
	if _, err := c.Targets(ctx); err != nil {
		return integration.Health{Status: integration.StatusUnreachable, Message: friendlyError(err)}
	}
	return integration.Health{Status: integration.StatusConnected}
}

func (c *Conn) Resources(ctx context.Context) ([]integration.Resource, error) {
	targets, err := c.Targets(ctx)
	if err != nil {
		return nil, err
	}
	base := TypeID + "/" + c.inst.ID.String()
	var out []integration.Resource
	for _, t := range targets {
		for _, inst := range t.Instances {
			out = append(out, integration.Resource{
				Path: base + "/instance/" + inst.ID,
				Kind: "instance",
				Name: inst.FriendlyName,
			})
		}
	}
	return out, nil
}

func (c *Conn) Collect(ctx context.Context) ([]metrics.Sample, error) {
	targets, err := c.Targets(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	base := TypeID + "/" + c.inst.ID.String()
	var samples []metrics.Sample
	for _, t := range targets {
		for _, inst := range t.Instances {
			res := base + "/instance/" + inst.ID
			if inst.CPUPercent != nil {
				samples = append(samples, metrics.Sample{
					Resource: res, Metric: "cpu", Unit: "fraction", TS: now, Value: *inst.CPUPercent / 100,
				})
			}
			if inst.MemUsedMB != nil && inst.MemMaxMB != nil {
				samples = append(samples,
					metrics.Sample{Resource: res, Metric: "mem_used", Unit: "bytes", TS: now, Value: *inst.MemUsedMB * 1024 * 1024},
					metrics.Sample{Resource: res, Metric: "mem_total", Unit: "bytes", TS: now, Value: *inst.MemMaxMB * 1024 * 1024},
				)
			}
			if inst.Players != nil {
				samples = append(samples, metrics.Sample{
					Resource: res, Metric: "players", Unit: "count", TS: now, Value: float64(*inst.Players),
				})
			}
		}
	}
	return samples, nil
}
