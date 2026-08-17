import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { KeyRound, Plus, Trash2 } from "lucide-react";
import { api, isApiError, unwrap, type TsCreatedKey, type TsKey } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { formatRelative } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Checkbox, CopyButton, PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { tsResource } from "./shared";

export function TsKeysPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ts/$instanceId/keys" });
  const { can } = useAuth();
  const queryClient = useQueryClient();
  const configurable = can(tsResource(instanceId), "configure");
  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<TsCreatedKey | null>(null);

  const keys = useQuery({
    queryKey: ["ts", instanceId, "keys"],
    queryFn: async () => unwrap(await api.GET("/ts/{id}/keys", { params: { path: { id: instanceId } } })),
  });

  if (keys.isLoading) return <PageLoader />;
  if (keys.isError) {
    return (
      <Card>
        <ErrorState error={keys.error} onRetry={() => void keys.refetch()} />
      </Card>
    );
  }

  const rows = keys.data ?? [];

  return (
    <div className="space-y-4">
      {configurable && (
        <div className="flex justify-end">
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Plus className="size-3.5" />
            {t("ts.keys.create")}
          </Button>
        </div>
      )}

      {rows.length === 0 ? (
        <Card>
          <EmptyState icon={KeyRound} title={t("ts.keys.emptyTitle")} body={t("ts.keys.emptyBody")} />
        </Card>
      ) : (
        <Card>
          <div className="overflow-x-auto">
            <Table>
              <THead>
                <TR>
                  <TH>{t("ts.keys.description")}</TH>
                  <TH>{t("ts.keys.properties")}</TH>
                  <TH>{t("ts.keys.createdCol")}</TH>
                  <TH>{t("ts.keys.expires")}</TH>
                  <TH aria-label={t("common.actions")} />
                </TR>
              </THead>
              <TBody>
                {rows.map((k) => (
                  <KeyRow key={k.id} instanceId={instanceId} row={k} configurable={configurable} />
                ))}
              </TBody>
            </Table>
          </div>
        </Card>
      )}

      {createOpen && (
        <CreateKeyDialog
          instanceId={instanceId}
          onClose={() => setCreateOpen(false)}
          onCreated={(key) => {
            setCreateOpen(false);
            setCreated(key);
            void queryClient.invalidateQueries({ queryKey: ["ts", instanceId, "keys"] });
          }}
        />
      )}
      {created && <CreatedKeyDialog createdKey={created} onClose={() => setCreated(null)} />}
    </div>
  );
}

