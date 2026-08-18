import { Fragment, useMemo, useState } from "react";
import { AlertTriangle, Binary, Check, Globe, Minus, Network, Shield } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * IPv4 subnet / CIDR calculator. Everything is pure 32-bit integer math with
 * `>>> 0` so shifts stay unsigned, and every result recomputes live from the
 * single text field (a CIDR) plus an optional prefix picker. Invalid input is
 * reported inline — the component never throws.
 */

type Tone = "neutral" | "accent" | "success" | "warning" | "danger" | "info";

interface Detail {
  octets: number[];
  ip: number;
  prefix: number;
  mask: number;
  wildcard: number;
  network: number;
  broadcast: number;
  total: number;
  usable: number;
  firstHost: number;
  lastHost: number;
  note: string | null;
  isPrivate: boolean;
  isLoopback: boolean;
  isLinkLocal: boolean;
  scope: { label: string; tone: Tone };
  cls: { letter: string; desc: string };
}

type Calc =
  | { status: "empty" }
  | { status: "error"; message: string }
  | { status: "ok"; data: Detail };

// ---- 32-bit helpers (unsigned throughout) --------------------------------

const u32 = (n: number) => n >>> 0;

function toInt(a: number, b: number, c: number, d: number): number {
  return u32((a << 24) | (b << 16) | (c << 8) | d);
}

function intToIp(n: number): string {
  n = u32(n);
  return `${(n >>> 24) & 255}.${(n >>> 16) & 255}.${(n >>> 8) & 255}.${n & 255}`;
}

/** Netmask for a prefix. Guards prefix 0, where `<< 32` would be a no-op. */
function maskFor(prefix: number): number {
  return prefix === 0 ? 0 : u32(0xffffffff << (32 - prefix));
}

/** Does `ip` fall inside the network a.b.c.d/prefix? */
function contains(a: number, b: number, c: number, d: number, prefix: number, ip: number): boolean {
  if (prefix === 0) return true;
  return u32((ip ^ toInt(a, b, c, d)) >>> (32 - prefix)) === 0;
}

/** Pull a trailing / leading "/nn" out of raw text (0–32), else null. */
function slashPrefixOf(text: string): number | null {
  const m = text.match(/\/\s*(\d{1,3})/);
  if (!m) return null;
  const n = Number(m[1]);
  return n >= 0 && n <= 32 ? n : null;
}

function scopeOf(ip: number): { label: string; tone: Tone } {
  const table: { label: string; tone: Tone; nets: [number, number, number, number, number][] }[] = [
    { label: "Private · RFC 1918", tone: "success", nets: [[10, 0, 0, 0, 8], [172, 16, 0, 0, 12], [192, 168, 0, 0, 16]] },
    { label: "Loopback", tone: "info", nets: [[127, 0, 0, 0, 8]] },
    { label: "Link-local · APIPA", tone: "warning", nets: [[169, 254, 0, 0, 16]] },
    { label: "Shared · CGNAT", tone: "warning", nets: [[100, 64, 0, 0, 10]] },
    { label: "Multicast", tone: "accent", nets: [[224, 0, 0, 0, 4]] },
    { label: "Reserved · Class E", tone: "neutral", nets: [[240, 0, 0, 0, 4]] },
    { label: "This network", tone: "neutral", nets: [[0, 0, 0, 0, 8]] },
    {
      label: "Documentation",
      tone: "neutral",
      nets: [[192, 0, 2, 0, 24], [198, 51, 100, 0, 24], [203, 0, 113, 0, 24]],
    },
  ];
  for (const row of table) if (row.nets.some((n) => contains(n[0], n[1], n[2], n[3], n[4], ip))) return row;
  return { label: "Public · global unicast", tone: "info" };
}

function classOf(first: number): { letter: string; desc: string } {
  if (first <= 127) return { letter: "A", desc: "1.0.0.0 – 127.255.255.255 · classful default /8" };
  if (first <= 191) return { letter: "B", desc: "128.0.0.0 – 191.255.255.255 · classful default /16" };
  if (first <= 223) return { letter: "C", desc: "192.0.0.0 – 223.255.255.255 · classful default /24" };
  if (first <= 239) return { letter: "D", desc: "224.0.0.0 – 239.255.255.255 · multicast" };
  return { letter: "E", desc: "240.0.0.0 – 255.255.255.255 · reserved / experimental" };
}

