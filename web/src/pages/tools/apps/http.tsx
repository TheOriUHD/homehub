import { useMemo, useRef, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowDownUp,
  Braces,
  CornerDownLeft,
  ListTree,
  Plus,
  Send,
  Terminal,
  Timer,
  WrapText,
  X,
} from "lucide-react";
import { api, isApiError, unwrap, type ApiSchemas } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * HTTP request tester — a mini API client (Postman-lite). Builds a request from
 * a method + URL + repeatable header rows + optional body, POSTs it to
 * /tools/http (the server performs the fetch), and lays the reply out: a
 * status header coloured by class, a Body panel that pretty-prints JSON when it
 * can, and a Headers panel. Every output is copyable; the request is also
 * exportable as a cURL command. Request failures ("no such host", "too many
 * redirects") surface inline and via a toast.
 */

type HTTPResult = ApiSchemas["HTTPResult"];

type ReqBody = {
  method: string;
  url: string;
  headers?: Record<string, string>;
  body?: string;
};

const METHODS = ["GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"] as const;
const BODY_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

/** Common header names offered as autocomplete in the key field. */
const HEADER_NAMES = [
  "Accept",
  "Accept-Encoding",
  "Authorization",
  "Cache-Control",
  "Content-Type",
  "Cookie",
  "Origin",
  "Referer",
  "User-Agent",
  "X-Api-Key",
  "X-Requested-With",
];

type Example = { method: string; url: string; label: string };
const EXAMPLES: Example[] = [
  { method: "GET", url: "https://api.github.com/zen", label: "github.com/zen" },
  { method: "GET", url: "https://httpbin.org/json", label: "httpbin/json" },
  { method: "GET", url: "https://httpbin.org/headers", label: "httpbin/headers" },
  { method: "POST", url: "https://httpbin.org/post", label: "httpbin/post" },
];

type HeaderRow = { id: number; key: string; value: string };

// ---- helpers --------------------------------------------------------------

/** Prepend https:// when the user typed a bare host. */
function normalizeUrl(raw: string): string {
  const t = raw.trim();
  if (!t) return t;
  return /^[a-z][a-z0-9+.-]*:\/\//i.test(t) ? t : `https://${t}`;
}

function headersObject(rows: HeaderRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) {
    const k = r.key.trim();
    if (k) out[k] = r.value;
  }
  return out;
}

/** Case-insensitive header lookup over the response header map. */
function lookupHeader(headers: Record<string, string>, name: string): string | undefined {
  const target = name.toLowerCase();
  for (const [k, v] of Object.entries(headers)) if (k.toLowerCase() === target) return v;
  return undefined;
}

function humanSize(n: number): string {
  if (n < 1024) return `${n} B`;
  const kib = n / 1024;
  if (kib < 1024) return `${kib.toFixed(kib < 10 ? 2 : 1)} KiB`;
  const mib = kib / 1024;
  if (mib < 1024) return `${mib.toFixed(mib < 10 ? 2 : 1)} MiB`;
  return `${(mib / 1024).toFixed(2)} GiB`;
}

function humanDuration(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)} ms`;
  return `${(ms / 1000).toFixed(2)} s`;
}

/** POSIX single-quote a shell argument for the cURL export. */
function shellQuote(s: string): string {
  return `'${s.replace(/'/g, `'\\''`)}'`;
}

function toCurl(method: string, url: string, headers: Record<string, string>, body: string | undefined): string {
  const parts = [`curl`];
  if (method !== "GET") parts.push(`-X ${method}`);
  parts.push(shellQuote(url));
  for (const [k, v] of Object.entries(headers)) parts.push(`-H ${shellQuote(`${k}: ${v}`)}`);
  if (body != null && body !== "") parts.push(`--data-raw ${shellQuote(body)}`);
  return parts.join(" \\\n  ");
}

/** Colour + badge tone for a status code, by class. */
function statusStyle(status: number): { tone: Parameters<typeof Badge>[0]["tone"]; cls: string } {
  if (status >= 200 && status < 300) return { tone: "success", cls: "text-emerald-400" };
  if (status >= 300 && status < 400) return { tone: "warning", cls: "text-amber-400" };
  if (status >= 400) return { tone: "danger", cls: "text-red-400" };
  return { tone: "neutral", cls: "text-fg-muted" };
}

