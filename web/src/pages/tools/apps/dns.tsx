import { useEffect, useMemo, useState, type FormEvent } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { AlertTriangle, Clock, Globe, Info, Search, Server } from "lucide-react";
import { api, isApiError, unwrap, type ApiSchemas } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * DNS lookup. Resolvers load on mount from the server (which also
 * auto-detects any paired AdGuard/Pi-hole instances); a query is a single
 * POST /tools/dns. The last successful answer is held in state so it stays
 * on screen while a follow-up query is in flight.
 */

type ResolverOption = ApiSchemas["ResolverOption"];
type DNSResult = ApiSchemas["DNSResult"];
type DNSRecord = ApiSchemas["DNSRecord"];
type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

const RECORD_TYPES = ["A", "AAAA", "CNAME", "TXT", "MX", "NS", "PTR"] as const;
type RecordType = (typeof RECORD_TYPES)[number];

const TYPE_META: Record<RecordType, { desc: string; tone: Tone }> = {
  A: { desc: "IPv4 address", tone: "accent" },
  AAAA: { desc: "IPv6 address", tone: "accent" },
  CNAME: { desc: "Canonical name", tone: "info" },
  TXT: { desc: "Text records", tone: "neutral" },
  MX: { desc: "Mail exchange", tone: "warning" },
  NS: { desc: "Name servers", tone: "info" },
  PTR: { desc: "Reverse (IP → name)", tone: "success" },
};

const PRESETS: { name: string; type: RecordType }[] = [
  { name: "example.com", type: "A" },
  { name: "cloudflare.com", type: "AAAA" },
  { name: "google.com", type: "MX" },
  { name: "github.com", type: "TXT" },
  { name: "1.1.1.1", type: "PTR" },
];

