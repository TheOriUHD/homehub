import { Fragment, useEffect, useRef, useState, type ReactNode } from "react";
import { Link, useNavigate, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowLeft, ChevronDown, History, Play, Power, Search, Trash2, Wrench } from "lucide-react";
import type { PatchpilotDryRunResult, PatchpilotHostRun } from "@/lib/api";
import { cn, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Textarea } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Switch } from "@/ui/switch";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import {
  managerLabel,
  runStatusTone,
  useDeleteHost,
  useHostAction,
  usePatchpilotHost,
  usePatchpilotHostRuns,
  useUpdateHost,
} from "./shared";

/** Which non-mutation action is currently awaiting its openapi-fetch call. */
type ActionKind = "dryRun" | "run" | "reboot" | "repair";

/**
 * PatchpilotHostDetailPage is one machine's control surface: an action bar
 * (check / update / reboot / repair / remove), an inline settings card, and
 * the host's own slice of every run it has taken part in.
 */
export function PatchpilotHostDetailPage() {
  const { t } = useTranslation();
  const { instanceId, hostId } = useParams({ from: "/shell/patchpilot/$instanceId/host/$hostId" });
  const toast = useToast();
  const navigate = useNavigate();

  const hostQuery = usePatchpilotHost(instanceId, hostId);
  const runsQuery = usePatchpilotHostRuns(instanceId, hostId);
  const action = useHostAction(instanceId);
  const updateHost = useUpdateHost(instanceId);
  const deleteHost = useDeleteHost(instanceId);

  const [busy, setBusy] = useState<ActionKind | null>(null);
  const [confirm, setConfirm] = useState<null | "reboot" | "remove">(null);
  const [dry, setDry] = useState<PatchpilotDryRunResult | null>(null);

  // Notes are a free-text field saved on blur. The poll refreshes the host
  // every 30s; only overwrite the local draft when the user isn't mid-edit.
  const host = hostQuery.data;
  const [notes, setNotes] = useState("");
  const notesDirty = useRef(false);
  useEffect(() => {
    if (!notesDirty.current) setNotes(host?.notes ?? "");
  }, [host?.notes]);

  const errMsg = (e: unknown): string => {
    const m = (e as { message?: unknown } | null | undefined)?.message;
    return typeof m === "string" && m ? m : t("common.unknownError");
  };

  if (hostQuery.isLoading) return <PageLoader />;
  if (hostQuery.isError) {
    return (
      <Card>
        <ErrorState error={hostQuery.error} onRetry={() => void hostQuery.refetch()} />
      </Card>
    );
  }
  if (!host) {
    return (
      <Card>
        <EmptyState icon={History} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  // ---- action-bar handlers (openapi-fetch: `{ data, error }`) ---------------

  const invoke = async (
    kind: ActionKind,
    call: () => Promise<{ data?: unknown; error?: unknown }>,
    onData: (data: unknown) => void,
  ) => {
    setBusy(kind);
    try {
      const { data, error } = await call();
      if (error) {
        toast("error", errMsg(error));
        return;
      }
      onData(data);
      action.invalidate();
    } catch (e) {
      toast("error", errMsg(e));
    } finally {
      setBusy(null);
    }
  };

  const bodyOf = (data: unknown): string | undefined => {
    const d = data as { summary?: unknown; message?: unknown } | null | undefined;
    const v = d?.summary ?? d?.message;
    return typeof v === "string" && v ? v : undefined;
  };

  const doDryRun = () =>
    invoke("dryRun", () => action.dryRun(hostId), (data) => {
      const result = data as PatchpilotDryRunResult;
      setDry(result);
      const title =
        result.packagesUpgraded > 0
          ? t("patchpilot.hostDetail.updatesAvailable", { count: result.packagesUpgraded })
          : t("patchpilot.hostDetail.dryRunUpToDate");
      toast("success", title, result.summary || undefined);
    });

  const doRun = () =>
    invoke("run", () => action.run(hostId), (data) =>
      toast("success", t("patchpilot.hostDetail.run"), bodyOf(data)),
    );

  const doReboot = () =>
    invoke("reboot", () => action.reboot(hostId), (data) => {
      setConfirm(null);
      toast("success", t("patchpilot.hostDetail.reboot"), bodyOf(data));
    });

  const doRepair = () =>
    invoke("repair", () => action.repairSudoers(hostId), (data) =>
      toast("success", t("patchpilot.hostDetail.repair"), bodyOf(data)),
    );

  const doRemove = () =>
    deleteHost.mutate(hostId, {
      onSuccess: () => {
        setConfirm(null);
        void navigate({ to: "/patchpilot/$instanceId", params: { instanceId } });
      },
      onError: (e) => {
        setConfirm(null);
        toast("error", errMsg(e));
      },
    });

  // ---- settings persistence -------------------------------------------------

  const patch = (body: { enabled?: boolean; autoReboot?: boolean; notes?: string }) =>
    updateHost.mutate(
      { hostId, body },
      { onError: (e) => toast("error", errMsg(e)) },
    );

  const saveNotes = () => {
    notesDirty.current = false;
    const next = notes.trim();
    if (next === (host.notes ?? "")) return;
    patch({ notes: next });
  };

  const runs = runsQuery.data ?? [];
  const busyAny = busy !== null;

  return (
    <div className="space-y-5">
      {/* ---- header --------------------------------------------------------- */}
      <div className="space-y-3">
        <Link
          to="/patchpilot/$instanceId"
          params={{ instanceId }}
          className="text-fg-muted hover:text-fg inline-flex items-center gap-1 text-[13px] font-medium transition-colors"
        >
          <ArrowLeft className="size-3.5" />
          {t("patchpilot.hostDetail.back")}
        </Link>

        <div className="min-w-0 space-y-2">
          <div className="flex flex-wrap items-center gap-2.5">
            <h1 className="text-fg flex items-center gap-2 text-xl font-semibold tracking-tight">
              <StatusDot tone={host.enabled ? "success" : "neutral"} />
              {host.name}
            </h1>
            <span className="text-fg-subtle font-mono text-[13px]">
              {host.hostname}:{host.sshPort}
            </span>
            {!host.enabled && <Badge tone="neutral">{t("patchpilot.hosts.disabled")}</Badge>}
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {host.osPretty && <Badge tone="neutral">{host.osPretty}</Badge>}
            <Badge tone="neutral">{managerLabel(host.packageManager)}</Badge>
            {host.arch && <Badge tone="neutral">{host.arch}</Badge>}
            {host.isProxmox && <Badge tone="info">Proxmox</Badge>}
          </div>

          <div className="flex flex-wrap items-center gap-1.5">
            {host.updatesPending > 0 ? (
              <Badge tone="warning">
                {t("patchpilot.hosts.pending", { count: host.updatesPending })}
              </Badge>
            ) : (
              <Badge tone="success">{t("patchpilot.hosts.upToDate")}</Badge>
            )}
            {host.rebootRequired && (
              <Badge tone="warning">{t("patchpilot.hosts.rebootRequired")}</Badge>
            )}
          </div>
        </div>
      </div>

      {/* ---- action bar ---------------------------------------------------- */}
      <Card>
        <CardBody className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="secondary"
              loading={busy === "dryRun"}
              disabled={busyAny}
              onClick={doDryRun}
            >
              <Search className="size-3.5" />
              {busy === "dryRun" ? t("patchpilot.hostDetail.dryRunning") : t("patchpilot.hostDetail.dryRun")}
            </Button>
            <Button variant="primary" loading={busy === "run"} disabled={busyAny} onClick={doRun}>
              <Play className="size-3.5" />
              {busy === "run" ? t("patchpilot.hostDetail.running") : t("patchpilot.hostDetail.run")}
            </Button>
            <Button
              variant="danger-ghost"
              loading={busy === "reboot"}
              disabled={busyAny}
              onClick={() => setConfirm("reboot")}
            >
              <Power className="size-3.5" />
              {t("patchpilot.hostDetail.reboot")}
            </Button>
            <Button variant="ghost" loading={busy === "repair"} disabled={busyAny} onClick={doRepair}>
              <Wrench className="size-3.5" />
              {t("patchpilot.hostDetail.repair")}
            </Button>
            <div className="ml-auto">
              <Button
                variant="danger-ghost"
                disabled={busyAny || deleteHost.isPending}
                onClick={() => setConfirm("remove")}
              >
                <Trash2 className="size-3.5" />
                {t("patchpilot.hostDetail.remove")}
              </Button>
            </div>
          </div>

          {dry && (
            <div className="bg-bg-subtle rounded-lg px-3.5 py-2.5 text-[13px]">
              <p className="text-fg font-medium">
                {dry.packagesUpgraded > 0
                  ? t("patchpilot.hostDetail.updatesAvailable", { count: dry.packagesUpgraded })
                  : t("patchpilot.hostDetail.dryRunUpToDate")}
              </p>
              {dry.summary && <p className="text-fg-muted mt-0.5">{dry.summary}</p>}
            </div>
          )}
        </CardBody>
      </Card>

      {/* ---- settings ------------------------------------------------------ */}
      <Card>
        <CardBody className="space-y-4">
          <SettingRow
            label={t("patchpilot.hostDetail.enabled")}
            hint={t("patchpilot.hostDetail.enabledHint")}
            checked={host.enabled}
            onCheckedChange={(v) => patch({ enabled: v })}
          />
          <SettingRow
            label={t("patchpilot.hostDetail.autoReboot")}
            hint={t("patchpilot.hostDetail.autoRebootHint")}
            checked={host.autoReboot}
            onCheckedChange={(v) => patch({ autoReboot: v })}
          />
          <Field label={t("patchpilot.hostDetail.notes")}>
            {(fieldProps) => (
              <Textarea
                {...fieldProps}
                rows={3}
                value={notes}
                onChange={(e) => {
                  notesDirty.current = true;
                  setNotes(e.target.value);
                }}
                onBlur={saveNotes}
              />
            )}
          </Field>
        </CardBody>
      </Card>

      {/* ---- run history --------------------------------------------------- */}
      <Card className="overflow-hidden">
        <CardHeader title={t("patchpilot.hostDetail.history")} />
        {runs.length === 0 ? (
          <EmptyState icon={History} title={t("patchpilot.hostDetail.noHistory")} />
        ) : (
          <Table>
            <THead>
              <TR>
                <TH>{t("patchpilot.runs.col.started")}</TH>
                <TH>{t("patchpilot.runs.col.status")}</TH>
                <TH className="text-right">{t("patchpilot.hosts.col.updates")}</TH>
                <TH>{t("patchpilot.hostDetail.reboot")}</TH>
                <TH className="text-right">{t("patchpilot.runs.log")}</TH>
              </TR>
            </THead>
            <TBody>
              {runs.map((run, i) => (
                <RunRow key={run.id || `${run.runId}-${i}`} run={run} />
              ))}
            </TBody>
          </Table>
        )}
      </Card>

      {/* ---- confirm dialogs ----------------------------------------------- */}
      <ConfirmModal
        open={confirm === "reboot"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t("patchpilot.hostDetail.reboot")}
        body={t("patchpilot.hostDetail.rebootConfirm", { name: host.name })}
        confirmLabel={t("patchpilot.hostDetail.reboot")}
        loading={busy === "reboot"}
        onConfirm={doReboot}
      />
      <ConfirmModal
        open={confirm === "remove"}
        onOpenChange={(o) => !o && setConfirm(null)}
        title={t("patchpilot.hostDetail.remove")}
        body={t("patchpilot.hostDetail.removeConfirm", { name: host.name })}
        confirmLabel={t("patchpilot.hostDetail.remove")}
        loading={deleteHost.isPending}
        onConfirm={doRemove}
      />
    </div>
  );
}

/** One label/hint + Switch row inside the settings card. */
function SettingRow({
  label,
  hint,
  checked,
  onCheckedChange,
}: {
  label: string;
  hint: string;
  checked: boolean;
  onCheckedChange: (checked: boolean) => void;
}) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        <p className="text-fg text-[13px] font-medium">{label}</p>
        <p className="text-fg-subtle text-[13px]">{hint}</p>
      </div>
      <Switch checked={checked} onCheckedChange={onCheckedChange} className="mt-0.5" />
    </div>
  );
}

