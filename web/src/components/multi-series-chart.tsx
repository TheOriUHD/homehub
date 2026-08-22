import { useEffect, useMemo, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { cn } from "@/lib/utils";
import { chartYRange, formatMetricValue } from "@/lib/metrics";
import { Tooltip } from "@/ui/tooltip";

export type MultiSeriesPoint = { ts: number; value: number };

export type ChartSeries = {
  /** Stable identity for legend toggling, e.g. "resource|metric". */
  key: string;
  label: string;
  /** Unit from /metrics/series — "fraction" | "bytes" | "per_min" | plain. */
  unit: string;
  points: MultiSeriesPoint[];
  /** CSS custom property from the categorical palette, e.g. "--hh-series-1". */
  color: string;
};

export type EventMarker = { ts: number; label: string };

/** Resolve theme tokens for chart ink; re-read when the theme flips. */
function useChartTheme() {
  const [themeKey, setThemeKey] = useState(() => document.documentElement.dataset.theme ?? "light");
  useEffect(() => {
    const observer = new MutationObserver(() => {
      setThemeKey(document.documentElement.dataset.theme ?? "light");
    });
    observer.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });
    return () => observer.disconnect();
  }, []);
  return useMemo(() => {
    const style = getComputedStyle(document.documentElement);
    const v = (name: string) => style.getPropertyValue(name).trim();
    return { key: themeKey, grid: v("--hh-edge"), axisInk: v("--hh-fg-subtle"), resolve: v };
  }, [themeKey]);
}

/**
 * MultiSeriesChart plots several picked metric series as 2px lines with a
 * shared crosshair tooltip and audit event markers. Series never share a
 * y-axis across units — mixed units render as one stacked plot per unit
 * group. A click-to-toggle legend appears whenever two or more series are
 * shown.
 */
export function MultiSeriesChart({
  series,
  eventMarkers,
  height = 260,
  className,
  onScrub,
  onScrubPin,
}: {
  series: ChartSeries[];
  eventMarkers?: EventMarker[];
  height?: number;
  className?: string;
  /** Fires with the hovered sample time (ms), or null when the cursor leaves. */
  onScrub?: (ts: number | null) => void;
  /** Fires when a time point is clicked — History pins the scrub panel on it. */
  onScrubPin?: (ts: number) => void;
}) {
  const theme = useChartTheme();
  const [hidden, setHidden] = useState<ReadonlySet<string>>(new Set());

  // One stacked plot per unit group, in order of first appearance — mixed
  // units never share a y-axis, and never render as dual axes on one plot.
  const groups = useMemo(() => {
    const order: string[] = [];
    const byUnit = new Map<string, ChartSeries[]>();
    for (const s of series) {
      if (!byUnit.has(s.unit)) {
        byUnit.set(s.unit, []);
        order.push(s.unit);
      }
      byUnit.get(s.unit)!.push(s);
    }
    return order.map((unit) => ({ unit, series: byUnit.get(unit)! }));
  }, [series]);

  // Forget hidden flags for series that were removed, so re-adding one later
  // starts visible again.
  useEffect(() => {
    setHidden((prev) => {
      const keys = new Set(series.map((s) => s.key));
      const next = new Set([...prev].filter((k) => keys.has(k)));
      return next.size === prev.size ? prev : next;
    });
  }, [series]);

  const groupHeight =
    groups.length > 1 ? Math.max(120, Math.round(height / groups.length)) : height;

  return (
    <div className={cn("space-y-3", className)}>
      {series.length >= 2 && (
        <div className="flex flex-wrap gap-x-1 gap-y-1">
          {series.map((s) => {
            const off = hidden.has(s.key);
            return (
              <button
                key={s.key}
                type="button"
                aria-pressed={!off}
                onClick={() =>
                  setHidden((prev) => {
                    const next = new Set(prev);
                    if (!next.delete(s.key)) next.add(s.key);
                    return next;
                  })
                }
                className={cn(
                  "hover:bg-surface-hover flex cursor-pointer items-center gap-1.5 rounded-md px-1.5 py-0.5 text-xs font-medium transition-colors",
                  off ? "text-fg-subtle" : "text-fg-muted hover:text-fg",
                )}
              >
                <span
                  aria-hidden
                  className={cn("size-2.5 shrink-0 rounded-[3px]", off && "opacity-30")}
                  style={{ background: `var(${s.color})` }}
                />
                <span className={cn(off && "line-through")}>{s.label}</span>
              </button>
            );
          })}
        </div>
      )}
      {groups.map((group) => (
        <UnitPlot
          key={group.unit}
          unit={group.unit}
          series={group.series}
          hidden={hidden}
          eventMarkers={eventMarkers}
          height={groupHeight}
          showUnitLabel={groups.length > 1}
          theme={theme}
          onScrub={onScrub}
          onScrubPin={onScrubPin}
        />
      ))}
    </div>
  );
}

