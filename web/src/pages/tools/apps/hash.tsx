import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import SparkMD5 from "spark-md5";
import jsSha1 from "js-sha1";
import { sha256 as jsSha256 } from "js-sha256";
import * as jsSha512 from "js-sha512";
import {
  CircleCheck,
  FileText,
  FlaskConical,
  Loader2,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
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
import { Switch } from "@/ui/switch";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Hash & checksum generator — MD5, SHA-1, SHA-256, SHA-384 and SHA-512 for
 * text (live) or any file, plus a case-insensitive verify field.
 *
 * MD5 comes from `spark-md5` (works in every context, and streamed in 4 MB
 * chunks for big files so the tab never freezes). The SHA family comes from
 * WebCrypto when available (fast), with a pure-JS fallback (js-sha1/256/512) so
 * the whole SHA family also works on a plain-http LAN origin, where
 * `crypto.subtle` is undefined.
 */

type Algo = "MD5" | "SHA-1" | "SHA-256" | "SHA-384" | "SHA-512";
type Hashes = Record<Algo, string>;

/** WebCrypto SubtleCrypto if the context allows it (HTTPS or localhost), else null. */
const SUBTLE: SubtleCrypto | null =
  typeof crypto !== "undefined" && typeof crypto.subtle?.digest === "function" ? crypto.subtle : null;

const ALGOS: { algo: Algo; bits: number; subtle: AlgorithmIdentifier | null; note: string }[] = [
  { algo: "MD5", bits: 128, subtle: null, note: "Legacy — fine for integrity checks, not for security" },
  { algo: "SHA-1", bits: 160, subtle: "SHA-1", note: "Legacy — broken against collisions, still used for ETags" },
  { algo: "SHA-256", bits: 256, subtle: "SHA-256", note: "The usual default for download & release checksums" },
  { algo: "SHA-384", bits: 384, subtle: "SHA-384", note: "SHA-2 family, 384-bit digest" },
  { algo: "SHA-512", bits: 512, subtle: "SHA-512", note: "SHA-2 family, 512-bit digest" },
];

/** hex length → the algorithm that produces it, for auto-detecting a pasted checksum. */
const BY_HEX_LEN: Record<number, Algo> = { 32: "MD5", 40: "SHA-1", 64: "SHA-256", 96: "SHA-384", 128: "SHA-512" };

const SAMPLE = "The quick brown fox jumps over the lazy dog";

const MAX_FILE = 512 * 1024 * 1024; // read-into-memory ceiling; above this we bow out politely
const CHUNK = 4 * 1024 * 1024;

const TEXTAREA =
  "border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg " +
  "min-h-[150px] resize-y leading-relaxed transition-colors " +
  "focus:border-accent focus:outline-none focus:ring-2 focus:ring-ring/40";

/** ArrayBuffer → lowercase hex. */
function toHex(buf: ArrayBuffer): string {
  const bytes = new Uint8Array(buf);
  let out = "";
  for (let i = 0; i < bytes.length; i++) out += bytes[i].toString(16).padStart(2, "0");
  return out;
}

async function subtleHex(algo: AlgorithmIdentifier, data: BufferSource): Promise<string> {
  const digest = await SUBTLE!.digest(algo, data);
  return toHex(digest);
}

// Pure-JS SHA fallback so every algorithm works on a plain-http LAN origin
// (where crypto.subtle is undefined). WebCrypto is still preferred when
// present — it's faster on big files — but these guarantee coverage.
const JS_SHA: Record<Exclude<Algo, "MD5">, (d: string | ArrayBuffer) => string> = {
  "SHA-1": jsSha1 as unknown as (d: string | ArrayBuffer) => string,
  "SHA-256": jsSha256,
  "SHA-384": jsSha512.sha384,
  "SHA-512": jsSha512.sha512,
};

async function shaFamily(str: string | null, buf: ArrayBuffer | null): Promise<Omit<Hashes, "MD5">> {
  const data: BufferSource | string = buf ?? str ?? "";
  if (SUBTLE && buf === null) {
    const bytes = new TextEncoder().encode(str ?? "");
    const [s1, s256, s384, s512] = await Promise.all([
      subtleHex("SHA-1", bytes),
      subtleHex("SHA-256", bytes),
      subtleHex("SHA-384", bytes),
      subtleHex("SHA-512", bytes),
    ]);
    return { "SHA-1": s1, "SHA-256": s256, "SHA-384": s384, "SHA-512": s512 };
  }
  if (SUBTLE && buf !== null) {
    const [s1, s256, s384, s512] = await Promise.all([
      subtleHex("SHA-1", buf),
      subtleHex("SHA-256", buf),
      subtleHex("SHA-384", buf),
      subtleHex("SHA-512", buf),
    ]);
    return { "SHA-1": s1, "SHA-256": s256, "SHA-384": s384, "SHA-512": s512 };
  }
  const src = buf ?? (str ?? "");
  void data;
  return {
    "SHA-1": JS_SHA["SHA-1"](src),
    "SHA-256": JS_SHA["SHA-256"](src),
    "SHA-384": JS_SHA["SHA-384"](src),
    "SHA-512": JS_SHA["SHA-512"](src),
  };
}

/** Hash a UTF-8 string. MD5 + the full SHA family, everywhere. */
async function hashText(text: string): Promise<Hashes> {
  return { MD5: SparkMD5.hash(text), ...(await shaFamily(text, null)) };
}

/**
 * Hash a file's bytes. MD5 is streamed in chunks (yielding to the event loop so
 * the progress bar paints). In a secure context the SHA family is one fast
 * WebCrypto call over the whole buffer; on a plain-http origin (no crypto.subtle)
 * it is computed by incremental pure-JS hashers folded into the same chunked
 * loop, so it yields to the event loop between chunks instead of freezing the tab.
 */
async function hashBuffer(buf: ArrayBuffer, onProgress: (p: number) => void): Promise<Hashes> {
  const spark = new SparkMD5.ArrayBuffer();
  // No WebCrypto → hash the SHA family incrementally alongside MD5 so it yields
  // between chunks; in a secure context leave it to the fast WebCrypto path below.
  const sha =
    SUBTLE === null
      ? {
          "SHA-1": jsSha1.sha1.create(),
          "SHA-256": jsSha256.create(),
          "SHA-384": jsSha512.sha384.create(),
          "SHA-512": jsSha512.sha512.create(),
        }
      : null;
  const total = buf.byteLength;
  let offset = 0;
  while (offset < total) {
    const end = Math.min(offset + CHUNK, total);
    const chunk = buf.slice(offset, end);
    spark.append(chunk);
    if (sha) {
      sha["SHA-1"].update(chunk);
      sha["SHA-256"].update(chunk);
      sha["SHA-384"].update(chunk);
      sha["SHA-512"].update(chunk);
    }
    offset = end;
    onProgress(total ? (offset / total) * 0.92 : 1);
    await new Promise((r) => setTimeout(r)); // let the UI breathe between chunks
  }
  const shaHashes = sha
    ? {
        "SHA-1": sha["SHA-1"].hex(),
        "SHA-256": sha["SHA-256"].hex(),
        "SHA-384": sha["SHA-384"].hex(),
        "SHA-512": sha["SHA-512"].hex(),
      }
    : await shaFamily(null, buf);
  const out: Hashes = { MD5: spark.end(), ...shaHashes };
  onProgress(1);
  return out;
}

type FileMeta = { name: string; size: number; type: string };

type Tab = "text" | "file";

type Compare =
  | { state: "match"; algo: Algo; guessed: Algo | null }
  | { state: "mismatch"; guessed: Algo | null; token: string }
  | { state: "unavailable"; guessed: Algo }
  | { state: "invalid" }
  | { state: "nohashes" };

export default function Tool() {
  const [tab, setTab] = useState<Tab>("text");

  // Text source (live, debounced).
  const [text, setText] = useState("");
  const [textHashes, setTextHashes] = useState<Hashes | null>(null);

  // File source.
  const [file, setFile] = useState<FileMeta | null>(null);
  const [fileHashes, setFileHashes] = useState<Hashes | null>(null);
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState(0);
  const [fileError, setFileError] = useState<string | null>(null);
  const [dragOver, setDragOver] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const jobRef = useRef(0);

  // Verify field + display option.
  const [expected, setExpected] = useState("");
  const [uppercase, setUppercase] = useState(false);

  const byteLen = useMemo(() => new TextEncoder().encode(text).length, [text]);

  // Live text hashing — debounced, with a cancel guard so a slow older compute
  // can never overwrite a newer result. The previous digest stays on screen
  // while typing (no flicker); an empty field clears it.
  useEffect(() => {
    if (!text) {
      setTextHashes(null);
      return;
    }
    let cancelled = false;
    const id = window.setTimeout(() => {
      void hashText(text).then((h) => {
        if (!cancelled) setTextHashes(h);
      });
    }, 150);
    return () => {
      cancelled = true;
      window.clearTimeout(id);
    };
  }, [text]);

  const processFile = useCallback(async (f: File) => {
    const job = ++jobRef.current;
    setTab("file");
    setFileError(null);
    setFile({ name: f.name, size: f.size, type: f.type });
    setFileHashes(null);
    setProgress(0);
    if (f.size > MAX_FILE) {
      setFileError(`That file is ${formatBytes(f.size)}. Files over ${formatBytes(MAX_FILE)} can't be hashed in the browser.`);
      return;
    }
    setBusy(true);
    try {
      const buf = await f.arrayBuffer();
      if (jobRef.current !== job) return;
      const h = await hashBuffer(buf, (p) => {
        if (jobRef.current === job) setProgress(p);
      });
      if (jobRef.current !== job) return;
      setFileHashes(h);
    } catch (e) {
      if (jobRef.current === job) setFileError(e instanceof Error ? e.message : "This file could not be read.");
    } finally {
      if (jobRef.current === job) setBusy(false);
    }
  }, []);

  const clearFile = () => {
    jobRef.current++; // abandon any in-flight job
    setFile(null);
    setFileHashes(null);
    setFileError(null);
    setBusy(false);
    setProgress(0);
    if (inputRef.current) inputRef.current.value = "";
  };

  const activeHashes = tab === "text" ? textHashes : fileHashes;
  const hasInput = tab === "text" ? text.length > 0 : !!file;
  const sourceLabel = tab === "text" ? "text" : "file";
  const show = (h: string) => (uppercase ? h.toUpperCase() : h);

  // ---- Verify: compare the pasted value against the active digests ----------
  const compare = useMemo<Compare | null>(() => {
    const raw = expected.trim();
    if (!raw) return null;
    // Accept a full checksum line ("<hash>  filename") and common separators.
    const token = raw.split(/\s{2,}|\s+\*/)[0].replace(/^0x/i, "").replace(/[:\s-]/g, "").toLowerCase();
    if (!token || !/^[0-9a-f]+$/.test(token)) return { state: "invalid" };
    const guessed = BY_HEX_LEN[token.length] ?? null;
    if (!activeHashes) return { state: "nohashes" };
    const hashes = activeHashes;
    const hit = ALGOS.find((a) => hashes[a.algo] && hashes[a.algo].toLowerCase() === token);
    if (hit) return { state: "match", algo: hit.algo, guessed };
    // No match: if the length points at an algo we couldn't compute (insecure
    // context → no SHA), say so rather than crying "mismatch".
    if (guessed && !hashes[guessed]) return { state: "unavailable", guessed };
    return { state: "mismatch", guessed, token };
  }, [expected, activeHashes]);

  const matchedAlgo = compare && compare.state === "match" ? compare.algo : null;

  let copyAll = "";
  if (activeHashes) {
    const h = activeHashes;
    copyAll = ALGOS.filter((a) => h[a.algo]).map((a) => `${a.algo}: ${show(h[a.algo])}`).join("\n");
  }

  return (
    <div className="space-y-4">
      {/* Source selector */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="bg-bg-subtle border-edge inline-flex items-center gap-0.5 rounded-lg border p-0.5">
          {(["text", "file"] as const).map((t) => (
            <button
              key={t}
              type="button"
              aria-pressed={tab === t}
              onClick={() => setTab(t)}
              className={cn(
                "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium transition-colors",
                tab === t ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
              )}
            >
              {t === "text" ? <Type className="size-3.5" /> : <FileText className="size-3.5" />}
              {t === "text" ? "Text" : "File"}
            </button>
          ))}
        </div>
        <span className="text-fg-subtle text-[12.5px]">
          {tab === "text" ? "Hashes update as you type." : "Everything runs locally — files never leave your device."}
        </span>
      </div>

      {/* Input */}
      {tab === "text" ? (
        <ToolCard
          title="Text input"
          right={
            <span className="text-fg-subtle text-[11.5px] tabular-nums">
              {text.length.toLocaleString()} chars · {byteLen.toLocaleString()} bytes
            </span>
          }
        >
          <textarea
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Type or paste text to hash…"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            data-gramm="false"
            className={TEXTAREA}
          />
          <div className="mt-2 flex flex-wrap items-center gap-1.5">
            <Button size="sm" variant="ghost" onClick={() => setText(SAMPLE)}>
              <FlaskConical className="size-3.5" />
              Load sample
            </Button>
            <Button size="sm" variant="ghost" onClick={() => setText("")} disabled={!text}>
              <Trash2 className="size-3.5" />
              Clear
            </Button>
          </div>
        </ToolCard>
      ) : (
        <ToolCard title="File input">
          <input
            ref={inputRef}
            type="file"
            className="hidden"
            onChange={(e) => {
              const f = e.target.files?.[0];
              if (f) void processFile(f);
            }}
          />
          {!file ? (
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
                if (f) void processFile(f);
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
              <span className="text-fg-subtle text-[12px]">Any file type · up to {formatBytes(MAX_FILE)}</span>
            </button>
          ) : (
            <div className="space-y-3">
              <div className="border-edge bg-bg-subtle/40 flex items-start gap-3 rounded-lg border p-3">
                <span className="bg-accent-soft text-accent-soft-fg mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg">
                  <FileText className="size-4" />
                </span>
                <div className="min-w-0 flex-1">
                  <ResultRow label="Name" value={file.name} />
                  <ResultRow label="Size" value={`${formatBytes(file.size)} · ${file.size.toLocaleString()} bytes`} mono={false} />
                  <ResultRow label="Type" value={file.type || "unknown"} mono={false} />
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button size="sm" variant="ghost" onClick={() => inputRef.current?.click()}>
                    Replace
                  </Button>
                  <button
                    type="button"
                    onClick={clearFile}
                    aria-label="Remove file"
                    title="Remove file"
                    className="text-fg-subtle hover:text-danger hover:bg-bg-subtle rounded-md p-1.5 transition-colors"
                  >
                    <X className="size-4" />
                  </button>
                </div>
              </div>

              {busy && (
                <div className="space-y-2">
                  <div className="text-fg-muted flex items-center gap-2 text-[12.5px]">
                    <Loader2 className="size-3.5 animate-spin" />
                    Hashing… {Math.round(progress * 100)}%
                  </div>
                  <div className="bg-bg-subtle h-2 w-full overflow-hidden rounded-full">
                    <div
                      className="bg-accent h-full rounded-full transition-[width] duration-150"
                      style={{ width: `${Math.max(3, Math.round(progress * 100))}%` }}
                    />
                  </div>
                </div>
              )}

              {fileError && (
                <div className="border-danger/40 bg-danger-soft/40 flex items-start gap-2 rounded-lg border p-2.5">
                  <TriangleAlert className="text-danger mt-0.5 size-4 shrink-0" />
                  <span className="text-danger text-[13px]">{fileError}</span>
                </div>
              )}
            </div>
          )}
        </ToolCard>
      )}

      {/* Results */}
      <ToolCard
        title="Checksums"
        right={
          activeHashes ? (
            <div className="flex items-center gap-3">
              <label className="text-fg-muted flex cursor-pointer items-center gap-1.5 text-[12px] select-none">
                <Switch checked={uppercase} onCheckedChange={setUppercase} />
                Uppercase
              </label>
              <div className="flex items-center gap-1">
                <span className="text-fg-subtle text-[11.5px]">Copy all</span>
                <CopyIconButton value={copyAll} label="Copy all checksums" />
              </div>
            </div>
          ) : undefined
        }
      >
        {!hasInput ? (
          <p className="text-fg-subtle py-6 text-center text-[13px]">
            {tab === "text" ? "Enter some text above to see its checksums." : "Choose a file above to compute its checksums."}
          </p>
        ) : tab === "file" && busy ? (
          <div className="space-y-2">
            {ALGOS.map((a) => (
              <div key={a.algo} className="border-edge bg-bg-subtle/30 h-[62px] animate-pulse rounded-lg border" />
            ))}
          </div>
        ) : !activeHashes ? (
          <p className="text-fg-subtle py-6 text-center text-[13px]">
            {fileError ? "Fix the error above to compute checksums." : "Computing…"}
          </p>
        ) : (
          <div className="space-y-2">
            {ALGOS.map(({ algo, bits, subtle, note }) => {
              const value = activeHashes[algo];
              const unavailable = !!subtle && !SUBTLE; // a SHA row on an insecure origin
              const matched = matchedAlgo === algo;
              return (
                <div
                  key={algo}
                  className={cn(
                    "rounded-lg border p-3 transition-colors",
                    matched ? "border-emerald-500/60 bg-emerald-500/[0.06]" : "border-edge bg-bg-subtle/40",
                  )}
                >
                  <div className="mb-1.5 flex flex-wrap items-center gap-2">
                    <span className="text-fg text-[12.5px] font-semibold tracking-tight">{algo}</span>
                    <Badge tone="neutral" className="tabular-nums">
                      {bits}-bit
                    </Badge>
                    {matched && (
                      <Badge tone="success">
                        <CircleCheck className="size-3" />
                        Match
                      </Badge>
                    )}
                    <span className="text-fg-subtle hidden truncate text-[11.5px] sm:inline">{note}</span>
                    <span className="flex-1" />
                    {value && <CopyIconButton value={show(value)} label={`Copy ${algo}`} />}
                  </div>
                  {value ? (
                    <div className="text-fg font-mono text-[12.5px] leading-relaxed break-all select-all">{show(value)}</div>
                  ) : unavailable ? (
                    <div className="text-fg-subtle flex items-center gap-1.5 text-[12px]">
                      <ShieldAlert className="size-3.5 text-amber-500" />
                      Requires a secure context (HTTPS or localhost)
                    </div>
                  ) : (
                    <div className="text-fg-subtle text-[12px]">—</div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </ToolCard>

      {/* Verify */}
      <ToolCard title="Verify a checksum">
        <p className="text-fg-subtle mb-2 text-[12.5px]">
          Paste an expected checksum (any algorithm) to compare against the {sourceLabel} above — case-insensitive, and a
          full <span className="font-mono">hash&nbsp;&nbsp;filename</span> line works too.
        </p>
        <div className="relative">
          <input
            value={expected}
            onChange={(e) => setExpected(e.target.value)}
            placeholder="e.g. 9e107d9d372bb6826bd81d3542a419d6"
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            className={cn(
              "border-edge bg-bg-subtle text-fg h-9 w-full rounded-lg border pr-9 pl-3 font-mono text-[13px] transition-colors",
              "focus:border-accent focus:ring-ring/40 focus:ring-2 focus:outline-none",
              compare?.state === "match" && "border-emerald-500/60",
              compare?.state === "mismatch" && "border-danger/60",
            )}
          />
          {expected && (
            <button
              type="button"
              onClick={() => setExpected("")}
              aria-label="Clear"
              className="text-fg-subtle hover:text-fg absolute inset-y-0 right-0 flex w-9 cursor-pointer items-center justify-center"
            >
              <X className="size-4" />
            </button>
          )}
        </div>

        {compare && (
          <div className="mt-3">
            {compare.state === "match" ? (
              <div className="flex items-start gap-2.5 rounded-lg border border-emerald-500/40 bg-emerald-500/[0.08] p-3">
                <ShieldCheck className="mt-0.5 size-5 shrink-0 text-emerald-500" />
                <div className="min-w-0">
                  <p className="text-[13px] font-semibold text-emerald-600 dark:text-emerald-400">
                    Verified — matches {compare.algo}
                  </p>
                  <p className="text-fg-muted text-[12.5px]">
                    The {compare.algo} checksum of your {sourceLabel} equals the value you pasted.
                  </p>
                </div>
              </div>
            ) : compare.state === "mismatch" ? (
              <div className="border-danger/40 bg-danger-soft/40 flex items-start gap-2.5 rounded-lg border p-3">
                <ShieldX className="text-danger mt-0.5 size-5 shrink-0" />
                <div className="min-w-0 flex-1">
                  <p className="text-danger text-[13px] font-semibold">No match</p>
                  <p className="text-fg-muted text-[12.5px]">
                    {compare.guessed
                      ? `This looks like a ${compare.guessed} checksum, but the ${sourceLabel}'s ${compare.guessed} is:`
                      : `That value (${compare.token.length} hex chars) doesn't match any computed checksum, and its length isn't a standard digest size.`}
                  </p>
                  {compare.guessed && activeHashes?.[compare.guessed] && (
                    <div className="mt-2 flex items-center gap-2">
                      <code className="text-fg bg-bg-subtle min-w-0 flex-1 rounded-md px-2 py-1.5 font-mono text-[12px] break-all">
                        {show(activeHashes[compare.guessed])}
                      </code>
                      <CopyIconButton value={show(activeHashes[compare.guessed])} label={`Copy ${compare.guessed}`} />
                    </div>
                  )}
                </div>
              </div>
            ) : compare.state === "unavailable" ? (
              <div className="border-edge bg-bg-subtle/50 flex items-start gap-2.5 rounded-lg border p-3">
                <ShieldAlert className="mt-0.5 size-5 shrink-0 text-amber-500" />
                <div className="min-w-0">
                  <p className="text-fg text-[13px] font-semibold">Can't verify {compare.guessed} here</p>
                  <p className="text-fg-muted text-[12.5px]">
                    That looks like a {compare.guessed} value, but SHA hashing needs a secure context (HTTPS or localhost).
                    MD5 checks still work.
                  </p>
                </div>
              </div>
            ) : compare.state === "nohashes" ? (
              <div className="border-edge bg-bg-subtle/50 text-fg-muted flex items-center gap-2 rounded-lg border p-3 text-[12.5px]">
                <TriangleAlert className="size-4 shrink-0 text-amber-500" />
                Add {tab === "text" ? "text" : "a file"} above first, then this will verify against it.
              </div>
            ) : (
              <div className="border-edge bg-bg-subtle/50 text-fg-muted flex items-center gap-2 rounded-lg border p-3 text-[12.5px]">
                <TriangleAlert className="size-4 shrink-0 text-amber-500" />
                That doesn't look like a hexadecimal checksum.
              </div>
            )}
          </div>
        )}
      </ToolCard>
    </div>
  );
}
