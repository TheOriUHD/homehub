import { useEffect, useMemo, useState, type ReactNode } from "react";
import YAML from "yaml";
import {
  ArrowLeftRight,
  Braces,
  CircleAlert,
  CircleCheck,
  Download,
  FileCode2,
  FlaskConical,
  Minimize2,
  Trash2,
  Wand2,
} from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * JSON ↔ YAML — format, validate and convert both ways with precise errors.
 *
 * Everything is derived live from the input via a single pure `computeResult`
 * (debounced) so the output pane, validity badge and error frame always agree.
 * Parsing is forgiving in Auto mode (tries the likely format, then the other),
 * while explicit JSON/YAML modes report that parser's error exactly — with
 * line:col dug out of the browser's JSON.parse message or YAML's `linePos`.
 */

type Fmt = "json" | "yaml";
type Source = "auto" | Fmt;
type Mode = "format" | "json" | "yaml" | "minify";

type ParseErr = { message: string; line?: number; col?: number };

type Result = {
  detected: Fmt | null;
  output: string;
  outputFormat: Fmt | null;
  error: ParseErr | null;
  note: string | null;
  valid: boolean;
};

const SAMPLE = `{
  "name": "homehub",
  "version": "1.4.0",
  "enabled": true,
  "features": ["dns", "docker", "backups"],
  "server": { "host": "0.0.0.0", "port": 8080, "tls": true },
  "limits": { "maxConnections": 100, "timeoutSeconds": 30 },
  "owner": null
}`;

/** Byte offset → 1-based line/col, counting '\n' (handles \r\n gracefully). */
function offsetToLineCol(text: string, offset: number): { line: number; col: number } {
  let line = 1;
  let col = 1;
  const n = Math.min(Math.max(offset, 0), text.length);
  for (let i = 0; i < n; i++) {
    if (text.charCodeAt(i) === 10) {
      line++;
      col = 1;
    } else {
      col++;
    }
  }
  return { line, col };
}

/** Cheap intent guess for Auto mode: object/array literal ⇒ JSON, else YAML. */
function guessFormat(text: string): Fmt {
  const c = text.trimStart()[0];
  return c === "{" || c === "[" ? "json" : "yaml";
}

/** Parse in the requested format; Auto tries the guess, then falls back. */
function parseInput(text: string, source: Source): { value: unknown; detected: Fmt } {
  if (source === "json") return { value: JSON.parse(text), detected: "json" };
  if (source === "yaml") return { value: YAML.parse(text, { merge: true, intAsBigInt: true }), detected: "yaml" };
  const guess = guessFormat(text);
  const other: Fmt = guess === "json" ? "yaml" : "json";
  try {
    return { value: guess === "json" ? JSON.parse(text) : YAML.parse(text, { merge: true, intAsBigInt: true }), detected: guess };
  } catch (primary) {
    try {
      return { value: other === "json" ? JSON.parse(text) : YAML.parse(text, { merge: true, intAsBigInt: true }), detected: other };
    } catch {
      throw primary; // surface the error for the format it most looked like
    }
  }
}

