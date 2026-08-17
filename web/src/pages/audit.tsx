import { Fragment, useState } from "react";
import { useInfiniteQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Download, FileClock } from "lucide-react";
import { api, unwrap, type AuditEntry } from "@/lib/api";
import { cn, formatDateTime } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";

const outcomeTone = { success: "success", failure: "danger", denied: "warning" } as const;

export function AuditPage() {
  const { t } = useTranslation();

  const [action, setAction] = useState("");
  const [resource, setResource] = useState("");
  const [outcome, setOutcome] = useState<"any" | "success" | "failure" | "denied">("any");
  const [expanded, setExpanded] = useState<number | null>(null);

  const filters = {
    action: action.trim() || undefined,
    resource: resource.trim() || undefined,
    outcome: outcome === "any" ? undefined : outcome,
  };

  const query = useInfiniteQuery({
    queryKey: ["audit", filters],
    queryFn: async ({ pageParam }) =>
      unwrap(
        await api.GET("/audit", {
          params: { query: { ...filters, before: pageParam ?? undefined, limit: 50 } },
        }),
      ),
    initialPageParam: null as number | null,
    getNextPageParam: (last) => last.nextBefore ?? null,
  });

  const entries = query.data?.pages.flatMap((p) => p.entries) ?? [];
  const hasFilters = Boolean(filters.action || filters.resource || filters.outcome);

  const exportUrl = () => {
    const params = new URLSearchParams();
    if (filters.action) params.set("action", filters.action);
    if (filters.resource) params.set("resource", filters.resource);
    if (filters.outcome) params.set("outcome", filters.outcome);
    return `/api/audit/export?${params.toString()}`;
  };

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("audit.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("audit.subtitle")}</p>
        </div>
        <Button variant="secondary" onClick={() => window.open(exportUrl(), "_blank")}>
          <Download className="size-4" />
          {t("audit.export")}
        </Button>
      </div>

      <div className="flex flex-wrap gap-2">
        <Input
          placeholder={t("audit.filter.action")}
          className="max-w-52"
          value={action}
          onChange={(e) => setAction(e.target.value)}
        />
        <Input
          placeholder={t("audit.filter.resource")}
          className="max-w-52 font-mono text-[13px]"
          value={resource}
          onChange={(e) => setResource(e.target.value)}
        />
        <Select value={outcome} onValueChange={(v) => setOutcome(v as typeof outcome)}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="any">{t("audit.filter.outcome")}</SelectItem>
            <SelectItem value="success">{t("audit.outcome.success")}</SelectItem>
            <SelectItem value="failure">{t("audit.outcome.failure")}</SelectItem>
            <SelectItem value="denied">{t("audit.outcome.denied")}</SelectItem>
          </SelectContent>
        </Select>
        {hasFilters && (
          <Button
            variant="ghost"
            onClick={() => {
              setAction("");
              setResource("");
              setOutcome("any");
            }}
          >
            {t("audit.filter.clear")}
          </Button>
        )}
      </div>

      <Card>
        {query.isLoading ? (
          <SkeletonRows rows={6} className="p-5" />
        ) : query.isError ? (
          <ErrorState error={query.error} onRetry={() => void query.refetch()} />
        ) : entries.length === 0 ? (
          <EmptyState icon={FileClock} title={t("audit.empty.title")} body={t("audit.empty.body")} />
        ) : (
          <>
            <Table>
              <THead>
                <tr>
                  <TH>{t("audit.columns.time")}</TH>
                  <TH>{t("audit.columns.actor")}</TH>
                  <TH>{t("audit.columns.action")}</TH>
                  <TH className="hidden md:table-cell">{t("audit.columns.resource")}</TH>
                  <TH>{t("audit.columns.outcome")}</TH>
                  <TH className="hidden lg:table-cell">{t("audit.columns.ip")}</TH>
                </tr>
              </THead>
              <TBody>
                {entries.map((entry) => (
                  <AuditRow
                    key={entry.id}
                    entry={entry}
                    expanded={expanded === entry.id}
                    onToggle={() => setExpanded(expanded === entry.id ? null : entry.id)}
                  />
                ))}
              </TBody>
            </Table>
            {query.hasNextPage && (
              <div className="border-edge flex justify-center border-t p-3">
                <Button variant="ghost" size="sm" loading={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
                  {t("audit.loadMore")}
                </Button>
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  );
}

function AuditRow({
  entry,
  expanded,
  onToggle,
}: {
  entry: AuditEntry;
  expanded: boolean;
  onToggle: () => void;
}) {
  const { t } = useTranslation();
  const hasDetail = entry.detail && Object.keys(entry.detail).length > 0;
  return (
    <Fragment>
      <TR
        interactive={Boolean(hasDetail)}
        onClick={hasDetail ? onToggle : undefined}
        className={cn(expanded && "bg-surface-hover")}
      >
        <TD className="text-fg-muted whitespace-nowrap">{formatDateTime(entry.ts)}</TD>
        <TD>
          <span className="text-fg font-medium">{entry.actorName || "—"}</span>
          {entry.actorKind !== "user" && (
            <Badge tone="neutral" className="ml-1.5">
              {entry.actorKind}
            </Badge>
          )}
        </TD>
        <TD>
          <code className="text-fg font-mono text-xs">{entry.action}</code>
        </TD>
        <TD className="hidden max-w-52 md:table-cell">
          <code className="text-fg-muted block truncate font-mono text-xs">{entry.resource || "—"}</code>
        </TD>
        <TD>
          <Badge tone={outcomeTone[entry.outcome]}>{t(`audit.outcome.${entry.outcome}`)}</Badge>
        </TD>
        <TD className="text-fg-subtle hidden font-mono text-xs lg:table-cell">{entry.ip || "—"}</TD>
      </TR>
      {expanded && hasDetail && (
        <tr className="border-edge bg-bg-subtle/60 border-b">
          <td colSpan={6} className="px-4 py-3">
            <p className="text-fg-subtle mb-1.5 text-xs font-semibold uppercase">{t("audit.detail")}</p>
            <pre className="text-fg-muted overflow-x-auto font-mono text-xs leading-relaxed">
              {JSON.stringify(entry.detail, null, 2)}
            </pre>
          </td>
        </tr>
      )}
    </Fragment>
  );
}
