import { useEffect, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ChevronRight, History, Terminal } from "lucide-react";
import type { PatchpilotHostRun, PatchpilotRun } from "@/lib/api";
import { live } from "@/lib/ws";
import { cn, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { ppTopic, runStatusTone, usePatchpilotRunDetail, usePatchpilotRuns } from "./shared";

/**
 * PatchpilotRunsPage is the run history of a fleet: every fleet-level run
 * newest-first, each opening a detail drawer that breaks the run down per
 * host with a collapsible log — streaming live while the run is in flight.
 */
export function PatchpilotRunsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/patchpilot/$instanceId" });
  const runs = usePatchpilotRuns(instanceId);
  const [openRun, setOpenRun] = useState<PatchpilotRun | null>(null);

  if (runs.isLoading) return <PageLoader />;
  if (runs.isError) {
    return (
      <Card>
        <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />
      </Card>
    );
  }

  const rows = runs.data ?? [];

  return (
    <div className="space-y-4">
      <h1 className="text-fg text-[17px] font-semibold">{t("patchpilot.runs.title")}</h1>

      {rows.length === 0 ? (
        <Card>
          <EmptyState
            icon={History}
            title={t("patchpilot.runs.empty.title")}
            body={t("patchpilot.runs.empty.body")}
          />
        </Card>
      ) : (
        <Card>
          <Table>
            <THead>
              <TR>
                <TH>{t("patchpilot.runs.col.started")}</TH>
                <TH>{t("patchpilot.runs.col.trigger")}</TH>
                <TH>{t("patchpilot.runs.col.status")}</TH>
                <TH>{t("patchpilot.runs.col.summary")}</TH>
              </TR>
            </THead>
            <TBody>
              {rows.map((run) => (
                <RunRow key={run.id} run={run} onOpen={() => setOpenRun(run)} />
              ))}
            </TBody>
          </Table>
        </Card>
      )}

      <Dialog open={!!openRun} onOpenChange={(o) => !o && setOpenRun(null)}>
        {openRun && <RunDetailDialog key={openRun.id} instanceId={instanceId} listRun={openRun} />}
      </Dialog>
    </div>
  );
}

/** One row of the run history table. */
function RunRow({ run, onOpen }: { run: PatchpilotRun; onOpen: () => void }) {
  const { t } = useTranslation();
  const running = run.status === "running";
  const tone = runStatusTone(run.status);
  const statusLabel = t(`patchpilot.status.${run.status}`, { defaultValue: run.status });

  return (
    <TR interactive onClick={onOpen}>
      <TD className="text-fg text-[13px] whitespace-nowrap">{formatRelative(run.startedAt)}</TD>
      <TD>
        <Badge tone={run.trigger === "scheduled" ? "info" : "neutral"}>
          {t(`patchpilot.runs.trigger.${run.trigger}`, { defaultValue: run.trigger })}
        </Badge>
      </TD>
      <TD>
        <span className="flex items-center gap-2 text-[13px]">
          <StatusDot tone={tone} pulse={running} />
          <span className="text-fg">{statusLabel}</span>
          {running && (
            <span className="text-fg-subtle text-[12px]">{t("patchpilot.runs.running")}</span>
          )}
        </span>
      </TD>
      <TD className="text-fg-muted text-[13px]">{run.summary}</TD>
    </TR>
  );
}

/**
 * RunDetailDialog fetches the full breakdown for one run and streams its logs
 * live while it is running. `running` drives both the 3s poll and the WS
 * subscription; the poll is the source of truth and live lines fill the gap
 * between polls, cleared whenever a fresh poll lands so nothing double-counts.
 */
