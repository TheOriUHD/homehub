import { useEffect, useRef, useState, type KeyboardEvent as ReactKeyboardEvent } from "react";
import { Terminal, type ITheme } from "@xterm/xterm";
import { FitAddon } from "@xterm/addon-fit";
import { SearchAddon } from "@xterm/addon-search";
import { AlertTriangle, ChevronDown, ChevronUp, Loader2, RotateCw, Search, SquareTerminal, X } from "lucide-react";
import { useTranslation } from "react-i18next";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import "@xterm/xterm/css/xterm.css";

// Keep in sync with --font-mono in app.css.
const MONO_STACK = 'ui-monospace, "SF Mono", "Cascadia Code", Menlo, Consolas, monospace';

const encoder = new TextEncoder();

// xterm's theme parser only understands hex/rgb, while the design tokens are
// oklch — round-tripping through a canvas fillStyle normalizes them to hex.
let colorCtx: CanvasRenderingContext2D | null = null;
function resolveColor(value: string, fallback: string): string {
  colorCtx ??= document.createElement("canvas").getContext("2d");
  if (!colorCtx || !value) return fallback;
  colorCtx.fillStyle = fallback;
  colorCtx.fillStyle = value;
  return colorCtx.fillStyle;
}

function readTerminalTheme(): ITheme {
  const styles = getComputedStyle(document.documentElement);
  const token = (name: string, fallback: string) => resolveColor(styles.getPropertyValue(name).trim(), fallback);
  return {
    background: token("--hh-bg", "#16171f"),
    foreground: token("--hh-fg", "#e6e7ec"),
    cursor: token("--hh-accent", "#8388e5"),
    cursorAccent: token("--hh-bg", "#16171f"),
    selectionBackground: token("--hh-accent-soft", "#3a3d63"),
    selectionForeground: token("--hh-accent-soft-fg", "#c9cdf4"),
  };
}

type PaneStatus =
  | { kind: "connecting" }
  | { kind: "open" }
  // message === null means the socket dropped without the server saying why.
  | { kind: "ended"; message: string | null; error: boolean };

type ServerFrame = { type?: string; message?: string };

/**
 * TerminalPane renders an interactive xterm.js session over a WebSocket that
 * carries raw terminal bytes as binary frames and control messages (resize,
 * exit, error) as JSON text frames. It never reconnects on its own — a dropped
 * session shows an overlay with a manual reconnect button.
 */
