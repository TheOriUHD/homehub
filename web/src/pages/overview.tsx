import { useCallback, useEffect, useState } from "react";
import { useMutation, useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Activity,
  BellRing,
  Cctv,
  Container,
  Cpu,
  Database,
  DatabaseBackup,
  FileClock,
  Gamepad2,
  HardDrive,
  Network,
  Pickaxe,
  Pin,
  Server,
  SlidersHorizontal,
  ShieldCheck,
  Users,
  Waypoints,
} from "lucide-react";
import { api, isApiError, unwrap, type Integration } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { statusTone, systemTypeIcon, useIntegrations } from "@/lib/systems";
import { cn, formatBytes, formatDuration, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Skeleton } from "@/ui/skeleton";
import { Switch } from "@/ui/switch";

/**
 * OverviewPage is the home dashboard: HomeHub's own vitals up top, every
 * connected system's headline number below, each card linking into its
 * module. Everything rides existing RBAC-filtered endpoints — a viewer
 * sees only their slice. Pinned cards sort first and hidden cards don't render — per-user, server-side.
 */

/** Per-user overview layout, stored server-side under pref key "overview"
 * so it follows the login across devices. `pins` order first; `hidden`
 * cards don't render at all. */
type OverviewPrefs = { pins: string[]; hidden: string[] };

const EMPTY_PREFS: OverviewPrefs = { pins: [], hidden: [] };
/** Pre-server pins lived in localStorage; adopted once, then ignored. */
const LEGACY_PINS_KEY = "hh.overview.pins";

function parsePrefs(value: Record<string, unknown> | null | undefined): OverviewPrefs | null {
  if (!value) return null;
  const arr = (v: unknown) =>
    Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
  return { pins: arr(value.pins), hidden: arr(value.hidden) };
}

