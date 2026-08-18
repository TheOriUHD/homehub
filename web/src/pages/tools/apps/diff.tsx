import { useEffect, useId, useMemo, useState, type ReactNode } from "react";
import { diffArrays, diffWords, type ArrayChange } from "diff";
import {
  AlignLeft,
  ArrowLeftRight,
  CircleAlert,
  CircleCheck,
  Columns2,
  FileDiff,
  FlaskConical,
  Trash2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Switch } from "@/ui/switch";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Diff tool — compare two blocks of text or config, side by side or unified.
 *
 * Lines are aligned with the `diff` library's `diffArrays` over our own line
 * split, driven by a custom comparator: this is what lets us honour both
 * "ignore whitespace" and "ignore case" (neither of which `diffLines` supports
 * together) while still displaying each side's text verbatim — we only ever use
 * the diff for *alignment*, then index back into the original line arrays. A
 * removed run immediately followed by an added run is paired up into "modified"
 * rows, and `diffWords` highlights the exact tokens that changed within them.
 *
 * The heavy line diff is memoised + debounced, capped to a sane row budget, and
 * runs under a timeout so pathological inputs degrade to a friendly notice
 * instead of freezing the tab.
 */

type Side = { n: number; text: string };
type RowType = "equal" | "add" | "remove" | "modify";
type Row = { type: RowType; left: Side | null; right: Side | null };

type Uni =
  | { kind: "context"; oldN: number; newN: number; text: string }
  | { kind: "remove"; oldN: number; text: string; other: string | null }
  | { kind: "add"; newN: number; text: string; other: string | null };

type DiffResult = {
  rows: Row[];
  unified: Uni[];
  added: number;
  removed: number;
  unchanged: number;
  identical: boolean;
  empty: boolean;
  aborted: boolean;
  truncated: boolean;
};

type WordPart = { value: string; added: boolean; removed: boolean };

const MAX_ROWS = 4000;

const SAMPLE_A = `server {
  listen 80;
  server_name example.com;
  root /var/www/html;

  location / {
    try_files $uri $uri/ =404;
  }

  gzip off;
}`;

const SAMPLE_B = `server {
  listen 443 ssl;
  server_name example.com www.example.com;
  root /var/www/html;

  location / {
    try_files $uri $uri/ /index.html;
  }

  ssl_certificate     /etc/ssl/cert.pem;
  ssl_certificate_key /etc/ssl/key.pem;
  gzip on;
}`;

/** Split into lines, dropping a single trailing newline's phantom empty line. */
function splitLines(text: string): string[] {
  if (text.length === 0) return [];
  const lines = text.split(/\r?\n/);
  if (lines.length > 0 && lines[lines.length - 1] === "") lines.pop();
  return lines;
}

