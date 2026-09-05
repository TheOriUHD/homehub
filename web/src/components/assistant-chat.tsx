import { Fragment, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import {
  Box,
  Check,
  Info,
  ChevronLeft,
  ChevronRight,
  Copy,
  Eraser,
  Gamepad2,
  Lightbulb,
  Loader2,
  Pencil,
  Pickaxe,
  RotateCcw,
  Send,
  Server,
  Square,
  ShieldCheck,
  Sparkles,
  Terminal,
  Volume2,
  VolumeX,
  Wrench,
  X,
} from "lucide-react";
import { api, isApiError, unwrap, type HaEntity } from "@/lib/api";
import { cn, randomId } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { ConfirmDialog } from "@/ui/confirm";
import { Textarea } from "@/ui/input";
import { PageLoader } from "@/ui/misc";
import { Popover, PopoverContent, PopoverTrigger } from "@/ui/popover";
import { Slider } from "@/ui/slider";
import { Switch } from "@/ui/switch";
import { Tooltip } from "@/ui/tooltip";
import { useToast } from "@/ui/toast";
import { AssistantModelPicker, modelForTurn, useAssistantModels } from "./assistant-model-picker";
import {
  branchKey,
  buildPath,
  loadChoices,
  loadOutcomes,
  saveChoices,
  saveOutcomes,
  ROOT,
  usageLine,
  type ActionOutcome,
  type ChatMessage,
  type OutcomeMap,
  type PathEntry,
} from "./assistant-branches";

/**
 * The shared assistant conversation — one thread per app session, shown
 * identically on the /assistant page and in the floating widget (both ride
 * the same query key, so they stay in sync). Assistant messages render
 * markdown, and every action the model took becomes a live chip: a light
 * it touched opens instant controls, a container it restarted shows its
 * lifecycle buttons. The chat is the receipt AND the remote.
 *
 * The thread is a TREE. Editing a question or retrying an answer forks a
 * branch rather than destroying what was there, and the reader cycles between
 * branches with the ‹ n/m › control on the message where they diverge.
 */

export type AssistantStep = {
  tool: string;
  input?: Record<string, unknown>;
  isError?: boolean;
};

type ToolStep = { name: string; running: boolean; isError?: boolean };
type PendingAction = { kind: string; summary: string; params: unknown };

/** The in-progress turn. userText is null while regenerating, where the
 *  question is already in the transcript and must not be echoed twice.
 *  `replacing` is the message this turn stands in for — a retried answer or
 *  an edited question — so it streams IN PLACE of the version it forks from
 *  instead of appearing underneath it. */
type Live = {
  userText: string | null;
  replacing: number | null;
  segments: string[];
  tools: ToolStep[];
  outcomes: ActionOutcome[];
  error?: string;
};

type StreamEvent = {
  type: "user" | "text" | "tool" | "tool_result" | "confirm" | "action_result" | "done" | "error";
  text?: string;
  tool?: string;
  isError?: boolean;
  actions?: PendingAction[];
  message?: string;
  messageId?: number;
  outcome?: ActionOutcome;
};

/** What the model queued, and the message it hangs off so the resumed answer
 *  lands on the branch the user is reading. */
type Pending = { actions: PendingAction[]; parentId: number | null };

/** How tall the composer may grow before its content scrolls instead. */
const COMPOSER_MAX_PX = 160;

/** Friendly label for a tool name in the live spinner chips. */
const toolLabel = (name: string) => name.replace(/_/g, " ");

/**
 * conversationId scopes the web chat to one app session: sessionStorage
 * survives navigation and reloads within the running app but is gone once the
 * app is fully closed — a fresh open starts a fresh conversation. The id is
 * opaque to the server, so it need not be a UUID — and crypto.randomUUID does
 * not exist on the plain-http LAN origin, where it took the whole pane down.
 */
function conversationId(): string {
  let id = sessionStorage.getItem("hh-assistant-conversation");
  if (!id) {
    id = randomId(32);
    sessionStorage.setItem("hh-assistant-conversation", id);
  }
  return id;
}

export function AssistantChatPane({ variant }: { variant: "page" | "widget" }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [draft, setDraft] = useState("");
  const [confirmClear, setConfirmClear] = useState(false);
  const bottomRef = useRef<HTMLDivElement>(null);
  const composerRef = useRef<HTMLTextAreaElement>(null);
  const abortRef = useRef<AbortController | null>(null);

  const conversation = conversationId();
  // Whether Auto can run decides what an untouched window sends (see
  // modelForTurn); the picker reads the same catalog, so the two agree.
  const { autoAvailable } = useAssistantModels();
  const history = useQuery({
    queryKey: ["assistant", "history", conversation],
    queryFn: async () =>
      unwrap(await api.GET("/assistant/history", { params: { query: { conversation } } })),
  });

  const [choice, setChoice] = useState<Record<number, number>>(() => loadChoices(conversation));
  const chooseBranch = useCallback(
    (parent: number, child: number) =>
      setChoice((prev) => {
        const next = { ...prev, [parent]: child };
        saveChoices(conversation, next);
        return next;
      }),
    [conversation],
  );

  // Raw command output for the answers that reported on it, so the receipt
  // stays one click away after the turn ends.
  const [outcomes, setOutcomes] = useState<OutcomeMap>(() => loadOutcomes(conversation));

  const [live, setLive] = useState<Live | null>(null);
  const [pending, setPending] = useState<Pending | null>(null);
  const [editing, setEditing] = useState<number | null>(null);
  const streaming = live !== null;

  const items = useMemo(
    () => ((history.data as ChatMessage[] | undefined) ?? []),
    [history.data],
  );
  const path = useMemo(() => buildPath(items, choice), [items, choice]);
  const knownIds = useMemo(() => new Set(items.map((m) => m.id)), [items]);
  const leafId = path.length > 0 ? path[path.length - 1].msg.id : null;

  /**
   * runStream drives one NDJSON turn — a new message, an edit branch, a
   * retry, or the continuation after approving actions — into the live
   * bubble. `body` decides which; `showUser` is the question to echo while it
   * runs (null when the transcript already shows it).
   */
  const runStream = useCallback(
    async (
      url: string,
      body: Record<string, unknown>,
      showUser: string | null,
      replacing: number | null = null,
    ) => {
      setPending(null);
      const controller = new AbortController();
      abortRef.current = controller;
      setLive({ userText: showUser, replacing, segments: [], tools: [], outcomes: [] });
      let confirm: Pending | null = null;
      const ran: ActionOutcome[] = [];
      try {
        const res = await fetch(url, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          credentials: "same-origin",
          body: JSON.stringify({ ...body, conversation, model: modelForTurn(autoAvailable) }),
          signal: controller.signal,
        });
        if (!res.ok || !res.body) {
          setLive((l) => (l ? { ...l, error: t("assistant.failed") } : l));
          return;
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buf = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          const lines = buf.split("\n");
          buf = lines.pop() ?? "";
          for (const line of lines) {
            if (!line.trim()) continue;
            let e: StreamEvent;
            try {
              e = JSON.parse(line) as StreamEvent;
            } catch {
              continue;
            }
            // Follow the branch this turn just created, so the reader lands on
            // the new answer rather than on the one it forked away from.
            if ((e.type === "user" || e.type === "done") && e.messageId) {
              const parent = e.type === "user" ? (body.parentId as number | null) : null;
              if (e.type === "user") chooseBranch(parent ?? ROOT, e.messageId);
              else if (body.regenerate) chooseBranch(body.parentId as number, e.messageId);
            }
            if (e.type === "action_result" && e.outcome) ran.push(e.outcome);
            if (e.type === "done" && e.messageId && ran.length > 0) {
              const id = e.messageId;
              setOutcomes((prev) => {
                const next = { ...prev, [id]: ran };
                saveOutcomes(conversation, next);
                return next;
              });
            }
            if (e.type === "confirm") {
              confirm = { actions: e.actions ?? [], parentId: e.messageId ?? null };
            }
            setLive((prev) => {
              if (!prev) return prev;
              switch (e.type) {
                case "text":
                  return { ...prev, segments: [...prev.segments, e.text ?? ""] };
                case "tool":
                  return { ...prev, tools: [...prev.tools, { name: e.tool ?? "", running: true }] };
                case "tool_result": {
                  const tools = [...prev.tools];
                  for (let i = tools.length - 1; i >= 0; i--) {
                    if (tools[i].name === e.tool && tools[i].running) {
                      tools[i] = { ...tools[i], running: false, isError: e.isError };
                      break;
                    }
                  }
                  return { ...prev, tools };
                }
                case "action_result":
                  return e.outcome ? { ...prev, outcomes: [...prev.outcomes, e.outcome] } : prev;
                case "error":
                  return { ...prev, error: e.message };
                default:
                  return prev;
              }
            });
          }
        }
        await queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
        if (confirm && confirm.actions.length > 0) setPending(confirm);
      } catch (err) {
        if (!(err instanceof DOMException && err.name === "AbortError")) {
          toast("error", t("assistant.failed"), err instanceof Error ? err.message : t("common.unknownError"));
        }
        await queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
      } finally {
        abortRef.current = null;
        setLive(null);
      }
    },
    [autoAvailable, chooseBranch, conversation, queryClient, t, toast],
  );

  const cancelSend = useCallback(() => abortRef.current?.abort(), []);

  const send = useCallback(
    (message: string, parentId: number | null) =>
      void runStream("/api/assistant/chat/stream", { message, parentId }, message),
    [runStream],
  );

  /** Retry re-answers the SAME question: a sibling of the answer it replaces,
   *  reachable again through the branch switcher. */
  const retry = useCallback(
    (entry: PathEntry) => {
      const parent = entry.msg.parentId;
      if (parent == null) return;
      void runStream(
        "/api/assistant/chat/stream",
        { parentId: parent, regenerate: true },
        null,
        entry.msg.id,
      );
    },
    [runStream],
  );

  /** Editing a question forks from the same point the original hung off. */
  const submitEdit = useCallback(
    (entry: PathEntry, text: string) => {
      setEditing(null);
      void runStream(
        "/api/assistant/chat/stream",
        { message: text, parentId: entry.msg.parentId ?? null },
        text,
        entry.msg.id,
      );
    },
    [runStream],
  );

  /** Approving resumes the paused turn: the actions run, then the model gets
   *  their output and finishes the answer it was in the middle of. */
  const applyActions = useCallback(() => {
    if (!pending) return;
    void runStream(
      "/api/assistant/actions/apply",
      { actions: pending.actions, parentId: pending.parentId },
      null,
    );
  }, [pending, runStream]);

  const clear = useMutation({
    mutationFn: async () => {
      unwrap(await api.DELETE("/assistant/history"));
    },
    onSuccess: () => {
      setConfirmClear(false);
      setChoice({});
      saveChoices(conversation, {});
      setOutcomes({});
      saveOutcomes(conversation, {});
      void queryClient.invalidateQueries({ queryKey: ["assistant", "history"] });
    },
  });

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth", block: "end" });
  }, [path.length, live, pending]);

  // A retry or an edit stands in for a message already on screen: hide it and
  // everything after it while the replacement streams, so the new version
  // appears exactly where the old one was rather than below it.
  const cut = live?.replacing != null ? path.findIndex((e) => e.msg.id === live.replacing) : -1;
  const shown = cut >= 0 ? path.slice(0, cut) : path;

  // Land in the composer on open and whenever the window regains focus, so
  // typing or pasting works immediately after tabbing back in. Skipped on
  // touch, where stealing focus would throw the on-screen keyboard up
  // unbidden every time the app comes forward.
  useEffect(() => {
    if (!window.matchMedia("(hover: hover)").matches) return;
    const focusComposer = () => {
      const el = composerRef.current;
      if (!el || el === document.activeElement) return;
      // Never yank focus out of a dialog or a field already in use.
      const active = document.activeElement;
      if (active && active !== document.body) return;
      if (document.querySelector("[role='dialog']")) return;
      el.focus();
    };
    focusComposer();
    window.addEventListener("focus", focusComposer);
    return () => window.removeEventListener("focus", focusComposer);
  }, []);

  // Grow the field with its content up to a few lines, then let it scroll.
  useEffect(() => {
    const el = composerRef.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = Math.min(el.scrollHeight, COMPOSER_MAX_PX) + "px";
  }, [draft]);

  const submit = () => {
    const message = draft.trim();
    if (!message || streaming) return;
    setDraft("");
    composerRef.current?.focus();
    send(message, leafId);
  };

  return (
    <div className="flex min-h-0 flex-1 flex-col gap-3">
      <div
        className={cn(
          "flex-1 space-y-3 overflow-y-auto pr-1",
          variant === "page" ? "min-h-[300px]" : "min-h-0",
        )}
      >
        {history.isLoading && <PageLoader className="py-10" />}
        {!history.isLoading && path.length === 0 && !live && (
          <div className="text-fg-muted flex h-full flex-col items-center justify-center gap-1 py-10 text-center text-sm">
            <Sparkles className="text-fg-subtle mb-1 size-5" />
            {t("assistant.emptyHint")}
            <span className="text-fg-subtle text-[12.5px]">{t("assistant.emptyExamples")}</span>
          </div>
        )}
        {shown.map((entry) => (
          <Fragment key={entry.msg.id}>
            {(outcomes[entry.msg.id] ?? []).map((o, i) => (
              <ActionOutcomeCard key={i} outcome={o} />
            ))}
            <MessageBubble
              entry={entry}
              compact={variant === "widget"}
              busy={streaming}
              editing={editing === entry.msg.id}
              onEdit={() => setEditing(entry.msg.id)}
              onCancelEdit={() => setEditing(null)}
              onSubmitEdit={(text) => submitEdit(entry, text)}
              onRetry={() => retry(entry)}
              onPickBranch={(childId) => chooseBranch(branchKey(entry.msg, knownIds), childId)}
            />
          </Fragment>
        ))}
        {live && (
          <>
            {live.userText !== null && (
              <SimpleBubble role="user" content={live.userText} />
            )}
            {live.outcomes.map((o, i) => (
              <ActionOutcomeCard key={i} outcome={o} />
            ))}
            <StreamingBubble live={live} />
          </>
        )}
        {pending && !live && (
          <ConfirmCard
            actions={pending.actions}
            onApprove={applyActions}
            onCancel={() => setPending(null)}
          />
        )}
        <div ref={bottomRef} />
      </div>

      {/* One composer surface — the field on top, controls beneath — so the
          model switcher sits with the send button instead of floating loose
          on the page. The border tracks focus as if it were a single input. */}
      <div className="border-edge bg-surface shadow-1 focus-within:border-accent rounded-xl border transition-colors">
        <textarea
          ref={composerRef}
          // The wrapper above owns the focus indication; without this the
          // global :focus-visible ring draws a second box around the field.
          data-focus-ring="none"
          rows={1}
          value={draft}
          placeholder={t("assistant.placeholder")}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
          }}
          // Height is driven by the content (see the effect above), capped at
          // COMPOSER_MAX_PX — a one-line field for a paragraph-long question
          // is its own small misery.
          className="text-fg placeholder:text-fg-subtle w-full resize-none bg-transparent px-3 pt-2.5 pb-1.5 text-sm focus:outline-none"
        />
        <div className="flex items-center justify-between gap-2 px-2 pb-2">
          <AssistantModelPicker compact={variant === "widget"} />
          <div className="flex items-center gap-1">
            {path.length > 0 && (
              <Button
                size="sm"
                variant="ghost"
                onClick={() => setConfirmClear(true)}
                aria-label={t("assistant.clear")}
              >
                <Eraser className="size-4" />
              </Button>
            )}
            {streaming ? (
              <Button size="sm" variant="secondary" onClick={cancelSend} aria-label={t("assistant.cancel")}>
                <Square className="size-3.5 fill-current" />
              </Button>
            ) : (
              <Button
                size="sm"
                variant="primary"
                disabled={!draft.trim()}
                onClick={submit}
                aria-label={t("assistant.send")}
              >
                <Send className="size-4" />
              </Button>
            )}
          </div>
        </div>
      </div>

      <ConfirmDialog
        open={confirmClear}
        onOpenChange={setConfirmClear}
        title={t("assistant.clearTitle")}
        body={t("assistant.clearBody")}
        confirmLabel={t("assistant.clear")}
        loading={clear.isPending}
        onConfirm={() => clear.mutate()}
      />
    </div>
  );
}

