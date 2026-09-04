package patchpilot

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"homehub/internal/store"
)

// ErrRunInProgress is returned when a run is already executing for a fleet.
type ErrRunInProgress struct{}

func (ErrRunInProgress) Error() string { return "a run is already in progress for this fleet" }

// StartRun admits and begins a run. Manual runs are rejected while another is
// active; scheduled runs skip (recorded on the schedule) — a human asking
// twice means twice; a cron firing mid-run means "you're already patching".
func (r *Registry) StartRun(ctx context.Context, integrationID uuid.UUID, trigger string, scheduleID *uuid.UUID, hostIDs []uuid.UUID, suppressReboot bool) (*store.PatchpilotRun, error) {
	r.mu.Lock()
	if r.running[integrationID] {
		r.mu.Unlock()
		if trigger == "scheduled" && scheduleID != nil {
			_ = r.store.MarkPatchpilotScheduleSkipped(context.Background(), *scheduleID, time.Now(), "a run was already in progress")
		}
		return nil, ErrRunInProgress{}
	}
	// Double-check the DB in case another process holds it.
	if active, _ := r.store.ActivePatchpilotRun(ctx, integrationID); active != nil {
		r.mu.Unlock()
		return nil, ErrRunInProgress{}
	}
	r.running[integrationID] = true
	r.mu.Unlock()

	run, err := r.store.CreatePatchpilotRun(ctx, store.PatchpilotRun{
		IntegrationID: integrationID, ScheduleID: scheduleID, Trigger: trigger, Status: "running",
	})
	if err != nil {
		r.clearRunning(integrationID)
		return nil, err
	}

	// Resolve + order the hosts now so a bad host list fails synchronously.
	hosts, err := r.orderedHosts(ctx, integrationID, hostIDs)
	if err != nil {
		r.clearRunning(integrationID)
		_ = r.store.FinishPatchpilotRun(ctx, run.ID, "failed", err.Error())
		return nil, err
	}

	// A dedicated cancellable context so an operator can abort the run; DB
	// bookkeeping inside executeRun uses context.Background() so it still
	// records the outcome after this ctx is cancelled.
	runCtx, cancel := context.WithCancel(context.Background())
	r.mu.Lock()
	r.cancels[run.ID] = cancel
	r.mu.Unlock()

	go r.executeRun(runCtx, run, hosts, suppressReboot)
	return run, nil
}

// AbortRun cancels an in-progress run: it kills the active host's command and
// short-circuits the remaining hosts. Returns ErrNoActiveRun if nothing matches.
func (r *Registry) AbortRun(runID uuid.UUID) error {
	r.mu.Lock()
	cancel := r.cancels[runID]
	r.mu.Unlock()
	if cancel == nil {
		return ErrNoActiveRun{}
	}
	cancel()
	return nil
}

// ErrNoActiveRun is returned when an abort targets a run that isn't executing
// in this process (already finished, or a stale id).
type ErrNoActiveRun struct{}

func (ErrNoActiveRun) Error() string { return "no active run to abort" }

func (r *Registry) clearRunning(integrationID uuid.UUID) {
	r.mu.Lock()
	delete(r.running, integrationID)
	r.mu.Unlock()
}

func (r *Registry) forgetCancel(runID uuid.UUID) {
	r.mu.Lock()
	delete(r.cancels, runID)
	delete(r.skips, runID)
	delete(r.hostCancels, runID)
	r.mu.Unlock()
}

// SkipHost takes one host out of an in-progress run: a queued host is never
// started, a host whose command is in flight has HomeHub stop waiting for it,
// and the run moves on to the next host either way. Returns ErrNoActiveRun
// when the run is not executing here.
func (r *Registry) SkipHost(runID, hostID uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancels[runID] == nil {
		return ErrNoActiveRun{}
	}
	if r.skips[runID] == nil {
		r.skips[runID] = map[uuid.UUID]bool{}
	}
	r.skips[runID][hostID] = true
	if cancel := r.hostCancels[runID][hostID]; cancel != nil {
		cancel()
	}
	return nil
}

func (r *Registry) skipped(runID, hostID uuid.UUID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.skips[runID][hostID]
}

