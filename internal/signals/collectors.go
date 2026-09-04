package signals

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	containertypes "github.com/docker/docker/api/types/container"
	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/integration"
	"github.com/TheOriUHD/homehub/internal/integrations/aperture"
	"github.com/TheOriUHD/homehub/internal/integrations/docker"
	"github.com/TheOriUHD/homehub/internal/integrations/homeassistant"
	"github.com/TheOriUHD/homehub/internal/integrations/minecraft"
	"github.com/TheOriUHD/homehub/internal/integrations/proxmox"
	"github.com/TheOriUHD/homehub/internal/integrations/tailscale"
	"github.com/TheOriUHD/homehub/internal/integrations/truenas"
	"github.com/TheOriUHD/homehub/internal/store"
)

// Deps is what the built-in collectors need to see the world.
type Deps struct {
	Store   *store.Store
	Manager *integration.Manager
}

// RegisterBuiltins wires every built-in collector into the service.
func RegisterBuiltins(s *Service, d Deps) {
	s.Register(&integrationStatusCollector{Deps: d})
	s.Register(&truenasAlertCollector{d})
	s.Register(&haBatteryCollector{d})
	s.Register(&patchpilotCollector{d})
	s.Register(&backupCollector{d})
	s.Register(&proxmoxCollector{d})
	s.Register(&dockerCollector{d})
	s.Register(&tailscaleCollector{d})
	s.Register(&apertureCollector{d})
	s.Register(&minecraftCollector{Deps: d})
	s.Register(&trashCollector{d})
}

// instances lists enabled integrations of one type.
func (d Deps) instances(ctx context.Context, typeID string) ([]store.Integration, error) {
	rows, err := d.Store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	out := rows[:0:0]
	for _, r := range rows {
		if r.Type == typeID && r.Enabled {
			out = append(out, r)
		}
	}
	return out, nil
}

func srcID(id uuid.UUID) *uuid.UUID { return &id }

// ---- generic integration status --------------------------------------------

// integrationStatusCollector turns every instance's connection state into a
// signal: unreachable is critical, degraded is a warning, and both flip to
// green the moment the instance reports connected again.
// heldFault is a fault kept open while its integration looks healthy again,
// until the recovery has lasted long enough to believe.
type heldFault struct {
	obs     Observation
	lastBad time.Time
}

type integrationStatusCollector struct {
	Deps
	mu     sync.Mutex
	faults map[uuid.UUID]heldFault
}

func (c *integrationStatusCollector) Name() string            { return "integrations" }
func (c *integrationStatusCollector) Interval() time.Duration { return time.Minute }

// Connections bounce. A reconnecting integration flips unreachable → connected
// → unreachable within a minute or two, and reported raw that becomes a
// signal that fires, auto-resolves and fires again — every cycle a pair of
// pushes. So the state has to hold before it counts:
//
//   - a fault must persist for alertDwell before it is reported at all, which
//     drops every short blip on the floor;
//   - once reported, it keeps being reported through brief recoveries until
//     the connection has held for recoveryDwell, so a flapping link stays ONE
//     open condition instead of a stream of new ones.
const (
	alertDwell    = 3 * time.Minute
	recoveryDwell = 15 * time.Minute
)

