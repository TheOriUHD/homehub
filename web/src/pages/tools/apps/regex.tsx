import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import {
  Braces,
  CircleAlert,
  FlaskConical,
  Hash,
  Highlighter,
  Info,
  Replace as ReplaceIcon,
  SearchCode,
  Sparkles,
  Trash2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Switch } from "@/ui/switch";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Regex tester — build a JavaScript RegExp from a pattern + flag chips and run
 * it live against a test string. `new RegExp` is wrapped so an invalid pattern
 * surfaces a friendly inline error instead of throwing. Matches are collected
 * with a zero-width guard (advance lastIndex on an empty match — by a whole code
 * point under the `u` flag, so the bump never splits a surrogate pair) and a
 * hard cap that bounds the match loop's ITERATION COUNT — but not the
 * backtracking inside a single `exec`. Because a synchronous exec is
 * uninterruptible from its own thread, matching (and the replace preview) runs
 * in a Web Worker raced against a wall-clock timeout: a catastrophic pattern is
 * terminated and surfaced as an inline error instead of freezing the tab. The
 * result drives three views that always agree: an inline highlight (alternating
 * shades, hover-linked to the list), a per-match breakdown with numbered + named
 * capture groups, and an optional find-and-replace preview using `$1` /
 * `$<name>` semantics.
 */

type GroupInfo = { num: number; name: string | null; value: string | undefined };
type MatchInfo = { ord: number; index: number; end: number; text: string; groups: GroupInfo[] };
type Result =
  | { kind: "empty" }
  | { kind: "error"; message: string }
  | { kind: "ok"; matches: MatchInfo[]; global: boolean; sticky: boolean; truncated: boolean };

type Seg = { text: string; ord: number | null; zero?: boolean };

const MAX_MATCHES = 5000; // collection cap — guards against runaway iteration
const LIST_LIMIT = 200; // rendered match rows (all matches still counted)
const WORKER_TIMEOUT_MS = 2000; // wall-clock budget before a match is deemed catastrophic
const SLOW_MESSAGE =
  "Pattern too slow — possible catastrophic backtracking. Simplify the pattern or shorten the test string.";

const FLAGS: { flag: string; name: string; desc: string }[] = [
  { flag: "g", name: "global", desc: "Find all matches, not just the first" },
  { flag: "i", name: "ignore case", desc: "Case-insensitive matching" },
  { flag: "m", name: "multiline", desc: "^ and $ match at line breaks" },
  { flag: "s", name: "dotAll", desc: ". also matches newline characters" },
  { flag: "u", name: "unicode", desc: "Treat the pattern as Unicode code points" },
  { flag: "y", name: "sticky", desc: "Match only from the regex's lastIndex" },
];
const FLAG_ORDER = FLAGS.map((f) => f.flag);

const PRESETS: { label: string; pattern: string; flags: string }[] = [
  { label: "Email", pattern: "[\\w.+-]+@[a-z0-9.-]+\\.[a-z]{2,}", flags: "gi" },
  { label: "URL", pattern: "https?:\\/\\/[^\\s/$.?#][^\\s]*", flags: "gi" },
  { label: "IPv4", pattern: "\\b(?:\\d{1,3}\\.){3}\\d{1,3}\\b", flags: "g" },
  { label: "Hex color", pattern: "#(?:[0-9a-f]{6}|[0-9a-f]{3})\\b", flags: "gi" },
  { label: "ISO date", pattern: "(?<year>\\d{4})-(?<month>\\d{2})-(?<day>\\d{2})", flags: "g" },
  { label: "Number", pattern: "-?\\d+(?:\\.\\d+)?", flags: "g" },
];

const EXAMPLE = {
  pattern: "(?<user>[\\w.+-]+)@(?<domain>[a-z0-9.-]+\\.[a-z]{2,})",
  flags: "gi",
  text:
    "Reach the team at support@homehub.io or sales@Example.COM.\n" +
    "Escalate to ops@lan.local and cc noreply@mail-server.net on outages.",
};

const FIELD_BASE = "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg leading-relaxed";
const TEXTAREA = cn(FIELD_BASE, "min-h-[220px] resize-y transition-colors focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40");
const PREVIEW = cn(FIELD_BASE, "min-h-[220px] overflow-auto whitespace-pre-wrap break-words");

