import { useEffect, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Eraser, Send, Settings2, Sparkles, Wrench } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Card, CardBody, CardHeader } from "@/ui/card";
import { ConfirmDialog } from "@/ui/confirm";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";

/**
 * The assistant: chat over the LLM tool loop. Every tool call runs under
 * the signed-in user's own permissions, so the conversation can never see
 * more than its human. Answers come from live tools, and each assistant
 * message wears chips naming the tools it used — the "how it knows" trace.
 */
export function AssistantPage() {
  const { t } = useTranslation();
  const [settingsOpen, setSettingsOpen] = useState(false);

  const state = useQuery({
    queryKey: ["assistant"],
    queryFn: async () => unwrap(await api.GET("/assistant")),
  });

  if (state.isLoading) return <PageLoader />;
  if (state.isError) {
    return (
      <Card>
        <ErrorState error={state.error} onRetry={() => void state.refetch()} />
      </Card>
    );
  }
  const info = state.data!;

  return (
    <div className="flex h-full min-h-0 flex-col space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Sparkles className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("assistant.title")}</h1>
          <p className="text-fg-muted text-sm">
            {info.enabled
              ? t("assistant.subtitle", {
                  model: info.model,
                  provider: info.providers.find((p) => p.id === info.provider)?.label ?? info.provider,
                })
              : t("assistant.disabledSubtitle")}
          </p>
        </div>
        {info.canConfigure && (
          <Button size="sm" variant="ghost" onClick={() => setSettingsOpen(true)}>
            <Settings2 className="size-3.5" />
            {t("assistant.settings")}
          </Button>
        )}
      </div>

      {info.enabled ? (
        <ChatPane />
      ) : (
        <Card>
          <EmptyState
            icon={Sparkles}
            title={t("assistant.offTitle")}
            body={info.canConfigure ? t("assistant.offBodyAdmin") : t("assistant.offBody")}
            action={
              info.canConfigure ? (
                <Button variant="primary" onClick={() => setSettingsOpen(true)}>
                  {t("assistant.setUp")}
                </Button>
              ) : undefined
            }
          />
        </Card>
      )}

      {settingsOpen && (
        <AssistantSettingsDialog
          info={info}
          onClose={() => setSettingsOpen(false)}
        />
      )}
    </div>
  );
}

type HistoryItem = {
  id: number;
  role: "user" | "assistant";
  content: string;
  tools?: string[];
  ts: string;
  pending?: boolean;
};

function ChatPane() {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);

  const history = useQuery({
    queryKey: ["assistant", "history"],
    queryFn: async () => unwrap(await api.GET("/assistant/history")),
  });

  const send = useMutation({
    mutationFn: async (message: string) =>
      unwrap(await api.POST("/assistant/chat", { body: { message } })),
    onSuccess: () => void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] }),
    onError: (err) => {
      void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
      toast("error", t("assistant.failed"), isApiError(err) ? err.message : t("common.unknownError"));
    },
  });

  const clear = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/assistant/history"));
    },
    onSuccess: () => {
      setConfirmClear(false);
      void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
    },
  });

  const items: HistoryItem[] = [...((history.data as HistoryItem[] | undefined) ?? [])];
  if (send.isPending && send.variables) {
    items.push({ id: -1, role: "user", content: send.variables, ts: "", pending: false });
    items.push({ id: -2, role: "assistant", content: "", ts: "", pending: true });
  }

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [items.length, send.isPending]);

  const submit = () => {
    const message = draft.trim();
    if (!message || send.isPending) return;
    setDraft("");
    send.mutate(message);
  };

  return (
    <Card className="flex min-h-0 flex-1 flex-col">
      <CardHeader
        title={t("assistant.chatTitle")}
        actions={
          items.length > 0 ? (
            <Button size="sm" variant="ghost" onClick={() => setConfirmClear(true)}>
              <Eraser className="size-3.5" />
              {t("assistant.clear")}
            </Button>
          ) : undefined
        }
      />
      <CardBody className="flex min-h-0 flex-1 flex-col gap-3">
        <div className="min-h-[300px] flex-1 space-y-3 overflow-y-auto pr-1">
          {history.isLoading && <PageLoader className="py-10" />}
          {!history.isLoading && items.length === 0 && (
            <div className="text-fg-muted flex h-full flex-col items-center justify-center gap-1 py-10 text-center text-sm">
              <Sparkles className="text-fg-subtle mb-1 size-5" />
              {t("assistant.emptyHint")}
              <span className="text-fg-subtle text-[12.5px]">{t("assistant.emptyExamples")}</span>
            </div>
          )}
          {items.map((item) => (
            <MessageBubble key={item.id} item={item} />
          ))}
          <div ref={bottomRef} />
        </div>

        <div className="flex items-end gap-2">
          <Input
            value={draft}
            placeholder={t("assistant.placeholder")}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                submit();
              }
            }}
          />
          <Button variant="primary" disabled={!draft.trim() || send.isPending} onClick={submit} aria-label={t("assistant.send")}>
            <Send className="size-4" />
          </Button>
        </div>
      </CardBody>

      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title={t("assistant.clearTitle")}
        body={t("assistant.clearBody")}
        confirmLabel={t("assistant.clear")}
        loading={clear.isPending}
        onConfirm={() => clear.mutate()}
      />
    </Card>
  );
}