function RunDetailDialog({ instanceId, listRun }: { instanceId: string; listRun: PatchpilotRun }) {
  const { t } = useTranslation();
  const qc = useQueryClient();

  const [running, setRunning] = useState(listRun.status === "running");
  const detail = usePatchpilotRunDetail(instanceId, listRun.id, running);

  const run = detail.data?.run ?? listRun;
  const hostRuns = detail.data?.hostRuns ?? [];

  // Follow the run's real status from the detail poll — start/stop live mode.
  useEffect(() => {
    const status = detail.data?.run.status;
    if (status) setRunning(status === "running");
  }, [detail.data?.run.status]);

  // Live-appended lines per hostRun, cleared whenever a fresh poll arrives (the
  // polled log already contains everything captured up to that point).
  const [liveLines, setLiveLines] = useState<Record<string, string[]>>({});
  useEffect(() => {
    setLiveLines({});
  }, [detail.data]);

  useEffect(() => {
    if (!running) return;
    return live.subscribe(ppTopic(instanceId), (payload) => {
      const frame = payload as { event?: string; data?: unknown } | undefined;
      if (frame?.event === "log.line") {
        const d = frame.data as { runId?: string; hostRunId?: string; line?: string } | undefined;
        if (!d || d.runId !== listRun.id || !d.hostRunId) return;
        const hostRunId = d.hostRunId;
        const line = d.line ?? "";
        setLiveLines((prev) => ({ ...prev, [hostRunId]: [...(prev[hostRunId] ?? []), line] }));
      } else if (frame?.event === "run.event") {
        // Nudge the detail query; a terminal runStatus settles on the next poll.
        void qc.invalidateQueries({ queryKey: ["patchpilot", instanceId, "run", listRun.id] });
      }
    });
  }, [running, instanceId, listRun.id, qc]);

  const tone = runStatusTone(run.status);
  const statusLabel = t(`patchpilot.status.${run.status}`, { defaultValue: run.status });

  return (
    <DialogContent title={t("patchpilot.runs.title")} wide>
      {/* Run header */}
      <div className="space-y-3">
        <div className="flex flex-wrap items-center gap-2">
          <StatusDot tone={tone} pulse={running} />
          <span className="text-fg text-[14px] font-semibold">{statusLabel}</span>
          {running && (
            <span className="text-fg-subtle text-[12px]">{t("patchpilot.runs.running")}</span>
          )}
          <Badge tone={run.trigger === "scheduled" ? "info" : "neutral"}>
            {t(`patchpilot.runs.trigger.${run.trigger}`, { defaultValue: run.trigger })}
          </Badge>
        </div>
        <p className="text-fg-muted text-[13px]">{run.summary}</p>
        <p className="text-fg-subtle flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
          <span>
            {t("patchpilot.runs.startedAt", { defaultValue: "Started" })}: {formatRelative(run.startedAt)}
          </span>
          {run.finishedAt && (
            <span>
              {t("patchpilot.runs.finishedAt", { defaultValue: "Finished" })}: {formatRelative(run.finishedAt)}
            </span>
          )}
        </p>
      </div>

      {/* Per-host breakdown */}
      <div className="mt-5 space-y-3">
        <h3 className="text-fg text-[13px] font-semibold">{t("patchpilot.runs.hostRuns")}</h3>
        {detail.isLoading && hostRuns.length === 0 ? (
          <PageLoader />
        ) : detail.isError ? (
          <Card>
            <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />
          </Card>
        ) : (
          hostRuns.map((hostRun, i) => (
            <HostRunCard
              key={hostRun.id ?? i}
              hostRun={hostRun}
              liveLines={liveLines[hostRun.id] ?? []}
            />
          ))
        )}
      </div>
    </DialogContent>
  );
}

/** One host's slice of the run: status header plus a collapsible log. */
function HostRunCard({ hostRun, liveLines }: { hostRun: PatchpilotHostRun; liveLines: string[] }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const tone = runStatusTone(hostRun.status);

  const stored = hostRun.log ?? "";
  const extra = liveLines.length ? liveLines.join("\n") : "";
  const log = [stored, extra].filter(Boolean).join(stored && extra ? "\n" : "");

  return (
    <Card className="overflow-hidden">
      <div className="flex flex-wrap items-center gap-2 px-4 py-3">
        <StatusDot tone={tone} pulse={hostRun.status === "running"} />
        <span className="text-fg text-[13px] font-semibold">{hostRun.hostName}</span>
        <Badge tone="neutral">
          {t("patchpilot.runs.packages", {
            defaultValue: "{{count}} upgraded",
            count: hostRun.packagesUpgraded,
          })}
        </Badge>
        {hostRun.rebootRequired && (
          <Badge tone="warning">
            {t("patchpilot.runs.rebootRequired", { defaultValue: "Reboot required" })}
          </Badge>
        )}
        {hostRun.errorSummary && (
          <span className="text-fg-subtle min-w-0 truncate text-[12px]">{hostRun.errorSummary}</span>
        )}
      </div>

      <div className="border-edge border-t">
        <button
          type="button"
          onClick={() => setOpen((v) => !v)}
          className="text-fg-muted hover:text-fg flex w-full items-center gap-1.5 px-4 py-2 text-left text-[12px] font-medium transition-colors"
        >
          <ChevronRight className={cn("size-3.5 transition-transform", open && "rotate-90")} />
          <Terminal className="size-3.5" />
          {t("patchpilot.runs.log")}
        </button>
        {open && (
          <div className="px-4 pb-3">
            {log ? (
              <pre className="bg-surface max-h-96 overflow-auto rounded-md p-3 font-mono text-[12px] whitespace-pre-wrap">
                {log}
              </pre>
            ) : (
              <p className="text-fg-subtle text-[12px]">{t("patchpilot.runs.noLog")}</p>
            )}
          </div>
        )}
      </div>
    </Card>
  );
}
