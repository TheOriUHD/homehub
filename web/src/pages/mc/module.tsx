import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Pickaxe, Users } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone, useIntegrations } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
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
          <div className="flex flex-wrap items-center gap-1.5">
            {typeof status.tps === "number" && (
              <Badge tone={tpsTone(status.tps)}>{t("mc.chips.tps", { tps: status.tps.toFixed(1) })}</Badge>
            )}
            <Badge tone="neutral">
              <Users aria-hidden className="size-3" />
              {t("mc.chips.players", { online: status.playersOnline, max: status.playersMax })}
            </Badge>
            {typeof status.heapUsedMb === "number" && (
              <Badge tone="neutral">
                {typeof status.heapMaxMb === "number"
                  ? t("mc.chips.heap", {
                      used: Math.round(status.heapUsedMb),
                      max: Math.round(status.heapMaxMb),
                    })
                  : t("mc.chips.heapNoMax", { used: Math.round(status.heapUsedMb) })}
              </Badge>
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