function compute(text: string, fallbackPrefix: number): Calc {
  const raw = text.trim();
  if (!raw) return { status: "empty" };

  let ipStr = raw;
  let prefix = fallbackPrefix;
  const slash = raw.indexOf("/");
  if (slash !== -1) {
    ipStr = raw.slice(0, slash).trim();
    const pStr = raw.slice(slash + 1).trim();
    // An empty prefix (mid-typing "1.2.3.4/") quietly falls back to the picker.
    if (pStr !== "") {
      if (!/^\d+$/.test(pStr)) return { status: "error", message: `“/${pStr}” is not a valid prefix — use a number from 0 to 32.` };
      prefix = Number(pStr);
      if (prefix > 32) return { status: "error", message: "Prefix length must be between 0 and 32." };
    }
  }

  const parts = ipStr.split(".");
  if (parts.length !== 4) return { status: "error", message: "Enter a full IPv4 address with four octets, e.g. 192.168.1.10." };

  const octets: number[] = [];
  for (const part of parts) {
    if (part === "" || !/^\d+$/.test(part)) return { status: "error", message: `“${part}” is not a valid octet — each must be a number 0 to 255.` };
    const n = Number(part);
    if (n > 255) return { status: "error", message: `Octet ${n} is out of range — each must be 0 to 255.` };
    octets.push(n);
  }

  const ip = toInt(octets[0], octets[1], octets[2], octets[3]);
  const mask = maskFor(prefix);
  const wildcard = u32(~mask);
  const network = u32(ip & mask);
  const broadcast = u32(network | wildcard);
  const total = 2 ** (32 - prefix);

  let usable: number;
  let firstHost: number;
  let lastHost: number;
  let note: string | null = null;
  if (prefix <= 30) {
    usable = total - 2;
    firstHost = u32(network + 1);
    lastHost = u32(broadcast - 1);
  } else if (prefix === 31) {
    usable = 2;
    firstHost = network;
    lastHost = broadcast;
    note = "A /31 is a point-to-point link (RFC 3021): both addresses are usable — there is no network or broadcast reservation.";
  } else {
    usable = 1;
    firstHost = network;
    lastHost = network;
    note = "A /32 is a single host — one address, with no usable range, network, or broadcast.";
  }

  return {
    status: "ok",
    data: {
      octets,
      ip,
      prefix,
      mask,
      wildcard,
      network,
      broadcast,
      total,
      usable,
      firstHost,
      lastHost,
      note,
      isPrivate:
        contains(10, 0, 0, 0, 8, network) || contains(172, 16, 0, 0, 12, network) || contains(192, 168, 0, 0, 16, network),
      isLoopback: contains(127, 0, 0, 0, 8, network),
      isLinkLocal: contains(169, 254, 0, 0, 16, network),
      scope: scopeOf(network),
      cls: classOf(octets[0]),
    },
  };
}

// ---- small presentational pieces -----------------------------------------

function Flag({ on, label }: { on: boolean; label: string }) {
  return (
    <div
      className={cn(
        "flex items-center gap-2 rounded-lg border px-3 py-2 text-[13px]",
        on ? "border-emerald-500/30 bg-emerald-500/10 text-fg" : "border-edge bg-bg-subtle text-fg-muted",
      )}
    >
      {on ? <Check className="size-4 shrink-0 text-emerald-500" /> : <Minus className="text-fg-subtle size-4 shrink-0" />}
      <span className={cn("truncate", on && "font-medium")}>{label}</span>
    </div>
  );
}