/* ---- message rendering ------------------------------------------------------ */

/** A bubble with no history behind it — the question echoed while its turn
 *  is still running, before the stored row exists. */
function SimpleBubble({ role, content }: { role: "user" | "assistant"; content: string }) {
  const isUser = role === "user";
  return (
    <div className={cn("flex", isUser ? "justify-end" : "justify-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-xl px-3.5 py-2.5 text-sm",
          isUser ? "bg-accent text-accent-fg" : "bg-bg-subtle text-fg",
        )}
      >
        <p className="whitespace-pre-wrap">{content}</p>
      </div>
    </div>
  );
}

function MessageBubble({
  entry,
  compact,
  busy,
  editing,
  onEdit,
  onCancelEdit,
  onSubmitEdit,
  onRetry,
  onPickBranch,
}: {
  entry: PathEntry;
  compact?: boolean;
  busy: boolean;
  editing: boolean;
  onEdit: () => void;
  onCancelEdit: () => void;
  onSubmitEdit: (text: string) => void;
  onRetry: () => void;
  onPickBranch: (childId: number) => void;
}) {
  const item = entry.msg;
  const isUser = item.role === "user";

  if (editing) {
    return <EditBubble initial={item.content} onCancel={onCancelEdit} onSubmit={onSubmitEdit} />;
  }

  return (
    <div className={cn("group/msg flex flex-col gap-1", isUser ? "items-end" : "items-start")}>
      <div
        className={cn(
          "max-w-[85%] rounded-xl px-3.5 py-2.5 text-sm",
          isUser ? "bg-accent text-accent-fg" : "bg-bg-subtle text-fg",
        )}
      >
        {isUser ? (
          <p className="whitespace-pre-wrap">{item.content}</p>
        ) : (
          <AssistantMarkdown content={item.content} />
        )}
        {!isUser && <ActionChips steps={item.steps ?? []} compact={compact} />}
      </div>
      <MessageActions
        entry={entry}
        busy={busy}
        onEdit={onEdit}
        onRetry={onRetry}
        onPickBranch={onPickBranch}
      />
    </div>
  );
}

