import { Link, useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Gauge, MemoryStick, Pickaxe, Users, Waypoints } from "lucide-react";
import { capacityTextClass } from "@/lib/capacity";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { loaderLabel, PROXY_LOADERS, tpsTone, useMcOverview } from "./shared";
import { MiniStat } from "@/components/mini-stat";

/**
 * The network dashboard: one card per member server — proxy or subserver —
 * each a live headline (status, players, TPS, heap) linking into that
 * server's console.
 */
export function McNetworkPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId/" });
  const overview = useMcOverview(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }
  const servers = overview.data?.servers ?? [];
  if (servers.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Pickaxe}
          title={t("mc.network.emptyTitle")}
          body={t("mc.network.emptyBody")}
        />
      </Card>
    );
  }

  // Proxies first — they front the network.
  const ordered = [...servers].sort((a, b) => {
    const ap = PROXY_LOADERS.includes(a.loader ?? "") ? 0 : 1;
    const bp = PROXY_LOADERS.includes(b.loader ?? "") ? 0 : 1;
    return ap - bp;
  });

  const connected = servers.filter((sv) => sv.connected);
  const players = connected.reduce((sum, sv) => sum + (sv.status?.playersOnline ?? 0), 0);
  const slots = connected.reduce((sum, sv) => sum + (sv.status?.playersMax ?? 0), 0);
  const tpsValues = connected
    .map((sv) => sv.status?.tps)
    .filter((v): v is number => typeof v === "number");
  const worstTps = tpsValues.length > 0 ? Math.min(...tpsValues) : null;
  const heapMb = connected.reduce((sum, sv) => sum + (sv.status?.heapUsedMb ?? 0), 0);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
        <MiniStat
          icon={Pickaxe}
          label={t("mc.network.stats.servers")}
          value={`${connected.length}/${servers.length}`}
          tone={connected.length === servers.length ? "success" : "warning"}
        />
        <MiniStat icon={Users} label={t("mc.network.stats.players")} value={players} sub={slots > 0 ? `/ ${slots}` : undefined} />
        <MiniStat
          icon={Gauge}
          label={t("mc.network.stats.worstTps")}
          value={worstTps != null ? worstTps.toFixed(1) : "—"}
          tone={worstTps != null ? (worstTps >= 18 ? "success" : worstTps >= 15 ? "warning" : "danger") : undefined}
        />
        <MiniStat icon={MemoryStick} label={t("mc.network.stats.heap")} value={heapMb > 0 ? `${(heapMb / 1024).toFixed(1)} GB` : "—"} />
      </div>

      <div className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
      {ordered.map((server) => {
        const isProxy = PROXY_LOADERS.includes(server.loader ?? "");
        const Icon = isProxy ? Waypoints : Pickaxe;
        const st = server.status;
        return (
          <Link
            key={server.id}
            to="/mc/$instanceId/s/$serverId"
            params={{ instanceId, serverId: server.id }}
            className="group block"
          >
            <Card className="hover:border-accent/50 h-full transition-colors">
              <CardBody className="space-y-3">
                <div className="flex items-center gap-2.5">
                  <span className="bg-accent-soft text-accent-soft-fg flex size-8 shrink-0 items-center justify-center rounded-lg">
                    <Icon className="size-4" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <p className="text-fg truncate text-[14.5px] font-semibold">{server.label}</p>
                    <p className="text-fg-subtle truncate text-[12px]">
                      {[loaderLabel(server.loader), server.mcVersion].filter(Boolean).join(" ") ||
                        t("mc.network.unknownLoader")}
                    </p>
                  </div>
                  <StatusDot tone={server.connected ? "success" : "neutral"} pulse={!server.connected} />
                </div>

                {server.connected && st ? (
                  <div className="flex flex-wrap gap-x-4 gap-y-1 text-[13px]">
                    <span className="text-fg-muted">
                      {t("mc.chips.players", { online: st.playersOnline, max: st.playersMax })}
                    </span>
                    {typeof st.tps === "number" && (
                      <span className={cn("font-medium tabular-nums", capacityTextClass[tpsTone(st.tps)])}>
                        {st.tps.toFixed(1)} TPS
                      </span>
                    )}
                    {typeof st.heapUsedMb === "number" && (
                      <span className="text-fg-muted tabular-nums">
                        {Math.round(st.heapUsedMb)} MB
                      </span>
                    )}
                    {isProxy && (st.downstream?.length ?? 0) > 0 && (
                      <Badge tone="neutral">
                        {t("mc.network.backends", { count: st.downstream!.length })}
                      </Badge>
                    )}
                  </div>
                ) : (
                  <p className="text-fg-subtle text-[13px]">{t("mc.network.offline")}</p>
                )}
              </CardBody>
            </Card>
          </Link>
        );
      })}
      </div>
    </div>
  );
}