/** 32 bits, MSB first, network bits in accent with a rule at the prefix boundary. */
function BitRow({ label, value, prefix }: { label: string; value: number; prefix: number }) {
  const bits: number[] = [];
  for (let i = 0; i < 32; i++) bits.push((value >>> (31 - i)) & 1);
  return (
    <div className="flex items-center gap-3">
      <span className="text-fg-subtle w-14 shrink-0 text-[11.5px]">{label}</span>
      <div className="flex items-center overflow-x-auto pb-0.5 font-mono text-[12.5px]">
        {bits.map((b, idx) => (
          <Fragment key={idx}>
            {idx > 0 && idx % 8 === 0 && <span className="text-fg-subtle mx-1 shrink-0">.</span>}
            {prefix > 0 && prefix < 32 && idx === prefix && <span className="bg-accent/70 mx-1 h-4 w-px shrink-0" aria-hidden />}
            <span className={cn("w-3 shrink-0 text-center", idx < prefix ? "text-accent font-semibold" : "text-fg-subtle")}>{b}</span>
          </Fragment>
        ))}
      </div>
    </div>
  );
}

const PREFIXES = Array.from({ length: 33 }, (_, i) => i);
const PRESETS = ["192.168.1.0/24", "10.0.0.0/8", "172.16.5.4/22", "100.64.0.1/10", "169.254.0.0/16", "203.0.113.7/31", "8.8.8.8/32"];