/** Compact caption for a unit group when several stacked plots are shown. */
const unitCaption: Record<string, string> = {
  fraction: "%",
  bytes: "bytes",
  per_min: "/min",
};

type MarkerPosition = { ts: number; label: string; left: number; top: number };

function markersEqual(a: MarkerPosition[], b: MarkerPosition[]): boolean {
  if (a.length !== b.length) return false;
  return a.every(
    (m, i) =>
      m.ts === b[i].ts && m.left === b[i].left && m.top === b[i].top && m.label === b[i].label,
  );
}

function UnitPlot({
  unit,
  series,
  hidden,
  eventMarkers,
  height,
  showUnitLabel,
  theme,
  onScrub,
  onScrubPin,
}: {
  unit: string;
  series: ChartSeries[];
  hidden: ReadonlySet<string>;
  eventMarkers?: EventMarker[];
  height: number;
  showUnitLabel: boolean;
  theme: ReturnType<typeof useChartTheme>;
  onScrub?: (ts: number | null) => void;
  onScrubPin?: (ts: number) => void;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const tooltipRef = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);
  const [markerPos, setMarkerPos] = useState<MarkerPosition[]>([]);

  // Callbacks and volatile props live in refs so mousemove-driven parent
  // re-renders never tear the plot down.
  const hiddenRef = useRef(hidden);
  hiddenRef.current = hidden;
  const markersRef = useRef(eventMarkers);
  markersRef.current = eventMarkers;
  const onScrubRef = useRef(onScrub);
  onScrubRef.current = onScrub;
  const onScrubPinRef = useRef(onScrubPin);
  onScrubPinRef.current = onScrubPin;
  const seriesRef = useRef(series);
  seriesRef.current = series;

  // uPlot wants aligned data: one x column, each series joined onto the union
  // of timestamps with nulls for gaps (spanGaps bridges them visually).
  const data = useMemo((): uPlot.AlignedData => {
    const stamps = new Set<number>();
    for (const s of series) for (const p of s.points) stamps.add(p.ts);
    const xs = [...stamps].sort((a, b) => a - b);
    const cols = series.map((s) => {
      const byTs = new Map(s.points.map((p) => [p.ts, p.value]));
      return xs.map((ts) => byTs.get(ts) ?? null);
    });
    return [xs.map((ts) => ts / 1000), ...cols];
  }, [series]);

  const seriesKey = series.map((s) => `${s.key}:${s.color}`).join("|");
  const empty = data[0].length === 0;

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const currentSeries = seriesRef.current;
    const syncMarkers = (u: uPlot) => {
      const markers = markersRef.current ?? [];
      const host = containerRef.current;
      if (!host) return;
      let next: MarkerPosition[] = [];
      if (markers.length > 0) {
        const overRect = u.over.getBoundingClientRect();
        const hostRect = host.getBoundingClientRect();
        const xMin = u.scales.x.min ?? -Infinity;
        const xMax = u.scales.x.max ?? Infinity;
        next = markers
          .filter((m) => m.ts / 1000 >= xMin && m.ts / 1000 <= xMax)
          .map((m) => ({
            ts: m.ts,
            label: m.label,
            left: overRect.left - hostRect.left + u.valToPos(m.ts / 1000, "x"),
            top: overRect.top - hostRect.top,
          }));
      }
      setMarkerPos((prev) => (markersEqual(prev, next) ? prev : next));
    };

    const plotSeries: uPlot.Series[] = [
      {},
      ...currentSeries.map((s) => ({
        stroke: theme.resolve(s.color),
        width: 2,
        spanGaps: true,
        points: { show: false },
        show: !hiddenRef.current.has(s.key),
      })),
    ];

    const options: uPlot.Options = {
      width: el.clientWidth,
      height,
      padding: [8, 8, 0, 0],
      legend: { show: false },
      cursor: {
        y: false,
        drag: { x: false, y: false },
        points: { size: 6 },
      },
      scales: {
        y: { range: (_u, min, max) => chartYRange(unit, min, max) },
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
          // Fixed 52px clipped anything longer than about seven characters —
          // "12.5 Mbps", "1,234 MB", "20.7 GB" all ran out of the frame.
          // Size the gutter to the widest label uPlot is about to draw.
          size: (_u, values) => {
            const widest = (values ?? []).reduce((n, v) => Math.max(n, String(v ?? "").length), 0);
            return Math.min(96, Math.max(52, widest * 6.5 + 14));
          },
          // Formatting can collapse distinct ticks onto the same text — a
          // per-minute axis spanning 0..0.07 rounds every tick to "0/min".
          // Draw each label once; the gridlines still mark every tick.
          values: (_u, ticks) => {
            let previous: string | null = null;
            return ticks.map((t) => {
              const label = formatMetricValue(unit, t);
              if (label === previous) return "";
              previous = label;
              return label;
            });
          },
        },
      ],
      series: plotSeries,
      hooks: {
        ready: [
          (u) => {
            u.over.addEventListener("click", () => {
              const idx = u.cursor.idx;
              if (idx == null) return;
              const ts = u.data[0][idx];
              if (ts != null) onScrubPinRef.current?.(ts * 1000);
            });
            syncMarkers(u);
          },
        ],
        draw: [
          // Audit event markers: subtle dashed verticals on the canvas; the
          // hoverable dots on top are DOM so they can carry tooltips.
          (u) => {
            syncMarkers(u);
            const markers = markersRef.current ?? [];
            if (markers.length === 0) return;
            const { ctx } = u;
            ctx.save();
            ctx.strokeStyle = theme.axisInk;
            ctx.globalAlpha = 0.4;
            ctx.setLineDash([4, 4]);
            ctx.lineWidth = 1;
            const xMin = u.scales.x.min ?? -Infinity;
            const xMax = u.scales.x.max ?? Infinity;
            for (const m of markers) {
              const ts = m.ts / 1000;
              if (ts < xMin || ts > xMax) continue;
              const x = Math.round(u.valToPos(ts, "x", true));
              ctx.beginPath();
              ctx.moveTo(x, u.bbox.top);
              ctx.lineTo(x, u.bbox.top + u.bbox.height);
              ctx.stroke();
            }
            ctx.restore();
          },
        ],
        setCursor: [
          (u) => {
            const tooltip = tooltipRef.current;
            if (!tooltip) return;
            const { idx, left } = u.cursor;
            const hide = () => {
              tooltip.style.opacity = "0";
              onScrubRef.current?.(null);
            };
            if (idx == null || left == null || left < 0) return hide();
            const ts = u.data[0][idx];
            if (ts == null) return hide();
            onScrubRef.current?.(ts * 1000);

            const xs = u.data[0];
            const spansDays = xs.length > 1 && xs[xs.length - 1] - xs[0] > 86_400;
            tooltip.replaceChildren();
            const time = document.createElement("div");
            time.className = "font-medium";
            time.textContent = new Date(ts * 1000).toLocaleString(undefined, {
              ...(spansDays ? { month: "short", day: "numeric" } : {}),
              hour: "2-digit",
              minute: "2-digit",
            });
            tooltip.append(time);
            seriesRef.current.forEach((s, i) => {
              if (hiddenRef.current.has(s.key)) return;
              // The ref can briefly lead the plot between a render and the
              // re-init effect — skip columns the plot doesn't have yet.
              const column = u.data[i + 1];
              if (!column) return;
              const value = column[idx];
              const row = document.createElement("div");
              row.className = "flex items-center gap-1.5";
              const chip = document.createElement("span");
              chip.className = "size-2 shrink-0 rounded-[2px]";
              chip.style.background = `var(${s.color})`;
              const label = document.createElement("span");
              label.className = "max-w-44 truncate opacity-80";
              label.textContent = s.label;
              const val = document.createElement("span");
              val.className = "ml-auto pl-2 font-medium tabular-nums";
              val.textContent = value == null ? "—" : formatMetricValue(unit, value);
              row.append(chip, label, val);
              tooltip.append(row);
            });
            tooltip.style.opacity = "1";
            const clamped = Math.min(Math.max(left, 70), u.width - 70);
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
    // Re-init on theme/series-set changes (and when data first arrives, since
    // the container only mounts then); point updates go through setData.
  }, [theme.key, height, unit, seriesKey, empty]);

  useEffect(() => {
    plotRef.current?.setData(data);
  }, [data]);

  // Legend toggles flip series visibility in place — no plot rebuild.
  useEffect(() => {
    const plot = plotRef.current;
    if (!plot) return;
    series.forEach((s, i) => {
      const shown = !hidden.has(s.key);
      if (plot.series[i + 1]?.show !== shown) plot.setSeries(i + 1, { show: shown });
    });
  }, [hidden, series]);

  // Marker set changed without a data change (e.g. audit loads later).
  useEffect(() => {
    plotRef.current?.redraw();
  }, [eventMarkers]);

  if (empty) return null;

  return (
    <div className="relative">
      {showUnitLabel && (
        <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
          {unitCaption[unit] ?? unit}
        </p>
      )}
      <div className="relative">
        <div ref={containerRef} />
        <div
          ref={tooltipRef}
          // Inside the plot, not above it: -top-1 put the box over the card
          // header and outside the chart's frame.
          className="bg-overlay text-fg border-edge pointer-events-none absolute top-2 z-10 min-w-32 -translate-x-1/2 space-y-0.5 rounded-md border px-2.5 py-1.5 text-[11px] whitespace-nowrap opacity-0 shadow-2 transition-opacity duration-100"
        />
        {markerPos.map((m) => (
          <Tooltip key={`${m.ts}-${m.left}`} label={m.label}>
            <span
              className="bg-fg-subtle border-bg absolute z-10 size-2 -translate-x-1/2 -translate-y-1/2 cursor-help rounded-full border"
              style={{ left: m.left, top: m.top }}
            />
          </Tooltip>
        ))}
      </div>
    </div>
  );
}