func (c *integrationStatusCollector) Collect(ctx context.Context) ([]Observation, error) {
	rows, err := c.Store.ListIntegrations(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Observation
	for _, r := range rows {
		if !r.Enabled {
			c.forget(r.ID)
			continue
		}
		var sev string
		switch r.Status {
		case "unreachable":
			sev = SevCritical
		case "degraded":
			sev = SevWarning
		default:
			// Healthy right now — but hold a recently-reported fault open
			// until the recovery has actually stuck, so a bouncing link does
			// not produce an all-clear it will contradict a minute later.
			if held, ok := c.heldFault(r.ID, now); ok {
				out = append(out, held)
			}
			continue
		}
		// A fault younger than the dwell has not earned an interruption yet;
		// most of them are gone before the next sweep.
		if now.Sub(r.StatusChangedAt) < alertDwell {
			continue
		}
		msg := r.StatusMessage
		if msg == "" {
			msg = r.Status
		}
		obs := Observation{
			Key:          "integ:" + r.ID.String() + ":status",
			Severity:     sev,
			Title:        fmt.Sprintf("%s is %s", r.Name, r.Status),
			Body:         msg,
			SourceType:   r.Type,
			SourceID:     srcID(r.ID),
			SourceName:   r.Name,
			AutoResolve:  true,
			ResolveTitle: fmt.Sprintf("%s is back to normal", r.Name),
			ResolveBody:  "The connection recovered on its own.",
		}
		c.remember(r.ID, obs, now)
		out = append(out, obs)
	}
	return out, nil
}

// remember records the fault so a brief recovery can be ridden out.
func (c *integrationStatusCollector) remember(id uuid.UUID, obs Observation, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.faults == nil {
		c.faults = map[uuid.UUID]heldFault{}
	}
	c.faults[id] = heldFault{obs: obs, lastBad: now}
}

// heldFault reports a fault that should stay open despite the integration
// currently looking healthy, and expires it once the recovery has held.
func (c *integrationStatusCollector) heldFault(id uuid.UUID, now time.Time) (Observation, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	held, ok := c.faults[id]
	if !ok {
		return Observation{}, false
	}
	if now.Sub(held.lastBad) >= recoveryDwell {
		delete(c.faults, id)
		return Observation{}, false
	}
	return held.obs, true
}

func (c *integrationStatusCollector) forget(id uuid.UUID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.faults, id)
}

// ---- TrueNAS alerts ---------------------------------------------------------

// truenasAlertCollector mirrors the NAS's own alert list one-to-one, so the
// bell you already trust on TrueNAS shows up here too. Alerts dismissed on
// the NAS auto-resolve here.
type truenasAlertCollector struct{ Deps }

func (c *truenasAlertCollector) Name() string            { return "truenas" }
func (c *truenasAlertCollector) Interval() time.Duration { return 2 * time.Minute }

func (c *truenasAlertCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "truenas")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue // connection problems are the status collector's story
		}
		tc, ok := conn.(*truenas.Conn)
		if !ok {
			continue
		}
		alerts, aerr := tc.Alerts(ctx)
		if aerr != nil {
			continue
		}
		for _, a := range alerts {
			if a.Dismissed {
				continue
			}
			sev := SevInfo
			switch strings.ToUpper(a.Level) {
			case "WARNING":
				sev = SevWarning
			case "ERROR", "CRITICAL", "ALERT", "EMERGENCY":
				sev = SevCritical
			}
			title := a.Formatted
			if i := strings.IndexAny(title, ".\n"); i > 8 && i < 90 {
				title = title[:i+1]
			}
			if len(title) > 120 {
				title = title[:117] + "…"
			}
			out = append(out, Observation{
				Key:        "truenas:" + inst.ID.String() + ":alert:" + a.UUID,
				Severity:   sev,
				Title:      strings.TrimSpace(title),
				Body:       a.Formatted,
				SourceType: "truenas",
				SourceID:   srcID(inst.ID),
				SourceName: inst.Name,
				Detail: map[string]any{
					"level":  a.Level,
					"raised": time.UnixMilli(a.Datetime.Ms).UTC().Format(time.RFC3339),
					"uuid":   a.UUID,
				},
				AutoResolve: true,
				ResolveBody: "The alert cleared on the NAS.",
			})
		}
	}
	return out, nil
}

// ---- Home Assistant batteries ----------------------------------------------

// haBatteryCollector flags every battery-powered device running low: battery
// sensors at or under 20% (critical at 10%), and binary battery-low sensors
// that report on. One signal per device, green again once it charges.
type haBatteryCollector struct{ Deps }

func (c *haBatteryCollector) Name() string            { return "ha-battery" }
func (c *haBatteryCollector) Interval() time.Duration { return 10 * time.Minute }