export default function Tool() {
  const [text, setText] = useState("192.168.1.10/24");
  const [prefixState, setPrefixState] = useState(24);

  const calc = useMemo(() => compute(text, prefixState), [text, prefixState]);
  const displayPrefix = slashPrefixOf(text) ?? prefixState;

  const setPrefix = (value: string) => {
    const n = Number(value);
    setPrefixState(n);
    setText((prev) => {
      const t = prev.trim();
      const slash = t.indexOf("/");
      const ipPart = slash === -1 ? t : t.slice(0, slash);
      return `${ipPart}/${n}`;
    });
  };

  const applyPreset = (preset: string) => {
    setText(preset);
    setPrefixState(slashPrefixOf(preset) ?? 24);
  };

  return (
    <div className="space-y-4">
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Network className="text-fg-muted size-4" />
            Address
          </span>
        }
      >
        <div className="flex flex-col gap-3 sm:flex-row sm:items-end">
          <div className="min-w-0 flex-1">
            <Field
              label="IPv4 address or CIDR"
              error={calc.status === "error" ? calc.message : null}
              hint="e.g. 192.168.1.10/24 — or type an address and choose the prefix."
            >
              {(p) => (
                <Input
                  {...p}
                  value={text}
                  onChange={(e) => setText(e.target.value)}
                  placeholder="192.168.1.0/24"
                  spellCheck={false}
                  autoComplete="off"
                  autoCapitalize="off"
                  className="font-mono"
                />
              )}
            </Field>
          </div>
          <div className="sm:w-52">
            <label htmlFor="subnet-prefix" className="text-fg mb-1.5 block text-[13px] font-medium">
              Prefix length
            </label>
            <Select value={String(displayPrefix)} onValueChange={setPrefix}>
              <SelectTrigger id="subnet-prefix">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {PREFIXES.map((n) => (
                  <SelectItem key={n} value={String(n)}>
                    /{n}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </div>

        <div className="mt-3 flex flex-wrap items-center gap-1.5">
          <span className="text-fg-subtle mr-1 text-[12px]">Try</span>
          {PRESETS.map((preset) => (
            <Button key={preset} size="sm" variant="ghost" className="font-mono text-[12px]" onClick={() => applyPreset(preset)}>
              {preset}
            </Button>
          ))}
        </div>
      </ToolCard>

      {calc.status !== "ok" ? (
        <ToolCard>
          <div className="text-fg-muted flex items-center gap-2 text-[13px]">
            {calc.status === "error" ? (
              <AlertTriangle className="size-4 shrink-0 text-amber-400" />
            ) : (
              <Network className="text-fg-subtle size-4 shrink-0" />
            )}
            <span>{calc.status === "error" ? calc.message : "Enter an IPv4 address or CIDR above to see the full breakdown."}</span>
          </div>
        </ToolCard>
      ) : (
        <Results d={calc.data} />
      )}
    </div>
  );
}

function Results({ d }: { d: Detail }) {
  const networkCidr = `${intToIp(d.network)}/${d.prefix}`;
  const summary = [
    ["CIDR", networkCidr],
    ["Network address", intToIp(d.network)],
    ["Broadcast address", intToIp(d.broadcast)],
    ["Netmask", intToIp(d.mask)],
    ["Wildcard mask", intToIp(d.wildcard)],
    ["First host", intToIp(d.firstHost)],
    ["Last host", intToIp(d.lastHost)],
    ["Usable hosts", d.usable.toLocaleString()],
    ["Total addresses", d.total.toLocaleString()],
    ["Class", d.cls.letter],
    ["Scope", d.scope.label],
  ]
    .map(([k, v]) => `${k.padEnd(18)}${v}`)
    .join("\n");

  return (
    <>
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Globe className="text-fg-muted size-4" />
            Network details
          </span>
        }
        right={<CopyIconButton value={summary} label="Copy all details" />}
      >
        <div className="border-edge bg-bg-subtle mb-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border px-4 py-3">
          <div className="min-w-0">
            <div className="flex items-center gap-1.5">
              <span className="text-fg font-mono text-[17px] font-semibold">{networkCidr}</span>
              <CopyIconButton value={networkCidr} label="Copy network CIDR" />
            </div>
            <p className="text-fg-muted mt-0.5 font-mono text-[12px]">
              {intToIp(d.firstHost)} – {intToIp(d.lastHost)}
              <span className="text-fg-subtle">
                {" · "}
                {d.usable.toLocaleString()} usable host{d.usable === 1 ? "" : "s"}
              </span>
            </p>
          </div>
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge tone="accent">Class {d.cls.letter}</Badge>
            <Badge tone={d.scope.tone}>{d.scope.label}</Badge>
          </div>
        </div>

        <div className="grid grid-cols-1 gap-x-8 lg:grid-cols-2">
          <ResultRow label="Network address" value={intToIp(d.network)} />
          <ResultRow label="Broadcast address" value={intToIp(d.broadcast)} />
          <ResultRow label="Netmask" value={intToIp(d.mask)} />
          <ResultRow label="Wildcard mask" value={intToIp(d.wildcard)} />
          <ResultRow label="First usable host" value={intToIp(d.firstHost)} />
          <ResultRow label="Last usable host" value={intToIp(d.lastHost)} />
          <ResultRow label="Total addresses" value={d.total.toLocaleString()} />
          <ResultRow label="Usable hosts" value={d.usable.toLocaleString()} />
          <ResultRow label="Host address" value={intToIp(d.ip)} />
          <ResultRow label="Prefix length" value={`/${d.prefix}`} />
        </div>

        {d.note && <p className="text-fg-muted border-edge mt-3 border-t pt-3 text-[12.5px]">{d.note}</p>}
      </ToolCard>

      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Shield className="text-fg-muted size-4" />
            Scope &amp; class
          </span>
        }
      >
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-3">
          <Flag on={d.isPrivate} label="Private · RFC 1918" />
          <Flag on={d.isLoopback} label="Loopback · 127/8" />
          <Flag on={d.isLinkLocal} label="Link-local · 169.254/16" />
        </div>
        <div className="mt-3 space-y-1 text-[12.5px]">
          <p className="text-fg-muted">
            <span className="text-fg-subtle">Scope&nbsp;&nbsp;</span>
            {d.scope.label}
          </p>
          <p className="text-fg-muted">
            <span className="text-fg-subtle">Class {d.cls.letter}&nbsp;&nbsp;</span>
            {d.cls.desc}
          </p>
        </div>
      </ToolCard>

      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Binary className="text-fg-muted size-4" />
            Binary
          </span>
        }
      >
        <div className="space-y-2">
          <BitRow label="Address" value={d.ip} prefix={d.prefix} />
          <BitRow label="Netmask" value={d.mask} prefix={d.prefix} />
          <BitRow label="Network" value={d.network} prefix={d.prefix} />
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-4 text-[11.5px]">
          <span className="flex items-center gap-1.5">
            <span className="bg-accent inline-block size-2.5 rounded-[2px]" />
            <span className="text-fg-muted">Network bits (/{d.prefix})</span>
          </span>
          <span className="flex items-center gap-1.5">
            <span className="bg-fg-subtle inline-block size-2.5 rounded-[2px]" />
            <span className="text-fg-muted">Host bits ({32 - d.prefix})</span>
          </span>
        </div>
      </ToolCard>
    </>
  );
}
