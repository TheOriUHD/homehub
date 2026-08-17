import { useMemo } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { FolderTree } from "lucide-react";
import type { TruenasDataset } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { datasetDepth, datasetRootPool, useTruenasDatasets } from "./shared";

/**
 * TruenasDatasetsPage lists datasets and zvols, grouped by their root pool
 * and indented by nesting depth so the tree structure reads at a glance.
 */
export function TruenasDatasetsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const datasets = useTruenasDatasets(instanceId);

  const groups = useMemo(() => {
    const byPool = new Map<string, TruenasDataset[]>();
    for (const ds of datasets.data ?? []) {
      const pool = datasetRootPool(ds.name);
      if (!byPool.has(pool)) byPool.set(pool, []);
      byPool.get(pool)!.push(ds);
    }
    return [...byPool.entries()]
      .map(([pool, rows]) => ({
        pool,
        rows: rows.slice().sort((a, b) => a.name.localeCompare(b.name)),
      }))
      .sort((a, b) => a.pool.localeCompare(b.pool));
  }, [datasets.data]);

  if (datasets.isLoading) return <PageLoader />;
  if (datasets.isError) {
    return (
      <Card>
        <ErrorState error={datasets.error} onRetry={() => void datasets.refetch()} />
      </Card>
    );
  }

  if (groups.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={FolderTree}
          title={t("truenas.datasets.empty.title")}
          body={t("truenas.datasets.empty.body")}
        />
      </Card>
    );
  }

  return (
    <div className="space-y-5">
      {groups.map((group) => (
        <section key={group.pool} className="space-y-2.5">
          <h2 className="text-fg text-[15px] font-semibold">{group.pool}</h2>
          <Card className="overflow-hidden">
            <Table>
              <THead>
                <TR>
                  <TH>{t("truenas.datasets.columns.name")}</TH>
                  <TH>{t("truenas.datasets.columns.type")}</TH>
                  <TH className="text-right">{t("truenas.datasets.columns.used")}</TH>
                  <TH className="text-right">{t("truenas.datasets.columns.available")}</TH>
                  <TH>{t("truenas.datasets.columns.compression")}</TH>
                  <TH>{t("truenas.datasets.columns.mountpoint")}</TH>
                </TR>
              </THead>
              <TBody>
                {group.rows.map((ds) => {
                  const depth = datasetDepth(ds.name);
                  const leaf = ds.name.split("/").slice(-1)[0];
                  const isVolume = ds.type.toUpperCase() === "VOLUME";
                  return (
                    <TR key={ds.name}>
                      <TD className="text-fg font-medium">
                        <span style={{ paddingLeft: `${depth * 1.25}rem` }}>{leaf}</span>
                      </TD>
                      <TD>
                        <Badge tone={isVolume ? "accent" : "neutral"}>{ds.type}</Badge>
                      </TD>
                      <TD className="text-fg-muted text-right tabular-nums">{formatBytes(ds.usedBytes)}</TD>
                      <TD className="text-fg-muted text-right tabular-nums">
                        {formatBytes(ds.availableBytes)}
                      </TD>
                      <TD>
                        {ds.compression ? (
                          <Badge tone="neutral">{ds.compression}</Badge>
                        ) : (
                          <span className="text-fg-subtle">—</span>
                        )}
                      </TD>
                      <TD className="text-fg-subtle font-mono text-xs">{ds.mountpoint || "—"}</TD>
                    </TR>
                  );
                })}
              </TBody>
            </Table>
          </Card>
        </section>
      ))}
    </div>
  );
}
