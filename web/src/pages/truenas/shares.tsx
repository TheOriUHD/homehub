import { useMemo } from "react";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Share2 } from "lucide-react";
import type { TruenasShare } from "@/lib/api";
import { Badge } from "@/ui/badge";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useTruenasShares } from "./shared";

/** Product-standard label per share protocol. */
const SHARE_TYPE_LABEL: Record<TruenasShare["type"], string> = {
  smb: "SMB",
  nfs: "NFS",
  iscsi: "iSCSI",
};

const SHARE_TYPE_ORDER: TruenasShare["type"][] = ["smb", "nfs", "iscsi"];

/**
 * TruenasSharesPage lists every SMB, NFS, and iSCSI share in one table, with a
 * protocol column and a read-only enabled badge (HomeHub does not mutate NAS).
 */
export function TruenasSharesPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/truenas/$instanceId" });

  const shares = useTruenasShares(instanceId);

  const sorted = useMemo(() => {
    return (shares.data ?? [])
      .slice()
      .sort(
        (a, b) =>
          SHARE_TYPE_ORDER.indexOf(a.type) - SHARE_TYPE_ORDER.indexOf(b.type) ||
          a.name.localeCompare(b.name),
      );
  }, [shares.data]);

  if (shares.isLoading) return <PageLoader />;
  if (shares.isError) {
    return (
      <Card>
        <ErrorState error={shares.error} onRetry={() => void shares.refetch()} />
      </Card>
    );
  }

  if (sorted.length === 0) {
    return (
      <Card>
        <EmptyState
          icon={Share2}
          title={t("truenas.shares.empty.title")}
          body={t("truenas.shares.empty.body")}
        />
      </Card>
    );
  }

  return (
    <Card className="overflow-hidden">
      <Table>
        <THead>
          <TR>
            <TH>{t("truenas.shares.columns.type")}</TH>
            <TH>{t("truenas.shares.columns.name")}</TH>
            <TH>{t("truenas.shares.columns.path")}</TH>
            <TH>{t("truenas.shares.columns.enabled")}</TH>
            <TH>{t("truenas.shares.columns.comment")}</TH>
          </TR>
        </THead>
        <TBody>
          {sorted.map((share, i) => (
            <TR key={`${share.type}-${share.name}-${i}`}>
              <TD>
                <Badge tone="accent">{SHARE_TYPE_LABEL[share.type]}</Badge>
              </TD>
              <TD className="text-fg font-medium">{share.name}</TD>
              <TD className="text-fg-subtle font-mono text-xs">{share.path || "—"}</TD>
              <TD>
                <Badge tone={share.enabled ? "success" : "neutral"}>
                  {share.enabled ? t("truenas.shares.on") : t("truenas.shares.off")}
                </Badge>
              </TD>
              <TD className="text-fg-muted">{share.comment || "—"}</TD>
            </TR>
          ))}
        </TBody>
      </Table>
    </Card>
  );
}
