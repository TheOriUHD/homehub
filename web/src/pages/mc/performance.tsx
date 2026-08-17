import { useMemo } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ChartSpline } from "lucide-react";
import { useMetricSeries } from "@/lib/metrics";
import { Card } from "@/ui/card";
import { EmptyState } from "@/ui/states";
import { TimeSeriesChart, type ChartKind, type SeriesPoint } from "@/components/time-series-chart";
import { mcResource } from "./shared";

/**
 * TPS / MSPT / players / heap from the metrics engine — an hour of history
 * with the live tail streaming in over WS, mirroring the docker container
 * detail layout. Charts without any recorded samples stay hidden; the whole
 * tab collapses to an empty state until the companion has reported once.
 */
export function McPerformancePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId/performance" });

  const resource = mcResource(instanceId);
  const topic = `metrics:${resource}`;
  const tps = useMetricSeries(topic, resource, "tps");
  const mspt = useMetricSeries(topic, resource, "mspt");
  const players = useMetricSeries(topic, resource, "players_online");
  const heapMb = useMetricSeries(topic, resource, "heap_used_mb");

  // Recorded in MB; bytes render with proper units on the shared chart.
  const heap = useMemo(
    (): SeriesPoint[] => heapMb.map((p) => ({ ts: p.ts, value: p.value * 1024 * 1024 })),
    [heapMb],
  );

  const allCharts: { key: string; label: string; points: SeriesPoint[]; kind: ChartKind }[] = [
    { key: "tps", label: t("mc.performance.tps"), points: tps, kind: "decimal" },
    { key: "mspt", label: t("mc.performance.mspt"), points: mspt, kind: "decimal" },
    { key: "players", label: t("mc.performance.players"), points: players, kind: "count" },
    { key: "heap", label: t("mc.performance.heap"), points: heap, kind: "bytes" },
  ];
  const charts = allCharts.filter((chart) => chart.points.length > 0);

  if (charts.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={ChartSpline}
          title={t("mc.performance.empty.title")}
          body={t("mc.performance.empty.body")}
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
