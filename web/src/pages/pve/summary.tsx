import { useCallback, useEffect, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Boxes, Cpu, MemoryStick, Network, Server } from "lucide-react";
import type { ReactNode } from "react";
import type { LucideIcon } from "lucide-react";
import { api, unwrap, type PveNode } from "@/lib/api";
import { capacityBarClass, capacityTextClass, capacityTone } from "@/lib/capacity";
import { useMetricSeries } from "@/lib/metrics";
import { cn, formatBytes, formatDuration } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { MultiSeriesChart, type ChartSeries } from "@/components/multi-series-chart";

/**
 * PveSummaryPage is the instance dashboard: an aggregate stat row (cluster
 * identity, nodes, guests, fleet CPU/memory computed from the node list),
 * compact per-node cards with capacity-toned CPU/memory bars, and per-node
 * history charts fed by the metrics engine. Storage and tasks live on their
 * own tabs and are deliberately not repeated here.
 */
export function PveSummaryPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId/" });

  const summary = useQuery({
    queryKey: ["pve", instanceId, "summary"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/summary", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });
  const guests = useQuery({
    queryKey: ["pve", instanceId, "guests"],
    queryFn: async () =>
      unwrap(await api.GET("/proxmox/{id}/guests", { params: { path: { id: instanceId } } })),
    refetchInterval: 30_000,
  });

  if (summary.isLoading) return <SummarySkeleton />;
  if (summary.isError) {
    return (
      <Card>
        <ErrorState error={summary.error} onRetry={() => void summary.refetch()} />
      </Card>
    );
  }

  const data = summary.data!;
  const online = data.nodes.filter((n) => n.status === "online");

  // Fleet aggregates over online nodes. A node's cpu is a 0..1 fraction of
  // all its cores, so the cluster percentage weights each node by core count.
  const cores = online.reduce((sum, n) => sum + n.maxCpu, 0);
  const cpuPct = cores > 0 ? (online.reduce((sum, n) => sum + n.cpu * n.maxCpu, 0) / cores) * 100 : 0;
  const memUsed = online.reduce((sum, n) => sum + n.memBytes, 0);
  const memTotal = online.reduce((sum, n) => sum + n.maxMemBytes, 0);
  const memPct = memTotal > 0 ? (memUsed / memTotal) * 100 : 0;

  const running = guests.data?.filter((g) => g.status === "running").length ?? 0;

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
        <StatTile
          icon={Network}
          label={data.clusterName ? t("pve.summary.cluster") : t("pve.summary.standalone")}
          value={data.clusterName || data.nodes[0]?.name || "—"}
          extra={
            data.quorate != null ? (
              <Badge tone={data.quorate ? "success" : "danger"}>
                {data.quorate ? t("pve.summary.quorate") : t("pve.summary.noQuorum")}
              </Badge>
            ) : undefined
          }
        />
        <StatTile
          icon={Server}
          label={t("pve.summary.nodesLabel")}
          value={
            <span className="flex items-baseline gap-1.5">
              {online.length}/{data.nodes.length}
              <StatusDot
                tone={online.length === data.nodes.length ? "success" : online.length > 0 ? "warning" : "danger"}
              />
            </span>
          }
        />
        <StatTile
          icon={Boxes}
          label={t("pve.summary.guestsLabel")}
          value={
            guests.data ? (
              <span>
                {running}
                <span className="text-fg-muted text-sm font-medium">/{guests.data.length}</span>
              </span>
            ) : (
              "—"
            )
          }
        />
        <StatTile
          icon={Cpu}
          label={t("pve.summary.cpuChart")}
          value={
            cores > 0 ? (
              <span>
                {cpuPct.toFixed(cpuPct < 10 ? 1 : 0)}%
                <span className="text-fg-muted text-sm font-medium">
                  {" "}
                  · {t("pve.summary.cores", { count: cores })}
                </span>
              </span>
            ) : (
              "—"
            )
          }
          meterPct={cores > 0 ? cpuPct : undefined}
        />
        <StatTile
          icon={MemoryStick}
          label={t("pve.summary.memChart")}
          value={
            memTotal > 0 ? (
              <span className="text-base">
                {formatBytes(memUsed)}
                <span className="text-fg-muted text-sm font-medium"> / {formatBytes(memTotal)}</span>
              </span>
            ) : (
              "—"
            )
          }
          meterPct={memTotal > 0 ? memPct : undefined}
        />
      </div>

      <section className="space-y-2.5">
        <h2 className="text-fg text-[15px] font-semibold">{t("pve.summary.nodes")}</h2>
        <div className="grid items-start gap-4 md:grid-cols-2 xl:grid-cols-3">
          {data.nodes.map((node) => (
            <NodeCard key={node.name} node={node} />
          ))}
        </div>
      </section>

      {data.nodes.length > 0 && <ClusterCharts instanceId={instanceId} nodes={data.nodes} />}
    </div>
  );
}

function SummarySkeleton() {
  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-5">
        {Array.from({ length: 5 }, (_, i) => (
          <Card key={i} className="p-4">
            <Skeleton className="h-3.5 w-24" />
            <Skeleton className="mt-2.5 h-6 w-20" />
          </Card>
        ))}
      </div>
      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
        {Array.from({ length: 2 }, (_, i) => (
          <Card key={i} className="p-4">
            <Skeleton className="h-5 w-40" />
            <Skeleton className="mt-4 h-4 w-full" />
            <Skeleton className="mt-3 h-4 w-full" />
          </Card>
        ))}
      </div>
    </div>
  );
}