func (c *haBatteryCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "homeassistant")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		hc, ok := conn.(*homeassistant.Conn)
		if !ok {
			continue
		}
		for _, e := range hc.Entities() {
			devClass, _ := e.Attributes["device_class"].(string)
			if devClass != "battery" {
				continue
			}
			key := "ha:" + inst.ID.String() + ":battery:" + e.EntityID
			switch e.Domain {
			case "sensor":
				pct, perr := strconv.ParseFloat(e.State, 64)
				if perr != nil || pct > 20 {
					continue
				}
				sev := SevWarning
				if pct <= 10 {
					sev = SevCritical
				}
				out = append(out, Observation{
					Key:      key,
					Severity: sev,
					Title:    fmt.Sprintf("%s at %.0f%%", batteryLabel(e.Name), pct),
					Body:     "Charge or replace the battery soon.",
					Detail: map[string]any{
						"entityId": e.EntityID, "battery": pct,
						"lastChanged": e.LastChanged.UTC().Format(time.RFC3339),
					},
					SourceType: "homeassistant", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("%s recovered", batteryLabel(e.Name)),
					ResolveBody:  "The battery level is back above 20%.",
				})
			case "binary_sensor":
				if e.State != "on" {
					continue
				}
				out = append(out, Observation{
					Key:      key,
					Severity: SevWarning,
					Title:    fmt.Sprintf("%s is low", batteryLabel(e.Name)),
					Body:     "Charge or replace the battery soon.",
					Detail: map[string]any{
						"entityId":    e.EntityID,
						"lastChanged": e.LastChanged.UTC().Format(time.RFC3339),
					},
					SourceType: "homeassistant", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("%s recovered", batteryLabel(e.Name)),
				})
			}
		}
	}
	return out, nil
}

// batteryLabel avoids "<Name> battery at N%" reading as "… Battery battery at N%"
// when the entity name already mentions the battery.
func batteryLabel(name string) string {
	if strings.Contains(strings.ToLower(name), "batter") {
		return name
	}
	return name + " battery"
}

// ---- PatchPilot -------------------------------------------------------------

// patchpilotCollector goes deep: failed host runs surface with their error
// and the tail of the actual console log; finished runs that applied updates
// leave a note; hosts waiting on a reboot stay flagged until they restart.
type patchpilotCollector struct{ Deps }

func (c *patchpilotCollector) Name() string            { return "patchpilot" }
func (c *patchpilotCollector) Interval() time.Duration { return 2 * time.Minute }

func logTail(log string, lines int) []string {
	log = strings.TrimRight(log, "\n")
	if log == "" {
		return nil
	}
	all := strings.Split(log, "\n")
	if len(all) > lines {
		all = all[len(all)-lines:]
	}
	// Keep each line sane for the panel.
	for i, l := range all {
		if len(l) > 300 {
			all[i] = l[:297] + "…"
		}
	}
	return all
}

// lastRunObservation turns a fleet's most recent finished run into the one
// signal about it: a failure names the hosts and carries their errors and
// log tails (fleet-scoped key, so the next clean run resolves it); a clean
// run that installed something is a one-shot note for two days; a clean run
// that did nothing is not news.
func lastRunObservation(inst store.Integration, last store.PatchpilotRun, hostRuns []store.PatchpilotHostRun) (Observation, bool) {
	var failedHosts []map[string]any
	var names []string
	totalPkgs, okHosts := 0, 0
	for _, hr := range hostRuns {
		switch hr.Status {
		case "failed":
			names = append(names, hr.HostName)
			failedHosts = append(failedHosts, map[string]any{
				"host":    hr.HostName,
				"error":   sentence(hr.ErrorSummary),
				"log":     logTail(hr.Log, 25),
				"exit":    hr.ExitCode,
				"started": hr.StartedAt,
			})
		case "success":
			okHosts++
			totalPkgs += hr.PackagesUpgraded
		}
	}
	src := Observation{SourceType: "patchpilot", SourceID: srcID(inst.ID), SourceName: inst.Name}
	switch {
	case last.Status == "failed" || len(failedHosts) > 0:
		sev := SevWarning
		if okHosts == 0 && len(failedHosts) > 0 {
			sev = SevCritical // the whole run burned
		}
		var title string
		switch {
		case len(failedHosts) == 0:
			title = "Last update run did not complete"
		case len(failedHosts) == 1:
			title = "Last update run failed on " + names[0]
		case len(failedHosts) == len(hostRuns):
			title = fmt.Sprintf("Last update run failed on all %d hosts", len(hostRuns))
		default:
			title = fmt.Sprintf("Last update run failed on %d of %d hosts", len(failedHosts), len(hostRuns))
		}
		body := fmt.Sprintf("%s run, %s: %s. Per-host errors and log tails are in the detail.",
			sentence(last.Trigger), last.StartedAt.Local().Format("2 Jan 15:04"), strings.Join(names, ", "))
		if len(failedHosts) == 0 {
			body = fmt.Sprintf("%s run, %s: %s.", sentence(last.Trigger), last.StartedAt.Local().Format("2 Jan 15:04"),
				firstNonEmpty(last.Summary, "the run did not complete"))
		}
		o := src
		o.Key = "pp:" + inst.ID.String() + ":lastrun"
		o.Severity = sev
		o.Title = title
		o.Body = body
		o.Detail = map[string]any{
			"kind":        "last-run",
			"runId":       last.ID,
			"trigger":     last.Trigger,
			"startedAt":   last.StartedAt,
			"failedHosts": failedHosts,
			"okHosts":     okHosts,
			"totalHosts":  len(hostRuns),
		}
		o.AutoResolve = true
		o.ResolveTitle = "Last update run succeeded"
		o.ResolveBody = "The most recent run finished without failures."
		return o, true
	case totalPkgs > 0 && last.FinishedAt != nil && time.Since(*last.FinishedAt) <= 48*time.Hour:
		o := src
		o.Key = "pp:" + inst.ID.String() + ":run:" + last.ID.String()
		o.Severity = SevInfo
		o.Title = fmt.Sprintf("Updates applied: %d packages across %d hosts", totalPkgs, okHosts)
		o.Body = fmt.Sprintf("A %s update run finished cleanly.", last.Trigger)
		o.Detail = map[string]any{"runId": last.ID, "trigger": last.Trigger, "packages": totalPkgs, "hosts": okHosts}
		return o, true
	}
	return Observation{}, false
}