/** Turn a RegExp construction SyntaxError into just its reason, slashes stripped. */
function friendlyRegexError(e: unknown): string {
  const raw = e instanceof Error ? e.message : String(e);
  const m = raw.match(/Invalid regular expression:\s*\/[\s\S]*\/[a-z]*:\s*([\s\S]+)$/i);
  return (m ? m[1] : raw).trim();
}

/**
 * Scan the pattern for capturing groups in source order, returning each group's
 * name (or null). Skips escaped parens, character classes, non-capturing groups,
 * lookaheads and lookbehinds — so index N maps to the Nth capture group.
 */
function extractGroupNames(pattern: string): (string | null)[] {
  const names: (string | null)[] = [];
  let inClass = false;
  for (let i = 0; i < pattern.length; i++) {
    const c = pattern[i];
    if (c === "\\") {
      i++; // skip the escaped character
      continue;
    }
    if (inClass) {
      if (c === "]") inClass = false;
      continue;
    }
    if (c === "[") {
      inClass = true;
      continue;
    }
    if (c !== "(") continue;
    if (pattern[i + 1] !== "?") {
      names.push(null); // plain capturing group
      continue;
    }
    // (? … — capturing only when it's a named group (?<name>, not lookbehind (?<= / (?<!
    if (pattern[i + 2] === "<" && pattern[i + 3] !== "=" && pattern[i + 3] !== "!") {
      let name = "";
      let j = i + 3;
      while (j < pattern.length && pattern[j] !== ">") name += pattern[j++];
      names.push(name);
    }
    // (?: (?= (?! (?<= (?<! → non-capturing, contribute no group number
  }
  return names;
}

function toMatchInfo(m: RegExpExecArray, ord: number, names: (string | null)[]): MatchInfo {
  const groups: GroupInfo[] = [];
  for (let g = 1; g < m.length; g++) {
    const raw = names[g - 1] ?? null;
    // Only trust a parsed name that actually exists in match.groups.
    const name = raw && m.groups && raw in m.groups ? raw : null;
    groups.push({ num: g, name, value: m[g] });
  }
  return { ord, index: m.index, end: m.index + m[0].length, text: m[0], groups };
}

/** The single source of truth — never throws; returns empty / error / ok. */
function computeMatches(pattern: string, flags: string, text: string): Result {
  if (!pattern) return { kind: "empty" };

  let regex: RegExp;
  try {
    regex = new RegExp(pattern, flags);
  } catch (e) {
    return { kind: "error", message: friendlyRegexError(e) };
  }

  const names = extractGroupNames(pattern);
  const matches: MatchInfo[] = [];
  let truncated = false;

  if (text) {
    // Both global and sticky advance lastIndex, so both need the loop.
    if (regex.global || regex.sticky) {
      let m: RegExpExecArray | null;
      let ord = 0;
      while ((m = regex.exec(text)) !== null) {
        matches.push(toMatchInfo(m, ++ord, names));
        // Zero-width match guard: without this an empty match never advances.
        // Under `u`, step a whole code point so the bump never lands inside a
        // surrogate pair (which would snap back and re-match, flooding the cap).
        if (m.index === regex.lastIndex)
          regex.lastIndex += regex.unicode && (text.codePointAt(regex.lastIndex) ?? 0) > 0xffff ? 2 : 1;
        if (matches.length >= MAX_MATCHES) {
          truncated = true;
          break;
        }
      }
    } else {
      const m = regex.exec(text);
      if (m) matches.push(toMatchInfo(m, 1, names));
    }
  }

  return { kind: "ok", matches, global: regex.global, sticky: regex.sticky, truncated };
}

/**
 * Body of the matching Web Worker — self-contained (closes over no module
 * binding) so it survives `.toString()` + bundling. Never called on the main
 * thread; its source is stringified into a Blob worker. Mirrors computeMatches
 * and the replace preview so a catastrophic pattern backtracks *here*, off the
 * render thread, where the wall-clock timeout can terminate it.
 */
