import type { AssistantStep } from "./assistant-chat";

/**
 * A conversation is a tree: editing a question or retrying an answer forks a
 * new branch off the same parent instead of overwriting what was there. The
 * server stores every branch; this module picks the ONE path the reader sees.
 */

export type ChatMessage = {
  id: number;
  parentId?: number | null;
  role: "user" | "assistant";
  content: string;
  tools?: string[];
  steps?: AssistantStep[];
  ts: string;
};

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