function StatTile({
  icon: Icon,
  label,
  value,
  extra,
  meterPct,
}: {
  icon: LucideIcon;
  label: string;
  value: ReactNode;
  /** Optional second row (e.g. the quorum badge) under the value. */
  extra?: ReactNode;
  meterPct?: number;
}) {
  const clamped = meterPct === undefined ? undefined : Math.min(Math.max(meterPct, 0), 100);
  return (
    <Card className="p-4">
      <div className="flex items-center gap-3.5">
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
          <Icon className="size-4" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">{label}</p>
          <div className="text-fg mt-0.5 truncate text-lg font-semibold tracking-tight">{value}</div>
        </div>
      </div>
      {extra !== undefined && <div className="mt-2.5">{extra}</div>}
      {clamped !== undefined && (
        <div className="bg-bg-subtle mt-3 h-1.5 w-full overflow-hidden rounded-full">
          <div
            className={cn("h-full rounded-full transition-[width]", capacityBarClass(clamped))}
            style={{ width: `${clamped}%` }}
          />
        </div>
      )}
    </Card>
  );
}

/**
 * One node's live state: identity with a status dot and uptime, then CPU and
 * memory bars in the shared capacity tones. Offline nodes stay on the board,
 * dimmed, without bars — their last-reported numbers would only mislead.
 */
/**
 * Proxmox reports versions like "pve-manager/9.2.10/43df2e01f27a1a19"; the
 * build hash after the second slash is noise at card scale.
 */
function shortPveVersion(version: string | undefined): string {
  if (!version) return "Proxmox VE";
  return version.split("/").slice(0, 2).join("/");
}

function NodeCard({ node }: { node: PveNode }) {
  const { t } = useTranslation();
  const online = node.status === "online";
  const cpuPct = node.cpu * 100;
  const memPct = node.maxMemBytes > 0 ? (node.memBytes / node.maxMemBytes) * 100 : 0;

  return (
    <Card className={cn("p-4", !online && "opacity-75")}>
      <div className="flex items-start gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 shrink-0 items-center justify-center rounded-lg">
          <Server className="size-4.5" aria-hidden />
        </span>
        <div className="min-w-0 flex-1">
          <p className="text-fg flex items-center gap-2 text-sm font-semibold">
            <span className="truncate">{node.name}</span>
            <StatusDot tone={online ? "success" : "danger"} pulse={!online} />
          </p>
          <p className="text-fg-muted truncate text-xs">
            {online
              ? `${shortPveVersion(node.pveVersion)} · ${t("health.uptime").toLowerCase()} ${formatDuration(node.uptimeSeconds)}`
              : node.status}
          </p>
        </div>
      </div>

      {online && (
        <div className="mt-3.5 space-y-3">
          <Meter
            label={t("pve.summary.cpuChart")}
            value={t("pve.summary.nodeCpu", {
              pct: cpuPct.toFixed(cpuPct < 10 ? 1 : 0),
              count: node.maxCpu,
            })}
            pct={cpuPct}
          />
          <Meter
            label={t("pve.summary.memChart")}
            value={t("pve.summary.memValue", {
              used: formatBytes(node.memBytes),
              total: formatBytes(node.maxMemBytes),
              pct: Math.round(memPct),
            })}
            pct={memPct}
          />
        </div>
      )}
    </Card>
  );
}

