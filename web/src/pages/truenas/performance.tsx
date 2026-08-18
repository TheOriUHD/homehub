import { useCallback, useEffect, useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowDownToLine, ArrowUpFromLine, ChartSpline, Network, ThermometerSun } from "lucide-react";
import { useMetricSeries } from "@/lib/metrics";
import type { TruenasDisk, TruenasInterfaceTraffic } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { TimeSeriesChart, type SeriesPoint } from "@/components/time-series-chart";
import { MultiSeriesChart, type ChartSeries } from "@/components/multi-series-chart";
import { TrafficChart } from "./traffic-chart";
import {
  formatBps,
  truenasDiskResource,
  truenasPoolResource,
  truenasResource,
  useTruenasOverview,
  useTruenasStats,
} from "./shared";

/**
 * TruenasPerformancePage is the live performance dashboard: per-adapter
 * traffic straight from the NAS's reporting API, CPU / load / memory
 * history, then the metrics-engine pool-capacity and disk-temperature
 * charts. Disk temps distinguish "no samples yet" from "this NAS doesn't
 * report temperatures" — the latter must never spin forever.
 */
export function TruenasPerformancePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const overview = useTruenasOverview(instanceId);
  const stats = useTruenasStats(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const { pools, disks } = overview.data!;
  const interfaces = stats.data?.interfaces ?? [];
  const hasStats =
    (stats.data?.cpu.length ?? 0) > 0 ||
    (stats.data?.load.length ?? 0) > 0 ||
    (stats.data?.memoryAvailable.length ?? 0) > 0;

  if (pools.length === 0 && disks.length === 0 && interfaces.length === 0 && !hasStats) {
    return (
      <Card>
        <EmptyState
          icon={ChartSpline}
          title={t("truenas.performance.empty.title")}
          body={t("truenas.performance.empty.body")}
        />
      </Card>
    );
  }

  return (
    <div className="space-y-4">
      {interfaces.map((ifc) => (
        <TrafficCard key={ifc.name} iface={ifc} />
      ))}

      {hasStats && <StatsGrid instanceId={instanceId} />}

      {pools.length > 0 && (
        <div className="grid gap-4 lg:grid-cols-2">
          {pools.map((pool) => (
            <PoolCapacityCard key={pool.name} instanceId={instanceId} pool={pool.name} />
          ))}
        </div>
      )}
      {disks.length > 0 && <DiskTemperatureCard instanceId={instanceId} disks={disks} />}
    </div>
  );
}

/* ---- adapter traffic -------------------------------------------------------- */

/**
 * TrafficCard mirrors TrueNAS's own dashboard widget: adapter identity and
 * live In/Out on the left, the mirrored auto-scaling area chart filling the
 * rest. The rates shown are the newest sample of the hour window.
 */
function TrafficCard({ iface }: { iface: TruenasInterfaceTraffic }) {
  const { t } = useTranslation();
  const last = iface.points.length > 0 ? iface.points[iface.points.length - 1] : null;

  return (
    <Card className="p-4">
      <div className="flex flex-col gap-4 sm:flex-row">
        <div className="w-full shrink-0 space-y-2.5 sm:w-52">
          <div className="flex items-center gap-2">
            <Network className="text-fg-subtle size-4" />
            <p className="text-fg text-lg font-semibold tracking-tight">{iface.name}</p>
            {iface.type !== "PHYSICAL" && (
              <Badge tone="neutral">{iface.type.toLowerCase()}</Badge>
            )}
          </div>

          {last && (
            <div className="space-y-1 text-[13px]">
              <p className="flex items-center gap-1.5">
                <ArrowDownToLine className="size-3.5" style={{ color: "var(--hh-series-1)" }} />
                <span className="text-fg-muted">{t("truenas.traffic.in")}</span>
                <span className="text-fg font-semibold tabular-nums">{formatBps(last.inBps)}</span>
              </p>
              <p className="flex items-center gap-1.5">
                <ArrowUpFromLine className="size-3.5" style={{ color: "var(--hh-series-3)" }} />
                <span className="text-fg-muted">{t("truenas.traffic.out")}</span>
                <span className="text-fg font-semibold tabular-nums">{formatBps(last.outBps)}</span>
              </p>
            </div>
          )}

          <div className="flex items-center gap-1.5 text-[11px] font-semibold uppercase tracking-wide">
            <StatusDot tone={iface.up ? "success" : "danger"} />
            <span className={iface.up ? "text-success" : "text-danger"}>
              {iface.up ? t("truenas.traffic.linkUp") : t("truenas.traffic.linkDown")}
            </span>
          </div>

          {(iface.media || (iface.addresses?.length ?? 0) > 0) && (
            <p className="text-fg-subtle text-[12px]">
              {[iface.media, ...(iface.addresses ?? [])].filter(Boolean).join(" · ")}
            </p>
          )}
        </div>

        <div className="min-w-0 flex-1">
          {iface.points.length > 1 ? (
            <TrafficChart
              points={iface.points}
              inLabel={t("truenas.traffic.in")}
              outLabel={t("truenas.traffic.out")}
            />
          ) : (
            <div className="text-fg-subtle flex h-[150px] items-center justify-center text-[13px]">
              {t("truenas.performance.collecting")}
            </div>
          )}
        </div>
      </div>
    </Card>
  );
}

/* ---- cpu / load / memory ---------------------------------------------------- */

