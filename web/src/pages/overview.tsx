import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Activity,
  Cctv,
  Container,
  Database,
  FileClock,
  Gamepad2,
  HardDrive,
  Network,
  Pickaxe,
  Server,
  ShieldCheck,
  Users,
  Waypoints,
} from "lucide-react";
import { api, unwrap, type Integration } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { RES } from "@/lib/perms";
import { statusTone, systemTypeIcon, useIntegrations } from "@/lib/systems";
import { formatBytes, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Skeleton } from "@/ui/skeleton";

/**
 * OverviewPage is the home dashboard: every connected system's headline
 * number at a glance, each card linking into its module. Everything rides
 * existing RBAC-filtered endpoints — a viewer sees only their slice.
 */
export function OverviewPage() {
  const { t } = useTranslation();
  const { me, can } = useAuth();
  const integrations = useIntegrations();
  const rows = (integrations.data ?? []).filter((row) => row.enabled);
  const byType = (type: string) => rows.filter((row) => row.type === type);
  const first = (type: string): Integration | undefined => byType(type)[0];

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

  // ---- aggregates -----------------------------------------------------------

  const mcConnected = mcRows.filter((_row, i) => mcOverviews[i]?.data?.connected).length;
  const mcPlayers = mcRows.reduce((sum, _row, i) => {
    const o = mcOverviews[i]?.data;
    return sum + (o?.connected ? (o.status?.playersOnline ?? 0) : 0);
  }, 0);

  const cameras = nvrOverview.data?.cameras ?? [];
  const camerasRecording = cameras.filter((c) => c.recordingOk).length;

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
      </div>

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

      {integrations.isLoading ? (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton key={i} className="h-24 rounded-xl" />
          ))}
        </div>
      ) : (
        <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {nvr && (
            <StatCard
              to={`/nvr/${nvr.id}`}
              icon={Cctv}
              value={`${camerasRecording} / ${cameras.length}`}
              label={t("overview.cards.cameras")}
              warn={cameras.length > 0 && camerasRecording < cameras.length}
            />
          )}
          {pve && (
            <StatCard
              to={`/pve/${pve.id}`}
              icon={Server}
              value={`${nodesOnline} / ${pveSummary.data?.nodes.length ?? "—"}`}
              label={t("overview.cards.nodes")}
            />
          )}
          {mcRows.length > 0 && (
            <StatCard
              to="/mc"
              icon={Pickaxe}
              value={`${mcConnected} / ${mcRows.length}`}
              label={t("overview.cards.mcServers", { players: mcPlayers })}
            />
          )}
          {nas && (
            <StatCard
              to={`/truenas/${nas.id}`}
              icon={HardDrive}
              value={poolTotal > 0 ? `${Math.round((poolUsed / poolTotal) * 100)}%` : "—"}
              label={t("overview.cards.storage", {
                used: formatBytes(poolUsed),
                total: formatBytes(poolTotal),
              })}
              warn={poolTotal > 0 && poolUsed / poolTotal > 0.85}
            />
          )}
          {ts && (
            <StatCard
              to={`/ts/${ts.id}`}
              icon={Network}
              value={`${devicesOnline} / ${devices.length}`}
              label={t("overview.cards.tailnet")}
            />
          )}
          {(dns.data ?? []).length > 0 && (
            <StatCard
              to="/dns"
              icon={ShieldCheck}
              value={
                dnsStats.queries > 0
                  ? `${Math.round((dnsStats.blocked / dnsStats.queries) * 100)}%`
                  : "—"
              }
              label={t("overview.cards.dns", { blocked: dnsStats.blocked.toLocaleString() })}
            />
          )}
          {docker && (
            <StatCard
              to="/docker"
              icon={Container}
              value={`${containersRunning} / ${containers.length}`}
              label={t("overview.cards.containers")}
            />
          )}
          {byType("amp").length > 0 && (
            <StatCard
              to="/amp"
              icon={Gamepad2}
              value={`${ampRunning} / ${ampTotal}`}
              label={t("overview.cards.ampServers")}
            />
          )}
          {caddy && (
            <StatCard
              to={`/caddy/${caddy.id}`}
              icon={Waypoints}
              value={String((caddySites.data?.entries.length ?? 0) + (caddySites.data?.rawSites.length ?? 0))}
              label={t("overview.cards.caddySites")}
            />
          )}
          {byType("mysql").length + byType("mongodb").length > 0 && (
            <StatCard
              to="/db"
              icon={Database}
              value={String(byType("mysql").length + byType("mongodb").length)}
              label={t("overview.cards.databases")}
            />
          )}
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