function workerBody() {
  const scope = self as unknown as {
    onmessage: ((e: MessageEvent) => void) | null;
    postMessage: (message: unknown) => void;
  };
  const MAX = 5000;

  function reason(e: unknown): string {
    const raw = e instanceof Error ? e.message : String(e);
    const m = raw.match(/Invalid regular expression:\s*\/[\s\S]*\/[a-z]*:\s*([\s\S]+)$/i);
    return (m ? m[1] : raw).trim();
  }

  function groupNames(pattern: string): (string | null)[] {
    const names: (string | null)[] = [];
    let inClass = false;
    for (let i = 0; i < pattern.length; i++) {
      const c = pattern[i];
      if (c === "\\") {
        i++;
        continue;
      }
      if (inClass) {
        if (c === "]") inClass = false;
        continue;
      }
      if (c === "[") {
        inClass = true;
        continue;
      }
      if (c !== "(") continue;
      if (pattern[i + 1] !== "?") {
        names.push(null);
        continue;
      }
      if (pattern[i + 2] === "<" && pattern[i + 3] !== "=" && pattern[i + 3] !== "!") {
        let name = "";
        let j = i + 3;
        while (j < pattern.length && pattern[j] !== ">") name += pattern[j++];
        names.push(name);
      }
    }
    return names;
  }

  function toInfo(m: RegExpExecArray, ord: number, names: (string | null)[]): MatchInfo {
    const groups: GroupInfo[] = [];
    for (let g = 1; g < m.length; g++) {
      const raw = names[g - 1] ?? null;
      const name = raw && m.groups && raw in m.groups ? raw : null;
      groups.push({ num: g, name, value: m[g] });
    }
    return { ord, index: m.index, end: m.index + m[0].length, text: m[0], groups };
  }

  function compute(pattern: string, flags: string, text: string): Result {
    if (!pattern) return { kind: "empty" };
    let regex: RegExp;
    try {
      regex = new RegExp(pattern, flags);
    } catch (e) {
      return { kind: "error", message: reason(e) };
    }
    const names = groupNames(pattern);
    const matches: MatchInfo[] = [];
    let truncated = false;
    if (text) {
      if (regex.global || regex.sticky) {
        let m: RegExpExecArray | null;
        let ord = 0;
        while ((m = regex.exec(text)) !== null) {
          matches.push(toInfo(m, ++ord, names));
          // Same zero-width guard as computeMatches: step a full code point
          // under `u` so the bump never lands inside a surrogate pair.
          if (m.index === regex.lastIndex)
            regex.lastIndex += regex.unicode && (text.codePointAt(regex.lastIndex) ?? 0) > 0xffff ? 2 : 1;
          if (matches.length >= MAX) {
            truncated = true;
            break;
          }
        }
      } else {
        const m = regex.exec(text);
        if (m) matches.push(toInfo(m, 1, names));
      }
    }
    return { kind: "ok", matches, global: regex.global, sticky: regex.sticky, truncated };
  }

  scope.onmessage = (e: MessageEvent) => {
    const d = (e.data || {}) as {
      jobId: number;
      pattern: string;
      flags: string;
      text: string;
      replaceOn: boolean;
      replacement: string;
    };
    const result = compute(d.pattern, d.flags, d.text);
    let replaced: string | null = null;
    if (d.replaceOn && result.kind === "ok") {
      try {
        replaced = d.text.replace(new RegExp(d.pattern, d.flags), d.replacement);
      } catch {
        replaced = null;
      }
    }
    scope.postMessage({ jobId: d.jobId, result, replaced });
  };
}

/** Serialized worker source — wrap the self-contained body in an IIFE. */
const WORKER_SRC = `(${workerBody})()`;

/** Spin up the matching worker from an inline Blob; null if workers are unavailable. */
function createComputeWorker(): Worker | null {
  try {
    const url = URL.createObjectURL(new Blob([WORKER_SRC], { type: "text/javascript" }));
    const worker = new Worker(url);
    URL.revokeObjectURL(url);
    return worker;
  } catch {
    return null;
  }
}

/** Split the test string into plain / matched / zero-width segments for highlighting. */
function buildSegments(text: string, matches: MatchInfo[]): Seg[] {
  const segs: Seg[] = [];
  let cursor = 0;
  for (const m of matches) {
    if (m.index < cursor) continue; // defensive: matches never overlap here
    if (m.index > cursor) segs.push({ text: text.slice(cursor, m.index), ord: null });
    if (m.end === m.index) segs.push({ text: "", ord: m.ord, zero: true });
    else segs.push({ text: text.slice(m.index, m.end), ord: m.ord });
    cursor = m.end;
  }
  if (cursor < text.length) segs.push({ text: text.slice(cursor), ord: null });
  return segs;
}