function StatsGrid({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const stats = useTruenasStats(instanceId);
  const data = stats.data;
  if (!data) return null;

  const cpuPoints: SeriesPoint[] = data.cpu.map((p) => ({ ts: p.ts, value: p.value / 100 }));

  const loadSeries: ChartSeries[] = [
    { key: "load1", label: t("truenas.stats.load1"), unit: "", color: "--hh-series-1", points: data.load.map((p) => ({ ts: p.ts, value: p.load1 })) },
    { key: "load5", label: t("truenas.stats.load5"), unit: "", color: "--hh-series-2", points: data.load.map((p) => ({ ts: p.ts, value: p.load5 })) },
    { key: "load15", label: t("truenas.stats.load15"), unit: "", color: "--hh-series-3", points: data.load.map((p) => ({ ts: p.ts, value: p.load15 })) },
  ].filter((s) => s.points.length > 0);

  const memSeries: ChartSeries[] = [
    {
      key: "arc",
      label: t("truenas.stats.arc"),
      unit: "bytes",
      color: "--hh-series-2",
      points: data.arcSize.map((p) => ({ ts: p.ts, value: p.value })),
    },
    {
      key: "available",
      label: t("truenas.stats.memAvailable"),
      unit: "bytes",
      color: "--hh-series-1",
      points: data.memoryAvailable.map((p) => ({ ts: p.ts, value: p.value })),
    },
  ].filter((s) => s.points.length > 0);

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {cpuPoints.length > 0 && (
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
            {t("truenas.stats.cpu")}
          </p>
          <TimeSeriesChart points={cpuPoints} kind="percent" height={170} />
        </Card>
      )}
      {loadSeries.length > 0 && (
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
            {t("truenas.stats.load")}
          </p>
          <MultiSeriesChart series={loadSeries} height={170} />
        </Card>
      )}
      {memSeries.length > 0 && (
        <Card className="p-4 lg:col-span-2">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
            {t("truenas.stats.memory", {
              total: data.totalMemoryBytes > 0 ? formatBytes(data.totalMemoryBytes) : "",
            })}
          </p>
          <MultiSeriesChart series={memSeries} height={190} />
        </Card>
      )}
    </div>
  );
}

/* ---- pools + disk temps (metrics engine) ------------------------------------ */

function PoolCapacityCard({ instanceId, pool }: { instanceId: string; pool: string }) {
  const { t } = useTranslation();
  const topic = `metrics:${truenasResource(instanceId)}`;
  const resource = truenasPoolResource(instanceId, pool);
  const raw = useMetricSeries(topic, resource, "used_pct");
  // used_pct arrives as 0..100; the percent chart kind expects a 0..1 fraction.
  const points = useMemo(
    (): SeriesPoint[] => raw.map((p) => ({ ts: p.ts, value: p.value / 100 })),
    [raw],
  );

  return (
    <Card className="p-4">
      <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
        {t("truenas.performance.poolCapacity", { pool })}
      </p>
      {points.length === 0 ? (
        <ChartCollecting />
      ) : (
        <TimeSeriesChart points={points} kind="percent" height={150} />
      )}
    </Card>
  );
}

function DiskTemperatureCard({ instanceId, disks }: { instanceId: string; disks: TruenasDisk[] }) {
  const { t } = useTranslation();
  const [series, setSeries] = useState<Record<string, ChartSeries>>({});

  const handleSeries = useCallback((name: string, s: ChartSeries) => {
    setSeries((prev) => ({ ...prev, [name]: s }));
  }, []);

  // Order series by disk index so colours stay stable across renders.
  const ordered = disks.map((d) => series[d.name]).filter((s): s is ChartSeries => Boolean(s));
  const hasData = ordered.some((s) => s.points.length > 0);
  // The NAS itself reports no temperature for any disk (virtualised, or
  // SMART hidden by the controller) — say so instead of spinning forever.
  const unavailable = disks.every((d) => d.temperature == null);

  return (
    <Card className="p-4">
      <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
        {t("truenas.performance.diskTemps")}
      </p>
      {disks.map((disk, i) => (
        <DiskTempLoader
          key={disk.name}
          instanceId={instanceId}
          disk={disk.name}
          color={`--hh-series-${(i % 6) + 1}`}
          onSeries={handleSeries}
        />
      ))}
      {hasData ? (
        <MultiSeriesChart series={ordered} height={220} />
      ) : unavailable ? (
        <div className="text-fg-subtle flex h-[120px] flex-col items-center justify-center gap-1.5 text-[13px]">
          <ThermometerSun className="size-5" />
          {t("truenas.performance.tempsUnavailable")}
        </div>
      ) : (
        <ChartCollecting />
      )}
    </Card>
  );
}

/**
 * DiskTempLoader loads one disk's temperature series and lifts it to the
 * parent — one metrics hook per disk, so the hook count stays constant per
 * mounted loader (Rules of Hooks) while the combined chart plots them all.
 */
function DiskTempLoader({
  instanceId,
  disk,
  color,
  onSeries,
}: {
  instanceId: string;
  disk: string;
  color: string;
  onSeries: (name: string, series: ChartSeries) => void;
}) {
  const topic = `metrics:${truenasResource(instanceId)}`;
  const resource = truenasDiskResource(instanceId, disk);
  const points = useMetricSeries(topic, resource, "temperature");

  useEffect(() => {
    onSeries(disk, { key: resource, label: disk, unit: "°C", points, color });
  }, [disk, resource, color, points, onSeries]);

  return null;
}

function ChartCollecting() {
  const { t } = useTranslation();
  return (
    <div className="text-fg-subtle flex h-[150px] items-center justify-center text-[13px]">
      {t("truenas.performance.collecting")}
    </div>
  );
}