export default function Tool() {
  const toast = useToast();

  const [name, setName] = useState("");
  const [type, setType] = useState<RecordType>("A");
  const [resolver, setResolver] = useState("system");
  const [customServer, setCustomServer] = useState("");

  const [nameError, setNameError] = useState<string | null>(null);
  const [customError, setCustomError] = useState<string | null>(null);
  const [apiError, setApiError] = useState<string | null>(null);
  const [result, setResult] = useState<DNSResult | null>(null);

  const resolversQuery = useQuery({
    queryKey: ["tools", "dns", "resolvers"],
    queryFn: async () => unwrap(await api.GET("/tools/dns/resolvers")),
  });
  // If the resolvers list can't load, fall back to a static set so the picker
  // is never empty — the user can always reach the system resolver, the public
  // ones, and the custom-server option.
  const FALLBACK_RESOLVERS: ResolverOption[] = [
    { id: "system", label: "System default" },
    { id: "cloudflare", label: "Cloudflare (1.1.1.1)", server: "1.1.1.1:53" },
    { id: "google", label: "Google (8.8.8.8)", server: "8.8.8.8:53" },
    { id: "quad9", label: "Quad9 (9.9.9.9)", server: "9.9.9.9:53" },
    { id: "custom", label: "Custom server…" },
  ];
  const resolvers: ResolverOption[] =
    resolversQuery.data && resolversQuery.data.length ? resolversQuery.data : FALLBACK_RESOLVERS;

  // Once resolvers load, snap the selection to a real option if the default
  // ("system") isn't offered by this server.
  useEffect(() => {
    if (resolvers.length && !resolvers.some((r) => r.id === resolver)) {
      setResolver(resolvers[0].id);
    }
  }, [resolvers, resolver]);

  const lookup = useMutation({
    mutationFn: async (body: {
      name: string;
      type: RecordType;
      resolver: string;
      customServer?: string;
    }) => unwrap(await api.POST("/tools/dns", { body })),
    onSuccess: (data) => {
      setResult(data);
      setApiError(null);
    },
    onError: (e) => {
      const msg = isApiError(e) ? e.message : String(e);
      setApiError(msg);
      toast("error", msg);
    },
  });

  const run = (rawName: string, rt: RecordType) => {
    const n = rawName.trim();
    if (!n) {
      setNameError("Enter a name to look up.");
      return;
    }
    const custom = customServer.trim();
    if (resolver === "custom" && !custom) {
      setCustomError("Enter a server as an IP or ip:port.");
      return;
    }
    setNameError(null);
    setCustomError(null);
    setApiError(null);
    lookup.mutate({
      name: n,
      type: rt,
      resolver,
      customServer: resolver === "custom" ? custom : undefined,
    });
  };

  const submit = (e: FormEvent) => {
    e.preventDefault();
    run(name, type);
  };

  const applyPreset = (p: { name: string; type: RecordType }) => {
    setName(p.name);
    setType(p.type);
    run(p.name, p.type);
  };

  const isPtr = type === "PTR";

  return (
    <div className="space-y-4">
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Globe className="text-fg-muted size-4" />
            Query
          </span>
        }
      >
        <form onSubmit={submit} className="space-y-3">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
            <div className="min-w-0 flex-1">
              <Field
                label="Name"
                error={nameError}
                hint={isPtr ? "Reverse lookup — enter an IP address." : "Domain or host name to resolve."}
              >
                {(p) => (
                  <Input
                    {...p}
                    value={name}
                    onChange={(e) => {
                      setName(e.target.value);
                      if (nameError) setNameError(null);
                    }}
                    placeholder={isPtr ? "1.1.1.1" : "example.com"}
                    spellCheck={false}
                    autoComplete="off"
                    autoCapitalize="off"
                    autoCorrect="off"
                    className="font-mono"
                  />
                )}
              </Field>
            </div>

            <div className="sm:w-36">
              <label htmlFor="dns-type" className="text-fg mb-1.5 block text-[13px] font-medium">
                Type
              </label>
              <Select value={type} onValueChange={(v) => setType(v as RecordType)}>
                <SelectTrigger id="dns-type">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RECORD_TYPES.map((t) => (
                    <SelectItem key={t} value={t}>
                      <span className="font-mono">{t}</span>
                      <span className="text-fg-subtle ml-2 text-[11.5px]">{TYPE_META[t].desc}</span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            <div className="sm:w-56">
              <label htmlFor="dns-resolver" className="text-fg mb-1.5 block text-[13px] font-medium">
                Resolver
              </label>
              <Select value={resolver} onValueChange={setResolver} disabled={resolversQuery.isLoading}>
                <SelectTrigger id="dns-resolver">
                  <SelectValue placeholder={resolversQuery.isLoading ? "Loading…" : "Resolver"} />
                </SelectTrigger>
                <SelectContent>
                  {resolvers.map((r) => (
                    <SelectItem key={r.id} value={r.id}>
                      <span>{r.label}</span>
                      {r.server && <span className="text-fg-subtle ml-2 font-mono text-[11.5px]">{r.server}</span>}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>

          {resolver === "custom" && (
            <div className="sm:max-w-xs">
              <Field label="Custom server" error={customError} hint="An IP, or ip:port (e.g. 9.9.9.9 or 1.1.1.1:53).">
                {(p) => (
                  <Input
                    {...p}
                    value={customServer}
                    onChange={(e) => {
                      setCustomServer(e.target.value);
                      if (customError) setCustomError(null);
                    }}
                    placeholder="9.9.9.9"
                    spellCheck={false}
                    autoComplete="off"
                    className="font-mono"
                  />
                )}
              </Field>
            </div>
          )}

          <div className="flex flex-wrap items-center gap-x-3 gap-y-2">
            <Button type="submit" variant="primary" size="sm" loading={lookup.isPending}>
              <Search className="size-3.5" />
              Look up
            </Button>
            <span className="text-fg-subtle text-[12px]">Try</span>
            <div className="flex flex-wrap items-center gap-1">
              {PRESETS.map((p) => (
                <Button
                  key={`${p.name}:${p.type}`}
                  type="button"
                  size="sm"
                  variant="ghost"
                  className="font-mono text-[12px]"
                  onClick={() => applyPreset(p)}
                >
                  {p.name}
                  <span className="text-fg-subtle">·{p.type}</span>
                </Button>
              ))}
            </div>
          </div>
        </form>
      </ToolCard>

      {apiError && (
        <ToolCard>
          <div className="text-fg flex items-start gap-2 text-[13px]">
            <AlertTriangle className="text-danger mt-0.5 size-4 shrink-0" />
            <span>
              <span className="font-medium">Query failed.</span> <span className="text-fg-muted">{apiError}</span>
            </span>
          </div>
        </ToolCard>
      )}

      {result ? (
        <Answer result={result} resolvers={resolvers} pending={lookup.isPending} />
      ) : (
        !apiError && (
          <ToolCard>
            <div className="text-fg-muted flex items-center gap-2 text-[13px]">
              <Globe className="text-fg-subtle size-4 shrink-0" />
              <span>Enter a name and choose a record type to run a lookup.</span>
            </div>
          </ToolCard>
        )
      )}
    </div>
  );
}

function Answer({
  result,
  resolvers,
  pending,
}: {
  result: DNSResult;
  resolvers: ResolverOption[];
  pending: boolean;
}) {
  const resolverLabel = useMemo(
    () => resolvers.find((r) => r.id === result.resolver)?.label ?? result.resolver,
    [resolvers, result.resolver],
  );

  const hasRecords = result.records.length > 0;
  const copyAll = useMemo(
    () => result.records.map((r) => `${r.type}\t${r.value}${r.ttl ? `\t${r.ttl}` : ""}`).join("\n"),
    [result.records],
  );

  return (
    <ToolCard
      title={
        <span className="flex items-center gap-2">
          <Badge tone={typeToneOf(result.type)} className="font-mono">
            {result.type}
          </Badge>
          <span className="text-fg font-mono">{result.name}</span>
          {pending && <span className="text-fg-subtle text-[12px] font-normal">querying…</span>}
        </span>
      }
      right={hasRecords ? <CopyIconButton value={copyAll} label="Copy all records" /> : undefined}
      className={cn(pending && "opacity-70 transition-opacity")}
    >
      {result.error ? (
        <div className="border-edge bg-bg-subtle text-fg flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[13px]">
          <Info className="mt-0.5 size-4 shrink-0 text-amber-400" />
          <span>{result.error}</span>
        </div>
      ) : hasRecords ? (
        <div className="divide-edge border-edge divide-y overflow-hidden rounded-lg border">
          {result.records.map((r, i) => (
            <RecordLine key={i} record={r} />
          ))}
        </div>
      ) : (
        <div className="border-edge bg-bg-subtle text-fg flex items-start gap-2 rounded-lg border px-3 py-2.5 text-[13px]">
          <Info className="mt-0.5 size-4 shrink-0 text-amber-400" />
          <span>No records found.</span>
        </div>
      )}

      <div className="text-fg-subtle mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px]">
        <span className="inline-flex items-center gap-1.5">
          <Globe className="size-3.5" />
          {resolverLabel}
        </span>
        {result.server && (
          <span className="inline-flex items-center gap-1.5">
            <Server className="size-3.5" />
            <span className="font-mono">{result.server}</span>
          </span>
        )}
        <span className="inline-flex items-center gap-1.5">
          <Clock className="size-3.5" />
          {result.queryMs} ms
        </span>
        {hasRecords && (
          <span>
            {result.records.length} record{result.records.length === 1 ? "" : "s"}
          </span>
        )}
      </div>
    </ToolCard>
  );
}

function RecordLine({ record }: { record: DNSRecord }) {
  return (
    <div className="hover:bg-bg-subtle flex items-center gap-3 px-3 py-2 transition-colors">
      <Badge tone={typeToneOf(record.type)} className="w-14 shrink-0 justify-center font-mono">
        {record.type}
      </Badge>
      <div className="min-w-0 flex-1 font-mono text-[13px] break-all">
        {record.type === "MX" ? <MxValue value={record.value} /> : <span className="text-fg">{record.value}</span>}
      </div>
      {record.ttl && <span className="text-fg-subtle shrink-0 text-[11.5px]">TTL {record.ttl}</span>}
      <CopyIconButton value={record.value} label="Copy value" />
    </div>
  );
}

/** MX values arrive as "priority host" — surface the priority as a subtle chip. */
function MxValue({ value }: { value: string }) {
  const m = value.match(/^\s*(\d+)\s+(.+?)\s*$/);
  if (!m) return <span className="text-fg">{value}</span>;
  return (
    <span className="inline-flex min-w-0 items-baseline gap-2">
      <span className="border-edge bg-bg-subtle text-fg-muted shrink-0 rounded border px-1.5 text-[11px]">
        pri {m[1]}
      </span>
      <span className="text-fg truncate">{m[2]}</span>
    </span>
  );
}

function typeToneOf(type: string): Tone {
  return (TYPE_META as Record<string, { tone: Tone }>)[type]?.tone ?? "neutral";
}
