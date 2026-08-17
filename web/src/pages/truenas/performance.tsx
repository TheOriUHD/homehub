import { useCallback, useEffect, useMemo, useState } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChartSpline } from "lucide-react";
import { useMetricSeries } from "@/lib/metrics";
import type { TruenasDisk } from "@/lib/api";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { TimeSeriesChart, type SeriesPoint } from "@/components/time-series-chart";
import { MultiSeriesChart, type ChartSeries } from "@/components/multi-series-chart";
import {
  truenasDiskResource,
  truenasPoolResource,
  truenasResource,
  useTruenasOverview,
} from "./shared";

/**
 * TruenasPerformancePage charts each pool's fill level (used_pct) and every
 * disk's temperature from the metrics engine — an hour of history with the
 * live tail streaming in over WS, mirroring the docker detail layout. Charts
 * without samples show an inline "collecting" state rather than blank axes.
 */
export function TruenasPerformancePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const overview = useTruenasOverview(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const { pools, disks } = overview.data!;

  if (pools.length === 0 && disks.length === 0) {
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
      {hasData ? <MultiSeriesChart series={ordered} height={220} /> : <ChartCollecting />}
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
