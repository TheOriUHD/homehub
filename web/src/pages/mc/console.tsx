import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent as ReactKeyboardEvent,
} from "react";
import { useMutation } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useTranslation } from "react-i18next";
import { ArrowDown, ChevronRight, Loader2, WifiOff } from "lucide-react";
import { api, unwrap, isApiError, type McConsoleLine } from "@/lib/api";
import { useAuth } from "@/lib/auth";
import { live } from "@/lib/ws";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { useToast } from "@/ui/toast";
import { mcResource, mcTopic, useMcOverview } from "./shared";

// The console is a fixed dark surface in BOTH themes — like a terminal, not a
// themed card — so the palette below is deliberately hard-coded, not tokens.
const PANE = {
  bg: "#101319",
  chrome: "#161a22",
  edge: "#262b36",
  text: "#ccd2de",
  dim: "#5b6272",
  warn: "#fbbf24",
  error: "#f87171",
  accentText: "#e7e9f0",
};

const LINE_H = 20; // px — fixed row height makes windowing arithmetic exact
const TOP_H = 36; // px — reserved slot for "load older" / history divider
const OVERSCAN = 12; // rows rendered beyond the viewport on each side
const INITIAL_LINES = 2000;
const PAGE = 1000;
const MAX_BUFFER = 100_000;
const PIN_THRESHOLD = 48; // px from the bottom still counting as "pinned"

function fmtTime(ts: number): string {
  const d = new Date(ts);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}

function levelColor(level: string | undefined): string {
  const l = (level ?? "").toUpperCase();
  if (l.startsWith("ERROR") || l.startsWith("FATAL") || l.startsWith("SEVERE")) return PANE.error;
  if (l.startsWith("WARN")) return PANE.warn;
  return PANE.text;
}

