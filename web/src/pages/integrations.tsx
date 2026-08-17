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
import { Input, PasswordInput, Textarea } from "@/ui/input";
import { Checkbox } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
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
                    {row.type === "proxmox" ? "Proxmox VE" : row.type === "docker" ? "Docker" : row.type} ·{" "}
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
                {row.type === "docker" && row.enabled && (
                  <Link
                    to="/docker/$instanceId"
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

type DockerTransport = "socket" | "tcp" | "ssh";

type DockerFormState = {
  name: string;
  transport: DockerTransport;
  socketPath: string;
  host: string;
  port: string;
  username: string;
  authType: "password" | "key";
  tls: boolean;
  caCert: string;
  clientCert: string;
  clientKey: string;
  sshSecret: string;
};

const emptyPveForm: PveFormState = {
  name: "",
  host: "",
  tokenId: "",
  tokenSecret: "",
  insecureTls: false,
  fingerprint: "",
};

const emptyDockerForm: DockerFormState = {
  name: "",
  transport: "socket",
  socketPath: "/var/run/docker.sock",
  host: "",
  port: "22",
  username: "",
  authType: "password",
  tls: false,
  caCert: "",
  clientCert: "",
  clientKey: "",
  sshSecret: "",
};

type FormPayload = { config: Record<string, unknown>; secrets: Record<string, string> };

function pvePayload(form: PveFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.tokenSecret) secrets.tokenSecret = form.tokenSecret;
  return {
    config: {
      host: form.host,
      tokenId: form.tokenId,
      insecureTls: form.insecureTls,
      fingerprint: form.fingerprint,
    },
    secrets,
  };
}

// Empty secrets are omitted so a stored value survives an edit untouched.
function dockerPayload(form: DockerFormState): FormPayload {
  const secrets: Record<string, string> = {};
  if (form.transport === "tcp" && form.tls) {
    if (form.caCert) secrets.caCert = form.caCert;
    if (form.clientCert) secrets.clientCert = form.clientCert;
    if (form.clientKey) secrets.clientKey = form.clientKey;
  }
  if (form.transport === "ssh" && form.sshSecret) secrets.sshSecret = form.sshSecret;
  return {
    config: {
      transport: form.transport,
      socketPath: form.socketPath,
      host: form.host,
      port: Number(form.port) || 22,
      username: form.username,
      authType: form.authType,
      tls: form.tls,
    },
    secrets,
  };
}

function pveComplete(form: PveFormState, isNew: boolean): boolean {
  return Boolean(
    form.name.trim() && form.host.trim() && form.tokenId.trim() && (form.tokenSecret.trim() || !isNew),
  );
}

function dockerComplete(form: DockerFormState, isNew: boolean): boolean {
  if (!form.name.trim()) return false;
  switch (form.transport) {
    case "socket":
      return true;
    case "tcp":
      return Boolean(form.host.trim());
    case "ssh":
      return Boolean(form.host.trim() && form.username.trim() && (form.sshSecret.trim() || !isNew));
  }
}

function AddIntegrationDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const types = useQuery({
    queryKey: ["integration-types"],
    queryFn: async () => unwrap(await api.GET("/integration-types")),
  });

  const [type, setType] = useState<IntegrationType | null>(null);
  const [pveForm, setPveForm] = useState<PveFormState>(emptyPveForm);
  const [dockerForm, setDockerForm] = useState<DockerFormState>(emptyDockerForm);
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const isDocker = type?.id === "docker";
  const payload = () => (isDocker ? dockerPayload(dockerForm) : pvePayload(pveForm));

  const test = useMutation({
    mutationFn: async () =>
      unwrap(await api.POST("/integrations/test", { body: { type: type!.id, ...payload() } })),
    onSuccess: setTestResult,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const create = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/integrations", {
          body: { type: type!.id, name: isDocker ? dockerForm.name : pveForm.name, ...payload() },
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

  const complete = isDocker ? dockerComplete(dockerForm, true) : pveComplete(pveForm, true);

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
        {isDocker ? (
          <DockerForm form={dockerForm} setForm={setDockerForm} isNew testResult={testResult} onFormChange={() => setTestResult(null)} />
        ) : (
          <PveForm form={pveForm} setForm={setPveForm} isNew testResult={testResult} onFormChange={() => setTestResult(null)} />
        )}
        {!testResult && <p className="text-fg-subtle mt-3 text-[13px]">{t("integrations.testFirst")}</p>}
      </DialogContent>
    </Dialog>
  );
}

function EditIntegrationDialog({ row, onClose }: { row: Integration; onClose: () => void }) {
  const { t } = useTranslation();
  const queryClient = useQueryClient();
  const toast = useToast();

  const isDocker = row.type === "docker";
  const cfg = row.config as Record<string, unknown>;
  const [pveForm, setPveForm] = useState<PveFormState>({
    name: row.name,
    host: String(cfg.host ?? ""),
    tokenId: String(cfg.tokenId ?? ""),
    tokenSecret: "",
    insecureTls: Boolean(cfg.insecureTls),
    fingerprint: String(cfg.fingerprint ?? ""),
  });
  const [dockerForm, setDockerForm] = useState<DockerFormState>({
    ...emptyDockerForm,
    name: row.name,
    transport: cfg.transport === "tcp" || cfg.transport === "ssh" ? cfg.transport : "socket",
    socketPath: String(cfg.socketPath ?? "") || "/var/run/docker.sock",
    host: String(cfg.host ?? ""),
    port: String(cfg.port ?? 22),
    username: String(cfg.username ?? ""),
    authType: cfg.authType === "key" ? "key" : "password",
    tls: Boolean(cfg.tls),
  });
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  const save = useMutation({
    mutationFn: async () => {
      const { config, secrets } = isDocker ? dockerPayload(dockerForm) : pvePayload(pveForm);
      return unwrap(
        await api.PATCH("/integrations/{id}", {
          params: { path: { id: row.id } },
          body: { name: isDocker ? dockerForm.name : pveForm.name, config, secrets },
        }),
      );
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["integrations"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const complete = isDocker ? dockerComplete(dockerForm, false) : pveComplete(pveForm, false);

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
            <Button variant="primary" disabled={!complete} loading={save.isPending} onClick={() => save.mutate()}>
              {t("common.save")}
            </Button>
          </>
        }
      >
        {isDocker ? (
          <DockerForm form={dockerForm} setForm={setDockerForm} isNew={false} testResult={testResult} onFormChange={() => setTestResult(null)} />
        ) : (
          <PveForm form={pveForm} setForm={setPveForm} isNew={false} testResult={testResult} onFormChange={() => setTestResult(null)} />
        )}
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

      <TestResultNote result={testResult} />
    </div>
  );
}

function DockerForm({
  form,
  setForm,
  isNew,
  testResult,
  onFormChange,
}: {
  form: DockerFormState;
  setForm: (fn: (prev: DockerFormState) => DockerFormState) => void;
  isNew: boolean;
  testResult: TestConnectionResult | null;
  onFormChange: () => void;
}) {
  const { t } = useTranslation();
  const update = (patch: Partial<DockerFormState>) => {
    onFormChange();
    setForm((prev) => ({ ...prev, ...patch }));
  };

  const secretHint = isNew ? undefined : t("integrations.docker.secretKeep");
  const pemClass = "font-mono text-xs";

  return (
    <div className="space-y-4">
      <div className="grid gap-4 sm:grid-cols-2">
        <Field label={t("integrations.form.name")} hint={t("integrations.form.nameHintDocker")}>
          {(p) => <Input {...p} autoFocus value={form.name} onChange={(e) => update({ name: e.target.value })} />}
        </Field>
        <Field label={t("integrations.docker.transport")}>
          {() => (
            <Select value={form.transport} onValueChange={(v) => update({ transport: v as DockerTransport })}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="socket">{t("integrations.docker.transportSocket")}</SelectItem>
                <SelectItem value="tcp">{t("integrations.docker.transportTcp")}</SelectItem>
                <SelectItem value="ssh">{t("integrations.docker.transportSsh")}</SelectItem>
              </SelectContent>
            </Select>
          )}
        </Field>
      </div>

      {form.transport === "socket" && (
        <Field label={t("integrations.docker.socketPath")} hint={t("integrations.docker.socketPathHint")}>
          {(p) => (
            <Input {...p} className="font-mono text-[13px]" placeholder="/var/run/docker.sock" value={form.socketPath} onChange={(e) => update({ socketPath: e.target.value })} />
          )}
        </Field>
      )}

      {form.transport === "tcp" && (
        <>
          <Field label={t("integrations.docker.host")} hint={t("integrations.docker.hostHintTcp")}>
            {(p) => <Input {...p} placeholder="192.168.1.20" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
          </Field>
          <Checkbox
            checked={form.tls}
            onCheckedChange={(checked) => update({ tls: checked })}
            label={t("integrations.docker.tls")}
            hint={t("integrations.docker.tlsHint")}
          />
          {form.tls && (
            <>
              <Field label={t("integrations.docker.caCert")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN CERTIFICATE-----" value={form.caCert} onChange={(e) => update({ caCert: e.target.value })} />
                )}
              </Field>
              <Field label={t("integrations.docker.clientCert")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN CERTIFICATE-----" value={form.clientCert} onChange={(e) => update({ clientCert: e.target.value })} />
                )}
              </Field>
              <Field label={t("integrations.docker.clientKey")} hint={secretHint} optional={t("common.optional")}>
                {(p) => (
                  <Textarea {...p} className={pemClass} rows={4} placeholder="-----BEGIN PRIVATE KEY-----" value={form.clientKey} onChange={(e) => update({ clientKey: e.target.value })} />
                )}
              </Field>
            </>
          )}
        </>
      )}

      {form.transport === "ssh" && (
        <>
          <div className="grid grid-cols-3 gap-3">
            <Field label={t("integrations.docker.host")} hint={t("integrations.docker.hostHintSsh")} className="col-span-2">
              {(p) => <Input {...p} placeholder="192.168.1.20" value={form.host} onChange={(e) => update({ host: e.target.value })} />}
            </Field>
            <Field label={t("integrations.docker.port")}>
              {(p) => <Input {...p} type="number" value={form.port} onChange={(e) => update({ port: e.target.value })} />}
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("integrations.docker.username")}>
              {(p) => <Input {...p} placeholder="root" value={form.username} onChange={(e) => update({ username: e.target.value })} />}
            </Field>
            <Field label={t("integrations.docker.authType")}>
              {() => (
                <Select value={form.authType} onValueChange={(v) => update({ authType: v as "password" | "key" })}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="password">{t("integrations.docker.authPassword")}</SelectItem>
                    <SelectItem value="key">{t("integrations.docker.authKey")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </Field>
          </div>
          <Field
            label={form.authType === "password" ? t("integrations.docker.sshPassword") : t("integrations.docker.sshKey")}
            hint={secretHint}
          >
            {(p) =>
              form.authType === "password" ? (
                <PasswordInput {...p} value={form.sshSecret} onChange={(e) => update({ sshSecret: e.target.value })} />
              ) : (
                <Textarea {...p} className={pemClass} rows={5} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" value={form.sshSecret} onChange={(e) => update({ sshSecret: e.target.value })} />
              )
            }
          </Field>
        </>
      )}

      <TestResultNote result={testResult} />
    </div>
  );
}

function TestResultNote({ result }: { result: TestConnectionResult | null }) {
  if (!result) return null;
  return (
    <div
      role="status"
      className={
        "rounded-md px-3 py-2.5 text-[13px] " +
        (result.ok ? "bg-success-soft text-success-soft-fg" : "bg-danger-soft text-danger-soft-fg")
      }
    >
      {result.message}
    </div>
  );
}

