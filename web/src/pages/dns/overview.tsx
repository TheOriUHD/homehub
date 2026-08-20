import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Ban, Clock, Globe, ListFilter, MonitorSmartphone, Server, ShieldCheck } from "lucide-react";
import { api, unwrap, isApiError, type DnsBlocking, type DnsInstance, type DnsTopItem } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useMetricSeries } from "@/lib/metrics";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { TimeSeriesChart } from "@/components/time-series-chart";
import { MultiSeriesChart } from "@/components/multi-series-chart";
import { statusTone } from "@/lib/systems";
import { dnsResource, dnsTypeLabel, useDnsInstances } from "./module";

function formatPct(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

/**
 * The DNS dashboard adapts to the fleet: a single instance gets the whole
 * page — deep stats, 24h history, leaderboards — while several instances get
 * the aggregate tiles and one card each.
 */
export function DnsOverviewPage() {
  const { t } = useTranslation();
  const instances = useDnsInstances();

  if (instances.isLoading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Card key={i} className="p-5">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="mt-3 h-7 w-32" />
          </Card>
        ))}
      </div>
    );
  }
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }

  const list = instances.data!;
  if (list.length === 1) {
    return <SingleInstanceDashboard instance={list[0]} />;
  }

  const totalQueries = list.reduce((sum, i) => sum + (i.stats?.queriesToday ?? 0), 0);
  const totalBlocked = list.reduce((sum, i) => sum + (i.stats?.blockedToday ?? 0), 0);
  const online = list.filter((i) => i.status === "connected").length;

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label={t("dns.overview.queriesToday")} value={totalQueries.toLocaleString()} />
        <StatTile label={t("dns.overview.blockedToday")} value={totalBlocked.toLocaleString()} />
        <StatTile
          label={t("dns.overview.blockedPct")}
          value={totalQueries > 0 ? formatPct(totalBlocked / totalQueries) : "—"}
        />
        <StatTile label={t("dns.overview.instancesOnline")} value={`${online}/${list.length}`} />
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {list.map((instance) => (
          <InstanceCard key={instance.id} instance={instance} />
        ))}
      </div>
    </div>
  );
}

// ---- single-instance dashboard ------------------------------------------------

function SingleInstanceDashboard({ instance }: { instance: DnsInstance }) {
  const { t } = useTranslation();
  const resource = dnsResource(instance);
  const topic = `metrics:${resource}`;
  const queriesPerMin = useMetricSeries(topic, resource, "queries_per_min");
  const blockedPct = useMetricSeries(topic, resource, "blocked_pct");
  const stats = instance.stats;

  const overview = useQuery({
    queryKey: ["dns", instance.id, "overview"],
    queryFn: async () =>
      unwrap(await api.GET("/dns/{id}/overview", { params: { path: { id: instance.id } } })),
    refetchInterval: 60_000,
    enabled: instance.status === "connected",
  });
  const ov = overview.data;

  const history = ov?.series ?? [];
  const historySeries = [
    {
      key: "queries",
      label: t("dns.overview.chartQueries"),
      unit: "",
      color: "--hh-series-1",
      points: history.map((p) => ({ ts: new Date(p.ts).getTime(), value: p.queries })),
    },
    {
      key: "blocked",
      label: t("dns.overview.blockedToday"),
      unit: "",
      color: "--hh-series-2",
      points: history.map((p) => ({ ts: new Date(p.ts).getTime(), value: p.blocked })),
    },
  ];

  return (
    <div className="space-y-5">
      {/* Identity + blocking controls */}
      <div className="flex flex-wrap items-center gap-3">
        <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
        <h2 className="text-fg text-lg font-semibold tracking-tight">{instance.name}</h2>
        <Badge tone="neutral">{dnsTypeLabel[instance.type]}</Badge>
        {stats?.version && <span className="text-fg-subtle text-[12.5px]">{stats.version}</span>}
        <div className="ml-auto">{stats && <BlockingControls instance={instance} compact />}</div>
      </div>

      {instance.status !== "connected" && instance.statusMessage && (
        <div className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]" role="alert">
          <AlertTriangle className="mt-0.5 size-4 shrink-0" />
          {instance.statusMessage}
        </div>
      )}

      {/* Stat tiles */}
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 xl:grid-cols-6">
        <IconTile icon={Globe} label={t("dns.overview.queriesToday")} value={(stats?.queriesToday ?? 0).toLocaleString()} />
        <IconTile
          icon={Ban}
          label={t("dns.overview.blockedToday")}
          value={(stats?.blockedToday ?? 0).toLocaleString()}
          sub={stats && stats.queriesToday > 0 ? formatPct(stats.blockedToday / stats.queriesToday) : undefined}
          tone="danger"
        />
        <IconTile icon={MonitorSmartphone} label={t("dns.overview.clients")} value={(stats?.clientsSeen ?? 0).toLocaleString()} />
        <IconTile icon={ListFilter} label={t("dns.overview.rules")} value={(stats?.domainsOnLists ?? 0).toLocaleString()} />
        <IconTile
          icon={Clock}
          label={t("dns.overview.avgResponse")}
          value={ov?.avgProcessingMs != null ? `${ov.avgProcessingMs.toFixed(1)} ms` : "—"}
        />
        <IconTile
          icon={ShieldCheck}
          label={t("dns.blocking.title")}
          value={stats?.enabled ? t("dns.blocking.active") : t("dns.blocking.disabled")}
          tone={stats?.enabled ? "success" : "warning"}
        />
      </div>

      {/* History (upstream's own buckets) + live rates side by side */}
      <div className="grid items-start gap-4 xl:grid-cols-2">
        <Card>
          <CardHeader title={t("dns.overview.historyTitle")} description={t("dns.overview.historyBody")} />
          <CardBody>
            {history.length > 1 ? (
              <MultiSeriesChart series={historySeries} height={220} />
            ) : (
              <p className="text-fg-subtle py-10 text-center text-[13px]">{t("dns.overview.noHistory")}</p>
            )}
          </CardBody>
        </Card>
        <Card>
          <CardHeader title={t("dns.overview.liveTitle")} description={t("dns.overview.liveBody")} />
          <CardBody className="space-y-4">
            <div>
              <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartQueries")}</p>
              <TimeSeriesChart points={queriesPerMin} kind="count" height={84} />
            </div>
            <div>
              <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartBlocked")}</p>
              <TimeSeriesChart points={blockedPct} kind="percent" height={84} />
            </div>
          </CardBody>
        </Card>
      </div>

      {/* Leaderboards */}
      {ov && (ov.topDomains.length > 0 || ov.topBlocked.length > 0 || ov.topClients.length > 0 || ov.topUpstreams.length > 0) && (
        <div className="grid items-start gap-4 sm:grid-cols-2 xl:grid-cols-4">
          <TopPanel icon={Globe} title={t("dns.overview.topDomains")} items={ov.topDomains} />
          <TopPanel icon={Ban} title={t("dns.overview.topBlocked")} items={ov.topBlocked} barClass="bg-danger/60" />
          <TopPanel icon={MonitorSmartphone} title={t("dns.overview.topClients")} items={ov.topClients} />
          <TopPanel icon={Server} title={t("dns.overview.upstreams")} items={ov.topUpstreams} />
        </div>
      )}
    </div>
  );
}