/** Turn a JSON.parse SyntaxError or a YAMLParseError into a friendly message + line:col. */
function normalizeError(err: unknown, text: string): ParseErr {
  const e = err as { name?: string; message?: string; linePos?: Array<{ line: number; col: number }> };

  // YAML: it hands us line/col directly and a code-framed message.
  if (e && (e.name === "YAMLParseError" || Array.isArray(e.linePos))) {
    const pos = Array.isArray(e.linePos) ? e.linePos[0] : undefined;
    let msg = String(e.message ?? "Invalid YAML").split("\n")[0];
    msg = msg.replace(/\s*at line \d+,?\s*column \d+:?\s*$/i, "").trim();
    return { message: msg || "Invalid YAML", line: pos?.line, col: pos?.col };
  }

  // JSON: derive position from the message ("… at position N" or "line N column M").
  const raw = String(e?.message ?? "Invalid JSON");
  let line: number | undefined;
  let col: number | undefined;
  const lc = raw.match(/line (\d+) column (\d+)/i);
  if (lc) {
    line = Number(lc[1]);
    col = Number(lc[2]);
  } else {
    const pos = raw.match(/position (\d+)/i);
    if (pos) {
      ({ line, col } = offsetToLineCol(text, Number(pos[1])));
    } else if (/unexpected end/i.test(raw)) {
      ({ line, col } = offsetToLineCol(text, text.length));
    }
  }
  const msg = raw
    .replace(/,\s*(?:\.\.\.)?".*$/is, "")
    .replace(/\s*(?:in|after) JSON at position \d+.*$/is, "")
    .replace(/\s*in JSON$/i, "")
    .replace(/^JSON\.parse:\s*/i, "")
    .trim();
  return { message: msg || "Invalid JSON", line, col };
}

/** The single source of truth: parse + stringify per the chosen mode. Never throws. */
function computeResult(text: string, source: Source, indent: number, mode: Mode): Result {
  const empty: Result = { detected: null, output: "", outputFormat: null, error: null, note: null, valid: false };
  if (!text.trim()) return empty;

  let parsed: { value: unknown; detected: Fmt };
  try {
    parsed = parseInput(text, source);
  } catch (err) {
    return {
      ...empty,
      detected: source === "auto" ? guessFormat(text) : source,
      error: normalizeError(err, text),
    };
  }

  const { value, detected } = parsed;
  if (value === undefined) {
    return {
      detected,
      output: "",
      outputFormat: null,
      error: null,
      note: "The document is empty (comments or blank lines only).",
      valid: true,
    };
  }

  try {
    let lostNonFinite = false;
    const jsonReplacer = (_k: string, x: unknown) => {
      if (typeof x === "bigint") return `__B__${x}__`;
      if (typeof x === "number" && !Number.isFinite(x)) {
        lostNonFinite = true;
        return null;
      }
      return x;
    };
    const toJson = (v: unknown, sp?: number) =>
      JSON.stringify(v, jsonReplacer, sp).replace(/"__B__(-?\d+)__"/g, "$1");

    let output: string;
    let outputFormat: Fmt;
    const target: Mode =
      mode === "format" ? (detected === "json" ? "json" : "yaml") : mode;
    if (target === "minify") {
      output = toJson(value);
      outputFormat = "json";
    } else if (target === "json") {
      output = toJson(value, indent);
      outputFormat = "json";
    } else {
      output = YAML.stringify(value, { indent, lineWidth: 0 });
      outputFormat = "yaml";
    }
    const note = lostNonFinite
      ? "Infinity/NaN can't be represented in JSON and were converted to null"
      : null;
    return { detected, output: output ?? "", outputFormat, error: null, note, valid: true };
  } catch {
    // Parsing succeeded but the value can't be serialized to JSON
    // (self-referential YAML anchors → circular structure). Report it as
    // valid-with-note rather than a JSON-worded parse error on valid YAML.
    return {
      detected,
      output: "",
      outputFormat: null,
      error: null,
      note: "Valid, but can't convert to JSON: the input has circular references (self-referential YAML anchors).",
      valid: true,
    };
  }
}

/** Debounce a value so heavy re-computes don't fire on every keystroke. */
function useDebounced<T>(value: T, ms: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = window.setTimeout(() => setDebounced(value), ms);
    return () => window.clearTimeout(id);
  }, [value, ms]);
  return value === debounced ? value : debounced;
}

function Segmented<T extends string>({
  value,
  onChange,
  options,
  label,
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode; title?: string }[];
  label: string;
}) {
  return (
    <div className="flex items-center gap-2">
      <span className="text-fg-subtle text-[12px] font-medium">{label}</span>
      <div className="bg-bg-subtle border-edge inline-flex items-center gap-0.5 rounded-lg border p-0.5">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            title={o.title}
            aria-pressed={value === o.value}
            onClick={() => onChange(o.value)}
            className={cn(
              "cursor-pointer rounded-md px-2.5 py-1 text-[13px] font-medium transition-colors",
              value === o.value ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
            )}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  );
}

const TEXTAREA =
  "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg " +
  "min-h-[360px] resize-y leading-relaxed transition-colors " +
  "focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40";

const ACTIONS: { mode: Mode; label: string; icon: typeof Wand2; title: string }[] = [
  { mode: "format", label: "Format", icon: Wand2, title: "Pretty-print, keeping the source format" },
  { mode: "json", label: "To JSON", icon: Braces, title: "Convert to pretty JSON" },
  { mode: "yaml", label: "To YAML", icon: FileCode2, title: "Convert to YAML" },
  { mode: "minify", label: "Minify", icon: Minimize2, title: "Minify to single-line JSON" },
];