// hostContext derives the context one host's commands run under, registered
// so SkipHost can end it, and returns the matching cleanup.
func (r *Registry) hostContext(ctx context.Context, runID, hostID uuid.UUID) (context.Context, func()) {
	hctx, cancel := context.WithCancel(ctx)
	r.mu.Lock()
	if r.hostCancels[runID] == nil {
		r.hostCancels[runID] = map[uuid.UUID]context.CancelFunc{}
	}
	r.hostCancels[runID][hostID] = cancel
	r.mu.Unlock()
	return hctx, func() {
		cancel()
		r.mu.Lock()
		delete(r.hostCancels[runID], hostID)
		r.mu.Unlock()
	}
}

// orderedHosts loads the requested hosts (or all enabled if none named) and
// orders them: standard non-Proxmox first, Proxmox hosts last (their held
// kernels make ordering matter). Disabled hosts are dropped.
func (r *Registry) orderedHosts(ctx context.Context, integrationID uuid.UUID, hostIDs []uuid.UUID) ([]store.PatchpilotHost, error) {
	all, err := r.store.ListPatchpilotHosts(ctx, integrationID)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]store.PatchpilotHost{}
	for _, h := range all {
		byID[h.ID] = h
	}
	var hosts []store.PatchpilotHost
	if len(hostIDs) == 0 {
		for _, h := range all {
			if h.Enabled {
				hosts = append(hosts, h)
			}
		}
	} else {
		for _, id := range hostIDs {
			if h, ok := byID[id]; ok && h.Enabled {
				hosts = append(hosts, h)
			}
		}
	}
	if len(hosts) == 0 {
		return nil, fmt.Errorf("no enabled hosts to run")
	}
	sort.SliceStable(hosts, func(i, j int) bool {
		gi, gj := execGroup(hosts[i]), execGroup(hosts[j])
		if gi != gj {
			return gi < gj
		}
		return hosts[i].Name < hosts[j].Name
	})
	return hosts, nil
}

func execGroup(h store.PatchpilotHost) int {
	if h.IsProxmox {
		return 3
	}
	return 0
}

// executeRun runs each host sequentially (concurrency deliberately off — two
// package managers fighting over a lock is worse than a slow run).
func (r *Registry) executeRun(ctx context.Context, run *store.PatchpilotRun, hosts []store.PatchpilotHost, suppressReboot bool) {
	defer r.clearRunning(run.IntegrationID)
	defer r.forgetCancel(run.ID)
	// DB/WS bookkeeping must survive an abort (which cancels ctx), so it uses a
	// fresh background context; ctx drives only the cancellable host commands.
	bg := context.Background()

	// Pre-create a pending host-run per host so the run shows its full roster
	// from the first tick — the progress denominator and the monitor's host
	// rail are correct immediately, not growing as hosts start.
	hostRuns := make([]*store.PatchpilotHostRun, len(hosts))
	for i, h := range hosts {
		hr, err := r.store.CreatePatchpilotHostRun(bg, store.PatchpilotHostRun{RunID: run.ID, HostID: &hosts[i].ID, HostName: h.Name})
		if err != nil {
			r.logger.Error("create host run", "host", h.Name, "error", err)
			continue
		}
		hostRuns[i] = hr
	}

	var success, failed, skipped int
	for i, h := range hosts {
		hctx, done := r.hostContext(ctx, run.ID, h.ID)
		outcome := r.executeHostRun(hctx, run, hostRuns[i], h, suppressReboot)
		done()
		switch outcome {
		case "success":
			success++
		case "skipped":
			skipped++
		default:
			failed++
		}
	}

	status := "success"
	switch {
	case failed > 0 && success == 0:
		status = "failed"
	case failed > 0 || skipped > 0:
		status = "partial"
	}
	summary := fmt.Sprintf("%d succeeded, %d failed, %d skipped", success, failed, skipped)
	// An operator abort cancels ctx mid-run — surface that plainly instead of a
	// generic "failed", so the history reads honestly.
	if ctx.Err() != nil {
		status = "cancelled"
		summary = fmt.Sprintf("Aborted by operator — %d succeeded, %d before abort", success, failed+skipped)
	}
	_ = r.store.FinishPatchpilotRun(bg, run.ID, status, summary)
	r.publish(run.IntegrationID, "run.event", map[string]any{
		"runId": run.ID, "runStatus": status, "summary": summary,
	})
}

