import { useMemo } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChartSpline } from "lucide-react";
import { useMetricSeries } from "@/lib/metrics";
import { Card } from "@/ui/card";
import { EmptyState } from "@/ui/states";
import { TimeSeriesChart, type ChartKind, type SeriesPoint } from "@/components/time-series-chart";
import { dbResource, useDbInstances } from "./shared";

/**
 * Performance charts from the metrics engine — an hour of history with the
 * live tail streaming in over WS, mirroring the docker container detail
 * layout. SQL engines chart qps / threads / slow queries; MongoDB charts
 * ops / connections / resident memory. Charts without samples stay hidden.
 */
export function DbPerformancePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/db/$instanceId/performance" });

  const instances = useDbInstances();
  const instance = instances.data?.find((row) => row.id === instanceId);

  // The module gate only mounts tabs once the instance list is cached, so the
  // resource is stable for the lifetime of this mount (hooks stay unconditional).
  const isMongo = instance?.engine === "mongodb";
  const resource = instance ? dbResource(instance) : "";
  const topic = `metrics:${resource}`;

  const throughput = useMetricSeries(topic, resource, isMongo ? "ops_per_sec" : "qps");
  const connections = useMetricSeries(topic, resource, isMongo ? "connections_current" : "threads_connected");
  const third = useMetricSeries(topic, resource, isMongo ? "mem_resident_mb" : "slow_queries_total");

  // Resident memory is recorded in MB; bytes render with proper units.
  const thirdPoints = useMemo(
    (): SeriesPoint[] =>
      isMongo ? third.map((p) => ({ ts: p.ts, value: p.value * 1024 * 1024 })) : third,
    [third, isMongo],
  );

  if (!instance) return null;

  const allCharts: { key: string; label: string; points: SeriesPoint[]; kind: ChartKind }[] = [
    {
      key: "throughput",
      label: isMongo ? t("db.performance.opsPerSec") : t("db.performance.qps"),
      points: throughput,
      kind: "decimal",
    },
    {
      key: "connections",
      label: isMongo ? t("db.performance.connections") : t("db.performance.threadsConnected"),
      points: connections,
      kind: "count",
    },
    {
      key: "third",
      label: isMongo ? t("db.performance.memResident") : t("db.performance.slowQueries"),
      points: thirdPoints,
      kind: isMongo ? "bytes" : "count",
    },
  ];
  const charts = allCharts.filter((chart) => chart.points.length > 0);

  if (charts.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={ChartSpline}
          title={t("db.performance.empty.title")}
          body={t("db.performance.empty.body")}
        />
      </Card>
    );
  }

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      {charts.map((chart) => (
        <Card key={chart.key} className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{chart.label}</p>
          <TimeSeriesChart points={chart.points} kind={chart.kind} height={150} />
        </Card>
      ))}
    </div>
  );
}
