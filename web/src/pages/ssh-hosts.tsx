import { Suspense, lazy, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Fingerprint, KeyRound, Pencil, Plus, ServerCog, SquareTerminal, TerminalSquare, Trash2, Zap } from "lucide-react";
import { api, isApiError, unwrap, type SSHHost, type TermSession, type TestConnectionResult } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { SHELL } from "@/lib/perms";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input, PasswordInput, Textarea } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { PageLoader } from "@/ui/misc";
import { EmptyState, ErrorState } from "@/ui/states";
import { Table, TBody, TD, TH, THead, TR } from "@/ui/table";
import { useToast } from "@/ui/toast";
import { HostKeyDialog, KnownHostsCard, hostKeyChanged } from "./ssh-host-keys";

const TerminalPane = lazy(() =>
  import("@/components/terminal").then((m) => ({ default: m.TerminalPane })),
);

export const SSH_RES = "system/ssh";

export function useSSHHosts(enabled = true) {
  return useQuery({
    queryKey: ["ssh-hosts"],
    queryFn: async () => unwrap(await api.GET("/ssh-hosts")),
    enabled,
  });
}

/**
 * The central SSH registry: every machine HomeHub can reach over SSH, entered
 * once and reused everywhere — Proxmox guest consoles, the assistant, ad-hoc
 * terminals. Secrets live in the vault and never come back over the API.
 */
