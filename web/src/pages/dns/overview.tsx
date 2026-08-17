import { useEffect, useRef, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { AlertTriangle } from "lucide-react";
import { api, unwrap, isApiError, type DnsBlocking, type DnsInstance } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useMetricSeries } from "@/lib/metrics";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { ErrorState } from "@/ui/states";
import { Skeleton } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { TimeSeriesChart } from "@/components/time-series-chart";
import { statusTone } from "@/lib/systems";
import { dnsResource, dnsTypeLabel, useDnsInstances } from "./module";

function formatPct(fraction: number): string {
  return `${(fraction * 100).toFixed(1)}%`;
}

export function DnsOverviewPage() {
  const { t } = useTranslation();
  const instances = useDnsInstances();

  if (instances.isLoading) {
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
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }

  const list = instances.data!;
  const totalQueries = list.reduce((sum, i) => sum + (i.stats?.queriesToday ?? 0), 0);
  const totalBlocked = list.reduce((sum, i) => sum + (i.stats?.blockedToday ?? 0), 0);
  const online = list.filter((i) => i.status === "connected").length;

  return (
    <div className="space-y-5">
      <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <StatTile label={t("dns.overview.queriesToday")} value={totalQueries.toLocaleString()} />
        <StatTile label={t("dns.overview.blockedToday")} value={totalBlocked.toLocaleString()} />
        <StatTile
          label={t("dns.overview.blockedPct")}
          value={totalQueries > 0 ? formatPct(totalBlocked / totalQueries) : "—"}
        />
        <StatTile label={t("dns.overview.instancesOnline")} value={`${online}/${list.length}`} />
      </div>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {list.map((instance) => (
          <InstanceCard key={instance.id} instance={instance} />
        ))}
      </div>
    </div>
  );
}

function StatTile({ label, value }: { label: string; value: string }) {
  return (
    <Card className="p-5">
      <p className="text-fg-subtle text-xs font-semibold tracking-wide uppercase">{label}</p>
      <p className="text-fg mt-1.5 truncate text-xl font-semibold tracking-tight">{value}</p>
    </Card>
  );
}

function InstanceCard({ instance }: { instance: DnsInstance }) {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const resource = dnsResource(instance);
  const topic = `metrics:${resource}`;
  const queriesPerMin = useMetricSeries(topic, resource, "queries_per_min");
  const blockedPct = useMetricSeries(topic, resource, "blocked_pct");

  const stats = instance.stats;
  const canControl = can(resource, "control");

  const setBlocking = useMutation({
    mutationFn: async (body: { enabled: boolean; seconds?: number }) =>
      unwrap(
        await api.POST("/dns/{id}/blocking", { params: { path: { id: instance.id } }, body }),
      ),
    // The response is authoritative for blocking state — patch it into the
    // cached instance list so the card flips without waiting for a refetch.
    onSuccess: (result: DnsBlocking) => {
      queryClient.setQueryData<DnsInstance[]>(["dns", "instances"], (prev) =>
        prev?.map((row) =>
          row.id === instance.id && row.stats
            ? { ...row, stats: { ...row.stats, enabled: result.enabled, disabledUntil: result.until } }
            : row,
        ),
      );
      toast(
        "success",
        result.enabled
          ? t("dns.blocking.enabledToast", { name: instance.name })
          : t("dns.blocking.disabledToast", { name: instance.name }),
      );
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Card>
      <CardHeader
        title={
          <span className="flex items-center gap-2">
            <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
            {instance.name}
            <Badge tone="neutral">{dnsTypeLabel[instance.type]}</Badge>
          </span>
        }
        description={
          stats
            ? `${stats.version} · ${t("dns.overview.instanceSummary", {
                queries: stats.queriesToday.toLocaleString(),
                pct: stats.queriesToday > 0 ? formatPct(stats.blockedToday / stats.queriesToday) : "—",
              })}`
            : t(`integrations.status.${instance.status}`)
        }
      />
      <CardBody className="space-y-4">
        {instance.status !== "connected" && instance.statusMessage && (
          <div className="bg-danger-soft text-danger-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]" role="alert">
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            {instance.statusMessage}
          </div>
        )}

        {stats && (
          <div className="flex flex-wrap items-center gap-2">
            {stats.enabled ? (
              <>
                <Badge tone="success">{t("dns.blocking.active")}</Badge>
                {canControl && (
                  <span className="flex flex-wrap items-center gap-1.5">
                    <span className="text-fg-subtle text-[13px]">{t("dns.blocking.disableFor")}</span>
                    <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 300 })}>
                      {t("dns.blocking.for5m")}
                    </Button>
                    <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 1800 })}>
                      {t("dns.blocking.for30m")}
                    </Button>
                    <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false, seconds: 3600 })}>
                      {t("dns.blocking.for1h")}
                    </Button>
                    <Button size="sm" variant="secondary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: false })}>
                      {t("dns.blocking.untilEnabled")}
                    </Button>
                  </span>
                )}
              </>
            ) : (
              <>
                <Badge tone="warning">{t("dns.blocking.disabled")}</Badge>
                {stats.disabledUntil && <Countdown until={stats.disabledUntil} />}
                {canControl && (
                  <Button size="sm" variant="primary" loading={setBlocking.isPending} onClick={() => setBlocking.mutate({ enabled: true })}>
                    {t("dns.blocking.enable")}
                  </Button>
                )}
              </>
            )}
          </div>
        )}

        <div className="grid gap-4 sm:grid-cols-2">
          <div>
            <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartQueries")}</p>
            <TimeSeriesChart points={queriesPerMin} kind="count" height={120} />
          </div>
          <div>
            <p className="text-fg-subtle mb-1 text-xs font-semibold uppercase">{t("dns.overview.chartBlocked")}</p>
            <TimeSeriesChart points={blockedPct} kind="percent" height={120} />
          </div>
        </div>
      </CardBody>
    </Card>
  );
}

/**
 * Countdown ticks client-side once a second until the disable window ends,
 * then refreshes the instance list once so the card flips back to enabled.
 */
function Countdown({ until }: { until: string }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const [now, setNow] = useState(() => Date.now());
  const firedRef = useRef(false);

  useEffect(() => {
    const id = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(id);
  }, []);

  const remaining = Math.max(0, Math.floor((new Date(until).getTime() - now) / 1000));

  useEffect(() => {
    if (remaining === 0 && !firedRef.current) {
      firedRef.current = true;
      void queryClient.invalidateQueries({ queryKey: ["dns", "instances"] });
    }
  }, [remaining, queryClient]);

  const hours = Math.floor(remaining / 3600);
  const minutes = Math.floor((remaining % 3600) / 60);
  const seconds = remaining % 60;
  const time =
    hours > 0
      ? `${hours}:${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`
      : `${minutes}:${String(seconds).padStart(2, "0")}`;

  return (
    <span className="text-fg-muted text-[13px] tabular-nums">
      {t("dns.blocking.resumesIn", { time })}
    </span>
  );
}
