import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Database } from "lucide-react";
import type { DbInstance } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { statusTone } from "@/lib/systems";
import { formatDuration } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { engineBadgeLabel, useDbInstances } from "./shared";

/**
 * DbLandingPage lists every database instance the user may see — MySQL,
 * MariaDB, and MongoDB side by side — each card linking into its module.
 */
export function DbLandingPage() {
  const { t } = useTranslation();
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

  const list = instances.data!;
  if (list.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Database}
          title={t("db.empty.title")}
          body={t("db.empty.body")}
          action={
            can("system/integrations", "configure") ? (
              <Link to="/systems" className="text-accent text-[13px] font-medium hover:underline">
                {t("db.empty.action")} →
              </Link>
            ) : undefined
          }
        />
      </Card>
    );
  }

  const online = list.filter((i) => i.status === "connected").length;

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Database className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("db.title")}</h1>
          <p className="text-fg-muted text-sm">
            {t("db.instancesSummary", { online, total: list.length })}
          </p>
        </div>
      </div>

      <div className="grid items-start gap-3 md:grid-cols-2">
        {list.map((instance) => (
          <InstanceCard key={instance.id} instance={instance} />
        ))}
      </div>
    </div>
  );
}

function InstanceCard({ instance }: { instance: DbInstance }) {
  const { t } = useTranslation();
  const stats = instance.stats;

  const chips: string[] = [];
  if (stats) {
    if (typeof stats.connectionsCurrent === "number" && typeof stats.connectionsMax === "number") {
      chips.push(
        t("db.chips.connections", {
          current: stats.connectionsCurrent.toLocaleString(),
          max: stats.connectionsMax.toLocaleString(),
        }),
      );
    }
    if (typeof stats.opsPerSec === "number") {
      chips.push(t("db.chips.ops", { ops: stats.opsPerSec.toFixed(1) }));
    }
    if (typeof stats.uptimeSeconds === "number") {
      chips.push(t("db.chips.uptime", { uptime: formatDuration(stats.uptimeSeconds) }));
    }
    if (instance.engine === "mysql" && typeof stats.slowQueriesTotal === "number") {
      chips.push(t("db.chips.slowQueries", { count: stats.slowQueriesTotal }));
    }
    if (instance.engine === "mongodb" && typeof stats.memResidentMb === "number") {
      chips.push(t("db.chips.memResident", { mb: stats.memResidentMb.toLocaleString() }));
    }
  }

  return (
    <Link to="/db/$instanceId" params={{ instanceId: instance.id }} className="block">
      <Card className="hover:border-accent p-4 transition-colors">
        <p className="text-fg flex items-center gap-2 text-sm font-semibold">
          <StatusDot
            tone={statusTone[instance.status]}
            pulse={instance.status !== "connected"}
          />
          <span className="truncate">{instance.name}</span>
          <Badge tone="neutral" className="ml-auto">
            {engineBadgeLabel(instance)}
          </Badge>
        </p>
        {instance.status !== "connected" && (
          <p className="text-fg-subtle mt-1.5 text-xs">
            {t(`integrations.status.${instance.status}`)}
            {instance.statusMessage && (
              <span className="text-danger-soft-fg"> · {instance.statusMessage}</span>
            )}
          </p>
        )}
        {chips.length > 0 && (
          <div className="mt-3 flex flex-wrap items-center gap-1.5">
            {chips.map((chip) => (
              <Badge key={chip} tone="neutral">
                {chip}
              </Badge>
            ))}
          </div>
        )}
      </Card>
    </Link>
  );
}