/** A host's slice of one run, with an expandable captured-log panel. */
function RunRow({ run }: { run: PatchpilotHostRun }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const tone = runStatusTone(run.status);

  return (
    <Fragment>
      <TR>
        <TD className="text-fg-muted whitespace-nowrap">{formatRelative(run.startedAt)}</TD>
        <TD>
          <span className="inline-flex items-center gap-1.5">
            <StatusDot tone={tone} pulse={run.status === "running"} />
            <span className="text-fg">{t(`patchpilot.status.${run.status}`, { defaultValue: run.status })}</span>
          </span>
          {run.errorSummary && (
            <p className="text-fg-subtle mt-0.5 max-w-md text-[12px]">{run.errorSummary}</p>
          )}
        </TD>
        <TD className="text-fg-muted text-right tabular-nums">{run.packagesUpgraded}</TD>
        <TD>
          {run.rebootRequired ? (
            <Badge tone="warning">{t("patchpilot.hosts.rebootRequired")}</Badge>
          ) : (
            <span className="text-fg-subtle">—</span>
          )}
        </TD>
        <TD className="text-right">
          {run.log ? (
            <Button variant="ghost" size="sm" onClick={() => setOpen((o) => !o)}>
              <ChevronDown className={cn("size-3.5 transition-transform duration-150", open && "rotate-180")} />
              {t("patchpilot.runs.viewLog")}
            </Button>
          ) : (
            <span className="text-fg-subtle">—</span>
          )}
        </TD>
      </TR>
      {open && run.log && (
        <TR>
          <TD colSpan={5} className="py-2">
            <pre className="bg-surface text-fg-muted max-h-80 overflow-auto rounded-lg p-3 font-mono text-[12px] whitespace-pre-wrap">
              {run.log}
            </pre>
          </TD>
        </TR>
      )}
    </Fragment>
  );
}

/**
 * A no-password destructive confirm — a plain Dialog with a danger button,
 * deliberately lighter than ConfirmDialog (which re-authenticates).
 */
function ConfirmModal({
  open,
  onOpenChange,
  title,
  body,
  confirmLabel,
  loading,
  onConfirm,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  body: ReactNode;
  confirmLabel: string;
  loading?: boolean;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={title}
        description={body}
        footer={
          <>
            <Button variant="ghost" onClick={() => onOpenChange(false)}>
              {t("common.cancel")}
            </Button>
            <Button variant="danger" loading={loading} onClick={onConfirm}>
              {confirmLabel}
            </Button>
          </>
        }
      />
    </Dialog>
  );
}