const methodTint: Record<string, string> = {
  GET: "text-emerald-400",
  POST: "text-amber-400",
  PUT: "text-sky-400",
  PATCH: "text-violet-400",
  DELETE: "text-red-400",
  HEAD: "text-fg-muted",
  OPTIONS: "text-fg-muted",
};

// ---- component ------------------------------------------------------------

let ROW_SEQ = 1;

export default function Tool() {
  const toast = useToast();
  const [method, setMethod] = useState<string>("GET");
  const [url, setUrl] = useState("https://api.github.com/zen");
  const [rows, setRows] = useState<HeaderRow[]>([{ id: ROW_SEQ++, key: "", value: "" }]);
  const [body, setBody] = useState("");
  const [attempted, setAttempted] = useState(false);
  const [sentUrl, setSentUrl] = useState("");
  const runId = useRef(0);
  const [runKey, setRunKey] = useState(0);

  const bodyAllowed = BODY_METHODS.has(method);

  const mutation = useMutation<HTTPResult, unknown, ReqBody>({
    mutationFn: async (payload) => unwrap(await api.POST("/tools/http", { body: payload })),
    onSuccess: () => {
      runId.current += 1;
      setRunKey(runId.current);
    },
    onError: (e) => toast("error", isApiError(e) ? e.message : String(e)),
  });

  const urlError = attempted && !url.trim() ? "Enter a URL to send." : null;

  const buildPayload = (over?: Partial<ReqBody>): ReqBody => {
    const finalUrl = normalizeUrl(over?.url ?? url);
    const hdrs = over?.headers ?? headersObject(rows);
    const useMethod = over?.method ?? method;
    const payload: ReqBody = { method: useMethod, url: finalUrl };
    if (Object.keys(hdrs).length) payload.headers = hdrs;
    const b = over?.body ?? body;
    if (BODY_METHODS.has(useMethod) && b.trim() !== "") payload.body = b;
    return payload;
  };

  const submit = () => {
    setAttempted(true);
    if (!url.trim()) return;
    const payload = buildPayload();
    setSentUrl(payload.url);
    mutation.mutate(payload);
  };

  const runExample = (ex: Example) => {
    setMethod(ex.method);
    setUrl(ex.url);
    const nextBody = ex.method === "POST" ? `{\n  "hello": "world"\n}` : "";
    setBody(nextBody);
    const hdrs: Record<string, string> = ex.method === "POST" ? { "Content-Type": "application/json" } : {};
    setRows(
      ex.method === "POST"
        ? [{ id: ROW_SEQ++, key: "Content-Type", value: "application/json" }]
        : [{ id: ROW_SEQ++, key: "", value: "" }],
    );
    setAttempted(true);
    const payload = buildPayload({ method: ex.method, url: ex.url, headers: hdrs, body: nextBody });
    setSentUrl(payload.url);
    mutation.mutate(payload);
  };

  // --- header row editing ---
  const setRow = (id: number, patch: Partial<HeaderRow>) =>
    setRows((prev) => prev.map((r) => (r.id === id ? { ...r, ...patch } : r)));
  const addRow = () => setRows((prev) => [...prev, { id: ROW_SEQ++, key: "", value: "" }]);
  const removeRow = (id: number) =>
    setRows((prev) => {
      const next = prev.filter((r) => r.id !== id);
      return next.length ? next : [{ id: ROW_SEQ++, key: "", value: "" }];
    });

  const activeHeaders = rows.filter((r) => r.key.trim()).length;
  const curl = toCurl(method, normalizeUrl(url) || "URL", headersObject(rows), bodyAllowed && body.trim() ? body : undefined);

  const formatBody = () => {
    try {
      setBody(JSON.stringify(JSON.parse(body), null, 2));
    } catch {
      toast("error", "Body is not valid JSON — nothing to format.");
    }
  };

  const reqError = mutation.isError ? (isApiError(mutation.error) ? mutation.error.message : String(mutation.error)) : null;

  return (
    <div className="space-y-4">
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Send className="text-fg-muted size-4" />
            Request
          </span>
        }
        right={
          url.trim() ? (
            <span className="text-fg-subtle flex items-center gap-1 text-[12px]">
              <Terminal className="size-3.5" />
              cURL
              <CopyIconButton value={curl} label="Copy as cURL" />
            </span>
          ) : null
        }
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          {/* method · url · send ------------------------------------------- */}
          <div className="flex flex-col gap-2 sm:flex-row sm:items-start">
            <div className="sm:w-36">
              <Select value={method} onValueChange={setMethod}>
                <SelectTrigger aria-label="HTTP method">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {METHODS.map((m) => (
                    <SelectItem key={m} value={m}>
                      <span className={cn("font-mono font-semibold", methodTint[m])}>{m}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="min-w-0 flex-1">
              <Input
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://api.example.com/path"
                aria-label="Request URL"
                aria-invalid={urlError ? true : undefined}
                spellCheck={false}
                autoComplete="off"
                autoCapitalize="off"
                className="font-mono"
              />
              {urlError && <p className="text-danger mt-1.5 text-[13px]">{urlError}</p>}
            </div>
            <Button type="submit" variant="primary" loading={mutation.isPending} className="h-8.5 shrink-0 px-4">
              {mutation.isPending ? "Sending…" : "Send"}
            </Button>
          </div>

          {/* headers editor ------------------------------------------------ */}
          <div className="mt-4">
            <div className="mb-2 flex items-center gap-2">
              <span className="text-fg-subtle text-[12.5px] font-medium">Headers</span>
              {activeHeaders > 0 && <Badge tone="neutral">{activeHeaders}</Badge>}
              <span className="flex-1" />
              <Button type="button" size="sm" variant="ghost" onClick={addRow}>
                <Plus className="size-3.5" />
                Add
              </Button>
            </div>
            <datalist id="http-header-names">
              {HEADER_NAMES.map((n) => (
                <option key={n} value={n} />
              ))}
            </datalist>
            <div className="space-y-2">
              {rows.map((r) => (
                <div key={r.id} className="flex items-center gap-2">
                  <Input
                    value={r.key}
                    onChange={(e) => setRow(r.id, { key: e.target.value })}
                    placeholder="Header"
                    list="http-header-names"
                    spellCheck={false}
                    autoComplete="off"
                    autoCapitalize="off"
                    className="font-mono text-[13px] sm:w-64"
                  />
                  <span className="text-fg-subtle shrink-0">:</span>
                  <Input
                    value={r.value}
                    onChange={(e) => setRow(r.id, { value: e.target.value })}
                    placeholder="value"
                    spellCheck={false}
                    autoComplete="off"
                    autoCapitalize="off"
                    className="min-w-0 flex-1 font-mono text-[13px]"
                  />
                  <button
                    type="button"
                    onClick={() => removeRow(r.id)}
                    aria-label="Remove header"
                    title="Remove header"
                    className="text-fg-subtle hover:text-danger hover:bg-bg-subtle inline-flex shrink-0 items-center justify-center rounded-md p-1.5 transition-colors"
                  >
                    <X className="size-4" />
                  </button>
                </div>
              ))}
            </div>
          </div>

          {/* body ---------------------------------------------------------- */}
          {bodyAllowed && (
            <div className="mt-4">
              <div className="mb-2 flex items-center gap-2">
                <span className="text-fg-subtle text-[12.5px] font-medium">Body</span>
                <span className="flex-1" />
                <Button type="button" size="sm" variant="ghost" onClick={formatBody} disabled={!body.trim()}>
                  <Braces className="size-3.5" />
                  Format JSON
                </Button>
              </div>
              <textarea
                value={body}
                onChange={(e) => setBody(e.target.value)}
                placeholder={`{\n  "key": "value"\n}`}
                spellCheck={false}
                rows={7}
                className="border-edge bg-bg-subtle w-full rounded-lg border p-2.5 font-mono text-[13px] text-fg"
              />
            </div>
          )}

          <p className="text-fg-subtle mt-3 flex items-center gap-1.5 text-[12px]">
            <CornerDownLeft className="size-3.5" />
            Press Enter in the URL to send.
          </p>
        </form>

        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-fg-subtle mr-1 text-[12px]">Try</span>
          {EXAMPLES.map((ex) => (
            <Button
              key={ex.url + ex.method}
              type="button"
              size="sm"
              variant="ghost"
              className="font-mono text-[12px]"
              disabled={mutation.isPending}
              onClick={() => runExample(ex)}
            >
              <span className={cn("font-semibold", methodTint[ex.method])}>{ex.method}</span>
              {ex.label}
            </Button>
          ))}
        </div>
      </ToolCard>

      {reqError && !mutation.isPending && (
        <ToolCard>
          <div className="flex items-start gap-2.5 text-[13px]">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-red-400" />
            <div className="min-w-0">
              <p className="text-fg font-medium">The request could not be completed</p>
              <p className="text-fg-muted mt-0.5 break-words">{reqError}</p>
            </div>
          </div>
        </ToolCard>
      )}

      {mutation.data && !mutation.isPending && !mutation.isError && (
        <ResponsePanel key={runKey} r={mutation.data} sentUrl={sentUrl} />
      )}

      {(!mutation.data || mutation.isError) && !reqError && !mutation.isPending && (
        <ToolCard>
          <p className="text-fg-subtle flex items-center gap-2 text-[13px]">
            <Send className="size-4 shrink-0" />
            Build a request above and hit Send — the server performs the call and the full response appears here.
          </p>
        </ToolCard>
      )}
    </div>
  );
}

// ---- response -------------------------------------------------------------

function ResponsePanel({ r, sentUrl }: { r: HTTPResult; sentUrl: string }) {
  const [tab, setTab] = useState<"body" | "headers">("body");
  const [view, setView] = useState<"pretty" | "raw">("pretty");
  const [wrap, setWrap] = useState(false);

  const style = statusStyle(r.status);
  const contentType = lookupHeader(r.headers, "content-type") ?? "";
  const looksJson = !r.binary && (/\bjson\b/i.test(contentType) || /^\s*[[{]/.test(r.body));

  const pretty = useMemo(() => {
    if (!looksJson) return null;
    try {
      return JSON.stringify(JSON.parse(r.body), null, 2);
    } catch {
      return null;
    }
  }, [r.body, looksJson]);

  const redirected = sentUrl && r.finalUrl && r.finalUrl !== sentUrl;

  const displayBody = r.binary
    ? "[binary — not shown]"
    : view === "pretty" && pretty != null
      ? pretty
      : r.body;

  const headersText = r.headerKeys.map((k) => `${k}: ${r.headers[k] ?? ""}`).join("\n");

  return (
    <>
      {/* status header ---------------------------------------------------- */}
      <ToolCard>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex min-w-0 items-baseline gap-3">
            <span className={cn("font-mono text-[34px] leading-none font-bold tabular-nums", style.cls)}>
              {r.status || "—"}
            </span>
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={style.tone} className="px-2.5 py-1 text-[13px]">
                  {r.statusText || `${r.status}`}
                </Badge>
                <span className="text-fg-subtle font-mono text-[12.5px]">{r.proto}</span>
              </div>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px]">
            <span className="text-fg-muted flex items-center gap-1.5">
              <Timer className="text-fg-subtle size-3.5" />
              <span className="font-mono tabular-nums">{humanDuration(r.durationMs)}</span>
            </span>
            <span className="text-fg-muted flex items-center gap-1.5">
              <ArrowDownUp className="text-fg-subtle size-3.5" />
              <span className="font-mono tabular-nums">{humanSize(r.size)}</span>
            </span>
          </div>
        </div>

        {redirected && (
          <p className="text-fg-muted border-edge mt-3 border-t pt-3 text-[12.5px]">
            <span className="text-fg-subtle">Redirected to </span>
            <span className="font-mono break-all">{r.finalUrl}</span>
          </p>
        )}
        {r.truncated && (
          <p className="mt-3 flex items-start gap-2 rounded-lg border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-[12.5px]">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-amber-400" />
            <span className="text-fg-muted">Response body was truncated at 5 MiB — only the first 5 MiB is shown.</span>
          </p>
        )}
      </ToolCard>

      {/* body / headers --------------------------------------------------- */}
      <ToolCard
        title={
          <div className="flex items-center gap-1">
            <button
              type="button"
              onClick={() => setTab("body")}
              className={cn(
                "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[13px] font-medium transition-colors",
                tab === "body" ? "bg-accent-soft text-accent-soft-fg" : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
              )}
            >
              <ListTree className="size-3.5" />
              Body
            </button>
            <button
              type="button"
              onClick={() => setTab("headers")}
              className={cn(
                "flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[13px] font-medium transition-colors",
                tab === "headers" ? "bg-accent-soft text-accent-soft-fg" : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
              )}
            >
              <ListTree className="size-3.5" />
              Headers
              <span className="text-fg-subtle text-[11px]">{r.headerKeys.length}</span>
            </button>
          </div>
        }
        right={
          tab === "body" ? (
            <div className="flex items-center gap-1.5">
              {pretty != null && (
                <div className="border-edge flex overflow-hidden rounded-md border text-[12px]">
                  {(["pretty", "raw"] as const).map((v) => (
                    <button
                      key={v}
                      type="button"
                      onClick={() => setView(v)}
                      className={cn(
                        "px-2 py-1 capitalize transition-colors",
                        view === v ? "bg-accent-soft text-accent-soft-fg" : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
                      )}
                    >
                      {v}
                    </button>
                  ))}
                </div>
              )}
              <button
                type="button"
                onClick={() => setWrap((w) => !w)}
                aria-label="Toggle line wrap"
                title="Toggle line wrap"
                className={cn(
                  "inline-flex items-center justify-center rounded-md p-1.5 transition-colors",
                  wrap ? "bg-accent-soft text-accent-soft-fg" : "text-fg-subtle hover:text-fg hover:bg-bg-subtle",
                )}
              >
                <WrapText className="size-3.5" />
              </button>
              {!r.binary && r.body !== "" && <CopyIconButton value={displayBody} label="Copy response body" />}
            </div>
          ) : (
            <CopyIconButton value={headersText} label="Copy all headers" />
          )
        }
      >
        {tab === "body" ? (
          <BodyView displayBody={displayBody} empty={!r.binary && r.body === ""} wrap={wrap} />
        ) : (
          <HeadersView r={r} />
        )}
      </ToolCard>
    </>
  );
}

function BodyView({ displayBody, empty, wrap }: { displayBody: string; empty: boolean; wrap: boolean }) {
  if (empty) {
    return <p className="text-fg-subtle text-[13px]">The response had an empty body.</p>;
  }
  return (
    <pre
      className={cn(
        "border-edge bg-bg-subtle text-fg max-h-[30rem] overflow-auto rounded-lg border p-3 font-mono text-[12.5px] leading-relaxed",
        wrap ? "break-words whitespace-pre-wrap" : "whitespace-pre",
      )}
    >
      {displayBody}
    </pre>
  );
}

function HeadersView({ r }: { r: HTTPResult }) {
  if (r.headerKeys.length === 0) {
    return <p className="text-fg-subtle text-[13px]">The response carried no headers.</p>;
  }
  return (
    <div className="divide-edge divide-y">
      {r.headerKeys.map((k, i) => {
        const value = r.headers[k] ?? "";
        return (
          <div key={`${k}-${i}`} className="flex items-start gap-3 py-1.5">
            <span className="text-fg-muted w-40 shrink-0 font-mono text-[12.5px] break-words">{k}</span>
            <span className="text-fg min-w-0 flex-1 font-mono text-[12.5px] break-words">{value || "—"}</span>
            {value && <CopyIconButton value={value} label={`Copy ${k}`} className="p-1" />}
          </div>
        );
      })}
    </div>
  );
}
