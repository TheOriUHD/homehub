import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { Check, LayoutList, Lightbulb } from "lucide-react";
import type { HaDashboardCard } from "@/lib/api";
import { cn, randomId } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Dialog, DialogContent } from "@/ui/dialog";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { stateLabel } from "./entity-card";
import { useHaEntities } from "./shared";

/** Server-enforced per-card limit, mirrored for instant feedback. */
const MAX_GROUP_ENTITIES = 50;

type CardType = HaDashboardCard["type"];

/**
 * CardEditorDialog is both the add flow (step 1 pick a type, step 2 pick
 * entities) and, given `card`, the edit flow (straight to a prefilled picker).
 * It only builds the card object — the board owns persistence.
 */
export function CardEditorDialog({
  instanceId,
  card,
  onClose,
  onSave,
}: {
  instanceId: string;
  card?: HaDashboardCard;
  onClose: () => void;
  onSave: (card: HaDashboardCard) => void;
}) {
  const { t } = useTranslation();
  const entities = useHaEntities(instanceId);

  const [type, setType] = useState<CardType | null>(card?.type ?? null);
  const [title, setTitle] = useState(card?.title ?? "");
  const [selected, setSelected] = useState<string[]>(card?.entityIds ?? []);
  const [search, setSearch] = useState("");
  const [areaFilter, setAreaFilter] = useState("all");
  const [domainFilter, setDomainFilter] = useState<Set<string>>(new Set());

  const all = useMemo(() => entities.data?.entities ?? [], [entities.data]);
  const areas = entities.data?.areas ?? [];

  const domains = useMemo(() => [...new Set(all.map((e) => e.domain))].sort(), [all]);

  const filtered = useMemo(() => {
    const needle = search.trim().toLowerCase();
    return all
      .filter((e) => {
        if (needle && !e.name.toLowerCase().includes(needle) && !e.entityId.toLowerCase().includes(needle)) {
          return false;
        }
        if (areaFilter === "none" && e.areaId) return false;
        if (areaFilter !== "all" && areaFilter !== "none" && e.areaId !== areaFilter) return false;
        if (domainFilter.size > 0 && !domainFilter.has(e.domain)) return false;
        return true;
      })
      .sort((a, b) => a.name.localeCompare(b.name));
  }, [all, search, areaFilter, domainFilter]);

  // Ids the card references that the entities response no longer contains
  // (removed from HA or RBAC-hidden since they were saved). Pinned above the
  // list so editing never silently drops them — the user decides.
  const missingSelected = useMemo(
    () => selected.filter((id) => !all.some((e) => e.entityId === id)),
    [selected, all],
  );

  const toggle = (entityId: string) =>
    setSelected((prev) => {
      if (type === "entity") return prev.includes(entityId) ? [] : [entityId];
      return prev.includes(entityId) ? prev.filter((id) => id !== entityId) : [...prev, entityId];
    });

  const pickType = (next: CardType) => {
    setType(next);
    // Narrowing group → entity keeps only the first pick.
    if (next === "entity") setSelected((prev) => prev.slice(0, 1));
  };

  const overLimit = type === "group" && selected.length > MAX_GROUP_ENTITIES;
  const valid =
    type !== null &&
    selected.length >= 1 &&
    (type === "entity" ? selected.length === 1 : !overLimit);

  const submit = () => {
    if (!valid || type === null) return;
    onSave({
      id: card?.id ?? randomId(8),
      type,
      entityIds: selected,
      ...(type === "group" && title.trim() !== "" ? { title: title.trim() } : {}),
      ...(card?.size ? { size: card.size } : {}),
    });
  };

  const toggleDomain = (domain: string) =>
    setDomainFilter((prev) => {
      const next = new Set(prev);
      if (next.has(domain)) next.delete(domain);
      else next.add(domain);
      return next;
    });

  const step: "type" | "pick" = type === null ? "type" : "pick";

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent
        wide
        title={card ? t("ha.board.dialog.editTitle") : t("ha.board.dialog.addTitle")}
        description={
          step === "type"
            ? t("ha.board.dialog.typePrompt")
            : type === "entity"
              ? t("ha.board.dialog.entityTypeBody")
              : t("ha.board.dialog.groupTypeBody")
        }
        footer={
          step === "pick" ? (
            <>
              {!card && (
                <Button variant="ghost" className="mr-auto" onClick={() => setType(null)}>
                  {t("common.back")}
                </Button>
              )}
              <Button variant="ghost" onClick={onClose}>
                {t("common.cancel")}
              </Button>
              <Button variant="primary" disabled={!valid} onClick={submit}>
                {t("ha.board.dialog.save")}
              </Button>
            </>
          ) : (
            <Button variant="ghost" onClick={onClose}>
              {t("common.cancel")}
            </Button>
          )
        }
      >
        {step === "type" ? (
          <div className="grid gap-3 sm:grid-cols-2">
            <TypeOption
              icon={Lightbulb}
              title={t("ha.board.dialog.entityType")}
              body={t("ha.board.dialog.entityTypeBody")}
              onClick={() => pickType("entity")}
            />
            <TypeOption
              icon={LayoutList}
              title={t("ha.board.dialog.groupType")}
              body={t("ha.board.dialog.groupTypeBody")}
              onClick={() => pickType("group")}
            />
          </div>
        ) : entities.isLoading ? (
          <PageLoader className="py-10" />
        ) : (
          <div className="space-y-3">
            {type === "group" && (
              <Field
                label={t("ha.board.dialog.groupTitle")}
                optional={t("common.optional")}
                hint={t("ha.board.dialog.titleHint")}
              >
                {(fieldProps) => (
                  <Input
                    {...fieldProps}
                    value={title}
                    onChange={(e) => setTitle(e.target.value)}
                    placeholder={t("ha.board.dialog.groupTitlePlaceholder")}
                    autoComplete="off"
                  />
                )}
              </Field>
            )}

            <div className="flex flex-wrap items-center gap-2">
              <Input
                autoFocus
                placeholder={t("ha.searchPlaceholder")}
                className="min-w-44 flex-1"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
              {areas.length > 0 && (
                <Select value={areaFilter} onValueChange={setAreaFilter}>
                  <SelectTrigger className="w-40" aria-label={t("ha.areaFilter")}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">{t("ha.allAreas")}</SelectItem>
                    {areas.map((area) => (
                      <SelectItem key={area.id} value={area.id}>
                        {area.name}
                      </SelectItem>
                    ))}
                    <SelectItem value="none">{t("ha.noArea")}</SelectItem>
                  </SelectContent>
                </Select>
              )}
            </div>

            {domains.length > 1 && (
              <div className="flex flex-wrap gap-1.5">
                {domains.map((domain) => {
                  const active = domainFilter.has(domain);
                  return (
                    <button
                      key={domain}
                      type="button"
                      onClick={() => toggleDomain(domain)}
                      aria-pressed={active}
                      className={cn(
                        "cursor-pointer rounded-full border px-2.5 py-1 text-xs font-medium transition-colors",
                        active
                          ? "bg-accent-soft text-accent-soft-fg border-transparent"
                          : "border-edge text-fg-muted hover:bg-surface-hover hover:text-fg bg-surface",
                      )}
                    >
                      {domain.replace(/_/g, " ")}
                    </button>
                  );
                })}
              </div>
            )}

            <div className="border-edge max-h-72 overflow-y-auto rounded-lg border">
              <ul className="divide-edge divide-y">
                {missingSelected.map((id) => (
                  <PickerRow
                    key={id}
                    checked
                    onToggle={() => toggle(id)}
                    name={<span className="font-mono text-xs">{id}</span>}
                    meta={t("ha.board.unavailable")}
                  />
                ))}
                {filtered.map((entity) => (
                  <PickerRow
                    key={entity.entityId}
                    checked={selected.includes(entity.entityId)}
                    onToggle={() => toggle(entity.entityId)}
                    name={entity.name}
                    sub={entity.entityId}
                    meta={stateLabel(t, entity.state)}
                  />
                ))}
              </ul>
              {filtered.length === 0 && missingSelected.length === 0 && (
                <p className="text-fg-subtle px-4 py-8 text-center text-[13px]">
                  {t("ha.noMatches.title")}
                </p>
              )}
            </div>

            {type === "group" && (
              <p className={cn("text-xs", overLimit ? "text-danger" : "text-fg-subtle")}>
                {t("ha.board.dialog.selectedCount", { count: selected.length })}
                {overLimit && ` — ${t("ha.board.dialog.maxEntities", { max: MAX_GROUP_ENTITIES })}`}
              </p>
            )}
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

/** One of the two step-1 choices: icon tile, name, one-line description. */
function TypeOption({
  icon: Icon,
  title,
  body,
  onClick,
}: {
  icon: typeof Lightbulb;
  title: string;
  body: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="border-edge bg-surface hover:border-accent flex cursor-pointer flex-col items-start gap-2 rounded-lg border p-4 text-left transition-colors"
    >
      <span className="bg-accent-soft text-accent-soft-fg flex size-9 items-center justify-center rounded-lg">
        <Icon className="size-4.5" aria-hidden />
      </span>
      <span className="text-fg text-sm font-semibold">{title}</span>
      <span className="text-fg-muted text-[13px]">{body}</span>
    </button>
  );
}

/** A selectable entity row with a checkbox-styled indicator. */
function PickerRow({
  checked,
  onToggle,
  name,
  sub,
  meta,
}: {
  checked: boolean;
  onToggle: () => void;
  name: React.ReactNode;
  sub?: string;
  meta?: string;
}) {
  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-pressed={checked}
        className={cn(
          "flex w-full cursor-pointer items-center gap-3 px-3.5 py-2 text-left transition-colors",
          checked ? "bg-accent-soft/40" : "hover:bg-surface-hover",
        )}
      >
        <span
          aria-hidden
          className={cn(
            "flex size-4 shrink-0 items-center justify-center rounded border transition-colors",
            checked ? "bg-accent border-accent" : "border-edge-strong bg-surface",
          )}
        >
          {checked && <Check className="text-accent-fg size-3" strokeWidth={3} />}
        </span>
        <span className="min-w-0 flex-1">
          <span className="text-fg block truncate text-[13px] font-medium">{name}</span>
          {sub && <span className="text-fg-subtle block truncate font-mono text-[11px]">{sub}</span>}
        </span>
        {meta && <span className="text-fg-subtle shrink-0 text-xs">{meta}</span>}
      </button>
    </li>
  );
}
