import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Settings2, Sparkles } from "lucide-react";
import { api, isApiError, unwrap } from "@/lib/api";
import { AssistantChatPane } from "@/components/assistant-chat";
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
