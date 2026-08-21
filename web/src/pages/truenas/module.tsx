import { useState } from "react";
import { Link, Outlet, useNavigate, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Bell, HardDrive, X } from "lucide-react";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone } from "@/lib/systems";
import { OpenSystemLink } from "@/components/open-system";
import { cn, formatBytes, formatRelative } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { EmptyState, ErrorState } from "@/ui/states";
import {
  alertTone,
  useDismissTruenasAlert,
  useTruenasAlerts,
  useTruenasInstances,
  useTruenasOverview,
} from "./shared";

/**
 * TruenasModulePage frames one NAS instance with the tab nav and a header
 * summarising its version, host, pool count, capacity, and memory. Every tab
 * reads the same shared instance/overview queries, so they never disagree.
 */
export function TruenasModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const instances = useTruenasInstances();
  useLiveInvalidate("integrations", [["truenas", "instances"]]);
  // Overview drives the capacity chip and pool count; it is shared with the
  // Overview and Performance tabs (same query key).
  const overview = useTruenasOverview(instanceId);

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

  const info = instance.info;
  const pools = overview.data?.pools;
  const poolCount = pools?.length ?? instance.poolCount ?? 0;
  const totalCapacity = pools?.reduce((sum, p) => sum + p.sizeBytes, 0);

  const base = `/truenas/${instanceId}`;
  const tabs = [
    { to: base, label: t("truenas.tabs.overview"), exact: true },
    { to: `${base}/datasets`, label: t("truenas.tabs.datasets") },
    { to: `${base}/shares`, label: t("truenas.tabs.shares") },
    { to: `${base}/snapshots`, label: t("truenas.tabs.snapshots") },
    { to: `${base}/performance`, label: t("truenas.tabs.performance") },
  ];

  // The upstream version string already embeds the product name
  // ("TrueNAS-SCALE-24.10.2"); show it verbatim, falling back to the label.
  const metaParts = [info?.version || "TrueNAS", info?.hostname || null].filter(Boolean);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <HardDrive className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg flex items-center gap-2.5 text-xl font-semibold tracking-tight">
            {instance.name}
            <Badge tone={statusTone[instance.status]}>
              <StatusDot
                tone={statusTone[instance.status]}
                pulse={instance.status !== "connected"}
              />
              {t(`integrations.status.${instance.status}`)}
            </Badge>
          </h1>
          <p className="text-fg-muted text-sm">{metaParts.join(" · ")}</p>
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          <Badge tone="neutral">{t("truenas.chips.pools", { count: poolCount })}</Badge>
          {typeof totalCapacity === "number" && (
            <Badge tone="neutral">
              {t("truenas.chips.capacity", { size: formatBytes(totalCapacity) })}
            </Badge>
          )}
          {typeof info?.physicalMemoryBytes === "number" && (
            <Badge tone="neutral">
              {t("truenas.chips.memory", { size: formatBytes(info.physicalMemoryBytes) })}
            </Badge>
          )}
          <OpenSystemLink instanceId={instanceId} />
        </div>
      </div>

      {instance.status !== "connected" && instance.statusMessage && (
        <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-4 py-3 text-[13px]" role="alert">
          {instance.statusMessage}
        </div>
      )}

      <nav className="border-edge no-scrollbar flex items-center gap-1 overflow-x-auto overflow-y-hidden border-b">
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
        <AlertsBell instanceId={instanceId} base={base} active={pathname.startsWith(`${base}/alerts`)} />
      </nav>

      <Outlet />
    </div>
  );
}

/**
 * AlertsBell sits at the far right of the tab bar — a shopping-cart-style
 * quick view. The bell rings (a periodic swing) while anything warning-or-
 * worse is active; clicking opens a popover with the worst alerts, per-row
 * dismiss, clear-all, and a link to the full alert page.
 */
