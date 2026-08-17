import { useEffect, useMemo, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { cn, formatBytes } from "@/lib/utils";

export type SeriesPoint = { ts: number; value: number };

export type ChartKind = "percent" | "bytes";

/** Resolve theme tokens for chart ink; re-read when the theme flips. */
function useChartTheme() {
  const [theme, setTheme] = useState(() => document.documentElement.dataset.theme ?? "light");
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setTheme(document.documentElement.dataset.theme ?? "light");
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, []);
  return useMemo(() => {
    const style = getComputedStyle(document.documentElement);
    const v = (name: string) => style.getPropertyValue(name).trim();
    return {
      key: theme,
      accent: v("--hh-accent"),
      grid: v("--hh-edge"),
      axisInk: v("--hh-fg-subtle"),
      capacity: v("--hh-fg-subtle"),
    };
  }, [theme]);
}

function formatValue(kind: ChartKind, value: number): string {
  if (kind === "percent") return `${(value * 100).toFixed(value < 0.1 ? 1 : 0)}%`;
  return formatBytes(value);
}

/**
 * TimeSeriesChart renders one metric series (optionally with a dashed
 * capacity reference) as a thin accent line with a soft area fill, recessive
 * axes, and a crosshair tooltip.
 */
export function TimeSeriesChart({
  points,
  capacity,
  kind,
  height = 160,
  className,
}: {
  points: SeriesPoint[];
  /** Optional capacity/limit series (e.g. total memory), drawn as a dashed neutral line. */
  capacity?: SeriesPoint[];
  kind: ChartKind;
  height?: number;
  className?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);
  const theme = useChartTheme();

  const data = useMemo((): uPlot.AlignedData => {
    const xs = points.map((p) => p.ts / 1000);
    const ys = points.map((p) => p.value);
    if (!capacity || capacity.length === 0) {
      return [xs, ys];
    }
    // Align capacity onto the same x positions (last known value carries).
    const cap: (number | null)[] = [];
    let ci = 0;
    for (const p of points) {
      while (ci < capacity.length - 1 && capacity[ci + 1].ts <= p.ts) ci++;
      cap.push(capacity[ci] ? capacity[ci].value : null);
    }
    return [xs, ys, cap];
  }, [points, capacity]);

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const series: uPlot.Series[] = [
      {},
      {
        stroke: theme.accent,
        width: 2,
        fill: (u) => {
          const gradient = u.ctx.createLinearGradient(0, u.bbox.top, 0, u.bbox.top + u.bbox.height);
          gradient.addColorStop(0, theme.accent + "33");
          gradient.addColorStop(1, theme.accent + "00");
          return gradient;
        },
        points: { show: false },
      },
    ];
    if (data.length > 2) {
      series.push({
        stroke: theme.capacity,
        width: 1,
        dash: [4, 4],
        points: { show: false },
      });
    }

    const options: uPlot.Options = {
      width: el.clientWidth,
      height,
      padding: [8, 8, 0, 0],
      legend: { show: false },
      cursor: {
        y: false,
        points: { size: 6, fill: theme.accent },
      },
      scales: {
        y: kind === "percent" ? { range: [0, 1] } : { range: (_u, _min, max) => [0, Math.max(max * 1.1, 1)] },
      },
      axes: [
        {
          stroke: theme.axisInk,
          grid: { show: false },
          ticks: { show: false },
          font: "11px Inter Variable, sans-serif",
          space: 80,
        },
        {
          stroke: theme.axisInk,
          grid: { stroke: theme.grid, width: 1 },
          ticks: { show: false },
          font: "11px Inter Variable, sans-serif",
          size: 52,
          values: (_u, ticks) => ticks.map((t) => formatValue(kind, t)),
        },
      ],
      series,
      hooks: {
        setCursor: [
          (u) => {
            const tooltip = tooltipRef.current;
            if (!tooltip) return;
            const { idx, left } = u.cursor;
            if (idx == null || left == null || left < 0) {
              tooltip.style.opacity = "0";
              return;
            }
            const ts = u.data[0][idx];
            const value = u.data[1][idx];
            if (ts == null || value == null) {
              tooltip.style.opacity = "0";
              return;
            }
            tooltip.textContent = `${new Date(ts * 1000).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })} · ${formatValue(kind, value)}`;
            tooltip.style.opacity = "1";
            const clamped = Math.min(Math.max(left, 40), u.width - 40);
            tooltip.style.left = `${clamped}px`;
          },
        ],
      },
    };

    const plot = new uPlot(options, data, el);
    plotRef.current = plot;

    const resize = new ResizeObserver(() => {
      plot.setSize({ width: el.clientWidth, height });
    });
    resize.observe(el);
    return () => {
      resize.disconnect();
      plot.destroy();
      plotRef.current = null;
    };
    // Re-init on theme change; data updates go through setData below.
  }, [theme.key, kind, height, data.length > 2]);

  useEffect(() => {
    plotRef.current?.setData(data);
  }, [data]);

  return (
    <div className={cn("relative", className)}>
      <div ref={containerRef} />
      <div
        ref={tooltipRef}
        className="bg-fg text-bg pointer-events-none absolute -top-1 -translate-x-1/2 rounded-md px-2 py-0.5 text-[11px] font-medium whitespace-nowrap opacity-0 transition-opacity duration-100"
      />
    </div>
  );
}
