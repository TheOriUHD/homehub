import { useMemo, useState, type ReactNode } from "react";
import { useMutation } from "@tanstack/react-query";
import {
  AlertTriangle,
  CalendarClock,
  Fingerprint,
  Globe,
  Link2,
  Lock,
  ScrollText,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
} from "lucide-react";
import { api, isApiError, unwrap, type ApiSchemas } from "@/lib/api";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * SSL / TLS certificate checker. POSTs a host+port to /tools/ssl and lays the
 * server's certificate reply out clearly: a prominent validity header with a
 * big days-remaining counter, the leaf certificate's full detail, and the
 * presented chain (leaf → intermediate → root). Every value is copyable and
 * connect failures surface both inline and via a toast.
 */

type SSLResult = ApiSchemas["SSLResult"];
type CertInfo = ApiSchemas["CertInfo"];

const EXAMPLES = ["github.com", "cloudflare.com", "expired.badssl.com", "self-signed.badssl.com"];

// ---- formatting helpers ---------------------------------------------------

function fmtDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** Pull the CN out of a "CN=…,O=…" DN, else return the whole string. */
function commonName(dn: string): string {
  const m = dn.match(/CN=([^,]+)/i);
  return (m ? m[1] : dn).trim() || dn;
}

/** Colour for the big counter and its accents, per the <14 / <30 thresholds. */
function daysAccent(days: number, expired: boolean): string {
  if (expired || days < 0 || days < 14) return "text-red-400";
  if (days < 30) return "text-amber-400";
  return "text-emerald-400";
}

// ---- component ------------------------------------------------------------

