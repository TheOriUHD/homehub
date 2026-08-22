import type { AssistantStep } from "./assistant-chat";

/**
 * A conversation is a tree: editing a question or retrying an answer forks a
 * new branch off the same parent instead of overwriting what was there. The
 * server stores every branch; this module picks the ONE path the reader sees.
 */

/** What producing one answer cost. Absent on questions, and on answers
 *  written before usage was recorded. */
export type MessageUsage = {
  model: string;
  modelLabel?: string;
  inputTokens: number;
  outputTokens: number;
  costUsd?: number;
  /** The Auto rule that chose this model, absent when the user pinned one. */
  route?: string;
  /** Wall-clock time to produce the answer. */
  durationMs?: number;
};

export type ChatMessage = {
  id: number;
  parentId?: number | null;
  role: "user" | "assistant";
  content: string;
  tools?: string[];
  steps?: AssistantStep[];
  usage?: MessageUsage;
  ts: string;
};

/** compactTokens renders a token count the way a reader scans it: exact while
 *  small, then one decimal of k or M. 420 → "420", 8151 → "8.2k". */
export function compactTokens(n: number): string {
  // A trailing ".0" is noise — "142k" reads better than "142.0k".
  const scaled = (v: number, unit: string) => v.toFixed(1).replace(/\.0$/, "") + unit;
  if (n < 1000) return String(n);
  if (n < 1_000_000) return scaled(n / 1000, "k");
  return scaled(n / 1_000_000, "M");
}

/** formatUsd keeps three decimals where fractions of a cent still matter, and
 *  refuses to render a real cost as "$0.000". */
export function formatUsd(v: number): string {
  if (v > 0 && v < 0.001) return "<$0.001";
  return "$" + v.toFixed(v < 1 ? 3 : 2);
}

/** formatDuration reads at a glance: sub-second in ms, then seconds to one
 *  decimal, then minutes once an answer takes that long. */
export function formatDuration(ms: number): string {
  if (ms < 1000) return Math.round(ms) + "ms";
  const seconds = ms / 1000;
  if (seconds < 60) return seconds.toFixed(1).replace(/\.0$/, "") + "s";
  const mins = Math.floor(seconds / 60);
  return mins + "m " + Math.round(seconds - mins * 60) + "s";
}

/** The one-line summary shown in the info tooltip:
 *  "Haiku 4.5 · 8.2k input · 420 output · $0.010 · 4.2s" */
export function usageLine(usage: MessageUsage): string {
  const parts = [
    usage.modelLabel || usage.model,
    compactTokens(usage.inputTokens) + " input",
    compactTokens(usage.outputTokens) + " output",
  ];
  if (usage.costUsd != null) parts.push(formatUsd(usage.costUsd));
  if (usage.durationMs != null) parts.push(formatDuration(usage.durationMs));
  return parts.join(" · ");
}

/** One message on the visible path, plus the branch controls it needs. */
export type PathEntry = {
  msg: ChatMessage;
  /** Every message sharing this one's parent, oldest first. */
  siblings: number[];
  /** Where msg sits among them — the "2" in "‹ 2/3 ›". */
  index: number;
};

/** The map key for the root level, where parentId is null. */
export const ROOT = 0;

/**
 * buildPath walks the tree from the root, taking the chosen sibling at each
 * step and falling back to the NEWEST one — so a fresh branch is what you see
 * immediately after creating it, without having to record a choice first.
 */
export function buildPath(items: ChatMessage[], choice: Record<number, number>): PathEntry[] {
  const ids = new Set(items.map((m) => m.id));
  const byParent = new Map<number, ChatMessage[]>();
  for (const m of items) {
    // A parent outside the fetched window can't be walked to, so its child
    // starts a root chain rather than vanishing from the transcript.
    const key = m.parentId != null && ids.has(m.parentId) ? m.parentId : ROOT;
    const list = byParent.get(key);
    if (list) list.push(m);
    else byParent.set(key, [m]);
  }
  for (const list of byParent.values()) list.sort((a, b) => a.id - b.id);

  const path: PathEntry[] = [];
  const seen = new Set<number>();
  let key = ROOT;
  for (;;) {
    const siblings = byParent.get(key);
    if (!siblings?.length) break;
    const picked = siblings.find((m) => m.id === choice[key]) ?? siblings[siblings.length - 1];
    // Data can only be a tree, but a cycle here would hang the render.
    if (seen.has(picked.id)) break;
    seen.add(picked.id);
    path.push({
      msg: picked,
      siblings: siblings.map((m) => m.id),
      index: siblings.findIndex((m) => m.id === picked.id),
    });
    key = picked.id;
  }
  return path;
}

/** branchKey is the choice-map key a message's siblings are recorded under. */
export function branchKey(msg: ChatMessage, known: Set<number>): number {
  return msg.parentId != null && known.has(msg.parentId) ? msg.parentId : ROOT;
}

/**
 * Branch choices and command-output receipts live beside the conversation id
 * in sessionStorage, so they share its lifetime exactly: all three survive
 * reloads within the running app and are gone once it is fully closed.
 */
export function loadChoices(conversation: string): Record<number, number> {
  try {
    const raw = sessionStorage.getItem("hh-assistant-branch-" + conversation);
    return raw ? (JSON.parse(raw) as Record<number, number>) : {};
  } catch {
    return {};
  }
}

export function saveChoices(conversation: string, choice: Record<number, number>) {
  try {
    sessionStorage.setItem("hh-assistant-branch-" + conversation, JSON.stringify(choice));
  } catch {
    /* private mode — selection just resets on reload */
  }
}

/** What one approved action did. Kept client-side: the transcript stores the
 *  model's reading of the output, this keeps the raw text a click away. */
export type ActionOutcome = { summary: string; output?: string; failed?: boolean };

/** Outcomes keyed by the assistant message that reported on them. */
export type OutcomeMap = Record<number, ActionOutcome[]>;

export function loadOutcomes(conversation: string): OutcomeMap {
  try {
    const raw = sessionStorage.getItem("hh-assistant-outcomes-" + conversation);
    return raw ? (JSON.parse(raw) as OutcomeMap) : {};
  } catch {
    return {};
  }
}

export function saveOutcomes(conversation: string, map: OutcomeMap) {
  try {
    sessionStorage.setItem("hh-assistant-outcomes-" + conversation, JSON.stringify(map));
  } catch {
    /* output too large for the quota, or private mode — it stays transient */
  }
}
