import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { useQuery } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { Check, ChevronDown, Sparkles, TriangleAlert, Zap } from "lucide-react";
import { api, unwrap } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import {
  Dropdown,
  DropdownContent,
  DropdownItem,
  DropdownSeparator,
  DropdownTrigger,
} from "@/ui/dropdown";

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
  autoLane?: "basic" | "advanced";
  aliases?: string[];
};

/** The picker sentinel for rule-based routing. */
export const MODEL_AUTO = "auto";

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

/**
 * The choice is one value shared by every component that shows it — the
 * picker in the composer and the model name in the page header. Holding it in
 * component state gave each its own copy, so switching models in the composer
 * left the header naming the old one until a reload.
 */
let current = loadModelChoice();
const listeners = new Set<() => void>();

function subscribe(fn: () => void) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function saveModelChoice(id: string) {
  current = id;
  try {
    sessionStorage.setItem(STORAGE_KEY, id);
  } catch {
    /* private mode — the choice lasts for this view only */
  }
  for (const fn of listeners) fn();
}

/** useAssistantModels resolves the catalog and the model in effect right now. */
export function useAssistantModels() {
  const settings = useQuery({
    queryKey: ["assistant"],
    queryFn: async () => unwrap(await api.GET("/assistant")),
    staleTime: 60_000,
  });
  const chosen = useSyncExternalStore(subscribe, () => current);

  const provider = settings.data?.providers?.find((p) => p.id === settings.data?.provider);
  const models = useMemo(() => (provider?.models ?? []) as AssistantModel[], [provider]);
  const autoAvailable = settings.data?.autoAvailable ?? false;
  // Which models Auto would route to, so the picker can say so out loud
  // instead of asking the reader to trust it.
  const lanes = useMemo(() => {
    const named = (lane: "basic" | "advanced") => models.find((m) => m.autoLane === lane)?.label;
    return {
      basic: named("basic") ?? settings.data?.autoBasic ?? "",
      advanced: named("advanced") ?? settings.data?.autoAdvanced ?? "",
    };
  }, [models, settings.data]);

  // Match the configured default through aliases too, so an instance pinned to
  // a dated snapshot still highlights that model instead of showing nothing.
  const configured = settings.data?.model ?? "";
  const matches = (m: AssistantModel, id: string) => m.id === id || (m.aliases ?? []).includes(id);
  const isAuto = chosen === MODEL_AUTO && autoAvailable;
  const active = isAuto
    ? null
    : models.find((m) => matches(m, chosen)) ?? models.find((m) => matches(m, configured)) ?? null;

  // A stored choice that this provider cannot serve must not keep applying —
  // including Auto, once its lanes are no longer configured.
  useEffect(() => {
    if (chosen === MODEL_AUTO) {
      if (settings.data && !autoAvailable) saveModelChoice("");
      return;
    }
    if (chosen && models.length > 0 && !models.some((m) => matches(m, chosen))) {
      saveModelChoice("");
    }
  }, [chosen, models, autoAvailable, settings.data]);

  const select = useCallback((id: string) => saveModelChoice(id), []);

  return { models, active, isAuto, autoAvailable, lanes, select, ready: models.length > 0 || autoAvailable };
}

export function AssistantModelPicker({ compact }: { compact?: boolean }) {
  const { t } = useTranslation();
  const { models, active, isAuto, autoAvailable, lanes, select, ready } = useAssistantModels();
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
            <Sparkles className={cn("size-3.5 shrink-0", isAuto && "text-accent")} />
            <span className="max-w-40 truncate">
              {isAuto ? t("assistant.model.auto") : (active?.label ?? t("assistant.model.pick"))}
            </span>
            <ChevronDown className="size-3.5 shrink-0 opacity-60" />
          </button>
        </DropdownTrigger>
        <DropdownContent align="start" side="top" className={cn("w-72", compact && "w-64")}>
          {autoAvailable && (
            <DropdownItem onSelect={() => select(MODEL_AUTO)} className="items-start gap-2.5 py-2">
              <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                <span className="flex items-center gap-1.5">
                  <Sparkles className="text-accent size-3.5" />
                  <span className="text-fg text-[13px] font-medium">{t("assistant.model.auto")}</span>
                </span>
                <span className="text-fg-subtle text-[11.5px] leading-snug">
                  {t("assistant.model.autoBlurb", { basic: lanes.basic, advanced: lanes.advanced })}
                </span>
              </span>
              {isAuto && <Check className="text-accent mt-0.5 size-4 shrink-0" />}
            </DropdownItem>
          )}
          {autoAvailable && models.length > 0 && <DropdownSeparator />}
          {models.map((model) => {
            const current = !isAuto && model.id === active?.id;
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
        centered
        title={t("assistant.model.confirmTitle", { model: model.label })}
        description={t("assistant.model.confirmBody", { model: model.label })}
        icon={
          <span
            className={cn(
              "flex size-11 items-center justify-center rounded-full",
              ultra ? "bg-danger-soft text-danger-soft-fg" : "bg-warning-soft text-warning-soft-fg",
            )}
          >
            <TriangleAlert className="size-5" />
          </span>
        }
        footer={
          <>
            <Button variant="secondary" onClick={onCancel}>
              {t("common.cancel")}
            </Button>
            <Button variant={ultra ? "danger" : "primary"} disabled={remaining > 0} onClick={onConfirm}>
              {remaining > 0
                ? t("assistant.model.confirmWait", { seconds: remaining })
                : t("assistant.model.confirmAction", { model: model.label })}
            </Button>
          </>
        }
      >
        {/* The number is the whole point of the dialog, so it gets its own
            line rather than being folded into the prose. */}
        <p
          className={cn(
            "rounded-lg px-3 py-2 text-center text-[13px] font-medium",
            ultra ? "bg-danger-soft text-danger-soft-fg" : "bg-warning-soft text-warning-soft-fg",
          )}
        >
          {model.costNote}
        </p>
      </DialogContent>
    </Dialog>
  );
}
