import { useMemo, useState } from "react";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import {
  ArrowLeft,
  ArrowRight,
  CircleOff,
  LayoutDashboard,
  Loader2,
  Pencil,
  Plus,
  X,
} from "lucide-react";
import { api, unwrap, isApiError, type HaDashboard, type HaDashboardCard, type HaEntity } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { ErrorState } from "@/ui/states";
import { SkeletonRows } from "@/ui/skeleton";
import { useToast } from "@/ui/toast";
import { CompactEntityControl, EntityCard } from "./entity-card";
import { EntityDetailDialog } from "./entity-detail";
import { CardEditorDialog } from "./card-editor";
import { haDashboardKey, useHaDashboard, useHaEntities } from "./shared";

/** Server-enforced board limit, mirrored so the UI never offers a doomed add. */
const MAX_CARDS = 100;

const SIZE_ORDER = ["small", "medium", "large"] as const;
type CardSize = (typeof SIZE_ORDER)[number];

/** Column spans on the responsive board grid (1 → 2 → 4 columns). */
const SIZE_CLASS: Record<CardSize, string> = {
  small: "",
  medium: "sm:col-span-2",
  large: "col-span-full",
};

/** Effective size when a card never had one saved. */
function cardSize(card: HaDashboardCard): CardSize {
  return card.size ?? (card.type === "group" ? "medium" : "small");
}

/**
 * useSaveBoard PUTs the entire layout on every structural change, applying it
 * optimistically so edits feel instant. On failure the previous layout is
 * restored and the server's message (400 validation, etc.) is toasted
 * verbatim; on settle the query refetches so the cache matches what was
 * actually persisted.
 */
function useSaveBoard(instanceId: string) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const key = haDashboardKey(instanceId);
  return useMutation({
    mutationFn: async (layout: HaDashboard) =>
      unwrap(
        await api.PUT("/ha/{id}/dashboard", {
          params: { path: { id: instanceId } },
          body: layout,
        }),
      ),
    onMutate: async (layout) => {
      await queryClient.cancelQueries({ queryKey: key });
      const previous = queryClient.getQueryData<HaDashboard>(key);
      queryClient.setQueryData<HaDashboard>(key, layout);
      return { previous };
    },
    onError: (err, _layout, ctx) => {
      if (ctx?.previous) queryClient.setQueryData(key, ctx.previous);
      toast("error", t("ha.board.saveError"), isApiError(err) ? err.message : t("common.unknownError"));
    },
    onSettled: () => void queryClient.invalidateQueries({ queryKey: key }),
  });
}

/**
 * HaBoardPage is the per-user dashboard: empty until this account curates it.
 * Cards only store entity ids; state comes from the shared entities query the
 * module-level WS subscription keeps patched, so the board is live for free.
 */
