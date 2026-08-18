import { useMemo, useRef, useState, type ReactNode } from "react";
import {
  ArrowLeftRight,
  Binary,
  CircleAlert,
  FileDigit,
  FlaskConical,
  Image as ImageIcon,
  Link2,
  ListTree,
  Lock,
  LockOpen,
  ScanSearch,
  Trash2,
  TriangleAlert,
  Type,
  Upload,
  X,
} from "lucide-react";
import { formatBytes } from "@/lib/file-lang";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Switch } from "@/ui/switch";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Base64 & URL encode / decode — two self-contained sections, everything live
 * and entirely in the browser.
 *
 * Base64 is UTF-8 safe both ways: encode runs the string through TextEncoder
 * and chunks the bytes into `btoa` (so multi-byte characters and emoji survive),
 * decode reverses it through TextDecoder. Decoding is lenient — it accepts the
 * URL-safe alphabet, missing padding and stray whitespace — and when the bytes
 * aren't valid UTF-8 it falls back to a lossy preview instead of crashing. A
 * file → data-URL panel covers binary payloads.
 *
 * The URL section wraps encodeURIComponent / encodeURI both ways with friendly
 * errors on malformed escapes, plus a `new URL()` parser that breaks any URL
 * into its parts and lists every query parameter.
 */

// ---------------------------------------------------------------------------
// Base64 helpers
// ---------------------------------------------------------------------------

const CHUNK = 0x8000; // 32 KiB — safely under the String.fromCharCode arg limit

/** UTF-8 string → Base64 (standard, or URL-safe with `-_` and stripped padding). */
function encodeBase64(text: string, urlSafe: boolean): string {
  const bytes = new TextEncoder().encode(text);
  let bin = "";
  for (let i = 0; i < bytes.length; i += CHUNK) {
    bin += String.fromCharCode(...bytes.subarray(i, i + CHUNK));
  }
  let b64 = btoa(bin);
  if (urlSafe) b64 = b64.replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
  return b64;
}

type Decoded = { text: string; lossy: boolean };

/** Base64 (standard or URL-safe, padded or not) → UTF-8 string. Throws on garbage. */
function decodeBase64(input: string): Decoded {
  let s = input.replace(/\s+/g, "").replace(/-/g, "+").replace(/_/g, "/");
  if (s.length === 0) return { text: "", lossy: false };
  if (!/^[A-Za-z0-9+/]*={0,2}$/.test(s)) {
    throw new Error("That isn't valid Base64 — it contains characters outside the Base64 alphabet.");
  }
  const rem = s.length % 4;
  if (rem === 1) throw new Error("That isn't valid Base64 — its length is off by one character.");
  if (rem) s += "=".repeat(4 - rem);
  let bin: string;
  try {
    bin = atob(s);
  } catch {
    throw new Error("That isn't valid Base64 and couldn't be decoded.");
  }
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  try {
    return { text: new TextDecoder("utf-8", { fatal: true }).decode(bytes), lossy: false };
  } catch {
    // Valid Base64, but the bytes aren't UTF-8 text (likely binary) — show a lossy view.
    return { text: new TextDecoder("utf-8").decode(bytes), lossy: true };
  }
}

type Conv = { output: string; error: string | null; warning: string | null };

function base64Convert(input: string, dir: Dir, urlSafe: boolean): Conv {
  if (!input) return { output: "", error: null, warning: null };
  try {
    if (dir === "encode") return { output: encodeBase64(input, urlSafe), error: null, warning: null };
    const { text, lossy } = decodeBase64(input);
    return {
      output: text,
      error: null,
      warning: lossy
        ? "These bytes aren't valid UTF-8 text — the data looks binary, so this is a lossy preview. Use the file panel for binary payloads."
        : null,
    };
  } catch (e) {
    return { output: "", error: e instanceof Error ? e.message : "Could not process that input.", warning: null };
  }
}