export function McConsolePage() {
  const { t } = useTranslation();
  const { instanceId } = useParams({ from: "/shell/mc/$instanceId/" });
  const { can } = useAuth();
  const toast = useToast();
  const overview = useMcOverview(instanceId);

  // The line buffer is a mutable ref — appending 100k lines through React
  // state would copy the array on every push. `version` bumps trigger renders.
  const linesRef = useRef<McConsoleLine[]>([]);
  const [version, setVersion] = useState(0);
  const bump = useCallback(() => setVersion((v) => v + 1), []);

  const oldestHeldRef = useRef(0);
  // seq of the newest line held; when the buffer is empty this is the seq just
  // before the next expected line, so "fresh" checks stay uniform.
  const lastSeqRef = useRef(-1);
  const initialLoadedRef = useRef(false);
  const catchingUpRef = useRef(false);

  const [phase, setPhase] = useState<"loading" | "ready" | "error">("loading");
  const [retryToken, setRetryToken] = useState(0);
  const [loadingOlder, setLoadingOlder] = useState(false);

  // Windowing + pinned-to-bottom scroll state.
  const scrollRef = useRef<HTMLDivElement>(null);
  const [range, setRange] = useState({ start: 0, end: 0 });
  const pinnedRef = useRef(true);
  const [pinned, setPinned] = useState(true);
  // Pixels to shift scrollTop after the next commit (prepends push down,
  // front-trims pull up) so the viewport keeps showing the same lines.
  const scrollAdjustRef = useRef(0);

  const fetchPage = useCallback(
    async (after: number, limit: number) =>
      unwrap(
        await api.GET("/mc/{id}/console", {
          params: { path: { id: instanceId }, query: { after, limit } },
        }),
      ),
    [instanceId],
  );

  const push = useCallback(
    (fresh: McConsoleLine[]) => {
      if (fresh.length === 0) return;
      const buf = linesRef.current;
      buf.push(...fresh);
      const excess = buf.length - MAX_BUFFER;
      if (excess > 0) {
        buf.splice(0, excess);
        scrollAdjustRef.current -= excess * LINE_H;
      }
      lastSeqRef.current = buf[buf.length - 1].seq;
      bump();
    },
    [bump],
  );

  /** Load the newest window, replacing the buffer. Initial load and resets. */
  const loadTail = useCallback(async () => {
    // One tiny probe to learn nextSeq, then fetch the newest window.
    const probe = await fetchPage(-1, 1);
    oldestHeldRef.current = probe.oldestSeq;
    const after = Math.max(probe.oldestSeq - 1, probe.nextSeq - INITIAL_LINES);
    let lines: McConsoleLine[] = [];
    if (probe.nextSeq - after - 1 > 0) {
      const res = await fetchPage(after, INITIAL_LINES);
      oldestHeldRef.current = res.oldestSeq;
      lines = res.lines.filter((l) => l.seq > after);
    }
    linesRef.current = lines;
    lastSeqRef.current = lines.length > 0 ? lines[lines.length - 1].seq : after;
    pinnedRef.current = true;
    setPinned(true);
    bump();
  }, [fetchPage, bump]);

  /** Fill any gap between the buffer and the server's newest line. */
  const catchUp = useCallback(async () => {
    if (catchingUpRef.current || !initialLoadedRef.current) return;
    catchingUpRef.current = true;
    try {
      for (let i = 0; i < 8; i++) {
        const res = await fetchPage(lastSeqRef.current, INITIAL_LINES);
        if (res.nextSeq <= lastSeqRef.current) {
          // Sequences moved backwards — the server restarted its buffer.
          await loadTail();
          break;
        }
        oldestHeldRef.current = res.oldestSeq;
        push(res.lines.filter((l) => l.seq > lastSeqRef.current));
        if (res.lines.length < INITIAL_LINES) break;
      }
    } catch {
      // The next WS event retries the catch-up.
    } finally {
      catchingUpRef.current = false;
    }
  }, [fetchPage, loadTail, push]);

  // Initial load per instance.
  useEffect(() => {
    let cancelled = false;
    initialLoadedRef.current = false;
    setPhase("loading");
    linesRef.current = [];
    lastSeqRef.current = -1;
    void (async () => {
      try {
        await loadTail();
        if (cancelled) return;
        initialLoadedRef.current = true;
        setPhase("ready");
      } catch {
        if (!cancelled) setPhase("error");
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [instanceId, loadTail, retryToken]);

  // Live lines: append when the sequence continues the buffer, otherwise
  // refetch the missing stretch. Companion reconnects also catch up — lines
  // captured while the tab was subscribed but the companion was switching.
  useEffect(() => {
    return live.subscribe(mcTopic(instanceId), (payload) => {
      const frame = payload as { event?: string; data?: unknown } | undefined;
      if (frame?.event === "console") {
        if (!initialLoadedRef.current) return;
        const incoming = (frame.data as { lines?: McConsoleLine[] } | undefined)?.lines ?? [];
        const fresh = incoming.filter((l) => l.seq > lastSeqRef.current);
        if (fresh.length === 0) return;
        if (fresh[0].seq === lastSeqRef.current + 1) push(fresh);
        else void catchUp();
      } else if (frame?.event === "companion-changed") {
        void catchUp();
      }
    });
  }, [instanceId, push, catchUp]);

  const total = linesRef.current.length;
  const atOldest = total > 0 && linesRef.current[0].seq <= oldestHeldRef.current;
  const evictedDivider = atOldest && oldestHeldRef.current > 0;

  const updateRange = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const count = linesRef.current.length;
    const start = Math.max(0, Math.floor((el.scrollTop - TOP_H) / LINE_H) - OVERSCAN);
    const end = Math.min(count, Math.ceil((el.scrollTop - TOP_H + el.clientHeight) / LINE_H) + OVERSCAN);
    setRange((prev) => (prev.start === start && prev.end === end ? prev : { start, end }));
    const atBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - PIN_THRESHOLD;
    pinnedRef.current = atBottom;
    setPinned(atBottom);
  }, []);

  // After every buffer change: keep the viewport stable across prepends and
  // trims, follow the tail while pinned, then recompute the visible window.
  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    if (pinnedRef.current) {
      el.scrollTop = el.scrollHeight;
      scrollAdjustRef.current = 0;
    } else if (scrollAdjustRef.current !== 0) {
      el.scrollTop += scrollAdjustRef.current;
      scrollAdjustRef.current = 0;
    }
    updateRange();
  }, [version, phase, updateRange]);

  useEffect(() => {
    const el = scrollRef.current;
    if (!el) return;
    const observer = new ResizeObserver(() => {
      if (pinnedRef.current) el.scrollTop = el.scrollHeight;
      updateRange();
    });
    observer.observe(el);
    return () => observer.disconnect();
  }, [phase, updateRange]);

  const loadOlder = useCallback(async () => {
    const first = linesRef.current[0]?.seq;
    if (first === undefined || first <= oldestHeldRef.current || loadingOlder) return;
    setLoadingOlder(true);
    try {
      const after = Math.max(oldestHeldRef.current - 1, first - PAGE - 1);
      const res = await fetchPage(after, first - after - 1);
      oldestHeldRef.current = res.oldestSeq;
      const older = res.lines.filter((l) => l.seq > after && l.seq < first);
      if (older.length > 0) {
        linesRef.current = [...older, ...linesRef.current];
        scrollAdjustRef.current += older.length * LINE_H;
        bump();
      }
    } catch (err) {
      toast("error", t("mc.console.loadOlderFailed"), isApiError(err) ? err.message : undefined);
    } finally {
      setLoadingOlder(false);
    }
  }, [fetchPage, loadingOlder, bump, toast, t]);

  const jumpToLatest = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    pinnedRef.current = true;
    setPinned(true);
    el.scrollTop = el.scrollHeight;
  }, []);

  const offline = overview.data ? !overview.data.connected : false;
  // Input visibility follows the client-side grant mirror (like every other
  // gated control in the app); a 403 that slips through still just toasts.
  const canControl = can(mcResource(instanceId), "control");

  const slice = linesRef.current.slice(range.start, range.end);

  return (
    <div
      className="flex flex-col overflow-hidden rounded-lg border"
      style={{
        background: PANE.bg,
        borderColor: PANE.edge,
        height: "min(680px, max(420px, calc(100vh - 21rem)))",
      }}
    >
      {offline && (
        <div
          role="status"
          className="flex items-center gap-2 border-b px-3.5 py-2 text-[12.5px]"
          style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.warn }}
        >
          <WifiOff aria-hidden className="size-3.5 shrink-0" />
          {t("mc.console.offlineBanner")}
        </div>
      )}

      <div className="relative min-h-0 flex-1">
        {phase === "loading" ? (
          <div className="flex h-full items-center justify-center">
            <Loader2 aria-hidden className="size-5 animate-spin" style={{ color: PANE.dim }} />
            <span className="sr-only">{t("common.loading")}</span>
          </div>
        ) : phase === "error" ? (
          <div className="flex h-full flex-col items-center justify-center gap-3 px-6 text-center">
            <p className="text-sm" style={{ color: PANE.text }}>
              {t("mc.console.loadFailed")}
            </p>
            <Button size="sm" variant="secondary" onClick={() => setRetryToken((n) => n + 1)}>
              {t("common.retry")}
            </Button>
          </div>
        ) : total === 0 ? (
          <div className="flex h-full items-center justify-center px-6 text-center">
            <p className="text-[13px]" style={{ color: PANE.dim }}>
              {t("mc.console.empty")}
            </p>
          </div>
        ) : (
          <>
            <div
              ref={scrollRef}
              onScroll={updateRange}
              className="h-full overflow-auto overscroll-contain font-mono text-[12.5px]"
              role="log"
              aria-label={t("mc.tabs.console")}
            >
              <div className="relative" style={{ height: TOP_H + total * LINE_H + 8 }}>
                <div
                  className="absolute inset-x-0 top-0 flex items-center justify-center"
                  style={{ height: TOP_H }}
                >
                  {!atOldest ? (
                    <button
                      type="button"
                      onClick={() => void loadOlder()}
                      disabled={loadingOlder}
                      className="flex cursor-pointer items-center gap-1.5 rounded-full border px-3 py-0.5 font-sans text-xs font-medium transition-opacity disabled:opacity-60"
                      style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.text }}
                    >
                      {loadingOlder && <Loader2 aria-hidden className="size-3 animate-spin" />}
                      {loadingOlder ? t("mc.console.loadingOlder") : t("mc.console.loadOlder")}
                    </button>
                  ) : evictedDivider ? (
                    <div className="flex w-full items-center gap-3 px-4" aria-hidden={false}>
                      <span className="h-px flex-1" style={{ background: PANE.edge }} />
                      <span className="font-sans text-[11px]" style={{ color: PANE.dim }}>
                        {t("mc.console.historyBegins")}
                      </span>
                      <span className="h-px flex-1" style={{ background: PANE.edge }} />
                    </div>
                  ) : null}
                </div>

                {slice.map((line, i) => (
                  <ConsoleRow
                    key={line.seq}
                    line={line}
                    style={{ top: TOP_H + (range.start + i) * LINE_H }}
                  />
                ))}
              </div>
            </div>

            {!pinned && (
              <button
                type="button"
                onClick={jumpToLatest}
                className="absolute bottom-3 left-1/2 flex -translate-x-1/2 cursor-pointer items-center gap-1.5 rounded-full border px-3 py-1 font-sans text-xs font-medium shadow-2 transition-colors"
                style={{ borderColor: PANE.edge, background: PANE.chrome, color: PANE.accentText }}
              >
                <ArrowDown aria-hidden className="size-3" />
                {t("mc.console.jumpToLatest")}
              </button>
            )}
          </>
        )}
      </div>

      {canControl && <CommandBar instanceId={instanceId} />}
    </div>
  );
}