export function HaBoardPage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/ha/$instanceId/" });
  const board = useHaDashboard(instanceId);
  const entities = useHaEntities(instanceId);
  const save = useSaveBoard(instanceId);

  const [editing, setEditing] = useState(false);
  // null = closed; card undefined = adding; card set = editing that card.
  const [editor, setEditor] = useState<{ card?: HaDashboardCard } | null>(null);
  const [detailId, setDetailId] = useState<string | null>(null);

  const byId = useMemo(() => {
    const map = new Map<string, HaEntity>();
    for (const e of entities.data?.entities ?? []) map.set(e.entityId, e);
    return map;
  }, [entities.data]);

  if (board.isLoading || entities.isLoading) return <SkeletonRows rows={5} />;
  if (board.isError || entities.isError) {
    const failed = board.isError ? board : entities;
    return (
      <Card>
        <ErrorState error={failed.error} onRetry={() => void failed.refetch()} />
      </Card>
    );
  }

  const cards = board.data?.cards ?? [];
  const commit = (next: HaDashboardCard[]) => save.mutate({ cards: next });

  const removeCard = (id: string) => commit(cards.filter((c) => c.id !== id));

  const moveCard = (index: number, dir: -1 | 1) => {
    const target = index + dir;
    if (target < 0 || target >= cards.length) return;
    const next = cards.slice();
    [next[index], next[target]] = [next[target], next[index]];
    commit(next);
  };

  const cycleSize = (card: HaDashboardCard) => {
    const size = SIZE_ORDER[(SIZE_ORDER.indexOf(cardSize(card)) + 1) % SIZE_ORDER.length];
    commit(cards.map((c) => (c.id === card.id ? { ...c, size } : c)));
  };

  const upsertCard = (card: HaDashboardCard) => {
    const exists = cards.some((c) => c.id === card.id);
    commit(exists ? cards.map((c) => (c.id === card.id ? card : c)) : [...cards, card]);
  };

  const detail = detailId ? byId.get(detailId) : undefined;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <div className="min-w-0 flex-1">
          <h2 className="text-fg text-[15px] font-semibold">{t("ha.board.title")}</h2>
          <p className="text-fg-muted text-[13px]">{t("ha.board.subtitle")}</p>
        </div>
        {save.isPending && (
          <span className="text-fg-subtle flex items-center gap-1.5 text-xs" role="status">
            <Loader2 className="size-3.5 animate-spin" aria-hidden />
            {t("common.saving")}
          </span>
        )}
        {editing ? (
          <Button variant="primary" size="sm" onClick={() => setEditing(false)}>
            {t("common.done")}
          </Button>
        ) : (
          cards.length > 0 && (
            <Button
              variant="ghost"
              size="icon"
              aria-label={t("ha.board.edit")}
              onClick={() => setEditing(true)}
            >
              <Pencil className="size-4" aria-hidden />
            </Button>
          )
        )}
      </div>

      {cards.length === 0 && !editing ? (
        <BoardEmptyState onAdd={() => setEditor({})} />
      ) : (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 xl:grid-cols-4">
          {cards.map((card, index) => (
            <div key={card.id} className={cn("flex min-w-0 flex-col", SIZE_CLASS[cardSize(card)])}>
              {editing && (
                <CardToolbar
                  card={card}
                  index={index}
                  count={cards.length}
                  onMove={(dir) => moveCard(index, dir)}
                  onCycleSize={() => cycleSize(card)}
                  onEdit={() => setEditor({ card })}
                  onRemove={() => removeCard(card.id)}
                />
              )}
              <div className="min-h-0 flex-1 *:h-full">
                <BoardCard
                  instanceId={instanceId}
                  card={card}
                  byId={byId}
                  onOpenDetail={setDetailId}
                />
              </div>
            </div>
          ))}
          {editing && cards.length < MAX_CARDS && (
            <button
              type="button"
              onClick={() => setEditor({})}
              className="border-edge-strong text-fg-muted hover:border-accent hover:text-fg flex min-h-32 cursor-pointer items-center justify-center gap-2 rounded-lg border-2 border-dashed text-sm font-medium transition-colors"
            >
              <Plus className="size-4" aria-hidden />
              {t("ha.board.addCard")}
            </button>
          )}
        </div>
      )}

      {editor && (
        <CardEditorDialog
          instanceId={instanceId}
          card={editor.card}
          onClose={() => setEditor(null)}
          onSave={(card) => {
            upsertCard(card);
            setEditor(null);
          }}
        />
      )}
      {detail && (
        <EntityDetailDialog instanceId={instanceId} entity={detail} onClose={() => setDetailId(null)} />
      )}
    </div>
  );
}

/** The welcome the first visit deserves: what this board is, and the one way in. */
function BoardEmptyState({ onAdd }: { onAdd: () => void }) {
  const { t } = useTranslation();
  return (
    <Card className="flex flex-col items-center justify-center px-6 py-16 text-center">
      <div className="bg-accent-soft text-accent-soft-fg mb-4 flex size-14 items-center justify-center rounded-2xl">
        <LayoutDashboard className="size-6" aria-hidden />
      </div>
      <h3 className="text-fg text-base font-semibold">{t("ha.board.empty.title")}</h3>
      <p className="text-fg-muted mt-1.5 max-w-sm text-sm">{t("ha.board.empty.body")}</p>
      <Button variant="primary" size="lg" className="mt-5" onClick={onAdd}>
        <Plus className="size-4" aria-hidden />
        {t("ha.board.addCard")}
      </Button>
    </Card>
  );
}

