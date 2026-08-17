import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { AlertTriangle, HardDrive } from "lucide-react";
import type { TruenasAlert, TruenasDisk, TruenasPool } from "@/lib/api";
import { cn, formatBytes, formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import {
  alertTone,
  capacityBarClass,
  capacityTone,
  diskTempWarn,
  poolHealthTone,
  useTruenasAlerts,
  useTruenasOverview,
} from "./shared";

/**
 * TruenasOverviewPage is the module's default tab: a capacity card per pool,
 * the disk inventory, and any active alerts.
 */
export function TruenasOverviewPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const overview = useTruenasOverview(instanceId);
  const alerts = useTruenasAlerts(instanceId);

  if (overview.isLoading) return <PageLoader />;
  if (overview.isError) {
    return (
      <Card>
        <ErrorState error={overview.error} onRetry={() => void overview.refetch()} />
      </Card>
    );
  }

  const { pools, disks } = overview.data!;
  const activeAlerts = (alerts.data ?? []).filter((a) => !a.dismissed);

  return (
    <div className="space-y-5">
      {activeAlerts.length > 0 && <AlertsSection alerts={activeAlerts} />}

      {pools.length === 0 ? (
        <Card>
          <EmptyState
            icon={HardDrive}
            title={t("truenas.overview.noPools.title")}
            body={t("truenas.overview.noPools.body")}
          />
        </Card>
      ) : (
        <div className="grid items-start gap-3 md:grid-cols-2">
          {pools.map((pool) => (
            <PoolCard key={pool.name} pool={pool} />
          ))}
        </div>
      )}

      <section className="space-y-2.5">
        <h2 className="text-fg text-[15px] font-semibold">{t("truenas.overview.disks")}</h2>
        <Card className="overflow-hidden">
          {disks.length === 0 ? (
            <EmptyState icon={HardDrive} title={t("truenas.overview.noDisks")} />
          ) : (
            <Table>
              <THead>
                <TR>
                  <TH>{t("truenas.overview.diskColumns.name")}</TH>
                  <TH>{t("truenas.overview.diskColumns.model")}</TH>
                  <TH>{t("truenas.overview.diskColumns.serial")}</TH>
                  <TH>{t("truenas.overview.diskColumns.type")}</TH>
                  <TH className="text-right">{t("truenas.overview.diskColumns.size")}</TH>
                  <TH className="text-right">{t("truenas.overview.diskColumns.temperature")}</TH>
                  <TH>{t("truenas.overview.diskColumns.pool")}</TH>
                </TR>
              </THead>
              <TBody>
                {disks.map((disk) => (
                  <DiskRow key={disk.name} disk={disk} />
                ))}
              </TBody>
            </Table>
          )}
        </Card>
      </section>
    </div>
  );
}

const capacityTextClass: Record<ReturnType<typeof capacityTone>, string> = {
  success: "text-success-soft-fg",
  warning: "text-warning-soft-fg",
  danger: "text-danger-soft-fg",
};

function PoolCard({ pool }: { pool: TruenasPool }) {
  const { t } = useTranslation();
  const pct = Math.min(Math.max(pool.usedPct, 0), 100);

  return (
    <Card className="p-4">
      <div className="flex items-center gap-2">
        <p className="text-fg truncate text-sm font-semibold">{pool.name}</p>
        <div className="ml-auto flex items-center gap-1.5">
          <Badge tone={poolHealthTone(pool)}>{pool.status}</Badge>
          <Badge tone={pool.healthy ? "success" : poolHealthTone(pool)}>
            {pool.healthy ? t("truenas.overview.healthy") : t("truenas.overview.unhealthy")}
          </Badge>
        </div>
      </div>

      <div className="mt-3">
        <div className="bg-bg-subtle h-2 w-full overflow-hidden rounded-full">
          <div
            className={cn("h-full rounded-full transition-[width]", capacityBarClass(pool.usedPct))}
            style={{ width: `${pct}%` }}
          />
        </div>
        <div className="text-fg-muted mt-1.5 flex items-center justify-between text-[13px]">
          <span className={cn("font-medium", capacityTextClass[capacityTone(pool.usedPct)])}>
            {t("truenas.overview.usedOfTotal", {
              used: formatBytes(pool.allocatedBytes),
              total: formatBytes(pool.sizeBytes),
            })}
          </span>
          <span>{t("truenas.overview.percentFull", { pct: Math.round(pool.usedPct) })}</span>
        </div>
      </div>

      <div className="text-fg-subtle mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-xs">
        <span>{t("truenas.overview.free", { size: formatBytes(pool.freeBytes) })}</span>
        {pool.fragmentation !== undefined && (
          <span>{t("truenas.overview.fragmentation", { pct: pool.fragmentation })}</span>
        )}
      </div>
    </Card>
  );
}

function DiskRow({ disk }: { disk: TruenasDisk }) {
  const { t } = useTranslation();
  const isSsd = /ssd|nvme/i.test(disk.type);
  return (
    <TR>
      <TD className="text-fg font-medium">{disk.name}</TD>
      <TD className="text-fg-muted">{disk.model || "—"}</TD>
      <TD className="text-fg-subtle font-mono text-xs">{disk.serial || "—"}</TD>
      <TD>
        <Badge tone={isSsd ? "info" : "neutral"}>{disk.type}</Badge>
      </TD>
      <TD className="text-fg-muted text-right tabular-nums">{formatBytes(disk.size)}</TD>
      <TD className="text-right tabular-nums">
        {typeof disk.temperature === "number" ? (
          <span className={diskTempWarn(disk.temperature) ? "text-warning-soft-fg font-medium" : "text-fg-muted"}>
            {t("truenas.overview.celsius", { value: disk.temperature })}
          </span>
        ) : (
          <span className="text-fg-subtle">—</span>
        )}
      </TD>
      <TD className="text-fg-muted">{disk.pool || "—"}</TD>
    </TR>
  );
}

function AlertsSection({ alerts }: { alerts: TruenasAlert[] }) {
  const { t } = useTranslation();
  return (
    <section className="space-y-2">
      <h2 className="text-fg flex items-center gap-2 text-[15px] font-semibold">
        <AlertTriangle className="text-fg-subtle size-4" />
        {t("truenas.overview.alerts")}
        <span className="text-fg-subtle text-[13px] font-normal">({alerts.length})</span>
      </h2>
      <div className="space-y-2">
        {alerts.map((alert, i) => {
          const tone = alertTone(alert.level);
          return (
            <div
              key={`${alert.createdAt ?? ""}-${i}`}
              className={cn(
                "flex flex-wrap items-center gap-x-3 gap-y-1 rounded-lg px-4 py-2.5 text-[13px]",
                tone === "danger" && "bg-danger-soft text-danger-soft-fg",
                tone === "warning" && "bg-warning-soft text-warning-soft-fg",
                tone === "info" && "bg-info-soft text-info-soft-fg",
                tone === "neutral" && "bg-bg-subtle text-fg-muted",
              )}
              role="alert"
            >
              <Badge tone={tone}>{alert.level}</Badge>
              <span className="min-w-0 flex-1">{alert.message}</span>
              {alert.createdAt && (
                <span className="text-fg-subtle shrink-0 text-xs">{formatRelative(alert.createdAt)}</span>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}
