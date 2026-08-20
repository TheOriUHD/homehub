import { Link, Outlet, useParams, useRouterState } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, Boxes, House, Lightbulb, Workflow } from "lucide-react";
import { useLiveInvalidate } from "@/lib/live";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Card } from "@/ui/card";
import { statusTone } from "@/lib/systems";
import { OpenSystemLink } from "@/components/open-system";
import { useHaEntities, useHaInstances, useHaLiveStates } from "./shared";
import { MiniStat } from "@/components/mini-stat";
import type { HaEntity } from "@/lib/api";

export function HaModulePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ha/$instanceId" });
  const pathname = useRouterState({ select: (s) => s.location.pathname });

  const instances = useHaInstances();
  useLiveInvalidate("integrations", [["ha", "instances"]]);
  const entities = useHaEntities(instanceId);
  // One subscription for the whole module: "state" events patch the entities
  // cache in place; "entities-changed" invalidates it.
  useHaLiveStates(instanceId);

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

  const base = `/ha/${instanceId}`;
  const tabs = [
    { to: base, label: t("ha.tabs.dashboard"), exact: true },
    { to: `${base}/entities`, label: t("ha.tabs.entities") },
    { to: `${base}/automations`, label: t("ha.tabs.automations") },
  ];

  const meta = [
    instance.info?.version ? t("ha.versionShort", { version: instance.info.version }) : null,
    instance.info?.locationName || null,
    t("ha.entityCount", { count: instance.entityCount }),
  ].filter(Boolean);

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <House className="size-5" />
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
            {t("ha.title")}
            {meta.length > 0 && ` · ${meta.join(" · ")}`}
          </p>
        </div>
        <OpenSystemLink instanceId={instanceId} />
      </div>

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
            ? t("ha.unreachableBanner", { message: instance.statusMessage })
            : t("ha.degradedBanner", { message: instance.statusMessage })}
        </div>
      )}

      {entities.data && <HaStatRow entities={entities.data.entities} areaCount={entities.data.areas.length} />}

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


/** HaStatRow condenses the home's live state into four glanceable tiles. */
function HaStatRow({ entities, areaCount }: { entities: HaEntity[]; areaCount: number }) {
  const { t } = useTranslation();
  if (entities.length === 0) return null;
  const lights = entities.filter((e) => e.domain === "light");
  const lightsOn = lights.filter((e) => e.state === "on").length;
  const automations = entities.filter((e) => e.domain === "automation");
  const automationsOn = automations.filter((e) => e.state === "on").length;
  const areas = areaCount;
  return (
    <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
      <MiniStat icon={Boxes} label={t("ha.stats.entities")} value={entities.length} sub={t("ha.stats.areas", { count: areas })} />
      <MiniStat
        icon={Lightbulb}
        label={t("ha.stats.lightsOn")}
        value={`${lightsOn}/${lights.length}`}
        tone={lightsOn > 0 ? "warning" : undefined}
      />
      <MiniStat icon={Workflow} label={t("ha.stats.automations")} value={`${automationsOn}/${automations.length}`} />
      <MiniStat
        icon={AlertTriangle}
        label={t("ha.stats.unavailable")}
        value={entities.filter((e) => e.state === "unavailable").length}
        tone={entities.some((e) => e.state === "unavailable") ? "warning" : "success"}
      />
    </div>
  );
}
