import { useEffect, useMemo, useState } from "react";
import { useMutation, useQueryClient, type UseQueryResult } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Archive,
  CheckCircle2,
  Cloud,
  DatabaseBackup,
  HardDrive,
  Pencil,
  Play,
  Server,
  Trash2,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type BackupTarget,
  type BackupTargetInput,
  type TestConnectionResult,
} from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { cn, formatBytes } from "@/lib/utils";
import { Card } from "@/ui/card";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input, PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";
import { Checkbox } from "@/ui/misc";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { EmptyState, ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { Tooltip } from "@/ui/tooltip";
import { useToast } from "@/ui/toast";
import { BACKUPS_RES } from "./shared";
import { ArchivesDialog } from "./archives";

type TargetType = BackupTarget["type"];

export const targetTypeIcon: Record<TargetType, LucideIcon> = {
  local: HardDrive,
  smb: Server,
  nfs: Server,
  s3: Cloud,
};

const statusTone = { unknown: "neutral", ok: "success", error: "danger" } as const;

type TargetFieldSpec = {
  key: string;
  labelKey: string;
  hintKey?: string;
  secret?: boolean;
  required?: boolean;
  numeric?: boolean;
  mono?: boolean;
  placeholder?: string;
};

/** Per-type form fields. Secret keys are stored write-only server-side. */
const targetFields: Record<TargetType, TargetFieldSpec[]> = {
  local: [
    { key: "path", required: true, mono: true, placeholder: "/mnt/backups", labelKey: "backups.fields.path", hintKey: "backups.fields.pathHint" },
  ],
  smb: [
    { key: "host", required: true, placeholder: "nas.lan", labelKey: "backups.fields.host" },
    { key: "share", required: true, placeholder: "backups", labelKey: "backups.fields.share" },
    { key: "subdir", mono: true, labelKey: "backups.fields.subdir", hintKey: "backups.fields.subdirHint" },
    { key: "domain", labelKey: "backups.fields.domain" },
    { key: "username", secret: true, required: true, labelKey: "backups.fields.username" },
    { key: "password", secret: true, required: true, labelKey: "backups.fields.password" },
  ],
  nfs: [
    { key: "host", required: true, placeholder: "nas.lan", labelKey: "backups.fields.host" },
    { key: "export", required: true, mono: true, placeholder: "/export/backups", labelKey: "backups.fields.export" },
    { key: "subdir", mono: true, labelKey: "backups.fields.subdir", hintKey: "backups.fields.subdirHint" },
    { key: "uid", numeric: true, labelKey: "backups.fields.uid", hintKey: "backups.fields.uidHint" },
    { key: "gid", numeric: true, labelKey: "backups.fields.gid" },
  ],
  s3: [
    { key: "endpoint", required: true, mono: true, placeholder: "https://s3.example.com", labelKey: "backups.fields.endpoint", hintKey: "backups.fields.endpointHint" },
    { key: "bucket", required: true, labelKey: "backups.fields.bucket" },
    { key: "prefix", mono: true, labelKey: "backups.fields.prefix", hintKey: "backups.fields.prefixHint" },
    { key: "region", labelKey: "backups.fields.region" },
    { key: "accessKey", secret: true, required: true, labelKey: "backups.fields.accessKey" },
    { key: "secretKey", secret: true, required: true, labelKey: "backups.fields.secretKey" },
  ],
};

export function TargetsSection({ targets }: { targets: UseQueryResult<BackupTarget[], unknown> }) {
  const { t } = useTranslation();
  const toast = useToast();
  const { can } = useAuth();
  const queryClient = useQueryClient();

  const canControl = can(BACKUPS_RES, "control");
  const canConfigure = can(BACKUPS_RES, "configure");
  const canDelete = can(BACKUPS_RES, "delete");

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<BackupTarget | null>(null);
  const [deleting, setDeleting] = useState<BackupTarget | null>(null);
  const [browsing, setBrowsing] = useState<BackupTarget | null>(null);
  const [testResults, setTestResults] = useState<Record<string, TestConnectionResult>>({});

  const test = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/backups/targets/{id}/test", { params: { path: { id } } })),
    onSuccess: (result, id) => {
      setTestResults((prev) => ({ ...prev, [id]: result }));
      void queryClient.invalidateQueries({ queryKey: ["backups", "targets"] });
    },
    onError: (err, id) =>
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, message: isApiError(err) ? err.message : t("common.unknownError") },
      })),
  });

  const runNow = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/backups/targets/{id}/run", { params: { path: { id } } })),
    onSuccess: () => {
      toast("success", t("backups.targets.runQueuedTitle"), t("backups.targets.runQueuedBody"));
      void queryClient.invalidateQueries({ queryKey: ["backups", "runs"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/backups/targets/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      toast("success", t("backups.targets.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["backups", "targets"] });
      void queryClient.invalidateQueries({ queryKey: ["backups", "schedules"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  return (
    <section className="space-y-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div>
          <h2 className="text-fg text-[15px] font-semibold">{t("backups.targets.title")}</h2>
          <p className="text-fg-muted mt-0.5 text-[13px]">{t("backups.targets.intro")}</p>
        </div>
        {canConfigure && (
          <Button
            variant="primary"
            onClick={() => {
              setEditing(null);
              setEditorOpen(true);
            }}
          >
            {t("backups.targets.add")}
          </Button>
        )}
      </div>

      {targets.isLoading ? (
        <SkeletonRows rows={3} />
      ) : targets.isError ? (
        <Card>
          <ErrorState error={targets.error} onRetry={() => void targets.refetch()} />
        </Card>
      ) : (targets.data ?? []).length === 0 ? (
        <Card>
          <EmptyState
            icon={DatabaseBackup}
            title={t("backups.targets.empty.title")}
            body={t("backups.targets.empty.body")}
            action={
              canConfigure ? (
                <Button
                  variant="primary"
                  onClick={() => {
                    setEditing(null);
                    setEditorOpen(true);
                  }}
                >
                  {t("backups.targets.add")}
                </Button>
              ) : undefined
            }
          />
        </Card>
      ) : (
        <div className="grid gap-3 md:grid-cols-2">
          {targets.data!.map((target) => {
            const Icon = targetTypeIcon[target.type];
            const result = testResults[target.id];
            const testing = test.isPending && test.variables === target.id;
            const running = runNow.isPending && runNow.variables === target.id;
            const cap = target.capacity;
            const hasCapacity =
              cap != null && !cap.unknown && cap.totalBytes != null && cap.freeBytes != null && cap.totalBytes > 0;
            const usedFraction = hasCapacity ? (cap.totalBytes! - cap.freeBytes!) / cap.totalBytes! : 0;
            return (
              <Card key={target.id} className="flex flex-col p-4">
                <div className="flex items-start justify-between gap-2">
                  <div className="flex min-w-0 items-start gap-2.5">
                    <Icon className="text-fg-subtle mt-0.5 size-4.5 shrink-0" />
                    <div className="min-w-0">
                      <p className="text-fg flex items-center gap-2 text-sm font-semibold">
                        <span className="truncate">{target.name}</span>
                        <StatusDot tone={statusTone[target.status]} />
                      </p>
                      <p className="text-fg-subtle mt-0.5 text-xs">
                        {t(`backups.targets.types.${target.type}`)} ·{" "}
                        {t(`backups.targets.status.${target.status}`)}
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0 gap-1">
                    {canConfigure && (
                      <Tooltip label={t("common.edit")}>
                        <Button
                          size="icon"
                          variant="ghost"
                          aria-label={t("common.edit")}
                          onClick={() => {
                            setEditing(target);
                            setEditorOpen(true);
                          }}
                        >
                          <Pencil className="size-3.5" />
                        </Button>
                      </Tooltip>
                    )}
                    {canDelete && (
                      <Tooltip label={t("common.delete")}>
                        <Button
                          size="icon"
                          variant="danger-ghost"
                          aria-label={t("common.delete")}
                          onClick={() => setDeleting(target)}
                        >
                          <Trash2 className="size-3.5" />
                        </Button>
                      </Tooltip>
                    )}
                  </div>
                </div>

                {target.status === "error" && target.statusMessage && (
                  <p className="text-danger-soft-fg bg-danger-soft mt-3 rounded-md px-3 py-2 text-[13px]">
                    {target.statusMessage}
                  </p>
                )}

                {hasCapacity && (
                  <div className="mt-3">
                    <div className="bg-bg-subtle h-1.5 overflow-hidden rounded-full">
                      <div
                        className={cn(
                          "h-full rounded-full",
                          usedFraction > 0.9 ? "bg-danger" : usedFraction > 0.75 ? "bg-warning" : "bg-accent",
                        )}
                        style={{ width: `${Math.min(Math.max(usedFraction, 0) * 100, 100)}%` }}
                      />
                    </div>
                    <p className="text-fg-subtle mt-1 text-xs">
                      {t("backups.targets.capacityFree", {
                        free: formatBytes(cap.freeBytes!),
                        total: formatBytes(cap.totalBytes!),
                      })}
                    </p>
                  </div>
                )}

                <div className="mt-3 flex flex-wrap items-center gap-1.5">
                  <Badge tone="neutral">
                    {t("backups.targets.retentionBadge", { days: target.retentionDays })}
                  </Badge>
                  {target.encrypt && <Badge tone="info">{t("backups.targets.encryptedBadge")}</Badge>}
                  {!target.enabled && <Badge tone="neutral">{t("backups.targets.disabledBadge")}</Badge>}
                </div>

                <div className="mt-3 flex flex-wrap items-center gap-1.5">
                  {canConfigure && (
                    <Button size="sm" variant="secondary" loading={testing} onClick={() => test.mutate(target.id)}>
                      {t("backups.targets.test")}
                    </Button>
                  )}
                  {canControl && (
                    <Button
                      size="sm"
                      variant="secondary"
                      disabled={!target.enabled}
                      loading={running}
                      onClick={() => runNow.mutate(target.id)}
                    >
                      <Play className="size-3.5" />
                      {t("backups.targets.runNow")}
                    </Button>
                  )}
                  <Button size="sm" variant="ghost" onClick={() => setBrowsing(target)}>
                    <Archive className="size-3.5" />
                    {t("backups.targets.browse")}
                  </Button>
                </div>

                {result && (
                  <p
                    className={cn(
                      "mt-2 flex items-center gap-1.5 text-[13px]",
                      result.ok ? "text-success-soft-fg" : "text-danger-soft-fg",
                    )}
                    role="status"
                  >
                    {result.ok ? (
                      <CheckCircle2 className="size-3.5 shrink-0" />
                    ) : (
                      <XCircle className="size-3.5 shrink-0" />
                    )}
                    {result.message || (result.ok ? t("backups.targets.testOk") : t("backups.targets.testFailed"))}
                  </p>
                )}
              </Card>
            );
          })}
        </div>
      )}

      <TargetEditor open={editorOpen} onOpenChange={setEditorOpen} target={editing} />

      {browsing && <ArchivesDialog target={browsing} onClose={() => setBrowsing(null)} />}

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("backups.targets.deleteConfirmTitle", { name: deleting?.name ?? "" })}
        body={t("backups.targets.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </section>
  );
}

// ---- editor ----------------------------------------------------------------

function TargetEditor({
  open,
  onOpenChange,
  target,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Existing target to edit, or null to create. */
  target: BackupTarget | null;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [type, setType] = useState<TargetType | null>(null);
  const [name, setName] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});
  const [retention, setRetention] = useState("30");
  const [encrypt, setEncrypt] = useState(false);
  const [enabled, setEnabled] = useState(true);
  const [testResult, setTestResult] = useState<TestConnectionResult | null>(null);

  useEffect(() => {
    if (!open) return;
    setTestResult(null);
    if (target) {
      setType(target.type);
      setName(target.name);
      const seeded: Record<string, string> = {};
      for (const spec of targetFields[target.type]) {
        if (!spec.secret) {
          const v = target.config[spec.key];
          seeded[spec.key] = v == null ? "" : String(v);
        }
      }
      setValues(seeded);
      setRetention(String(target.retentionDays));
      setEncrypt(target.encrypt);
      setEnabled(target.enabled);
    } else {
      setType(null);
      setName("");
      setValues({});
      setRetention("30");
      setEncrypt(false);
      setEnabled(true);
    }
  }, [open, target]);

  const specs = useMemo(() => (type ? targetFields[type] : []), [type]);

  const payload = (): { config: Record<string, unknown>; secrets: Record<string, string> } => {
    const config: Record<string, unknown> = {};
    const secrets: Record<string, string> = {};
    for (const spec of specs) {
      const raw = (values[spec.key] ?? "").trim();
      if (spec.secret) {
        if (raw) secrets[spec.key] = raw;
      } else if (raw) {
        config[spec.key] = spec.numeric ? Number(raw) : raw;
      }
    }
    return { config, secrets };
  };

  const test = useMutation({
    mutationFn: async () => {
      const { config, secrets } = payload();
      return unwrap(
        await api.POST("/backups/targets/test", { body: { type: type!, config, secrets } }),
      );
    },
    onSuccess: setTestResult,
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const save = useMutation({
    mutationFn: async (body: BackupTargetInput) =>
      target
        ? unwrap(
            await api.PATCH("/backups/targets/{id}", { params: { path: { id: target.id } }, body }),
          )
        : unwrap(await api.POST("/backups/targets", { body })),
    onSuccess: () => {
      toast("success", target ? t("backups.targets.updated") : t("backups.targets.created"));
      void queryClient.invalidateQueries({ queryKey: ["backups", "targets"] });
      onOpenChange(false);
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const retentionNum = Number(retention);
  const retentionValid =
    Number.isInteger(retentionNum) && retentionNum >= 1 && retentionNum <= 3650;

  const complete =
    type !== null &&
    name.trim().length > 0 &&
    retentionValid &&
    specs.every((spec) => {
      if (!spec.required) return true;
      // Required secrets may stay empty on edit ("leave empty to keep").
      if (spec.secret && target) return true;
      return (values[spec.key] ?? "").trim().length > 0;
    });

  const submit = () => {
    if (!complete || !type) return;
    const { config, secrets } = payload();
    save.mutate({
      type,
      name: name.trim(),
      config,
      retentionDays: retentionNum,
      encrypt,
      enabled,
      ...(Object.keys(secrets).length > 0 ? { secrets } : {}),
    });
  };

  // Any change invalidates a previous test result (create mode).
  const touch = () => setTestResult(null);

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        wide={type !== null}
        title={target ? t("backups.targets.editTitle", { name: target.name }) : t("backups.targets.add")}
        description={type === null ? t("backups.targets.pickType") : undefined}
        footer={
          type !== null ? (
            <>
              <Button variant="ghost" onClick={() => onOpenChange(false)}>
                {t("common.cancel")}
              </Button>
              {!target && (
                <Button
                  variant="secondary"
                  disabled={!complete}
                  loading={test.isPending}
                  onClick={() => test.mutate()}
                >
                  {test.isPending ? t("backups.targets.testing") : t("backups.targets.test")}
                </Button>
              )}
              <Button
                variant="primary"
                disabled={!complete || (!target && !testResult?.ok)}
                loading={save.isPending}
                onClick={submit}
              >
                {target ? t("common.save") : t("common.create")}
              </Button>
            </>
          ) : undefined
        }
      >
        {type === null ? (
          <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
            {(Object.keys(targetFields) as TargetType[]).map((option) => {
              const Icon = targetTypeIcon[option];
              return (
                <button
                  key={option}
                  type="button"
                  onClick={() => {
                    setType(option);
                    setValues({});
                    setTestResult(null);
                  }}
                  className="border-edge hover:border-accent hover:bg-accent-soft/30 cursor-pointer rounded-lg border p-3.5 text-left transition-colors"
                >
                  <p className="text-fg flex items-center gap-2 text-sm font-semibold">
                    <Icon className="text-fg-subtle size-4 shrink-0" />
                    {t(`backups.targets.types.${option}`)}
                  </p>
                  <p className="text-fg-subtle mt-1 text-[13px]">
                    {t(`backups.targets.typeDescriptions.${option}`)}
                  </p>
                </button>
              );
            })}
          </div>
        ) : (
          <div className="space-y-4">
            <Field label={t("common.name")} hint={t("backups.targets.nameHint")}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  value={name}
                  onChange={(e) => {
                    touch();
                    setName(e.target.value);
                  }}
                />
              )}
            </Field>

            {specs.map((spec) => {
              const hint =
                spec.secret && target
                  ? t("backups.targets.secretKeep")
                  : spec.hintKey
                    ? t(spec.hintKey)
                    : undefined;
              const optional = !spec.required || (spec.secret && target !== null);
              const onChange = (e: React.ChangeEvent<HTMLInputElement>) => {
                touch();
                setValues((prev) => ({ ...prev, [spec.key]: e.target.value }));
              };
              return (
                <Field
                  key={spec.key}
                  label={t(spec.labelKey)}
                  hint={hint}
                  optional={optional ? t("common.optional") : undefined}
                >
                  {(p) =>
                    spec.secret ? (
                      <PasswordInput {...p} value={values[spec.key] ?? ""} onChange={onChange} />
                    ) : (
                      <Input
                        {...p}
                        type={spec.numeric ? "number" : "text"}
                        placeholder={spec.placeholder}
                        className={spec.mono ? "font-mono text-[13px]" : undefined}
                        value={values[spec.key] ?? ""}
                        onChange={onChange}
                      />
                    )
                  }
                </Field>
              );
            })}

            <Field
              label={t("backups.targets.retention")}
              hint={t("backups.targets.retentionHint")}
              error={retention !== "" && !retentionValid ? t("backups.targets.retentionRange") : null}
            >
              {(p) => (
                <Input
                  {...p}
                  type="number"
                  min={1}
                  max={3650}
                  required
                  className="max-w-32"
                  value={retention}
                  onChange={(e) => setRetention(e.target.value)}
                />
              )}
            </Field>

            <Checkbox
              checked={encrypt}
              onCheckedChange={setEncrypt}
              label={t("backups.targets.encrypt")}
              hint={t("backups.targets.encryptHint")}
            />
            <Checkbox
              checked={enabled}
              onCheckedChange={setEnabled}
              label={t("common.enabled")}
              hint={t("backups.targets.enabledHint")}
            />

            {testResult && (
              <div
                role="status"
                className={cn(
                  "rounded-md px-3 py-2.5 text-[13px]",
                  testResult.ok
                    ? "bg-success-soft text-success-soft-fg"
                    : "bg-danger-soft text-danger-soft-fg",
                )}
              >
                {testResult.message ||
                  (testResult.ok ? t("backups.targets.testOk") : t("backups.targets.testFailed"))}
              </div>
            )}
            {!target && !testResult && (
              <p className="text-fg-subtle text-[13px]">{t("backups.targets.testFirst")}</p>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