func firstNonEmpty(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// lastRunChanged reports whether the last-run row on record — live, or
// dismissed and still standing as the acknowledgement — is about a different
// run than the one just observed. Only then is the observation a recurrence:
// a newer failure the operator has not seen.
func (c *patchpilotCollector) lastRunChanged(ctx context.Context, key string, runID uuid.UUID) bool {
	row, _ := c.Store.GetLiveSignal(ctx, key)
	if row == nil {
		row, _ = c.Store.GetSuppressedSignal(ctx, key)
	}
	if row == nil {
		return false
	}
	var d struct {
		RunID uuid.UUID `json:"runId"`
	}
	if err := json.Unmarshal(row.Detail, &d); err != nil {
		return true
	}
	return d.RunID != runID
}

// sentence capitalises the first letter, so texts assembled from lower-case
// error strings read as sentences in the panel.
func sentence(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

func (c *patchpilotCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "patchpilot")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		// One signal per fleet about the most recent finished run, and only
		// that one: a failed run three days ago is history the moment a newer
		// run has happened. The key is fleet-scoped so a later success flips
		// the same row to recovered instead of leaving a trail of one-shots;
		// a *new* failed run after the operator dismissed the previous one
		// recurs (a fresh row), a re-observation of the same failed run does
		// not — the run id in the detail is how the two are told apart.
		runs, rerr := c.Store.ListPatchpilotRuns(ctx, inst.ID, 15)
		if rerr != nil {
			continue
		}
		var last *store.PatchpilotRun
		for i := range runs {
			if runs[i].FinishedAt != nil {
				last = &runs[i]
				break
			}
		}
		if last == nil {
			continue
		}
		hostRuns, herr := c.Store.ListPatchpilotHostRuns(ctx, last.ID)
		if herr != nil {
			continue
		}
		if o, ok := lastRunObservation(inst, *last, hostRuns); ok {
			if o.AutoResolve {
				o.Recur = c.lastRunChanged(ctx, o.Key, last.ID)
			}
			out = append(out, o)
		}

		// Hosts waiting on a reboot.
		hosts, herr := c.Store.ListPatchpilotHosts(ctx, inst.ID)
		if herr != nil {
			continue
		}
		for _, h := range hosts {
			if !h.RebootRequired || !h.Enabled {
				continue
			}
			out = append(out, Observation{
				Key:        "pp:" + inst.ID.String() + ":reboot:" + h.ID.String(),
				Severity:   SevInfo,
				Title:      fmt.Sprintf("%s is waiting for a reboot", h.Name),
				Body:       "Updates were installed that need a restart to take effect.",
				Detail:     map[string]any{"hostId": h.ID, "host": h.Name, "os": h.OSPretty},
				SourceType: "patchpilot", SourceID: srcID(inst.ID), SourceName: inst.Name,
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("%s rebooted", h.Name),
				ResolveBody:  "The pending reboot happened; nothing left to do.",
			})
		}
	}
	return out, nil
}