export function OverviewPage() {
  const { t } = useTranslation();
  const { me, can } = useAuth();
  const integrations = useIntegrations();
  const rows = (integrations.data ?? []).filter((row) => row.enabled);
  const byType = (type: string) => rows.filter((row) => row.type === type);
  const first = (type: string): Integration | undefined => byType(type)[0];

  const [prefs, setPrefs] = useState<OverviewPrefs | null>(null);
  const [customizeOpen, setCustomizeOpen] = useState(false);

  const prefsQuery = useQuery({
    queryKey: ["prefs", "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/prefs/{key}", { params: { path: { key: "overview" } } })),
  });
  const savePrefs = useMutation({
    mutationFn: async (next: OverviewPrefs) => {
      unwrap(
        await api.PUT("/prefs/{key}", {
          params: { path: { key: "overview" } },
          body: { value: next },
        }),
      );
    },
  });

  // Hydrate once; a user who pinned before this was server-side gets their
  // localStorage pins adopted into the account on first load.
  useEffect(() => {
    if (prefs !== null || prefsQuery.data === undefined) return;
    const stored = parsePrefs(prefsQuery.data.value);
    if (stored) {
      setPrefs(stored);
      return;
    }
    let legacy: string[] = [];
    try {
      const v: unknown = JSON.parse(localStorage.getItem(LEGACY_PINS_KEY) ?? "[]");
      legacy = Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : [];
    } catch {
      /* ignore */
    }
    const initial = { ...EMPTY_PREFS, pins: legacy };
    setPrefs(initial);
    if (legacy.length > 0) {
      savePrefs.mutate(initial);
      localStorage.removeItem(LEGACY_PINS_KEY);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [prefsQuery.data, prefs]);

  const pins = prefs?.pins ?? [];
  const hidden = prefs?.hidden ?? [];
  const update = useCallback(
    (next: OverviewPrefs) => {
      setPrefs(next);
      savePrefs.mutate(next);
    },
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );
  const togglePin = useCallback(
    (key: string) => {
      const cur = prefs ?? EMPTY_PREFS;
      update({
        ...cur,
        pins: cur.pins.includes(key) ? cur.pins.filter((x) => x !== key) : [...cur.pins, key],
      });
    },
    [prefs, update],
  );
  const toggleHidden = useCallback(
    (key: string) => {
      const cur = prefs ?? EMPTY_PREFS;
      update({
        ...cur,
        hidden: cur.hidden.includes(key) ? cur.hidden.filter((x) => x !== key) : [...cur.hidden, key],
      });
    },
    [prefs, update],
  );

  const pve = first("proxmox");
  const pveSummary = useQuery({
    queryKey: ["pve", pve?.id, "summary"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/summary", { params: { path: { id: pve!.id } } })),
    enabled: !!pve,
    refetchInterval: 30_000,
  });

  const mcRows = byType("minecraft");
  const mcOverviews = useQueries({
    queries: mcRows.map((row) => ({
      queryKey: ["mc", row.id, "overview"],
      queryFn: async () =>
        unwrap(await api.GET("/mc/{id}/overview", { params: { path: { id: row.id } } })),
      refetchInterval: 30_000,
    })),
  });

  const nvr = first("aperture");
  const nvrOverview = useQuery({
    queryKey: ["nvr", nvr?.id, "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/nvr/{id}/overview", { params: { path: { id: nvr!.id } } })),
    enabled: !!nvr,
    refetchInterval: 30_000,
  });

  const nas = first("truenas");
  const nasOverview = useQuery({
    queryKey: ["truenas", nas?.id, "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/truenas/{id}/overview", { params: { path: { id: nas!.id } } })),
    enabled: !!nas,
    refetchInterval: 60_000,
  });

  const ts = first("tailscale");
  const tsDevices = useQuery({
    queryKey: ["ts", ts?.id, "devices"],
    queryFn: async () =>
      unwrap(await api.GET("/ts/{id}/devices", { params: { path: { id: ts!.id } } })),
    enabled: !!ts,
    refetchInterval: 60_000,
  });

  const dns = useQuery({
    queryKey: ["dns", "instances"],
    queryFn: async () => unwrap(await api.GET("/dns/instances")),
    enabled: byType("pihole").length + byType("adguard").length > 0,
    refetchInterval: 60_000,
  });

  const docker = first("docker");
  const dockerContainers = useQuery({
    queryKey: ["docker", docker?.id, "containers"],
    queryFn: async () =>
      unwrap(await api.GET("/docker/{id}/containers", { params: { path: { id: docker!.id } } })),
    enabled: !!docker,
    refetchInterval: 60_000,
  });

  const ampPanels = useQuery({
    queryKey: ["amp", "instances"],
    queryFn: async () => unwrap(await api.GET("/amp/instances")),
    enabled: byType("amp").length > 0,
    refetchInterval: 60_000,
  });

  const caddy = first("caddy");
  const caddySites = useQuery({
    queryKey: ["caddy", caddy?.id, "sites"],
    queryFn: async () =>
      unwrap(await api.GET("/caddy/{id}/sites", { params: { path: { id: caddy!.id } } })),
    enabled: !!caddy,
    refetchInterval: 60_000,
  });

  const canAudit = can(RES.audit);
  const audit = useQuery({
    queryKey: ["audit", "recent"],
    queryFn: async () => unwrap(await api.GET("/audit", { params: { query: { limit: 6 } } })),
    enabled: canAudit,
  });

  // Alerts are RBAC-filtered server-side; a 403 means the user can't see
  // the alerting surface at all, so the firing panel simply doesn't render.
  const alerts = useQuery({
    queryKey: ["alerts"],
    queryFn: async () => unwrap(await api.GET("/alerts")),
    refetchInterval: 60_000,
  });
  const alertsVisible = !(alerts.isError && isApiError(alerts.error) && alerts.error.status === 403);
  const firing = alertsVisible ? (alerts.data ?? []).filter((r) => r.enabled && r.state === "firing") : [];

  // ---- aggregates -----------------------------------------------------------

  const mcConnected = mcRows.filter((_row, i) => mcOverviews[i]?.data?.connected).length;
  const mcPlayers = mcRows.reduce((sum, _row, i) => {
    const o = mcOverviews[i]?.data;
    return sum + (o?.connected ? (o.status?.playersOnline ?? 0) : 0);
  }, 0);

  const cameras = nvrOverview.data?.cameras ?? [];
  // Only cameras that are supposed to record count against the total —
  // live-only cameras are healthy without footage.
  const camerasExpected = cameras.filter((c) => c.recordingExpected);
  const camerasRecording = camerasExpected.filter((c) => c.recordingOk).length;

  const pools = nasOverview.data?.pools ?? [];
  const poolUsed = pools.reduce((sum, p) => sum + (p.allocatedBytes ?? 0), 0);
  const poolTotal = pools.reduce((sum, p) => sum + (p.sizeBytes ?? 0), 0);

  const devices = tsDevices.data ?? [];
  const devicesOnline = devices.filter((d) => d.online).length;

  const dnsStats = (dns.data ?? []).reduce(
    (acc, inst) => {
      if (inst.stats) {
        acc.queries += inst.stats.queriesToday ?? 0;
        acc.blocked += inst.stats.blockedToday ?? 0;
      }
      return acc;
    },
    { queries: 0, blocked: 0 },
  );

  const containers = dockerContainers.data ?? [];
  const containersRunning = containers.filter((c) => c.state === "running").length;

  const ampRunning = (ampPanels.data ?? []).reduce((sum, p) => sum + p.runningCount, 0);
  const ampTotal = (ampPanels.data ?? []).reduce((sum, p) => sum + p.instanceCount, 0);

  const nodesOnline = (pveSummary.data?.nodes ?? []).filter((n) => n.status === "online").length;

  const problems = rows.filter((row) => row.status === "unreachable" || row.status === "degraded");

  // ---- stat cards, pinned-first ---------------------------------------------

  type CardDef = { key: string; title: string; node: React.ReactNode };
  const defs: CardDef[] = [];
  const add = (key: string, node: React.ReactNode) =>
    defs.push({ key, title: t(`overview.cardNames.${key}`), node });

  if (nvr) {
    add(
      "nvr",
      <StatCard
        to={`/nvr/${nvr.id}`}
        icon={Cctv}
        value={
          camerasExpected.length > 0
            ? `${camerasRecording} / ${camerasExpected.length}`
            : String(cameras.length)
        }
        label={
          camerasExpected.length > 0
            ? t("overview.cards.cameras")
            : t("overview.cards.camerasLiveOnly")
        }
        warn={camerasExpected.length > 0 && camerasRecording < camerasExpected.length}
      />,
    );
  }
  if (pve) {
    add(
      "pve",
      <StatCard
        to={`/pve/${pve.id}`}
        icon={Server}
        value={`${nodesOnline} / ${pveSummary.data?.nodes.length ?? "—"}`}
        label={t("overview.cards.nodes")}
      />,
    );
  }
  if (mcRows.length > 0) {
    add(
      "mc",
      <StatCard
        to="/mc"
        icon={Pickaxe}
        value={`${mcConnected} / ${mcRows.length}`}
        label={t("overview.cards.mcServers", { players: mcPlayers })}
      />,
    );
  }
  if (nas) {
    add(
      "nas",
      <StatCard
        to={`/truenas/${nas.id}`}
        icon={HardDrive}
        value={poolTotal > 0 ? `${Math.round((poolUsed / poolTotal) * 100)}%` : "—"}
        label={t("overview.cards.storage", {
          used: formatBytes(poolUsed),
          total: formatBytes(poolTotal),
        })}
        warn={poolTotal > 0 && poolUsed / poolTotal > 0.85}
      />,
    );
  }
  if (ts) {
    add(
      "ts",
      <StatCard
        to={`/ts/${ts.id}`}
        icon={Network}
        value={`${devicesOnline} / ${devices.length}`}
        label={t("overview.cards.tailnet")}
      />,
    );
  }
  if ((dns.data ?? []).length > 0) {
    add(
      "dns",
      <StatCard
        to="/dns"
        icon={ShieldCheck}
        value={
          dnsStats.queries > 0 ? `${Math.round((dnsStats.blocked / dnsStats.queries) * 100)}%` : "—"
        }
        label={t("overview.cards.dns", { blocked: dnsStats.blocked.toLocaleString() })}
      />,
    );
  }
  if (docker) {
    add(
      "docker",
      <StatCard
        to="/docker"
        icon={Container}
        value={`${containersRunning} / ${containers.length}`}
        label={t("overview.cards.containers")}
      />,
    );
  }
  if (byType("amp").length > 0) {
    add(
      "amp",
      <StatCard
        to="/amp"
        icon={Gamepad2}
        value={`${ampRunning} / ${ampTotal}`}
        label={t("overview.cards.ampServers")}
      />,
    );
  }
  if (caddy) {
    add(
      "caddy",
      <StatCard
        to={`/caddy/${caddy.id}`}
        icon={Waypoints}
        value={String((caddySites.data?.entries.length ?? 0) + (caddySites.data?.rawSites.length ?? 0))}
        label={t("overview.cards.caddySites")}
      />,
    );
  }
  if (byType("mysql").length + byType("mongodb").length > 0) {
    add(
      "db",
      <StatCard
        to="/db"
        icon={Database}
        value={String(byType("mysql").length + byType("mongodb").length)}
        label={t("overview.cards.databases")}
      />,
    );
  }

  const visible = defs.filter((d) => !hidden.includes(d.key));
  const ordered = [
    ...pins.map((key) => visible.find((d) => d.key === key)).filter((d): d is CardDef => !!d),
    ...visible.filter((d) => !pins.includes(d.key)),
  ];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">
            {t("overview.greeting", { name: me?.user.displayName.split(" ")[0] })}
          </h1>
          <p className="text-fg-muted mt-1 text-sm">
            {problems.length === 0
              ? t("overview.allQuiet", { count: rows.length })
              : t("overview.problems", { count: problems.length })}
          </p>
        </div>
        <Button size="sm" variant="ghost" onClick={() => setCustomizeOpen(true)}>
          <SlidersHorizontal className="size-3.5" />
          {t("overview.customize")}
        </Button>
      </div>

      {customizeOpen && (
        <CustomizeDialog
          cards={[{ key: "hub", title: t("overview.cardNames.hub"), node: null }, ...defs]}
          pins={pins}
          hidden={hidden}
          onTogglePin={togglePin}
          onToggleHidden={toggleHidden}
          onClose={() => setCustomizeOpen(false)}
        />
      )}

      {can(RES.health) && !hidden.includes("hub") && (
        <HomeHubCard firingCount={alertsVisible ? firing.length : null} />
      )}

      {problems.length > 0 && (
        <div className="flex flex-wrap gap-2">
          {problems.map((row) => {
            const Icon = systemTypeIcon(row.type);
            return (
              <Badge key={row.id} tone={statusTone[row.status]}>
                <Icon className="size-3" />
                {row.name}
                <StatusDot tone={statusTone[row.status]} pulse />
              </Badge>
            );
          })}
        </div>
      )}

      {firing.length > 0 && (
        <Card className="border-danger/40">
          <CardHeader
            title={
              <span className="text-danger flex items-center gap-2">
                <BellRing className="size-4" />
                {t("overview.firingTitle", { count: firing.length })}
              </span>
            }
            actions={
              <Link to="/alerts" className="text-accent text-[13px] font-medium hover:underline">
                {t("overview.viewAll")}
              </Link>
            }
          />
          <CardBody className="space-y-2">
            {firing.slice(0, 5).map((rule) => (
              <div key={rule.id} className="flex items-center gap-2.5 text-[13px]">
                <StatusDot tone="danger" pulse />
                <span className="text-fg min-w-0 flex-1 truncate font-medium">{rule.name}</span>
                <span className="text-fg-muted shrink-0 font-mono text-xs">{rule.resource}</span>
                <span className="text-fg-subtle shrink-0 text-xs">
                  {formatRelative(rule.stateChangedAt)}
                </span>
              </div>
            ))}
          </CardBody>
        </Card>
      )}

      {integrations.isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {ordered.map((d) => (
            <PinnableCard
              key={d.key}
              pinned={pins.includes(d.key)}
              onToggle={() => togglePin(d.key)}
              pinLabel={pins.includes(d.key) ? t("overview.unpin") : t("overview.pin")}
            >
              {d.node}
            </PinnableCard>
          ))}
        </div>
      )}

      <div className="grid items-start gap-3 lg:grid-cols-2">
        <Card>
          <CardHeader title={t("overview.systemsTitle")} />
          <CardBody className="flex flex-wrap gap-2">
            {rows.map((row) => {
              const Icon = systemTypeIcon(row.type);
              return (
                <Link
                  key={row.id}
                  to="/systems"
                  className="border-edge bg-surface hover:bg-surface-hover flex items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[12.5px] font-medium transition-colors"
                >
                  <Icon className="text-fg-subtle size-3.5" />
                  <span className="text-fg">{row.name}</span>
                  <StatusDot tone={statusTone[row.status]} pulse={row.status !== "connected"} />
                </Link>
              );
            })}
            {rows.length === 0 && (
              <p className="text-fg-muted text-[13px]">{t("overview.noSystems")}</p>
            )}
          </CardBody>
        </Card>

        {canAudit && (
          <Card>
            <CardHeader
              title={
                <span className="flex items-center gap-2">
                  <FileClock className="text-fg-subtle size-4" />
                  {t("overview.recentActivity")}
                </span>
              }
              actions={
                <Link to="/admin/audit" className="text-accent text-[13px] font-medium hover:underline">
                  {t("overview.viewAll")}
                </Link>
              }
            />
            <CardBody className="space-y-2">
              {(audit.data?.entries ?? []).map((entry) => (
                <div key={entry.id} className="flex items-center gap-2.5 text-[13px]">
                  <Activity className="text-fg-subtle size-3.5 shrink-0" />
                  <span className="text-fg min-w-0 flex-1 truncate">
                    <span className="font-medium">{entry.actorName}</span> · {entry.action}
                  </span>
                  <span className="text-fg-subtle shrink-0 text-xs">{formatRelative(entry.ts)}</span>
                </div>
              ))}
              {audit.data?.entries.length === 0 && (
                <p className="text-fg-muted text-[13px]">{t("overview.noActivity")}</p>
              )}
            </CardBody>
          </Card>
        )}
      </div>
    </div>
  );
}

