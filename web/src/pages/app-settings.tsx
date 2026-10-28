import { useEffect, useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import {
  Bell,
  BellRing,
  CheckCircle2,
  Mail,
  MessageSquare,
  Pencil,
  Send,
  Trash2,
  Webhook,
  XCircle,
  type LucideIcon,
} from "lucide-react";
import {
  api,
  unwrap,
  isApiError,
  type Channel,
  type ChannelInput,
  type TestConnectionResult,
} from "@/lib/api";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { Button } from "@/ui/button";
import { Input, PasswordInput } from "@/ui/input";
import { Field } from "@/ui/field";
import { Switch } from "@/ui/switch";
import { Dialog, DialogContent } from "@/ui/dialog";
import { ConfirmDialog } from "@/ui/confirm";
import { EmptyState, ErrorState } from "@/ui/states";
import { Skeleton, SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";

export function AppSettingsPage() {
  const { t } = useTranslation();

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-fg text-xl font-semibold tracking-tight">{t("settingsPage.title")}</h1>
        <p className="text-fg-muted mt-1 text-sm">{t("settingsPage.subtitle")}</p>
      </div>

      <GeneralCard />
      <RetentionCard />
      <HistoryRetentionCard />
      <ChannelsCard />
    </div>
  );
}

// ---- general ---------------------------------------------------------------

function GeneralCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: async () => unwrap(await api.GET("/settings")),
  });

  const [appUrl, setAppUrl] = useState("");
  const [sessionTtl, setSessionTtl] = useState("");
  useEffect(() => {
    if (settings.data) {
      setAppUrl(settings.data.appUrl);
      setSessionTtl(String(settings.data.sessionTtlHours));
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/settings", {
          body: { appUrl, sessionTtlHours: Number(sessionTtl) },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      toast("success", t("settingsPage.saved"));
    },
  });

  const error = save.error && isApiError(save.error) ? save.error.message : null;

  return (
    <Card className="max-w-xl">
      {settings.isLoading ? (
        <CardBody className="space-y-4">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </CardBody>
      ) : settings.isError ? (
        <ErrorState error={settings.error} onRetry={() => void settings.refetch()} />
      ) : (
        <>
          <CardHeader title={t("settingsPage.title")} />
          <CardBody>
            <form
              className="space-y-4"
              onSubmit={(e) => {
                e.preventDefault();
                save.mutate();
              }}
            >
              <Field label={t("settingsPage.appUrl")} hint={t("settingsPage.appUrlHint")} error={error}>
                {(p) => (
                  <Input
                    {...p}
                    type="url"
                    placeholder="https://hub.example.com"
                    value={appUrl}
                    onChange={(e) => setAppUrl(e.target.value)}
                  />
                )}
              </Field>
              <Field label={t("settingsPage.sessionTtl")} hint={t("settingsPage.sessionTtlHint")}>
                {(p) => (
                  <Input
                    {...p}
                    type="number"
                    min={1}
                    max={8760}
                    required
                    className="max-w-32"
                    value={sessionTtl}
                    onChange={(e) => setSessionTtl(e.target.value)}
                  />
                )}
              </Field>
              <div className="flex justify-end">
                <Button type="submit" variant="primary" loading={save.isPending}>
                  {save.isPending ? t("common.saving") : t("common.save")}
                </Button>
              </div>
            </form>
          </CardBody>
        </>
      )}
    </Card>
  );
}

// ---- metric retention ------------------------------------------------------

function RetentionCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: async () => unwrap(await api.GET("/settings")),
  });

  const [rawHours, setRawHours] = useState("");
  const [fiveMinDays, setFiveMinDays] = useState("");
  const [hourlyDays, setHourlyDays] = useState("");
  useEffect(() => {
    if (settings.data) {
      setRawHours(String(settings.data.metricsRawRetentionHours));
      setFiveMinDays(String(settings.data.metrics5mRetentionDays));
      setHourlyDays(String(settings.data.metrics1hRetentionDays));
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/settings", {
          body: {
            metricsRawRetentionHours: Number(rawHours),
            metrics5mRetentionDays: Number(fiveMinDays),
            metrics1hRetentionDays: Number(hourlyDays),
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      toast("success", t("settingsPage.saved"));
    },
  });

  const error = save.error && isApiError(save.error) ? save.error.message : null;

  // The general card above surfaces settings load errors with a retry.
  if (settings.isError) return null;

  if (settings.isLoading) {
    return (
      <Card className="max-w-xl">
        <CardBody className="space-y-4">
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
          <Skeleton className="h-9 w-full" />
        </CardBody>
      </Card>
    );
  }

  return (
    <Card className="max-w-xl">
      <CardHeader title={t("settingsPage.retention.title")} description={t("settingsPage.retention.intro")} />
      <CardBody>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field
            label={t("settingsPage.retention.raw")}
            hint={t("settingsPage.retention.rawHint")}
            error={error}
          >
            {(p) => (
              <Input
                {...p}
                type="number"
                min={6}
                max={720}
                required
                className="max-w-32"
                value={rawHours}
                onChange={(e) => setRawHours(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("settingsPage.retention.fiveMin")} hint={t("settingsPage.retention.fiveMinHint")}>
            {(p) => (
              <Input
                {...p}
                type="number"
                min={1}
                max={365}
                required
                className="max-w-32"
                value={fiveMinDays}
                onChange={(e) => setFiveMinDays(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("settingsPage.retention.hourly")} hint={t("settingsPage.retention.hourlyHint")}>
            {(p) => (
              <Input
                {...p}
                type="number"
                min={7}
                max={3650}
                required
                className="max-w-32"
                value={hourlyDays}
                onChange={(e) => setHourlyDays(e.target.value)}
              />
            )}
          </Field>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" loading={save.isPending}>
              {save.isPending ? t("common.saving") : t("common.save")}
            </Button>
          </div>
        </form>
      </CardBody>
    </Card>
  );
}

// ---- history retention -----------------------------------------------------

function HistoryRetentionCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const settings = useQuery({
    queryKey: ["settings"],
    queryFn: async () => unwrap(await api.GET("/settings")),
  });

  const [auditDays, setAuditDays] = useState("");
  const [patchpilotDays, setPatchpilotDays] = useState("");
  useEffect(() => {
    if (settings.data) {
      setAuditDays(String(settings.data.auditRetentionDays));
      setPatchpilotDays(String(settings.data.patchpilotRetentionDays));
    }
  }, [settings.data]);

  const save = useMutation({
    mutationFn: async () =>
      unwrap(
        await api.PATCH("/settings", {
          body: {
            auditRetentionDays: Number(auditDays),
            patchpilotRetentionDays: Number(patchpilotDays),
          },
        }),
      ),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["settings"] });
      toast("success", t("settingsPage.saved"));
    },
  });

  const error = save.error && isApiError(save.error) ? save.error.message : null;

  // The general card surfaces load errors; the metric card shows the skeleton.
  if (settings.isError || settings.isLoading) return null;

  return (
    <Card className="max-w-xl">
      <CardHeader title={t("settingsPage.history.title")} description={t("settingsPage.history.intro")} />
      <CardBody>
        <form
          className="space-y-4"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
        >
          <Field label={t("settingsPage.history.audit")} hint={t("settingsPage.history.auditHint")} error={error}>
            {(p) => (
              <Input
                {...p}
                type="number"
                min={30}
                max={3650}
                required
                className="max-w-32"
                value={auditDays}
                onChange={(e) => setAuditDays(e.target.value)}
              />
            )}
          </Field>
          <Field label={t("settingsPage.history.patchpilot")} hint={t("settingsPage.history.patchpilotHint")}>
            {(p) => (
              <Input
                {...p}
                type="number"
                min={7}
                max={3650}
                required
                className="max-w-32"
                value={patchpilotDays}
                onChange={(e) => setPatchpilotDays(e.target.value)}
              />
            )}
          </Field>
          <div className="flex justify-end">
            <Button type="submit" variant="primary" loading={save.isPending}>
              {save.isPending ? t("common.saving") : t("common.save")}
            </Button>
          </div>
        </form>
      </CardBody>
    </Card>
  );
}

// ---- outbound channels -----------------------------------------------------

type ChannelType = Channel["type"];

export const channelTypeIcon: Record<ChannelType, LucideIcon> = {
  webhook: Webhook,
  email: Mail,
  ntfy: BellRing,
  gotify: Bell,
  telegram: Send,
  discord: MessageSquare,
};

type ChannelFieldSpec = {
  key: string;
  labelKey: string;
  hintKey?: string;
  secret?: boolean;
  required?: boolean;
  numeric?: boolean;
  defaultValue?: string;
};

/** Per-type form fields. Secret keys are stored write-only server-side. */
const channelFields: Record<ChannelType, ChannelFieldSpec[]> = {
  webhook: [
    { key: "url", required: true, labelKey: "channels.fields.url" },
    { key: "authHeader", labelKey: "channels.fields.authHeader", hintKey: "channels.fields.authHeaderHint" },
    { key: "authValue", secret: true, labelKey: "channels.fields.authValue" },
  ],
  email: [
    { key: "host", required: true, labelKey: "channels.fields.smtpHost" },
    { key: "port", required: true, numeric: true, defaultValue: "587", labelKey: "channels.fields.smtpPort" },
    { key: "from", required: true, labelKey: "channels.fields.from" },
    { key: "username", secret: true, labelKey: "channels.fields.username" },
    { key: "password", secret: true, labelKey: "channels.fields.password" },
  ],
  ntfy: [
    { key: "server", required: true, defaultValue: "https://ntfy.sh", labelKey: "channels.fields.server" },
    { key: "topic", required: true, labelKey: "channels.fields.topic" },
    { key: "token", secret: true, labelKey: "channels.fields.token" },
  ],
  gotify: [
    { key: "server", required: true, labelKey: "channels.fields.server" },
    { key: "token", secret: true, required: true, labelKey: "channels.fields.token" },
  ],
  telegram: [
    { key: "chatId", required: true, labelKey: "channels.fields.chatId" },
    { key: "botToken", secret: true, required: true, labelKey: "channels.fields.botToken" },
  ],
  discord: [
    { key: "webhookUrl", secret: true, required: true, labelKey: "channels.fields.webhookUrl", hintKey: "channels.fields.webhookUrlHint" },
  ],
};

function ChannelsCard() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const channels = useQuery({
    queryKey: ["channels"],
    queryFn: async () => unwrap(await api.GET("/channels")),
    retry: (count, err) => !(isApiError(err) && err.status === 403) && count < 2,
  });

  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<Channel | null>(null);
  const [deleting, setDeleting] = useState<Channel | null>(null);
  const [testResults, setTestResults] = useState<Record<string, TestConnectionResult>>({});

  const toggle = useMutation({
    mutationFn: async ({ channel, enabled }: { channel: Channel; enabled: boolean }) =>
      unwrap(
        await api.PATCH("/channels/{id}", {
          params: { path: { id: channel.id } },
          body: { type: channel.type, name: channel.name, config: channel.config, enabled },
        }),
      ),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["channels"] }),
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  const test = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.POST("/channels/{id}/test", { params: { path: { id } } })),
    onSuccess: (result, id) => setTestResults((prev) => ({ ...prev, [id]: result })),
    onError: (err, id) =>
      setTestResults((prev) => ({
        ...prev,
        [id]: { ok: false, message: isApiError(err) ? err.message : t("common.unknownError") },
      })),
  });

  const remove = useMutation({
    mutationFn: async (id: string) =>
      unwrap(await api.DELETE("/channels/{id}", { params: { path: { id } } })),
    onSuccess: () => {
      setDeleting(null);
      toast("success", t("channels.deleted"));
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
    },
    onError: (err) => toast("error", isApiError(err) ? err.message : t("common.unknownError")),
  });

  // Notification channels are admin-only (system/notifications) — anyone
  // else simply doesn't see the card.
  if (channels.isError && isApiError(channels.error) && channels.error.status === 403) return null;

  return (
    <Card className="max-w-2xl">
      <CardHeader
        title={t("channels.title")}
        description={t("channels.intro")}
        actions={
          <Button
            size="sm"
            variant="primary"
            onClick={() => {
              setEditing(null);
              setEditorOpen(true);
            }}
          >
            {t("channels.add")}
          </Button>
        }
      />
      <CardBody className="px-0 py-0">
        {channels.isLoading ? (
          <SkeletonRows rows={2} className="p-5" />
        ) : channels.isError ? (
          <ErrorState error={channels.error} onRetry={() => void channels.refetch()} />
        ) : (channels.data ?? []).length === 0 ? (
          <EmptyState icon={BellRing} title={t("channels.empty.title")} body={t("channels.empty.body")} />
        ) : (
          <ul>
            {channels.data!.map((channel) => {
              const Icon = channelTypeIcon[channel.type];
              const result = testResults[channel.id];
              const testing = test.isPending && test.variables === channel.id;
              return (
                <li key={channel.id} className="border-edge border-b px-5 py-3 last:border-b-0">
                  <div className="flex flex-wrap items-center gap-3">
                    <Icon className="text-fg-subtle size-4 shrink-0" />
                    <div className="min-w-0 flex-1">
                      <p className="text-fg truncate text-sm font-medium">{channel.name}</p>
                      <p className="text-fg-subtle text-xs">{t(`channels.types.${channel.type}`)}</p>
                    </div>
                    <Switch
                      checked={channel.enabled}
                      disabled={toggle.isPending}
                      aria-label={t("channels.toggleAria", { name: channel.name })}
                      onCheckedChange={(enabled) => toggle.mutate({ channel, enabled })}
                    />
                    <Button size="sm" variant="secondary" loading={testing} onClick={() => test.mutate(channel.id)}>
                      {t("channels.test")}
                    </Button>
                    <Button
                      size="icon"
                      variant="ghost"
                      aria-label={t("common.edit")}
                      onClick={() => {
                        setEditing(channel);
                        setEditorOpen(true);
                      }}
                    >
                      <Pencil className="size-3.5" />
                    </Button>
                    <Button
                      size="icon"
                      variant="danger-ghost"
                      aria-label={t("common.delete")}
                      onClick={() => setDeleting(channel)}
                    >
                      <Trash2 className="size-3.5" />
                    </Button>
                  </div>
                  {result && (
                    <p
                      className={`mt-2 flex items-center gap-1.5 text-[13px] ${result.ok ? "text-success-soft-fg" : "text-danger-soft-fg"}`}
                      role="status"
                    >
                      {result.ok ? (
                        <CheckCircle2 className="size-3.5 shrink-0" />
                      ) : (
                        <XCircle className="size-3.5 shrink-0" />
                      )}
                      {result.message || (result.ok ? t("channels.testOk") : t("channels.testFailed"))}
                    </p>
                  )}
                </li>
              );
            })}
          </ul>
        )}
      </CardBody>

      <ChannelEditor open={editorOpen} onOpenChange={setEditorOpen} channel={editing} />

      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        title={t("channels.deleteConfirmTitle", { name: deleting?.name ?? "" })}
        body={t("channels.deleteConfirmBody")}
        confirmLabel={t("common.delete")}
        loading={remove.isPending}
        onConfirm={() => deleting && remove.mutate(deleting.id)}
      />
    </Card>
  );
}

