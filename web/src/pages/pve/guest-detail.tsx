import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  ArrowRightLeft,
  Camera,
  ChevronDown,
  Copy,
  HardDrive,
  KeyRound,
  Monitor,
  Network,
  Play,
  Power,
  RotateCcw,
  Square,
  Terminal,
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
import { SHELL } from "@/lib/perms";
import { SSHHostPicker } from "@/pages/ssh-hosts";
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
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { ErrorState, EmptyState } from "@/ui/states";
import { useToast } from "@/ui/toast";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { lazy, Suspense } from "react";
import { TimeSeriesChart } from "@/components/time-series-chart";
import { agentAddresses, primaryIpv4, useGuestAgent } from "./agent";

// Consoles pull in xterm.js and noVNC — loaded only when a console opens.
const TerminalPane = lazy(() =>
  import("@/components/terminal").then((m) => ({ default: m.TerminalPane })),
);
const VncViewer = lazy(() =>
  import("@/components/vnc-viewer").then((m) => ({ default: m.VncViewer })),
);
import { useMetricSeries } from "@/lib/metrics";
import { guestStatusLabel, guestStatusTone } from "./guests";

type TabKey = "overview" | "hardware" | "snapshots" | "backup" | "console";

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
  const canShell = can(guestRes, SHELL);

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
    { key: "console", label: t("pve.guest.console"), show: canShell },
  ];

  return (
    <div className="space-y-5">
      <div>
        {/* The resource tree replaces this link from lg up, where it is always
            on screen; below that the tree sits behind a toggle, so keep it. */}
        <Link
          to="/pve/$instanceId/guests"
          params={{ instanceId }}
          className="text-fg-subtle hover:text-fg mb-3 inline-flex items-center gap-1.5 text-[13px] transition-colors lg:hidden"
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
            {guest.kind === "qemu" && <AgentSummary instanceId={instanceId} vmid={vmid} />}
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

      <nav className="border-edge flex gap-1 overflow-x-auto overflow-y-hidden border-b">
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
        <HardwareTab
          instanceId={instanceId}
          vmid={vmid}
          kind={guest.kind}
          config={detail.data!.config}
          canConfigure={canConfigure}
        />
      )}
      {tab === "snapshots" && <SnapshotsTab instanceId={instanceId} vmid={vmid} canControl={canControl} />}
      {tab === "backup" && canControl && <BackupTab instanceId={instanceId} vmid={vmid} />}
      {tab === "console" && canShell && (
        <ConsoleTab instanceId={instanceId} vmid={vmid} kind={guest.kind} canConfigure={canConfigure} />
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

// ---- guest agent header line ----------------------------------------------

/**
 * AgentSummary surfaces what the QEMU guest agent reports — primary IPv4,
 * hostname, OS — right in the header. When the agent isn't talking it shows a
 * single quiet line with the reason and stays out of the way.
 */
function AgentSummary({ instanceId, vmid }: { instanceId: string; vmid: number }) {
  const { t } = useTranslation();
  const agent = useGuestAgent(instanceId, vmid, true);
  const info = agent.data;
  if (!info) return null;

  if (!info.available) {
    return (
      <p className="text-fg-subtle mt-1 text-xs">
        {t("pve.guest.agentUnavailable")}
        {info.reason ? ` — ${info.reason}` : ""}
      </p>
    );
  }

  const ip = primaryIpv4(info);
  const addrs = agentAddresses(info);
  const moreCount = ip ? addrs.length - 1 : addrs.length;

  return (
    <div className="text-fg-muted mt-1.5 flex flex-wrap items-center gap-x-3 gap-y-1 text-[13px]">
      {ip && (
        <span className="inline-flex items-center gap-1.5">
          <Network className="text-fg-subtle size-3.5" />
          <code className="text-fg font-mono text-xs font-medium">{ip}</code>
        </span>
      )}
      {moreCount > 0 && (
        <Popover>
          <PopoverTrigger asChild>
            <button className="text-fg-subtle hover:text-fg cursor-pointer text-xs underline-offset-2 hover:underline">
              {t("pve.guest.agentMoreIps", { count: moreCount })}
            </button>
          </PopoverTrigger>
          <PopoverContent align="start" className="w-80">
            <p className="text-fg-subtle mb-2 text-xs font-semibold tracking-wide uppercase">
              {t("pve.guest.agentAddresses")}
            </p>
            <ul className="space-y-1.5">
              {addrs.map(({ iface, addr }) => (
                <li key={iface + addr.address} className="flex items-baseline justify-between gap-3 text-xs">
                  <span className="text-fg-subtle shrink-0">{iface}</span>
                  <code className="text-fg font-mono break-all">
                    {addr.address}
                    {addr.prefix != null ? `/${addr.prefix}` : ""}
                  </code>
                </li>
              ))}
            </ul>
          </PopoverContent>
        </Popover>
      )}
      {info.hostname && <span className="truncate">{info.hostname}</span>}
      {info.os && <span className="text-fg-subtle max-w-72 truncate">{info.os}</span>}
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
  const topic = `metrics:proxmox/${instanceId}`;
  const cpu = useMetricSeries(topic, resource, "cpu");
  const memUsed = useMetricSeries(topic, resource, "mem_used");
  const memTotal = useMetricSeries(topic, resource, "mem_total");

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

// ---- hardware: config parsing helpers --------------------------------------

type Token = { k: string; v: string | null };

/** tokenize splits a Proxmox value string ("a=1,b,c=x") into ordered tokens. */
function tokenize(raw: string): Token[] {
  if (!raw.trim()) return [];
  return raw
    .split(",")
    .map((part) => {
      const i = part.indexOf("=");
      return i === -1
        ? { k: part.trim(), v: null }
        : { k: part.slice(0, i).trim(), v: part.slice(i + 1) };
    })
    .filter((tk) => tk.k !== "");
}

function serializeTokens(tokens: Token[]): string {
  return tokens.map((tk) => (tk.v === null ? tk.k : `${tk.k}=${tk.v}`)).join(",");
}

const NET_MODELS = ["virtio", "e1000", "e1000e", "rtl8139", "vmxnet3"];
const NET_KEY = /^net\d+$/;
const DISK_KEY = /^(?:rootfs$|(?:scsi|virtio|ide|sata|mp|efidisk|tpmstate|unused)\d+$)/;

type NetView = { model: string; mac: string; bridge: string; vlan: string; extras: string };

/** netView pulls the fields we edit out of a netN value; extras stays raw. */
function netView(kind: "qemu" | "lxc", raw: string): NetView {
  let model = "";
  let mac = "";
  let bridge = "";
  let vlan = "";
  const rest: Token[] = [];
  for (const tk of tokenize(raw)) {
    if (kind === "qemu" && NET_MODELS.includes(tk.k)) {
      model = tk.k;
      mac = tk.v ?? "";
    } else if (kind === "lxc" && tk.k === "hwaddr") {
      mac = tk.v ?? "";
    } else if (tk.k === "bridge") {
      bridge = tk.v ?? "";
    } else if (tk.k === "tag") {
      vlan = tk.v ?? "";
    } else {
      rest.push(tk);
    }
  }
  return { model, mac, bridge, vlan, extras: serializeTokens(rest) };
}

/**
 * netEdit rewrites one field and re-serializes, keeping every token it does
 * not understand (firewall, mtu, queues, …) byte-for-byte where it was.
 */
function netEdit(
  kind: "qemu" | "lxc",
  raw: string,
  patch: Partial<Pick<NetView, "model" | "mac" | "bridge" | "vlan">>,
): string {
  const view = { ...netView(kind, raw), ...patch };
  const out: Token[] = [];
  let sawModel = false;
  let sawHwaddr = false;
  let sawBridge = false;
  let sawTag = false;
  for (const tk of tokenize(raw)) {
    if (kind === "qemu" && NET_MODELS.includes(tk.k)) {
      sawModel = true;
      if (view.model) out.push({ k: view.model, v: view.mac || null });
    } else if (kind === "lxc" && tk.k === "hwaddr") {
      sawHwaddr = true;
      if (view.mac) out.push({ k: "hwaddr", v: view.mac });
    } else if (tk.k === "bridge") {
      sawBridge = true;
      if (view.bridge) out.push({ k: "bridge", v: view.bridge });
    } else if (tk.k === "tag") {
      sawTag = true;
      if (view.vlan) out.push({ k: "tag", v: view.vlan });
    } else {
      out.push(tk);
    }
  }
  if (kind === "qemu" && !sawModel && view.model) out.unshift({ k: view.model, v: view.mac || null });
  if (kind === "lxc" && !sawHwaddr && view.mac) out.push({ k: "hwaddr", v: view.mac });
  if (!sawBridge && view.bridge) out.push({ k: "bridge", v: view.bridge });
  if (!sawTag && view.vlan) out.push({ k: "tag", v: view.vlan });
  return serializeTokens(out);
}

/** agentEnabled reads the QEMU agent flag from "1", "0", "enabled=1,…", … */
function agentEnabled(raw: string): boolean {
  const tokens = tokenize(raw);
  const enabledTk = tokens.find((tk) => tk.k === "enabled");
  if (enabledTk) return enabledTk.v === "1";
  return tokens[0]?.k === "1";
}

/** agentToggle flips the flag while keeping extra agent options intact. */
function agentToggle(raw: string, on: boolean): string {
  const flag = on ? "1" : "0";
  const tokens = tokenize(raw);
  if (tokens.length === 0) return flag;
  const enabledTk = tokens.find((tk) => tk.k === "enabled");
  if (enabledTk) {
    enabledTk.v = flag;
    return serializeTokens(tokens);
  }
  if (tokens[0].k === "0" || tokens[0].k === "1") {
    tokens[0] = { k: flag, v: null };
    return serializeTokens(tokens);
  }
  return serializeTokens([{ k: flag, v: null }, ...tokens]);
}

function diskView(raw: string): { volume: string; size: string; extras: string } {
  const tokens = tokenize(raw);
  let volume = "";
  let size = "";
  const rest: Token[] = [];
  tokens.forEach((tk, i) => {
    if (i === 0) volume = tk.v === null ? tk.k : `${tk.k}=${tk.v}`;
    else if (tk.k === "size") size = tk.v ?? "";
    else rest.push(tk);
  });
  return { volume, size, extras: serializeTokens(rest) };
}

// ---- hardware tab ----------------------------------------------------------

/**
 * HardwareTab is a structured editor over the raw Proxmox config: typed
 * controls for the common knobs, parsed network devices, read-only disks, and
 * a raw-value escape hatch. Every edit lands in one pending-changes set and a
 * single PUT applies the whole batch after an explicit confirmation.
 */
function HardwareTab({
  instanceId,
  vmid,
  kind,
  config,
  canConfigure,
}: {
  instanceId: string;
  vmid: number;
  kind: "qemu" | "lxc";
  config: Record<string, unknown>;
  canConfigure: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [changes, setChanges] = useState<Record<string, string>>({});
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [newKey, setNewKey] = useState("");

  const orig = (key: string): string | undefined =>
    key in config ? String(config[key] ?? "") : undefined;
  const current = (key: string): string => changes[key] ?? orig(key) ?? "";
  const isModified = (key: string): boolean => key in changes;

  /** setKey stages a change; a value equal to the original un-stages it. */
  const setKey = (key: string, value: string, fallback = "") => {
    setChanges((prev) => {
      const next = { ...prev };
      if (value === (orig(key) ?? fallback)) delete next[key];
      else next[key] = value;
      return next;
    });
  };

  const nameKey = kind === "qemu" ? "name" : "hostname";

  const netKeys = useMemo(
    () => Object.keys(config).filter((k) => NET_KEY.test(k)).sort(),
    [config],
  );
  const diskKeys = useMemo(
    () => Object.keys(config).filter((k) => DISK_KEY.test(k)).sort(),
    [config],
  );
  const generalKeys = useMemo(
    () => (kind === "qemu" ? [nameKey, "cores", "memory", "onboot", "agent", "boot"] : [nameKey, "cores", "memory", "onboot"]),
    [kind, nameKey],
  );
  const otherKeys = useMemo(() => {
    const known = new Set(["digest", ...generalKeys, ...netKeys, ...diskKeys]);
    const keys = Object.keys(config).filter((k) => !known.has(k));
    // Keys staged via "set another key" that aren't in the config yet.
    for (const k of Object.keys(changes)) {
      if (!known.has(k) && !(k in config) && !keys.includes(k)) keys.push(k);
    }
    return keys.sort();
  }, [config, changes, generalKeys, netKeys, diskKeys]);

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
      setConfirmOpen(false);
      void queryClient.invalidateQueries({ queryKey: ["pve", instanceId, "guest", vmid] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const pending = Object.keys(changes).length;
  const memoryMb = Number(current("memory"));
  const memoryHint =
    Number.isFinite(memoryMb) && memoryMb > 0
      ? t("pve.guest.hwMemoryGb", { gb: (memoryMb / 1024).toFixed(memoryMb % 1024 === 0 ? 0 : 1) })
      : undefined;

  return (
    <div className="space-y-4">
      {/* General */}
      <Card>
        <CardHeader title={t("pve.guest.hwGeneral")} />
        <CardBody className="grid gap-x-6 gap-y-4 sm:grid-cols-2">
          <MarkedField modified={isModified(nameKey)}>
            <Field label={t("pve.guest.hwName")}>
              {(p) => (
                <Input
                  {...p}
                  disabled={!canConfigure}
                  value={current(nameKey)}
                  onChange={(e) => setKey(nameKey, e.target.value)}
                />
              )}
            </Field>
          </MarkedField>
          <div className="grid grid-cols-2 gap-3">
            <MarkedField modified={isModified("cores")}>
              <Field label={t("pve.guest.hwCores")}>
                {(p) => (
                  <Input
                    {...p}
                    disabled={!canConfigure}
                    type="number"
                    min={1}
                    step={1}
                    placeholder="1"
                    value={current("cores")}
                    onChange={(e) => setKey("cores", e.target.value)}
                  />
                )}
              </Field>
            </MarkedField>
            <MarkedField modified={isModified("memory")}>
              <Field label={t("pve.guest.hwMemory")} hint={memoryHint}>
                {(p) => (
                  <Input
                    {...p}
                    disabled={!canConfigure}
                    type="number"
                    min={16}
                    step={128}
                    value={current("memory")}
                    onChange={(e) => setKey("memory", e.target.value)}
                  />
                )}
              </Field>
            </MarkedField>
          </div>
          <MarkedField modified={isModified("onboot")}>
            <div className="flex items-center justify-between gap-3 py-1">
              <span className="text-fg text-[13px] font-medium">{t("pve.guest.hwOnboot")}</span>
              <Switch
                disabled={!canConfigure}
                checked={current("onboot") === "1"}
                onCheckedChange={(on) => setKey("onboot", on ? "1" : "0", "0")}
              />
            </div>
          </MarkedField>
          {kind === "qemu" && (
            <>
              <MarkedField modified={isModified("agent")}>
                <div className="flex items-center justify-between gap-3 py-1">
                  <span className="text-fg text-[13px] font-medium">{t("pve.guest.hwAgent")}</span>
                  <Switch
                    disabled={!canConfigure}
                    checked={agentEnabled(current("agent"))}
                    onCheckedChange={(on) => setKey("agent", agentToggle(orig("agent") ?? "", on), "")}
                  />
                </div>
              </MarkedField>
              <MarkedField modified={isModified("boot")}>
                <Field label={t("pve.guest.hwBoot")} hint={t("pve.guest.hwBootHint")}>
                  {(p) => (
                    <Input
                      {...p}
                      disabled={!canConfigure}
                      className="font-mono text-xs"
                      value={current("boot")}
                      onChange={(e) => setKey("boot", e.target.value)}
                    />
                  )}
                </Field>
              </MarkedField>
            </>
          )}
        </CardBody>
      </Card>

      {/* Network devices */}
      <Card>
        <CardHeader title={t("pve.guest.hwNetwork")} />
        <CardBody className="space-y-3">
          {netKeys.length === 0 && (
            <p className="text-fg-subtle py-2 text-center text-[13px]">{t("pve.guest.hwNoNet")}</p>
          )}
          {netKeys.map((key) => (
            <NetDeviceRow
              key={key}
              keyName={key}
              kind={kind}
              raw={current(key)}
              modified={isModified(key)}
              disabled={!canConfigure}
              onChange={(next) => setKey(key, next)}
            />
          ))}
        </CardBody>
      </Card>

      {/* Disks — read-only */}
      <Card>
        <CardHeader title={t("pve.guest.hwDisks")} description={t("pve.guest.hwDisksHint")} />
        <CardBody className="px-0 py-0">
          {diskKeys.length === 0 ? (
            <p className="text-fg-subtle px-5 py-4 text-center text-[13px]">{t("pve.guest.hwNoDisks")}</p>
          ) : (
            <ul>
              {diskKeys.map((key) => {
                const disk = diskView(current(key));
                return (
                  <li key={key} className="border-edge flex flex-wrap items-center gap-3 border-b px-5 py-3 last:border-b-0">
                    <HardDrive className="text-fg-subtle size-4 shrink-0" />
                    <code className="bg-bg-subtle text-fg rounded px-1.5 py-0.5 font-mono text-xs font-semibold">{key}</code>
                    <code className="text-fg-muted min-w-0 flex-1 truncate font-mono text-xs">{disk.volume}</code>
                    {disk.size && <Badge tone="neutral">{disk.size}</Badge>}
                    {disk.extras && (
                      <span className="text-fg-subtle w-full pl-7 font-mono text-[11px] sm:w-auto sm:pl-0">
                        {disk.extras}
                      </span>
                    )}
                  </li>
                );
              })}
            </ul>
          )}
        </CardBody>
      </Card>

      {/* Everything else — raw values */}
      <Card>
        <CardHeader title={t("pve.guest.hwOther")} description={t("pve.guest.hwOtherHint")} />
        <CardBody className="px-0 py-0">
          <Table>
            <THead>
              <tr>
                <TH className="w-52">{t("pve.guest.hardwareKey")}</TH>
                <TH>{t("pve.guest.hardwareValue")}</TH>
              </tr>
            </THead>
            <TBody>
              {otherKeys.map((key) => (
                <TR key={key} className={isModified(key) ? "bg-accent-soft/35" : undefined}>
                  <TD>
                    <span className="flex items-center gap-2">
                      <code className="text-fg font-mono text-xs">{key}</code>
                      {isModified(key) && <Badge tone="accent">{t("pve.guest.hwModified")}</Badge>}
                    </span>
                  </TD>
                  <TD>
                    {canConfigure ? (
                      <Input
                        className="h-7.5 font-mono text-xs"
                        value={current(key)}
                        onChange={(e) => setKey(key, e.target.value)}
                      />
                    ) : (
                      <code className="text-fg-muted font-mono text-xs break-all">{current(key)}</code>
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
                        onChange={(e) => setKey(newKey.trim(), e.target.value)}
                      />
                    )}
                  </TD>
                </TR>
              )}
            </TBody>
          </Table>
        </CardBody>
      </Card>

      {/* Pending-changes bar */}
      {canConfigure && pending > 0 && (
        <div className="bg-overlay border-edge sticky bottom-4 z-20 flex flex-wrap items-center justify-between gap-3 rounded-xl border px-4 py-3 shadow-3">
          <span className="text-fg flex items-center gap-2 text-sm font-medium">
            <Badge tone="accent">{pending}</Badge>
            {t("pve.guest.hwPending", { count: pending })}
          </span>
          <div className="flex gap-2">
            <Button variant="ghost" size="sm" onClick={() => setChanges({})}>
              {t("pve.guest.hwDiscard")}
            </Button>
            <Button variant="primary" size="sm" onClick={() => setConfirmOpen(true)}>
              {t("pve.guest.hardwareApply")}
            </Button>
          </div>
        </div>
      )}

      {/* Confirm: list every change, then one batched PUT */}
      <Dialog open={confirmOpen} onOpenChange={setConfirmOpen}>
        <DialogContent
          wide
          title={t("pve.guest.hwConfirmTitle", { count: pending })}
          description={t("pve.guest.hwConfirmBody")}
          footer={
            <>
              <Button variant="ghost" onClick={() => setConfirmOpen(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" loading={apply.isPending} onClick={() => apply.mutate()}>
                {t("pve.guest.hardwareApply")}
              </Button>
            </>
          }
        >
          <ul className="space-y-3">
            {Object.entries(changes).map(([key, value]) => {
              const before = orig(key);
              return (
                <li key={key} className="text-xs">
                  <code className="text-fg font-mono font-semibold">{key}</code>
                  <div className="mt-1 space-y-0.5 pl-3">
                    <p>
                      <code className={cn("text-fg-subtle font-mono break-all", before !== undefined && "line-through")}>
                        {before !== undefined && before !== "" ? before : t("pve.guest.hwUnset")}
                      </code>
                    </p>
                    <p>
                      <code className="text-fg font-mono break-all">{value || t("pve.guest.hwUnset")}</code>
                    </p>
                  </div>
                </li>
              );
            })}
          </ul>
        </DialogContent>
      </Dialog>
    </div>
  );
}

/** MarkedField draws the pending-change marker along a field's left edge. */
function MarkedField({ modified, children }: { modified: boolean; children: React.ReactNode }) {
  return (
    <div className={cn("-ml-3 border-l-2 pl-3 transition-colors", modified ? "border-accent" : "border-transparent")}>
      {children}
    </div>
  );
}

/** NetDeviceRow edits one netN entry via typed fields; unknown options ride along untouched. */
function NetDeviceRow({
  keyName,
  kind,
  raw,
  modified,
  disabled,
  onChange,
}: {
  keyName: string;
  kind: "qemu" | "lxc";
  raw: string;
  modified: boolean;
  disabled: boolean;
  onChange: (next: string) => void;
}) {
  const { t } = useTranslation();
  const view = netView(kind, raw);
  const edit = (patch: Partial<Pick<NetView, "model" | "mac" | "bridge" | "vlan">>) =>
    onChange(netEdit(kind, raw, patch));

  return (
    <div className={cn("rounded-lg border p-4 transition-colors", modified ? "border-accent" : "border-edge")}>
      <div className="mb-3 flex items-center gap-2">
        <Network className="text-fg-subtle size-4" />
        <code className="bg-bg-subtle text-fg rounded px-1.5 py-0.5 font-mono text-xs font-semibold">{keyName}</code>
        {modified && <Badge tone="accent">{t("pve.guest.hwModified")}</Badge>}
      </div>
      <div className={cn("grid gap-3 sm:grid-cols-2", kind === "qemu" ? "lg:grid-cols-4" : "lg:grid-cols-3")}>
        {kind === "qemu" && (
          <Field label={t("pve.guest.hwModel")}>
            {() => (
              <Select value={view.model || "virtio"} onValueChange={(v) => edit({ model: v })}>
                <SelectTrigger disabled={disabled}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {NET_MODELS.map((m) => (
                    <SelectItem key={m} value={m}>
                      {m}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>
        )}
        <Field label={t("pve.guest.hwBridge")}>
          {(p) => (
            <Input
              {...p}
              disabled={disabled}
              className="font-mono text-xs"
              placeholder="vmbr0"
              value={view.bridge}
              onChange={(e) => edit({ bridge: e.target.value })}
            />
          )}
        </Field>
        <Field label={t("pve.guest.hwVlan")} hint={t("pve.guest.hwVlanHint")}>
          {(p) => (
            <Input
              {...p}
              disabled={disabled}
              type="number"
              min={1}
              max={4094}
              value={view.vlan}
              onChange={(e) => edit({ vlan: e.target.value })}
            />
          )}
        </Field>
        <Field label={t("pve.guest.hwMac")}>
          {(p) => (
            <Input
              {...p}
              disabled={disabled}
              className="font-mono text-xs"
              placeholder="auto"
              value={view.mac}
              onChange={(e) => edit({ mac: e.target.value })}
            />
          )}
        </Field>
      </div>
      {view.extras && (
        <p className="text-fg-subtle mt-2.5 text-[11px]">
          {t("pve.guest.hwAlsoSet")} <code className="font-mono">{view.extras}</code>
        </p>
      )}
    </div>
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

// ---- console (native console first, SSH demoted) ---------------------------

type ConsoleMode = "vnc" | "serial" | "ssh";

/**
 * ConsoleTab leads with the guest's own console — noVNC for VMs, the serial
 * terminal for containers — one prominent Connect, no credentials involved.
 * SSH still exists but lives in a collapsed secondary section below, clearly
 * labeled as the option that needs stored guest credentials.
 */
function ConsoleTab({
  instanceId,
  vmid,
  kind,
  canConfigure,
}: {
  instanceId: string;
  vmid: number;
  kind: "qemu" | "lxc";
  canConfigure: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const [session, setSession] = useState<(TermSession & { mode: ConsoleMode }) | null>(null);

  const open = useMutation({
    mutationFn: async (consoleKind: "vnc" | "serial") => {
      const data = unwrap(
        await api.POST("/proxmox/{id}/guests/{vmid}/console", {
          params: { path: { id: instanceId, vmid } },
          body: { kind: consoleKind },
        }),
      );
      return { ...data, mode: consoleKind as ConsoleMode };
    },
    onSuccess: setSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const wsUrl = (path: string) =>
    `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${path}`;

  if (session) {
    const label =
      session.mode === "vnc"
        ? t("pve.guest.consoleVnc")
        : session.mode === "serial"
          ? t("pve.guest.consoleSerial")
          : t("pve.guest.consoleSsh");
    // Session tickets are single-use and expire server-side; the reconnect
    // button mints a fresh one instead of re-dialing the dead URL.
    const mode = session.mode;
    const renewSession = async (): Promise<TermSession> => {
      const data =
        mode === "ssh"
          ? unwrap(await api.POST("/proxmox/{id}/guests/{vmid}/ssh", { params: { path: { id: instanceId, vmid } } }))
          : unwrap(
              await api.POST("/proxmox/{id}/guests/{vmid}/console", {
                params: { path: { id: instanceId, vmid } },
                body: { kind: mode },
              }),
            );
      setSession({ ...data, mode });
      return data;
    };
    return (
      <div className="space-y-2.5">
        <div className="flex items-center justify-between gap-3">
          <p className="text-fg-muted flex items-center gap-2 text-[13px] font-medium">
            {session.mode === "vnc" ? <Monitor className="size-4" /> : <Terminal className="size-4" />}
            {label}
          </p>
          <Button size="sm" variant="secondary" onClick={() => setSession(null)}>
            {t("pve.guest.consoleDisconnect")}
          </Button>
        </div>
        {/* Fill the rest of the viewport instead of a fixed height, so the
            console uses the whole screen below the chrome above it. The VNC
            stage draws its own border hugging the scaled display, so only
            the terminal keeps the outer frame. */}
        <div
          className={session.mode === "vnc" ? undefined : "border-edge overflow-hidden rounded-lg border"}
          style={{ height: "max(480px, calc(100vh - 21rem))" }}
        >
          <Suspense fallback={<PageLoader className="h-full" />}>
            {session.mode === "vnc" ? (
              <VncViewer
                wsUrl={wsUrl(session.wsPath)}
                credential={session.credential ?? undefined}
                renew={async () => {
                  const fresh = await renewSession();
                  return { wsUrl: wsUrl(fresh.wsPath), credential: fresh.credential ?? undefined };
                }}
                className="h-full"
              />
            ) : (
              <TerminalPane
                wsUrl={wsUrl(session.wsPath)}
                renew={async () => wsUrl((await renewSession()).wsPath)}
                className="h-full"
                onExit={() => setSession(null)}
              />
            )}
          </Suspense>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <Card>
        <CardBody className="flex flex-col items-center gap-4 py-12 text-center">
          <span className="bg-accent-soft text-accent-soft-fg flex size-12 items-center justify-center rounded-2xl">
            {kind === "qemu" ? <Monitor className="size-6" /> : <Terminal className="size-6" />}
          </span>
          <div className="max-w-md">
            <h3 className="text-fg text-[15px] font-semibold">
              {kind === "qemu" ? t("pve.guest.consoleQemuTitle") : t("pve.guest.consoleLxcTitle")}
            </h3>
            <p className="text-fg-muted mt-1 text-[13px]">{t("pve.guest.consoleLead")}</p>
          </div>
          <Button
            variant="primary"
            size="lg"
            loading={open.isPending}
            onClick={() => open.mutate(kind === "lxc" ? "serial" : "vnc")}
          >
            {kind === "qemu" ? <Monitor className="size-4" /> : <Terminal className="size-4" />}
            {t("pve.guest.consoleConnect")}
          </Button>
          {kind === "qemu" && (
            <button
              className="text-fg-subtle hover:text-fg cursor-pointer text-[13px] underline-offset-2 transition-colors hover:underline"
              onClick={() => open.mutate("serial")}
            >
              {t("pve.guest.consoleSerialInstead")}
            </button>
          )}
        </CardBody>
      </Card>

      <SshSection
        instanceId={instanceId}
        vmid={vmid}
        canConfigure={canConfigure}
        onSession={(s) => setSession({ ...s, mode: "ssh" })}
      />
    </div>
  );
}

/**
 * SshSection is the demoted path into the guest: collapsed by default, opens
 * to the stored-credential settings plus a Connect button. Uses the guest OS
 * network stack, so it only works when sshd is reachable — the native console
 * above never needs any of that.
 */
function SshSection({
  instanceId,
  vmid,
  canConfigure,
  onSession,
}: {
  instanceId: string;
  vmid: number;
  canConfigure: boolean;
  onSession: (s: TermSession) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const resource = `proxmox/${instanceId}/guest/${vmid}`;
  const [expanded, setExpanded] = useState(false);

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
    enabled: expanded,
    retry: false,
  });

  const [host, setHost] = useState("");
  const [port, setPort] = useState("22");
  const [username, setUsername] = useState("root");
  const [authType, setAuthType] = useState<"password" | "key">("password");
  const [secret, setSecret] = useState("");
  // Picking a saved SSH host is the primary path; null means "type it here".
  const [hostId, setHostId] = useState<string | null>(null);
  const [hydrated, setHydrated] = useState(false);
  if (cred.data && !hydrated) {
    setHost(cred.data.host);
    setPort(String(cred.data.port));
    setUsername(cred.data.username);
    setAuthType(cred.data.authType);
    setHostId(cred.data.hostId ?? null);
    setHydrated(true);
  }

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PUT("/resources/ssh", {
          body: hostId
            ? { resource, hostId, username, authType }
            : {
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

  const connect = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/proxmox/{id}/guests/{vmid}/ssh", { params: { path: { id: instanceId, vmid } } })),
    onSuccess: onSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <button
        className="flex w-full cursor-pointer items-center gap-3 px-5 py-3.5 text-left"
        aria-expanded={expanded}
        onClick={() => setExpanded((v) => !v)}
      >
        <KeyRound className="text-fg-subtle size-4 shrink-0" />
        <span className="min-w-0 flex-1">
          <span className="text-fg block text-sm font-medium">{t("pve.guest.sshSecondaryTitle")}</span>
          <span className="text-fg-subtle block text-xs">{t("pve.guest.sshSecondaryHint")}</span>
        </span>
        <ChevronDown className={cn("text-fg-subtle size-4 shrink-0 transition-transform", expanded && "rotate-180")} />
      </button>

      {expanded && (
        <div className="border-edge grid items-start gap-6 border-t px-5 py-4 lg:grid-cols-2">
          <div className="space-y-4">
            <p className="text-fg-muted text-[13px]">{t("pve.guest.sshIntro")}</p>
            {/* The registry is the default path — manual entry stays available
                for one-off guests that don't warrant a saved host. */}
            <Field label={t("ssh.picker.label")} hint={t("ssh.picker.hint")}>
              {() => <SSHHostPicker value={hostId} onChange={setHostId} />}
            </Field>
            <div className={cn("grid grid-cols-3 gap-3", hostId && "hidden")}>
              <Field label={t("pve.guest.sshHost")} className="col-span-2">
                {(p) => <Input {...p} disabled={!canConfigure} placeholder="10.0.0.14" value={host} onChange={(e) => setHost(e.target.value)} />}
              </Field>
              <Field label={t("pve.guest.sshPort")}>
                {(p) => <Input {...p} disabled={!canConfigure} type="number" value={port} onChange={(e) => setPort(e.target.value)} />}
              </Field>
            </div>
            <div className={cn(hostId && "hidden")}>
              <Field label={t("pve.guest.sshUser")}>
                {(p) => <Input {...p} disabled={!canConfigure} value={username} onChange={(e) => setUsername(e.target.value)} />}
              </Field>
            </div>
            {canConfigure && !hostId && (
              <>
                <Field label={t("pve.guest.sshAuthType")}>
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
              </>
            )}
            {/* Save lives outside the manual-entry block: binding a saved host
                needs saving too (it was unreachable when a host was picked). */}
            {canConfigure && (
              <div className="flex justify-end">
                <Button
                  variant="secondary"
                  size="sm"
                  disabled={
                    hostId
                      ? false
                      : !host.trim() || !username.trim() || (!secret && !cred.data)
                  }
                  loading={save.isPending}
                  onClick={() => save.mutate()}
                >
                  {t("common.save")}
                </Button>
              </div>
            )}
          </div>

          <div className="bg-bg-subtle rounded-lg">
            {cred.data ? (
              <div className="flex flex-col items-center gap-3 px-4 py-10 text-center">
                <p className="text-fg-muted font-mono text-[13px]">
                  {cred.data.username}@{cred.data.host}:{cred.data.port}
                </p>
                <Button variant="secondary" loading={connect.isPending} onClick={() => connect.mutate()}>
                  <KeyRound className="size-3.5" />
                  {t("pve.guest.sshConnect")}
                </Button>
              </div>
            ) : (
              <EmptyState title={t("pve.guest.sshNotConfigured")} className="py-10" />
            )}
          </div>
        </div>
      )}
    </Card>
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
