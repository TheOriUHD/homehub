import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  ArrowRightLeft,
  Camera,
  Copy,
  HardDrive,
  Play,
  Power,
  RotateCcw,
  Square,
  Trash2,
} from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type PveGuestAction,
  type PveSnapshot,
  type TermSession,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { cn, formatBytes, formatDateTime, formatDuration } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input, PasswordInput, Textarea } from "@/ui/input";
import { Checkbox, PageLoader } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { ErrorState, EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { lazy, Suspense } from "react";
import { TimeSeriesChart } from "@/components/time-series-chart";

// Consoles pull in xterm.js and noVNC — loaded only when a console opens.
const TerminalPane = lazy(() =>
  import("@/components/terminal").then((m) => ({ default: m.TerminalPane })),
);
const VncViewer = lazy(() =>
  import("@/components/vnc-viewer").then((m) => ({ default: m.VncViewer })),
);
import { useMetricSeries } from "./summary";
import { guestStatusLabel, guestStatusTone } from "./guests";

type TabKey = "overview" | "hardware" | "snapshots" | "backup" | "console" | "ssh";

export function PveGuestDetailPage() {
  const { t } = useTranslation();
  const { instanceId, vmid: vmidRaw } = useParams({ from: "/shell/pve/$instanceId/guests/$vmid" });
  const vmid = Number(vmidRaw);
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const toast = useToast();

  const guestKeys = [["pve", instanceId, "guest", vmid], ["pve", instanceId, "guests"]];
  const detail = useQuery({
    queryKey: ["pve", instanceId, "guest", vmid],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/guests/{vmid}", { params: { path: { id: instanceId, vmid } } })),
  });
  useLiveInvalidate(`integration:proxmox/${instanceId}`, guestKeys, ["guests-changed", "tasks-changed"]);

  const [tab, setTab] = useState<TabKey>("overview");
  const guestRes = `proxmox/${instanceId}/guest/${vmid}`;
  const canControl = can(guestRes, "control");
  const canConfigure = can(guestRes, "configure");
  const canDelete = can(guestRes, "delete");

  const action = useMutation({
    mutationFn: async (a: PveGuestAction) =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/action", {
          params: { path: { id: instanceId, vmid } },
          body: { action: a },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setTimeout(() => void queryClient.invalidateQueries({ queryKey: ["pve", instanceId] }), 1500);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const [confirmStop, setConfirmStop] = useState(false);
  const [confirmDelete, setConfirmDelete] = useState(false);
  const [purge, setPurge] = useState(true);
  const [migrateOpen, setMigrateOpen] = useState(false);
  const [cloneOpen, setCloneOpen] = useState(false);

  const remove = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.DELETE("/proxmox/{id}/guests/{vmid}", {
          params: { path: { id: instanceId, vmid }, query: { purge } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setConfirmDelete(false);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (detail.isLoading) return <PageLoader />;
  if (detail.isError) {
    return (
      <Card>
        <ErrorState error={detail.error} onRetry={() => void detail.refetch()} />
      </Card>
    );
  }
  const guest = detail.data!.guest;
  const running = guest.status === "running";

  const tabs: { key: TabKey; label: string; show: boolean }[] = [
    { key: "overview", label: t("pve.guest.overview"), show: true },
    { key: "hardware", label: t("pve.guest.hardware"), show: true },
    { key: "snapshots", label: t("pve.guest.snapshots"), show: true },
    { key: "backup", label: t("pve.guest.backup"), show: canControl },
    { key: "console", label: t("pve.guest.console"), show: canControl },
    { key: "ssh", label: t("pve.guest.terminal"), show: canControl },
  ];

  return (
    <div className="space-y-5">
      <div>
        <Link
          to="/pve/$instanceId/guests"
          params={{ instanceId }}
          className="text-fg-subtle hover:text-fg mb-3 inline-flex items-center gap-1.5 text-[13px] transition-colors"
        >
          <ArrowLeft className="size-3.5" />
          {t("pve.tabs.guests")}
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <span
            className={cn(
              "flex size-10 shrink-0 items-center justify-center rounded-xl text-xs font-bold",
              guest.kind === "qemu" ? "bg-accent-soft text-accent-soft-fg" : "bg-info-soft text-info-soft-fg",
            )}
          >
            {guest.kind === "qemu" ? "VM" : "CT"}
          </span>
          <div className="min-w-0 flex-1">
            <h1 className="text-fg flex flex-wrap items-center gap-2 text-xl font-semibold tracking-tight">
              {guest.name || `#${vmid}`}
              <Badge tone={guestStatusTone[guest.status as keyof typeof guestStatusTone] ?? "neutral"}>
                {guestStatusLabel(t, guest.status)}
              </Badge>
            </h1>
            <p className="text-fg-muted text-sm">
              {vmid} · {t("pve.guest.onNode", { node: guest.node })}
            </p>
          </div>

          {canControl && (
            <div className="flex flex-wrap gap-1.5">
              {!running ? (
                <Button variant="primary" size="sm" loading={action.isPending} onClick={() => action.mutate("start")}>
                  <Play className="size-3.5" /> {t("pve.actions.start")}
                </Button>
              ) : (
                <>
                  <Button variant="secondary" size="sm" loading={action.isPending} onClick={() => action.mutate("shutdown")}>
                    <Power className="size-3.5" /> {t("pve.actions.shutdown")}
                  </Button>
                  <Button variant="secondary" size="sm" loading={action.isPending} onClick={() => action.mutate("reboot")}>
                    <RotateCcw className="size-3.5" /> {t("pve.actions.reboot")}
                  </Button>
                  <Button variant="danger-ghost" size="sm" onClick={() => setConfirmStop(true)}>
                    <Square className="size-3.5" /> {t("pve.actions.stop")}
                  </Button>
                </>
              )}
              <Button variant="secondary" size="sm" onClick={() => setMigrateOpen(true)}>
                <ArrowRightLeft className="size-3.5" /> {t("pve.actions.migrate")}
              </Button>
              {canConfigure && (
                <Button variant="secondary" size="sm" onClick={() => setCloneOpen(true)}>
                  <Copy className="size-3.5" /> {t("pve.actions.clone")}
                </Button>
              )}
              {canDelete && !running && (
                <Button variant="danger-ghost" size="sm" onClick={() => setConfirmDelete(true)}>
                  <Trash2 className="size-3.5" /> {t("pve.actions.delete")}
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      <nav className="border-edge flex gap-1 overflow-x-auto border-b">
        {tabs.filter((entry) => entry.show).map((entry) => (
          <button
            key={entry.key}
            onClick={() => setTab(entry.key)}
            className={cn(
              "-mb-px shrink-0 cursor-pointer border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
              tab === entry.key ? "border-accent text-fg" : "text-fg-muted hover:text-fg border-transparent",
            )}
          >
            {entry.label}
          </button>
        ))}
      </nav>

      {tab === "overview" && <OverviewTab instanceId={instanceId} vmid={vmid} guest={guest} />}
      {tab === "hardware" && (
        <HardwareTab instanceId={instanceId} vmid={vmid} config={detail.data!.config} canConfigure={canConfigure} />
      )}
      {tab === "snapshots" && <SnapshotsTab instanceId={instanceId} vmid={vmid} canControl={canControl} />}
      {tab === "backup" && canControl && <BackupTab instanceId={instanceId} vmid={vmid} />}
      {tab === "console" && canControl && <ConsoleTab instanceId={instanceId} vmid={vmid} kind={guest.kind} />}
      {tab === "ssh" && canControl && (
        <SSHTab instanceId={instanceId} vmid={vmid} canConfigure={canConfigure} />
      )}

      <ConfirmDialog
        open={confirmStop}
        onOpenChange={setConfirmStop}
        title={t("pve.actions.stopConfirmTitle", { name: guest.name || vmid })}
        body={t("pve.actions.stopConfirmBody")}
        confirmLabel={t("pve.actions.stop")}
        loading={action.isPending}
        onConfirm={() => {
          action.mutate("stop");
          setConfirmStop(false);
        }}
      />
      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent
          title={t("pve.actions.deleteConfirmTitle", { name: guest.name || "guest", vmid })}
          description={t("pve.actions.deleteConfirmBody")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="danger" loading={remove.isPending} onClick={() => remove.mutate()}>
                {t("pve.actions.delete")}
              </Button>
            </>
          }
        >
          <Checkbox checked={purge} onCheckedChange={setPurge} label={t("pve.actions.deletePurge")} />
        </DialogContent>
      </Dialog>
      {migrateOpen && (
        <MigrateDialog instanceId={instanceId} vmid={vmid} name={guest.name} currentNode={guest.node} onClose={() => setMigrateOpen(false)} />
      )}
      {cloneOpen && (
        <CloneDialog instanceId={instanceId} vmid={vmid} name={guest.name} onClose={() => setCloneOpen(false)} />
      )}
    </div>
  );
}

// ---- overview --------------------------------------------------------------

function OverviewTab({
  instanceId,
  vmid,
  guest,
}: {
  instanceId: string;
  vmid: number;
  guest: { status: string; cpu: number; maxCpu: number; memBytes: number; maxMemBytes: number; diskBytes: number; maxDiskBytes: number; uptimeSeconds: number };
}) {
  const { t } = useTranslation();
  const resource = `proxmox/${instanceId}/guest/${vmid}`;
  const cpu = useMetricSeries(instanceId, resource, "cpu");
  const memUsed = useMetricSeries(instanceId, resource, "mem_used");
  const memTotal = useMetricSeries(instanceId, resource, "mem_total");

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <MiniStat label={t("pve.guest.cpuUsage")} value={guest.status === "running" ? `${Math.round(guest.cpu * 100)}% · ${guest.maxCpu} vCPU` : "—"} />
        <MiniStat
          label={t("pve.guest.memUsage")}
          value={guest.status === "running" ? `${formatBytes(guest.memBytes)} / ${formatBytes(guest.maxMemBytes)}` : formatBytes(guest.maxMemBytes)}
        />
        <MiniStat label={t("pve.guest.diskUsage")} value={formatBytes(guest.maxDiskBytes)} />
        <MiniStat label={t("pve.guest.uptime")} value={guest.status === "running" ? formatDuration(guest.uptimeSeconds) : "—"} />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("pve.guest.chartCpu")}</p>
          <TimeSeriesChart points={cpu} kind="percent" height={150} />
        </Card>
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("pve.guest.chartMem")}</p>
          <TimeSeriesChart points={memUsed} capacity={memTotal} kind="bytes" height={150} />
        </Card>
      </div>
    </div>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-4">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1 truncate text-lg font-semibold">{value}</p>
    </Card>
  );
}

// ---- hardware --------------------------------------------------------------

function HardwareTab({
  instanceId,
  vmid,
  config,
  canConfigure,
}: {
  instanceId: string;
  vmid: number;
  config: Record<string, unknown>;
  canConfigure: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [changes, setChanges] = useState<Record<string, string>>({});
  const [newKey, setNewKey] = useState("");

  const entries = useMemo(
    () =>
      Object.entries(config)
        .filter(([k]) => k !== "digest")
        .sort(([a], [b]) => a.localeCompare(b)),
    [config],
  );

  const apply = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/proxmox/{id}/guests/{vmid}/config", {
          params: { path: { id: instanceId, vmid } },
          body: { changes },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.guest.hardwareApplied"));
      setChanges({});
      void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "guest", vmid] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const dirty = Object.keys(changes).length > 0;

  return (
    <Card>
      <CardHeader
        title={t("pve.guest.hardware")}
        description={t("pve.guest.hardwareIntro")}
        actions={
          canConfigure && dirty ? (
            <Button variant="primary" size="sm" loading={apply.isPending} onClick={() => apply.mutate()}>
              {t("pve.guest.hardwareApply")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        <Table>
          <THead>
            <tr>
              <TH className="w-48">{t("pve.guest.hardwareKey")}</TH>
              <TH>{t("pve.guest.hardwareValue")}</TH>
            </tr>
          </THead>
          <TBody>
            {entries.map(([key, value]) => (
              <TR key={key}>
                <TD>
                  <code className="text-fg font-mono text-xs">{key}</code>
                </TD>
                <TD>
                  {canConfigure ? (
                    <Input
                      className="h-7.5 font-mono text-xs"
                      value={changes[key] ?? String(value)}
                      onChange={(e) => setChanges((prev) => ({ ...prev, [key]: e.target.value }))}
                    />
                  ) : (
                    <code className="text-fg-muted font-mono text-xs break-all">{String(value)}</code>
                  )}
                </TD>
              </TR>
            ))}
            {canConfigure && (
              <TR>
                <TD>
                  <Input
                    className="h-7.5 font-mono text-xs"
                    placeholder={t("pve.guest.hardwareAddKey")}
                    value={newKey}
                    onChange={(e) => setNewKey(e.target.value)}
                  />
                </TD>
                <TD>
                  {newKey.trim() && (
                    <Input
                      className="h-7.5 font-mono text-xs"
                      value={changes[newKey.trim()] ?? ""}
                      onChange={(e) => setChanges((prev) => ({ ...prev, [newKey.trim()]: e.target.value }))}
                    />
                  )}
                </TD>
              </TR>
            )}
          </TBody>
        </Table>
      </CardBody>
    </Card>
  );
}

// ---- snapshots -------------------------------------------------------------

function SnapshotsTab({ instanceId, vmid, canControl }: { instanceId: string; vmid: number; canControl: boolean }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const snapshots = useQuery({
    queryKey: ["pve", instanceId, "guest", vmid, "snapshots"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/guests/{vmid}/snapshots", { params: { path: { id: instanceId, vmid } } })),
  });

  const [createOpen, setCreateOpen] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [vmstate, setVmstate] = useState(false);
  const [rollback, setRollback] = useState<PveSnapshot | null>(null);
  const [deleting, setDeleting] = useState<PveSnapshot | null>(null);

  const refetch = () => void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "guest", vmid, "snapshots"] });

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/snapshots", {
          params: { path: { id: instanceId, vmid } },
          body: { name, description: description || undefined, vmstate },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setCreateOpen(false);
      setName("");
      setDescription("");
      setTimeout(refetch, 2000);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const doRollback = useMutation({
    mutationFn: async (snap: string) =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/snapshots/{name}/rollback", {
          params: { path: { id: instanceId, vmid, name: snap } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setRollback(null);
      setTimeout(refetch, 2000);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const doDelete = useMutation({
    mutationFn: async (snap: string) =>
      unwrap(
        await api.DELETE("/proxmox/{id}/guests/{vmid}/snapshots/{name}", {
          params: { path: { id: instanceId, vmid, name: snap } },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setDeleting(null);
      setTimeout(refetch, 2000);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <CardHeader
        title={t("pve.guest.snapshots")}
        actions={
          canControl ? (
            <Button variant="primary" size="sm" onClick={() => setCreateOpen(true)}>
              <Camera className="size-3.5" />
              {t("pve.guest.snapshotNew")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="px-0 py-0">
        {snapshots.isLoading ? (
          <p className="text-fg-subtle px-5 py-6 text-center text-[13px]">{t("common.loading")}</p>
        ) : (snapshots.data ?? []).length === 0 ? (
          <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("pve.guest.snapshotsEmpty")}</p>
        ) : (
          <ul>
            {snapshots.data!.map((snap) => (
              <li key={snap.name} className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0">
                <Camera className="text-fg-subtle size-4 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-fg flex items-center gap-2 text-sm font-medium">
                    {snap.name}
                    {snap.vmstate && <Badge tone="accent">RAM</Badge>}
                  </p>
                  <p className="text-fg-subtle text-xs">
                    {snap.snaptime ? formatDateTime(snap.snaptime) : "—"}
                    {snap.description && ` · ${snap.description}`}
                  </p>
                </div>
                {canControl && (
                  <div className="flex gap-1.5">
                    <Button size="sm" variant="secondary" onClick={() => setRollback(snap)}>
                      {t("pve.guest.snapshotRollback")}
                    </Button>
                    <Button size="sm" variant="danger-ghost" onClick={() => setDeleting(snap)}>
                      {t("common.delete")}
                    </Button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardBody>

      <Dialog open={createOpen} onOpenChange={setCreateOpen}>
        <DialogContent
          title={t("pve.guest.snapshotNew")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setCreateOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" disabled={!name.trim()} loading={create.isPending} onClick={() => create.mutate()}>
                {t("common.create")}
              </Button>
            </>
          }
        >
          <div className="space-y-4">
            <Field label={t("pve.guest.snapshotName")}>
              {(p) => <Input {...p} autoFocus placeholder="pre-update" value={name} onChange={(e) => setName(e.target.value)} />}
            </Field>
            <Field label={t("pve.guest.snapshotDescription")} optional={t("common.optional")}>
              {(p) => <Textarea {...p} value={description} onChange={(e) => setDescription(e.target.value)} />}
            </Field>
            <Checkbox checked={vmstate} onCheckedChange={setVmstate} label={t("pve.guest.snapshotVmstate")} />
          </div>
        </DialogContent>
      </Dialog>

      <ConfirmDialog
        open={rollback !== null}
        onOpenChange={(open) => !open && setRollback(null)}
        title={rollback ? t("pve.guest.snapshotRollbackConfirmTitle", { name: rollback.name }) : ""}
        body={t("pve.guest.snapshotRollbackConfirmBody")}
        confirmLabel={t("pve.guest.snapshotRollback")}
        loading={doRollback.isPending}
        onConfirm={() => rollback && doRollback.mutate(rollback.name)}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("pve.guest.snapshotDeleteConfirmTitle", { name: deleting.name }) : ""}
        body={t("pve.guest.snapshotDeleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={doDelete.isPending}
        onConfirm={() => deleting && doDelete.mutate(deleting.name)}
      />
    </Card>
  );
}

// ---- backup ----------------------------------------------------------------

function BackupTab({ instanceId, vmid }: { instanceId: string; vmid: number }) {
  const { t } = useTranslation();
  const toast = useToast();

  const storage = useQuery({
    queryKey: ["pve", instanceId, "storage"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/storage", { params: { path: { id: instanceId } } })),
  });

  const pools = useMemo(() => {
    const names = new Set<string>();
    for (const pool of storage.data ?? []) {
      if (pool.content.includes("backup") || pool.content.includes("vztmpl") || pool.type === "dir") {
        names.add(pool.storage);
      }
    }
    if (names.size === 0) for (const pool of storage.data ?? []) names.add(pool.storage);
    return [...names];
  }, [storage.data]);

  const [target, setTarget] = useState("");
  const [mode, setMode] = useState<"snapshot" | "suspend" | "stop">("snapshot");

  const backup = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/backup", {
          params: { path: { id: instanceId, vmid } },
          body: { storage: target || pools[0], mode },
        }),
      ),
    onSuccess: () => toast("success", t("pve.actions.started")),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card className="max-w-xl">
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <HardDrive className="text-fg-subtle size-4" />
            {t("pve.actions.backupNow")}
          </span>
        }
      />
      <CardBody className="space-y-4">
        <Field label={t("pve.guest.backupStorage")}>
          {() => (
            <Select value={target || pools[0] || ""} onValueChange={setTarget}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {pools.map((pool) => (
                  <SelectItem key={pool} value={pool}>
                    {pool}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </Field>
        <Field label={t("pve.guest.backupMode")} hint={t("pve.guest.backupModeHint")}>
          {() => (
            <Select value={mode} onValueChange={(v) => setMode(v as typeof mode)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="snapshot">snapshot</SelectItem>
                <SelectItem value="suspend">suspend</SelectItem>
                <SelectItem value="stop">stop</SelectItem>
              </SelectContent>
            </Select>
          )}
        </Field>
        <div className="flex justify-end">
          <Button variant="primary" disabled={pools.length === 0} loading={backup.isPending} onClick={() => backup.mutate()}>
            {t("pve.actions.backupNow")}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}

// ---- console ---------------------------------------------------------------

function ConsoleTab({ instanceId, vmid, kind }: { instanceId: string; vmid: number; kind: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [session, setSession] = useState<(TermSession & { consoleKind: "vnc" | "serial" }) | null>(null);
  const [consoleKind, setConsoleKind] = useState<"vnc" | "serial">(kind === "lxc" ? "serial" : "vnc");

  const open = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/console", {
          params: { path: { id: instanceId, vmid } },
          body: { kind: consoleKind },
        }),
      ),
    onSuccess: (data) => setSession({ ...data, consoleKind }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const wsUrl = (path: string) =>
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${path}`;

  if (session) {
    return (
      <div className="border-edge overflow-hidden rounded-lg border" style={{ height: 520 }}>
        <Suspense fallback={<PageLoader className="h-full" />}>
          {session.consoleKind === "vnc" ? (
            <VncViewer wsUrl={wsUrl(session.wsPath)} credential={session.credential ?? undefined} className="h-full" />
          ) : (
            <TerminalPane wsUrl={wsUrl(session.wsPath)} className="h-full" onExit={() => setSession(null)} />
          )}
        </Suspense>
      </div>
    );
  }

  return (
    <Card className="max-w-xl">
      <CardHeader title={t("pve.guest.console")} description={kind === "lxc" ? t("pve.guest.consoleHintLxc") : undefined} />
      <CardBody className="space-y-4">
        {kind !== "lxc" && (
          <Field label={t("pve.guest.consoleKind")}>
            {() => (
              <Select value={consoleKind} onValueChange={(v) => setConsoleKind(v as "vnc" | "serial")}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="vnc">{t("pve.guest.consoleVnc")}</SelectItem>
                  <SelectItem value="serial">{t("pve.guest.consoleSerial")}</SelectItem>
                </SelectContent>
              </Select>
            )}
          </Field>
        )}
        <div className="flex justify-end">
          <Button variant="primary" loading={open.isPending} onClick={() => open.mutate()}>
            {t("pve.guest.consoleOpen")}
          </Button>
        </div>
      </CardBody>
    </Card>
  );
}

// ---- ssh -------------------------------------------------------------------

function SSHTab({ instanceId, vmid, canConfigure }: { instanceId: string; vmid: number; canConfigure: boolean }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const resource = `proxmox/${instanceId}/guest/${vmid}`;

  const cred = useQuery({
    queryKey: ["ssh-cred", resource],
    queryFn: async () => {
      try {
        return unwrap(await api.GET("/resources/ssh", { params: { query: { resource } } }));
      } catch (err) {
        if (isApiError(err) && err.status === 404) return null;
        throw err;
      }
    },
    retry: false,
  });

  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [username, setUsername] = useState("root");
  const [authType, setAuthType] = useState<"password" | "key">("password");
  const [secret, setSecret] = useState("");
  const [hydrated, setHydrated] = useState(false);
  if (cred.data && !hydrated) {
    setHost(cred.data.host);
    setPort(String(cred.data.port));
    setUsername(cred.data.username);
    setAuthType(cred.data.authType);
    setHydrated(true);
  }

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/resources/ssh", {
          body: {
            resource,
            host,
            port: Number(port) || 22,
            username,
            authType,
            secret: secret || undefined,
          },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.guest.sshSaved"));
      setSecret("");
      void queryClient.invalidateQueries({ queryKey: ["ssh-cred", resource] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const [session, setSession] = useState<TermSession | null>(null);
  const connect = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/proxmox/{id}/guests/{vmid}/ssh", { params: { path: { id: instanceId, vmid } } })),
    onSuccess: setSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const wsUrl = (path: string) =>
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${path}`;

  if (session) {
    return (
      <div className="border-edge overflow-hidden rounded-lg border" style={{ height: 520 }}>
        <Suspense fallback={<PageLoader className="h-full" />}>
          <TerminalPane wsUrl={wsUrl(session.wsPath)} className="h-full" onExit={() => setSession(null)} />
        </Suspense>
      </div>
    );
  }

  return (
    <div className="grid items-start gap-4 lg:grid-cols-2">
      <Card>
        <CardHeader title={t("pve.guest.sshSettings")} description={t("pve.guest.sshIntro")} />
        <CardBody className="space-y-4">
          <div className="grid grid-cols-3 gap-3">
            <Field label={t("pve.guest.sshHost")} className="col-span-2">
              {(p) => <Input {...p} disabled={!canConfigure} placeholder="10.0.0.14" value={host} onChange={(e) => setHost(e.target.value)} />}
            </Field>
            <Field label={t("pve.guest.sshPort")}>
              {(p) => <Input {...p} disabled={!canConfigure} type="number" value={port} onChange={(e) => setPort(e.target.value)} />}
            </Field>
          </div>
          <Field label={t("pve.guest.sshUser")}>
            {(p) => <Input {...p} disabled={!canConfigure} value={username} onChange={(e) => setUsername(e.target.value)} />}
          </Field>
          {canConfigure && (
            <>
              <Field label={t("pve.guest.consoleKind")}>
                {() => (
                  <Select value={authType} onValueChange={(v) => setAuthType(v as "password" | "key")}>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="password">{t("pve.guest.sshAuthPassword")}</SelectItem>
                      <SelectItem value="key">{t("pve.guest.sshAuthKey")}</SelectItem>
                    </SelectContent>
                  </Select>
                )}
              </Field>
              <Field
                label={authType === "password" ? t("pve.guest.sshSecretPassword") : t("pve.guest.sshSecretKey")}
                hint={cred.data ? t("pve.guest.sshSecretKeep") : undefined}
              >
                {(p) =>
                  authType === "password" ? (
                    <PasswordInput {...p} value={secret} onChange={(e) => setSecret(e.target.value)} />
                  ) : (
                    <Textarea {...p} className="font-mono text-xs" rows={5} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" value={secret} onChange={(e) => setSecret(e.target.value)} />
                  )
                }
              </Field>
              <div className="flex justify-end">
                <Button
                  variant="primary"
                  disabled={!host.trim() || !username.trim() || (!secret && !cred.data)}
                  loading={save.isPending}
                  onClick={() => save.mutate()}
                >
                  {t("common.save")}
                </Button>
              </div>
            </>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardBody>
          {cred.data ? (
            <div className="flex flex-col items-center gap-3 py-8 text-center">
              <p className="text-fg-muted text-sm">
                {cred.data.username}@{cred.data.host}:{cred.data.port}
              </p>
              <Button variant="primary" loading={connect.isPending} onClick={() => connect.mutate()}>
                {t("pve.guest.sshConnect")}
              </Button>
            </div>
          ) : (
            <EmptyState title={t("pve.guest.sshNotConfigured")} className="py-8" />
          )}
        </CardBody>
      </Card>
    </div>
  );
}

// ---- migrate / clone dialogs ----------------------------------------------

function MigrateDialog({
  instanceId,
  vmid,
  name,
  currentNode,
  onClose,
}: {
  instanceId: string;
  vmid: number;
  name: string;
  currentNode: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();

  const summary = useQuery({
    queryKey: ["pve", instanceId, "summary"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/summary", { params: { path: { id: instanceId } } })),
  });
  const targets = (summary.data?.nodes ?? []).filter((n) => n.name !== currentNode && n.status === "online");

  const [target, setTarget] = useState("");
  const [online, setOnline] = useState(true);

  const migrate = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/migrate", {
          params: { path: { id: instanceId, vmid } },
          body: { target, online },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("pve.guest.migrateTitle", { name: name || vmid })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!target} loading={migrate.isPending} onClick={() => migrate.mutate()}>
              {t("pve.actions.migrate")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("pve.guest.migrateTarget")}>
            {() => (
              <Select value={target} onValueChange={setTarget}>
                <SelectTrigger>
                  <SelectValue placeholder="—" />
                </SelectTrigger>
                <SelectContent>
                  {targets.map((node) => (
                    <SelectItem key={node.name} value={node.name}>
                      {node.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>
          <Checkbox checked={online} onCheckedChange={setOnline} label={t("pve.guest.migrateOnline")} />
        </div>
      </DialogContent>
    </Dialog>
  );
}

function CloneDialog({
  instanceId,
  vmid,
  name,
  onClose,
}: {
  instanceId: string;
  vmid: number;
  name: string;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [newid, setNewid] = useState("");
  const [cloneName, setCloneName] = useState(name ? name + "-copy" : "");
  const [full, setFull] = useState(true);

  const clone = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/clone", {
          params: { path: { id: instanceId, vmid } },
          body: { newid: Number(newid), name: cloneName || undefined, full },
        }),
      ),
    onSuccess: () => {
      toast("success", t("pve.actions.started"));
      setTimeout(() => void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "guests"] }), 2500);
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("pve.guest.cloneTitle", { name: name || vmid })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" disabled={!newid} loading={clone.isPending} onClick={() => clone.mutate()}>
              {t("pve.actions.clone")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label={t("pve.guest.cloneNewId")}>
              {(p) => <Input {...p} autoFocus type="number" placeholder="200" value={newid} onChange={(e) => setNewid(e.target.value)} />}
            </Field>
            <Field label={t("pve.guest.cloneName")} optional={t("common.optional")}>
              {(p) => <Input {...p} value={cloneName} onChange={(e) => setCloneName(e.target.value)} />}
            </Field>
          </div>
          <Checkbox checked={full} onCheckedChange={setFull} label={t("pve.guest.cloneFull")} />
        </div>
      </DialogContent>
    </Dialog>
  );
}
