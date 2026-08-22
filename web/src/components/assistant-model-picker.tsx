import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, Sparkles, TriangleAlert, Zap } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Dropdown, DropdownContent, DropdownItem, DropdownTrigger } from "@/ui/dropdown";

/**
 * The model switcher: a composer-bar pill that changes which model answers the
 * NEXT turn. It never writes the instance-wide default — that stays an
 * administrator's setting — so switching is local, immediate, and mid-
 * conversation, and the thread keeps its history across the switch.
 *
 * Expensive models sit behind a spend gate. A premium model asks once; an
 * ultra one also makes you wait a few seconds, because the whole point is that
 * the price registers before you click rather than after the bill does.
 */

export type AssistantModel = {
  id: string;
  label: string;
  blurb: string;
  tier?: "premium" | "ultra";
  costNote?: string;
  aliases?: string[];
};

/** How long the ultra-tier confirm button stays disabled. */
const ULTRA_WAIT_SECONDS = 5;

/** The chosen model rides the app session, like the conversation it belongs to. */
const STORAGE_KEY = "hh-assistant-model";

export function loadModelChoice(): string {
  try {
    return sessionStorage.getItem(STORAGE_KEY) ?? "";
  } catch {
    return "";
  }
}

function saveModelChoice(id: string) {
  try {
    sessionStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* private mode — the choice lasts for this view only */
  }
}

/** useAssistantModels resolves the catalog and the model in effect right now. */
export function useAssistantModels() {
  const settings = useQuery({
    queryKey: ["assistant"],
    queryFn: async () => unwrap(await api.GET("/assistant")),
    staleTime: 60_000,
  });
  const [chosen, setChosen] = useState<string>(() => loadModelChoice());

  const provider = settings.data?.providers?.find((p) => p.id === settings.data?.provider);
  const models = useMemo(() => (provider?.models ?? []) as AssistantModel[], [provider]);

  // Match the configured default through aliases too, so an instance pinned to
  // a dated snapshot still highlights that model instead of showing nothing.
  const configured = settings.data?.model ?? "";
  const matches = (m: AssistantModel, id: string) => m.id === id || (m.aliases ?? []).includes(id);
  const active =
    models.find((m) => matches(m, chosen)) ?? models.find((m) => matches(m, configured)) ?? null;

  // A stored choice from another provider must not silently keep applying.
  useEffect(() => {
    if (chosen && models.length > 0 && !models.some((m) => matches(m, chosen))) {
      setChosen("");
      saveModelChoice("");
    }
  }, [chosen, models]);

  const select = (id: string) => {
    setChosen(id);
    saveModelChoice(id);
  };

  return { models, active, select, ready: models.length > 0 };
}

export function AssistantModelPicker({ compact }: { compact?: boolean }) {
  const { t } = useTranslation();
  const { models, active, select, ready } = useAssistantModels();
  const [gated, setGated] = useState<AssistantModel | null>(null);

  if (!ready) return null;

  const choose = (model: AssistantModel) => {
    if (model.tier) setGated(model);
    else select(model.id);
  };

  return (
    <>
      <Dropdown>
        <DropdownTrigger asChild>
          <button
            type="button"
            aria-label={t("assistant.model.switch")}
            className="text-fg-muted hover:bg-surface-hover hover:text-fg flex cursor-pointer items-center gap-1.5 rounded-lg px-2 py-1 text-[12.5px] font-medium transition-colors"
          >
            <Sparkles className="size-3.5 shrink-0" />
            <span className="max-w-40 truncate">{active?.label ?? t("assistant.model.pick")}</span>
            <ChevronDown className="size-3.5 shrink-0 opacity-60" />
          </button>
        </DropdownTrigger>
        <DropdownContent align="start" side="top" className={cn("w-72", compact && "w-64")}>
          {models.map((model) => {
            const current = model.id === active?.id;
            return (
              <DropdownItem
                key={model.id}
                onSelect={() => choose(model)}
                className="items-start gap-2.5 py-2"
              >
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="flex items-center gap-1.5">
                    <span className="text-fg text-[13px] font-medium">{model.label}</span>
                    {model.tier && <CostBadge tier={model.tier} />}
                  </span>
                  <span className="text-fg-subtle text-[11.5px] leading-snug">{model.blurb}</span>
                </span>
                {current && <Check className="text-accent mt-0.5 size-4 shrink-0" />}
              </DropdownItem>
            );
          })}
        </DropdownContent>
      </Dropdown>

      <CostGateDialog
        model={gated}
        onCancel={() => setGated(null)}
        onConfirm={() => {
          if (gated) select(gated.id);
          setGated(null);
        }}
      />
    </>
  );
}

function CostBadge({ tier }: { tier: "premium" | "ultra" }) {
  const { t } = useTranslation();
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1 rounded-full px-1.5 py-px text-[10px] font-semibold",
        tier === "ultra" ? "bg-danger-soft text-danger-soft-fg" : "bg-warning-soft text-warning-soft-fg",
      )}
    >
      <Zap className="size-2.5" />
      {t(tier === "ultra" ? "assistant.model.ultraBadge" : "assistant.model.premiumBadge")}
    </span>
  );
}

/**
 * The spend gate. Ultra-tier models hold the confirm button disabled for a few
 * seconds — a deliberate speed bump, not decoration, so the multiplier is read
 * rather than clicked past.
 */
function CostGateDialog({
  model,
  onCancel,
  onConfirm,
}: {
  model: AssistantModel | null;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const { t } = useTranslation();
  const ultra = model?.tier === "ultra";
  const [remaining, setRemaining] = useState(0);

  useEffect(() => {
    if (!model) return;
    if (!ultra) {
      setRemaining(0);
      return;
    }
    setRemaining(ULTRA_WAIT_SECONDS);
    const timer = window.setInterval(() => {
      setRemaining((left) => {
        if (left <= 1) {
          window.clearInterval(timer);
          return 0;
        }
        return left - 1;
      });
    }, 1000);
    return () => window.clearInterval(timer);
  }, [model, ultra]);

  if (!model) return null;

  return (
    <Dialog open onOpenChange={(next) => !next && onCancel()}>
      <DialogContent
        title={t("assistant.model.confirmTitle", { model: model.label })}
        description={
          <span className="flex flex-col gap-2">
            <span
              className={cn(
                "flex items-start gap-2 rounded-lg px-3 py-2 text-[13px]",
                ultra ? "bg-danger-soft text-danger-soft-fg" : "bg-warning-soft text-warning-soft-fg",
              )}
            >
              <TriangleAlert className="mt-0.5 size-4 shrink-0" />
              <span>{model.costNote}</span>
            </span>
            <span className="text-fg-muted text-[13px]">
              {t("assistant.model.confirmBody", { model: model.label })}
            </span>
          </span>
        }
        footer={
          <>
            <Button variant="ghost" onClick={onCancel}>
              {t("common.cancel")}
            </Button>
            <Button
              variant={ultra ? "danger" : "primary"}
              disabled={remaining > 0}
              onClick={onConfirm}
            >
              {remaining > 0
                ? t("assistant.model.confirmWait", { seconds: remaining })
                : t("assistant.model.confirmAction", { model: model.label })}
            </Button>
          </>
        }
      />
    </Dialog>
  );
}