// executeHostRun runs one host's slice and returns its terminal status.
func (r *Registry) executeHostRun(ctx context.Context, run *store.PatchpilotRun, hr *store.PatchpilotHostRun, h store.PatchpilotHost, suppressReboot bool) string {
	if hr == nil { // its row failed to create above
		return "failed"
	}
	// Host bookkeeping survives an abort; only the SSH work below rides ctx.
	bg := context.Background()
	// A host the operator skipped, or one queued behind an abort, is marked
	// skipped (not failed) — it was never attempted.
	if r.skipped(run.ID, h.ID) || ctx.Err() != nil {
		why := "Run aborted"
		if r.skipped(run.ID, h.ID) {
			why = "Skipped by operator"
		}
		_ = r.store.FinishPatchpilotHostRun(bg, hr.ID, "skipped", nil, 0, false, why)
		_ = r.store.SetPatchpilotHostRunStatus(bg, h.ID, "skipped", time.Now())
		r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "skipped", why)
		return "skipped"
	}
	_ = r.store.StartPatchpilotHostRun(bg, hr.ID)
	r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "running", "")

	fail := func(summary string) string {
		_ = r.store.FinishPatchpilotHostRun(bg, hr.ID, "failed", nil, 0, false, summary)
		_ = r.store.SetPatchpilotHostRunStatus(bg, h.ID, "failed", time.Now())
		r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "failed", summary)
		return "failed"
	}

	rctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()

	conn, err := r.connectHost(rctx, &h)
	if err != nil {
		return fail(err.Error())
	}
	defer conn.Close()

	paths := r.resolvedPaths(rctx, conn, h.PackageManager)

	// Stream the upgrade, tee-ing each chunk to the DB log and the WS topic.
	updateCmd := UpdateCommand(h.PackageManager, paths, "sudo -n ")
	r.appendLog(rctx, run, hr.ID, h, "$ "+redactCmd(updateCmd)+"\n")
	code, serr := conn.Stream(rctx, "sh -c "+shellSingleQuote(updateCmd), func(chunk string) {
		r.appendLog(rctx, run, hr.ID, h, chunk)
	})
	if serr != nil {
		// An abort or the deadline ends HomeHub's wait, not the command: the
		// package manager keeps running on the host, and killing dpkg
		// mid-configure would be the real damage. Say so, as its own outcome,
		// so the host does not read as broken.
		const keepsRunning = " — the package manager keeps running on the host until it finishes; the next run waits for it"
		if r.skipped(run.ID, h.ID) {
			msg := "Skipped by operator" + keepsRunning
			_ = r.store.FinishPatchpilotHostRun(bg, hr.ID, "skipped", nil, 0, false, msg)
			_ = r.store.SetPatchpilotHostRunStatus(bg, h.ID, "skipped", time.Now())
			r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "skipped", msg)
			return "skipped"
		}
		if ctx.Err() != nil {
			msg := "Aborted by operator" + keepsRunning
			_ = r.store.FinishPatchpilotHostRun(bg, hr.ID, "cancelled", nil, 0, false, msg)
			_ = r.store.SetPatchpilotHostRunStatus(bg, h.ID, "cancelled", time.Now())
			r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "cancelled", msg)
			return "cancelled"
		}
		if rctx.Err() != nil {
			return fail("Timed out after 30 minutes" + keepsRunning)
		}
		return fail("Update stream error: " + serr.Error())
	}
	if code != 0 {
		return fail(fmt.Sprintf("Package manager exited with code %d", code))
	}

	// Reboot detection. A host only reboots when it opted in (auto_reboot) and
	// the schedule didn't suppress it — otherwise we just report.
	rebootRequired := false
	if !suppressReboot {
		out, rc, _ := conn.Run(rctx, RebootRequiredCommand(h.PackageManager, paths, "sudo -n "))
		_ = out
		rebootRequired = rc == 0
	}
	autoRebooted := false
	if rebootRequired && h.AutoReboot && !suppressReboot {
		r.appendLog(rctx, run, hr.ID, h, "\n[patchpilot] reboot required — auto-rebooting this host\n")
		autoRebooted = true
	}

	// Count what changed via a post-run simulate (cheap; advisory).
	pkgCount := 0
	if dryOut, _, derr := conn.Run(rctx, "sh -c "+shellSingleQuote(DryRunCommand(h.PackageManager, paths, "sudo -n "))); derr == nil {
		// After a successful upgrade the simulate should show ~0; the count
		// we care about is captured in the run log. Leave 0 unless parse finds some.
		pkgCount = ParseDryRun(h.PackageManager, dryOut).PackagesUpgraded
	}

	code0 := 0
	// If we're about to auto-reboot, the reboot resolves the requirement, so
	// record the host as no longer needing one.
	stillNeedsReboot := rebootRequired && !autoRebooted
	_ = r.store.FinishPatchpilotHostRun(bg, hr.ID, "success", &code0, pkgCount, stillNeedsReboot, "")
	_ = r.store.SetPatchpilotHostRunStatus(bg, h.ID, "success", time.Now())
	_ = r.store.SetPatchpilotHostSnapshot(bg, h.ID, pkgCount, stillNeedsReboot, time.Now())
	r.hostEvent(run.IntegrationID, run.ID, hr.ID, h, "success", "")

	// Issue the auto-reboot last — it drops the connection (deferred Close
	// handles the teardown).
	if autoRebooted {
		_, _, _ = conn.Run(rctx, RebootCommand(paths, "sudo -n "))
	}
	return "success"
}