function AlertsBell({ instanceId, base, active }: { instanceId: string; base: string; active: boolean }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [open, setOpen] = useState(false);

  const alerts = useTruenasAlerts(instanceId);
  const dismiss = useDismissTruenasAlert(instanceId);

  const rank = { danger: 0, warning: 1, info: 2, neutral: 3 } as const;
  const activeAlerts = [...(alerts.data ?? [])]
    .filter((a) => !a.dismissed)
    .sort((a, b) => rank[alertTone(a.level)] - rank[alertTone(b.level)]);
  const worst = alertTone(activeAlerts[0]?.level ?? "");
  const bad = worst === "danger" || worst === "warning";

  const goToAll = () => {
    setOpen(false);
    void navigate({ to: `${base}/alerts` });
  };

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          type="button"
          aria-label={t("truenas.alerts.bell")}
          className={cn(
            "-mb-px ml-auto flex shrink-0 items-center gap-1.5 border-b-2 px-3.5 py-2 text-[13px] font-medium transition-colors",
            active ? "border-accent text-fg" : "text-fg-muted hover:text-fg border-transparent",
          )}
        >
          <span className="relative inline-flex">
            <Bell
              className={cn(
                "size-4",
                bad && (worst === "danger" ? "text-danger" : "text-warning"),
                bad && "hh-bell-ringing",
              )}
            />
            {activeAlerts.length > 0 && (
              <span
                className={cn(
                  "text-accent-fg absolute -right-1.5 -top-1.5 flex h-3.5 min-w-3.5 items-center justify-center rounded-full px-0.5 text-[9.5px] font-bold leading-none",
                  worst === "danger" ? "bg-danger" : worst === "warning" ? "bg-warning" : "bg-accent",
                )}
              >
                {activeAlerts.length}
              </span>
            )}
          </span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-88 p-0">
        <div className="flex items-center gap-2 px-3.5 py-2.5">
          <span className="text-fg text-[13px] font-semibold">
            {t("truenas.alerts.activeCount", { count: activeAlerts.length })}
          </span>
          {activeAlerts.some((a) => a.uuid) && (
            <Button
              variant="ghost"
              size="sm"
              className="ml-auto"
              disabled={dismiss.isPending}
              onClick={() => dismiss.mutate({ all: true })}
            >
              {t("truenas.alerts.clearAll")}
            </Button>
          )}
        </div>
        <div className="border-edge/70 max-h-80 overflow-y-auto border-t">
          {activeAlerts.length === 0 && (
            <p className="text-fg-subtle px-3.5 py-4 text-[13px]">{t("truenas.alerts.noneActive")}</p>
          )}
          {activeAlerts.slice(0, 6).map((alert, i) => {
            const tone = alertTone(alert.level);
            return (
              <div
                key={alert.uuid ?? i}
                className="border-edge/70 flex items-start gap-2 border-b px-3.5 py-2.5 text-[12.5px] last:border-b-0"
              >
                <span className="mt-1">
                  <StatusDot tone={tone === "neutral" ? "info" : tone} />
                </span>
                <div className="min-w-0 flex-1">
                  <p className="text-fg line-clamp-2">{alert.message}</p>
                  {alert.createdAt && (
                    <p className="text-fg-subtle mt-0.5 text-[11px]">{formatRelative(alert.createdAt)}</p>
                  )}
                </div>
                {alert.uuid && (
                  <button
                    type="button"
                    className="text-fg-subtle hover:text-fg mt-0.5 shrink-0"
                    aria-label={t("truenas.alerts.dismiss")}
                    disabled={dismiss.isPending}
                    onClick={() => dismiss.mutate({ uuid: alert.uuid })}
                  >
                    <X className="size-3.5" />
                  </button>
                )}
              </div>
            );
          })}
        </div>
        <div className="border-edge/70 border-t px-2 py-1.5">
          <Button variant="ghost" size="sm" className="w-full" onClick={goToAll}>
            {t("truenas.alerts.viewAll")}
          </Button>
        </div>
      </PopoverContent>
    </Popover>
  );
}