export function TerminalPane({
  wsUrl,
  onExit,
  className,
}: {
  wsUrl: string;
  onExit?: (message: string) => void;
  className?: string;
}) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const searchRef = useRef<SearchAddon | null>(null);
  const wsRef = useRef<WebSocket | null>(null);
  const searchInputRef = useRef<HTMLInputElement>(null);
  const onExitRef = useRef(onExit);
  onExitRef.current = onExit;

  const [status, setStatus] = useState<PaneStatus>({ kind: "connecting" });
  const [attempt, setAttempt] = useState(0);
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");

  // The terminal lives for the component's lifetime; sockets come and go
  // across manual reconnects and keep writing into the same scrollback.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const term = new Terminal({
      fontFamily: MONO_STACK,
      fontSize: 13,
      cursorBlink: true,
      scrollback: 5000,
      theme: readTerminalTheme(),
    });
    const fit = new FitAddon();
    const search = new SearchAddon();
    term.loadAddon(fit);
    term.loadAddon(search);
    term.open(el);
    fit.fit();
    term.focus();
    termRef.current = term;
    searchRef.current = search;

    const dataSub = term.onData((chunk) => {
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(chunk));
    });
    const resizeSub = term.onResize(({ cols, rows }) => {
      const ws = wsRef.current;
      if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols, rows }));
    });
    // Ctrl/Cmd+F opens the pane's own search instead of reaching the shell.
    term.attachCustomKeyEventHandler((ev) => {
      if (ev.type === "keydown" && (ev.ctrlKey || ev.metaKey) && !ev.altKey && !ev.shiftKey && ev.key.toLowerCase() === "f") {
        setSearchOpen(true);
        return false;
      }
      return true;
    });

    const ro = new ResizeObserver(() => fit.fit());
    ro.observe(el);
    const mo = new MutationObserver(() => {
      term.options.theme = readTerminalTheme();
    });
    mo.observe(document.documentElement, { attributes: true, attributeFilter: ["data-theme"] });

    return () => {
      ro.disconnect();
      mo.disconnect();
      dataSub.dispose();
      resizeSub.dispose();
      termRef.current = null;
      searchRef.current = null;
      term.dispose();
    };
  }, []);

  useEffect(() => {
    setStatus({ kind: "connecting" });
    const ws = new WebSocket(wsUrl);
    ws.binaryType = "arraybuffer";
    wsRef.current = ws;
    // Once a text frame decides the session's fate, the close event that
    // follows must not overwrite the server's message.
    let ended = false;

    ws.onopen = () => {
      setStatus({ kind: "open" });
      const term = termRef.current;
      // The server missed any fits that happened while connecting.
      if (term) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      term?.focus();
    };
    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        termRef.current?.write(new Uint8Array(ev.data));
        return;
      }
      if (typeof ev.data !== "string") return;
      let frame: ServerFrame;
      try {
        frame = JSON.parse(ev.data);
      } catch {
        return;
      }
      if (frame.type === "exit" || frame.type === "error") {
        ended = true;
        setStatus({ kind: "ended", message: frame.message ?? null, error: frame.type === "error" });
        onExitRef.current?.(frame.message ?? "");
        ws.close(1000);
      }
    };
    ws.onclose = () => {
      if (!ended) setStatus({ kind: "ended", message: null, error: true });
    };

    return () => {
      ended = true;
      ws.onopen = null;
      ws.onmessage = null;
      ws.onclose = null;
      wsRef.current = null;
      ws.close(1000);
    };
  }, [wsUrl, attempt]);

  useEffect(() => {
    if (searchOpen) searchInputRef.current?.select();
  }, [searchOpen]);

  const closeSearch = () => {
    setSearchOpen(false);
    searchRef.current?.clearDecorations();
    termRef.current?.focus();
  };

  const onQueryChange = (value: string) => {
    setQuery(value);
    if (value) searchRef.current?.findNext(value, { incremental: true });
  };

  const onSearchKeyDown = (e: ReactKeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" && query) {
      e.preventDefault();
      if (e.shiftKey) searchRef.current?.findPrevious(query);
      else searchRef.current?.findNext(query);
    } else if (e.key === "Escape") {
      e.preventDefault();
      closeSearch();
    }
  };

  // Catches Ctrl/Cmd+F when focus is inside the pane but outside the
  // terminal itself (e.g. in the search input), keeping browser find out.
  const onPaneKeyDown = (e: ReactKeyboardEvent<HTMLDivElement>) => {
    if ((e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "f") {
      e.preventDefault();
      setSearchOpen(true);
      searchInputRef.current?.select();
    }
  };

  return (
    <div className={cn("bg-bg relative h-full min-h-0 w-full overflow-hidden", className)} onKeyDown={onPaneKeyDown}>
      <div ref={containerRef} className="absolute inset-0 p-2" />

      {searchOpen && (
        <div className="bg-overlay border-edge absolute top-2 right-2 z-20 flex items-center gap-0.5 rounded-lg border p-1 shadow-2 animate-[hh-pop-in_150ms_var(--ease-out-quick)]">
          <Search aria-hidden className="text-fg-subtle ml-1.5 size-3.5 shrink-0" />
          <input
            ref={searchInputRef}
            value={query}
            onChange={(e) => onQueryChange(e.target.value)}
            onKeyDown={onSearchKeyDown}
            placeholder={t("term.searchPlaceholder")}
            aria-label={t("term.searchPlaceholder")}
            className="text-fg placeholder:text-fg-subtle h-6.5 w-44 bg-transparent px-1.5 text-[13px] focus:outline-none"
          />
          <button
            type="button"
            aria-label={t("term.searchPrev")}
            onClick={() => query && searchRef.current?.findPrevious(query)}
            className="interactive-muted flex size-6.5 shrink-0 cursor-pointer items-center justify-center"
          >
            <ChevronUp className="size-3.5" />
          </button>
          <button
            type="button"
            aria-label={t("term.searchNext")}
            onClick={() => query && searchRef.current?.findNext(query)}
            className="interactive-muted flex size-6.5 shrink-0 cursor-pointer items-center justify-center"
          >
            <ChevronDown className="size-3.5" />
          </button>
          <button
            type="button"
            aria-label={t("term.searchClose")}
            onClick={closeSearch}
            className="interactive-muted flex size-6.5 shrink-0 cursor-pointer items-center justify-center"
          >
            <X className="size-3.5" />
          </button>
        </div>
      )}

      {status.kind === "connecting" && (
        <div
          role="status"
          className="bg-bg/60 absolute inset-0 z-10 flex items-center justify-center animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
        >
          <Loader2 aria-hidden className="text-fg-subtle size-5 animate-spin" />
          <span className="sr-only">{t("term.connecting")}</span>
        </div>
      )}

      {status.kind === "ended" && (
        <div
          role="alert"
          className="bg-bg/80 absolute inset-0 z-10 flex flex-col items-center justify-center px-6 text-center backdrop-blur-[2px] animate-[hh-fade-in_150ms_var(--ease-out-quick)]"
        >
          <div
            className={cn(
              "mb-3 flex size-11 items-center justify-center rounded-xl",
              status.error ? "bg-danger-soft text-danger-soft-fg" : "bg-bg-subtle text-fg-subtle",
            )}
          >
            {status.error ? <AlertTriangle className="size-5" /> : <SquareTerminal className="size-5" />}
          </div>
          <p className="text-fg max-w-md text-sm font-medium">{status.message ?? t("term.disconnected")}</p>
          <Button variant="secondary" size="sm" className="mt-4" onClick={() => setAttempt((a) => a + 1)}>
            <RotateCw aria-hidden className="size-3.5" />
            {t("term.reconnect")}
          </Button>
        </div>
      )}
    </div>
  );
}