/** Never throws: pathological inputs return `aborted`, empty inputs `empty`. */
function computeDiff(
  original: string,
  changed: string,
  opts: { ignoreWhitespace: boolean; ignoreCase: boolean },
): DiffResult {
  const base: DiffResult = {
    rows: [],
    unified: [],
    added: 0,
    removed: 0,
    unchanged: 0,
    identical: false,
    empty: false,
    aborted: false,
    truncated: false,
  };

  if (original === "" && changed === "") return { ...base, empty: true };

  const a = splitLines(original);
  const b = splitLines(changed);

  const norm = (s: string) => {
    let t = s;
    if (opts.ignoreCase) t = t.toLowerCase();
    if (opts.ignoreWhitespace) t = t.replace(/\s+/g, " ").trim();
    return t;
  };

  let parts: ArrayChange<string>[] | undefined;
  try {
    parts = diffArrays(a, b, { comparator: (x, y) => norm(x) === norm(y), timeout: 300 });
  } catch {
    parts = undefined;
  }
  if (!parts) return { ...base, aborted: true };

  const rows: Row[] = [];
  let ai = 0;
  let bi = 0;
  let added = 0;
  let removed = 0;
  let unchanged = 0;

  for (let k = 0; k < parts.length; k++) {
    const part = parts[k];
    const count = part.count;

    if (!part.added && !part.removed) {
      for (let i = 0; i < count; i++) {
        rows.push({ type: "equal", left: { n: ai + 1, text: a[ai] }, right: { n: bi + 1, text: b[bi] } });
        ai++;
        bi++;
        unchanged++;
      }
    } else if (part.removed) {
      const next = parts[k + 1];
      if (next && next.added) {
        // Paired replacement: line up removed-vs-added into "modified" rows.
        const remCount = count;
        const addCount = next.count;
        const m = Math.max(remCount, addCount);
        for (let i = 0; i < m; i++) {
          const left = i < remCount ? { n: ai + 1, text: a[ai] } : null;
          const right = i < addCount ? { n: bi + 1, text: b[bi] } : null;
          if (left) ai++;
          if (right) bi++;
          if (left && right) {
            rows.push({ type: "modify", left, right });
            added++;
            removed++;
          } else if (left) {
            rows.push({ type: "remove", left, right: null });
            removed++;
          } else if (right) {
            rows.push({ type: "add", left: null, right });
            added++;
          }
        }
        k++; // the following "added" part was consumed here
      } else {
        for (let i = 0; i < count; i++) {
          rows.push({ type: "remove", left: { n: ai + 1, text: a[ai] }, right: null });
          ai++;
          removed++;
        }
      }
    } else {
      for (let i = 0; i < count; i++) {
        rows.push({ type: "add", left: null, right: { n: bi + 1, text: b[bi] } });
        bi++;
        added++;
      }
    }
  }

  const identical = added === 0 && removed === 0;

  let capped = rows;
  let truncated = false;
  if (rows.length > MAX_ROWS) {
    capped = rows.slice(0, MAX_ROWS);
    truncated = true;
  }

  const unified: Uni[] = [];
  for (const r of capped) {
    if (r.type === "equal" && r.left && r.right) {
      unified.push({ kind: "context", oldN: r.left.n, newN: r.right.n, text: r.left.text });
    } else if (r.type === "modify" && r.left && r.right) {
      unified.push({ kind: "remove", oldN: r.left.n, text: r.left.text, other: r.right.text });
      unified.push({ kind: "add", newN: r.right.n, text: r.right.text, other: r.left.text });
    } else if (r.type === "remove" && r.left) {
      unified.push({ kind: "remove", oldN: r.left.n, text: r.left.text, other: null });
    } else if (r.type === "add" && r.right) {
      unified.push({ kind: "add", newN: r.right.n, text: r.right.text, other: null });
    }
  }

  return { rows: capped, unified, added, removed, unchanged, identical, empty: false, aborted: false, truncated };
}

/** Word-level highlight: keep tokens for `side`, tint the ones that changed. */
function wordSpans(parts: WordPart[], side: "left" | "right"): ReactNode[] {
  const nodes: ReactNode[] = [];
  parts.forEach((p, i) => {
    if (side === "left" && p.added) return;
    if (side === "right" && p.removed) return;
    const changed = side === "left" ? p.removed : p.added;
    nodes.push(
      changed ? (
        <span
          key={i}
          className={cn("rounded-[3px]", side === "left" ? "bg-red-500/25" : "bg-emerald-500/25")}
        >
          {p.value}
        </span>
      ) : (
        <span key={i}>{p.value}</span>
      ),
    );
  });
  return nodes;
}

/** Empty lines still need to occupy a row. */
function plain(text: string): ReactNode {
  return text === "" ? " " : text;
}

function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return value === debounced ? value : debounced;
}

function Half({
  num,
  tint,
  content,
  border = true,
}: {
  num?: number;
  tint: "add" | "remove" | "equal" | "empty";
  content: ReactNode;
  border?: boolean;
}) {
  const bg =
    tint === "add"
      ? "bg-emerald-500/10"
      : tint === "remove"
        ? "bg-red-500/10"
        : tint === "empty"
          ? "bg-bg-subtle/40"
          : "";
  const gutter =
    tint === "add"
      ? "text-emerald-600 dark:text-emerald-400"
      : tint === "remove"
        ? "text-red-600 dark:text-red-400"
        : "text-fg-subtle";
  return (
    <div className={cn("flex w-1/2 min-w-0", border && "border-edge border-r", bg)}>
      <span className={cn("w-10 shrink-0 select-none py-0.5 pr-2 text-right text-[11px] tabular-nums", gutter)}>
        {num ?? ""}
      </span>
      <span className="text-fg min-w-0 flex-1 py-0.5 pr-3 pl-1 break-words whitespace-pre-wrap">{content}</span>
    </div>
  );
}

