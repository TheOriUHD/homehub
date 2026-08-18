import { useMemo, useRef, useState, useLayoutEffect, type MouseEvent } from "react";
import { useTranslation } from "react-i18next";
import { ArrowDownToLine, ArrowUpFromLine, Network } from "lucide-react";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import type { TruenasInterfaceTraffic, TruenasTrafficPoint } from "@/lib/api";
import { formatBps } from "./shared";

/**
 * TrafficChart is a netdata-style mirrored throughput chart: incoming fills
 * upward from the baseline, outgoing mirrors downward, and each side scales
 * independently to fit recent activity (so a quiet uplink doesn't flatten a
 * busy downlink). Pure SVG — no chart library — because the mirrored
 * dual-scale layout doesn't map onto the shared uPlot components.
 */
export function TrafficChart({
  points,
  height = 190,
  inLabel,
  outLabel,
}: {
  points: TruenasTrafficPoint[];
  height?: number;
  inLabel: string;
  outLabel: string;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(600);
  const [hover, setHover] = useState<number | null>(null);

  useLayoutEffect(() => {
    const el = wrapRef.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setWidth(el.clientWidth || 600));
    ro.observe(el);
    setWidth(el.clientWidth || 600);
    return () => ro.disconnect();
  }, []);

  const pad = { left: 8, right: 56, top: 6, bottom: 18 };
  const plotW = Math.max(width - pad.left - pad.right, 40);
  const plotH = Math.max(height - pad.top - pad.bottom, 60);

  const model = useMemo(() => {
    if (points.length < 2) return null;
    const maxIn = Math.max(...points.map((p) => p.inBps), 1);
    const maxOut = Math.max(...points.map((p) => p.outBps), 1);
    // Each side gets plot height proportional to its peak, clamped so the
    // smaller side never collapses below a readable band.
    const share = Math.min(Math.max(maxIn / (maxIn + maxOut), 0.3), 0.82);
    const inH = plotH * share;
    const outH = plotH - inH;
    const inMax = niceCeil(maxIn);
    const outMax = niceCeil(maxOut);

    const t0 = points[0].ts;
    const t1 = points[points.length - 1].ts;
    const span = Math.max(t1 - t0, 1);
    const x = (ts: number) => pad.left + ((ts - t0) / span) * plotW;
    const yIn = (v: number) => pad.top + inH - (Math.min(v, inMax) / inMax) * inH;
    const yOut = (v: number) => pad.top + inH + (Math.min(v, outMax) / outMax) * outH;
    const base = pad.top + inH;

    let inPath = `M ${x(points[0].ts)} ${base}`;
    let outPath = `M ${x(points[0].ts)} ${base}`;
    for (const p of points) {
      inPath += ` L ${x(p.ts)} ${yIn(p.inBps)}`;
      outPath += ` L ${x(p.ts)} ${yOut(p.outBps)}`;
    }
    inPath += ` L ${x(t1)} ${base} Z`;
    outPath += ` L ${x(t1)} ${base} Z`;

    return { inPath, outPath, base, inMax, outMax, x, yIn, yOut, t0, t1, span, inH, outH };
  }, [points, plotW, plotH, pad.left, pad.top]);

  if (!model) return null;

  const hoverPoint = hover != null ? points[hover] : null;

  const onMove = (e: MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = e.clientX - rect.left;
    const frac = (px - pad.left) / plotW;
    const ts = model.t0 + frac * model.span;
    let best = 0;
    let bestD = Infinity;
    for (let i = 0; i < points.length; i++) {
      const d = Math.abs(points[i].ts - ts);
      if (d < bestD) {
        bestD = d;
        best = i;
      }
    }
    setHover(best);
  };

  return (
    <div ref={wrapRef} className="relative w-full">
      <svg
        width={width}
        height={height}
        className="block"
        onMouseMove={onMove}
        onMouseLeave={() => setHover(null)}
      >
        {/* grid + scale labels: one mid tick per side plus the peaks */}
        {[
          { y: model.yIn(model.inMax), label: formatBps(model.inMax) },
          { y: model.yIn(model.inMax / 2), label: formatBps(model.inMax / 2) },
          { y: model.base, label: "0" },
          ...(model.outH > 24
            ? [{ y: model.yOut(model.outMax), label: formatBps(model.outMax) }]
            : []),
        ].map((tick, i) => (
          <g key={i}>
            <line
              x1={pad.left}
              x2={pad.left + plotW}
              y1={tick.y}
              y2={tick.y}
              stroke="var(--hh-edge)"
              strokeDasharray={tick.label === "0" ? undefined : "3 4"}
            />
            <text
              x={pad.left + plotW + 6}
              y={tick.y + 3.5}
              fontSize={10.5}
              fill="var(--hh-fg-subtle)"
            >
              {tick.label}
            </text>
          </g>
        ))}

        <path d={model.inPath} fill="var(--hh-series-1)" fillOpacity={0.75} />
        <path d={model.outPath} fill="var(--hh-series-3)" fillOpacity={0.8} />

        {/* time axis */}
        {timeTicksOf(model).map((tick, i) => (
          <text
            key={i}
            x={tick.x}
            y={height - 4}
            fontSize={10.5}
            fill="var(--hh-fg-subtle)"
            textAnchor={i === 0 ? "start" : i === 2 ? "end" : "middle"}
          >
            {tick.label}
          </text>
        ))}

        {hoverPoint && (
          <line
            x1={model.x(hoverPoint.ts)}
            x2={model.x(hoverPoint.ts)}
            y1={pad.top}
            y2={pad.top + plotH}
            stroke="var(--hh-fg-subtle)"
            strokeOpacity={0.5}
          />
        )}
      </svg>

      {hoverPoint && (
        <div
          className="bg-surface-raised border-edge text-fg pointer-events-none absolute top-1 z-10 rounded-md border px-2.5 py-1.5 text-[11.5px] shadow-sm"
          style={{
            left: Math.min(Math.max(model.x(hoverPoint.ts) - 60, 0), width - 150),
          }}
        >
          <p className="text-fg-subtle">
            {new Date(hoverPoint.ts * 1000).toLocaleTimeString(undefined, {
              hour: "2-digit",
              minute: "2-digit",
            })}
          </p>
          <p>
            <span className="mr-1 inline-block size-2 rounded-full" style={{ background: "var(--hh-series-1)" }} />
            {inLabel} {formatBps(hoverPoint.inBps)}
          </p>
          <p>
            <span className="mr-1 inline-block size-2 rounded-full" style={{ background: "var(--hh-series-3)" }} />
            {outLabel} {formatBps(hoverPoint.outBps)}
          </p>
        </div>
      )}
    </div>
  );
}

// Time ticks are derived, not stateful — tiny helper to keep render tidy.
function timeTicksOf(model: { t0: number; t1: number; span: number; x: (ts: number) => number }) {
  return [model.t0, model.t0 + model.span / 2, model.t1].map((ts) => ({
    x: model.x(ts),
    label: new Date(ts * 1000).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" }),
  }));
}

/** niceCeil rounds a max up to a 1/2/5 × 10ⁿ boundary so scales read clean. */
function niceCeil(v: number): number {
  const exp = Math.floor(Math.log10(v));
  const base = Math.pow(10, exp);
  const m = v / base;
  if (m <= 1) return base;
  if (m <= 2) return 2 * base;
  if (m <= 5) return 5 * base;
  return 10 * base;
}

/**
 * TrafficCard mirrors TrueNAS's own dashboard widget: adapter identity and
 * live In/Out on the left, the mirrored auto-scaling area chart filling the
 * rest. The rates shown are the newest sample of the hour window.
 */
export function TrafficCard({ iface }: { iface: TruenasInterfaceTraffic }) {
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