function ChannelEditor({
  open,
  onOpenChange,
  channel,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  channel: Channel | null;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();

  const [type, setType] = useState<ChannelType | null>(null);
  const [name, setName] = useState("");
  const [values, setValues] = useState<Record<string, string>>({});

  useEffect(() => {
    if (!open) return;
    if (channel) {
      setType(channel.type);
      setName(channel.name);
      const seeded: Record<string, string> = {};
      for (const spec of channelFields[channel.type]) {
        if (!spec.secret) {
          const v = channel.config[spec.key];
          seeded[spec.key] = v == null ? "" : String(v);
        }
      }
      setValues(seeded);
    } else {
      setType(null);
      setName("");
      setValues({});
    }
  }, [open, channel]);

  const pickType = (next: ChannelType) => {
    setType(next);
    const seeded: Record<string, string> = {};
    for (const spec of channelFields[next]) {
      if (spec.defaultValue) seeded[spec.key] = spec.defaultValue;
    }
    setValues(seeded);
  };

  const save = useMutation({
    mutationFn: async (body: ChannelInput) =>
      channel
        ? unwrap(await api.PATCH("/channels/{id}", { params: { path: { id: channel.id } }, body }))
        : unwrap(await api.POST("/channels", { body })),
    onSuccess: () => {
      toast("success", channel ? t("channels.updated") : t("channels.created"));
      void queryClient.invalidateQueries({ queryKey: ["channels"] });
      onOpenChange(false);
    },
  });

  const specs = useMemo(() => (type ? channelFields[type] : []), [type]);

  const valid =
    type !== null &&
    name.trim().length > 0 &&
    specs.every((spec) => {
      if (!spec.required) return true;
      // Required secrets may stay empty on edit ("leave empty to keep").
      if (spec.secret && channel) return true;
      return (values[spec.key] ?? "").trim().length > 0;
    });

  const submit = () => {
    if (!valid || !type) return;
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
    save.mutate({
      type,
      name: name.trim(),
      config,
      enabled: channel?.enabled ?? true,
      ...(Object.keys(secrets).length > 0 ? { secrets } : {}),
    });
  };

  const apiError = save.error && isApiError(save.error) ? save.error.message : null;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent
        title={channel ? t("channels.editTitle", { name: channel.name }) : t("channels.add")}
        description={type === null ? t("channels.pickType") : undefined}
        footer={
          type !== null ? (
            <>
              <Button variant="ghost" onClick={() => onOpenChange(false)}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" disabled={!valid} loading={save.isPending} onClick={submit}>
                {channel ? t("common.save") : t("common.create")}
              </Button>
            </>
          ) : undefined
        }
      >
        {type === null ? (
          <div className="grid grid-cols-2 gap-2">
            {(Object.keys(channelFields) as ChannelType[]).map((option) => {
              const Icon = channelTypeIcon[option];
              return (
                <button
                  key={option}
                  type="button"
                  onClick={() => pickType(option)}
                  className="border-edge hover:border-edge-strong hover:bg-surface-hover flex cursor-pointer items-center gap-2.5 rounded-lg border px-3.5 py-3 text-left transition-colors"
                >
                  <Icon className="text-fg-subtle size-4.5 shrink-0" />
                  <span className="text-fg text-sm font-medium">{t(`channels.types.${option}`)}</span>
                </button>
              );
            })}
          </div>
        ) : (
          <div className="space-y-4">
            {(type === "email" || type === "telegram") && (
              <p className="bg-info-soft text-info-soft-fg rounded-lg px-4 py-3 text-[13px]">
                {t(`channels.${type}.note`)}
              </p>
            )}
            <Field label={t("common.name")} error={apiError}>
              {(p) => (
                <Input
                  {...p}
                  autoFocus
                  placeholder={t("channels.namePlaceholder")}
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              )}
            </Field>
            {specs.map((spec) => {
              const hint = spec.secret && channel ? t("channels.secretKeep") : spec.hintKey ? t(spec.hintKey) : undefined;
              const optional = !spec.required || (spec.secret && channel !== null);
              const onChange = (e: React.ChangeEvent<HTMLInputElement>) =>
                setValues((prev) => ({ ...prev, [spec.key]: e.target.value }));
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
                        value={values[spec.key] ?? ""}
                        onChange={onChange}
                      />
                    )
                  }
                </Field>
              );
            })}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}
