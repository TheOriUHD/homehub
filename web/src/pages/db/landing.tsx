import { useQueries, useQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  Activity,
  AlertTriangle,
  ArrowUpRight,
  Boxes,
  Database,
  Layers,
  SquareTerminal,
  Users,
} from "lucide-react";
import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { api, unwrap, type DbInstance } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { useMetricSeries } from "@/lib/metrics";
import { statusTone } from "@/lib/systems";
import { cn, formatBytes, formatDuration } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { Skeleton } from "@/ui/skeleton";
import { Sparkline } from "@/ui/sparkline";
import { EmptyState, ErrorState } from "@/ui/states";
import { TimeSeriesChart } from "@/components/time-series-chart";
import { dbResource, engineBadgeLabel, engineIcon, useDbInstances } from "./shared";

/**
 * DbLandingPage is the fleet dashboard for every database instance the user
 * may see: aggregate tiles up top (health, combined throughput, database
 * count, engines) and one rich card per instance with a live ops/s sparkline,
 * key stats, and quick links into the module.
 */
export function DbLandingPage() {
  const { t } = useTranslation();
  const { can } = useAuth();

  const instances = useDbInstances();
  useLiveInvalidate("integrations", [["db", "instances"]]);

  const list = instances.data ?? [];
  const connected = list.filter((i) => i.status === "connected");

  // One databases query per connected instance — the same keys the module's
  // tabs use, so whatever was fetched there is reused here and vice versa.
  const databaseCounts = useQueries({
    queries: connected.map((instance) => ({
      queryKey: ["db", instance.id, "databases"],
      queryFn: async () =>
        unwrap(await api.GET("/db/{id}/databases", { params: { path: { id: instance.id } } })),
      staleTime: 30_000,
    })),
  });

  if (instances.isLoading) return <LandingSkeleton />;
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }

  if (list.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Database}
          title={t("db.empty.title")}
          body={t("db.empty.body")}
          action={
            can("system/integrations", "configure") ? (
              <Link to="/systems" className="text-accent text-[13px] font-medium hover:underline">
                {t("db.empty.action")} →
              </Link>
            ) : undefined
          }
        />
      </Card>
    );
  }

  // A single instance gets the whole page — deep stats, live charts, database
  // and session leaderboards — instead of fleet tiles plus one card.
  if (list.length === 1) {
    return (
      <SingleDbDashboard
        instance={list[0]}
        consoleVisible={list[0].engine === "mysql" && can(dbResource(list[0]), "control")}
      />
    );
  }

  const online = connected.length;
  const totalOps = connected.reduce((sum, i) => sum + (i.stats?.opsPerSec ?? 0), 0);

  // Sum settles once every per-instance list has answered; partial sums flicker.
  const countsSettled = databaseCounts.every((q) => !q.isPending);
  const totalDatabases = databaseCounts.reduce((sum, q) => sum + (q.data?.length ?? 0), 0);

  // Distinct engine products across the fleet, versioned ("MariaDB 11.8").
  const engines = [...new Map(list.map((i) => [engineBadgeLabel(i).split(" ")[0], engineBadgeLabel(i)])).values()];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Database className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("db.title")}</h1>
          <p className="text-fg-muted text-sm">
            {t("db.instancesSummary", { online, total: list.length })}
          </p>
        </div>
      </div>

      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
          icon={Database}
          label={t("db.dashboard.instancesOnline")}
          value={
            <span className="flex items-baseline gap-1.5">
              {online}/{list.length}
              <StatusDot tone={online === list.length ? "success" : online > 0 ? "warning" : "danger"} />
            </span>
          }
        />
        <StatTile
          icon={Activity}
          label={t("db.dashboard.throughput")}
          value={t("db.dashboard.throughputValue", { ops: totalOps.toFixed(1) })}
        />
        <StatTile
          icon={Boxes}
          label={t("db.dashboard.databases")}
          value={countsSettled ? totalDatabases.toLocaleString() : "—"}
        />
        <StatTile
          icon={Layers}
          label={t("db.dashboard.engines")}
          value={
            <span className="flex flex-wrap gap-1">
              {engines.map((engine) => (
                <Badge key={engine} tone="neutral" className="text-[11px]">
                  {engine}
                </Badge>
              ))}
            </span>
          }
        />
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {list.map((instance) => (
          <InstanceCard
            key={instance.id}
            instance={instance}
            consoleVisible={instance.engine === "mysql" && can(dbResource(instance), "control")}
          />
        ))}
      </div>
    </div>
  );
}