/** The edit-in-place form: submitting forks a new branch from the same point
 *  the original question hung off, leaving the old one reachable. */
function EditBubble({
  initial,
  onCancel,
  onSubmit,
}: {
  initial: string;
  onCancel: () => void;
  onSubmit: (text: string) => void;
}) {
  const { t } = useTranslation();
  const [text, setText] = useState(initial);
  const submit = () => {
    const trimmed = text.trim();
    if (trimmed) onSubmit(trimmed);
  };
  return (
    <div className="flex justify-end">
      <div className="border-accent/50 bg-surface w-full max-w-[85%] rounded-xl border p-2.5">
        <Textarea
          autoFocus
          rows={3}
          value={text}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey) {
              e.preventDefault();
              submit();
            }
            if (e.key === "Escape") onCancel();
          }}
        />
        <div className="mt-2 flex justify-end gap-2">
          <Button size="sm" variant="ghost" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
          <Button size="sm" variant="primary" disabled={!text.trim()} onClick={submit}>
            {t("assistant.actions.saveEdit")}
          </Button>
        </div>
      </div>
    </div>
  );
}

/**
 * The per-message controls. They stay faint until the message is hovered or
 * focused, so a long transcript doesn't read as a wall of buttons — except
 * the branch switcher, which is state, not an action, and is always legible.
 */
