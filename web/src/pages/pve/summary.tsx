import { useQuery } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { api, unwrap, type PveNode } from "@/lib/api";
import { useMetricSeries } from "@/lib/metrics";
import { formatBytes, formatDuration } from "@/lib/utils";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { TimeSeriesChart } from "@/components/time-series-chart";

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
  });

  if (summary.isLoading) {
    return (
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Card key={i} className="p-5">
            <Skeleton className="h-4 w-24" />
            <Skeleton className="mt-3 h-7 w-32" />
          </Card>
        ))}
      </div>
    );
  }
  if (summary.isError) {
    return (
      <Card>
        <ErrorState error={summary.error} onRetry={() => void summary.refetch()} />
      </Card>
    );
  }
  const data = summary.data!;
  const onlineNodes = data.nodes.filter((n) => n.status === "online").length;
  const running = (guests.data ?? []).filter((g) => g.status === "running").length;

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile
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
          label={t("pve.tabs.guests")}
          value={t("pve.summary.guestsRunning", { running, total: guests.data?.length ?? 0 })}
        />
        <StatTile
          label={t("pve.summary.nodesOnline", { online: onlineNodes, total: data.nodes.length })}
          value={`${onlineNodes}/${data.nodes.length}`}
        />
        <StatTile
          label={t("pve.summary.storageUsed")}
          value={`${formatBytes(data.storageUsedBytes)} / ${formatBytes(data.storageTotalBytes)}`}
        />
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        {data.nodes.map((node) => (
          <NodeCard key={node.name} instanceId={instanceId} node={node} />
        ))}
      </div>
    </div>
  );
}

function StatTile({ label, value, extra }: { label: string; value: string; extra?: React.ReactNode }) {
  return (
    <Card className="p-5">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <div className="mt-1.5 flex items-center gap-2">
        <p className="text-fg truncate text-xl font-semibold tracking-tight">{value}</p>
        {extra}
      </div>
    </Card>
  );
}

function NodeCard({ instanceId, node }: { instanceId: string; node: PveNode }) {
  const { t } = useTranslation();
  const resource = `proxmox/${instanceId}/node/${node.name}`;
  const topic = `metrics:proxmox/${instanceId}`;
  const cpu = useMetricSeries(topic, resource, "cpu");
  const memUsed = useMetricSeries(topic, resource, "mem_used");
  const memTotal = useMetricSeries(topic, resource, "mem_total");

  const online = node.status === "online";
  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <StatusDot tone={online ? "success" : "danger"} pulse={!online} />
            {node.name}
          </span>
        }
        description={
          online
            ? `${node.pveVersion ?? "Proxmox VE"} · ${t("health.uptime").toLowerCase()} ${formatDuration(node.uptimeSeconds)}`
            : node.status
        }
        actions={
          <span className="text-fg-muted text-[13px]">
            {Math.round(node.cpu * 100)}% · {formatBytes(node.memBytes)} / {formatBytes(node.maxMemBytes)}
          </span>
        }
      />
      <CardBody className="grid gap-4 sm:grid-cols-2">
        <div>
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("pve.summary.cpuChart")}</p>
          <TimeSeriesChart points={cpu} kind="percent" height={120} />
        </div>
        <div>
          <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("pve.summary.memChart")}</p>
          <TimeSeriesChart points={memUsed} capacity={memTotal} kind="bytes" height={120} />
        </div>
      </CardBody>
    </Card>
  );
}