// ---- backups ----------------------------------------------------------------

// backupCollector watches the app's own backup history: a target whose most
// recent run failed stays critical until a good one lands (then green), and
// a target quiet for over a week gets a warning.
type backupCollector struct{ Deps }

func (c *backupCollector) Name() string            { return "backups" }
func (c *backupCollector) Interval() time.Duration { return 10 * time.Minute }

func (c *backupCollector) Collect(ctx context.Context) ([]Observation, error) {
	runs, err := c.Store.ListBackupRuns(ctx, 200)
	if err != nil {
		return nil, err
	}
	// Only report on targets that STILL EXIST. Run history outlives a deleted
	// target, so without this a removed target's last (failed) run would
	// resurrect a phantom "Backups to X are failing" forever.
	targets, terr := c.Store.ListBackupTargets(ctx)
	if terr != nil {
		return nil, terr
	}
	live := make(map[string]bool, len(targets))
	for _, tg := range targets {
		live[tg.Name] = true
	}
	type state struct {
		lastStatus  string
		lastError   string
		lastAt      time.Time
		lastSuccess time.Time
	}
	byTarget := map[string]*state{}
	for _, r := range runs { // newest first
		st, ok := byTarget[r.TargetName]
		if !ok {
			st = &state{lastStatus: r.Status, lastError: r.Error, lastAt: r.StartedAt}
			byTarget[r.TargetName] = st
		}
		if r.Status == "success" && st.lastSuccess.IsZero() {
			st.lastSuccess = r.StartedAt
		}
	}
	var out []Observation
	for name, st := range byTarget {
		if name == "" || !live[name] {
			continue // unnamed, or the target was deleted — don't flag ghosts
		}
		if st.lastStatus == "failed" {
			body := st.lastError
			if body == "" {
				body = "The most recent backup run failed."
			}
			out = append(out, Observation{
				Key:        "backup:" + name + ":failing",
				Severity:   SevCritical,
				Title:      fmt.Sprintf("Backups to %s are failing", name),
				Body:       body,
				Detail:     map[string]any{"target": name, "lastRun": st.lastAt, "lastSuccess": st.lastSuccess},
				SourceType: "system", SourceName: "Backups",
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("Backups to %s recovered", name),
				ResolveBody:  "A backup completed successfully.",
			})
		} else if !st.lastSuccess.IsZero() && time.Since(st.lastSuccess) > 7*24*time.Hour {
			out = append(out, Observation{
				Key:        "backup:" + name + ":stale",
				Severity:   SevWarning,
				Title:      fmt.Sprintf("No successful backup to %s in over a week", name),
				Body:       fmt.Sprintf("Last success: %s.", st.lastSuccess.Format("Jan 2, 15:04")),
				Detail:     map[string]any{"target": name, "lastSuccess": st.lastSuccess},
				SourceType: "system", SourceName: "Backups",
				AutoResolve: true,
				ResolveBody: "A recent backup landed.",
			})
		}
	}
	return out, nil
}

// ---- Proxmox ----------------------------------------------------------------

// proxmoxCollector checks node membership and storage headroom: an offline
// node is critical; storage over 85% warns, over 95% is critical.
type proxmoxCollector struct{ Deps }

func (c *proxmoxCollector) Name() string            { return "proxmox" }
func (c *proxmoxCollector) Interval() time.Duration { return 5 * time.Minute }