function ConsoleRow({ line, style }: { line: McConsoleLine; style: CSSProperties }) {
  return (
    <div
      className="absolute left-0 flex min-w-full items-center whitespace-pre"
      style={{ ...style, height: LINE_H, lineHeight: `${LINE_H}px` }}
    >
      <span
        className="w-[4.75rem] shrink-0 pr-3 text-right text-[11px] select-none"
        style={{ color: PANE.dim }}
        aria-hidden
      >
        {fmtTime(line.ts)}
      </span>
      <span className="pr-4" style={{ color: levelColor(line.level) }}>
        {line.text}
      </span>
    </div>
  );
}

/**
 * CommandBar sends console commands. History is ↑/↓-navigable, local to this
 * mount, never persisted. While a send is in flight the field only looks
 * inert (readOnly + dimmed) so focus is never yanked away; the entered text
 * survives failures for editing and retrying.
 */
function CommandBar({ instanceId }: { instanceId: string }) {
  const { t } = useTranslation();
  const toast = useToast();
  const [value, setValue] = useState("");
  const historyRef = useRef<string[]>([]);
  const indexRef = useRef<number | null>(null);
  const draftRef = useRef("");

  // A rejected command is console feedback, not an application error: a real
  // console prints "unknown command" and moves on. Only transport failures
  // (permissions, companion gone) toast.
  const [feedback, setFeedback] = useState<string | null>(null);

  const send = useMutation({
    mutationFn: async (command: string) =>
      unwrap(
        await api.POST("/mc/{id}/command", {
          params: { path: { id: instanceId } },
          body: { command },
        }),
      ),
    onSuccess: (res) => {
      setValue("");
      setFeedback(res.ok ? null : res.message || t("mc.console.commandRejected"));
    },
    onError: (err) =>
      toast(
        "error",
        t("mc.console.commandFailed"),
        isApiError(err) ? err.message : t("common.unknownError"),
      ),
  });

  const submit = () => {
    const command = value.trim();
    if (!command || send.isPending) return;
    const history = historyRef.current;
    if (history[history.length - 1] !== command) history.push(command);
    indexRef.current = null;
    send.mutate(command);
  };

  const onKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter") {
      e.preventDefault();
      submit();
      return;
    }
    const history = historyRef.current;
    if (e.key === "ArrowUp") {
      if (history.length === 0) return;
      e.preventDefault();
      if (indexRef.current === null) {
        draftRef.current = value;
        indexRef.current = history.length - 1;
      } else if (indexRef.current > 0) {
        indexRef.current -= 1;
      }
      setValue(history[indexRef.current]);
    } else if (e.key === "ArrowDown") {
      if (indexRef.current === null) return;
      e.preventDefault();
      if (indexRef.current < history.length - 1) {
        indexRef.current += 1;
        setValue(history[indexRef.current]);
      } else {
        indexRef.current = null;
        setValue(draftRef.current);
      }
    } else {
      indexRef.current = null;
    }
  };

  return (
    <div style={{ background: PANE.chrome }}>
      {feedback && (
        <p
          className="border-t px-3 py-1 font-mono text-[12px]"
          style={{ borderColor: PANE.edge, color: "var(--hh-warning)" }}
        >
          ! {feedback}
        </p>
      )}
      <div
        className="flex items-center gap-1.5 border-t px-3"
        style={{ borderColor: PANE.edge }}
      >
      {send.isPending ? (
        <Loader2 aria-hidden className="size-4 shrink-0 animate-spin" style={{ color: PANE.dim }} />
      ) : (
        <ChevronRight aria-hidden className="size-4 shrink-0" style={{ color: PANE.dim }} />
      )}
      <input
        value={value}
        onChange={(e) => {
          setFeedback(null);
          setValue(e.target.value);
        }}
        onKeyDown={onKeyDown}
        readOnly={send.isPending}
        placeholder={t("mc.console.commandPlaceholder")}
        aria-label={t("mc.console.commandAria")}
        autoComplete="off"
        autoCorrect="off"
        autoCapitalize="off"
        spellCheck={false}
        className={cn(
          "h-10 w-full bg-transparent font-mono text-[13px] placeholder:text-[#5b6272] focus:outline-none",
          send.isPending && "opacity-50",
        )}
        style={{ color: PANE.accentText, caretColor: PANE.accentText }}
      />
      </div>
    </div>
  );
}