// ---- single-instance dashboard ------------------------------------------------

function SingleDbDashboard({ instance, consoleVisible }: { instance: DbInstance; consoleVisible: boolean }) {
  const { t } = useTranslation();
  const isMongo = instance.engine === "mongodb";
  const EngineIcon = engineIcon[instance.engine];
  const offline = instance.status !== "connected";
  const stats = instance.stats;

  const resource = dbResource(instance);
  const ops = useMetricSeries(`metrics:${resource}`, resource, isMongo ? "ops_per_sec" : "qps");
  const conns = useMetricSeries(
    `metrics:${resource}`,
    resource,
    isMongo ? "connections_current" : "threads_connected",
  );

  const databases = useQuery({
    queryKey: ["db", instance.id, "databases"],
    queryFn: async () =>
      unwrap(await api.GET("/db/{id}/databases", { params: { path: { id: instance.id } } })),
    staleTime: 30_000,
    enabled: !offline,
  });
  const sessions = useQuery({
    queryKey: ["db", instance.id, "sessions"],
    queryFn: async () =>
      unwrap(await api.GET("/db/{id}/sessions", { params: { path: { id: instance.id } } })),
    refetchInterval: 10_000,
    enabled: !offline,
  });

  const dbs = [...(databases.data ?? [])].sort((a, b) => b.sizeBytes - a.sizeBytes);
  const totalSize = dbs.reduce((sum, d) => sum + d.sizeBytes, 0);
  const maxSize = dbs.reduce((m, d) => Math.max(m, d.sizeBytes), 0);
  const activeSessions = [...(sessions.data ?? [])]
    .filter((s) => (s.command ?? "").toLowerCase() !== "sleep")
    .sort((a, b) => b.timeSec - a.timeSec);

  return (
    <div className="space-y-5">
      {/* Identity + quick links into the module */}
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <EngineIcon className="size-5" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <StatusDot tone={statusTone[instance.status]} pulse={offline} />
            <Badge tone="neutral">{engineBadgeLabel(instance)}</Badge>
          </h1>
          <p className="text-fg-muted text-sm">
            {instance.host && <span className="font-mono">{instance.host}</span>}
            {instance.connectionUser && (
              <span className="text-fg-subtle"> · {t("db.dashboard.asUser", { user: instance.connectionUser })}</span>
            )}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <QuickLink to="/db/$instanceId" params={{ instanceId: instance.id }} icon={Boxes} label={t("db.tabs.databases")} />
          {consoleVisible && (
            <QuickLink to="/db/$instanceId/console" params={{ instanceId: instance.id }} icon={SquareTerminal} label={t("db.tabs.console")} />
          )}
          <QuickLink to="/db/$instanceId/sessions" params={{ instanceId: instance.id }} icon={Users} label={t("db.tabs.sessions")} />
          <QuickLink to="/db/$instanceId/performance" params={{ instanceId: instance.id }} icon={Activity} label={t("db.tabs.performance")} />
        </div>
      </div>

      {offline && (
        <div className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]" role="alert">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
          <span>
            {t(`integrations.status.${instance.status}`)}
            {instance.statusMessage && ` — ${instance.statusMessage}`}
          </span>
        </div>
      )}

      {/* Stat tiles */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <SmallTile label={t("db.dashboard.throughput")} value={stats?.opsPerSec != null ? t("db.dashboard.throughputValue", { ops: stats.opsPerSec.toFixed(1) }) : "—"} />
        <SmallTile
          label={t("db.dashboard.connections")}
          value={
            stats?.connectionsCurrent != null
              ? `${stats.connectionsCurrent}${stats.connectionsMax ? ` / ${stats.connectionsMax}` : ""}`
              : "—"
          }
        />
        <SmallTile label={t("db.dashboard.uptime")} value={stats?.uptimeSeconds != null ? formatDuration(stats.uptimeSeconds) : "—"} />
        <SmallTile label={t("db.dashboard.databases")} value={databases.data ? String(dbs.length) : "—"} />
        <SmallTile label={t("db.dashboard.totalSize")} value={databases.data ? formatBytes(totalSize) : "—"} />
        {isMongo ? (
          <SmallTile label={t("db.dashboard.memResident")} value={stats?.memResidentMb != null ? formatBytes(stats.memResidentMb * 1024 * 1024) : "—"} />
        ) : (
          <SmallTile label={t("db.dashboard.slowQueries")} value={stats?.slowQueriesTotal != null ? stats.slowQueriesTotal.toLocaleString() : "—"} />
        )}
      </div>

      {/* Live charts */}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-[11px] font-semibold tracking-wide uppercase">{t("db.dashboard.opsSpark")}</p>
          <TimeSeriesChart points={ops} kind="count" height={130} />
        </Card>
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-[11px] font-semibold tracking-wide uppercase">{t("db.dashboard.connections")}</p>
          <TimeSeriesChart points={conns} kind="count" height={130} />
        </Card>
      </div>

      {/* Databases by size + active sessions */}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Card>
          <div className="border-edge flex items-center gap-2 border-b px-4 py-2.5">
            <Boxes className="text-fg-subtle size-4" />
            <span className="text-fg text-[13px] font-semibold">{t("db.dashboard.largestDatabases")}</span>
            <span className="text-fg-subtle text-[12px]">{dbs.length}</span>
          </div>
          <div className="p-2">
            {dbs.length === 0 ? (
              <p className="text-fg-subtle px-2 py-6 text-center text-[12.5px]">{t("db.dashboard.noData")}</p>
            ) : (
              dbs.slice(0, 8).map((d) => (
                <Link
                  key={d.name}
                  to="/db/$instanceId"
                  params={{ instanceId: instance.id }}
                  className="hover:bg-surface-hover relative block overflow-hidden rounded-md px-2.5 py-1.5 transition-colors"
                >
                  <div
                    className="bg-accent/45 absolute inset-y-0 left-0 rounded-md opacity-25"
                    style={{ width: maxSize > 0 ? `${Math.max(3, (d.sizeBytes / maxSize) * 100)}%` : 0 }}
                  />
                  <div className="relative flex items-center justify-between gap-2">
                    <span className="text-fg truncate font-mono text-[12px]">{d.name}</span>
                    <span className="text-fg-muted shrink-0 text-[12px] tabular-nums">
                      {formatBytes(d.sizeBytes)}
                      <span className="text-fg-subtle"> · {t("db.dashboard.objects", { count: d.objectCount })}</span>
                    </span>
                  </div>
                </Link>
              ))
            )}
          </div>
        </Card>

        <Card>
          <div className="border-edge flex items-center gap-2 border-b px-4 py-2.5">
            <Users className="text-fg-subtle size-4" />
            <span className="text-fg text-[13px] font-semibold">{t("db.dashboard.activeSessions")}</span>
            <span className="text-fg-subtle text-[12px]">{activeSessions.length}</span>
            <Link
              to="/db/$instanceId/sessions"
              params={{ instanceId: instance.id }}
              className="text-accent ml-auto text-[12px] hover:underline"
            >
              {t("db.dashboard.allSessions")} →
            </Link>
          </div>
          <div className="p-2">
            {activeSessions.length === 0 ? (
              <p className="text-fg-subtle px-2 py-6 text-center text-[12.5px]">{t("db.dashboard.noActiveSessions")}</p>
            ) : (
              activeSessions.slice(0, 8).map((s) => (
                <div key={s.id} className="flex items-center justify-between gap-2 rounded-md px-2.5 py-1.5">
                  <span className="min-w-0 flex-1">
                    <span className="text-fg block truncate font-mono text-[12px]" title={s.query || s.command}>
                      {s.query || s.command}
                    </span>
                    <span className="text-fg-subtle block truncate text-[11px]">
                      {[s.user, s.database, s.client].filter(Boolean).join(" · ")}
                    </span>
                  </span>
                  <span className="text-fg-muted shrink-0 text-[12px] tabular-nums">
                    {s.timeSec < 60 ? `${s.timeSec}s` : formatDuration(s.timeSec)}
                  </span>
                </div>
              ))
            )}
          </div>
        </Card>
      </div>
    </div>
  );
}

