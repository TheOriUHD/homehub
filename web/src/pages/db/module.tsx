import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Database } from "lucide-react";
import { useAuth } from "@/lib/auth";
import { SHELL } from "@/lib/perms";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone } from "@/lib/systems";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { dbResource, engineBadgeLabel, engineLabel, useDbInstances } from "./shared";

/**
 * DbModulePage frames one instance — either engine — with the tab nav. The
 * console tab exists only for SQL engines and only with a shell grant on
 * the instance (the perms mirror; the server still re-checks every run).
 */
export function DbModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/db/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });
  const { can } = useAuth();

  const instances = useDbInstances();
  useLiveInvalidate("integrations", [["db", "instances"]]);

  if (instances.isLoading) return <PageLoader />;
  if (instances.isError) {
    return (
      <Card>
        <ErrorState error={instances.error} onRetry={() => void instances.refetch()} />
      </Card>
    );
  }
  const instance = instances.data?.find((row) => row.id === instanceId);
  if (!instance) {
    return (
      <Card>
        <EmptyState icon={AlertTriangle} title={t("errors.notFound.title")} body={t("errors.notFound.body")} />
      </Card>
    );
  }

  const consoleVisible =
    instance.engine === "mysql" && can(dbResource(instance), SHELL);
  const base = `/db/${instanceId}`;
  const tabs = [
    { to: base, label: t("db.tabs.databases"), exact: true },
    ...(consoleVisible ? [{ to: `${base}/console`, label: t("db.tabs.console") }] : []),
    // Live statements of every client — an operator's view, as on the server.
    ...(can(dbResource(instance), "control") ? [{ to: `${base}/sessions`, label: t("db.tabs.sessions") }] : []),
    { to: `${base}/performance`, label: t("db.tabs.performance") },
  ];

  const stats = instance.stats;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Database className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone="neutral">{engineBadgeLabel(instance)}</Badge>
            <Badge tone={statusTone[instance.status]}>
              <StatusDot
                tone={statusTone[instance.status]}
                pulse={instance.status !== "connected"}
              />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">
            {t("db.title")} · {stats?.flavor || engineLabel[instance.engine]}
            {stats?.version && ` ${stats.version}`}
          </p>
        </div>

        {stats && (
          <div className="flex flex-wrap items-center gap-1.5">
            {typeof stats.connectionsCurrent === "number" &&
              typeof stats.connectionsMax === "number" && (
                <Badge tone="neutral">
                  {t("db.chips.connections", {
                    current: stats.connectionsCurrent.toLocaleString(),
                    max: stats.connectionsMax.toLocaleString(),
                  })}
                </Badge>
              )}
            {typeof stats.opsPerSec === "number" && (
              <Badge tone="neutral">{t("db.chips.ops", { ops: stats.opsPerSec.toFixed(1) })}</Badge>
            )}
          </div>
        )}
      </div>

      <nav className="border-edge no-scrollbar flex gap-1 overflow-x-auto overflow-y-hidden border-b">
        {tabs.map((tab) => {
          // The browse page is a child of the databases tab, so the exact
          // match also claims /browse/… paths.
          const active = tab.exact
            ? pathname === tab.to || pathname.startsWith(`${tab.to}/browse/`)
            : pathname.startsWith(tab.to);
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