function IconTile({
  icon: Icon,
  label,
  value,
  sub,
  tone,
}: {
  icon: React.ComponentType<{ className?: string }>;
  label: string;
  value: string;
  sub?: string;
  tone?: "danger" | "success" | "warning";
}) {
  return (
    <Card className="p-4">
      <div className="flex items-center gap-2">
        <Icon
          className={
            tone === "danger"
              ? "text-danger size-4"
              : tone === "success"
                ? "text-success size-4"
                : tone === "warning"
                  ? "text-warning size-4"
                  : "text-fg-subtle size-4"
          }
        />
        <p className="text-fg-subtle truncate text-[11px] font-semibold tracking-wide uppercase">{label}</p>
      </div>
      <p className="text-fg mt-1.5 truncate text-xl font-semibold tracking-tight tabular-nums">
        {value}
        {sub && <span className="text-fg-subtle ml-1.5 text-[13px] font-normal">{sub}</span>}
      </p>
    </Card>
  );
}

/** TopPanel renders a leaderboard with proportional bars behind each row. */
function TopPanel({
  icon: Icon,
  title,
  items,
  barClass = "bg-accent/45",
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  items: DnsTopItem[];
  barClass?: string;
}) {
  const { t } = useTranslation();
  const max = items.reduce((m, i) => Math.max(m, i.count), 0);
  return (
    <Card>
      <div className="border-edge flex items-center gap-2 border-b px-4 py-2.5">
        <Icon className="text-fg-subtle size-4" />
        <span className="text-fg text-[13px] font-semibold">{title}</span>
      </div>
      <div className="p-2">
        {items.length === 0 ? (
          <p className="text-fg-subtle px-2 py-6 text-center text-[12.5px]">{t("dns.overview.noData")}</p>
        ) : (
          items.slice(0, 8).map((item) => (
            <div key={item.name} className="relative overflow-hidden rounded-md px-2.5 py-1.5">
              <div
                className={`absolute inset-y-0 left-0 rounded-md opacity-25 ${barClass}`}
                style={{ width: max > 0 ? `${Math.max(3, (item.count / max) * 100)}%` : 0 }}
              />
              <div className="relative flex items-center justify-between gap-2">
                <span className="text-fg truncate font-mono text-[12px]" title={item.name}>
                  {item.name}
                </span>
                <span className="text-fg-muted shrink-0 text-[12px] font-semibold tabular-nums">
                  {item.count.toLocaleString()}
                </span>
              </div>
            </div>
          ))
        )}
      </div>
    </Card>
  );
}