export function SSHHostsPage() {
  const { t } = useTranslation();
  const { can } = useAuth();
  const hosts = useSSHHosts();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [editing, setEditing] = useState<SSHHost | null>(null);
  const [creating, setCreating] = useState(false);
  const [deleting, setDeleting] = useState<SSHHost | null>(null);
  const [session, setSession] = useState<{ host: SSHHost; term: TermSession } | null>(null);
  const [tested, setTested] = useState<Record<string, TestConnectionResult>>({});
  const [hostKeyFor, setHostKeyFor] = useState<SSHHost | null>(null);

  const canConfigure = can(SSH_RES, "configure");

  const test = useMutation({
    mutationFn: async (host: SSHHost) =>
      ({ id: host.id, result: unwrap(await api.POST("/ssh-hosts/{id}/test", { params: { path: { id: host.id } } })) }),
    onSuccess: ({ id, result }) => setTested((prev) => ({ ...prev, [id]: result })),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const openTerminal = useMutation({
    mutationFn: async (host: SSHHost) => ({
      host,
      term: unwrap(await api.POST("/ssh-hosts/{id}/terminal", { params: { path: { id: host.id } } })),
    }),
    onSuccess: setSession,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (host: SSHHost) =>
      unwrap(await api.DELETE("/ssh-hosts/{id}", { params: { path: { id: host.id } } })),
    onSuccess: () => {
      setDeleting(null);
      void queryClient.invalidateQueries({ queryKey: ["ssh-hosts"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  if (hosts.isLoading) return <PageLoader />;
  if (hosts.isError) {
    return (
      <Card>
        <ErrorState error={hosts.error} onRetry={() => void hosts.refetch()} />
      </Card>
    );
  }
  const list = hosts.data ?? [];

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <ServerCog className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("ssh.title")}</h1>
          <p className="text-fg-muted text-sm">{t("ssh.subtitle")}</p>
        </div>
        {canConfigure && (
          <Button variant="primary" onClick={() => setCreating(true)}>
            <Plus className="size-4" />
            {t("ssh.add")}
          </Button>
        )}
      </div>

      {list.length === 0 ? (
        <Card>
          <EmptyState icon={ServerCog} title={t("ssh.empty.title")} body={t("ssh.empty.body")} />
        </Card>
      ) : (
        <Card>
          <div className="overflow-x-auto">
            <Table>
              <THead>
                <TR>
                  <TH>{t("ssh.cols.name")}</TH>
                  <TH>{t("ssh.cols.target")}</TH>
                  <TH>{t("ssh.cols.auth")}</TH>
                  <TH>{t("ssh.cols.usedBy")}</TH>
                  <TH aria-label={t("common.actions")} />
                </TR>
              </THead>
              <TBody>
                {list.map((host) => {
                  const result = tested[host.id];
                  return (
                    <TR key={host.id}>
                      <TD>
                        <p className="text-fg font-medium">{host.name}</p>
                        {host.notes && <p className="text-fg-subtle truncate text-[12px]">{host.notes}</p>}
                        {result && (
                          <p className={result.ok ? "text-success text-[12px]" : "text-danger text-[12px]"}>
                            {result.message}
                          </p>
                        )}
                        {result && !result.ok && hostKeyChanged(result.message) && (
                          <button
                            type="button"
                            onClick={() => setHostKeyFor(host)}
                            className="text-accent mt-0.5 cursor-pointer text-[12px] font-medium hover:underline"
                          >
                            {t("ssh.hostKey.fixIt")}
                          </button>
                        )}
                      </TD>
                      <TD>
                        <code className="font-mono text-[12.5px]">
                          {host.username}@{host.hostname}
                          {host.port !== 22 ? `:${host.port}` : ""}
                        </code>
                      </TD>
                      <TD>
                        <Badge tone={host.authType === "key" ? "success" : "neutral"}>
                          {host.authType === "key" ? (
                            <KeyRound className="size-3" />
                          ) : null}
                          {t(`ssh.auth.${host.authType}`)}
                        </Badge>
                      </TD>
                      <TD>
                        {host.inUse > 0 ? (
                          <span className="text-fg-muted text-[12.5px]" title={(host.boundResources ?? []).join("\n")}>
                            {t("ssh.boundCount", { count: host.inUse })}
                          </span>
                        ) : (
                          <span className="text-fg-subtle text-[12.5px]">—</span>
                        )}
                      </TD>
                      <TD>
                        <span className="flex justify-end gap-1">
                          <Button size="sm" variant="ghost" aria-label={t("ssh.test")} title={t("ssh.test")}
                            loading={test.isPending && test.variables?.id === host.id}
                            onClick={() => test.mutate(host)}>
                            <Zap className="size-4" />
                          </Button>
                          <Button size="sm" variant="ghost" aria-label={t("ssh.hostKey.title")} title={t("ssh.hostKey.title")}
                            onClick={() => setHostKeyFor(host)}>
                            <Fingerprint className="size-4" />
                          </Button>
                          {can(`ssh/${host.id}`, SHELL) || can(SSH_RES, SHELL) ? (
                            <Button size="sm" variant="ghost" aria-label={t("ssh.terminal")} title={t("ssh.terminal")}
                              loading={openTerminal.isPending && openTerminal.variables?.id === host.id}
                              onClick={() => openTerminal.mutate(host)}>
                              <SquareTerminal className="size-4" />
                            </Button>
                          ) : null}
                          {canConfigure && (
                            <>
                              <Button size="sm" variant="ghost" aria-label={t("common.edit")} onClick={() => setEditing(host)}>
                                <Pencil className="size-4" />
                              </Button>
                              <Button size="sm" variant="ghost" aria-label={t("common.delete")} onClick={() => setDeleting(host)}>
                                <Trash2 className="text-danger size-4" />
                              </Button>
                            </>
                          )}
                        </span>
                      </TD>
                    </TR>
                  );
                })}
              </TBody>
            </Table>
          </div>
        </Card>
      )}

      <KnownHostsCard />

      {hostKeyFor && (
        <HostKeyDialog
          host={hostKeyFor.hostname}
          port={hostKeyFor.port}
          label={hostKeyFor.name}
          onClose={() => setHostKeyFor(null)}
        />
      )}

      {(creating || editing) && (
        <SSHHostDialog
          host={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}

      <ConfirmDialog
        open={Boolean(deleting)}
        onOpenChange={(o) => !o && setDeleting(null)}
        tone="danger"
        title={t("ssh.deleteConfirm.title")}
        body={
          deleting && deleting.inUse > 0
            ? t("ssh.deleteConfirm.bodyInUse", { name: deleting.name, count: deleting.inUse })
            : t("ssh.deleteConfirm.body", { name: deleting?.name ?? "" })
        }
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting)}
      />

      {session && (
        <Dialog open onOpenChange={(o) => !o && setSession(null)}>
          <DialogContent wide className="max-w-[min(94vw,1200px)]" title={`${session.host.username}@${session.host.hostname}`}>
            <div className="border-edge overflow-hidden rounded-lg border" style={{ height: "min(70vh, 560px)" }}>
              <Suspense fallback={<PageLoader className="h-full" />}>
                <TerminalPane
                  wsUrl={`${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${session.term.wsPath}`}
                  renew={async () => {
                    const fresh = unwrap(
                      await api.POST("/ssh-hosts/{id}/terminal", { params: { path: { id: session.host.id } } }),
                    );
                    return `${location.protocol === "https:" ? "wss" : "ws"}://${location.host}${fresh.wsPath}`;
                  }}
                  className="h-full"
                />
              </Suspense>
            </div>
          </DialogContent>
        </Dialog>
      )}
    </div>
  );
}

function SSHHostDialog({ host, onClose }: { host: SSHHost | null; onClose: () => void }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [name, setName] = useState(host?.name ?? "");
  const [hostname, setHostname] = useState(host?.hostname ?? "");
  const [port, setPort] = useState(String(host?.port ?? 22));
  const [username, setUsername] = useState(host?.username ?? "");
  const [authType, setAuthType] = useState<"password" | "key">(host?.authType ?? "password");
  const [secret, setSecret] = useState("");
  const [notes, setNotes] = useState(host?.notes ?? "");
  // "generate" is the default for new hosts: HomeHub makes the keypair,
  // installs it with the password once, and stores only the private half.
  const [mode, setMode] = useState<"generate" | "password" | "key">(host ? host.authType : "generate");

  const provision = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.POST("/ssh-hosts/provision", {
          body: {
            ...(host ? { id: host.id } : {}),
            name: name.trim() || undefined,
            hostname: hostname.trim(),
            port: Number(port) || 22,
            username: username.trim(),
            password: secret,
            notes: notes.trim(),
          },
        }),
      ),
    onSuccess: () => {
      toast("success", t("ssh.generated"));
      void queryClient.invalidateQueries({ queryKey: ["ssh-hosts"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const save = useMutation({
    mutationFn: async () => {
      const body = {
        name: name.trim(),
        hostname: hostname.trim(),
        port: Number(port) || 22,
        username: username.trim(),
        authType,
        notes: notes.trim(),
        ...(secret.trim() ? { secret } : {}),
      };
      return host
        ? unwrap(await api.PUT("/ssh-hosts/{id}", { params: { path: { id: host.id } }, body }))
        : unwrap(await api.POST("/ssh-hosts", { body }));
    },
    onSuccess: () => {
      toast("success", t("ssh.saved"));
      void queryClient.invalidateQueries({ queryKey: ["ssh-hosts"] });
      onClose();
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const complete = name.trim() && hostname.trim() && username.trim() && (host || secret.trim());
  // Generating needs only the address, the user, and the password once.
  const generateReady = hostname.trim() && username.trim() && secret.trim();

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent title={host ? t("ssh.editTitle") : t("ssh.addTitle")} description={t("ssh.dialogBody")}>
        <div className="space-y-4">
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("ssh.form.name")} hint={t("ssh.form.nameHint")}>
              {(p) => <Input {...p} autoFocus value={name} onChange={(e) => setName(e.target.value)} placeholder="Proxmox host" />}
            </Field>
            <Field label={t("ssh.form.notes")}>
              {(p) => <Input {...p} value={notes} onChange={(e) => setNotes(e.target.value)} />}
            </Field>
          </div>
          <div className="grid grid-cols-[1fr_110px] gap-2">
            <Field label={t("ssh.form.hostname")}>
              {(p) => <Input {...p} value={hostname} onChange={(e) => setHostname(e.target.value)} placeholder="192.168.20.10" />}
            </Field>
            <Field label={t("ssh.form.port")}>
              {(p) => <Input {...p} type="number" value={port} onChange={(e) => setPort(e.target.value)} />}
            </Field>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <Field label={t("ssh.form.username")}>
              {(p) => <Input {...p} value={username} onChange={(e) => setUsername(e.target.value)} placeholder="root" />}
            </Field>
            <Field label={t("ssh.form.authType")}>
              {(p) => (
                <Select
                  value={mode}
                  onValueChange={(v) => {
                    const m = v as "generate" | "password" | "key";
                    setMode(m);
                    setSecret("");
                    if (m !== "generate") setAuthType(m);
                  }}
                >
                  <SelectTrigger id={p.id}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="generate">{t("ssh.auth.generate")}</SelectItem>
                    <SelectItem value="password">{t("ssh.auth.password")}</SelectItem>
                    <SelectItem value="key">{t("ssh.auth.key")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </Field>
          </div>
          <Field
            label={
              mode === "generate"
                ? t("ssh.form.passwordOnce")
                : mode === "key"
                  ? t("ssh.form.privateKey")
                  : t("ssh.form.password")
            }
            hint={
              mode === "generate"
                ? t("ssh.form.passwordOnceHint")
                : host
                  ? t("ssh.form.secretKeep")
                  : undefined
            }
          >
            {(p) =>
              mode === "key" ? (
                <Textarea {...p} rows={5} className="font-mono text-[12px]" value={secret}
                  onChange={(e) => setSecret(e.target.value)} placeholder="-----BEGIN OPENSSH PRIVATE KEY-----" />
              ) : (
                <PasswordInput {...p} value={secret} onChange={(e) => setSecret(e.target.value)} />
              )
            }
          </Field>
          {mode === "generate" && (
            <p className="text-fg-subtle flex items-start gap-1.5 text-[12px]">
              <KeyRound className="mt-0.5 size-3.5 shrink-0" />
              {t("ssh.form.generateExplainer")}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            {mode === "generate" ? (
              <Button variant="primary" loading={provision.isPending} disabled={!generateReady} onClick={() => provision.mutate()}>
                <KeyRound className="size-4" />
                {t("ssh.generateAndSave")}
              </Button>
            ) : (
              <Button variant="primary" loading={save.isPending} disabled={!complete} onClick={() => save.mutate()}>
                {host ? t("common.save") : t("ssh.add")}
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}

/**
 * SSHHostPicker is the reusable "choose a saved SSH host" control other
 * surfaces embed (Proxmox guests, and anywhere else SSH is configured).
 * Selecting "manual" hands control back to the caller's own fields.
 */
export function SSHHostPicker({
  value,
  onChange,
  allowManual = true,
}: {
  value: string | null;
  onChange: (hostId: string | null) => void;
  allowManual?: boolean;
}) {
  const { t } = useTranslation();
  const hosts = useSSHHosts();
  const list = hosts.data ?? [];
  return (
    <Select value={value ?? "manual"} onValueChange={(v) => onChange(v === "manual" ? null : v)}>
      <SelectTrigger>
        <SelectValue placeholder={t("ssh.picker.placeholder")} />
      </SelectTrigger>
      <SelectContent>
        {list.map((h) => (
          <SelectItem key={h.id} value={h.id}>
            <span className="flex items-center gap-1.5">
              <TerminalSquare className="size-3.5" />
              {h.name}
              <span className="text-fg-subtle">
                {h.username}@{h.hostname}
              </span>
            </span>
          </SelectItem>
        ))}
        {allowManual && <SelectItem value="manual">{t("ssh.picker.manual")}</SelectItem>}
      </SelectContent>
    </Select>
  );
}