function SplitRow({ row, words, ignoreCase }: { row: Row; words: boolean; ignoreCase: boolean }) {
  const parts =
    words && row.type === "modify" && row.left && row.right
      ? diffWords(row.left.text, row.right.text, { ignoreCase })
      : null;
  const lTint = row.type === "remove" || row.type === "modify";
  const rTint = row.type === "add" || row.type === "modify";
  return (
    <div className="flex min-w-0">
      <Half
        num={row.left?.n}
        tint={row.left ? (lTint ? "remove" : "equal") : "empty"}
        content={row.left ? (parts ? wordSpans(parts, "left") : plain(row.left.text)) : null}
      />
      <Half
        num={row.right?.n}
        tint={row.right ? (rTint ? "add" : "equal") : "empty"}
        content={row.right ? (parts ? wordSpans(parts, "right") : plain(row.right.text)) : null}
        border={false}
      />
    </div>
  );
}

function UnifiedRow({ line, words, ignoreCase }: { line: Uni; words: boolean; ignoreCase: boolean }) {
  let content: ReactNode;
  if (line.kind === "context") {
    content = plain(line.text);
  } else if (line.kind === "remove") {
    const parts = words && line.other != null ? diffWords(line.text, line.other, { ignoreCase }) : null;
    content = parts ? wordSpans(parts, "left") : plain(line.text);
  } else {
    const parts = words && line.other != null ? diffWords(line.other, line.text, { ignoreCase }) : null;
    content = parts ? wordSpans(parts, "right") : plain(line.text);
  }

  const bg = line.kind === "add" ? "bg-emerald-500/10" : line.kind === "remove" ? "bg-red-500/10" : "";
  const sign = line.kind === "add" ? "+" : line.kind === "remove" ? "-" : " ";
  const signColor =
    line.kind === "add"
      ? "text-emerald-600 dark:text-emerald-400"
      : line.kind === "remove"
        ? "text-red-600 dark:text-red-400"
        : "text-fg-subtle";
  const oldN = line.kind === "add" ? "" : line.oldN;
  const newN = line.kind === "remove" ? "" : line.newN;

  return (
    <div className={cn("flex min-w-0", bg)}>
      <span className="text-fg-subtle w-10 shrink-0 select-none py-0.5 pr-2 text-right text-[11px] tabular-nums">
        {oldN}
      </span>
      <span className="text-fg-subtle w-10 shrink-0 select-none py-0.5 pr-2 text-right text-[11px] tabular-nums">
        {newN}
      </span>
      <span className={cn("w-4 shrink-0 select-none py-0.5 text-center text-[12px]", signColor)}>{sign}</span>
      <span className="text-fg min-w-0 flex-1 py-0.5 pr-3 break-words whitespace-pre-wrap">{content}</span>
    </div>
  );
}

function SwitchField({
  label,
  checked,
  onChange,
}: {
  label: string;
  checked: boolean;
  onChange: (v: boolean) => void;
}) {
  const id = useId();
  return (
    <div className="flex items-center gap-2">
      <Switch id={id} checked={checked} onCheckedChange={onChange} />
      <label htmlFor={id} className="text-fg-muted cursor-pointer text-[13px] select-none">
        {label}
      </label>
    </div>
  );
}

