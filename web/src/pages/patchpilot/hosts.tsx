import { useEffect, useState } from "react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Check, Loader2, Play, Plus, ServerCog, X } from "lucide-react";
import { isApiError, type PatchpilotDetectResult, type PatchpilotHost } from "@/lib/api";
import { live } from "@/lib/ws";
import { formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { Radar } from "lucide-react";
import {
  managerLabel,
  ppTopic,
  runStatusTone,
  useDetectHost,
  useOnboardHost,
  usePatchpilotHosts,
  usePatchpilotRuns,
  useRunFleet,
} from "./shared";
import { RunMonitor } from "./run-monitor";

/**
 * PatchpilotHostsPage is the fleet's landing tab: a "run everything" and an
 * "add host" action over the roster table. Each row links into that host's
 * detail view; onboarding a new host is a detect-then-provision flow whose
 * bootstrap steps stream live over the fleet's WS topic.
 */
export function PatchpilotHostsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/patchpilot/$instanceId" });
  const navigate = useNavigate();
  const toast = useToast();

  const hosts = usePatchpilotHosts(instanceId);
  const runFleet = useRunFleet(instanceId);
  const runs = usePatchpilotRuns(instanceId);
  const [adding, setAdding] = useState(false);
  // The run currently shown in the monitor window (null = closed).
  const [monitorRunId, setMonitorRunId] = useState<string | null>(null);

  // A run in progress on this fleet — powers the "View current run" affordance
  // so the monitor is reachable even after it's been closed.
  const activeRun = (runs.data ?? []).find((r) => r.status === "running");

  const runAll = () => {
    runFleet.mutate(
      {},
      {
        onSuccess: (run) => {
          setMonitorRunId(run.id); // pop the live monitor open immediately
          void runs.refetch();
        },
        onError: (err) =>
          toast("error", isApiError(err) ? err.message : t("common.unknownError")),
      },
    );
  };

  if (hosts.isLoading) return <PageLoader />;
  if (hosts.isError) {
    return (
      <Card>
        <ErrorState error={hosts.error} onRetry={() => void hosts.refetch()} />
      </Card>
    );
  }

  const list = hosts.data ?? [];

  const addButton = (
    <Button variant="primary" size="sm" onClick={() => setAdding(true)}>
      <Plus className="size-3.5" />
      {t("patchpilot.hosts.add")}
    </Button>
  );

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-fg flex-1 text-[15px] font-semibold">{t("patchpilot.hosts.title")}</h2>
        <div className="flex items-center gap-1.5">
          {activeRun ? (
            <Button variant="secondary" size="sm" onClick={() => setMonitorRunId(activeRun.id)}>
              <Radar className="size-3.5" />
              {t("patchpilot.monitor.viewCurrent")}
            </Button>
          ) : (
            <Button
              variant="secondary"
              size="sm"
              loading={runFleet.isPending}
              disabled={list.length === 0}
              onClick={runAll}
            >
              <Play className="size-3.5" />
              {t("patchpilot.hosts.runAll")}
            </Button>
          )}
          {addButton}
        </div>
      </div>

      {list.length === 0 ? (
        <Card>
          <EmptyState
            icon={ServerCog}
            title={t("patchpilot.hosts.empty.title")}
            body={t("patchpilot.hosts.empty.body")}
          />
          <div className="flex justify-center pb-6">{addButton}</div>
        </Card>
      ) : (
        <Card className="overflow-hidden">
          <Table>
            <THead>
              <TR>
                <TH>{t("patchpilot.hosts.col.name")}</TH>
                <TH>{t("patchpilot.hosts.col.os")}</TH>
                <TH>{t("patchpilot.hosts.col.manager")}</TH>
                <TH>{t("patchpilot.hosts.col.updates")}</TH>
                <TH>{t("patchpilot.hosts.col.lastRun")}</TH>
                <TH>{t("patchpilot.hosts.col.status")}</TH>
              </TR>
            </THead>
            <TBody>
              {list.map((host) => (
                <HostRow
                  key={host.id}
                  host={host}
                  onOpen={() =>
                    void navigate({
                      to: "/patchpilot/$instanceId/host/$hostId",
                      params: { instanceId, hostId: host.id },
                    })
                  }
                />
              ))}
            </TBody>
          </Table>
        </Card>
      )}

      {adding && <AddHostDialog instanceId={instanceId} onClose={() => setAdding(false)} />}
      {monitorRunId && (
        <RunMonitor instanceId={instanceId} runId={monitorRunId} onClose={() => setMonitorRunId(null)} />
      )}
    </div>
  );
}