// appendLog persists a log chunk and streams it live.
func (r *Registry) appendLog(ctx context.Context, run *store.PatchpilotRun, hostRunID uuid.UUID, h store.PatchpilotHost, chunk string) {
	if chunk == "" {
		return
	}
	_ = r.store.AppendPatchpilotHostRunLog(ctx, hostRunID, chunk)
	r.publish(run.IntegrationID, "log.line", map[string]any{
		"runId": run.ID, "hostRunId": hostRunID, "hostId": h.ID, "line": chunk,
	})
}

func (r *Registry) hostEvent(integrationID, runID, hostRunID uuid.UUID, h store.PatchpilotHost, status, message string) {
	r.publish(integrationID, "run.event", map[string]any{
		"runId": runID, "hostRunId": hostRunID, "hostId": h.ID, "hostName": h.Name,
		"hostStatus": status, "message": message,
	})
}

// redactCmd shortens the noisy apt one-liner for the log header.
func redactCmd(cmd string) string {
	if i := strings.Index(cmd, "&&"); i > 0 && len(cmd) > 80 {
		return strings.TrimSpace(cmd[:i]) + " && …"
	}
	return cmd
}

// ---- one-off host actions ---------------------------------------------------

// DryRun connects to a host and reports available updates without changing it.
func (r *Registry) DryRun(ctx context.Context, h *store.PatchpilotHost) (*DryRunResult, error) {
	// Bound the whole probe: the caller's HTTP request context carries no
	// deadline, so without this a wedged apt (e.g. a stalled mirror) would keep
	// the handler goroutine alive as long as the client stays connected.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	conn, err := r.connectHost(ctx, h)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	paths := r.resolvedPaths(ctx, conn, h.PackageManager)
	out, _, err := conn.Run(ctx, "sh -c "+shellSingleQuote(DryRunCommand(h.PackageManager, paths, "sudo -n ")))
	if err != nil {
		return nil, err
	}
	res := ParseDryRun(h.PackageManager, out)
	if _, rc, _ := conn.Run(ctx, RebootRequiredCommand(h.PackageManager, paths, "sudo -n ")); rc == 0 {
		res.RebootRequired = true
	}
	_ = r.store.SetPatchpilotHostSnapshot(ctx, h.ID, res.PackagesUpgraded, res.RebootRequired, time.Now())
	return &res, nil
}

// Reboot issues an explicit reboot (only ever on operator request).
func (r *Registry) Reboot(ctx context.Context, h *store.PatchpilotHost) error {
	// Reboot is quick; bound it so a hung transport can't pin the handler.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	conn, err := r.connectHost(ctx, h)
	if err != nil {
		return err
	}
	defer conn.Close()
	paths := r.resolvedPaths(ctx, conn, h.PackageManager)
	// The reboot drops the connection; treat a transport error as success.
	_, _, err = conn.Run(ctx, RebootCommand(paths, "sudo -n "))
	if err == nil || strings.Contains(err.Error(), "EOF") {
		// We rebooted it — the pending reboot is resolved, so clear the flag
		// now instead of waiting for the next check.
		_ = r.store.ClearPatchpilotHostReboot(context.Background(), h.ID)
		r.publish(h.IntegrationID, "hosts-changed", map[string]any{"hostId": h.ID})
		return nil
	}
	return err
}
