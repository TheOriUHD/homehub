import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { CircleCheck, CircleX, Radar, Send, Settings2, Sparkles } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { AssistantChatPane } from "@/components/assistant-chat";
import { TelegramDialog } from "./assistant-telegram";
import { Button } from "@/ui/button";
import { Card, CardBody } from "@/ui/card";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { EmptyState, ErrorState } from "@/ui/states";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import { useAssistantModels } from "@/components/assistant-model-picker";

/**
 * The assistant: chat over the LLM tool loop. Every tool call runs under
 * the signed-in user's own permissions, so the conversation can never see
 * more than its human. Answers come from live tools, and each assistant
 * message wears chips naming the tools it used — the "how it knows" trace.
 */
export function AssistantPage() {
  const { t } = useTranslation();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [telegramOpen, setTelegramOpen] = useState(false);
  const { active: activeModel } = useAssistantModels();

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
    // Locked to the viewport (screen minus the 3.5rem header and the main
    // area's py-6): the page never scrolls; past messages scroll inside
    // the conversation card.
    <div className="flex h-[calc(100dvh-6.5rem)] min-h-0 flex-col space-y-5">
      <div className="flex flex-wrap items-center gap-3">
        <span className="bg-accent-soft text-accent-soft-fg flex size-10 items-center justify-center rounded-xl">
          <Sparkles className="size-5" />
        </span>
        <div className="min-w-0 flex-1">
          <h1 className="text-fg text-xl font-semibold tracking-tight">{t("assistant.title")}</h1>
          <p className="text-fg-muted text-sm">
            {info.enabled
              ? t("assistant.subtitle", {
                  // The header names the model actually answering, which the
                  // composer's switcher may have moved off the instance default.
                  model: activeModel?.label ?? info.model,
                  provider: info.providers.find((p) => p.id === info.provider)?.label ?? info.provider,
                })
              : t("assistant.disabledSubtitle")}
          </p>
        </div>
        <Button size="sm" variant="ghost" onClick={() => setTelegramOpen(true)}>
          <Send className="size-3.5" />
          {t("assistant.telegram.button")}
        </Button>
        {info.canConfigure && (
          <Button size="sm" variant="ghost" onClick={() => setSettingsOpen(true)}>
            <Settings2 className="size-3.5" />
            {t("assistant.settings")}
          </Button>
        )}
      </div>

      {info.enabled ? (
        <Card className="flex min-h-0 flex-1 flex-col">
          <CardBody className="flex min-h-0 flex-1 flex-col">
            <AssistantChatPane variant="page" />
          </CardBody>
        </Card>
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

      {telegramOpen && <TelegramDialog canConfigure={info.canConfigure} onClose={() => setTelegramOpen(false)} />}
      {settingsOpen && (
        <AssistantSettingsDialog
          info={info}
          onClose={() => setSettingsOpen(false)}
        />
      )}
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
    keysSet: string[];
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
          {selected?.kind === "local" && (
            <EndpointVerify
              baseUrl={baseUrl || selected?.defaultBaseUrl || ""}
              provider={provider}
              draftKey={apiKey}
              onModel={(m) => setModel(m)}
            />
          )}
          {provider === "lmstudio" && (
            <>
              <LmStudioFinder
                onPick={(url, model) => {
                  setBaseUrl(url);
                  if (model) setModel(model);
                }}
              />
              <div className="bg-bg-subtle border-edge rounded-lg border px-3 py-2.5">
                <p className="text-fg mb-1 text-[12.5px] font-medium">{t("assistant.lmGuideTitle")}</p>
                <ol className="text-fg-muted ml-4 list-decimal space-y-0.5 text-[12px]">
                  <li>{t("assistant.lmGuide1")}</li>
                  <li>{t("assistant.lmGuide2")}</li>
                  <li>{t("assistant.lmGuide3")}</li>
                  <li>{t("assistant.lmGuide4")}</li>
                </ol>
              </div>
            </>
          )}
          <Field
            label={t("assistant.apiKey")}
            hint={
              info.keysSet.includes(provider)
                ? t("assistant.apiKeyStoredHint", { provider: selected?.label ?? provider })
                : selected && !selected.requiresKey
                  ? t("assistant.apiKeyOptionalHint")
                  : t("assistant.apiKeyHint")
            }
          >
            {(p) => (
              <Input
                {...p}
                type="password"
                placeholder={info.keysSet.includes(provider) ? "••••••••" : t("assistant.apiKeyPlaceholder")}
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


/**
 * One-click LAN sweep for LM Studio: the server probes its attached /24
 * networks for the :1234 /v1/models signature; a result click fills the
 * endpoint and loaded model in.
 */
function LmStudioFinder({ onPick }: { onPick: (baseUrl: string, model?: string) => void }) {
  const { t } = useTranslation();
  const scan = useMutation({
    mutationFn: async () => unwrap(await api.POST("/assistant/discover")),
  });

  return (
    <div className="space-y-2">
      <Button size="sm" variant="secondary" loading={scan.isPending} onClick={() => scan.mutate()}>
        <Radar className="size-3.5" />
        {scan.isPending ? t("assistant.scanning") : t("assistant.scanNetwork")}
      </Button>
      {scan.isSuccess && scan.data.instances.length === 0 && (
        <p className="text-fg-muted text-[12.5px]">{t("assistant.scanEmpty")}</p>
      )}
      {scan.isSuccess &&
        scan.data.instances.map((inst) => (
          <button
            key={inst.baseUrl}
            type="button"
            onClick={() => onPick(inst.baseUrl, inst.models[0])}
            className="border-edge bg-surface hover:border-accent/60 hover:bg-surface-hover block w-full rounded-lg border px-3 py-2 text-left transition-colors"
          >
            <p className="text-fg font-mono text-[12.5px]">{inst.baseUrl}</p>
            <p className={inst.requiresKey ? "text-warning truncate text-[11.5px]" : "text-fg-muted truncate text-[11.5px]"}>
              {inst.requiresKey
                ? t("assistant.scanLocked")
                : inst.models.length > 0
                  ? inst.models.join(" · ")
                  : t("assistant.scanNoModels")}
            </p>
          </button>
        ))}
      {scan.isError && (
        <p className="text-danger text-[12.5px]">
          {isApiError(scan.error) ? scan.error.message : t("common.unknownError")}
        </p>
      )}
    </div>
  );
}


/**
 * Server-side probe of the entered endpoint: green with the loaded models,
 * or the exact diagnosis ("needs /v1", "nothing answered", …). The check
 * runs from the HomeHub host — the same vantage point the assistant uses.
 */
function EndpointVerify({
  baseUrl,
  provider,
  draftKey,
  onModel,
}: {
  baseUrl: string;
  provider: string;
  draftKey: string;
  onModel: (model: string) => void;
}) {
  const { t } = useTranslation();
  const verify = useMutation({
    // The probe authenticates like the chat would: a key typed but not
    // yet saved wins, else the server uses this provider's stored key.
    mutationFn: async () =>
      unwrap(
        await api.POST("/assistant/verify-endpoint", {
          body: { baseUrl, provider, apiKey: draftKey.trim() || undefined },
        }),
      ),
  });

  return (
    <div className="space-y-1.5">
      <Button size="sm" variant="secondary" loading={verify.isPending} disabled={!baseUrl.trim()} onClick={() => verify.mutate()}>
        <CircleCheck className="size-3.5" />
        {t("assistant.testEndpoint")}
      </Button>
      {verify.isSuccess && verify.data.ok && (
        <p className="text-success flex items-start gap-1.5 text-[12.5px]">
          <CircleCheck className="mt-0.5 size-3.5 shrink-0" />
          <span>
            {verify.data.models.length > 0
              ? t("assistant.endpointOkModels", { models: verify.data.models.join(", ") })
              : t("assistant.endpointOkEmpty")}
            {verify.data.models.length > 0 && (
              <button
                type="button"
                className="text-accent ml-1 underline underline-offset-2"
                onClick={() => onModel(verify.data.models[0])}
              >
                {t("assistant.useModel", { model: verify.data.models[0] })}
              </button>
            )}
          </span>
        </p>
      )}
      {verify.isSuccess && !verify.data.ok && (
        <p className="text-danger flex items-start gap-1.5 text-[12.5px]">
          <CircleX className="mt-0.5 size-3.5 shrink-0" />
          {verify.data.message}
        </p>
      )}
    </div>
  );
}