func (c *proxmoxCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "proxmox")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		pc, ok := conn.(*proxmox.Conn)
		if !ok {
			continue
		}
		status, serr := pc.Client.ClusterStatus(ctx)
		if serr != nil {
			continue
		}
		for _, entry := range status {
			if entry.Type != "node" {
				continue
			}
			if !bool(entry.Online) {
				out = append(out, Observation{
					Key:        "pve:" + inst.ID.String() + ":node:" + entry.Name,
					Severity:   SevCritical,
					Title:      fmt.Sprintf("Node %s is offline", entry.Name),
					Body:       "The cluster reports this node as unreachable.",
					SourceType: "proxmox", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("Node %s is back online", entry.Name),
				})
				continue
			}
			pools, perr := pc.Client.NodeStorage(ctx, entry.Name)
			if perr != nil {
				continue
			}
			for _, p := range pools {
				if p.Total == 0 || !bool(p.Active) {
					continue
				}
				frac := float64(int64(p.Used)) / float64(int64(p.Total))
				if frac < 0.85 {
					continue
				}
				sev := SevWarning
				if frac >= 0.95 {
					sev = SevCritical
				}
				out = append(out, Observation{
					Key:      "pve:" + inst.ID.String() + ":storage:" + entry.Name + "/" + p.Storage,
					Severity: sev,
					Title:    fmt.Sprintf("Storage %s on %s is %.0f%% full", p.Storage, entry.Name, frac*100),
					Body:     fmt.Sprintf("%s of %s used.", formatBytes(int64(p.Used)), formatBytes(int64(p.Total))),
					Detail: map[string]any{
						"node": entry.Name, "storage": p.Storage,
						"used": p.Used, "total": p.Total, "type": p.Type,
					},
					SourceType: "proxmox", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("Storage %s on %s has headroom again", p.Storage, entry.Name),
				})
			}
		}
	}
	return out, nil
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ---- Docker -----------------------------------------------------------------

// dockerCollector flags containers stuck restarting and ones that exited
// with a non-zero code; both clear on their own once the container runs.
type dockerCollector struct{ Deps }

func (c *dockerCollector) Name() string            { return "docker" }
func (c *dockerCollector) Interval() time.Duration { return 3 * time.Minute }

func parseExitCode(status string) (int, bool) {
	// "Exited (137) 2 hours ago"
	i := strings.Index(status, "(")
	j := strings.Index(status, ")")
	if i < 0 || j <= i {
		return 0, false
	}
	n, err := strconv.Atoi(status[i+1 : j])
	if err != nil {
		return 0, false
	}
	return n, true
}

func (c *dockerCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "docker")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		dc, ok := conn.(*docker.Conn)
		if !ok {
			continue
		}
		list, lerr := dc.Client.ContainerList(ctx, containertypes.ListOptions{All: true})
		if lerr != nil {
			continue
		}
		for _, item := range list {
			name := docker.ResourceName(item.Names, item.ID)
			key := "docker:" + inst.ID.String() + ":container:" + name
			switch item.State {
			case "restarting":
				out = append(out, Observation{
					Key:        key,
					Severity:   SevWarning,
					Title:      fmt.Sprintf("Container %s is stuck restarting", name),
					Body:       item.Status,
					Detail:     map[string]any{"container": name, "state": item.State, "status": item.Status, "image": item.Image},
					SourceType: "docker", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("Container %s settled", name),
				})
			case "exited":
				code, ok := parseExitCode(item.Status)
				if !ok || code == 0 {
					continue // clean exits are normal for one-shots
				}
				out = append(out, Observation{
					Key:        key,
					Severity:   SevWarning,
					Title:      fmt.Sprintf("Container %s exited with code %d", name, code),
					Body:       item.Status,
					Detail:     map[string]any{"container": name, "exitCode": code, "status": item.Status, "image": item.Image},
					SourceType: "docker", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("Container %s is running again", name),
				})
			}
		}
	}
	return out, nil
}

// ---- Tailscale --------------------------------------------------------------

// tailscaleCollector warns when a device's node key approaches expiry (the
// thing that silently drops machines off the tailnet) and mentions devices
// with a pending client update.
type tailscaleCollector struct{ Deps }

func (c *tailscaleCollector) Name() string            { return "tailscale" }
func (c *tailscaleCollector) Interval() time.Duration { return time.Hour }

