import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, MoreHorizontal, Waypoints, Wrench } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { Button } from "@/ui/button";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger } from "@/ui/dropdown";
import { CaddySetupDialog } from "./setup";
import { caddyResource } from "./shared";
import { api, unwrap } from "@/lib/api";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Card } from "@/ui/card";
import { statusTone } from "@/lib/systems";

export function CaddyModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/caddy/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { can } = useAuth();
  const [setupOpen, setSetupOpen] = useState(false);

  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  useLiveInvalidate(`integration:caddy/${instanceId}`, [["integrations"]]);

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
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const base = `/caddy/${instanceId}`;
  const tabs = [
    { to: base, label: t("caddy.tabs.sites"), exact: true },
    { to: `${base}/dns`, label: t("caddy.tabs.dns") },
    { to: `${base}/served`, label: t("caddy.tabs.served") },
  ];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Waypoints className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot tone={statusTone[instance.status]} pulse={instance.status !== "connected"} />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">{t("caddy.subtitle")}</p>
        </div>

        {can(caddyResource(instanceId), "configure") && (
          <Dropdown>
            <DropdownTrigger asChild>
              <Button size="sm" variant="ghost" aria-label={t("common.actions")}>
                <MoreHorizontal className="size-4" />
              </Button>
            </DropdownTrigger>
            <DropdownContent align="end">
              <DropdownItem onSelect={() => setSetupOpen(true)}>
                <Wrench className="size-3.5" />
                {t("caddy.setup.menuLabel")}
              </DropdownItem>
            </DropdownContent>
          </Dropdown>
        )}
      </div>

      {setupOpen && <CaddySetupDialog instanceId={instanceId} onClose={() => setSetupOpen(false)} />}

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
          {instance.status === "degraded"
            ? t("caddy.degradedBanner", { message: instance.statusMessage })
            : t("caddy.unreachableBanner", { message: instance.statusMessage })}
        </div>
      )}

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto border-b">
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