function KeyRow({
  instanceId,
  row,
  configurable,
}: {
  instanceId: string;
  row: TsKey;
  configurable: boolean;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [confirmRevoke, setConfirmRevoke] = useState(false);

  const revoke = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.DELETE("/ts/{id}/keys/{keyId}", {
          params: { path: { id: instanceId, keyId: row.id } },
        }),
      );
    },
    onSuccess: () => {
      setConfirmRevoke(false);
      void queryClient.invalidateQueries({ queryKey: ["ts", instanceId, "keys"] });
    },
    onError: (err) =>
      toast("error", t("ts.keys.revokeFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  const revoked = Boolean(row.revoked);

  return (
    <>
      <TR>
        <TD>
          <div className="flex items-center gap-2">
            <KeyRound className="text-fg-muted size-4 shrink-0" />
            <div className="min-w-0">
              <p className="text-fg truncate font-medium">{row.description || t("ts.keys.noDescription")}</p>
              <p className="text-fg-muted truncate font-mono text-[11.5px]">{row.id}</p>
            </div>
            {revoked ? (
              <Badge tone="danger">{t("ts.keys.revoked")}</Badge>
            ) : row.invalid ? (
              <Badge tone="neutral">{t("ts.keys.expired")}</Badge>
            ) : null}
          </div>
        </TD>
        <TD>
          <span className="flex flex-wrap gap-1">
            {row.reusable && <Badge tone="info">{t("ts.keys.reusable")}</Badge>}
            {row.ephemeral && <Badge tone="neutral">{t("ts.keys.ephemeral")}</Badge>}
            {row.preauthorized && <Badge tone="success">{t("ts.keys.preauthorized")}</Badge>}
            {row.tags.map((tag) => (
              <Badge key={tag} tone="neutral">
                {tag.replace(/^tag:/, "")}
              </Badge>
            ))}
          </span>
        </TD>
        <TD>
          <span className="text-fg-muted text-[13px]">{row.created ? formatRelative(row.created) : "—"}</span>
        </TD>
        <TD>
          <span className="text-fg-muted text-[13px]">{row.expires ? formatRelative(row.expires) : "—"}</span>
        </TD>
        <TD>
          {configurable && !revoked && !row.invalid && (
            <Button size="sm" variant="ghost" onClick={() => setConfirmRevoke(true)} aria-label={t("ts.keys.revoke")}>
              <Trash2 className="size-4" />
            </Button>
          )}
        </TD>
      </TR>
      <ConfirmDialog
        open={confirmRevoke}
        onOpenChange={setConfirmRevoke}
        title={t("ts.keys.revokeTitle")}
        body={t("ts.keys.revokeBody", { name: row.description || row.id })}
        confirmLabel={t("ts.keys.revoke")}
        loading={revoke.isPending}
        onConfirm={() => revoke.mutate()}
      />
    </>
  );
}

function CreateKeyDialog({
  instanceId,
  onClose,
  onCreated,
}: {
  instanceId: string;
  onClose: () => void;
  onCreated: (key: TsCreatedKey) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const [description, setDescription] = useState("");
  const [expiryDays, setExpiryDays] = useState("90");
  const [reusable, setReusable] = useState(false);
  const [ephemeral, setEphemeral] = useState(false);
  const [preauthorized, setPreauthorized] = useState(false);

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/ts/{id}/keys", {
          params: { path: { id: instanceId } },
          body: {
            description: description.trim() || undefined,
            expiryDays: Math.max(1, Math.min(365, Number(expiryDays) || 90)),
            reusable,
            ephemeral,
            preauthorized,
          },
        }),
      ),
    onSuccess: onCreated,
    onError: (err) =>
      toast("error", t("ts.keys.createFailed"), isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("ts.keys.createTitle")}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={create.isPending} onClick={() => create.mutate()}>
              {t("ts.keys.create")}
            </Button>
          </>
        }
      >
        <div className="space-y-4">
          <Field label={t("ts.keys.description")}>
            {(p) => (
              <Input
                {...p}
                autoFocus
                placeholder={t("ts.keys.descriptionPlaceholder")}
                value={description}
                onChange={(e) => setDescription(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("ts.keys.expiryDays")} hint={t("ts.keys.expiryHint")}>
            {(p) => (
              <Input
                {...p}
                type="number"
                min={1}
                max={365}
                value={expiryDays}
                onChange={(e) => setExpiryDays(e.target.value)}
              />
            )}
          </Field>
          <Checkbox checked={reusable} onCheckedChange={setReusable} label={t("ts.keys.reusableLabel")} hint={t("ts.keys.reusableHint")} />
          <Checkbox checked={ephemeral} onCheckedChange={setEphemeral} label={t("ts.keys.ephemeralLabel")} hint={t("ts.keys.ephemeralHint")} />
          <Checkbox
            checked={preauthorized}
            onCheckedChange={setPreauthorized}
            label={t("ts.keys.preauthorizedLabel")}
            hint={t("ts.keys.preauthorizedHint")}
          />
        </div>
      </DialogContent>
    </Dialog>
  );
}

function CreatedKeyDialog({ createdKey, onClose }: { createdKey: TsCreatedKey; onClose: () => void }) {
  const { t } = useTranslation();
  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("ts.keys.createdTitle")}
        footer={
          <Button variant="primary" onClick={onClose}>
            {t("common.done")}
          </Button>
        }
      >
        <div className="space-y-3">
          <p className="text-fg-muted text-[13px]">{t("ts.keys.createdBody")}</p>
          <div className="border-edge bg-surface flex items-center gap-2 rounded-lg border px-3 py-2">
            <code className="min-w-0 flex-1 break-all font-mono text-[12.5px]">{createdKey.key}</code>
            <CopyButton value={createdKey.key} />
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