// ---------------------------------------------------------------------------
// URL helpers
// ---------------------------------------------------------------------------

function urlConvert(input: string, dir: Dir, full: boolean): Conv {
  if (!input) return { output: "", error: null, warning: null };
  try {
    const output = dir === "encode"
      ? (full ? encodeURI(input) : encodeURIComponent(input))
      : (full ? decodeURI(input) : decodeURIComponent(input));
    return { output, error: null, warning: null };
  } catch {
    return {
      output: "",
      error:
        dir === "decode"
          ? "Malformed percent-encoding — there's a stray “%” or an incomplete “%XX” sequence."
          : "That input can't be encoded.",
      warning: null,
    };
  }
}

const DEFAULT_PORTS: Record<string, string> = {
  "http:": "80",
  "https:": "443",
  "ws:": "80",
  "wss:": "443",
  "ftp:": "21",
};

type Parsed =
  | null
  | { ok: false; error: string }
  | { ok: true; url: URL; assumed: boolean; params: [string, string][] };

/** Parse with `new URL`; if it looks scheme-less, retry assuming https:// so hosts still parse. */
function parseUrl(input: string): Parsed {
  const raw = input.trim();
  if (!raw) return null;
  let url: URL | null = null;
  let assumed = false;
  // Only treat the input as already-schemed when it carries a real scheme, so a
  // bare "host:port" (nas:5000, homehub.local:8443) isn't misread as an opaque
  // scheme by WHATWG. That means either "scheme://…" (http/https/ftp/ws…) or an
  // authority-less scheme like mailto:/tel: whose colon is NOT followed by a port
  // digit. Everything else assumes https:// so the host still parses.
  if (/^[a-z][a-z0-9+.-]*:(\/\/|(?!\d))/i.test(raw)) {
    try {
      url = new URL(raw);
    } catch {
      /* invalid */
    }
  } else {
    try {
      url = new URL(`https://${raw}`);
      assumed = true;
    } catch {
      try {
        url = new URL(raw);
      } catch {
        /* fall through to error */
      }
    }
  }
  if (!url) {
    return { ok: false, error: "That isn't a valid URL. Include a scheme like https:// (or at least a host)." };
  }
  const params: [string, string][] = [];
  url.searchParams.forEach((v, k) => params.push([k, v]));
  return { ok: true, url, assumed, params };
}

// ---------------------------------------------------------------------------
// Shared bits
// ---------------------------------------------------------------------------

type Dir = "encode" | "decode";
type Section = "base64" | "url";

const PANE =
  "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg " +
  "min-h-[190px] resize-y leading-relaxed transition-colors " +
  "focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40";

const byteLen = (s: string) => (s ? new TextEncoder().encode(s).length : 0);

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
            "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium transition-colors",
            value === o.value ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

function ErrorBox({ children }: { children: ReactNode }) {
  return (
    <div className="border-danger/40 bg-danger-soft/40 mt-3 flex items-start gap-2 rounded-lg border p-2.5">
      <CircleAlert className="text-danger mt-0.5 size-4 shrink-0" />
      <span className="text-danger text-[13px]">{children}</span>
    </div>
  );
}

function WarnBox({ children }: { children: ReactNode }) {
  return (
    <div className="border-edge bg-bg-subtle/60 text-fg-muted mt-3 flex items-start gap-2 rounded-lg border p-2.5 text-[12.5px]">
      <TriangleAlert className="mt-0.5 size-4 shrink-0 text-amber-500" />
      <span>{children}</span>
    </div>
  );
}

/** A labelled read-only monospace output block with its own copy button. */
function OutputBlock({ label, value, minH = "min-h-[104px]" }: { label: string; value: string; minH?: string }) {
  return (
    <div>
      <div className="mb-1.5 flex items-center gap-2">
        <span className="text-fg text-[12.5px] font-semibold">{label}</span>
        <span className="text-fg-subtle text-[11.5px] tabular-nums">{value.length.toLocaleString()} chars</span>
        <span className="flex-1" />
        <CopyIconButton value={value} label={`Copy ${label}`} />
      </div>
      <textarea readOnly value={value} spellCheck={false} className={cn(PANE, minH, "cursor-text")} />
    </div>
  );
}