/** Debounce heavy recompute so it doesn't fire on every keystroke. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return value === debounced ? value : debounced;
}

function MatchRow({ m, active, onHover }: { m: MatchInfo; active: boolean; onHover: (n: number | null) => void }) {
  return (
    <div
      onMouseEnter={() => onHover(m.ord)}
      onMouseLeave={() => onHover(null)}
      className={cn(
        "rounded-lg border p-2.5 transition-colors",
        active ? "border-accent bg-accent-soft/20" : "border-edge bg-bg-subtle/40",
      )}
    >
      <div className="flex items-center gap-2">
        <Badge tone="accent" className="tabular-nums">
          #{m.ord}
        </Badge>
        <span className="text-fg-subtle inline-flex items-center gap-1 text-[11.5px] tabular-nums">
          <Hash className="size-3" />
          {m.index}–{m.end}
          {m.end === m.index ? " · empty" : ` · ${m.end - m.index} ch`}
        </span>
        <span className="flex-1" />
        <CopyIconButton value={m.text} label="Copy match" />
      </div>

      <code className="text-fg mt-1.5 block overflow-x-auto rounded-md bg-surface px-2 py-1 font-mono text-[12.5px] whitespace-pre">
        {m.text === "" ? <span className="text-fg-subtle">(empty match)</span> : m.text}
      </code>

      {m.groups.length > 0 && (
        <div className="mt-2 space-y-1">
          {m.groups.map((g) => (
            <div key={g.num} className="flex items-center gap-2">
              <Badge tone="neutral" className="tabular-nums">{`$${g.num}`}</Badge>
              {g.name && <Badge tone="info">{g.name}</Badge>}
              <code
                className={cn(
                  "min-w-0 flex-1 truncate rounded bg-surface px-2 py-0.5 font-mono text-[12.5px]",
                  g.value === undefined ? "text-fg-subtle italic" : "text-fg",
                )}
              >
                {g.value === undefined ? "did not match" : g.value === "" ? "(empty)" : g.value}
              </code>
              {g.value !== undefined && <CopyIconButton value={g.value} label={`Copy group ${g.name ?? g.num}`} />}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

export default function Tool() {
  const [pattern, setPattern] = useState(EXAMPLE.pattern);
  const [flagSet, setFlagSet] = useState<Set<string>>(() => new Set(EXAMPLE.flags.split("")));
  const [text, setText] = useState(EXAMPLE.text);
  const [hovered, setHovered] = useState<number | null>(null);
  const [replaceOn, setReplaceOn] = useState(false);
  const [replacement, setReplacement] = useState("$<user> [at] $<domain>");

  const flagStr = useMemo(() => FLAG_ORDER.filter((f) => flagSet.has(f)).join(""), [flagSet]);
  const toggleFlag = (f: string) =>
    setFlagSet((prev) => {
      const next = new Set(prev);
      if (next.has(f)) next.delete(f);
      else next.add(f);
      return next;
    });

  const debPattern = useDebounced(pattern, 150);
  const debText = useDebounced(text, 150);

  // Matching — and the replace preview — runs in a Web Worker, never on the
  // render thread: a single RegExp.exec that catastrophically backtracks cannot
  // be interrupted from its own thread, so a same-thread cap can't save us. We
  // race the worker against a wall-clock timeout, terminate it if it overruns,
  // and surface an inline error instead of freezing the tab. Initial state is
  // computed once from the safe EXAMPLE so the first paint already shows matches.
  const [result, setResult] = useState<Result>(() =>
    computeMatches(EXAMPLE.pattern, EXAMPLE.flags, EXAMPLE.text),
  );
  const [replaced, setReplaced] = useState<string | null>(null);
  const workerRef = useRef<Worker | null>(null);
  const jobRef = useRef(0);
  const noWorkerRef = useRef(false);

  useEffect(() => {
    const jobId = ++jobRef.current;

    if (!workerRef.current && !noWorkerRef.current) {
      const created = createComputeWorker();
      if (created) workerRef.current = created;
      else noWorkerRef.current = true;
    }
    const worker = workerRef.current;

    // No worker available (blocked/unsupported): compute synchronously. This is
    // the one path that can still block, but it keeps the tool working rather
    // than dead when workers cannot be created.
    if (!worker) {
      const res = computeMatches(debPattern, flagStr, debText);
      setResult(res);
      let rep: string | null = null;
      if (replaceOn && res.kind === "ok") {
        try {
          rep = debText.replace(new RegExp(debPattern, flagStr), replacement);
        } catch {
          rep = null;
        }
      }
      setReplaced(rep);
      return;
    }

    let settled = false;
    let timer = 0;
    const onMessage = (e: MessageEvent) => {
      const data = e.data as { jobId?: number; result?: Result; replaced?: string | null } | null;
      if (settled || !data || data.jobId !== jobId) return; // ignore stale / late replies
      settled = true;
      window.clearTimeout(timer);
      worker.removeEventListener("message", onMessage);
      setResult(data.result ?? { kind: "empty" });
      setReplaced(data.replaced ?? null);
    };
    worker.addEventListener("message", onMessage);
    timer = window.setTimeout(() => {
      if (settled) return;
      settled = true;
      worker.removeEventListener("message", onMessage);
      worker.terminate();
      workerRef.current = null; // force a fresh worker for the next job
      setResult({ kind: "error", message: SLOW_MESSAGE });
      setReplaced(null);
    }, WORKER_TIMEOUT_MS);
    worker.postMessage({ jobId, pattern: debPattern, flags: flagStr, text: debText, replaceOn, replacement });

    return () => {
      window.clearTimeout(timer);
      worker.removeEventListener("message", onMessage);
    };
  }, [debPattern, flagStr, debText, replaceOn, replacement]);

  // Tear the worker down when the tool unmounts (and null the ref so a dev
  // strict-mode remount rebuilds a live worker instead of reusing a dead one).
  useEffect(
    () => () => {
      workerRef.current?.terminate();
      workerRef.current = null;
    },
    [],
  );

  const count = result.kind === "ok" ? result.matches.length : 0;
  const segments = useMemo(
    () => (result.kind === "ok" ? buildSegments(debText, result.matches) : []),
    [result, debText],
  );

  const allJson = useMemo(() => {
    if (result.kind !== "ok" || result.matches.length === 0) return "[]";
    return JSON.stringify(
      result.matches.map((m) => ({
        index: m.index,
        end: m.end,
        match: m.text,
        groups: m.groups.reduce<Record<string, string | null>>((acc, g) => {
          acc[g.name ?? `$${g.num}`] = g.value ?? null;
          return acc;
        }, {}),
      })),
      null,
      2,
    );
  }, [result]);

  const applyPreset = (p: { pattern: string; flags: string }) => {
    setPattern(p.pattern);
    setFlagSet(new Set(p.flags.split("")));
  };
  const loadExample = () => {
    setPattern(EXAMPLE.pattern);
    setFlagSet(new Set(EXAMPLE.flags.split("")));
    setText(EXAMPLE.text);
  };
  const clearAll = () => {
    setPattern("");
    setText("");
  };

  const renderPreview = (): ReactNode => {
    if (result.kind === "empty") return <span className="text-fg-subtle">Enter a pattern to start matching.</span>;
    if (result.kind === "error")
      return debText ? (
        <span className="text-fg-muted">{debText}</span>
      ) : (
        <span className="text-fg-subtle">Fix the pattern to see matches.</span>
      );
    if (!debText) return <span className="text-fg-subtle">Type or paste a test string to see matches.</span>;
    if (segments.length === 0) return <span className="text-fg-muted">{debText}</span>;
    return segments.map((s, i) => {
      if (s.ord == null) return <span key={i}>{s.text}</span>;
      const active = hovered === s.ord;
      if (s.zero)
        return (
          <span
            key={i}
            onMouseEnter={() => setHovered(s.ord)}
            onMouseLeave={() => setHovered(null)}
            title={`Match ${s.ord} · zero-width`}
            className={cn(
              "mx-[0.5px] inline-block h-[1.15em] w-[2px] translate-y-[0.2em] rounded-full bg-accent align-baseline",
              active && "ring-2 ring-accent",
            )}
          />
        );
      return (
        <mark
          key={i}
          onMouseEnter={() => setHovered(s.ord)}
          onMouseLeave={() => setHovered(null)}
          title={`Match ${s.ord}`}
          className={cn(
            "rounded-[3px] px-[1px] transition-shadow",
            s.ord % 2 === 1 ? "bg-accent-soft text-accent-soft-fg" : "bg-emerald-400/25 text-fg",
            active && "ring-2 ring-accent",
          )}
        >
          {s.text}
        </mark>
      );
    });
  };

  return (
    <div className="space-y-4">
      {/* Pattern & flags */}
      <ToolCard
        title={"Pattern & flags"}
        right={
          result.kind === "error" ? (
            <Badge tone="danger">
              <CircleAlert className="size-3" />
              Invalid
            </Badge>
          ) : result.kind === "ok" && pattern ? (
            <Badge tone="success">Valid</Badge>
          ) : null
        }
      >
        <Field
          label="Regular expression"
          error={result.kind === "error" ? result.message : undefined}
          hint="JavaScript RegExp syntax — the slashes and flags are added for you."
        >
          {(p) => (
            <Input
              {...p}
              value={pattern}
              onChange={(e) => setPattern(e.target.value)}
              placeholder={"e.g. (?<user>\\w+)@(?<domain>[\\w.]+)"}
              className="font-mono"
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              autoComplete="off"
              data-gramm="false"
            />
          )}
        </Field>

        {/* Regex literal */}
        <div className="mt-3 flex items-center gap-2">
          <div className="border-edge bg-bg-subtle text-fg flex min-w-0 flex-1 items-center gap-0.5 overflow-x-auto rounded-md border px-2 py-1 font-mono text-[12.5px]">
            <span className="text-fg-subtle">/</span>
            <span className="whitespace-pre">{pattern || <span className="text-fg-subtle">pattern</span>}</span>
            <span className="text-fg-subtle">/{flagStr}</span>
          </div>
          <CopyIconButton value={`/${pattern}/${flagStr}`} label="Copy regex literal" />
        </div>

        {/* Flag chips */}
        <div className="mt-3 flex flex-wrap gap-1.5">
          {FLAGS.map((f) => {
            const active = flagSet.has(f.flag);
            return (
              <button
                key={f.flag}
                type="button"
                aria-pressed={active}
                title={`${f.flag} — ${f.desc}`}
                onClick={() => toggleFlag(f.flag)}
                className={cn(
                  "inline-flex cursor-pointer items-center gap-1.5 rounded-lg border px-2.5 py-1.5 text-[12.5px] font-medium transition-colors",
                  active
                    ? "border-accent bg-accent-soft text-accent-soft-fg"
                    : "border-edge bg-bg-subtle text-fg-muted hover:text-fg hover:border-edge-strong",
                )}
              >
                <span className="font-mono text-[13px] font-semibold">{f.flag}</span>
                <span className="opacity-80">{f.name}</span>
              </button>
            );
          })}
        </div>

        {/* Presets + actions */}
        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-fg-subtle mr-0.5 inline-flex items-center gap-1 text-[12px] font-medium">
            <Sparkles className="size-3.5" />
            Presets
          </span>
          {PRESETS.map((p) => (
            <button
              key={p.label}
              type="button"
              onClick={() => applyPreset(p)}
              className="border-edge bg-bg-subtle text-fg-muted hover:text-fg hover:border-edge-strong cursor-pointer rounded-md border px-2 py-1 text-[12px] font-medium transition-colors"
            >
              {p.label}
            </button>
          ))}
          <span className="flex-1" />
          <Button size="sm" variant="ghost" onClick={loadExample}>
            <FlaskConical className="size-3.5" />
            Example
          </Button>
          <Button size="sm" variant="ghost" onClick={clearAll} disabled={!pattern && !text}>
            <Trash2 className="size-3.5" />
            Clear
          </Button>
        </div>
      </ToolCard>

      {/* Test string & highlight */}
      <ToolCard
        title={
          <span className="inline-flex items-center gap-1.5">
            <Highlighter className="size-3.5" />
            Test string &amp; matches
          </span>
        }
        right={
          <div className="flex items-center gap-2">
            <Badge tone={count > 0 ? "accent" : "neutral"} className="tabular-nums">
              {count} {count === 1 ? "match" : "matches"}
            </Badge>
            <CopyIconButton value={text} label="Copy test string" />
          </div>
        }
      >
        <div className="grid gap-3 lg:grid-cols-2">
          <div className="space-y-1.5">
            <div className="text-fg-subtle text-[12px] font-medium">Input</div>
            <textarea
              value={text}
              onChange={(e) => setText(e.target.value)}
              placeholder="Paste the text to test against…"
              spellCheck={false}
              autoCapitalize="off"
              autoCorrect="off"
              data-gramm="false"
              className={TEXTAREA}
            />
          </div>
          <div className="space-y-1.5">
            <div className="text-fg-subtle text-[12px] font-medium">Highlighted</div>
            <div className={PREVIEW} aria-live="polite">
              {renderPreview()}
            </div>
          </div>
        </div>

        {result.kind === "ok" && !result.global && count > 0 && (
          <div className="border-edge bg-bg-subtle/60 text-fg-muted mt-3 flex flex-wrap items-center gap-2 rounded-lg border p-2.5 text-[12.5px]">
            <Info className="text-accent size-3.5 shrink-0" />
            Showing the first match only — enable the{" "}
            <span className="text-fg font-mono font-semibold">g</span> flag to find every match.
            <span className="flex-1" />
            <Button size="sm" variant="ghost" onClick={() => toggleFlag("g")}>
              Enable g
            </Button>
          </div>
        )}
      </ToolCard>

      {/* Match details */}
      {result.kind === "ok" && (
        <ToolCard
          title={
            <span className="inline-flex items-center gap-1.5">
              <SearchCode className="size-3.5" />
              Match details
            </span>
          }
          right={
            count > 0 ? (
              <div className="flex items-center gap-1">
                <Braces className="text-fg-subtle size-3.5" />
                <span className="text-fg-subtle text-[11.5px]">Copy all</span>
                <CopyIconButton value={allJson} label="Copy all matches as JSON" />
              </div>
            ) : null
          }
        >
          {count === 0 ? (
            <div className="text-fg-muted py-6 text-center text-[13px]">
              {debText ? "No matches found." : "Enter a test string above to see matches."}
            </div>
          ) : (
            <div className="space-y-2">
              {result.truncated && (
                <div className="text-fg-subtle text-[12px]">Stopped at the {MAX_MATCHES.toLocaleString()}-match cap.</div>
              )}
              {result.matches.slice(0, LIST_LIMIT).map((m) => (
                <MatchRow key={m.ord} m={m} active={hovered === m.ord} onHover={setHovered} />
              ))}
              {result.matches.length > LIST_LIMIT && (
                <div className="text-fg-subtle pt-1 text-center text-[12px]">
                  Showing the first {LIST_LIMIT} of {result.matches.length.toLocaleString()} matches.
                </div>
              )}
            </div>
          )}
        </ToolCard>
      )}

      {/* Replace preview */}
      {result.kind === "ok" && (
        <ToolCard
          title={
            <span className="inline-flex items-center gap-1.5">
              <ReplaceIcon className="size-3.5" />
              Replace
            </span>
          }
          right={<Switch checked={replaceOn} onCheckedChange={setReplaceOn} />}
        >
          {replaceOn ? (
            <div className="space-y-3">
              <Field
                label="Replacement"
                hint="Use $1 / $<name> for groups, $& for the whole match, $$ for a literal dollar."
              >
                {(p) => (
                  <Input
                    {...p}
                    value={replacement}
                    onChange={(e) => setReplacement(e.target.value)}
                    placeholder="$<user> [at] $<domain>"
                    className="font-mono"
                    spellCheck={false}
                    autoCapitalize="off"
                    autoCorrect="off"
                    data-gramm="false"
                  />
                )}
              </Field>
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <span className="text-fg-subtle text-[12px] font-medium">Result</span>
                  <CopyIconButton value={replaced ?? ""} label="Copy replaced text" />
                </div>
                <div className={PREVIEW}>
                  {replaced === null ? (
                    <span className="text-fg-subtle">—</span>
                  ) : replaced === "" ? (
                    <span className="text-fg-subtle">(empty result)</span>
                  ) : (
                    replaced
                  )}
                </div>
              </div>
              {!result.global && (
                <div className="text-fg-subtle inline-flex items-center gap-1 text-[12px]">
                  <Info className="size-3" />
                  Without the g flag only the first match is replaced.
                </div>
              )}
            </div>
          ) : (
            <div className="text-fg-muted text-[13px]">
              Turn on to preview a find-and-replace across the test string using this pattern.
            </div>
          )}
        </ToolCard>
      )}
    </div>
  );
}
