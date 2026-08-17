import { useCallback, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "./api";
import { formatBytes } from "./utils";
import { useLiveSamples, type LiveSample } from "./live";
import type { SeriesPoint } from "@/components/time-series-chart";

/**
 * formatMetricValue renders a sample for humans based on the unit reported by
 * /metrics/series — "fraction" (0..1 shown as %), "bytes", "per_min", and
 * anything else as a plain number.
 */
export function formatMetricValue(unit: string, value: number): string {
  if (unit === "fraction") return `${(value * 100).toFixed(value < 0.1 && value > 0 ? 1 : 0)}%`;
  if (unit === "bytes") return formatBytes(value);
  if (unit === "per_min") return `${Math.round(value).toLocaleString()}/min`;
  if (Number.isInteger(value)) return value.toLocaleString();
  return value.toFixed(Math.abs(value) < 10 ? 2 : 1);
}

/**
 * useMetricSeries loads an hour of history for one resource/metric and appends
 * live samples from the given WS topic (e.g. `metrics:proxmox/<instanceId>`).
 */
export function useMetricSeries(topic: string, resource: string, metric: string) {
  const query = useQuery({
    queryKey: ["metrics", resource, metric],
    queryFn: async () => {
      const to = new Date();
      const from = new Date(to.getTime() - 60 * 60 * 1000);
      return unwrap(
        await api.GET("/metrics/query", {
          params: { query: { resource, metric, from: from.toISOString(), to: to.toISOString() } },
        }),
      );
    },
    staleTime: 60_000,
  });

  const [liveTail, setLiveTail] = useState<SeriesPoint[]>([]);
  const onSamples = useCallback(
    (samples: LiveSample[]) => {
      const relevant = samples.filter((s) => s.resource === resource && s.metric === metric);
      if (relevant.length) {
        setLiveTail((prev) =>
          [...prev, ...relevant.map((s) => ({ ts: s.ts, value: s.value }))].slice(-240),
        );
      }
    },
    [resource, metric],
  );
  useLiveSamples(topic, onSamples);

  return useMemo((): SeriesPoint[] => {
    const history = (query.data?.points ?? []).map((p) => ({
      ts: new Date(p.ts).getTime(),
      value: p.avg,
    }));
    const lastHistory = history.length ? history[history.length - 1].ts : 0;
    return [...history, ...liveTail.filter((p) => p.ts > lastHistory)];
  }, [query.data, liveTail]);
}