func (c *tailscaleCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "tailscale")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		tc, ok := conn.(*tailscale.Conn)
		if !ok {
			continue
		}
		devices, derr := tc.Devices(ctx)
		if derr != nil {
			continue
		}
		for _, dev := range devices {
			if dev.KeyExpiryDisabled || dev.Expires.IsZero() {
				continue
			}
			left := time.Until(dev.Expires.Time)
			key := "ts:" + inst.ID.String() + ":keyexpiry:" + dev.ID
			detail := map[string]any{"device": dev.Name, "expires": dev.Expires.Time, "lastSeen": dev.LastSeen.Time}

			// A device that both dropped off long ago AND has an expired key is
			// dormant, not urgent — it already left the tailnet weeks/months ago
			// and nothing depending on it broke. Surface it as a low-severity
			// cleanup nudge, not a critical alarm.
			seenAgo := time.Duration(0)
			dormant := false
			if !dev.LastSeen.IsZero() {
				seenAgo = time.Since(dev.LastSeen.Time)
				dormant = seenAgo >= dormantAfter
			}
			if dormant {
				if left > 0 {
					continue // dormant but key still valid — nothing to say yet
				}
				out = append(out, Observation{
					Key:      key,
					Severity: SevInfo,
					Title:    fmt.Sprintf("%s is dormant on Tailscale", dev.Name),
					Body: fmt.Sprintf("Last connected %s ago and its key has since expired, so it's no longer on the tailnet. "+
						"Remove it in the Tailscale admin console, or re-authenticate it if you still use it.", humanAgo(seenAgo)),
					Detail:     detail,
					SourceType: "tailscale", SourceID: srcID(inst.ID), SourceName: inst.Name,
					AutoResolve:  true,
					ResolveTitle: fmt.Sprintf("%s is back on Tailscale", dev.Name),
				})
				continue
			}

			// Still-active device: the genuinely actionable window.
			if left > 7*24*time.Hour {
				continue
			}
			sev := SevWarning
			title := fmt.Sprintf("%s's key expires in %s", dev.Name, humanDuration(left))
			body := "Re-authenticate the device (or disable key expiry) to keep it on the tailnet."
			switch {
			case left <= 0:
				// Recently active but just expired — re-auth to bring it back.
				title = fmt.Sprintf("%s's key has expired", dev.Name)
			case left < 48*time.Hour:
				// Still connected, about to drop — the most time-sensitive case.
				sev = SevCritical
			}
			out = append(out, Observation{
				Key:        key,
				Severity:   sev,
				Title:      title,
				Body:       body,
				Detail:     detail,
				SourceType: "tailscale", SourceID: srcID(inst.ID), SourceName: inst.Name,
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("%s's key was renewed", dev.Name),
			})
		}
	}
	return out, nil
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= 48*time.Hour {
		return fmt.Sprintf("%d days", int(d.Hours()/24))
	}
	if d >= 2*time.Hour {
		return fmt.Sprintf("%d hours", int(d.Hours()))
	}
	return fmt.Sprintf("%d minutes", int(d.Minutes()))
}

// dormantAfter is how long a Tailscale device can go unseen before an expired
// key is treated as "dormant" (a low-severity cleanup nudge) rather than an
// urgent warning — it already left the tailnet, so it isn't breaking anything.
const dormantAfter = 14 * 24 * time.Hour

// humanAgo renders an elapsed duration coarsely ("3 days", "2 months").
func humanAgo(d time.Duration) string {
	days := int(d.Hours() / 24)
	switch {
	case days >= 60:
		return fmt.Sprintf("%d months", days/30)
	case days >= 1:
		return fmt.Sprintf("%d days", days)
	default:
		return "less than a day"
	}
}