// ---- shared bits ----------------------------------------------------------------

function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-5">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1.5 truncate text-xl font-semibold tracking-tight">{value}</p>
    </Card>
  );
}

/** BlockingControls is the enable / timed-disable row, shared by both layouts. */
function BlockingControls({ instance, compact }: { instance: DnsInstance; compact?: boolean }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();
  const stats = instance.stats;
  const canControl = can(dnsResource(instance), "control");

  const setBlocking = useMutation({
    mutationFn: async (body: { enabled: boolean; seconds?: number }) =>
      unwrap(await api.POST("/dns/{id}/blocking", { params: { path: { id: instance.id } }, body })),
    // The response is authoritative for blocking state — patch it into the
    // cached instance list so the card flips without waiting for a refetch.
    onSuccess: (result: DnsBlocking) => {
      queryClient.setQueryData<DnsInstance[]>(["dns", "instances"], (prev) =>
        prev?.map((row) =>
          row.id === instance.id && row.stats
            ? { ...row, stats: { ...row.stats, enabled: result.enabled, disabledUntil: result.until } }
            : row,
        ),
      );
      toast(
        "success",
        result.enabled
          ? t("dns.blocking.enabledToast", { name: instance.name })
          : t("dns.blocking.disabledToast", { name: instance.name }),
      );
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (!stats) return null;
  return (
    <div className="flex flex-wrap items-center gap-2">
      {stats.enabled ? (
        <>
          <Badge tone="success">{t("dns.blocking.active")}</Badge>
          {canControl && (
            <span className="flex flex-wrap items-center gap-1.5">
              {!compact && <span className="text-fg-subtle text-[13px]">{t("dns.blocking.disableFor")}</span>}
              <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 300 })}>
                {t("dns.blocking.for5m")}
              </Button>
              <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 1800 })}>
                {t("dns.blocking.for30m")}
              </Button>
              <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 3600 })}>
                {t("dns.blocking.for1h")}
              </Button>
              <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false })}>
                {t("dns.blocking.untilEnabled")}
              </Button>
            </span>
          )}
        </>
      ) : (
        <>
          <Badge tone="warning">{t("dns.blocking.disabled")}</Badge>
          {stats.disabledUntil && <Countdown until={stats.disabledUntil} />}
          {canControl && (
            <Button size="sm" variant="primary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: true })}>
              {t("dns.blocking.enable")}
            </Button>
          )}
        </>
      )}
    </div>
  );
}

function InstanceCard({ instance }: { instance: DnsInstance }) {
  const { t } = useTranslation();
  const resource = dnsResource(instance);
  const topic = `metrics:${resource}`;
  const queriesPerMin = useMetricSeries(topic, resource, "queries_per_min");
  const blockedPct = useMetricSeries(topic, resource, "blocked_pct");
  const stats = instance.stats;

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
            {instance.name}
            <Badge tone="neutral">{dnsTypeLabel[instance.type]}</Badge>
          </span>
        }
        description={
          stats
            ? `${stats.version} · ${t("dns.overview.instanceSummary", {
                queries: stats.queriesToday.toLocaleString(),
                pct: stats.queriesToday > 0 ? formatPct(stats.blockedToday / stats.queriesToday) : "—",
              })}`
            : t(`integrations.status.${instance.status}`)
        }
      />
      <CardBody className="space-y-4">
        {instance.status !== "connected" && instance.statusMessage && (
          <div className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]" role="alert">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            {instance.statusMessage}
          </div>
        )}

        <BlockingControls instance={instance} />

        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartQueries")}</p>
            <TimeSeriesChart points={queriesPerMin} kind="count" height={120} />
          </div>
          <div>
            <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartBlocked")}</p>
            <TimeSeriesChart points={blockedPct} kind="percent" height={120} />
          </div>
        </div>
      </CardBody>
    </Card>
  );
}

/**
 * Countdown ticks client-side once a second until the disable window ends,
 * then refreshes the instance list once so the card flips back to enabled.
 */
function Countdown({ until }: { until: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  const firedRef = useRef(false);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const remaining = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));

  useEffect(() => {
    if (remaining === 0 && !firedRef.current) {
      firedRef.current = true;
      void queryClient.invalidateQueries({ queryKey: ["dns", "instances"] });
    }
  }, [remaining, queryClient]);

  const hours = Math.floor(remaining / 3600);
  const minutes = Math.floor((remaining % 3600) / 60);
  const seconds = remaining % 60;
  const time =
    hours > 0
      ? `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`
      : `${minutes}:${String(seconds).padStart(2, "0")}`;

  return (
    <span className="text-fg-muted text-[13px] tabular-nums">
      {t("dns.blocking.resumesIn", { time })}
    </span>
  );
}
