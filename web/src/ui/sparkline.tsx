import { useEffect, useMemo, useRef, useState } from "react";
import uPlot from "uplot";
import "uplot/dist/uPlot.min.css";
import { cn } from "@/lib/utils";

export type SparklinePoint = { ts: number; value: number };

/**
 * Sparkline is the axis-less miniature of TimeSeriesChart: a thin accent
 * line with a soft area fill and nothing else — no grid, no ticks, no
 * cursor. Made for dashboard cards where the number next to it carries the
 * meaning and the line only shows the shape of the last hour.
 */

// withAlpha derives a translucent canvas color from a design token, whatever
// its syntax: hex gets a hex alpha suffix, functional colors (oklch, modern
// rgb/hsl) get a slash alpha — appending hex to those is a parse error.
function withAlpha(color: string, alpha: number): string {
  if (color.startsWith("#")) {
    const hex = Math.round(alpha * 255)
      .toString(16)
      .padStart(2, "0");
    return color + hex;
  }
  if (color.endsWith(")")) {
    return `${color.slice(0, -1)} / ${alpha})`;
  }
  return color;
}

/** Resolve the accent token for canvas ink; re-read when the theme flips. */
function useAccentInk() {
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
    return { key: theme, accent: style.getPropertyValue("--hh-accent").trim() };
  }, [theme]);
}

export function Sparkline({
  points,
  height = 48,
  className,
}: {
  points: SparklinePoint[];
  height?: number;
  className?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const plotRef = useRef<uPlot | null>(null);
  const ink = useAccentInk();

  const data = useMemo(
    (): uPlot.AlignedData => [points.map((p) => p.ts / 1000), points.map((p) => p.value)],
    [points],
  );

  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const options: uPlot.Options = {
      width: el.clientWidth,
      height,
      padding: [2, 0, 2, 0],
      legend: { show: false },
      cursor: { show: false },
      scales: {
        y: { range: (_u, _min, max) => [0, Math.max(max * 1.1, 1)] },
      },
      axes: [{ show: false }, { show: false }],
      series: [
        {},
        {
          stroke: ink.accent,
          width: 1.5,
          fill: (u) => {
            const gradient = u.ctx.createLinearGradient(0, u.bbox.top, 0, u.bbox.top + u.bbox.height);
            gradient.addColorStop(0, withAlpha(ink.accent, 0.22));
            gradient.addColorStop(1, withAlpha(ink.accent, 0));
            return gradient;
          },
          points: { show: false },
        },
      ],
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
    // Re-init on theme change only; data updates go through setData below.
  }, [ink.key, height]);

  useEffect(() => {
    plotRef.current?.setData(data);
  }, [data]);

  // Decorative: the adjacent text carries the value for assistive tech.
  return <div ref={containerRef} aria-hidden className={cn("pointer-events-none", className)} />;
}
