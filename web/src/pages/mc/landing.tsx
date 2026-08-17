import { useQueries } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Cpu, Gauge, Pickaxe, Users } from "lucide-react";
import { api, unwrap, type Integration, type McOverview } from "@/lib/api";
import { capacityTextClass } from "@/lib/capacity";
import { statusTone, useIntegrations } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card, CardBody } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { loaderLabel, mcOverviewKey, tpsTone } from "./shared";

/**
 * McLandingPage is the Minecraft dashboard across every server: summary
 * cards up top, then one live card per server with players, TPS, and heap —
 * not just a bare list of names.
 */
export function McLandingPage() {
  const { t } = useTranslation();
  const integrations = useIntegrations();

  const rows = (integrations.data ?? []).filter(
    (row): row is Integration => row.type === "minecraft" && row.enabled,
  );

  const overviews = useQueries({
    queries: rows.map((row) => ({
      queryKey: mcOverviewKey(row.id),
      queryFn: async () =>
        unwrap(await api.GET("/mc/{id}/overview", { params: { path: { id: row.id } } })),
      refetchInterval: 15_000,
    })),
  });

  if (integrations.isLoading) return <PageLoader />;
  if (integrations.isError) {
    return (
      <Card>
        <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
      </Card>
    );
  }

  const byId = new Map<string, McOverview>();
  rows.forEach((row, i) => {
    const data = overviews[i]?.data;
    if (data) byId.set(row.id, data);
  });

  const connected = rows.filter((row) => byId.get(row.id)?.connected).length;
  const players = rows.reduce((sum, row) => {
    const o = byId.get(row.id);
    return sum + (o?.connected ? (o.status?.playersOnline ?? 0) : 0);
  }, 0);
  const maxPlayers = rows.reduce((sum, row) => {
    const o = byId.get(row.id);
    return sum + (o?.connected ? (o.status?.playersMax ?? 0) : 0);
  }, 0);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Pickaxe className="size-5" />
        </span>
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("mc.landing.title")}</h1>
          <p className="text-fg-muted text-sm">{t("mc.landing.subtitle")}</p>
        </div>
      </div>

      <div className="grid gap-3 sm:grid-cols-2">
        <StatCard icon={Pickaxe} value={`${connected} / ${rows.length}`} label={t("mc.landing.serversConnected")} />
        <StatCard icon={Users} value={maxPlayers > 0 ? `${players} / ${maxPlayers}` : String(players)} label={t("mc.landing.playersOnline")} />
      </div>

      {rows.length === 0 ? (
        <Card>
          <EmptyState icon={Pickaxe} title={t("mc.landing.emptyTitle")} body={t("mc.landing.emptyBody")} />
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
          {rows.map((row) => (
            <ServerCard key={row.id} row={row} overview={byId.get(row.id)} />
          ))}
        </div>
      )}
    </div>
  );
}

function StatCard({ icon: Icon, value, label }: { icon: typeof Users; value: string; label: string }) {
  return (
    <Card>
      <CardBody className="flex items-center gap-4">
        <span className="bg-accent-soft text-accent-soft-fg flex size-11 shrink-0 items-center justify-center rounded-xl">
          <Icon className="size-5" />
        </span>
        <div className="min-w-0">
          <p className="text-fg text-2xl font-semibold tracking-tight">{value}</p>
          <p className="text-fg-muted text-[13px]">{label}</p>
        </div>
      </CardBody>
    </Card>
  );
}

function ServerCard({ row, overview }: { row: Integration; overview: McOverview | undefined }) {
  const { t } = useTranslation();
  const status = overview?.connected ? overview.status : null;
  const loader = loaderLabel(overview?.loader);
  const meta = [
    loader ? (overview?.mcVersion ? `${loader} ${overview.mcVersion}` : loader) : (overview?.mcVersion ?? null),
    overview?.serverName || null,
  ].filter(Boolean);

  return (
    <Link to="/mc/$instanceId" params={{ instanceId: row.id }} className="group block">
      <Card className="hover:border-accent/50 h-full transition-colors">
        <CardBody className="space-y-3">
          <div className="flex items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="text-fg group-hover:text-accent truncate font-medium transition-colors">{row.name}</p>
              <p className="text-fg-muted truncate text-[12px]">{meta.length > 0 ? meta.join(" · ") : t("mc.landing.notPairedMeta")}</p>
            </div>
            <Badge tone={statusTone[row.status]}>
              <StatusDot tone={statusTone[row.status]} pulse={row.status !== "connected"} />
              {t(`integrations.status.${row.status}`)}
            </Badge>
          </div>

          <div className="grid grid-cols-3 gap-2 text-[12px]">
            <MetricCell
              icon={Users}
              label={t("mc.stats.players")}
              value={status ? `${status.playersOnline}/${status.playersMax}` : "—"}
            />
            <MetricCell
              icon={Gauge}
              label={t("mc.stats.tps")}
              value={typeof status?.tps === "number" ? status.tps.toFixed(1) : "—"}
              valueClass={typeof status?.tps === "number" ? capacityTextClass[tpsTone(status.tps)] : undefined}
            />
            <MetricCell
              icon={Cpu}
              label={t("mc.stats.heap")}
              value={
                typeof status?.heapUsedMb === "number"
                  ? typeof status.heapMaxMb === "number"
                    ? `${(status.heapUsedMb / 1024).toFixed(1)}/${(status.heapMaxMb / 1024).toFixed(0)} GB`
                    : `${(status.heapUsedMb / 1024).toFixed(1)} GB`
                  : "—"
              }
            />
          </div>
        </CardBody>
      </Card>
    </Link>
  );
}

function MetricCell({
  icon: Icon,
  label,
  value,
  valueClass,
}: {
  icon: typeof Users;
  label: string;
  value: string;
  valueClass?: string;
}) {
  return (
    <div className="min-w-0">
      <p className="text-fg-muted flex items-center gap-1 truncate">
        <Icon className="size-3 shrink-0" />
        {label}
      </p>
      <p className={cn("truncate font-medium", valueClass ?? "text-fg")}>{value}</p>
    </div>
  );
}