export default function Tool() {
  const toast = useToast();
  const [input, setInput] = useState("");
  const [source, setSource] = useState<Source>("auto");
  const [indent, setIndent] = useState(2);
  const [mode, setMode] = useState<Mode>("format");

  const debInput = useDebounced(input, 160);
  const result = useMemo(
    () => computeResult(debInput, source, indent, mode),
    [debInput, source, indent, mode],
  );

  const hasText = debInput.trim().length > 0;
  const frameLine =
    result.error?.line != null ? debInput.split(/\r?\n/)[result.error.line - 1] ?? "" : null;
  const caret = result.error?.col != null ? " ".repeat(Math.max(0, result.error.col - 1)) + "^" : null;
  const outLines = result.output ? result.output.split("\n").length : 0;

  const detectedLabel = (source === "auto" ? result.detected : source)?.toUpperCase();

  const download = () => {
    if (!result.output || !result.outputFormat) return;
    const ext = result.outputFormat === "json" ? "json" : "yaml";
    const type = result.outputFormat === "json" ? "application/json" : "text/yaml";
    const blob = new Blob([result.output], { type: `${type};charset=utf-8` });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `converted.${ext}`;
    a.click();
    URL.revokeObjectURL(url);
  };

  const useOutputAsInput = () => {
    if (!result.output) return;
    setInput(result.output);
    if (result.outputFormat) setSource(result.outputFormat);
    setMode("format");
    toast("success", "Moved output into the input pane");
  };

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <ToolCard>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
          <Segmented<Source>
            label="Source"
            value={source}
            onChange={setSource}
            options={[
              { value: "auto", label: "Auto", title: "Detect JSON or YAML automatically" },
              { value: "json", label: "JSON" },
              { value: "yaml", label: "YAML" },
            ]}
          />
          <Segmented<string>
            label="Indent"
            value={String(indent)}
            onChange={(v) => setIndent(Number(v))}
            options={[
              { value: "2", label: "2" },
              { value: "4", label: "4" },
            ]}
          />

          <span className="flex-1" />

          <div className="flex flex-wrap items-center gap-1.5">
            {ACTIONS.map((a) => (
              <Button
                key={a.mode}
                size="sm"
                variant={mode === a.mode ? "primary" : "secondary"}
                title={a.title}
                onClick={() => setMode(a.mode)}
              >
                <a.icon className="size-3.5" />
                {a.label}
              </Button>
            ))}
          </div>
        </div>
      </ToolCard>

      {/* Panes */}
      <div className="grid gap-4 lg:grid-cols-2">
        {/* Input */}
        <ToolCard
          title="Input"
          right={
            <div className="flex items-center gap-2">
              {hasText &&
                (result.error ? (
                  <Badge tone="danger">
                    <CircleAlert className="size-3" />
                    Invalid{detectedLabel ? ` ${detectedLabel}` : ""}
                  </Badge>
                ) : (
                  <Badge tone="success">
                    <CircleCheck className="size-3" />
                    Valid{result.detected ? ` ${result.detected.toUpperCase()}` : ""}
                  </Badge>
                ))}
              <span className="text-fg-subtle text-[11.5px] tabular-nums">
                {input.length.toLocaleString()} chars
              </span>
            </div>
          }
        >
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="Paste JSON or YAML here…"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            data-gramm="false"
            className={cn(TEXTAREA, result.error && "border-danger/60 focus:border-danger")}
          />

          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <Button size="sm" variant="ghost" onClick={() => setInput(SAMPLE)}>
              <FlaskConical className="size-3.5" />
              Load sample
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setInput("")} disabled={!input}>
              <Trash2 className="size-3.5" />
              Clear
            </Button>
          </div>

          {result.error && (
            <div className="border-danger/40 bg-danger-soft/40 mt-3 rounded-lg border p-2.5">
              <div className="flex flex-wrap items-center gap-2">
                <CircleAlert className="text-danger size-4 shrink-0" />
                <span className="text-danger text-[13px] font-medium">{result.error.message}</span>
                {result.error.line != null && (
                  <Badge tone="danger" className="tabular-nums">
                    Ln {result.error.line}
                    {result.error.col != null ? ` · Col ${result.error.col}` : ""}
                  </Badge>
                )}
              </div>
              {frameLine != null && caret && (
                <div className="bg-bg-subtle mt-2 overflow-x-auto rounded-md p-2 font-mono text-[12px] leading-[1.5]">
                  <div className="text-fg whitespace-pre">{frameLine || " "}</div>
                  <div className="text-danger whitespace-pre">{caret}</div>
                </div>
              )}
            </div>
          )}
        </ToolCard>

        {/* Output */}
        <ToolCard
          title="Output"
          right={
            <div className="flex items-center gap-1.5">
              {result.outputFormat && (
                <Badge tone="accent">{result.outputFormat.toUpperCase()}</Badge>
              )}
              <Button
                size="sm"
                variant="ghost"
                onClick={useOutputAsInput}
                disabled={!result.output}
                title="Send the output back into the input pane"
              >
                <ArrowLeftRight className="size-3.5" />
                Use as input
              </Button>
              <Button size="sm" variant="ghost" onClick={download} disabled={!result.output} title="Download output">
                <Download className="size-3.5" />
              </Button>
              <CopyIconButton value={result.output} label="Copy output" />
            </div>
          }
        >
          <textarea
            readOnly
            value={result.output}
            placeholder={
              hasText ? "Fix the errors on the left to see output…" : "Converted output appears here…"
            }
            spellCheck={false}
            className={cn(TEXTAREA, "cursor-text")}
          />

          <div className="text-fg-subtle mt-2 flex min-h-[1.5rem] flex-wrap items-center gap-x-3 gap-y-1 text-[11.5px]">
            {result.note ? (
              <span className="text-fg-muted">{result.note}</span>
            ) : result.output ? (
              <>
                <span className="tabular-nums">{outLines.toLocaleString()} lines</span>
                <span className="tabular-nums">{result.output.length.toLocaleString()} characters</span>
                {mode === "minify" && input && (
                  <span className="tabular-nums">
                    {Math.max(0, 100 - Math.round((result.output.length / Math.max(1, input.length)) * 100))}% smaller
                  </span>
                )}
              </>
            ) : (
              <span className="opacity-0">.</span>
            )}
          </div>
        </ToolCard>
      </div>
    </div>
  );
}
