package signals

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/TheOriUHD/homehub/internal/store"
)

func lastRunFixture(status string, hosts ...store.PatchpilotHostRun) (store.Integration, store.PatchpilotRun) {
	inst := store.Integration{ID: uuid.New(), Name: "Fleet"}
	fin := time.Now().Add(-time.Minute)
	run := store.PatchpilotRun{ID: uuid.New(), IntegrationID: inst.ID, Trigger: "manual", Status: status,
		StartedAt: time.Date(2026, 9, 4, 11, 38, 0, 0, time.Local), FinishedAt: &fin}
	return inst, run
}

func hostRun(name, status string, pkgs int, errSummary string) store.PatchpilotHostRun {
	return store.PatchpilotHostRun{HostName: name, Status: status, PackagesUpgraded: pkgs, ErrorSummary: errSummary}
}

func TestLastRunObservationNamesTheFailedHostsAsSentences(t *testing.T) {
	inst, run := lastRunFixture("partial")
	o, ok := lastRunObservation(inst, run, []store.PatchpilotHostRun{
		hostRun("AdGuard Home LXC", "failed", 0, "authentication failed — the fleet key was not accepted"),
		hostRun("WebHost LXC", "failed", 0, "could not reach 192.168.10.235:22 — no route to host"),
		hostRun("Minecraft VM", "success", 3, ""),
	})
	if !ok {
		t.Fatal("a run with failures must produce an observation")
	}
	if o.Key != "pp:"+inst.ID.String()+":lastrun" || !o.AutoResolve {
		t.Fatalf("expected the fleet-scoped auto-resolving key, got %q auto=%v", o.Key, o.AutoResolve)
	}
	if o.Severity != SevWarning {
		t.Fatalf("one host still succeeded, so warning, got %s", o.Severity)
	}
	if o.Title != "Last update run failed on 2 of 3 hosts" {
		t.Fatalf("title: %q", o.Title)
	}
	if !strings.HasPrefix(o.Body, "Manual run, 4 Sep 11:38: AdGuard Home LXC, WebHost LXC.") {
		t.Fatalf("body: %q", o.Body)
	}
	d := o.Detail.(map[string]any)
	if d["kind"] != "last-run" || d["runId"] != run.ID || d["okHosts"] != 1 || d["totalHosts"] != 3 {
		t.Fatalf("detail: %+v", d)
	}
	failed := d["failedHosts"].([]map[string]any)
	if failed[0]["error"] != "Authentication failed — the fleet key was not accepted" ||
		failed[1]["error"] != "Could not reach 192.168.10.235:22 — no route to host" {
		t.Fatalf("errors should read as sentences: %v / %v", failed[0]["error"], failed[1]["error"])
	}
	if o.ResolveTitle != "Last update run succeeded" {
		t.Fatalf("resolve title: %q", o.ResolveTitle)
	}
}

func TestLastRunObservationSeverityAndTitleShapes(t *testing.T) {
	inst, run := lastRunFixture("failed")
	o, _ := lastRunObservation(inst, run, []store.PatchpilotHostRun{
		hostRun("a", "failed", 0, "x"), hostRun("b", "failed", 0, "y"),
	})
	if o.Severity != SevCritical || o.Title != "Last update run failed on all 2 hosts" {
		t.Fatalf("whole run burned: %s %q", o.Severity, o.Title)
	}
	o, _ = lastRunObservation(inst, run, []store.PatchpilotHostRun{
		hostRun("only", "failed", 0, "x"), hostRun("fine", "success", 0, ""),
	})
	if o.Title != "Last update run failed on only" {
		t.Fatalf("single host: %q", o.Title)
	}
	// An aborted run: hosts skipped, none failed — still the last run, and it did not finish.
	run.Summary = "Aborted by operator — 0 succeeded, 6 before abort"
	o, ok := lastRunObservation(inst, run, []store.PatchpilotHostRun{
		hostRun("a", "skipped", 0, "run aborted"), hostRun("b", "skipped", 0, "run aborted"),
	})
	if !ok || o.Title != "Last update run did not complete" || !strings.Contains(o.Body, "Aborted by operator") {
		t.Fatalf("aborted run: ok=%v title=%q body=%q", ok, o.Title, o.Body)
	}
}

func TestLastRunObservationCleanRuns(t *testing.T) {
	inst, run := lastRunFixture("success")
	o, ok := lastRunObservation(inst, run, []store.PatchpilotHostRun{
		hostRun("a", "success", 4, ""), hostRun("b", "success", 1, ""),
	})
	if !ok || o.Severity != SevInfo || o.AutoResolve || !strings.HasPrefix(o.Key, "pp:"+inst.ID.String()+":run:") {
		t.Fatalf("a clean run that installed packages is a one-shot info note, got ok=%v %+v", ok, o)
	}
	if o.Title != "Updates applied: 5 packages across 2 hosts" {
		t.Fatalf("title: %q", o.Title)
	}
	if _, ok := lastRunObservation(inst, run, []store.PatchpilotHostRun{hostRun("a", "success", 0, "")}); ok {
		t.Fatal("a clean run that did nothing is not news")
	}
}
