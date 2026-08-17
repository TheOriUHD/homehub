import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { Boxes, Pencil, Plug, Trash2 } from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type Integration,
  type IntegrationType,
  type TestConnectionResult,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { useLiveInvalidate } from "@/lib/live";
import { formatRelative } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Button } from "@/ui/button";
import { StatusDot } from "@/ui/badge";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { Field } from "@/ui/field";
import { Input, PasswordInput } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { Tooltip } from "@/ui/tooltip";

export const statusTone = {
  pending: "neutral",
  connecting: "info",
  connected: "success",
  degraded: "warning",
  unreachable: "danger",
  disabled: "neutral",
} as const;

export function IntegrationsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const toast = useToast();
  const queryClient = useQueryClient();

  const integrations = useQuery({
    queryKey: ["integrations"],
    queryFn: async () => unwrap(await api.GET("/integrations")),
  });
  useLiveInvalidate("integrations", [["integrations"]]);

  const canManage = can("system/integrations", "configure");
  const [addOpen, setAddOpen] = useState(false);
  const [editing, setEditing] = useState<Integration | null>(null);
  const [deleting, setDeleting] = useState<Integration | null>(null);

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/integrations/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const toggle = useMutation({
    mutationFn: async (row: Integration) =>
      unwrap(
        await api.PATCH("/integrations/{id}", {
          params: { path: { id: row.id } },
          body: { enabled: !row.enabled },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["integrations"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("integrations.title")}</h1>
          <p className="text-fg-muted mt-1 text-sm">{t("integrations.subtitle")}</p>
        </div>
        {canManage && (
          <Button variant="primary" onClick={() => setAddOpen(true)}>
            <Plug className="size-4" />
            {t("integrations.add")}
          </Button>
        )}
      </div>

      {integrations.isLoading ? (
        <SkeletonRows rows={3} />
      ) : integrations.isError ? (
        <Card>
          <ErrorState error={integrations.error} onRetry={() => void integrations.refetch()} />
        </Card>
      ) : integrations.data!.length === 0 ? (
        <Card>
          <EmptyState
            icon={Boxes}
            title={t("integrations.empty.title")}
            body={t("integrations.empty.body")}
            action={
              canManage ? (
                <Button variant="primary" onClick={() => setAddOpen(true)}>
                  {t("integrations.add")}
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {integrations.data!.map((row) => (
            <Card key={row.id} className="p-4">
              <div className="flex items-start justify-between gap-2">
                <div className="min-w-0">
                  <p className="text-fg flex items-center gap-2 text-sm font-semibold">
                    <StatusDot
                      tone={statusTone[row.status]}
                      pulse={row.status === "connecting" || row.status === "unreachable"}
                    />
                    {row.name}
                  </p>
                  <p className="text-fg-subtle mt-0.5 text-xs">
                    {row.type === "proxmox" ? "Proxmox VE" : row.type} ·{" "}
                    {t(`integrations.status.${row.status}`)} · {formatRelative(row.statusChangedAt)}
                  </p>
                </div>
                <div className="flex shrink-0 gap-1">
                  {canManage && (
                    <>
                      <Tooltip label={t("common.edit")}>
                        <Button variant="ghost" size="icon" aria-label={t("common.edit")} onClick={() => setEditing(row)}>
                          <Pencil className="size-4" />
                        </Button>
                      </Tooltip>
                      <Tooltip label={t("common.delete")}>
                        <Button variant="ghost" size="icon" aria-label={t("common.delete")} onClick={() => setDeleting(row)}>
                          <Trash2 className="text-danger size-4" />
                        </Button>
                      </Tooltip>
                    </>
                  )}
                </div>
              </div>

              {row.statusMessage && (
                <p className="text-danger-soft-fg bg-danger-soft mt-3 rounded-md px-3 py-2 text-[13px]">
                  {row.statusMessage}
                </p>
              )}

              <div className="mt-3 flex items-center gap-2">
                {row.type === "proxmox" && row.enabled && (
                  <Link
                    to="/pve/$instanceId"
                    params={{ instanceId: row.id }}
                    className="text-accent text-[13px] font-medium hover:underline"
                  >
                    {t("integrations.openModule")} →
                  </Link>
                )}
                {canManage && (
                  <Button size="sm" variant="ghost" className="ml-auto" loading={toggle.isPending} onClick={() => toggle.mutate(row)}>
                    {row.enabled ? t("integrations.disable") : t("integrations.enable")}
                  </Button>
                )}
              </div>
            </Card>
          ))}
        </div>
      )}

      {addOpen && <AddIntegrationDialog onClose={() => setAddOpen(false)} />}
      {editing && <EditIntegrationDialog row={editing} onClose={() => setEditing(null)} />}
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={deleting ? t("integrations.deleteConfirmTitle", { name: deleting.name }) : ""}
        body={t("integrations.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </div>
  );
}

// ---- add / edit ------------------------------------------------------------

type PveFormState = {
  name: string;
  host: string;
  tokenId: string;
  tokenSecret: string;
  insecureTls: boolean;
  fingerprint: string;
};

function AddIntegrationDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const types = useQuery({
    queryKey: ["integration-types"],
    queryFn: async () => unwrap(await api.GET("/integration-types")),
  });

  const [type, setType] = useState<IntegrationType | null>(null);
  const [form, setForm] = useState<PveFormState>({
    name: "",
    host: "",
    tokenId: "",
    tokenSecret: "",
    insecureTls: false,
    fingerprint: "",
  });
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const config = () => ({
    host: form.host,
    tokenId: form.tokenId,
    insecureTls: form.insecureTls,
    fingerprint: form.fingerprint,
  });

  const test = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/integrations/test", {
          body: { type: type!.id, config: config(), secrets: { tokenSecret: form.tokenSecret } },
        }),
      ),
    onSuccess: setTestResult,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/integrations", {
          body: {
            type: type!.id,
            name: form.name,
            config: config(),
            secrets: { tokenSecret: form.tokenSecret },
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (!type) {
    return (
      <Dialog open onOpenChange={(open) => !open && onClose()}>
        <DialogContent title={t("integrations.picker.title")} description={t("integrations.picker.intro")}>
          <div className="space-y-2">
            {(types.data ?? []).map((typeInfo) => (
              <button
                key={typeInfo.id}
                onClick={() => setType(typeInfo)}
                className="border-edge hover:border-accent hover:bg-accent-soft/30 w-full cursor-pointer rounded-lg border p-3.5 text-left transition-colors"
              >
                <p className="text-fg text-sm font-semibold">{typeInfo.name}</p>
                <p className="text-fg-subtle mt-0.5 text-[13px]">{typeInfo.description}</p>
              </button>
            ))}
            {types.isLoading && <SkeletonRows rows={2} />}
          </div>
        </DialogContent>
      </Dialog>
    );
  }

  const complete = form.name.trim() && form.host.trim() && form.tokenId.trim() && form.tokenSecret.trim();

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={type.name}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="secondary" disabled={!complete} loading={test.isPending} onClick={() => test.mutate()}>
              {test.isPending ? t("integrations.testing") : t("integrations.test")}
            </Button>
            <Button
              variant="primary"
              disabled={!complete || !testResult?.ok}
              loading={create.isPending}
              onClick={() => create.mutate()}
            >
              {t("common.create")}
            </Button>
          </>
        }
      >
        <PveForm form={form} setForm={setForm} isNew testResult={testResult} onFormChange={() => setTestResult(null)} />
        {!testResult && <p className="text-fg-subtle mt-3 text-[13px]">{t("integrations.testFirst")}</p>}
      </DialogContent>
    </Dialog>
  );
}

function EditIntegrationDialog({ row, onClose }: { row: Integration; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const cfg = row.config as Record<string, unknown>;
  const [form, setForm] = useState<PveFormState>({
    name: row.name,
    host: String(cfg.host ?? ""),
    tokenId: String(cfg.tokenId ?? ""),
    tokenSecret: "",
    insecureTls: Boolean(cfg.insecureTls),
    fingerprint: String(cfg.fingerprint ?? ""),
  });
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const secrets: Record<string, string> = {};
      if (form.tokenSecret) secrets.tokenSecret = form.tokenSecret;
      return unwrap(
        await api.PATCH("/integrations/{id}", {
          params: { path: { id: row.id } },
          body: {
            name: form.name,
            config: {
              host: form.host,
              tokenId: form.tokenId,
              insecureTls: form.insecureTls,
              fingerprint: form.fingerprint,
            },
            secrets,
          },
        }),
      );
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={t("integrations.form.editTitle", { name: row.name })}
        footer={
          <>
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button variant="primary" loading={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        <PveForm form={form} setForm={setForm} isNew={false} testResult={testResult} onFormChange={() => setTestResult(null)} />
      </DialogContent>
    </Dialog>
  );
}

function PveForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: PveFormState;
  setForm: (fn: (prev: PveFormState) => PveFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<PveFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintPve")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.pve.host")} hint={t("integrations.pve.hostHint")}>
          {(p) => <Input {...p} placeholder="pve.lan" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
        </Field>
      </div>
      <Field label={t("integrations.pve.tokenId")} hint={t("integrations.pve.tokenIdHint")}>
        {(p) => (
          <Input {...p} placeholder="root@pam!homehub" className="font-mono text-[13px]" value={form.tokenId} onChange={(e) => update({ tokenId: e.target.value })} />
        )}
      </Field>
      <Field
        label={t("integrations.pve.tokenSecret")}
        hint={isNew ? t("integrations.pve.tokenSecretHint") : t("integrations.pve.tokenSecretKeep")}
      >
        {(p) => (
          <PasswordInput {...p} className="font-mono text-[13px]" value={form.tokenSecret} onChange={(e) => update({ tokenSecret: e.target.value })} />
        )}
      </Field>
      <Field label={t("integrations.pve.fingerprint")} hint={t("integrations.pve.fingerprintHint")} optional={t("common.optional")}>
        {(p) => (
          <Input {...p} placeholder="AA:BB:CC:…" className="font-mono text-[13px]" value={form.fingerprint} onChange={(e) => update({ fingerprint: e.target.value })} />
        )}
      </Field>
      <Checkbox
        checked={form.insecureTls}
        onCheckedChange={(checked) => update({ insecureTls: checked })}
        label={t("integrations.pve.insecureTls")}
        hint={t("integrations.pve.insecureTlsHint")}
      />

      {testResult && (
        <div
          role="status"
          className={
            "rounded-md px-3 py-2.5 text-[13px] " +
            (testResult.ok ? "bg-success-soft text-success-soft-fg" : "bg-danger-soft text-danger-soft-fg")
          }
        >
          {testResult.message}
        </div>
      )}
    </div>
  );
}

