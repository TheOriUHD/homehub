import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, ServerCog } from "lucide-react";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { useFleetInstances, usePatchpilotHosts, ppTopic } from "./shared";

/**
 * PatchpilotModulePage frames one fleet with the tab nav and a header
 * summarising host count and pending updates. Live WS events (bootstrap
 * steps, run events, host changes) invalidate the relevant queries.
 */
export function PatchpilotModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/patchpilot/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const instances = useFleetInstances();
  useLiveInvalidate("integrations", [["integrations"]]);
  useLiveInvalidate(ppTopic(instanceId), [["patchpilot", instanceId]]);
  const hosts = usePatchpilotHosts(instanceId);

  if (instances.isLoading) return <PageLoader />;
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }
  const instance = instances.data?.find((row) => row.id === instanceId && row.type === "patchpilot");
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const hostList = hosts.data ?? [];
  const pendingTotal = hostList.reduce((sum, h) => sum + h.updatesPending, 0);
  const rebootCount = hostList.filter((h) => h.rebootRequired).length;

  const base = `/patchpilot/${instanceId}`;
  const tabs = [
    { to: base, label: t("patchpilot.tabs.hosts"), exact: true },
    { to: `${base}/schedules`, label: t("patchpilot.tabs.schedules") },
    { to: `${base}/runs`, label: t("patchpilot.tabs.runs") },
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <ServerCog className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">{t("patchpilot.subtitle")}</p>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <Badge tone="neutral">{t("patchpilot.chips.hosts", { count: hostList.length })}</Badge>
          {pendingTotal > 0 && (
            <Badge tone="warning">{t("patchpilot.chips.updates", { count: pendingTotal })}</Badge>
          )}
          {rebootCount > 0 && (
            <Badge tone="warning">{t("patchpilot.chips.reboot", { count: rebootCount })}</Badge>
          )}
        </div>
      </div>

      {instance.status !== "connected" && instance.statusMessage && (
        <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-4 py-3 text-[13px]" role="alert">
          {instance.statusMessage}
        </div>
      )}

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto overflow-y-hidden border-b">
        {tabs.map((tab) => {
          const active = tab.exact ? pathname === tab.to : pathname.startsWith(tab.to);
          return (
            <Link
              key={tab.to}
              to={tab.to}
              className={cn(
                "-mb-px shrink-0 border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
                active ? "border-accent text-fg" : "text-fg-muted hover:text-fg border-transparent",
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