function Meter({ label, value, pct }: { label: string; value: string; pct: number }) {
  const clamped = Math.min(Math.max(pct, 0), 100);
  return (
    <div>
      <div className="flex items-baseline justify-between gap-2">
        <span className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">
          {label}
        </span>
        <span className={cn("text-[13px] font-medium tabular-nums", capacityTextClass[capacityTone(clamped)])}>
          {value}
        </span>
      </div>
      <div className="bg-bg-subtle mt-1.5 h-1.5 w-full overflow-hidden rounded-full">
        <div
          className={cn("h-full rounded-full transition-[width]", capacityBarClass(clamped))}
          style={{ width: `${clamped}%` }}
        />
      </div>
    </div>
  );
}

/* ---- history charts --------------------------------------------------------- */

type NodeSeries = { cpu: ChartSeries; mem: ChartSeries };

/**
 * ClusterCharts plots the last hour of every node's CPU and memory from the
 * metrics engine — one line per node in the fixed categorical order, so the
 * same node wears the same color in both charts. Node timestamps are not
 * bucket-aligned across nodes; MultiSeriesChart null-joins them instead of
 * pretending they sum.
 */
function ClusterCharts({ instanceId, nodes }: { instanceId: string; nodes: PveNode[] }) {
  const { t } = useTranslation();
  const [series, setSeries] = useState<Record<string, NodeSeries>>({});

  const handleSeries = useCallback((node: string, s: NodeSeries) => {
    setSeries((prev) => ({ ...prev, [node]: s }));
  }, []);

  // Order by node index so colors stay stable across renders.
  const loaded = nodes.map((n) => series[n.name]).filter((s): s is NodeSeries => Boolean(s));
  const cpuSeries = loaded.map((s) => s.cpu);
  const memSeries = loaded.map((s) => s.mem);
  const hasCpu = cpuSeries.some((s) => s.points.length > 0);
  const hasMem = memSeries.some((s) => s.points.length > 0);

  return (
    <>
      {nodes.map((node, i) => (
        <NodeSeriesLoader
          key={node.name}
          instanceId={instanceId}
          node={node.name}
          color={`--hh-series-${(i % 6) + 1}`}
          onSeries={handleSeries}
        />
      ))}
      <div className="grid items-start gap-4 lg:grid-cols-2">
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
            {t("pve.summary.chartCpu")}
          </p>
          {hasCpu ? <MultiSeriesChart series={cpuSeries} height={180} /> : <ChartCollecting />}
        </Card>
        <Card className="p-4">
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">
            {t("pve.summary.chartMem")}
          </p>
          {hasMem ? <MultiSeriesChart series={memSeries} height={180} /> : <ChartCollecting />}
        </Card>
      </div>
    </>
  );
}

/**
 * NodeSeriesLoader loads one node's cpu + mem_used series and lifts them to
 * the parent — one metrics hook pair per mounted loader, so the hook count
 * stays constant (Rules of Hooks) while the combined charts plot them all.
 */
function NodeSeriesLoader({
  instanceId,
  node,
  color,
  onSeries,
}: {
  instanceId: string;
  node: string;
  color: string;
  onSeries: (node: string, series: NodeSeries) => void;
}) {
  const topic = `metrics:proxmox/${instanceId}`;
  const resource = `proxmox/${instanceId}/node/${node}`;
  const cpu = useMetricSeries(topic, resource, "cpu");
  const mem = useMetricSeries(topic, resource, "mem_used");

  useEffect(() => {
    onSeries(node, {
      cpu: { key: `${resource}|cpu`, label: node, unit: "fraction", points: cpu, color },
      mem: { key: `${resource}|mem_used`, label: node, unit: "bytes", points: mem, color },
    });
  }, [node, resource, color, cpu, mem, onSeries]);

  return null;
}

function ChartCollecting() {
  const { t } = useTranslation();
  return (
    <div className="text-fg-subtle flex h-[180px] items-center justify-center text-[13px]">
      {t("pve.summary.collecting")}
    </div>
  );
}