export default function Tool() {
  const [host, setHost] = useState("");
  const [port, setPort] = useState("");
  const [touched, setTouched] = useState(false);

  const portError = useMemo(() => {
    const raw = port.trim();
    if (raw === "") return null;
    if (!/^\d+$/.test(raw)) return "Port must be a whole number.";
    const n = Number(raw);
    if (n < 1 || n > 65535) return "Port must be between 1 and 65535.";
    return null;
  }, [port]);

  const toast = useToast();

  const mutation = useMutation<SSLResult, unknown, { host: string; port?: number }>({
    mutationFn: async (body) => unwrap(await api.POST("/tools/ssl", { body })),
    onError: (e) => toast("error", isApiError(e) ? e.message : String(e)),
  });

  const submit = () => {
    setTouched(true);
    const h = host.trim();
    if (!h || portError) return;
    const p = port.trim() === "" ? undefined : Number(port);
    mutation.mutate({ host: h, port: p });
  };

  const runExample = (h: string) => {
    setHost(h);
    setPort("");
    setTouched(true);
    mutation.mutate({ host: h });
  };

  const result = mutation.data;
  const connectError = mutation.isError ? (isApiError(mutation.error) ? mutation.error.message : String(mutation.error)) : null;

  return (
    <div className="space-y-4">
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Lock className="text-fg-muted size-4" />
            Host
          </span>
        }
      >
        <form
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
          className="flex flex-col gap-3 sm:flex-row sm:items-end"
        >
          <div className="min-w-0 flex-1">
            <Field
              label="Hostname or IP"
              error={touched && !host.trim() ? "Enter a host to check." : null}
              hint="A domain or address that speaks TLS, e.g. github.com."
            >
              {(p) => (
                <Input
                  {...p}
                  value={host}
                  onChange={(e) => setHost(e.target.value)}
                  placeholder="github.com"
                  spellCheck={false}
                  autoComplete="off"
                  autoCapitalize="off"
                  autoFocus
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <div className="sm:w-32">
            <Field label="Port" optional="443" error={touched ? portError : null}>
              {(p) => (
                <Input
                  {...p}
                  value={port}
                  onChange={(e) => setPort(e.target.value)}
                  placeholder="443"
                  inputMode="numeric"
                  spellCheck={false}
                  autoComplete="off"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <Button type="submit" variant="primary" loading={mutation.isPending} className="h-8.5 shrink-0 px-4">
            {mutation.isPending ? "Checking…" : "Check"}
          </Button>
        </form>

        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-fg-subtle mr-1 text-[12px]">Try</span>
          {EXAMPLES.map((h) => (
            <Button
              key={h}
              size="sm"
              variant="ghost"
              className="font-mono text-[12px]"
              disabled={mutation.isPending}
              onClick={() => runExample(h)}
            >
              {h}
            </Button>
          ))}
        </div>
      </ToolCard>

      {connectError && !mutation.isPending && (
        <ToolCard>
          <div className="flex items-start gap-2.5 text-[13px]">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-red-400" />
            <div className="min-w-0">
              <p className="text-fg font-medium">Could not complete the TLS check</p>
              <p className="text-fg-muted mt-0.5">{connectError}</p>
            </div>
          </div>
        </ToolCard>
      )}

      {result && <Results r={result} />}
    </div>
  );
}

// ---- results --------------------------------------------------------------

function Results({ r }: { r: SSLResult }) {
  const expired = r.expired || r.daysRemaining < 0;
  const ok = r.valid && !expired;

  const status = ok
    ? { label: "Valid", tone: "success" as const, Icon: ShieldCheck, cls: "text-emerald-400" }
    : expired
      ? { label: "Expired", tone: "danger" as const, Icon: ShieldAlert, cls: "text-red-400" }
      : { label: "Invalid", tone: "danger" as const, Icon: ShieldX, cls: "text-red-400" };

  const counterCls = daysAccent(r.daysRemaining, expired);
  const daysLabel =
    r.daysRemaining < 0 ? `expired ${Math.abs(r.daysRemaining)} day${Math.abs(r.daysRemaining) === 1 ? "" : "s"} ago` : "days remaining";

  const leaf = r.leaf;
  const sans = leaf.dnsNames ?? [];

  const summary = [
    ["Host", `${r.host}:${r.port}`],
    ["Status", status.label],
    ["Days remaining", String(r.daysRemaining)],
    ["Subject", leaf.subject],
    ["Issuer", leaf.issuer],
    ["Valid from", fmtDate(leaf.notBefore)],
    ["Valid until", fmtDate(leaf.notAfter)],
    ["Serial", leaf.serial],
    ["Signature", leaf.sigAlgo],
    ["TLS", r.tlsVersion],
    ["Cipher", r.cipher],
    ["SANs", sans.join(", ")],
  ]
    .map(([k, v]) => `${k.padEnd(16)}${v}`)
    .join("\n");

  return (
    <>
      {/* Status header ---------------------------------------------------- */}
      <ToolCard>
        <div className="flex flex-wrap items-center justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <status.Icon className={cn("size-9 shrink-0", status.cls)} />
            <div className="min-w-0">
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={status.tone} className="px-2.5 py-1 text-[13px]">
                  {status.label}
                </Badge>
                <span className="text-fg flex items-center gap-1.5 font-mono text-[15px] font-semibold">
                  <Globe className="text-fg-subtle size-4" />
                  {r.host}
                  <span className="text-fg-subtle">:{r.port}</span>
                </span>
              </div>
              <p className="text-fg-muted mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[12.5px]">
                <span>{r.tlsVersion}</span>
                <span className="text-fg-subtle">·</span>
                <span className="font-mono">{r.cipher}</span>
              </p>
            </div>
          </div>

          <div className="text-right">
            <div className={cn("font-mono text-[34px] leading-none font-bold tabular-nums", counterCls)}>
              {r.daysRemaining}
            </div>
            <div className="text-fg-subtle mt-1 text-[12px]">{daysLabel}</div>
          </div>
        </div>

        {!r.valid && r.validationError && (
          <div className="mt-3 flex items-start gap-2.5 rounded-lg border border-red-500/30 bg-red-500/10 px-3 py-2.5 text-[13px]">
            <AlertTriangle className="mt-0.5 size-4 shrink-0 text-red-400" />
            <div className="min-w-0">
              <p className="text-fg font-medium">Validation failed</p>
              <p className="text-fg-muted mt-0.5 break-words">{r.validationError}</p>
            </div>
          </div>
        )}
      </ToolCard>

      {/* Leaf certificate ------------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <ScrollText className="text-fg-muted size-4" />
            Certificate
          </span>
        }
        right={<CopyIconButton value={summary} label="Copy all details" />}
      >
        <div className="grid grid-cols-1 gap-x-8 lg:grid-cols-2">
          <ResultRow label="Subject" value={leaf.subject} />
          <ResultRow label="Issuer" value={leaf.issuer} />
          <ResultRow label="Valid from" value={fmtDate(leaf.notBefore)} />
          <ResultRow label="Valid until" value={fmtDate(leaf.notAfter)} />
          <ResultRow label="Days remaining" value={String(r.daysRemaining)} />
          <ResultRow label="Serial" value={leaf.serial} />
          <ResultRow label="Signature algorithm" value={leaf.sigAlgo} />
          <ResultRow label="TLS version" value={r.tlsVersion} />
          <ResultRow label="Cipher" value={r.cipher} />
        </div>

        <div className="border-edge mt-3 border-t pt-3">
          <div className="mb-2 flex items-center gap-2">
            <span className="text-fg-subtle flex items-center gap-1.5 text-[12.5px]">
              <Fingerprint className="size-3.5" />
              Subject alternative names
            </span>
            <span className="text-fg-subtle text-[12px]">({sans.length})</span>
            <span className="flex-1" />
            {sans.length > 0 && <CopyIconButton value={sans.join("\n")} label="Copy all SANs" />}
          </div>
          {sans.length === 0 ? (
            <p className="text-fg-subtle text-[12.5px]">No subject alternative names presented.</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {sans.map((name) => (
                <span
                  key={name}
                  className="border-edge bg-bg-subtle text-fg inline-flex items-center gap-1 rounded-md border py-0.5 pr-1 pl-2 font-mono text-[12px]"
                >
                  {name}
                  <CopyIconButton value={name} label={`Copy ${name}`} className="p-0.5" />
                </span>
              ))}
            </div>
          )}
        </div>
      </ToolCard>

      {/* Chain ------------------------------------------------------------ */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Link2 className="text-fg-muted size-4" />
            Chain
          </span>
        }
        right={<span className="text-fg-subtle text-[12px]">{r.chain.length} certificate{r.chain.length === 1 ? "" : "s"}</span>}
      >
        {r.chain.length === 0 ? (
          <p className="text-fg-subtle text-[12.5px]">The server presented no chain certificates.</p>
        ) : (
          <ol className="space-y-2">
            {r.chain.map((cert, i) => (
              <ChainRow key={`${cert.serial}-${i}`} cert={cert} index={i} last={i === r.chain.length - 1} total={r.chain.length} />
            ))}
          </ol>
        )}
      </ToolCard>
    </>
  );
}

function ChainRow({ cert, index, last, total }: { cert: CertInfo; index: number; last: boolean; total: number }) {
  const role: { label: string; tone: Parameters<typeof Badge>[0]["tone"] } =
    total === 1
      ? { label: "Leaf · self-signed", tone: "warning" }
      : index === 0
        ? { label: "Leaf", tone: "accent" }
        : last
          ? { label: "Root", tone: "neutral" }
          : { label: "Intermediate", tone: "info" };

  return (
    <li className="border-edge bg-bg-subtle relative rounded-lg border p-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-fg-subtle font-mono text-[11px]">#{index + 1}</span>
        <Badge tone={role.tone}>{role.label}</Badge>
        {cert.isCA && <Badge tone="neutral">CA</Badge>}
        <span className="flex-1" />
        <span className="text-fg-subtle flex items-center gap-1 text-[12px]">
          <CalendarClock className="size-3.5" />
          {fmtDate(cert.notAfter)}
        </span>
      </div>
      <div className="mt-2 space-y-0.5">
        <NameLine label="Subject" value={commonName(cert.subject)} full={cert.subject} />
        <NameLine label="Issuer" value={commonName(cert.issuer)} full={cert.issuer} />
      </div>
    </li>
  );
}

function NameLine({ label, value, full }: { label: string; value: string; full: string }): ReactNode {
  return (
    <div className="flex items-center gap-2">
      <span className="text-fg-subtle w-14 shrink-0 text-[11.5px]">{label}</span>
      <span className="text-fg min-w-0 flex-1 truncate font-mono text-[12.5px]" title={full}>
        {value}
      </span>
      <CopyIconButton value={full} label={`Copy ${label.toLowerCase()}`} className="p-1" />
    </div>
  );
}