function SmallTile({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-4">
      <p className="text-fg-subtle truncate text-[11px] font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1.5 truncate text-xl font-semibold tracking-tight tabular-nums">{value}</p>
    </Card>
  );
}

function QuickLink({
  to,
  params,
  icon: Icon,
  label,
}: {
  to: string;
  params: Record<string, string>;
  icon: LucideIcon;
  label: string;
}) {
  return (
    <Link
      // The four targets are static module tabs; the generic prop defeats the
      // router's literal-type inference, so cast the pair together.
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      {...({ to, params } as any)}
      className="border-edge bg-surface hover:bg-surface-hover text-fg flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-[12.5px] font-medium shadow-1 transition-colors"
    >
      <Icon className="size-3.5" aria-hidden />
      {label}
    </Link>
  );
}

function LandingSkeleton() {
  return (
    <div className="space-y-5">
      <div className="flex items-center gap-3">
        <Skeleton className="size-10 rounded-xl" />
        <div className="space-y-2">
          <Skeleton className="h-5 w-36" />
          <Skeleton className="h-3.5 w-48" />
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Card key={i} className="p-4">
            <Skeleton className="h-3.5 w-24" />
            <Skeleton className="mt-2.5 h-6 w-16" />
          </Card>
        ))}
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {Array.from({ length: 2 }, (_, i) => (
          <Card key={i} className="p-4">
            <Skeleton className="h-5 w-44" />
            <Skeleton className="mt-4 h-12 w-full" />
            <Skeleton className="mt-3 h-10 w-full" />
          </Card>
        ))}
      </div>
    </div>
  );
}

