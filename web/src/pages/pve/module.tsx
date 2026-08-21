import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, PanelLeft, Server } from "lucide-react";
import { api, unwrap, type Integration } from "@/lib/api";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Card } from "@/ui/card";
import { statusTone } from "@/lib/systems";
import { Button } from "@/ui/button";
import { OpenSystemLink } from "@/components/open-system";
import { PveResourceTree } from "./tree";

/** usePveInstance loads the integration row for the current module route. */
export function usePveInstance(): { instanceId: string; instance: Integration | undefined } {
  const { instanceId } = useParams({ strict: false }) as { instanceId: string };
  const query = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  return { instanceId, instance: query.data?.find((row) => row.id === instanceId) };
}

export function PveModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/pve/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  // The tree is permanent from lg up and a disclosure panel below it.
  const [treeOpen, setTreeOpen] = useState(false);

  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  useLiveInvalidate(`integration:proxmox/${instanceId}`, [["integrations"]]);

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
    // Not an ErrorState(new Error(...)): ErrorState only surfaces ApiFailure
    // messages and would flatten this into the generic "something went wrong".
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const base = `/pve/${instanceId}`;
  const tabs = [
    { to: base, label: t("pve.tabs.summary"), exact: true },
    { to: `${base}/guests`, label: t("pve.tabs.guests") },
    { to: `${base}/storage`, label: t("pve.tabs.storage") },
    { to: `${base}/tasks`, label: t("pve.tabs.tasks") },
    { to: `${base}/backups`, label: t("pve.tabs.backups") },
  ];

  return (
    <div className="flex items-start gap-5">
      {/* Proxmox-style resource tree: sticky beside the content on wide
          screens, collapsed behind a toggle on narrow ones. The shell's
          <main> is the scroll container, so top-0 sticks under the header. */}
      <PveResourceTree
        instanceId={instanceId}
        className="sticky top-0 hidden max-h-[calc(100dvh-6.5rem)] w-64 shrink-0 lg:flex"
      />

      <div className="min-w-0 flex-1 space-y-5">
        <div className="flex flex-wrap items-center gap-3">
          <Button
            variant="secondary"
            size="sm"
            className="lg:hidden"
            aria-expanded={treeOpen}
            onClick={() => setTreeOpen((open) => !open)}
          >
            <PanelLeft className="size-4" />
            {t("pve.tree.title")}
          </Button>
          <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
            <Server className="size-5" />
          </span>
          <div className="min-w-0 flex-1">
            <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
              {instance.name}
              <Badge tone={statusTone[instance.status]}>
                <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
                {t(`integrations.status.${instance.status}`)}
              </Badge>
            </h1>
            <p className="text-fg-muted text-sm">Proxmox</p>
          </div>
          <OpenSystemLink instanceId={instanceId} />
        </div>

        {treeOpen && (
          <PveResourceTree
            instanceId={instanceId}
            className="max-h-[70vh] lg:hidden"
            onNavigate={() => setTreeOpen(false)}
          />
        )}

        {(instance.status === "unreachable" || instance.status === "degraded") && (
          <div
            className={cn(
              "flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]",
              instance.status === "unreachable"
                ? "bg-danger-soft text-danger-soft-fg"
                : "bg-warning-soft text-warning-soft-fg",
            )}
            role="alert"
          >
            <AlertTriangle className="mt-0.5 size-4 shrink-0" />
            {instance.status === "unreachable"
              ? t("pve.unreachableBanner", { message: instance.statusMessage })
              : t("pve.degradedBanner", { message: instance.statusMessage })}
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
    </div>
  );
}