function MessageActions({
  entry,
  busy,
  onEdit,
  onRetry,
  onPickBranch,
}: {
  entry: PathEntry;
  busy: boolean;
  onEdit: () => void;
  onRetry: () => void;
  onPickBranch: (childId: number) => void;
}) {
  const { t } = useTranslation();
  const toast = useToast();
  const isUser = entry.msg.role === "user";
  const speech = useSpeech();
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(entry.msg.content);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 1400);
    } catch {
      toast("error", t("assistant.actions.copyFailed"));
    }
  };

  return (
    <div className={cn("flex items-center gap-0.5", isUser ? "flex-row-reverse" : "flex-row")}>
      {entry.siblings.length > 1 && (
        <BranchSwitcher entry={entry} busy={busy} onPick={onPickBranch} />
      )}
      {entry.msg.usage && <UsageInfo usage={entry.msg.usage} />}
      <div className="flex items-center gap-0.5 opacity-100 transition-opacity focus-within:opacity-100 group-hover/msg:opacity-100 [@media(hover:hover)]:opacity-0">
        <IconAction label={copied ? t("assistant.actions.copied") : t("assistant.actions.copy")} onClick={copy}>
          {copied ? <Check className="text-success size-3.5" /> : <Copy className="size-3.5" />}
        </IconAction>
        {isUser ? (
          <IconAction label={t("assistant.actions.edit")} onClick={onEdit} disabled={busy}>
            <Pencil className="size-3.5" />
          </IconAction>
        ) : (
          <>
            <IconAction label={t("assistant.actions.retry")} onClick={onRetry} disabled={busy}>
              <RotateCcw className="size-3.5" />
            </IconAction>
            {speech.supported && (
              <IconAction
                label={speech.speaking ? t("assistant.actions.stopSpeaking") : t("assistant.actions.speak")}
                onClick={() => (speech.speaking ? speech.stop() : speech.speak(entry.msg.content))}
              >
                {speech.speaking ? <VolumeX className="text-accent size-3.5" /> : <Volume2 className="size-3.5" />}
              </IconAction>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/**
 * The cost receipt: an info dot that reveals what the answer took to produce.
 * Always visible rather than hover-revealed like the actions, since it is a
 * property of the message; the numbers themselves stay behind the hover so a
 * transcript doesn't turn into an invoice.
 */
function UsageInfo({ usage }: { usage: NonNullable<ChatMessage["usage"]> }) {
  const { t } = useTranslation();
  return (
    <Tooltip
      // Opens downward: the actions row sits under the message, so a bubble
      // above it covers the answer you just read.
      side="bottom"
      // One line, sized to its content, on a surface that belongs to the
      // current theme rather than the inverted chip short labels use.
      className="bg-overlay text-fg border-edge max-w-none border font-normal whitespace-nowrap"
      label={<span className="tabular-nums">{usageLine(usage)}</span>}
    >
      <button
        type="button"
        aria-label={t("assistant.usage.label")}
        className="text-fg-subtle hover:text-fg cursor-help rounded-md p-1 transition-colors"
      >
        <Info className="size-3.5" />
      </button>
    </Tooltip>
  );
}

function IconAction({
  label,
  onClick,
  disabled,
  children,
}: {
  label: string;
  onClick: () => void;
  disabled?: boolean;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      disabled={disabled}
      onClick={onClick}
      className="text-fg-subtle hover:text-fg hover:bg-surface-hover cursor-pointer rounded-md p-1 transition-colors disabled:pointer-events-none disabled:opacity-40"
    >
      {children}
    </button>
  );
}

/** ‹ 2/3 › — moves between the alternative answers or questions that fork at
 *  this point in the conversation. */
function BranchSwitcher({
  entry,
  busy,
  onPick,
}: {
  entry: PathEntry;
  busy: boolean;
  onPick: (childId: number) => void;
}) {
  const { t } = useTranslation();
  const at = entry.index;
  const total = entry.siblings.length;
  return (
    <span className="text-fg-subtle flex items-center gap-0.5 text-[11px] tabular-nums">
      <button
        type="button"
        aria-label={t("assistant.actions.prevBranch")}
        disabled={busy || at === 0}
        onClick={() => onPick(entry.siblings[at - 1])}
        className="hover:text-fg cursor-pointer rounded p-0.5 transition-colors disabled:pointer-events-none disabled:opacity-30"
      >
        <ChevronLeft className="size-3.5" />
      </button>
      {at + 1}/{total}
      <button
        type="button"
        aria-label={t("assistant.actions.nextBranch")}
        disabled={busy || at === total - 1}
        onClick={() => onPick(entry.siblings[at + 1])}
        className="hover:text-fg cursor-pointer rounded p-0.5 transition-colors disabled:pointer-events-none disabled:opacity-30"
      >
        <ChevronRight className="size-3.5" />
      </button>
    </span>
  );
}

/**
 * useSpeech wraps the browser's speech synthesis. It reads the message as
 * PROSE: fenced code, inline code and markdown punctuation are stripped, since
 * a wall of shell output read aloud character by character is useless.
 */
function useSpeech() {
  const [speaking, setSpeaking] = useState(false);
  const supported = typeof window !== "undefined" && "speechSynthesis" in window;

  // A page navigated away from mid-sentence would otherwise keep talking.
  useEffect(() => () => { if (supported) window.speechSynthesis.cancel(); }, [supported]);

  const stop = useCallback(() => {
    if (!supported) return;
    window.speechSynthesis.cancel();
    setSpeaking(false);
  }, [supported]);

  const speak = useCallback(
    (markdown: string) => {
      if (!supported) return;
      window.speechSynthesis.cancel();
      const utterance = new SpeechSynthesisUtterance(speechText(markdown));
      utterance.onend = () => setSpeaking(false);
      utterance.onerror = () => setSpeaking(false);
      setSpeaking(true);
      window.speechSynthesis.speak(utterance);
    },
    [supported],
  );

  return { supported, speaking, speak, stop };
}

/** speechText reduces a markdown answer to what is worth hearing. */
export function speechText(markdown: string): string {
  return markdown
    .replace(/```[\s\S]*?```/g, " (code block) ")
    .replace(/`([^`]*)`/g, "$1")
    .replace(/!\[[^\]]*\]\([^)]*\)/g, "")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/^\s*[#>]+\s*/gm, "")
    .replace(/^\s*[-*+]\s+/gm, "")
    .replace(/[*_~|]/g, "")
    .replace(/\n{2,}/g, ". ")
    .replace(/\s+/g, " ")
    .trim();
}

/** The in-progress assistant bubble: interim text as it streams, a spinner
 * per running tool, and an ongoing indicator until the turn finishes. */
function StreamingBubble({ live }: { live: Live }) {
  const { t } = useTranslation();
  const text = live.segments.join("\n\n");
  return (
    <div className="flex justify-start">
      <div className="bg-bg-subtle text-fg max-w-[85%] rounded-xl px-3.5 py-2.5 text-sm">
        {text ? (
          <AssistantMarkdown content={text} />
        ) : (
          <span className="text-fg-muted flex items-center gap-2 text-[13px]">
            <Sparkles className="size-3.5 animate-pulse" />
            {t("assistant.thinking")}
          </span>
        )}
        {live.tools.length > 0 && (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {live.tools.map((s, i) => (
              <span
                key={i}
                className="border-edge text-fg-muted inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[11px]"
              >
                {s.running ? (
                  <Loader2 className="size-3 animate-spin" />
                ) : s.isError ? (
                  <X className="text-danger size-3" />
                ) : (
                  <Check className="text-success size-3" />
                )}
                {toolLabel(s.name)}
              </span>
            ))}
          </div>
        )}
        {live.error ? (
          <p className="text-danger mt-1.5 text-[12.5px]">{live.error}</p>
        ) : (
          <span className="text-fg-subtle mt-1.5 flex items-center gap-1.5 text-[11.5px]">
            <Loader2 className="size-3 animate-spin" />
            {t("assistant.working")}
          </span>
        )}
      </div>
    </div>
  );
}

/**
 * What an approved action actually did. The raw output belongs in a terminal
 * frame, not in the prose — the model quotes the lines that matter in its
 * answer, and this stays available underneath, collapsed by default.
 */
function ActionOutcomeCard({ outcome }: { outcome: ActionOutcome }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  const lines = outcome.output ? outcome.output.split("\n").length : 0;
  return (
    <div className="flex justify-start">
      <div className="border-edge bg-surface max-w-[85%] min-w-0 rounded-xl border px-3.5 py-2.5">
        <p className="text-fg flex items-center gap-2 text-[12.5px] font-medium">
          {outcome.failed ? (
            <X className="text-danger size-3.5 shrink-0" />
          ) : (
            <Check className="text-success size-3.5 shrink-0" />
          )}
          <Terminal className="text-fg-subtle size-3.5 shrink-0" />
          <span className="min-w-0 truncate">{outcome.summary}</span>
        </p>
        {outcome.output && (
          <>
            <button
              type="button"
              onClick={() => setOpen((o) => !o)}
              className="text-fg-subtle hover:text-fg mt-1.5 cursor-pointer text-[11.5px] underline underline-offset-2"
            >
              {open ? t("assistant.actions.hideOutput") : t("assistant.actions.showOutput", { count: lines })}
            </button>
            {open && (
              <pre className="bg-bg border-edge text-fg-muted mt-1.5 max-h-72 overflow-auto rounded-md border px-3 py-2 font-mono text-[12px] whitespace-pre">
                {outcome.output}
              </pre>
            )}
          </>
        )}
      </div>
    </div>
  );
}

/** The write-action confirmation gate: nothing has changed yet. Approving
 *  runs the actions and resumes the answer they were queued for. */
function ConfirmCard({
  actions,
  onApprove,
  onCancel,
}: {
  actions: PendingAction[];
  onApprove: () => void;
  onCancel: () => void;
}) {
  const { t } = useTranslation();
  return (
    <div className="flex justify-start">
      <div className="border-accent/40 bg-accent-soft/30 max-w-[85%] rounded-xl border px-3.5 py-2.5">
        <p className="text-fg mb-1.5 text-[13px] font-medium">{t("assistant.confirmTitle")}</p>
        <ul className="mb-2.5 space-y-1">
          {actions.map((a, i) => (
            <li key={i} className="text-fg-muted flex items-start gap-1.5 text-[12.5px]">
              <span className="bg-accent mt-1.5 size-1.5 shrink-0 rounded-full" />
              <span className="min-w-0 break-words">{a.summary}</span>
            </li>
          ))}
        </ul>
        <div className="flex gap-2">
          <Button size="sm" variant="primary" onClick={onApprove}>
            <Check className="size-3.5" />
            {t("assistant.approve")}
          </Button>
          <Button size="sm" variant="ghost" onClick={onCancel}>
            {t("common.cancel")}
          </Button>
        </div>
      </div>
    </div>
  );
}

/** Markdown scoped to chat: bold, italics, code, lists, links, tables —
 * styled with design tokens, no raw HTML ever. */
function AssistantMarkdown({ content }: { content: string }) {
  return (
    <div className="min-w-0 space-y-2 [&>*:first-child]:mt-0 [&>*:last-child]:mb-0">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          p: (props) => <p className="leading-relaxed whitespace-pre-wrap" {...props} />,
          strong: (props) => <strong className="font-semibold" {...props} />,
          em: (props) => <em className="italic" {...props} />,
          a: ({ href, children }) => (
            <a
              href={href}
              target="_blank"
              rel="noopener noreferrer"
              className="text-accent underline underline-offset-2"
            >
              {children}
            </a>
          ),
          ul: (props) => <ul className="ml-4 list-disc space-y-1" {...props} />,
          ol: (props) => <ol className="ml-4 list-decimal space-y-1" {...props} />,
          li: (props) => <li className="leading-relaxed" {...props} />,
          code: ({ className, children }) => {
            const block = /language-/.test(className ?? "");
            return block ? (
              <code className="block font-mono text-[12.5px] whitespace-pre">{children}</code>
            ) : (
              <code className="bg-bg rounded px-1 py-0.5 font-mono text-[12.5px]">{children}</code>
            );
          },
          pre: (props) => (
            <pre
              className="bg-bg border-edge max-h-96 overflow-auto rounded-md border px-3 py-2 text-[12.5px]"
              {...props}
            />
          ),
          h1: (props) => <p className="text-[15px] font-semibold" {...props} />,
          h2: (props) => <p className="text-[14px] font-semibold" {...props} />,
          h3: (props) => <p className="font-semibold" {...props} />,
          table: (props) => (
            <div className="overflow-x-auto">
              <table className="border-edge w-full border-collapse text-[12.5px] [&_td]:border [&_td]:border-(--hh-edge) [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-(--hh-edge) [&_th]:px-2 [&_th]:py-1 [&_th]:text-left [&_th]:font-semibold" {...props} />
            </div>
          ),
          blockquote: (props) => (
            <blockquote className="border-edge-strong text-fg-muted border-l-2 pl-3" {...props} />
          ),
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  );
}

/* ---- action chips ----------------------------------------------------------- */

/**
 * Chips are the receipt of what the model DID, and live handles on it:
 * touched Home Assistant entities open instant controls, containers show
 * lifecycle buttons, everything else links into its module. Read tools
 * stay as quiet wrench tags.
 */
function ActionChips({ steps, compact }: { steps: AssistantStep[]; compact?: boolean }) {
  const { t } = useTranslation();
  if (steps.length === 0) return null;

  const str = (step: AssistantStep, key: string) =>
    typeof step.input?.[key] === "string" ? (step.input[key] as string) : "";

  const ok = steps.filter((s) => !s.isError);
  const haSteps = ok.filter((s) => s.tool === "ha_call_service" && str(s, "entityId"));
  // One chip per distinct entity; one grouped chip when several were touched.
  const haEntities = [...new Map(haSteps.map((s) => [str(s, "systemId") + "|" + str(s, "entityId"), s])).values()];
  const dockerSteps = [...new Map(
    ok.filter((s) => s.tool === "docker_container_action" && str(s, "name"))
      .map((s) => [str(s, "systemId") + "|" + str(s, "name"), s]),
  ).values()];
  const linkSteps = ok.filter((s) =>
    ["mc_command", "amp_instance_action", "pve_guest_action", "dns_blocking"].includes(s.tool),
  );
  const readTools = [...new Set(steps.filter((s) =>
    !["ha_call_service", "docker_container_action", "mc_command", "amp_instance_action", "pve_guest_action", "dns_blocking"].includes(s.tool),
  ).map((s) => s.tool))];

  const hasAction = haEntities.length + dockerSteps.length + linkSteps.length > 0;
  if (!hasAction && readTools.length === 0) return null;

  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      {haEntities.length === 1 && (
        <HaEntityChip systemId={str(haEntities[0], "systemId")} entityId={str(haEntities[0], "entityId")} />
      )}
      {haEntities.length > 1 && (
        <HaGroupChip
          entities={haEntities.map((s) => ({ systemId: str(s, "systemId"), entityId: str(s, "entityId") }))}
        />
      )}
      {dockerSteps.map((s) => (
        <DockerChip key={str(s, "name")} systemId={str(s, "systemId")} name={str(s, "name")} />
      ))}
      {linkSteps.map((s, i) => (
        <LinkChip key={i} step={s} />
      ))}
      {readTools.map((tool) => (
        <Badge key={tool} tone="neutral">
          <Wrench className="size-2.5" />
          {tool}
        </Badge>
      ))}
      {!compact && hasAction && (
        <span className="text-fg-subtle text-[10.5px]">{t("assistant.chips.tapHint")}</span>
      )}
    </div>
  );
}

const chipClass =
  "border-edge bg-surface hover:border-accent/60 hover:bg-surface-hover flex cursor-pointer items-center gap-1.5 rounded-full border px-2.5 py-1 text-[12px] font-medium shadow-1 transition-colors";

/** One touched Home Assistant entity: live state dot + instant controls. */
function HaEntityChip({ systemId, entityId }: { systemId: string; entityId: string }) {
  const [open, setOpen] = useState(false);
  const entity = useHaEntity(systemId, entityId, open);
  const on = entity.data?.state === "on";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Lightbulb className={cn("size-3", on ? "text-warning" : "text-fg-subtle")} />
          <span className="text-fg">{entity.data?.name ?? entityId.split(".")[1]}</span>
          <StatusDot tone={on ? "success" : "neutral"} />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64">
        <HaEntityControls systemId={systemId} entityId={entityId} />
      </PopoverContent>
    </Popover>
  );
}

/** Several entities in one turn: a grouped chip that expands to a rail of
 * individually controllable rows. */
function HaGroupChip({ entities }: { entities: Array<{ systemId: string; entityId: string }> }) {
  const { t } = useTranslation();
  const [open, setOpen] = useState(false);
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Lightbulb className="text-warning size-3" />
          <span className="text-fg">{t("assistant.chips.devices", { count: entities.length })}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="max-h-80 w-72 space-y-3 overflow-y-auto">
        {entities.map((e) => (
          <HaEntityControls key={e.entityId} systemId={e.systemId} entityId={e.entityId} />
        ))}
      </PopoverContent>
    </Popover>
  );
}

function useHaEntity(systemId: string, entityId: string, live: boolean) {
  return useQuery({
    queryKey: ["ha", systemId, "entity", entityId],
    queryFn: async () =>
      unwrap(
        await api.GET("/ha/{id}/entities/{entityId}", {
          params: { path: { id: systemId, entityId } },
        }),
      ),
    refetchInterval: live ? 4_000 : false,
  });
}

/** Instant control row: name + state, toggle, brightness when it's a lit
 * light. The same service calls the HA boards make. */
function HaEntityControls({ systemId, entityId }: { systemId: string; entityId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const entity = useHaEntity(systemId, entityId, true);

  const callService = useMutation({
    mutationFn: async ({ service, data }: { service: string; data?: Record<string, unknown> }) => {
      unwrap(
        await api.POST("/ha/{id}/entities/{entityId}/service", {
          params: { path: { id: systemId, entityId } },
          body: data ? { service, data } : { service },
        }),
      );
    },
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["ha", systemId, "entity", entityId] }),
    onError: (err) =>
      toast("error", t("assistant.chips.controlFailed"), isApiError(err) ? err.message : undefined),
  });

  const e = entity.data as (HaEntity & { attributes?: Record<string, unknown> }) | undefined;
  if (!e) return <PageLoader className="py-4" />;

  const domain = entityId.split(".")[0];
  const toggleable = ["light", "switch", "fan", "input_boolean", "media_player"].includes(domain);
  const on = e.state === "on";
  const brightness = typeof e.attributes?.brightness === "number" ? (e.attributes.brightness as number) : null;

  return (
    <div className="space-y-2.5">
      <div className="flex items-center justify-between gap-3">
        <div className="min-w-0">
          <p className="text-fg truncate text-[13px] font-medium">{e.name}</p>
          <p className="text-fg-subtle truncate text-[11.5px]">
            {entityId} · {e.state}
          </p>
        </div>
        {toggleable ? (
          <Switch
            checked={on}
            disabled={callService.isPending}
            onCheckedChange={() => callService.mutate({ service: "toggle" })}
            aria-label={t("assistant.chips.toggle", { name: e.name })}
          />
        ) : (
          <Badge tone={on ? "success" : "neutral"}>{e.state}</Badge>
        )}
      </div>
      {domain === "light" && on && brightness !== null && (
        <Slider
          value={Math.round((brightness / 255) * 100)}
          min={1}
          max={100}
          onValueCommit={(v) => callService.mutate({ service: "turn_on", data: { brightness_pct: v } })}
          aria-label={t("assistant.chips.brightness")}
        />
      )}
    </div>
  );
}

/** A container the model touched: live state + lifecycle buttons. */
function DockerChip({ systemId, name }: { systemId: string; name: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);

  const container = useQuery({
    queryKey: ["docker", systemId, "container", name],
    queryFn: async () =>
      unwrap(
        await api.GET("/docker/{id}/containers/{name}", {
          params: { path: { id: systemId, name } },
        }),
      ),
    enabled: open,
    refetchInterval: open ? 4_000 : false,
  });

  const action = useMutation({
    mutationFn: async (act: "start" | "stop" | "restart") => {
      unwrap(
        await api.POST("/docker/{id}/containers/{name}/action", {
          params: { path: { id: systemId, name } },
          body: { action: act },
        }),
      );
    },
    onSuccess: () =>
      void queryClient.invalidateQueries({ queryKey: ["docker", systemId, "container", name] }),
    onError: (err) =>
      toast("error", t("assistant.chips.controlFailed"), isApiError(err) ? err.message : undefined),
  });

  const running = container.data?.container.state === "running";
  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button type="button" className={chipClass}>
          <Box className="text-fg-subtle size-3" />
          <span className="text-fg">{name}</span>
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-60 space-y-2.5">
        <div className="flex items-center justify-between gap-2">
          <p className="text-fg truncate text-[13px] font-medium">{name}</p>
          <Badge tone={running ? "success" : "neutral"}>
            {container.data?.container.state ?? "…"}
          </Badge>
        </div>
        <div className="flex gap-1.5">
          {running ? (
            <>
              <Button size="sm" variant="secondary" loading={action.isPending} onClick={() => action.mutate("restart")}>
                {t("assistant.chips.restart")}
              </Button>
              <Button size="sm" variant="danger-ghost" loading={action.isPending} onClick={() => action.mutate("stop")}>
                {t("assistant.chips.stop")}
              </Button>
            </>
          ) : (
            <Button size="sm" variant="primary" loading={action.isPending} onClick={() => action.mutate("start")}>
              {t("assistant.chips.start")}
            </Button>
          )}
        </div>
      </PopoverContent>
    </Popover>
  );
}

/** Actions whose home is a module page: chip navigates there. */
function LinkChip({ step }: { step: AssistantStep }) {
  const systemId = typeof step.input?.systemId === "string" ? (step.input.systemId as string) : "";
  let to = "";
  let icon = Server;
  let label = step.tool;
  switch (step.tool) {
    case "mc_command":
      to = `/mc/${systemId}`;
      icon = Pickaxe;
      label = typeof step.input?.command === "string" ? `/${step.input.command as string}` : "console";
      break;
    case "amp_instance_action":
      to = `/amp/${systemId}`;
      icon = Gamepad2;
      label = `${step.input?.action ?? ""} ${step.input?.instanceId ?? ""}`.trim();
      break;
    case "pve_guest_action":
      to = `/pve/${systemId}`;
      icon = Server;
      label = `${step.input?.action ?? ""} #${step.input?.vmid ?? ""}`.trim();
      break;
    case "dns_blocking":
      to = "/dns";
      icon = ShieldCheck;
      label = step.input?.enabled === false ? "blocking off" : "blocking on";
      break;
  }
  const Icon = icon;
  if (!to) return null;
  return (
    <Link to={to} className={chipClass}>
      <Icon className="text-fg-subtle size-3" />
      <span className="text-fg">{label}</span>
    </Link>
  );
}
