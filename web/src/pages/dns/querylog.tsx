import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Globe, RefreshCw } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useDnsInstances } from "./module";

const queryStatusTone = { allowed: "success", blocked: "danger", cached: "neutral" } as const;

export function DnsQueryLogPage() {
  const { t } = useTranslation();
  const instances = useDnsInstances();
  const list = instances.data ?? [];

  const [instanceId, setInstanceId] = useState<string | null>(null);
  const selected = instanceId ?? list[0]?.id;

  const [search, setSearch] = useState("");
  const [client, setClient] = useState("");
  const [status, setStatus] = useState<"all" | "allowed" | "blocked" | "cached">("all");

  const filters = {
    search: search.trim() || undefined,
    client: client.trim() || undefined,
    status: status === "all" ? undefined : status,
  };

  const queries = useQuery({
    queryKey: ["dns", selected, "queries", filters],
    enabled: Boolean(selected),
    queryFn: async () =>
      unwrap(
        await api.GET("/dns/{id}/queries", {
          params: { path: { id: selected! }, query: { ...filters, limit: 300 } },
        }),
      ),
    refetchInterval: 15_000,
  });

  if (instances.isLoading) return <SkeletonRows rows={8} />;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={selected} onValueChange={setInstanceId}>
          <SelectTrigger className="w-52" aria-label={t("dns.instance")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {list.map((instance) => (
              <SelectItem key={instance.id} value={instance.id}>
                {instance.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        <Input
          placeholder={t("dns.queryLog.searchPlaceholder")}
          className="max-w-60"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
        <Input
          placeholder={t("dns.queryLog.clientPlaceholder")}
          className="max-w-52"
          value={client}
          onChange={(e) => setClient(e.target.value)}
        />
        <Select value={status} onValueChange={(v) => setStatus(v as typeof status)}>
          <SelectTrigger className="w-40">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">{t("dns.queryLog.statusAll")}</SelectItem>
            <SelectItem value="allowed">{t("dns.queryLog.status.allowed")}</SelectItem>
            <SelectItem value="blocked">{t("dns.queryLog.status.blocked")}</SelectItem>
            <SelectItem value="cached">{t("dns.queryLog.status.cached")}</SelectItem>
          </SelectContent>
        </Select>
        <Button
          variant="secondary"
          size="sm"
          className="ml-auto"
          loading={queries.isFetching && !queries.isLoading}
          onClick={() => void queries.refetch()}
        >
          <RefreshCw className="size-3.5" />
          {t("dns.queryLog.refresh")}
        </Button>
      </div>

      <Card>
        {queries.isLoading ? (
          <SkeletonRows rows={8} className="p-5" />
        ) : queries.isError ? (
          <ErrorState error={queries.error} onRetry={() => void queries.refetch()} />
        ) : queries.data!.length === 0 ? (
          <EmptyState icon={Globe} title={t("dns.queryLog.empty.title")} body={t("dns.queryLog.empty.body")} />
        ) : (
          <Table>
            <THead>
              <tr>
                <TH>{t("dns.queryLog.columns.time")}</TH>
                <TH>{t("dns.queryLog.columns.domain")}</TH>
                <TH className="hidden sm:table-cell">{t("dns.queryLog.columns.client")}</TH>
                <TH className="hidden md:table-cell">{t("dns.queryLog.columns.type")}</TH>
                <TH>{t("dns.queryLog.columns.status")}</TH>
                <TH className="hidden lg:table-cell">{t("dns.queryLog.columns.reason")}</TH>
              </tr>
            </THead>
            <TBody>
              {queries.data!.map((query, i) => (
                <TR key={`${query.time}-${query.domain}-${i}`}>
                  <TD className="text-fg-muted whitespace-nowrap">{formatRelative(query.time)}</TD>
                  <TD className="text-fg max-w-64 truncate font-mono text-[13px]">{query.domain}</TD>
                  <TD className="text-fg-muted hidden max-w-40 truncate sm:table-cell">
                    {query.clientName || query.client}
                  </TD>
                  <TD className="text-fg-muted hidden whitespace-nowrap md:table-cell">{query.type}</TD>
                  <TD>
                    <Badge tone={queryStatusTone[query.status]}>
                      {t(`dns.queryLog.status.${query.status}`)}
                    </Badge>
                  </TD>
                  <TD className="text-fg-subtle hidden max-w-56 truncate text-xs lg:table-cell">
                    {query.reason || "—"}
                  </TD>
                </TR>
              ))}
            </TBody>
          </Table>
        )}
      </Card>
    </div>
  );
}