function Segmented<T extends string>({
  value,
  onChange,
  options,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode }[];
}) {
  return (
    <div className="bg-bg-subtle border-edge inline-flex items-center gap-0.5 rounded-lg border p-0.5">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={value === o.value}
          onClick={() => onChange(o.value)}
          className={cn(
            "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1 text-[13px] font-medium transition-colors",
            value === o.value ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

const TEXTAREA =
  "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg " +
  "min-h-[240px] resize-y leading-relaxed transition-colors " +
  "focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40";

function InputPane({
  title,
  value,
  onChange,
  placeholder,
  footer,
}: {
  title: string;
  value: string;
  onChange: (v: string) => void;
  placeholder: string;
  footer: ReactNode;
}) {
  return (
    <ToolCard
      title={title}
      right={
        <div className="flex items-center gap-1">
          <span className="text-fg-subtle text-[11.5px] tabular-nums">{value.length.toLocaleString()} chars</span>
          <CopyIconButton value={value} label={`Copy ${title.toLowerCase()}`} />
        </div>
      }
    >
      <textarea
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        spellCheck={false}
        autoCapitalize="off"
        autoCorrect="off"
        data-gramm="false"
        className={TEXTAREA}
      />
      <div className="mt-2 flex flex-wrap items-center gap-1.5">{footer}</div>
    </ToolCard>
  );
}

export default function Tool() {
  const toast = useToast();
  const [original, setOriginal] = useState("");
  const [changed, setChanged] = useState("");
  const [view, setView] = useState<"split" | "unified">("split");
  const [words, setWords] = useState(true);
  const [ignoreWs, setIgnoreWs] = useState(false);
  const [ignoreCase, setIgnoreCase] = useState(false);

  const debOriginal = useDebounced(original, 180);
  const debChanged = useDebounced(changed, 180);

  const result = useMemo(
    () => computeDiff(debOriginal, debChanged, { ignoreWhitespace: ignoreWs, ignoreCase }),
    [debOriginal, debChanged, ignoreWs, ignoreCase],
  );

  const unifiedText = useMemo(
    () =>
      result.unified
        .map((u) => (u.kind === "add" ? "+" : u.kind === "remove" ? "-" : " ") + u.text)
        .join("\n"),
    [result.unified],
  );

  const loadSample = () => {
    setOriginal(SAMPLE_A);
    setChanged(SAMPLE_B);
    toast("success", "Loaded a sample comparison");
  };
  const swap = () => {
    setOriginal(changed);
    setChanged(original);
    toast("success", "Swapped the two sides");
  };

  const showDiff = !result.empty && !result.aborted && !result.identical;
  const ignoreNote = [ignoreWs && "whitespace", ignoreCase && "case"].filter(Boolean).join(" and ");

  return (
    <div className="space-y-4">
      {/* Inputs */}
      <div className="grid gap-4 lg:grid-cols-2">
        <InputPane
          title="Original"
          value={original}
          onChange={setOriginal}
          placeholder="Paste the original text or config…"
          footer={
            <>
              <Button size="sm" variant="ghost" onClick={loadSample}>
                <FlaskConical className="size-3.5" />
                Load sample
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setOriginal("")} disabled={!original}>
                <Trash2 className="size-3.5" />
                Clear
              </Button>
            </>
          }
        />
        <InputPane
          title="Changed"
          value={changed}
          onChange={setChanged}
          placeholder="Paste the changed version…"
          footer={
            <>
              <Button size="sm" variant="ghost" onClick={swap} disabled={!original && !changed}>
                <ArrowLeftRight className="size-3.5" />
                Swap sides
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setChanged("")} disabled={!changed}>
                <Trash2 className="size-3.5" />
                Clear
              </Button>
            </>
          }
        />
      </div>

      {/* Options + summary */}
      <ToolCard>
        <div className="flex flex-wrap items-center gap-x-4 gap-y-3">
          <div className="flex items-center gap-2">
            <span className="text-fg-subtle text-[12px] font-medium">View</span>
            <Segmented
              value={view}
              onChange={setView}
              options={[
                {
                  value: "split",
                  label: (
                    <>
                      <Columns2 className="size-3.5" />
                      Side by side
                    </>
                  ),
                },
                {
                  value: "unified",
                  label: (
                    <>
                      <AlignLeft className="size-3.5" />
                      Unified
                    </>
                  ),
                },
              ]}
            />
          </div>
          <div className="bg-edge hidden h-5 w-px sm:block" />
          <SwitchField label="Highlight words" checked={words} onChange={setWords} />
          <SwitchField label="Ignore whitespace" checked={ignoreWs} onChange={setIgnoreWs} />
          <SwitchField label="Ignore case" checked={ignoreCase} onChange={setIgnoreCase} />

          <span className="flex-1" />

          <div className="flex items-center gap-1.5">
            {result.empty ? (
              <span className="text-fg-subtle text-[12px]">Awaiting input</span>
            ) : result.aborted ? (
              <Badge tone="warning">
                <CircleAlert className="size-3" />
                Too large
              </Badge>
            ) : result.identical ? (
              <Badge tone="accent">
                <CircleCheck className="size-3" />
                Identical
              </Badge>
            ) : (
              <>
                <Badge tone="success" className="tabular-nums">
                  +{result.added.toLocaleString()}
                </Badge>
                <Badge tone="danger" className="tabular-nums">
                  −{result.removed.toLocaleString()}
                </Badge>
                <span className="text-fg-subtle hidden text-[12px] tabular-nums sm:inline">
                  {result.unchanged.toLocaleString()} unchanged
                </span>
              </>
            )}
          </div>
        </div>
      </ToolCard>

      {/* Result */}
      <ToolCard
        title="Differences"
        right={
          showDiff ? (
            <div className="flex items-center gap-2">
              <span className="text-fg-subtle text-[11.5px] tabular-nums">
                {(view === "split" ? result.rows.length : result.unified.length).toLocaleString()} rows
              </span>
              <CopyIconButton value={unifiedText} label="Copy diff" />
            </div>
          ) : undefined
        }
      >
        {result.empty ? (
          <div className="flex flex-col items-center justify-center gap-2 py-14 text-center">
            <FileDiff className="text-fg-subtle size-7" />
            <p className="text-fg-muted text-sm">Paste text into both panes to see a comparison.</p>
          </div>
        ) : result.aborted ? (
          <div className="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
            <CircleAlert className="mt-0.5 size-4 shrink-0 text-amber-500" />
            <div className="text-[13px]">
              <p className="text-fg font-medium">These inputs are too large or too different to diff quickly.</p>
              <p className="text-fg-muted mt-0.5">
                Try comparing smaller sections, or turn on “Ignore whitespace” to cut the noise.
              </p>
            </div>
          </div>
        ) : result.identical ? (
          <div className="flex items-center gap-2 rounded-lg border border-emerald-500/40 bg-emerald-500/10 p-3">
            <CircleCheck className="size-4 shrink-0 text-emerald-500" />
            <p className="text-fg text-[13px] font-medium">
              The two inputs are identical{ignoreNote ? ` (ignoring ${ignoreNote})` : ""}.
            </p>
          </div>
        ) : (
          <>
            <div className="border-edge overflow-hidden rounded-lg border">
              {view === "split" && (
                <div className="text-fg-subtle bg-bg-subtle border-edge flex border-b text-[11px] font-semibold tracking-wide uppercase">
                  <div className="border-edge w-1/2 border-r py-1.5 pl-11">Original</div>
                  <div className="w-1/2 py-1.5 pl-11">Changed</div>
                </div>
              )}
              <div className="max-h-[560px] overflow-auto font-mono text-[12.5px] leading-[1.7]">
                {view === "split"
                  ? result.rows.map((row, i) => (
                      <SplitRow key={i} row={row} words={words} ignoreCase={ignoreCase} />
                    ))
                  : result.unified.map((line, i) => (
                      <UnifiedRow key={i} line={line} words={words} ignoreCase={ignoreCase} />
                    ))}
              </div>
            </div>
            {result.truncated && (
              <p className="text-fg-subtle mt-2 text-[12px]">
                Showing the first {MAX_ROWS.toLocaleString()} rows — narrow the inputs to see the rest.
              </p>
            )}
          </>
        )}
      </ToolCard>
    </div>
  );
}