function StatTile({ icon: Icon, label, value }: { icon: LucideIcon; label: string; value: ReactNode }) {
  return (
    <Card className="flex items-center gap-3.5 p-4">
      <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
        <Icon className="size-4" aria-hidden />
      </span>
      <div className="min-w-0 flex-1">
        <p className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">{label}</p>
        <div className="text-fg mt-0.5 truncate text-lg font-semibold tracking-tight">{value}</div>
      </div>
    </Card>
  );
}

/**
 * One instance's dashboard card: identity row with the Open link, an ops/s
 * sparkline over the last hour, the key stat pairs, and quick actions.
 * Unreachable instances stay on the board — dimmed, with the reported error.
 */
function InstanceCard({
  instance,
  consoleVisible,
}: {
  instance: DbInstance;
  consoleVisible: boolean;
}) {
  const { t } = useTranslation();
  const isMongo = instance.engine === "mongodb";
  const EngineIcon = engineIcon[instance.engine];
  const offline = instance.status !== "connected";
  const stats = instance.stats;

  // Same series the Performance tab charts, shrunk to a sparkline.
  const resource = dbResource(instance);
  const ops = useMetricSeries(`metrics:${resource}`, resource, isMongo ? "ops_per_sec" : "qps");

  return (
    <Card className={cn("flex flex-col", offline && "opacity-75")}>
      <div className="flex items-start gap-3 p-4 pb-0">
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
          <EngineIcon className="size-4.5" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-fg flex items-center gap-2 text-sm font-semibold">
            <Link
              to="/db/$instanceId"
              params={{ instanceId: instance.id }}
              className="hover:text-accent truncate transition-colors"
            >
              {instance.name}
            </Link>
            <StatusDot tone={statusTone[instance.status]} pulse={offline} />
          </p>
          <p className="text-fg-muted truncate text-xs">
            {engineBadgeLabel(instance)}
            {instance.host && <span className="text-fg-subtle font-mono"> · {instance.host}</span>}
          </p>
        </div>
        <Link
          to="/db/$instanceId"
          params={{ instanceId: instance.id }}
          className="text-fg-muted hover:text-fg hover:bg-surface-hover flex shrink-0 items-center gap-1 rounded-md px-2 py-1 text-[13px] font-medium transition-colors"
        >
          {t("db.dashboard.open")}
          <ArrowUpRight className="size-3.5" aria-hidden />
        </Link>
      </div>

      {offline ? (
        <div className="p-4">
          <div
            className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-3.5 py-2.5 text-[13px]"
            role="alert"
          >
            <AlertTriangle className="mt-0.5 size-4 shrink-0" aria-hidden />
            <span>
              {t(`integrations.status.${instance.status}`)}
              {instance.statusMessage && ` — ${instance.statusMessage}`}
            </span>
          </div>
        </div>
      ) : (
        <>
          <div className="px-4 pt-3.5">
            <div className="flex items-baseline justify-between gap-2">
              <p className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">
                {t("db.dashboard.opsSpark")}
              </p>
              {typeof stats?.opsPerSec === "number" && (
                <p className="text-fg text-sm font-semibold tabular-nums">
                  {t("db.chips.ops", { ops: stats.opsPerSec.toFixed(1) })}
                </p>
              )}
            </div>
            {ops.length > 1 ? (
              <Sparkline points={ops} height={48} className="mt-1.5" />
            ) : (
              <p className="text-fg-subtle flex h-12 items-center text-xs">
                {t("db.dashboard.noSamples")}
              </p>
            )}
          </div>

          <div className="grid grid-cols-3 gap-2 px-4 pt-3">
            <MiniStat
              label={t("db.dashboard.connections")}
              value={
                typeof stats?.connectionsCurrent === "number" &&
                typeof stats?.connectionsMax === "number"
                  ? `${stats.connectionsCurrent.toLocaleString()}/${stats.connectionsMax.toLocaleString()}`
                  : "—"
              }
            />
            <MiniStat
              label={t("db.dashboard.uptime")}
              value={
                typeof stats?.uptimeSeconds === "number" ? formatDuration(stats.uptimeSeconds) : "—"
              }
            />
            {isMongo ? (
              <MiniStat
                label={t("db.dashboard.memResident")}
                value={
                  typeof stats?.memResidentMb === "number"
                    ? t("db.dashboard.memResidentValue", { mb: stats.memResidentMb.toLocaleString() })
                    : "—"
                }
              />
            ) : (
              <MiniStat
                label={t("db.dashboard.slowQueries")}
                value={
                  typeof stats?.slowQueriesTotal === "number"
                    ? stats.slowQueriesTotal.toLocaleString()
                    : "—"
                }
              />
            )}
          </div>

          <div className="border-edge mt-4 flex items-center gap-1 border-t px-2 py-1.5">
            {consoleVisible && (
              <Link
                to="/db/$instanceId/console"
                params={{ instanceId: instance.id }}
                className="text-fg-muted hover:text-fg hover:bg-surface-hover flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[13px] font-medium transition-colors"
              >
                <SquareTerminal className="size-3.5" aria-hidden />
                {t("db.tabs.console")}
              </Link>
            )}
            <Link
              to="/db/$instanceId/sessions"
              params={{ instanceId: instance.id }}
              className="text-fg-muted hover:text-fg hover:bg-surface-hover flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-[13px] font-medium transition-colors"
            >
              <Users className="size-3.5" aria-hidden />
              {t("db.tabs.sessions")}
            </Link>
          </div>
        </>
      )}
    </Card>
  );
}

function MiniStat({ label, value }: { label: string; value: string }) {
  return (
    <div className="bg-bg-subtle min-w-0 rounded-lg px-2.5 py-2">
      <p className="text-fg-subtle truncate text-[10.5px] font-semibold tracking-wide uppercase">
        {label}
      </p>
      <p className="text-fg mt-0.5 truncate text-[13px] font-semibold tabular-nums">{value}</p>
    </div>
  );
}
