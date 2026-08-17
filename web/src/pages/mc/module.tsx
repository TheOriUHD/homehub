import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Pickaxe } from "lucide-react";
import type { ReactNode } from "react";
import { useAuth } from "@/lib/auth";
import { capacityTextClass } from "@/lib/capacity";
import { useLiveInvalidate } from "@/lib/live";
import { useMetricSeries } from "@/lib/metrics";
import { statusTone, useIntegrations } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { Sparkline } from "@/ui/sparkline";
import { ErrorState } from "@/ui/states";
import { loaderLabel, mcResource, tpsTone, useMcLive, useMcOverview } from "./shared";

export function McModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { can } = useAuth();

  const integrations = useIntegrations();
  useLiveInvalidate("integrations", [["integrations"]]);
  const overview = useMcOverview(instanceId);
  // One subscription for the whole module: status/players patch the overview
  // cache in place; companion connects/disconnects invalidate it.
  useMcLive(instanceId);

  if (integrations.isLoading) return <PageLoader />;
  if (integrations.isError) {
    return (
      <Card>
        <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
      </Card>
    );
  }
  const instance = integrations.data?.find((row) => row.id === instanceId);
  if (!instance) {
    return (
      <Card>
        <ErrorState error={new Error(t("errors.notFound.body"))} />
      </Card>
    );
  }

  const canConfigure = can(mcResource(instanceId), "configure");
  const base = `/mc/${instanceId}`;
  const tabs = [
    { to: base, label: t("mc.tabs.console"), exact: true },
    { to: `${base}/players`, label: t("mc.tabs.players") },
    ...(canConfigure ? [{ to: `${base}/setup`, label: t("mc.tabs.setup") }] : []),
    { to: `${base}/performance`, label: t("mc.tabs.performance") },
  ];

  const data = overview.data;
  const loader = loaderLabel(data?.loader);
  const meta = [
    data?.serverName || null,
    loader ? (data?.mcVersion ? `${loader} ${data.mcVersion}` : loader) : data?.mcVersion || null,
    data?.companionVersion ? t("mc.companionVersion", { version: data.companionVersion }) : null,
  ].filter(Boolean);

  const status = data?.connected ? data.status : null;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Pickaxe className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">
            {t("mc.title")}
            {meta.length > 0 && ` · ${meta.join(" · ")}`}
          </p>
        </div>

        {status && (
          <div className="flex flex-wrap items-stretch gap-2">
            {typeof status.tps === "number" && (
              <HeaderStat
                label={t("mc.stats.tps")}
                value={status.tps.toFixed(1)}
                valueClass={capacityTextClass[tpsTone(status.tps)]}
              >
                {data?.loader !== "velocity" && <HeaderTpsSparkline instanceId={instanceId} />}
              </HeaderStat>
            )}
            <HeaderStat
              label={t("mc.stats.players")}
              value={t("mc.chips.players", { online: status.playersOnline, max: status.playersMax })}
            />
            {typeof status.heapUsedMb === "number" && (
              <HeaderStat
                label={t("mc.stats.heap")}
                value={
                  typeof status.heapMaxMb === "number"
                    ? t("mc.stats.heapValue", {
                        used: Math.round(status.heapUsedMb),
                        max: Math.round(status.heapMaxMb),
                      })
                    : t("mc.stats.heapValueNoMax", { used: Math.round(status.heapUsedMb) })
                }
              />
            )}
          </div>
        )}
      </div>

      <nav className="border-edge flex gap-1 overflow-x-auto border-b">
        {tabs.map((tab) => {
          const active = tab.exact ? pathname === tab.to : pathname.startsWith(tab.to);
          return (
            <Link
              key={tab.to}
              to={tab.to}
              className={cn(
                "-mb-px shrink-0 border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
                active
                  ? "border-accent text-fg"
                  : "text-fg-muted hover:text-fg border-transparent",
              )}
            >
              {tab.label}
            </Link>
          );
        })}
      </nav>

      <Outlet />
    </div>
  );
}

/** One compact header stat: tiny uppercase label over the live value. */
function HeaderStat({
  label,
  value,
  valueClass,
  children,
}: {
  label: string;
  value: string;
  valueClass?: string;
  children?: ReactNode;
}) {
  return (
    <div className="bg-bg-subtle flex items-center gap-3 rounded-lg px-3 py-1.5">
      <div className="min-w-0">
        <p className="text-fg-subtle text-[10px] font-semibold tracking-wide uppercase">{label}</p>
        <p className={cn("text-[13px] leading-tight font-semibold tabular-nums", valueClass ?? "text-fg")}>
          {value}
        </p>
      </div>
      {children}
    </div>
  );
}

/**
 * The last hour of TPS as a header-scale sparkline beside the live number.
 * Renders nothing until the series has at least two samples; never mounted
 * for proxy loaders (Velocity has no tick loop, so no TPS).
 */
function HeaderTpsSparkline({ instanceId }: { instanceId: string }) {
  const resource = mcResource(instanceId);
  const points = useMetricSeries(`metrics:${resource}`, resource, "tps");
  if (points.length < 2) return null;
  return (
    <div className="w-[110px] shrink-0">
      <Sparkline points={points} height={36} />
    </div>
  );
}
