import { useTranslation } from "react-i18next";
import { Plus, Trash2 } from "lucide-react";
import type { GrantInput } from "@/lib/api";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Tooltip } from "@/ui/tooltip";
import { cn } from "@/lib/utils";

const LEVELS = ["view", "control", "configure", "delete"] as const;

/**
 * GrantEditor edits a list of grants: resource selector, level set, effect.
 * Used by the role editor and per-user overrides — one component, one mental
 * model everywhere permissions are edited.
 */
export function GrantEditor({
  grants,
  onChange,
}: {
  grants: GrantInput[];
  onChange: (grants: GrantInput[]) => void;
}) {
  const { t } = useTranslation();

  const update = (index: number, patch: Partial<GrantInput>) => {
    onChange(grants.map((g, i) => (i === index ? { ...g, ...patch } : g)));
  };

  return (
    <div className="space-y-3">
      {grants.map((grant, index) => (
        <div key={index} className="border-edge bg-bg-subtle/50 space-y-3 rounded-lg border p-3">
          <div className="flex items-end gap-2">
            <div className="min-w-0 flex-1">
              <label className="text-fg-subtle mb-1 block text-xs font-medium">
                {t("roles.editor.selector")}
              </label>
              <Input
                value={grant.selector}
                placeholder="*"
                className="font-mono text-[13px]"
                onChange={(e) => update(index, { selector: e.target.value })}
              />
            </div>
            <div className="w-28 shrink-0">
              <label className="text-fg-subtle mb-1 block text-xs font-medium">
                {t("roles.editor.effect")}
              </label>
              <Select
                value={grant.effect ?? "allow"}
                onValueChange={(effect) => update(index, { effect: effect as GrantInput["effect"] })}
              >
                <SelectTrigger
                  className={cn(grant.effect === "deny" && "text-danger border-danger/40")}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="allow">{t("roles.editor.allow")}</SelectItem>
                  <SelectItem value="deny">{t("roles.editor.deny")}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("roles.editor.removeGrant")}
              onClick={() => onChange(grants.filter((_, i) => i !== index))}
            >
              <Trash2 className="text-fg-subtle size-4" />
            </Button>
          </div>

          <div className="flex flex-wrap gap-1.5">
            {LEVELS.map((level) => {
              const active = grant.levels.includes(level);
              return (
                <Tooltip key={level} label={t(`roles.editor.levelHint.${level}`)}>
                  <button
                    type="button"
                    onClick={() => {
                      const levels = active
                        ? grant.levels.filter((l) => l !== level)
                        : [...grant.levels, level];
                      update(index, { levels });
                    }}
                    className={cn(
                      "cursor-pointer rounded-full border px-2.5 py-1 text-xs font-medium transition-colors",
                      active
                        ? "bg-accent-soft text-accent-soft-fg border-transparent"
                        : "border-edge text-fg-subtle hover:border-edge-strong hover:text-fg-muted bg-transparent",
                    )}
                    aria-pressed={active}
                  >
                    {t(`roles.editor.level.${level}`)}
                  </button>
                </Tooltip>
              );
            })}
          </div>
        </div>
      ))}

      <Button
        variant="secondary"
        size="sm"
        onClick={() => onChange([...grants, { selector: "", levels: ["view"], effect: "allow" }])}
      >
        <Plus className="size-3.5" />
        {t("roles.editor.addGrant")}
      </Button>
      <p className="text-fg-subtle text-xs">{t("roles.editor.selectorHint")}</p>
    </div>
  );
}