/** Small ghost button, shared by the "Load sample" / "Clear" rows. */
function MiniActions({ onSample, onClear, clearDisabled }: { onSample: () => void; onClear: () => void; clearDisabled: boolean }) {
  return (
    <div className="mt-2 flex flex-wrap items-center gap-1.5">
      <Button size="sm" variant="ghost" onClick={onSample}>
        <FlaskConical className="size-3.5" />
        Load sample
      </Button>
      <Button size="sm" variant="ghost" onClick={onClear} disabled={clearDisabled}>
        <Trash2 className="size-3.5" />
        Clear
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Base64 · text
// ---------------------------------------------------------------------------

function Base64TextPanel() {
  const [input, setInput] = useState("");
  const [dir, setDir] = useState<Dir>("encode");
  const [urlSafe, setUrlSafe] = useState(false);

  const result = useMemo(() => base64Convert(input, dir, urlSafe), [input, dir, urlSafe]);
  const inBytes = useMemo(() => byteLen(input), [input]);
  const outBytes = useMemo(() => byteLen(result.output), [result.output]);

  const swap = () => {
    if (!result.output || result.error) return;
    setInput(result.output);
    setDir((d) => (d === "encode" ? "decode" : "encode"));
  };

  const loadSample = () =>
    setInput(
      dir === "encode"
        ? "HomeHub 🏠 — encode me to Base64! Grüße, 日本語, and emoji 🎉 all survive."
        : "SGVsbG8sIHdvcmxkISDwn4yN",
    );

  const inLabel = dir === "encode" ? "Plain text" : "Base64";
  const outLabel = dir === "encode" ? "Base64" : "Plain text";

  return (
    <div className="space-y-4">
      <ToolCard>
        <div className="flex flex-wrap items-center gap-x-5 gap-y-3">
          <div className="flex items-center gap-1.5">
            <Button size="sm" variant={dir === "encode" ? "primary" : "secondary"} onClick={() => setDir("encode")}>
              <Lock className="size-3.5" />
              Encode
            </Button>
            <Button size="sm" variant={dir === "decode" ? "primary" : "secondary"} onClick={() => setDir("decode")}>
              <LockOpen className="size-3.5" />
              Decode
            </Button>
          </div>
          <label className="text-fg-muted flex cursor-pointer items-center gap-2 text-[12.5px] select-none">
            <Switch checked={urlSafe} onCheckedChange={setUrlSafe} />
            URL-safe alphabet
            <span className="text-fg-subtle hidden sm:inline">
              (<span className="font-mono">-_</span> instead of <span className="font-mono">+/</span>, no padding)
            </span>
          </label>
          <span className="flex-1" />
          <span className="text-fg-subtle text-[11.5px] tabular-nums">
            in {input.length.toLocaleString()} chars · {inBytes.toLocaleString()} bytes
          </span>
        </div>
      </ToolCard>

      <div className="grid gap-4 lg:grid-cols-2">
        {/* Input */}
        <ToolCard title={inLabel}>
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={dir === "encode" ? "Type or paste text to encode…" : "Paste Base64 to decode…"}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            data-gramm="false"
            className={cn(PANE, result.error && "border-danger/60 focus:border-danger")}
          />
          <MiniActions onSample={loadSample} onClear={() => setInput("")} clearDisabled={!input} />
          {result.error && <ErrorBox>{result.error}</ErrorBox>}
        </ToolCard>

        {/* Output */}
        <ToolCard
          title={outLabel}
          right={
            <div className="flex items-center gap-1.5">
              {urlSafe && dir === "encode" && <Badge tone="accent">URL-safe</Badge>}
              <Button
                size="sm"
                variant="ghost"
                onClick={swap}
                disabled={!result.output || !!result.error}
                title="Move the output into the input and flip the direction"
              >
                <ArrowLeftRight className="size-3.5" />
                Swap
              </Button>
              <CopyIconButton value={result.output} label={`Copy ${outLabel}`} />
            </div>
          }
        >
          <textarea
            readOnly
            value={result.output}
            placeholder={result.error ? "Fix the input to see output…" : "Result appears here…"}
            spellCheck={false}
            className={cn(PANE, "cursor-text")}
          />
          <div className="text-fg-subtle mt-2 min-h-[1.25rem] text-[11.5px] tabular-nums">
            {result.output ? `${result.output.length.toLocaleString()} chars · ${outBytes.toLocaleString()} bytes` : ""}
          </div>
          {result.warning && <WarnBox>{result.warning}</WarnBox>}
        </ToolCard>
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Base64 · file → data URL
// ---------------------------------------------------------------------------

const MAX_FILE = 15 * 1024 * 1024;

type FileMeta = { name: string; size: number; type: string };

function Base64FilePanel() {
  const [meta, setMeta] = useState<FileMeta | null>(null);
  const [dataUrl, setDataUrl] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  const read = (f: File) => {
    setError(null);
    setDataUrl("");
    setMeta({ name: f.name, size: f.size, type: f.type });
    if (f.size > MAX_FILE) {
      setError(`That file is ${formatBytes(f.size)}. Keep it under ${formatBytes(MAX_FILE)} to encode it here.`);
      return;
    }
    setBusy(true);
    const reader = new FileReader();
    reader.onload = () => {
      setDataUrl(typeof reader.result === "string" ? reader.result : "");
      setBusy(false);
    };
    reader.onerror = () => {
      setError("This file couldn't be read.");
      setBusy(false);
    };
    reader.readAsDataURL(f);
  };

  const clear = () => {
    setMeta(null);
    setDataUrl("");
    setError(null);
    setBusy(false);
    if (inputRef.current) inputRef.current.value = "";
  };

  const bare = dataUrl ? dataUrl.slice(dataUrl.indexOf(",") + 1) : "";
  const isImage = !!meta && meta.type.startsWith("image/") && !!dataUrl;

  return (
    <ToolCard title="File → Base64 data URL">
      <input
        ref={inputRef}
        type="file"
        className="hidden"
        onChange={(e) => {
          const f = e.target.files?.[0];
          if (f) read(f);
        }}
      />

      {!meta ? (
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          onDragOver={(e) => {
            e.preventDefault();
            setDragOver(true);
          }}
          onDragLeave={() => setDragOver(false)}
          onDrop={(e) => {
            e.preventDefault();
            setDragOver(false);
            const f = e.dataTransfer.files?.[0];
            if (f) read(f);
          }}
          className={cn(
            "flex w-full cursor-pointer flex-col items-center justify-center gap-2 rounded-lg border border-dashed px-4 py-10 text-center transition-colors",
            dragOver ? "border-accent bg-accent-soft/30" : "border-edge bg-bg-subtle/40 hover:border-edge-strong",
          )}
        >
          <span className="bg-accent-soft text-accent-soft-fg flex size-11 items-center justify-center rounded-xl">
            <Upload className="size-5" />
          </span>
          <span className="text-fg text-[13.5px] font-medium">Drop a file here, or click to browse</span>
          <span className="text-fg-subtle text-[12px]">Any file type · up to {formatBytes(MAX_FILE)} · never leaves your device</span>
        </button>
      ) : (
        <div className="space-y-3">
          <div className="border-edge bg-bg-subtle/40 flex items-start gap-3 rounded-lg border p-3">
            <span className="bg-accent-soft text-accent-soft-fg mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg">
              <FileDigit className="size-4" />
            </span>
            <div className="min-w-0 flex-1">
              <ResultRow label="Name" value={meta.name} mono={false} />
              <ResultRow label="Type" value={meta.type || "unknown"} mono={false} />
              <ResultRow label="Size" value={`${formatBytes(meta.size)} · ${meta.size.toLocaleString()} bytes`} mono={false} />
              {bare && <ResultRow label="Base64 length" value={`${bare.length.toLocaleString()} chars`} mono={false} />}
            </div>
            <div className="flex shrink-0 items-center gap-1">
              <Button size="sm" variant="ghost" onClick={() => inputRef.current?.click()}>
                Replace
              </Button>
              <button
                type="button"
                onClick={clear}
                aria-label="Remove file"
                title="Remove file"
                className="text-fg-subtle hover:text-danger hover:bg-bg-subtle rounded-md p-1.5 transition-colors"
              >
                <X className="size-4" />
              </button>
            </div>
          </div>

          {error && <ErrorBox>{error}</ErrorBox>}
          {busy && <p className="text-fg-muted text-[12.5px]">Reading file…</p>}

          {isImage && (
            <div className="border-edge bg-bg-subtle/40 flex flex-col items-center gap-2 rounded-lg border p-3">
              <span className="text-fg-subtle flex items-center gap-1.5 self-start text-[11.5px] font-medium">
                <ImageIcon className="size-3.5" />
                Preview
              </span>
              <img src={dataUrl} alt="Preview of the selected file" className="max-h-56 max-w-full rounded-md object-contain" />
            </div>
          )}

          {dataUrl && (
            <>
              <OutputBlock label="Data URL" value={dataUrl} minH="min-h-[120px]" />
              <OutputBlock label="Base64 only" value={bare} minH="min-h-[120px]" />
            </>
          )}
        </div>
      )}
    </ToolCard>
  );
}

// ---------------------------------------------------------------------------
// URL · encode / decode
// ---------------------------------------------------------------------------

type UrlMode = "component" | "full";

function UrlConvertPanel() {
  const [input, setInput] = useState("");
  const [dir, setDir] = useState<Dir>("encode");
  const [mode, setMode] = useState<UrlMode>("component");

  const result = useMemo(() => urlConvert(input, dir, mode === "full"), [input, dir, mode]);

  const swap = () => {
    if (!result.output || result.error) return;
    setInput(result.output);
    setDir((d) => (d === "encode" ? "decode" : "encode"));
  };

  const loadSample = () =>
    setInput(
      dir === "encode"
        ? "name=John Doe & city=São Paulo/日本?ok"
        : "name%3DJohn%20Doe%20%26%20city%3DS%C3%A3o%20Paulo",
    );

  return (
    <ToolCard title="URL encode / decode">
      <div className="mb-3 flex flex-wrap items-center gap-x-5 gap-y-3">
        <div className="flex items-center gap-1.5">
          <Button size="sm" variant={dir === "encode" ? "primary" : "secondary"} onClick={() => setDir("encode")}>
            <Lock className="size-3.5" />
            Encode
          </Button>
          <Button size="sm" variant={dir === "decode" ? "primary" : "secondary"} onClick={() => setDir("decode")}>
            <LockOpen className="size-3.5" />
            Decode
          </Button>
        </div>
        <Segmented<UrlMode>
          value={mode}
          onChange={setMode}
          options={[
            { value: "component", label: "Component" },
            { value: "full", label: "Full URL" },
          ]}
        />
        <span className="flex-1" />
        <span className="text-fg-subtle hidden max-w-[16rem] text-[11.5px] sm:block">
          {mode === "component"
            ? "encodeURIComponent — escapes every reserved character. Use for one query value or path segment."
            : "encodeURI — keeps URL structure (:/?#&=) intact. Use for a whole URL."}
        </span>
      </div>

      <div className="grid gap-4 lg:grid-cols-2">
        <div>
          <div className="mb-1.5 flex items-center gap-2">
            <span className="text-fg text-[12.5px] font-semibold">{dir === "encode" ? "Plain text" : "Encoded"}</span>
            <span className="text-fg-subtle text-[11.5px] tabular-nums">{input.length.toLocaleString()} chars</span>
          </div>
          <textarea
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder={dir === "encode" ? "Text to percent-encode…" : "Percent-encoded text to decode…"}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            data-gramm="false"
            className={cn(PANE, "min-h-[150px]", result.error && "border-danger/60 focus:border-danger")}
          />
          <MiniActions onSample={loadSample} onClear={() => setInput("")} clearDisabled={!input} />
          {result.error && <ErrorBox>{result.error}</ErrorBox>}
        </div>

        <div>
          <div className="mb-1.5 flex items-center gap-2">
            <span className="text-fg text-[12.5px] font-semibold">{dir === "encode" ? "Encoded" : "Plain text"}</span>
            <span className="text-fg-subtle text-[11.5px] tabular-nums">{result.output.length.toLocaleString()} chars</span>
            <span className="flex-1" />
            <Button
              size="sm"
              variant="ghost"
              onClick={swap}
              disabled={!result.output || !!result.error}
              title="Move the output into the input and flip the direction"
            >
              <ArrowLeftRight className="size-3.5" />
              Swap
            </Button>
            <CopyIconButton value={result.output} label="Copy output" />
          </div>
          <textarea
            readOnly
            value={result.output}
            placeholder={result.error ? "Fix the input to see output…" : "Result appears here…"}
            spellCheck={false}
            className={cn(PANE, "min-h-[150px] cursor-text")}
          />
        </div>
      </div>
    </ToolCard>
  );
}

// ---------------------------------------------------------------------------
// URL · parse
// ---------------------------------------------------------------------------

const PARSE_SAMPLE = "https://user:pass@example.com:8443/path/to/page?q=hello+world&lang=en&tags=a&tags=b#section";

function UrlParsePanel() {
  const [input, setInput] = useState("");
  const parsed = useMemo(() => parseUrl(input), [input]);

  const rows: { label: string; value: string }[] = [];
  if (parsed && parsed.ok) {
    const u = parsed.url;
    const port = u.port || (DEFAULT_PORTS[u.protocol] ? `${DEFAULT_PORTS[u.protocol]} (default)` : "—");
    rows.push({ label: "Protocol", value: u.protocol });
    if (u.username) rows.push({ label: "Username", value: u.username });
    if (u.password) rows.push({ label: "Password", value: u.password });
    rows.push({ label: "Host", value: u.host });
    rows.push({ label: "Hostname", value: u.hostname });
    rows.push({ label: "Port", value: port });
    rows.push({ label: "Path", value: u.pathname || "—" });
    rows.push({ label: "Query string", value: u.search || "—" });
    rows.push({ label: "Fragment", value: u.hash || "—" });
    rows.push({ label: "Origin", value: u.origin });
  }

  return (
    <ToolCard
      title="Parse a URL"
      right={
        parsed && parsed.ok ? (
          <Badge tone={parsed.params.length ? "accent" : "neutral"}>
            <ListTree className="size-3" />
            {parsed.params.length} param{parsed.params.length === 1 ? "" : "s"}
          </Badge>
        ) : undefined
      }
    >
      <div className="relative">
        <Input
          value={input}
          onChange={(e) => setInput(e.target.value)}
          placeholder="https://example.com:8443/path?q=1&lang=en#top"
          spellCheck={false}
          autoCapitalize="off"
          autoCorrect="off"
          className="pr-9 font-mono text-[13px]"
        />
        {input && (
          <button
            type="button"
            onClick={() => setInput("")}
            aria-label="Clear"
            className="text-fg-subtle hover:text-fg absolute inset-y-0 right-0 flex w-9 cursor-pointer items-center justify-center"
          >
            <X className="size-4" />
          </button>
        )}
      </div>
      <div className="mt-2">
        <Button size="sm" variant="ghost" onClick={() => setInput(PARSE_SAMPLE)}>
          <FlaskConical className="size-3.5" />
          Load sample
        </Button>
      </div>

      {!parsed ? (
        <p className="text-fg-subtle mt-3 flex items-center gap-2 text-[13px]">
          <ScanSearch className="size-4" />
          Paste a URL above to break it into its parts.
        </p>
      ) : !parsed.ok ? (
        <ErrorBox>{parsed.error}</ErrorBox>
      ) : (
        <div className="mt-3 space-y-3">
          {parsed.assumed && (
            <WarnBox>
              No scheme was given, so this was parsed as <span className="font-mono">https://</span> — the parts below assume that.
            </WarnBox>
          )}

          <div className="border-edge divide-edge bg-bg-subtle/40 divide-y rounded-lg border px-3">
            {rows.map((r) => (
              <ResultRow key={r.label} label={r.label} value={r.value} mono />
            ))}
          </div>

          <div>
            <div className="text-fg mb-1.5 flex items-center gap-1.5 text-[12.5px] font-semibold">
              <ListTree className="size-3.5" />
              Query parameters
            </div>
            {parsed.params.length === 0 ? (
              <p className="text-fg-subtle border-edge bg-bg-subtle/40 rounded-lg border px-3 py-2.5 text-[13px]">
                No query parameters.
              </p>
            ) : (
              <div className="border-edge overflow-x-auto rounded-lg border">
                <table className="w-full border-collapse text-[13px]">
                  <thead>
                    <tr className="bg-bg-subtle text-fg-subtle text-left text-[11.5px]">
                      <th className="px-3 py-1.5 font-medium">Key</th>
                      <th className="px-3 py-1.5 font-medium">Value (decoded)</th>
                      <th className="w-9 px-1 py-1.5" />
                    </tr>
                  </thead>
                  <tbody className="divide-edge divide-y">
                    {parsed.params.map(([k, v], i) => (
                      <tr key={`${k}-${i}`} className="align-top">
                        <td className="text-fg px-3 py-1.5 font-mono break-all">{k}</td>
                        <td className="text-fg px-3 py-1.5 font-mono break-all">
                          {v || <span className="text-fg-subtle">(empty)</span>}
                        </td>
                        <td className="px-1 py-1">
                          <CopyIconButton value={v} label={`Copy value of ${k}`} />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </div>
        </div>
      )}
    </ToolCard>
  );
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

export default function Tool() {
  const [section, setSection] = useState<Section>("base64");
  const [b64tab, setB64tab] = useState<"text" | "file">("text");

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <Segmented<Section>
          value={section}
          onChange={setSection}
          options={[
            { value: "base64", label: (<><Binary className="size-3.5" />Base64</>) },
            { value: "url", label: (<><Link2 className="size-3.5" />URL</>) },
          ]}
        />
        <span className="text-fg-subtle text-[12px]">
          {section === "base64"
            ? "UTF-8 safe, both ways — text or files. Everything runs locally."
            : "Percent-encode components and dissect any URL into its parts."}
        </span>
      </div>

      {/* Base64 section — panels stay mounted so switching tabs keeps your work. */}
      <div className={cn("space-y-4", section !== "base64" && "hidden")}>
        <Segmented
          value={b64tab}
          onChange={setB64tab}
          options={[
            { value: "text", label: (<><Type className="size-3.5" />Text</>) },
            { value: "file", label: (<><FileDigit className="size-3.5" />File</>) },
          ]}
        />
        <div className={cn(b64tab !== "text" && "hidden")}>
          <Base64TextPanel />
        </div>
        <div className={cn(b64tab !== "file" && "hidden")}>
          <Base64FilePanel />
        </div>
      </div>

      {/* URL section */}
      <div className={cn("space-y-4", section !== "url" && "hidden")}>
        <UrlConvertPanel />
        <UrlParsePanel />
      </div>
    </div>
  );
}