function MessageBubble({ item }: { item: HistoryItem }) {
  const { t } = useTranslation();
  const isUser = item.role === "user";
  return (
    <div className={cn("flex", isUser ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-xl px-3.5 py-2.5 text-sm",
          isUser ? "bg-accent text-accent-fg" : "bg-bg-subtle text-fg",
        )}
      >
        {item.pending ? (
          <span className="text-fg-muted flex items-center gap-2 text-[13px]">
            <Sparkles className="size-3.5 animate-pulse" />
            {t("assistant.thinking")}
          </span>
        ) : (
          <p className="whitespace-pre-wrap">{item.content}</p>
        )}
        {!isUser && (item.tools?.length ?? 0) > 0 && (
          <div className="mt-2 flex flex-wrap gap-1">
            {[...new Set(item.tools)].map((tool) => (
              <Badge key={tool} tone="neutral">
                <Wrench className="size-2.5" />
                {tool}
              </Badge>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

type ProviderInfo = {
  id: string;
  label: string;
  kind: "cloud" | "local";
  requiresKey: boolean;
  defaultModel: string;
  defaultBaseUrl?: string;
  modelHint?: string;
};

function AssistantSettingsDialog({
  info,
  onClose,
}: {
  info: {
    enabled: boolean;
    provider: string;
    model: string;
    baseUrl: string;
    keySet: boolean;
    providers: ProviderInfo[];
  };
  onClose: () => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [enabled, setEnabled] = useState(info.enabled);
  const [provider, setProvider] = useState(info.provider);
  const [model, setModel] = useState(info.model);
  const [baseUrl, setBaseUrl] = useState(info.baseUrl);
  const [apiKey, setApiKey] = useState("");

  const selected = info.providers.find((p) => p.id === provider);
  // Switching providers adopts that provider's defaults — the common case
  // is trying a different runtime, not carrying a model name across.
  const pickProvider = (id: string) => {
    setProvider(id);
    const next = info.providers.find((p) => p.id === id);
    if (next) {
      setModel(next.defaultModel);
      setBaseUrl(next.defaultBaseUrl ?? "");
    }
  };

  const save = useMutation({
    mutationFn: async () => {
      unwrap(
        await api.PUT("/assistant/settings", {
          body: {
            enabled,
            provider,
            model: model.trim(),
            baseUrl: baseUrl.trim() || undefined,
            apiKey: apiKey.trim() || undefined,
          },
        }),
      );
    },
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["assistant"] });
      toast("success", t("assistant.saved"));
      onClose();
    },
    onError: (err) =>
      toast("error", t("common.unknownError"), isApiError(err) ? err.message : undefined),
  });

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        title={t("assistant.settingsTitle")}
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
        <div className="space-y-4">
          <div className="flex items-center justify-between gap-4">
            <div>
              <p className="text-fg text-sm font-medium">{t("assistant.enable")}</p>
              <p className="text-fg-muted text-[13px]">{t("assistant.enableHint")}</p>
            </div>
            <Switch checked={enabled} onCheckedChange={setEnabled} aria-label={t("assistant.enable")} />
          </div>
          <Field label={t("assistant.provider")} hint={t("assistant.providerHint")}>
            {(p) => (
              <Select value={provider} onValueChange={pickProvider}>
                <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <p className="text-fg-subtle px-2 pt-1.5 pb-1 text-[10.5px] font-semibold tracking-wide uppercase">
                    {t("assistant.groupCloud")}
                  </p>
                  {info.providers
                    .filter((entry) => entry.kind === "cloud")
                    .map((entry) => (
                      <SelectItem key={entry.id} value={entry.id}>
                        {entry.label}
                      </SelectItem>
                    ))}
                  <p className="text-fg-subtle px-2 pt-2 pb-1 text-[10.5px] font-semibold tracking-wide uppercase">
                    {t("assistant.groupLocal")}
                  </p>
                  {info.providers
                    .filter((entry) => entry.kind === "local")
                    .map((entry) => (
                      <SelectItem key={entry.id} value={entry.id}>
                        {entry.label}
                      </SelectItem>
                    ))}
                </SelectContent>
              </Select>
            )}
          </Field>
          <Field label={t("assistant.model")} hint={selected?.modelHint || t("assistant.modelHint")}>
            {(p) => <Input {...p} value={model} onChange={(e) => setModel(e.target.value)} />}
          </Field>
          {(selected?.kind === "local" || selected?.defaultBaseUrl) && (
            <Field label={t("assistant.baseUrl")} hint={t("assistant.baseUrlHint")}>
              {(p) => (
                <Input
                  {...p}
                  placeholder={selected?.defaultBaseUrl}
                  value={baseUrl}
                  onChange={(e) => setBaseUrl(e.target.value)}
                />
              )}
            </Field>
          )}
          <Field
            label={t("assistant.apiKey")}
            hint={
              selected && !selected.requiresKey
                ? t("assistant.apiKeyOptionalHint")
                : info.keySet
                  ? t("assistant.apiKeySetHint")
                  : t("assistant.apiKeyHint")
            }
          >
            {(p) => (
              <Input
                {...p}
                type="password"
                placeholder={info.keySet ? "••••••••" : t("assistant.apiKeyPlaceholder")}
                value={apiKey}
                onChange={(e) => setApiKey(e.target.value)}
              />
            )}
          </Field>
          <p className="text-fg-muted text-[12.5px]">{t("assistant.scopeNote")}</p>
        </div>
      </DialogContent>
    </Dialog>
  );
}
