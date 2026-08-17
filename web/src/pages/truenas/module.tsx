import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, HardDrive } from "lucide-react";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone } from "@/lib/systems";
import { cn, formatBytes } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { useTruenasInstances, useTruenasOverview } from "./shared";

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
        </div>
      </div>

      {instance.status !== "connected" && instance.statusMessage && (
        <div className="bg-danger-soft text-danger-soft-fg rounded-lg px-4 py-3 text-[13px]" role="alert">
          {instance.statusMessage}
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