/**
 * One menu for the whole overview: every card in natural order, each with
 * a visibility switch and a pin toggle. Pinned cards sort first in the
 * grid; hidden cards don't render. Saved per user, server-side.
 */
function CustomizeDialog({
  cards,
  pins,
  hidden,
  onTogglePin,
  onToggleHidden,
  onClose,
}: {
  cards: Array<{ key: string; title: string; node: React.ReactNode }>;
  pins: string[];
  hidden: string[];
  onTogglePin: (key: string) => void;
  onToggleHidden: (key: string) => void;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("overview.customizeTitle")}
        footer={
          <Button variant="primary" onClick={onClose}>
            {t("common.done")}
          </Button>
        }
      >
        <div className="space-y-1">
          <p className="text-fg-muted mb-3 text-[13px]">{t("overview.customizeBody")}</p>
          {cards.map((card) => {
            const isHidden = hidden.includes(card.key);
            const isPinned = pins.includes(card.key);
            return (
              <div
                key={card.key}
                className="hover:bg-bg-subtle flex items-center gap-3 rounded-lg px-2 py-2 transition-colors"
              >
                <span className={cn("min-w-0 flex-1 truncate text-sm", isHidden ? "text-fg-subtle" : "text-fg")}>
                  {card.title}
                </span>
                {card.key !== "hub" && (
                  <button
                    type="button"
                    aria-label={isPinned ? t("overview.unpin") : t("overview.pin")}
                    aria-pressed={isPinned}
                    disabled={isHidden}
                    onClick={() => onTogglePin(card.key)}
                    className={cn(
                      "rounded-md p-1 transition-colors disabled:opacity-30",
                      isPinned ? "text-accent" : "text-fg-subtle hover:text-fg",
                    )}
                  >
                    <Pin className={cn("size-4", isPinned && "fill-current")} />
                  </button>
                )}
                <Switch
                  checked={!isHidden}
                  onCheckedChange={() => onToggleHidden(card.key)}
                  aria-label={t("overview.showCard", { name: card.title })}
                />
              </div>
            );
          })}
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * HomeHub's own vitals in one wide bar: version + uptime, database size and
 * round-trip, job queue, live WebSocket connections, alerts firing, and the
 * last backup. Gated on system/health — the same data the health page owns.
 */
function HomeHubCard({ firingCount }: { firingCount: number | null }) {
  const { t } = useTranslation();

  const health = useQuery({
    queryKey: ["health"],
    queryFn: async () => unwrap(await api.GET("/health")),
    refetchInterval: 30_000,
  });

  const runs = useQuery({
    queryKey: ["backups", "runs", "latest"],
    queryFn: async () =>
      unwrap(await api.GET("/backups/runs", { params: { query: { limit: 10 } } })),
    refetchInterval: 5 * 60_000,
  });
  const backupsVisible = !(runs.isError && isApiError(runs.error) && runs.error.status === 403);
  const lastBackup = (runs.data ?? []).find((r) => r.kind === "app-backup");

  const h = health.data;
  if (health.isError) return null;

  return (
    <Card>
      <CardBody className="flex flex-wrap items-center gap-x-7 gap-y-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 shrink-0 items-center justify-center rounded-xl">
          <Cpu className="size-4.5" />
        </span>

        <HubStat
          label={t("overview.hub.app")}
          value={h ? `v${h.version}` : "…"}
          detail={h ? t("overview.hub.uptime", { uptime: formatDuration(h.uptimeSeconds) }) : undefined}
        />
        <HubStat
          label={t("overview.hub.database")}
          value={h ? formatBytes(h.database.sizeBytes) : "…"}
          detail={h ? t("overview.hub.dbLatency", { ms: h.database.latencyMs.toFixed(1) }) : undefined}
          warn={h ? !h.database.reachable : false}
        />
        <HubStat
          label={t("overview.hub.jobs")}
          value={h ? String(h.jobs.running + h.jobs.pending) : "…"}
          detail={
            h
              ? h.jobs.failedRecent > 0
                ? t("overview.hub.jobsFailed", { count: h.jobs.failedRecent })
                : t("overview.hub.jobsDetail", { running: h.jobs.running, pending: h.jobs.pending })
              : undefined
          }
          warn={(h?.jobs.failedRecent ?? 0) > 0}
        />
        <HubStat
          label={t("overview.hub.live")}
          value={h ? String(h.websocketClients) : "…"}
          detail={t("overview.hub.liveDetail")}
        />
        {firingCount !== null && (
          <Link to="/alerts" className="group">
            <HubStat
              label={t("overview.hub.alerts")}
              value={String(firingCount)}
              detail={
                firingCount > 0 ? t("overview.hub.alertsFiring") : t("overview.hub.alertsQuiet")
              }
              warn={firingCount > 0}
            />
          </Link>
        )}
        {backupsVisible && (
          <Link to="/backups" className="group">
            <HubStat
              label={t("overview.hub.backup")}
              value={
                lastBackup
                  ? lastBackup.status === "failed"
                    ? t("overview.hub.backupFailed")
                    : formatRelative(lastBackup.startedAt)
                  : t("overview.hub.backupNever")
              }
              detail={lastBackup ? lastBackup.targetName : undefined}
              warn={lastBackup?.status === "failed" || !lastBackup}
              icon={DatabaseBackup}
            />
          </Link>
        )}

        <Link
          to="/health"
          className="text-accent ml-auto shrink-0 text-[13px] font-medium hover:underline"
        >
          {t("overview.hub.details")}
        </Link>
      </CardBody>
    </Card>
  );
}

function HubStat({
  label,
  value,
  detail,
  warn,
  icon: Icon,
}: {
  label: string;
  value: string;
  detail?: string;
  warn?: boolean;
  icon?: typeof Users;
}) {
  return (
    <div className="min-w-0">
      <p className="text-fg-subtle text-[10.5px] font-semibold tracking-wide uppercase">{label}</p>
      <p
        className={cn(
          "flex items-center gap-1.5 text-[15px] leading-tight font-semibold tabular-nums",
          warn ? "text-warning" : "text-fg",
        )}
      >
        {Icon && <Icon className="size-3.5 shrink-0" />}
        {value}
      </p>
      {detail && <p className="text-fg-muted truncate text-[11.5px]">{detail}</p>}
    </div>
  );
}

/** Wraps a stat card with a pin toggle: pinned cards sort first and keep the
 * pin visible; unpinned cards reveal it on hover. Stored per browser. */
function PinnableCard({
  pinned,
  onToggle,
  pinLabel,
  children,
}: {
  pinned: boolean;
  onToggle: () => void;
  pinLabel: string;
  children: React.ReactNode;
}) {
  return (
    <div className="group/pin relative">
      {children}
      <button
        type="button"
        aria-label={pinLabel}
        aria-pressed={pinned}
        onClick={(e) => {
          e.preventDefault();
          e.stopPropagation();
          onToggle();
        }}
        className={cn(
          "absolute top-2 right-2 rounded-md p-1 transition-opacity",
          pinned
            ? "text-accent opacity-100"
            : "text-fg-subtle hover:text-fg opacity-0 group-hover/pin:opacity-100",
        )}
      >
        <Pin className={cn("size-3.5", pinned && "fill-current")} />
      </button>
    </div>
  );
}

function StatCard({
  to,
  icon: Icon,
  value,
  label,
  warn,
}: {
  to: string;
  icon: typeof Users;
  value: string;
  label: string;
  warn?: boolean;
}) {
  return (
    <Link to={to} className="group block">
      <Card className="hover:border-accent/50 h-full transition-colors">
        <CardBody className="flex items-center gap-3.5">
          <span
            className={
              warn
                ? "bg-warning-soft text-warning-soft-fg flex size-10 shrink-0 items-center justify-center rounded-xl"
                : "bg-accent-soft text-accent-soft-fg flex size-10 shrink-0 items-center justify-center rounded-xl"
            }
          >
            <Icon className="size-4.5" />
          </span>
          <div className="min-w-0">
            <p className="text-fg text-xl font-semibold tracking-tight">{value}</p>
            <p className="text-fg-muted truncate text-[12.5px]">{label}</p>
          </div>
        </CardBody>
      </Card>
    </Link>
  );
}
