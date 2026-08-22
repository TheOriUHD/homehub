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
/** Units that read as a suffix on the number. Anything not listed prints bare,
 *  which is right for counts and for units we have not met yet. */
const unitSuffix: Record<string, string> = {
  "%": "%",
  percent: "%",
  "°C": " °C",
  celsius: " °C",
  W: " W",
  V: " V",
  A: " A",
  mV: " mV",
  mm: " mm",
  MB: " MB",
  h: " h",
  hours: " h",
};

export function formatMetricValue(unit: string, value: number): string {
  if (unit === "fraction") return `${(value * 100).toFixed(value < 0.1 && value > 0 ? 1 : 0)}%`;
  if (unit === "bytes") return formatBytes(value);
  if (unit === "per_min") return `${Math.round(value).toLocaleString()}/min`;
  if (unit === "bool") return value >= 0.5 ? "on" : "off";
  if (unit === "bps") {
    const units = ["bps", "kbps", "Mbps", "Gbps"];
    let v = value;
    let i = 0;
    while (v >= 1000 && i < units.length - 1) {
      v /= 1000;
      i++;
    }
    return `${i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
  }
  const n = Number.isInteger(value)
    ? value.toLocaleString()
    : value.toFixed(Math.abs(value) < 10 ? 2 : 1);
  return n + (unitSuffix[unit] ?? "");
}

/**
 * chartYRange picks the y-axis band for a metric.
 *
 * The old rule — always [0, max(max × 1.1, 1)] — assumed every metric is a
 * magnitude that starts at zero. Most are not: a voltage that lives between
 * 228 V and 240 V drawn from zero is a flat line at the top, a pool at 77–78%
 * full never appears to move, a constant 0 gets an axis labelled 0 and 1, and
 * anything negative is clipped away entirely.
 *
 * So: pin a fraction to its full 0..1 scale, because a proportion is meant to
 * be read against the whole; otherwise follow the data with 10% padding, and
 * anchor to zero only when the data already lives near it.
 */
export function chartYRange(unit: string, min: number | null, max: number | null): [number, number] {
  if (min == null || max == null || !Number.isFinite(min) || !Number.isFinite(max)) {
    return [0, 1];
  }
  if (unit === "fraction") return [0, 1];

  if (min === max) {
    // A constant series has no span to pad. Sit it inside a band rather than
    // flat against an axis edge, where it reads as missing data.
    if (min === 0) return [0, 1];
    const pad = Math.abs(min) * 0.1;
    return [min > 0 && min - pad < 0 ? 0 : min - pad, min + pad];
  }

  const span = max - min;
  const pad = span * 0.1;
  let lo = min - pad;
  // Never give non-negative data a negative floor: it invents readings that
  // cannot exist (a disk cannot be -3% full).
  if (min >= 0 && lo < 0) lo = 0;
  // Anchor to zero when the data already sits near it — 0–5 W read from zero
  // is honest, while 228–240 V read from zero hides the entire variation.
  if (min >= 0 && min <= span) lo = 0;
  return [lo, max + pad];
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
