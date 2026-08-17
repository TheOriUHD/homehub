import { useState, type ReactNode } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, HardDrive } from "lucide-react";
import type {
  TruenasAlert,
  TruenasDisk,
  TruenasMemory,
  TruenasPool,
  TruenasSystemInfo,
} from "@/lib/api";
import { cn, formatBytes, formatDuration, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import {
  alertTone,
  capacityBarClass,
  capacityTone,
  diskTempWarn,
  poolHealthTone,
  useTruenasAlerts,
  useTruenasOverview,
} from "./shared";

/**
 * TruenasOverviewPage is the module's default tab, laid out as a dashboard:
 * a top row of System / Memory / Storage summary cards (the memory card is
 * the TrueNAS-style RAM dial), then the per-pool capacity cards, then any
 * active alerts followed by the disk inventory.
 */
export function TruenasOverviewPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const overview = useTruenasOverview(instanceId);
  const alerts = useTruenasAlerts(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const { info, memory, pools, disks } = overview.data!;
  const activeAlerts = (alerts.data ?? []).filter((a) => !a.dismissed);

  return (
    <div className="space-y-5">
      {/* Two wide bars (System, Storage) stacked beside the memory dial —
          the dial is naturally tall, the other two naturally wide. */}
      <div className="grid items-stretch gap-3 lg:grid-cols-3">
        <div className="flex min-w-0 flex-col gap-3 lg:col-span-2">
          <SystemCard info={info} />
          <StorageSummaryCard pools={pools} className="flex-1" />
        </div>
        <MemoryCard memory={memory} />
      </div>

      {activeAlerts.length > 0 && <AlertsSection alerts={activeAlerts} />}

      <section className="space-y-2.5">
        <h2 className="text-fg text-[15px] font-semibold">{t("truenas.overview.pools")}</h2>
        {pools.length === 0 ? (
          <Card>
            <EmptyState
              icon={HardDrive}
              title={t("truenas.overview.noPools.title")}
              body={t("truenas.overview.noPools.body")}
            />
          </Card>
        ) : (
          <div
            className={cn(
              "grid items-start gap-3",
              // One pool owns the row; more split it.
              pools.length > 1 && "md:grid-cols-2",
              pools.length > 2 && "xl:grid-cols-3",
            )}
          >
            {pools.map((pool) => (
              <PoolCard key={pool.name} pool={pool} />
            ))}
          </div>
        )}
      </section>

      <section className="space-y-2.5">
        <h2 className="text-fg text-[15px] font-semibold">{t("truenas.overview.disks")}</h2>
        <Card className="overflow-hidden">
          {disks.length === 0 ? (
            <EmptyState icon={HardDrive} title={t("truenas.overview.noDisks")} />
          ) : (
            <Table>
              <THead>
                <TR>
                  <TH>{t("truenas.overview.diskColumns.name")}</TH>
                  <TH>{t("truenas.overview.diskColumns.model")}</TH>
                  <TH>{t("truenas.overview.diskColumns.serial")}</TH>
                  <TH>{t("truenas.overview.diskColumns.type")}</TH>
                  <TH className="text-right">{t("truenas.overview.diskColumns.size")}</TH>
                  <TH className="text-right">{t("truenas.overview.diskColumns.temperature")}</TH>
                  <TH>{t("truenas.overview.diskColumns.pool")}</TH>
                </TR>
              </THead>
              <TBody>
                {disks.map((disk) => (
                  <DiskRow key={disk.name} disk={disk} />
                ))}
              </TBody>
            </Table>
          )}
        </Card>
      </section>
    </div>
  );
}

/** Muted inline note for a summary card whose data source is absent. */
function CardNote({ children }: { children: ReactNode }) {
  return <p className="text-fg-subtle py-6 text-center text-[13px]">{children}</p>;
}

/* ---- system card ---------------------------------------------------------- */

function SystemCard({ info }: { info: TruenasSystemInfo | null | undefined }) {
  const { t } = useTranslation();

  const rows: Array<{ key: string; value: string }> = [];
  if (info) {
    // The hostname leads; the version becomes its subtitle. If the hostname
    // is missing the version takes the lead slot instead of repeating below.
    if (info.model) rows.push({ key: "model", value: info.model });
    if (typeof info.cores === "number") rows.push({ key: "cores", value: String(info.cores) });
    // Prefer the numeric uptime — the raw string is "3:21:54.413328".
    if (typeof info.uptimeSeconds === "number" && info.uptimeSeconds > 0) {
      rows.push({ key: "uptime", value: formatDuration(info.uptimeSeconds) });
    } else if (info.uptime) {
      rows.push({ key: "uptime", value: info.uptime });
    }
  }
  const lead = info?.hostname || info?.version;

  return (
    <Card>
      <CardHeader title={t("truenas.overview.system.title")} />
      <CardBody>
        {!info || !lead ? (
          <CardNote>{t("truenas.overview.system.unavailable")}</CardNote>
        ) : (
          <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-3">
            <div className="min-w-0">
              <p className="text-fg truncate text-lg font-semibold tracking-tight">{lead}</p>
              {info.hostname && info.version && (
                <p className="text-fg-muted truncate text-[13px]">{info.version}</p>
              )}
            </div>
            {rows.length > 0 && (
              <dl className="flex shrink-0 flex-wrap items-center gap-x-6 gap-y-2 text-[13px]">
                {rows.map((row) => (
                  <div key={row.key} className="min-w-0">
                    <dt className="text-fg-subtle text-[10.5px] font-semibold tracking-wide uppercase">
                      {t(`truenas.overview.system.${row.key}`)}
                    </dt>
                    <dd className="text-fg truncate font-medium">{row.value}</dd>
                  </div>
                ))}
              </dl>
            )}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

/* ---- memory card (the RAM dial) ------------------------------------------- */

/**
 * Dial geometry. Segment identity lives in the legend and per-segment
 * <title>s, never in color alone. Hues: ZFS Cache wears the accent, Services
 * the next categorical slot (--hh-series-2, teal — validated CVD-separable
 * from the accent in both themes), and Free the edge tone: a deliberate
 * neutral for the unallocated remainder, with the legend carrying its value.
 */
const DIAL_SIZE = 160;
const DIAL_CENTER = DIAL_SIZE / 2;
const DIAL_RADIUS = 64;
const DIAL_STROKE = 15;
/** A 2px surface gap between segments, expressed as degrees at the radius. */
const DIAL_GAP_DEG = (2 / DIAL_RADIUS) * (180 / Math.PI);

const MEMORY_SEGMENTS = [
  { key: "zfsCache", color: "var(--hh-accent)", pick: (m: TruenasMemory) => m.arcBytes },
  { key: "services", color: "var(--hh-series-2)", pick: (m: TruenasMemory) => m.servicesBytes },
  { key: "free", color: "var(--hh-edge-strong)", pick: (m: TruenasMemory) => m.freeBytes },
] as const;

function dialPoint(deg: number): { x: number; y: number } {
  const rad = ((deg - 90) * Math.PI) / 180; // 0° at 12 o'clock, clockwise
  return {
    x: DIAL_CENTER + DIAL_RADIUS * Math.cos(rad),
    y: DIAL_CENTER + DIAL_RADIUS * Math.sin(rad),
  };
}

function dialArcPath(startDeg: number, endDeg: number): string {
  const s = dialPoint(startDeg);
  const e = dialPoint(endDeg);
  const large = endDeg - startDeg > 180 ? 1 : 0;
  return `M ${s.x.toFixed(2)} ${s.y.toFixed(2)} A ${DIAL_RADIUS} ${DIAL_RADIUS} 0 ${large} 1 ${e.x.toFixed(2)} ${e.y.toFixed(2)}`;
}

function MemoryCard({ memory }: { memory: TruenasMemory | null | undefined }) {
  const { t } = useTranslation();

  const segments = memory
    ? MEMORY_SEGMENTS.map((s) => ({ key: s.key, color: s.color, bytes: s.pick(memory) })).filter(
        (s) => s.bytes > 0,
      )
    : [];
  const sum = segments.reduce((acc, s) => acc + s.bytes, 0);

  if (!memory || sum <= 0) {
    return (
      <Card>
        <CardHeader title={t("truenas.overview.memory.title")} />
        <CardBody>
          <CardNote>{t("truenas.overview.memory.unavailable")}</CardNote>
        </CardBody>
      </Card>
    );
  }

  let angle = 0;
  const arcs = segments.map((s) => {
    const start = angle;
    const sweep = (s.bytes / sum) * 360;
    angle += sweep;
    return { ...s, start, sweep, pct: Math.round((s.bytes / sum) * 100) };
  });

  const total = memory.totalBytes > 0 ? memory.totalBytes : sum;
  const segmentLabel = (a: (typeof arcs)[number]) =>
    t("truenas.overview.memory.segment", {
      name: t(`truenas.overview.memory.${a.key}`),
      size: formatBytes(a.bytes),
      pct: a.pct,
    });
  const dialLabel = t("truenas.overview.memory.dial", {
    total: formatBytes(total),
    segments: arcs.map(segmentLabel).join(", "),
  });

  return (
    <Card className="flex flex-col">
      <CardHeader title={t("truenas.overview.memory.title")} />
      <CardBody className="flex flex-1 flex-wrap content-center items-center justify-center gap-x-5 gap-y-3">
        <div className="relative size-[160px] shrink-0">
          <svg
            viewBox={`0 0 ${DIAL_SIZE} ${DIAL_SIZE}`}
            className="size-full"
            role="img"
            aria-label={dialLabel}
          >
            {arcs.length === 1 ? (
              <circle
                cx={DIAL_CENTER}
                cy={DIAL_CENTER}
                r={DIAL_RADIUS}
                fill="none"
                stroke={arcs[0].color}
                strokeWidth={DIAL_STROKE}
              >
                <title>{segmentLabel(arcs[0])}</title>
              </circle>
            ) : (
              arcs.map((a) => {
                // Trim half the surface gap from each end; slivers keep a
                // minimal visible sweep instead of inverting.
                const half = DIAL_GAP_DEG / 2;
                const a0 = a.start + half;
                const a1 = Math.max(a.start + a.sweep - half, a0 + 0.5);
                return (
                  <path
                    key={a.key}
                    d={dialArcPath(a0, a1)}
                    fill="none"
                    stroke={a.color}
                    strokeWidth={DIAL_STROKE}
                    strokeLinecap="butt"
                  >
                    <title>{segmentLabel(a)}</title>
                  </path>
                );
              })
            )}
          </svg>
          <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center">
            <span className="text-fg text-xl font-semibold tracking-tight">
              {formatBytes(total)}
            </span>
            <span className="text-fg-subtle text-xs">{t("truenas.overview.memory.total")}</span>
          </div>
        </div>

        <ul className="min-w-36 max-w-full space-y-2">
          {arcs.map((a) => (
            <li key={a.key} className="flex items-center gap-2 text-[13px]">
              <span
                aria-hidden
                className="size-2.5 shrink-0 rounded-full"
                style={{ background: a.color }}
              />
              <span className="text-fg-muted min-w-0 flex-1 truncate">
                {t(`truenas.overview.memory.${a.key}`)}
              </span>
              <span className="text-fg shrink-0 font-medium tabular-nums">
                {formatBytes(a.bytes)}
              </span>
            </li>
          ))}
        </ul>
      </CardBody>
    </Card>
  );
}

/* ---- storage summary card -------------------------------------------------- */

function StorageSummaryCard({ pools, className }: { pools: TruenasPool[]; className?: string }) {
  const { t } = useTranslation();

  const totalSize = pools.reduce((acc, p) => acc + p.sizeBytes, 0);
  const totalUsed = pools.reduce((acc, p) => acc + p.allocatedBytes, 0);
  const overallPct = totalSize > 0 ? (totalUsed / totalSize) * 100 : 0;

  return (
    <Card className={className}>
      <CardHeader
        title={t("truenas.overview.storage.title")}
        actions={<Badge tone="neutral">{t("truenas.chips.pools", { count: pools.length })}</Badge>}
      />
      <CardBody>
        {pools.length === 0 || totalSize <= 0 ? (
          <CardNote>{t("truenas.overview.storage.unavailable")}</CardNote>
        ) : (
          <div className="space-y-3">
            <div>
              <div className="flex flex-wrap items-baseline justify-between gap-x-3 gap-y-0.5">
                <p className="text-fg text-lg font-semibold tracking-tight">
                  {t("truenas.overview.usedOfTotal", {
                    used: formatBytes(totalUsed),
                    total: formatBytes(totalSize),
                  })}
                </p>
                <span className={cn("text-[13px] font-medium", capacityTextClass[capacityTone(overallPct)])}>
                  {t("truenas.overview.percentFull", { pct: Math.round(overallPct) })}
                </span>
              </div>
              <div className="bg-bg-subtle mt-2 h-2 w-full overflow-hidden rounded-full">
                <div
                  className={cn("h-full rounded-full transition-[width]", capacityBarClass(overallPct))}
                  style={{ width: `${Math.min(Math.max(overallPct, 0), 100)}%` }}
                />
              </div>
            </div>

            <ul className="space-y-1.5">
              {pools.map((pool) => {
                const pct = Math.min(Math.max(pool.usedPct, 0), 100);
                return (
                  <li
                    key={pool.name}
                    className="grid grid-cols-[minmax(3.5rem,1fr)_2fr_2.75rem] items-center gap-2.5 text-[13px]"
                  >
                    <span className="text-fg truncate font-medium">{pool.name}</span>
                    <span className="bg-bg-subtle h-1.5 overflow-hidden rounded-full">
                      <span
                        className={cn("block h-full rounded-full", capacityBarClass(pool.usedPct))}
                        style={{ width: `${pct}%` }}
                      />
                    </span>
                    <span className="text-fg-muted text-right text-xs tabular-nums">
                      {t("truenas.overview.pct", { pct: Math.round(pool.usedPct) })}
                    </span>
                  </li>
                );
              })}
            </ul>
          </div>
        )}
      </CardBody>
    </Card>
  );
}

/* ---- pools ----------------------------------------------------------------- */

const capacityTextClass: Record<ReturnType<typeof capacityTone>, string> = {
  success: "text-success-soft-fg",
  warning: "text-warning-soft-fg",
  danger: "text-danger-soft-fg",
};

function PoolCard({ pool }: { pool: TruenasPool }) {
  const { t } = useTranslation();
  const pct = Math.min(Math.max(pool.usedPct, 0), 100);

  return (
    <Card className="p-4">
      <div className="flex items-center gap-2">
        <p className="text-fg truncate text-sm font-semibold">{pool.name}</p>
        <div className="ml-auto flex items-center gap-1.5">
          <Badge tone={poolHealthTone(pool)}>{pool.status}</Badge>
          <Badge tone={pool.healthy ? "success" : poolHealthTone(pool)}>
            {pool.healthy ? t("truenas.overview.healthy") : t("truenas.overview.unhealthy")}
          </Badge>
        </div>
      </div>

      <div className="mt-3">
        <div className="bg-bg-subtle h-2 w-full overflow-hidden rounded-full">
          <div
            className={cn("h-full rounded-full transition-[width]", capacityBarClass(pool.usedPct))}
            style={{ width: `${pct}%` }}
          />
        </div>
        <div className="text-fg-muted mt-1.5 flex items-center justify-between text-[13px]">
          <span className={cn("font-medium", capacityTextClass[capacityTone(pool.usedPct)])}>
            {t("truenas.overview.usedOfTotal", {
              used: formatBytes(pool.allocatedBytes),
              total: formatBytes(pool.sizeBytes),
            })}
          </span>
          <span>{t("truenas.overview.percentFull", { pct: Math.round(pool.usedPct) })}</span>
        </div>
      </div>

      <div className="text-fg-subtle mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        <span>{t("truenas.overview.free", { size: formatBytes(pool.freeBytes) })}</span>
        {pool.fragmentation !== undefined && (
          <span>{t("truenas.overview.fragmentation", { pct: pool.fragmentation })}</span>
        )}
      </div>
    </Card>
  );
}

/* ---- disks + alerts --------------------------------------------------------- */

function DiskRow({ disk }: { disk: TruenasDisk }) {
  const { t } = useTranslation();
  const isSsd = /ssd|nvme/i.test(disk.type);
  return (
    <TR>
      <TD className="text-fg font-medium">{disk.name}</TD>
      <TD className="text-fg-muted">{disk.model || "—"}</TD>
      <TD className="text-fg-subtle font-mono text-xs">{disk.serial || "—"}</TD>
      <TD>
        <Badge tone={isSsd ? "info" : "neutral"}>{disk.type}</Badge>
      </TD>
      <TD className="text-fg-muted text-right tabular-nums">{formatBytes(disk.size)}</TD>
      <TD className="text-right tabular-nums">
        {typeof disk.temperature === "number" ? (
          <span className={diskTempWarn(disk.temperature) ? "text-warning-soft-fg font-medium" : "text-fg-muted"}>
            {t("truenas.overview.celsius", { value: disk.temperature })}
          </span>
        ) : (
          <span className="text-fg-subtle">—</span>
        )}
      </TD>
      <TD className="text-fg-muted">{disk.pool || "—"}</TD>
    </TR>
  );
}

/**
 * AlertsSection is a single slim card: one line per alert with a severity
 * dot, sorted worst-first, truncated to three with an expander. Present but
 * quiet — alerts should be seen, not shout over the dashboard.
 */
function AlertsSection({ alerts }: { alerts: TruenasAlert[] }) {
  const { t } = useTranslation();
  const [showAll, setShowAll] = useState(false);
  const rank = { danger: 0, warning: 1, info: 2, neutral: 3 } as const;
  const sorted = [...alerts].sort((a, b) => rank[alertTone(a.level)] - rank[alertTone(b.level)]);
  const visible = showAll ? sorted : sorted.slice(0, 3);
  const worst = alertTone(sorted[0]?.level ?? "");

  return (
    <Card>
      <div className="divide-edge/70 divide-y">
        <div className="flex items-center gap-2 px-4 py-2.5">
          <AlertTriangle
            className={cn(
              "size-4",
              worst === "danger" ? "text-danger" : worst === "warning" ? "text-warning" : "text-fg-subtle",
            )}
          />
          <span className="text-fg text-[13.5px] font-semibold">
            {t("truenas.overview.alertCount", { count: alerts.length })}
          </span>
        </div>
        {visible.map((alert, i) => {
          const tone = alertTone(alert.level);
          return (
            <div key={`${alert.createdAt ?? ""}-${i}`} className="flex items-center gap-2.5 px-4 py-2 text-[13px]">
              <StatusDot tone={tone === "neutral" ? "info" : tone} />
              <span className="text-fg min-w-0 flex-1 truncate" title={alert.message}>
                {alert.message}
              </span>
              {alert.createdAt && (
                <span className="text-fg-subtle shrink-0 text-xs">{formatRelative(alert.createdAt)}</span>
              )}
            </div>
          );
        })}
        {sorted.length > 3 && (
          <button
            className="text-accent hover:text-accent/80 w-full cursor-pointer px-4 py-2 text-left text-[12.5px] font-medium transition-colors"
            onClick={() => setShowAll((v) => !v)}
          >
            {showAll ? t("truenas.overview.alertsLess") : t("truenas.overview.alertsMore", { count: sorted.length - 3 })}
          </button>
        )}
      </div>
    </Card>
  );
}