/* ---- roster row ------------------------------------------------------------ */

function HostRow({ host, onOpen }: { host: PatchpilotHost; onOpen: () => void }) {
  const { t } = useTranslation();

  return (
    <TR interactive onClick={onOpen}>
      <TD>
        <div className="text-fg font-semibold">{host.name}</div>
        <div className="text-fg-subtle text-[12px]">
          {host.hostname}:{host.sshPort}
        </div>
      </TD>
      <TD className="text-fg-muted">{host.osPretty || "—"}</TD>
      <TD className="text-fg-muted">{managerLabel(host.packageManager)}</TD>
      <TD>
        <div className="flex flex-wrap items-center gap-1.5">
          {host.updatesPending > 0 ? (
            <Badge tone="warning">
              {t("patchpilot.hosts.pending", { count: host.updatesPending })}
            </Badge>
          ) : (
            <span className="text-fg-subtle text-[13px]">{t("patchpilot.hosts.upToDate")}</span>
          )}
          {host.rebootRequired && (
            <Badge tone="warning">{t("patchpilot.hosts.rebootRequired")}</Badge>
          )}
        </div>
      </TD>
      <TD className="text-fg-muted text-[13px]">
        {host.lastRunAt ? formatRelative(host.lastRunAt) : t("patchpilot.hosts.never")}
      </TD>
      <TD>
        {!host.enabled ? (
          <Badge tone="neutral">{t("patchpilot.hosts.disabled")}</Badge>
        ) : host.lastRunStatus ? (
          <span className="inline-flex items-center gap-1.5 text-[13px]">
            <StatusDot tone={runStatusTone(host.lastRunStatus)} />
            <span className="text-fg-muted">
              {t(`patchpilot.runStatus.${host.lastRunStatus}`, { defaultValue: host.lastRunStatus })}
            </span>
          </span>
        ) : (
          <span className="text-fg-subtle text-[13px]">—</span>
        )}
      </TD>
    </TR>
  );
}

/* ---- add-host dialog ------------------------------------------------------- */

type BootstrapStep = { step: string; status: string; message: string };

function detectOk(result: PatchpilotDetectResult | null): boolean {
  return Boolean(result && result.reachable && !result.refusal && !result.blocker);
}

