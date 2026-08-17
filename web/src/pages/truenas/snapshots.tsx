import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Camera } from "lucide-react";
import { formatBytes, formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useTruenasSnapshots } from "./shared";

/**
 * TruenasSnapshotsPage lists ZFS snapshots newest-first: full name, source
 * dataset, referenced space, and relative creation time.
 */
export function TruenasSnapshotsPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const snapshots = useTruenasSnapshots(instanceId);

  if (snapshots.isLoading) return <PageLoader />;
  if (snapshots.isError) {
    return (
      <Card>
        <ErrorState error={snapshots.error} onRetry={() => void snapshots.refetch()} />
      </Card>
    );
  }

  const rows = snapshots.data ?? [];
  if (rows.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Camera}
          title={t("truenas.snapshots.empty.title")}
          body={t("truenas.snapshots.empty.body")}
        />
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden">
      <Table>
        <THead>
          <TR>
            <TH>{t("truenas.snapshots.columns.name")}</TH>
            <TH>{t("truenas.snapshots.columns.dataset")}</TH>
            <TH className="text-right">{t("truenas.snapshots.columns.used")}</TH>
            <TH className="text-right">{t("truenas.snapshots.columns.created")}</TH>
          </TR>
        </THead>
        <TBody>
          {rows.map((snap, i) => (
            <TR key={`${snap.name}-${i}`}>
              <TD className="text-fg font-mono text-xs">{snap.name}</TD>
              <TD className="text-fg-muted">{snap.dataset}</TD>
              <TD className="text-fg-muted text-right tabular-nums">
                {typeof snap.usedBytes === "number" ? formatBytes(snap.usedBytes) : "—"}
              </TD>
              <TD className="text-fg-subtle text-right">{formatRelative(snap.createdAt)}</TD>
            </TR>
          ))}
        </TBody>
      </Table>
    </Card>
  );
}
