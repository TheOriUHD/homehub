import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Info, MonitorSmartphone, Pencil, Plus, Trash2 } from "lucide-react";
import { api, unwrap, isApiError, type DnsClient, type DnsGroup } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Badge } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input, Textarea } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";
import { dnsResource, useDnsInstances } from "./module";

export function DnsClientsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const instances = useDnsInstances();
  const list = instances.data ?? [];
  const [instanceId, setInstanceId] = useState<string | null>(null);
  const selected = instanceId ?? list[0]?.id;
  const instance = list.find((i) => i.id === selected);
  const canConfigure = instance ? can(dnsResource(instance), "configure") : false;

  const clients = useQuery({
    queryKey: ["dns", selected, "clients"],
    enabled: Boolean(selected),
    queryFn: async () =>
      unwrap(await api.GET("/dns/{id}/clients", { params: { path: { id: selected! } } })),
  });

  const [editing, setEditing] = useState<DnsClient | null>(null);
  const [adding, setAdding] = useState(false);
  const [deletingClient, setDeletingClient] = useState<DnsClient | null>(null);
  const [deletingGroup, setDeletingGroup] = useState<DnsGroup | null>(null);

  const invalidate = () =>
    void queryClient.invalidateQueries({ queryKey: ["dns", selected, "clients"] });

  const removeClient = useMutation({
    mutationFn: async (client: DnsClient) =>
      unwrap(
        await api.POST("/dns/{id}/clients/delete", {
          params: { path: { id: selected! } },
          body: { id: client.id },
        }),
      ),
    onSuccess: () => {
      setDeletingClient(null);
      invalidate();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const toggleGroup = useMutation({
    mutationFn: async (group: DnsGroup) =>
      unwrap(
        await api.POST("/dns/{id}/groups", {
          params: { path: { id: selected! } },
          body: { ...group, enabled: !group.enabled },
        }),
      ),
    onSuccess: invalidate,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const removeGroup = useMutation({
    mutationFn: async (group: DnsGroup) =>
      unwrap(
        await api.POST("/dns/{id}/groups/delete", {
          params: { path: { id: selected! } },
          body: { id: group.id },
        }),
      ),
    onSuccess: () => {
      setDeletingGroup(null);
      invalidate();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (instances.isLoading) return <SkeletonRows rows={6} />;

  const data = clients.data;
  const caps = data?.capabilities ?? instance?.capabilities;
  const groups = data?.groups ?? [];
  const canClients = canConfigure && Boolean(caps?.clientCrud);
  const canGroups = canConfigure && Boolean(caps?.groupCrud);
  const readOnlyPihole = instance?.type === "pihole" && caps && !caps.clientCrud && !caps.groupCrud;
  const groupName = (id: string) => groups.find((g) => g.id === id)?.name ?? id;
  const clientLabel = (client: DnsClient) =>
    client.name || client.identifiers[0] || t("dns.clients.unnamed");

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

      {clients.isLoading ? (
        <SkeletonRows rows={6} />
      ) : clients.isError ? (
        <Card>
          <ErrorState error={clients.error} onRetry={() => void clients.refetch()} />
        </Card>
      ) : (
        <>
          {readOnlyPihole && (
            <div className="bg-info-soft text-info-soft-fg flex items-start gap-2.5 rounded-lg px-4 py-3 text-[13px]">
              <Info className="mt-0.5 size-4 shrink-0" />
              {t("dns.clients.readOnlyNote")}
            </div>
          )}

          <Card>
            <CardHeader
              title={t("dns.clients.title")}
              description={t("dns.clients.count", { count: data!.clients.length })}
              actions={
                canClients ? (
                  <Button variant="primary" size="sm" onClick={() => setAdding(true)}>
                    <Plus className="size-3.5" />
                    {t("dns.clients.add")}
                  </Button>
                ) : undefined
              }
            />
            <CardBody className="px-0 py-0">
              {data!.clients.length === 0 ? (
                <EmptyState
                  icon={MonitorSmartphone}
                  title={t("dns.clients.empty.title")}
                  body={t("dns.clients.empty.body")}
                />
              ) : (
                <Table>
                  <THead>
                    <tr>
                      <TH>{t("dns.clients.columns.client")}</TH>
                      <TH className="hidden md:table-cell">{t("dns.clients.columns.identifiers")}</TH>
                      <TH className="hidden sm:table-cell">{t("dns.clients.columns.groups")}</TH>
                      {canClients && <TH className="w-20" />}
                    </tr>
                  </THead>
                  <TBody>
                    {data!.clients.map((client) => (
                      <TR key={client.id}>
                        <TD>
                          <p className="text-fg max-w-56 truncate font-medium">{clientLabel(client)}</p>
                        </TD>
                        <TD className="hidden md:table-cell">
                          <span className="flex max-w-72 flex-wrap gap-1">
                            {client.identifiers.map((identifier) => (
                              <code
                                key={identifier}
                                className="bg-bg-subtle border-edge text-fg-muted rounded border px-1.5 py-0.5 font-mono text-xs"
                              >
                                {identifier}
                              </code>
                            ))}
                          </span>
                        </TD>
                        <TD className="hidden sm:table-cell">
                          <span className="flex max-w-64 flex-wrap gap-1">
                            {client.groupIds.length === 0
                              ? "—"
                              : client.groupIds.map((id) => (
                                  <Badge key={id} tone="accent">
                                    {groupName(id)}
                                  </Badge>
                                ))}
                          </span>
                        </TD>
                        {canClients && (
                          <TD>
                            <span className="flex justify-end gap-1">
                              <Tooltip label={t("common.edit")}>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  aria-label={t("common.edit")}
                                  onClick={() => setEditing(client)}
                                >
                                  <Pencil className="size-4" />
                                </Button>
                              </Tooltip>
                              <Tooltip label={t("common.delete")}>
                                <Button
                                  variant="ghost"
                                  size="icon"
                                  aria-label={t("common.delete")}
                                  onClick={() => setDeletingClient(client)}
                                >
                                  <Trash2 className="text-danger size-4" />
                                </Button>
                              </Tooltip>
                            </span>
                          </TD>
                        )}
                      </TR>
                    ))}
                  </TBody>
                </Table>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader
              title={instance?.type === "adguard" ? t("dns.clients.tagsTitle") : t("dns.clients.groupsTitle")}
              description={
                caps?.groupCrud
                  ? undefined
                  : instance?.type === "adguard"
                    ? t("dns.clients.tagsHint")
                    : undefined
              }
            />
            <CardBody className={caps?.groupCrud ? "px-0 py-0" : undefined}>
              {groups.length === 0 ? (
                <p className={`text-fg-subtle text-center text-[13px] ${caps?.groupCrud ? "px-5 py-8" : "py-4"}`}>
                  {t("dns.clients.groupsEmpty")}
                </p>
              ) : caps?.groupCrud ? (
                <ul>
                  {groups.map((group) => (
                    <li
                      key={group.id}
                      className="border-edge flex items-center gap-3 border-b px-5 py-3 last:border-b-0"
                    >
                      <p className="text-fg min-w-0 flex-1 truncate text-sm font-medium">{group.name}</p>
                      {canGroups ? (
                        <>
                          <Switch
                            checked={group.enabled}
                            disabled={toggleGroup.isPending}
                            onCheckedChange={() => toggleGroup.mutate(group)}
                            aria-label={t("dns.clients.groupEnabled", { name: group.name })}
                          />
                          <Tooltip label={t("common.delete")}>
                            <Button
                              variant="ghost"
                              size="icon"
                              aria-label={t("common.delete")}
                              onClick={() => setDeletingGroup(group)}
                            >
                              <Trash2 className="text-danger size-4" />
                            </Button>
                          </Tooltip>
                        </>
                      ) : (
                        <Badge tone={group.enabled ? "success" : "neutral"}>
                          {group.enabled ? t("common.enabled") : t("common.disabled")}
                        </Badge>
                      )}
                    </li>
                  ))}
                </ul>
              ) : (
                <div className="flex flex-wrap gap-1.5">
                  {groups.map((group) => (
                    <Badge key={group.id} tone="neutral">
                      {group.name}
                    </Badge>
                  ))}
                </div>
              )}
            </CardBody>
          </Card>
        </>
      )}

      {(adding || editing) && selected && (
        <ClientDialog
          instanceId={selected}
          groups={groups}
          client={editing}
          onClose={() => {
            setAdding(false);
            setEditing(null);
          }}
        />
      )}

      <ConfirmDialog
        open={deletingClient !== null}
        onOpenChange={(open) => !open && setDeletingClient(null)}
        title={deletingClient ? t("dns.clients.deleteConfirmTitle", { name: clientLabel(deletingClient) }) : ""}
        body={t("dns.clients.deleteConfirmBody", { instance: instance?.name ?? "" })}
        confirmLabel={t("common.delete")}
        loading={removeClient.isPending}
        onConfirm={() => deletingClient && removeClient.mutate(deletingClient)}
      />
      <ConfirmDialog
        open={deletingGroup !== null}
        onOpenChange={(open) => !open && setDeletingGroup(null)}
        title={deletingGroup ? t("dns.clients.groupDeleteConfirmTitle", { name: deletingGroup.name }) : ""}
        body={t("dns.clients.groupDeleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={removeGroup.isPending}
        onConfirm={() => deletingGroup && removeGroup.mutate(deletingGroup)}
      />
    </div>
  );
}

function ClientDialog({
  instanceId,
  groups,
  client,
  onClose,
}: {
  instanceId: string;
  groups: DnsGroup[];
  client: DnsClient | null;
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [name, setName] = useState(client?.name ?? "");
  const [identifiers, setIdentifiers] = useState(client?.identifiers.join("\n") ?? "");
  const [groupIds, setGroupIds] = useState<Set<string>>(new Set(client?.groupIds ?? []));

  const parsedIdentifiers = identifiers
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/dns/{id}/clients", {
          params: { path: { id: instanceId } },
          body: {
            id: client?.id ?? "",
            name: name.trim(),
            identifiers: parsedIdentifiers,
            groupIds: [...groupIds],
          },
        }),
      ),
    onSuccess: () => {
      toast("success", t("dns.clients.saved"));
      void queryClient.invalidateQueries({ queryKey: ["dns", instanceId, "clients"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={
          client
            ? t("dns.clients.editorTitleEdit", {
                name: client.name || client.identifiers[0] || t("dns.clients.unnamed"),
              })
            : t("dns.clients.editorTitleNew")
        }
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button
              variant="primary"
              disabled={parsedIdentifiers.length === 0}
              loading={save.isPending}
              onClick={() => save.mutate()}
            >
              {t("common.save")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("dns.clients.name")} optional={t("common.optional")}>
            {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} />}
          </Field>
          <Field label={t("dns.clients.identifiers")} hint={t("dns.clients.identifiersHint")}>
            {(p) => (
              <Textarea
                {...p}
                className="font-mono text-[13px]"
                rows={4}
                placeholder={"192.168.1.42\naa:bb:cc:dd:ee:ff"}
                value={identifiers}
                onChange={(e) => setIdentifiers(e.target.value)}
              />
            )}
          </Field>
          {groups.length > 0 && (
            <div className="space-y-2">
              <p className="text-fg text-[13px] font-medium">{t("dns.clients.groups")}</p>
              {groups.map((group) => (
                <Checkbox
                  key={group.id}
                  checked={groupIds.has(group.id)}
                  onCheckedChange={(checked) =>
                    setGroupIds((prev) => {
                      const next = new Set(prev);
                      if (checked) next.add(group.id);
                      else next.delete(group.id);
                      return next;
                    })
                  }
                  label={group.name}
                />
              ))}
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
