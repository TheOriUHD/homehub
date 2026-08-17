import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { ListChecks, Plus, Trash2 } from "lucide-react";
import { api, unwrap, isApiError, type DnsListEntry, type DnsListType } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Input } from "@/ui/input";
import { ConfirmDialog } from "@/ui/confirm";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { dnsResource, useDnsInstances } from "./module";

const listSections: DnsListType[] = ["allow", "deny", "allow_regex", "deny_regex"];

export function DnsListsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const instances = useDnsInstances();
  const list = instances.data ?? [];
  const [instanceId, setInstanceId] = useState<string | null>(null);
  const selected = instanceId ?? list[0]?.id;
  const instance = list.find((i) => i.id === selected);
  const caps = instance?.capabilities;
  const canConfigure = instance ? can(dnsResource(instance), "configure") : false;

  const entries = useQuery({
    queryKey: ["dns", selected, "lists"],
    enabled: Boolean(selected),
    queryFn: async () =>
      unwrap(await api.GET("/dns/{id}/lists", { params: { path: { id: selected! } } })),
  });

  // Falls back to "deny" if the selected instance loses the chosen capability
  // (e.g. switching from AdGuard to a Pi-hole without custom rules).
  const [addType, setAddType] = useState<DnsListType>("deny");
  const [domain, setDomain] = useState("");
  const [comment, setComment] = useState("");
  const [removing, setRemoving] = useState<DnsListEntry | null>(null);

  // The set of entry types this instance can hold.
  const typeOptions: DnsListType[] = [
    "allow",
    "deny",
    ...(caps?.regexLists ? (["allow_regex", "deny_regex"] as DnsListType[]) : []),
    ...(caps?.customRules ? (["custom"] as DnsListType[]) : []),
  ];
  const effectiveType: DnsListType = typeOptions.includes(addType) ? addType : "deny";

  const invalidate = () =>
    void queryClient.invalidateQueries({ queryKey: ["dns", selected, "lists"] });

  const add = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/dns/{id}/lists", {
          params: { path: { id: selected! } },
          body: {
            type: effectiveType,
            domain: domain.trim(),
            ...(caps?.listComment && comment.trim() ? { comment: comment.trim() } : {}),
          },
        }),
      ),
    onSuccess: () => {
      toast("success", t("dns.lists.added", { domain: domain.trim() }));
      setDomain("");
      setComment("");
      invalidate();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (entry: DnsListEntry) =>
      unwrap(
        await api.POST("/dns/{id}/lists/remove", {
          params: { path: { id: selected! } },
          body: { type: entry.type, domain: entry.domain, id: entry.id },
        }),
      ),
    onSuccess: (_data, entry) => {
      toast("success", t("dns.lists.removed", { domain: entry.domain }));
      setRemoving(null);
      invalidate();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (instances.isLoading) return <SkeletonRows rows={6} />;

  const grouped = new Map<DnsListType, DnsListEntry[]>();
  for (const entry of entries.data ?? []) {
    const bucket = grouped.get(entry.type) ?? [];
    bucket.push(entry);
    grouped.set(entry.type, bucket);
  }

  const visibleSections = listSections.filter(
    (type) =>
      type === "allow" || type === "deny" || caps?.regexLists || (grouped.get(type)?.length ?? 0) > 0,
  );
  const customEntries = grouped.get("custom") ?? [];
  const showCustom = Boolean(caps?.customRules) && customEntries.length > 0;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-2">
        <Select value={selected} onValueChange={setInstanceId}>
          <SelectTrigger className="w-52" aria-label={t("dns.instance")}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {list.map((row) => (
              <SelectItem key={row.id} value={row.id}>
                {row.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>

      {canConfigure && (
        <Card>
          <CardBody className="flex flex-wrap items-center gap-2">
            <Select value={effectiveType} onValueChange={(v) => setAddType(v as DnsListType)}>
              <SelectTrigger className="w-40">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {typeOptions.map((type) => (
                  <SelectItem key={type} value={type}>
                    {t(`dns.lists.types.${type}`)}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Input
              placeholder={t("dns.lists.domainPlaceholder")}
              className="min-w-48 flex-1 font-mono text-[13px]"
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter" && domain.trim()) add.mutate();
              }}
            />
            {caps?.listComment && (
              <Input
                placeholder={t("dns.lists.commentPlaceholder")}
                className="min-w-40 flex-1"
                value={comment}
                onChange={(e) => setComment(e.target.value)}
              />
            )}
            <Button variant="primary" disabled={!domain.trim()} loading={add.isPending} onClick={() => add.mutate()}>
              <Plus className="size-4" />
              {t("dns.lists.add")}
            </Button>
          </CardBody>
        </Card>
      )}

      {entries.isLoading ? (
        <SkeletonRows rows={6} />
      ) : entries.isError ? (
        <Card>
          <ErrorState error={entries.error} onRetry={() => void entries.refetch()} />
        </Card>
      ) : (entries.data!.length === 0 && !canConfigure) ? (
        <Card>
          <EmptyState icon={ListChecks} title={t("dns.lists.empty.title")} body={t("dns.lists.empty.body")} />
        </Card>
      ) : (
        <div className="grid items-start gap-4 lg:grid-cols-2">
          {visibleSections.map((type) => (
            <ListSection
              key={type}
              type={type}
              entries={grouped.get(type) ?? []}
              canConfigure={canConfigure}
              onRemove={setRemoving}
            />
          ))}
          {showCustom && (
            <ListSection
              type="custom"
              entries={customEntries}
              canConfigure={canConfigure}
              onRemove={setRemoving}
            />
          )}
        </div>
      )}

      <ConfirmDialog
        open={removing !== null}
        onOpenChange={(open) => !open && setRemoving(null)}
        title={removing ? t("dns.lists.removeConfirmTitle", { domain: removing.domain }) : ""}
        body={t("dns.lists.removeConfirmBody", { name: instance?.name ?? "" })}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => removing && remove.mutate(removing)}
      />
    </div>
  );
}

function ListSection({
  type,
  entries,
  canConfigure,
  onRemove,
}: {
  type: DnsListType;
  entries: DnsListEntry[];
  canConfigure: boolean;
  onRemove: (entry: DnsListEntry) => void;
}) {
  const { t } = useTranslation();
  return (
    <Card>
      <CardHeader
        title={t(`dns.lists.sections.${type}`)}
        description={t("dns.lists.entryCount", { count: entries.length })}
      />
      <CardBody className="px-0 py-0">
        {entries.length === 0 ? (
          <p className="text-fg-subtle px-5 py-8 text-center text-[13px]">{t("dns.lists.sectionEmpty")}</p>
        ) : (
          <ul>
            {entries.map((entry) => (
              <li
                key={`${entry.type}-${entry.id}-${entry.domain}`}
                className="border-edge flex items-center gap-3 border-b px-5 py-2.5 last:border-b-0"
              >
                <div className="min-w-0 flex-1">
                  <p className="text-fg truncate font-mono text-[13px]">{entry.domain}</p>
                  {entry.comment && <p className="text-fg-subtle truncate text-xs">{entry.comment}</p>}
                </div>
                {!entry.enabled && <Badge tone="neutral">{t("common.disabled")}</Badge>}
                {canConfigure && (
                  <Tooltip label={t("common.delete")}>
                    <Button
                      variant="ghost"
                      size="icon"
                      aria-label={t("common.delete")}
                      onClick={() => onRemove(entry)}
                    >
                      <Trash2 className="text-danger size-4" />
                    </Button>
                  </Tooltip>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardBody>
    </Card>
  );
}
