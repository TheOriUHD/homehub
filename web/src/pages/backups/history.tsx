import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { FileClock } from "lucide-react";
import { api, unwrap, type BackupRun } from "@/lib/api";
import { formatBytes, formatDateTime, formatRelative } from "@/lib/utils";
import { Card, CardHeader } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { Tooltip } from "@/ui/tooltip";

const kindTone = { "app-backup": "info", restore: "warning", prune: "neutral" } as const;

/** Locale-neutral compact duration between two run timestamps. */
function runDuration(startedAt: string, finishedAt?: string | null): string {
  if (!finishedAt) return "—";
  const seconds = Math.max(0, (new Date(finishedAt).getTime() - new Date(startedAt).getTime()) / 1000);
  if (seconds < 1) return "<1s";
  if (seconds < 60) return `${Math.round(seconds)}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ${Math.round(seconds % 60)}s`;
  return `${Math.floor(minutes / 60)}h ${minutes % 60}m`;
}

function StatusBadge({ run }: { run: BackupRun }) {
  const { t } = useTranslation();
  if (run.status === "running") {
    return (
      <Badge tone="info">
        <StatusDot tone="info" pulse />
        {t("backups.history.status.running")}
      </Badge>
    );
  }
  if (run.status === "succeeded") {
    return <Badge tone="success">{t("backups.history.status.succeeded")}</Badge>;
  }
  const badge = <Badge tone="danger">{t("backups.history.status.failed")}</Badge>;
  // The failure reason rides on a hover tooltip — the row stays scannable.
  return run.error ? (
    <Tooltip label={run.error}>
      <span className="inline-flex cursor-help">{badge}</span>
    </Tooltip>
  ) : (
    badge
  );
}

export function RunsSection() {
  const { t } = useTranslation();

  const runs = useQuery({
    queryKey: ["backups", "runs"],
    queryFn: async () =>
      unwrap(await api.GET("/backups/runs", { params: { query: { limit: 50 } } })),
    // Runs progress server-side; keep the table fresh while it's on screen.
    refetchInterval: 15_000,
  });

  return (
    <Card>
      <CardHeader title={t("backups.history.title")} description={t("backups.history.intro")} />
      {runs.isLoading ? (
        <SkeletonRows rows={4} className="p-5" />
      ) : runs.isError ? (
        <ErrorState error={runs.error} onRetry={() => void runs.refetch()} />
      ) : (runs.data ?? []).length === 0 ? (
        <EmptyState
          icon={FileClock}
          title={t("backups.history.empty.title")}
          body={t("backups.history.empty.body")}
        />
      ) : (
        <Table>
          <THead>
            <tr>
              <TH>{t("backups.history.columns.kind")}</TH>
              <TH>{t("backups.history.columns.target")}</TH>
              <TH>{t("backups.history.columns.status")}</TH>
              <TH className="hidden sm:table-cell">{t("backups.history.columns.size")}</TH>
              <TH className="hidden md:table-cell">{t("backups.history.columns.duration")}</TH>
              <TH>{t("backups.history.columns.started")}</TH>
            </tr>
          </THead>
          <TBody>
            {runs.data!.map((run) => (
              <TR key={run.id}>
                <TD>
                  <Badge tone={kindTone[run.kind]}>{t(`backups.history.kind.${run.kind}`)}</Badge>
                </TD>
                <TD>
                  <span className="text-fg font-medium">{run.targetName || "—"}</span>
                  {run.filePath && (
                    <span className="text-fg-subtle mt-0.5 hidden max-w-56 truncate font-mono text-xs lg:block" title={run.filePath}>
                      {run.filePath.split("/").pop()}
                    </span>
                  )}
                </TD>
                <TD>
                  <StatusBadge run={run} />
                </TD>
                <TD className="text-fg-muted hidden whitespace-nowrap sm:table-cell">
                  {run.sizeBytes > 0 ? formatBytes(run.sizeBytes) : "—"}
                </TD>
                <TD className="text-fg-muted hidden whitespace-nowrap md:table-cell">
                  {runDuration(run.startedAt, run.finishedAt)}
                </TD>
                <TD className="text-fg-muted whitespace-nowrap" title={formatDateTime(run.startedAt)}>
                  {formatRelative(run.startedAt)}
                </TD>
              </TR>
            ))}
          </TBody>
        </Table>
      )}
    </Card>
  );
}
