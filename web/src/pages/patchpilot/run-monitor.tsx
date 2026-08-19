import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { CheckCircle2, ChevronRight, Loader2, Pin, PinOff, ServerCog, XCircle } from "lucide-react";
import { live } from "@/lib/ws";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import type { PatchpilotHostRun } from "@/lib/api";
import { ppTopic, runStatusTone, usePatchpilotRunDetail } from "./shared";

/**
 * RunMonitor is the live "watch the run" surface: a big window with the run's
 * roll-up stats, a rail of every host and its status, and the streaming
 * console of the host currently updating — which auto-follows from host to
 * host as the run advances, unless the operator pins one. It stays useful
 * after the run finishes as the run's full record.
 */
export function RunMonitor({
  instanceId,
  runId,
  onClose,
}: {
  instanceId: string;
  runId: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();

  // Poll while running; a terminal run needs no polling.
  const [running, setRunning] = useState(true);
  const detail = usePatchpilotRunDetail(instanceId, runId, running);
  const run = detail.data?.run;
  const hostRuns = useMemo(() => detail.data?.hostRuns ?? [], [detail.data]);
  useEffect(() => {
    if (run && run.status !== "running") setRunning(false);
    else if (run && run.status === "running") setRunning(true);
  }, [run]);

  // Live log lines since the last poll, keyed by hostRun id. Cleared whenever
  // a fresh poll lands (the DB log then contains everything up to that point),
  // so the two never double-print.
  const [liveLog, setLiveLog] = useState<Record<string, string>>({});
  useEffect(() => setLiveLog({}), [detail.dataUpdatedAt]);
  useEffect(() => {
    return live.subscribe(ppTopic(instanceId), (raw: unknown) => {
      const { event, data } = (raw ?? {}) as { event?: string; data?: Record<string, unknown> };
      if (!data || data.runId !== runId) return;
      if (event === "log.line" && typeof data.hostRunId === "string" && typeof data.line === "string") {
        const hrId = data.hostRunId;
        const line = data.line;
        setLiveLog((prev) => ({ ...prev, [hrId]: (prev[hrId] ?? "") + line }));
      }
      if (event === "run.event") {
        void detail.refetch();
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [instanceId, runId]);

  // Selection: follow the running host automatically unless one is pinned.
  const [pinned, setPinned] = useState<string | null>(null);
  const autoSelected =
    hostRuns.find((h) => h.status === "running") ??
    hostRuns.find((h) => h.status === "pending") ??
    hostRuns[hostRuns.length - 1];
  const selected = hostRuns.find((h) => h.id === (pinned ?? autoSelected?.id)) ?? autoSelected;

  const done = hostRuns.filter((h) => isTerminal(h.status)).length;
  const succeeded = hostRuns.filter((h) => h.status === "success").length;
  const failed = hostRuns.filter((h) => h.status === "failed").length;
  const skipped = hostRuns.filter((h) => h.status === "skipped").length;

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent wide title={t("patchpilot.monitor.title")} className="max-w-5xl">
        {/* Roll-up stats */}
        <div className="border-edge mb-3 flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg border p-3">
          <div className="flex items-center gap-2">
            <StatusDot tone={runStatusTone(run?.status ?? "")} pulse={run?.status === "running"} />
            <span className="text-fg text-sm font-semibold">
              {t(`patchpilot.status.${run?.status ?? "running"}`)}
            </span>
          </div>
          <Stat label={t("patchpilot.monitor.progress")} value={`${done} / ${hostRuns.length}`} />
          <Stat label={t("patchpilot.status.success")} value={String(succeeded)} tone="success" />
          {failed > 0 && <Stat label={t("patchpilot.status.failed")} value={String(failed)} tone="danger" />}
          {skipped > 0 && <Stat label={t("patchpilot.status.skipped")} value={String(skipped)} tone="warning" />}
          {run?.startedAt && <Elapsed startedAt={run.startedAt} finishedAt={run.finishedAt} running={run.status === "running"} />}
          {run?.summary && run.status !== "running" && (
            <span className="text-fg-muted ml-auto text-[13px]">{run.summary}</span>
          )}
        </div>

        <div className="flex gap-3" style={{ height: "60vh" }}>
          {/* Host rail */}
          <div className="no-scrollbar w-56 shrink-0 space-y-1 overflow-y-auto">
            {hostRuns.map((h) => {
              const active = h.id === selected?.id;
              return (
                <button
                  key={h.id}
                  type="button"
                  onClick={() => setPinned(h.id)}
                  className={cn(
                    "flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[13px] transition-colors",
                    active ? "bg-surface-hover text-fg" : "text-fg-muted hover:bg-surface-hover/60",
                  )}
                >
                  <HostStatusIcon status={h.status} />
                  <span className="min-w-0 flex-1 truncate">{h.hostName}</span>
                  {h.status === "success" && h.packagesUpgraded > 0 && (
                    <Badge tone="neutral">{h.packagesUpgraded}</Badge>
                  )}
                  {active && <ChevronRight className="text-fg-subtle size-3.5 shrink-0" />}
                </button>
              );
            })}
          </div>

          {/* Console for the selected host */}
          <div className="border-edge flex min-w-0 flex-1 flex-col rounded-lg border">
            <div className="border-edge flex items-center gap-2 border-b px-3 py-2">
              {selected && <HostStatusIcon status={selected.status} />}
              <span className="text-fg text-[13px] font-semibold">{selected?.hostName ?? "—"}</span>
              {selected && (
                <Badge tone={runStatusTone(selected.status)}>{t(`patchpilot.status.${selected.status}`)}</Badge>
              )}
              {selected?.errorSummary && (
                <span className="text-danger truncate text-[12px]" title={selected.errorSummary}>
                  {selected.errorSummary}
                </span>
              )}
              <div className="ml-auto">
                {pinned ? (
                  <Button variant="ghost" size="sm" onClick={() => setPinned(null)}>
                    <PinOff className="size-3.5" />
                    {t("patchpilot.monitor.following")}
                  </Button>
                ) : (
                  <span className="text-fg-subtle flex items-center gap-1 text-[12px]">
                    <Pin className="size-3" />
                    {t("patchpilot.monitor.autoFollow")}
                  </span>
                )}
              </div>
            </div>
            <Console
              text={(selected?.log ?? "") + (liveLog[selected?.id ?? ""] ?? "")}
              running={selected?.status === "running"}
            />
          </div>
        </div>

        <div className="mt-3 flex justify-end">
          <Button variant="secondary" onClick={onClose}>
            {t("common.close")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function isTerminal(status: string): boolean {
  return status === "success" || status === "failed" || status === "skipped";
}

function HostStatusIcon({ status }: { status: string }) {
  if (status === "running") return <Loader2 className="text-info size-3.5 shrink-0 animate-spin" />;
  if (status === "success") return <CheckCircle2 className="text-success size-3.5 shrink-0" />;
  if (status === "failed") return <XCircle className="text-danger size-3.5 shrink-0" />;
  if (status === "skipped") return <ServerCog className="text-fg-subtle size-3.5 shrink-0" />;
  return <StatusDot tone="neutral" />;
}

function Stat({ label, value, tone }: { label: string; value: string; tone?: "success" | "danger" | "warning" }) {
  return (
    <div className="flex items-baseline gap-1.5">
      <span className="text-fg-subtle text-[11px] font-semibold uppercase tracking-wide">{label}</span>
      <span
        className={cn(
          "text-fg text-sm font-semibold tabular-nums",
          tone === "success" && "text-success",
          tone === "danger" && "text-danger",
          tone === "warning" && "text-warning",
        )}
      >
        {value}
      </span>
    </div>
  );
}

function Elapsed({ startedAt, finishedAt, running }: { startedAt: string; finishedAt?: string | null; running: boolean }) {
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (!running) return;
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, [running]);
  const end = finishedAt ? new Date(finishedAt).getTime() : now;
  const secs = Math.max(0, Math.round((end - new Date(startedAt).getTime()) / 1000));
  const mm = Math.floor(secs / 60);
  const ss = secs % 60;
  return <Stat label="⏱" value={`${mm}:${String(ss).padStart(2, "0")}`} />;
}

/** Console auto-scrolls to the newest output unless the operator scrolled up. */
function Console({ text, running }: { text: string; running: boolean }) {
  const ref = useRef<HTMLPreElement>(null);
  const pinnedBottom = useRef(true);
  useLayoutEffect(() => {
    const el = ref.current;
    if (el && pinnedBottom.current) el.scrollTop = el.scrollHeight;
  }, [text]);
  const onScroll = () => {
    const el = ref.current;
    if (!el) return;
    pinnedBottom.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };
  const { t } = useTranslation();
  return (
    <pre
      ref={ref}
      onScroll={onScroll}
      className="bg-surface text-fg-muted min-h-0 flex-1 overflow-auto rounded-b-lg p-3 font-mono text-[12px] leading-relaxed whitespace-pre-wrap"
    >
      {text || (running ? t("patchpilot.monitor.waiting") : t("patchpilot.runs.noLog"))}
    </pre>
  );
}

export type { PatchpilotHostRun };