function AddHostDialog({ instanceId, onClose }: { instanceId: string; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();

  const [hostname, setHostname] = useState("");
  const [sshPort, setSshPort] = useState("22");
  const [sshUser, setSshUser] = useState("root");
  const [password, setPassword] = useState("");
  const [rootPassword, setRootPassword] = useState("");
  const [name, setName] = useState("");
  const [result, setResult] = useState<PatchpilotDetectResult | null>(null);
  const [steps, setSteps] = useState<BootstrapStep[]>([]);

  const detect = useDetectHost(instanceId);
  const onboard = useOnboardHost(instanceId);

  // Bootstrap steps stream over the fleet topic for the dialog's lifetime;
  // each event either updates an existing step or appends a new one.
  useEffect(() => {
    return live.subscribe(ppTopic(instanceId), (payload) => {
      const frame = payload as { event?: string; data?: BootstrapStep } | undefined;
      if (frame?.event !== "bootstrap.step" || !frame.data) return;
      const incoming = frame.data;
      setSteps((prev) => {
        const idx = prev.findIndex((s) => s.step === incoming.step);
        if (idx >= 0) {
          const next = prev.slice();
          next[idx] = { step: incoming.step, status: incoming.status, message: incoming.message };
          return next;
        }
        return [...prev, { step: incoming.step, status: incoming.status, message: incoming.message }];
      });
    });
  }, [instanceId]);

  // Any credential edit invalidates a prior detection — force a re-test.
  const resetDetection = () => {
    if (result) setResult(null);
  };

  const credentials = () => ({
    hostname: hostname.trim(),
    sshPort: Number(sshPort) || 22,
    sshUser: sshUser.trim(),
    password: password || undefined,
    rootPassword: rootPassword || undefined,
  });

  const runDetect = () => {
    setResult(null);
    detect.mutate(credentials(), {
      onSuccess: setResult,
      onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
    });
  };

  const runOnboard = () => {
    setSteps([]);
    onboard.mutate(
      { ...credentials(), name: name.trim() },
      {
        onSuccess: () => {
          toast("success", t("patchpilot.addHost.onboarded", { name: name.trim() }));
          onClose();
        },
        onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
      },
    );
  };

  const ok = detectOk(result);
  const canDetect = hostname.trim() !== "" && sshUser.trim() !== "";
  const canOnboard = ok && name.trim() !== "" && hostname.trim() !== "";

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("patchpilot.addHost.title")}
        description={t("patchpilot.addHost.intro")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="secondary"
              disabled={!canDetect}
              loading={detect.isPending}
              onClick={runDetect}
            >
              {t("patchpilot.addHost.detect")}
            </Button>
            <Button
              variant="primary"
              disabled={!canOnboard}
              loading={onboard.isPending}
              onClick={runOnboard}
            >
              {t("patchpilot.addHost.onboard")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-[1fr_7rem]">
            <Field label={t("patchpilot.addHost.hostname")}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  placeholder="server.lan"
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono text-[13px]"
                  value={hostname}
                  onChange={(e) => {
                    setHostname(e.target.value);
                    resetDetection();
                  }}
                />
              )}
            </Field>
            <Field label={t("patchpilot.addHost.port")}>
              {(p) => (
                <Input
                  {...p}
                  type="number"
                  value={sshPort}
                  onChange={(e) => {
                    setSshPort(e.target.value);
                    resetDetection();
                  }}
                />
              )}
            </Field>
          </div>

          <Field label={t("patchpilot.addHost.user")} hint={t("patchpilot.addHost.userHint")}>
            {(p) => (
              <Input
                {...p}
                placeholder="root"
                spellCheck={false}
                autoComplete="off"
                value={sshUser}
                onChange={(e) => {
                  setSshUser(e.target.value);
                  resetDetection();
                }}
              />
            )}
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("patchpilot.addHost.password")} hint={t("patchpilot.addHost.passwordHint")}>
              {(p) => (
                <Input
                  {...p}
                  type="password"
                  autoComplete="new-password"
                  value={password}
                  onChange={(e) => {
                    setPassword(e.target.value);
                    resetDetection();
                  }}
                />
              )}
            </Field>
            <Field
              label={t("patchpilot.addHost.rootPassword")}
              hint={t("patchpilot.addHost.rootPasswordHint")}
              optional={t("common.optional")}
            >
              {(p) => (
                <Input
                  {...p}
                  type="password"
                  autoComplete="new-password"
                  value={rootPassword}
                  onChange={(e) => {
                    setRootPassword(e.target.value);
                    resetDetection();
                  }}
                />
              )}
            </Field>
          </div>

          {result && <DetectResultNote result={result} />}

          {ok && (
            <Field label={t("patchpilot.addHost.name")}>
              {(p) => (
                <Input
                  {...p}
                  placeholder={result?.hostname || hostname}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              )}
            </Field>
          )}

          {steps.length > 0 && (
            <div className="border-edge space-y-2 rounded-lg border p-3">
              <p className="text-fg text-[13px] font-medium">{t("patchpilot.addHost.steps")}</p>
              <ul className="space-y-1.5">
                {steps.map((s, i) => (
                  <li key={`${s.step}-${i}`} className="flex items-start gap-2 text-[13px]">
                    <StepIcon status={s.status} />
                    <span className="text-fg-muted">{s.message}</span>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <details className="text-fg-subtle text-[12px]">
            <summary className="hover:text-fg-muted cursor-pointer">
              {t("patchpilot.addHost.manualKeyTitle")}
            </summary>
            <p className="mt-1.5">{t("patchpilot.addHost.manualKey")}</p>
          </details>
        </div>
      </DialogContent>
    </Dialog>
  );
}

function DetectResultNote({ result }: { result: PatchpilotDetectResult }) {
  const { t } = useTranslation();

  if (!detectOk(result)) {
    return (
      <div className="bg-danger-soft text-danger-soft-fg rounded-md px-3 py-2.5 text-[13px]" role="alert">
        {result.refusal || result.blocker || result.message}
      </div>
    );
  }

  return (
    <div className="bg-success-soft text-success-soft-fg rounded-md px-3 py-2.5 text-[13px]" role="status">
      {t("patchpilot.addHost.detected", {
        os: result.osPretty || result.arch || "—",
        manager: managerLabel(result.packageManager ?? ""),
        escalation: result.escalation ?? "",
      })}
    </div>
  );
}

function StepIcon({ status }: { status: string }) {
  if (status === "ok") return <Check className="text-success mt-0.5 size-4 shrink-0" />;
  if (status === "error") return <X className="text-danger mt-0.5 size-4 shrink-0" />;
  return <Loader2 className="text-fg-muted mt-0.5 size-4 shrink-0 animate-spin" />;
}