// shortAgo renders a recent duration at outage resolution ("12 minutes",
// "3 hours") — humanAgo's day-level coarseness reads uselessly for downtime.
func shortAgo(d time.Duration) string {
	switch {
	case d < 2*time.Minute:
		return "moments"
	case d < 2*time.Hour:
		return fmt.Sprintf("%d minutes", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d hours", int(d.Hours()))
	default:
		return humanAgo(d)
	}
}

// ---- Aperture (NVR) ---------------------------------------------------------

// apertureCollector watches camera health: not delivering live video,
// expected-but-broken recording, or a stale watchdog.
type apertureCollector struct{ Deps }

func (c *apertureCollector) Name() string            { return "aperture" }
func (c *apertureCollector) Interval() time.Duration { return 5 * time.Minute }

func (c *apertureCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "aperture")
	if err != nil {
		return nil, err
	}
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		ac, ok := conn.(*aperture.Conn)
		if !ok {
			continue
		}
		health, herr := ac.Status(ctx)
		if herr != nil {
			continue
		}
		for _, cam := range health {
			if !cam.Enabled {
				continue
			}
			var problems []string
			if !cam.LiveOk {
				problems = append(problems, "no live video")
			}
			if cam.RecordingExpected && !cam.RecordingOk {
				problems = append(problems, "recording broken")
			}
			if cam.WatchdogStale {
				problems = append(problems, "watchdog stale")
			}
			if len(problems) == 0 {
				continue
			}
			sort.Strings(problems)
			out = append(out, Observation{
				Key:        "nvr:" + inst.ID.String() + ":camera:" + cam.CameraID,
				Severity:   SevWarning,
				Title:      fmt.Sprintf("Camera %s: %s", cam.Name, strings.Join(problems, ", ")),
				Body:       fmt.Sprintf("Coverage today: %d%%. Restarts today: %d.", cam.CoveragePercent, cam.RestartsToday),
				Detail:     map[string]any{"camera": cam.Name, "problems": problems, "coverage": cam.CoveragePercent, "restarts": cam.RestartsToday},
				SourceType: "aperture", SourceID: srcID(inst.ID), SourceName: inst.Name,
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("Camera %s is healthy again", cam.Name),
			})
		}
	}
	return out, nil
}

// ---- minecraft: per-server offline detection ---------------------------------

// minecraftCollector signals each paired member server whose companion has
// been gone longer than a short grace window. The instance-level status
// collector only says "5 of 6 servers connected" — this one names the server.
// The grace window keeps routine restarts (nightly reboots, /restart) from
// paging anyone; a signal fires only for sustained downtime and auto-resolves
// the sweep after the companion returns.
type minecraftCollector struct {
	Deps
	mu           sync.Mutex
	offlineSince map[uuid.UUID]time.Time
}

const mcOfflineGrace = 2 * time.Minute

func (c *minecraftCollector) Name() string            { return "minecraft" }
func (c *minecraftCollector) Interval() time.Duration { return time.Minute }

func (c *minecraftCollector) Collect(ctx context.Context) ([]Observation, error) {
	insts, err := c.instances(ctx, "minecraft")
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.offlineSince == nil {
		c.offlineSince = map[uuid.UUID]time.Time{}
	}
	c.mu.Unlock()

	now := time.Now()
	var out []Observation
	for _, inst := range insts {
		conn, cerr := c.Manager.Connection(inst.ID)
		if cerr != nil {
			continue
		}
		mc, ok := conn.(*minecraft.Conn)
		if !ok {
			continue
		}
		servers, serr := mc.ServerStates(ctx)
		if serr != nil {
			continue
		}
		for _, server := range servers {
			if !server.Paired {
				continue // still in setup, not an outage
			}
			c.mu.Lock()
			if server.Connected {
				delete(c.offlineSince, server.ID)
				c.mu.Unlock()
				continue
			}
			since, seen := c.offlineSince[server.ID]
			if !seen {
				since = now
				c.offlineSince[server.ID] = since
			}
			c.mu.Unlock()
			if now.Sub(since) < mcOfflineGrace {
				continue // restart grace — don't page for a quick bounce
			}
			body := "The companion has not reconnected."
			if server.LastConnectedAt != nil {
				body = fmt.Sprintf("Last connected %s ago.", shortAgo(now.Sub(*server.LastConnectedAt)))
			}
			instID := inst.ID
			out = append(out, Observation{
				Key:          "mc:" + inst.ID.String() + ":server:" + server.ID.String() + ":offline",
				Severity:     SevWarning,
				Title:        fmt.Sprintf("Minecraft server %s is offline", server.Label),
				Body:         body,
				SourceType:   "minecraft",
				SourceID:     &instID,
				SourceName:   inst.Name,
				AutoResolve:  true,
				ResolveTitle: fmt.Sprintf("Minecraft server %s is back online", server.Label),
				ResolveBody:  "The companion reconnected.",
				Detail:       map[string]any{"serverId": server.ID, "label": server.Label},
			})
		}
	}
	return out, nil
}