/** The edit-mode strip above each card: reorder, resize, edit, remove. */
function CardToolbar({
  card,
  index,
  count,
  onMove,
  onCycleSize,
  onEdit,
  onRemove,
}: {
  card: HaDashboardCard;
  index: number;
  count: number;
  onMove: (dir: -1 | 1) => void;
  onCycleSize: () => void;
  onEdit: () => void;
  onRemove: () => void;
}) {
  const { t } = useTranslation();
  const size = cardSize(card);
  return (
    <div className="border-edge bg-bg-subtle/60 mb-1.5 flex items-center gap-0.5 rounded-md border px-1 py-0.5">
      <Button
        size="icon"
        variant="ghost"
        className="size-6"
        disabled={index === 0}
        aria-label={t("ha.board.moveLeft")}
        onClick={() => onMove(-1)}
      >
        <ArrowLeft className="size-3.5" aria-hidden />
      </Button>
      <Button
        size="icon"
        variant="ghost"
        className="size-6"
        disabled={index === count - 1}
        aria-label={t("ha.board.moveRight")}
        onClick={() => onMove(1)}
      >
        <ArrowRight className="size-3.5" aria-hidden />
      </Button>
      <Button
        size="sm"
        variant="ghost"
        className="h-6 px-1.5 font-mono text-[11px]"
        aria-label={t("ha.board.cycleSize", { size: t(`ha.board.sizeName.${size}`) })}
        onClick={onCycleSize}
      >
        {t(`ha.board.sizeAbbr.${size}`)}
      </Button>
      <span className="flex-1" />
      <Button
        size="icon"
        variant="ghost"
        className="size-6"
        aria-label={t("ha.board.editCard")}
        onClick={onEdit}
      >
        <Pencil className="size-3.5" aria-hidden />
      </Button>
      <Button
        size="icon"
        variant="danger-ghost"
        className="size-6"
        aria-label={t("ha.board.removeCard")}
        onClick={onRemove}
      >
        <X className="size-3.5" aria-hidden />
      </Button>
    </div>
  );
}

/** One board slot: full entity card, titled group list, or the missing stub. */
function BoardCard({
  instanceId,
  card,
  byId,
  onOpenDetail,
}: {
  instanceId: string;
  card: HaDashboardCard;
  byId: Map<string, HaEntity>;
  onOpenDetail: (entityId: string) => void;
}) {
  if (card.type === "group") {
    return <GroupCard instanceId={instanceId} card={card} byId={byId} onOpenDetail={onOpenDetail} />;
  }
  const entityId = card.entityIds[0];
  const entity = entityId !== undefined ? byId.get(entityId) : undefined;
  if (!entity) return <MissingEntityCard entityId={entityId ?? ""} />;
  return (
    <EntityCard
      instanceId={instanceId}
      entity={entity}
      onOpenDetail={(e) => onOpenDetail(e.entityId)}
    />
  );
}

/** A titled compact list: one row per entity, name + primary control. */
function GroupCard({
  instanceId,
  card,
  byId,
  onOpenDetail,
}: {
  instanceId: string;
  card: HaDashboardCard;
  byId: Map<string, HaEntity>;
  onOpenDetail: (entityId: string) => void;
}) {
  const { t } = useTranslation();
  return (
    <Card className="flex h-full flex-col overflow-hidden">
      <div className="border-edge flex items-baseline justify-between gap-3 border-b px-4 py-2.5">
        <p className="text-fg min-w-0 truncate text-sm font-semibold">
          {card.title || t("ha.board.groupUntitled")}
        </p>
        <span className="text-fg-subtle shrink-0 text-xs">
          {t("ha.entityCount", { count: card.entityIds.length })}
        </span>
      </div>
      <ul className="divide-edge max-h-80 min-h-0 flex-1 divide-y overflow-y-auto">
        {card.entityIds.map((id) => {
          const entity = byId.get(id);
          return (
            <li key={id} className="flex min-h-11 items-center justify-between gap-3 px-4 py-1.5">
              {entity ? (
                <>
                  <button
                    type="button"
                    onClick={() => onOpenDetail(entity.entityId)}
                    className="group min-w-0 cursor-pointer text-left"
                  >
                    <p className="text-fg group-hover:text-accent truncate text-[13px] font-medium transition-colors">
                      {entity.name}
                    </p>
                  </button>
                  <CompactEntityControl instanceId={instanceId} entity={entity} />
                </>
              ) : (
                // RBAC-hidden or gone from HA: show only the id the user
                // themselves saved, nothing else about it.
                <>
                  <span className="text-fg-subtle min-w-0 truncate font-mono text-xs">{id}</span>
                  <span className="text-fg-subtle shrink-0 text-xs">{t("ha.board.unavailable")}</span>
                </>
              )}
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

/**
 * MissingEntityCard fills an entity card's slot when its entity is no longer
 * in the caller's entities response — removed from HA or hidden by RBAC. The
 * board keeps its shape, reveals nothing beyond the saved id, and the edit
 * toolbar still offers removal.
 */
function MissingEntityCard({ entityId }: { entityId: string }) {
  const { t } = useTranslation();
  return (
    <Card className="flex h-full flex-col items-start justify-center gap-1.5 border-dashed p-4">
      <div className="text-fg-muted flex items-center gap-2">
        <CircleOff className="size-4 shrink-0" aria-hidden />
        <p className="text-[13px] font-medium">{t("ha.board.unavailable")}</p>
      </div>
      {entityId && <p className="text-fg-subtle font-mono text-[11px] break-all">{entityId}</p>}
      <p className="text-fg-subtle text-xs">{t("ha.board.unavailableBody")}</p>
    </Card>
  );
}
