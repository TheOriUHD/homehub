import type { HaDashboardCard } from "@/lib/api";

/**
 * Per-row display-name overrides for group cards (entity id → custom name).
 *
 * The Go API spec already carries `HaDashboardCard.labels`, but the generated
 * TS schema (src/lib/schema.d.ts) has not been regenerated yet. Every read and
 * write of the field goes through this module — once the schema is regenerated
 * the casts below collapse to plain property access and this file shrinks to
 * (or disappears into) one line.
 */
export type CardLabels = Record<string, string>;

type LabeledCard = HaDashboardCard & { labels?: CardLabels };

/** Read a card's per-row rename map (undefined when nothing was renamed). */
export function cardLabels(card: HaDashboardCard): CardLabels | undefined {
  return (card as LabeledCard).labels;
}

/**
 * Return a copy of `card` carrying `labels`, pruned to the card's own
 * entityIds with blank names dropped. The key is omitted entirely when
 * nothing survives, so cards without renames round-trip unchanged.
 */
export function withCardLabels(
  card: HaDashboardCard,
  labels: CardLabels | undefined,
): HaDashboardCard {
  const pruned: CardLabels = {};
  if (labels) {
    for (const id of card.entityIds) {
      const name = labels[id]?.trim();
      if (name) pruned[id] = name;
    }
  }
  const next = { ...card } as LabeledCard;
  if (Object.keys(pruned).length > 0) next.labels = pruned;
  else delete next.labels;
  return next;
}
