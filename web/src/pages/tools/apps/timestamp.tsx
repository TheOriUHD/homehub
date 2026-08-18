import { useEffect, useMemo, useState, type ReactNode } from "react";
import { ArrowDown, CalendarClock, Clock, Globe, Hourglass } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge, StatusDot } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { useToast } from "@/ui/toast";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Timestamp converter — Unix epoch ↔ human time, timezone-aware, all
 * client-side. A live "current epoch" header feeds two independent panels:
 * epoch → date (auto-detecting seconds / millis / micros / nanos by magnitude,
 * rendered in Local / UTC / a chosen IANA zone) and date → epoch (a wall time
 * in a chosen zone resolved to an instant, robust across DST via a two-pass
 * offset lookup). Everything is built on the platform Intl APIs — no library —
 * and invalid input surfaces a friendly inline error instead of throwing.
 */

// ── Units ─────────────────────────────────────────────────────────────────
type Unit = "s" | "ms" | "us" | "ns";
type UnitOpt = Unit | "auto";

const UNIT_CHOICES: { id: UnitOpt; label: string }[] = [
  { id: "auto", label: "Auto" },
  { id: "s", label: "Seconds" },
  { id: "ms", label: "Millis" },
  { id: "us", label: "Micros" },
  { id: "ns", label: "Nanos" },
];

const UNIT_NAME: Record<Unit, string> = {
  s: "seconds",
  ms: "milliseconds",
  us: "microseconds",
  ns: "nanoseconds",
};

// JS Date spans ±8.64e15 ms from the epoch (±271,821 years).
const MAX_MS = 8.64e15;

const pad = (n: number) => String(n).padStart(2, "0");

/** Guess the unit of a bare number by its integer-digit count. */
function detectUnit(n: number): Unit {
  const digits = Math.floor(Math.abs(n)).toString().length;
  if (digits <= 11) return "s"; // through ~year 5138 in seconds
  if (digits <= 14) return "ms";
  if (digits <= 17) return "us";
  return "ns";
}

const toMs = (n: number, u: Unit): number =>
  u === "s" ? n * 1000 : u === "ms" ? n : u === "us" ? n / 1000 : n / 1e6;

// ── Timezone maths (Intl only) ─────────────────────────────────────────────

/** Wall-clock fields for an instant as seen in a given IANA zone. */
function zoneParts(tz: string, ms: number) {
  const dtf = new Intl.DateTimeFormat("en-US", {
    timeZone: tz,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
  const m: Record<string, number> = {};
  for (const p of dtf.formatToParts(new Date(ms))) {
    if (p.type !== "literal") m[p.type] = parseInt(p.value, 10);
  }
  return { year: m.year, month: m.month, day: m.day, hour: m.hour, minute: m.minute, second: m.second };
}

/** Offset of `tz` from UTC, in ms, at the given instant (east of UTC positive). */
function offsetMsAt(tz: string, ms: number): number {
  const base = Math.floor(ms / 1000) * 1000; // offsets are whole minutes
  const p = zoneParts(tz, base);
  const asUTC = Date.UTC(p.year, p.month - 1, p.day, p.hour, p.minute, p.second);
  return asUTC - base;
}

/** "+05:30" / "-04:00" — the numeric offset of `tz` at an instant. */
function offsetLabel(tz: string, ms: number): string {
  const off = offsetMsAt(tz, ms);
  const sign = off < 0 ? "-" : "+";
  const a = Math.abs(off);
  return `${sign}${pad(Math.floor(a / 3600000))}:${pad(Math.floor((a % 3600000) / 60000))}`;
}

/** ISO-8601 with the zone's own offset, e.g. 2026-08-18T08:34:56-04:00. */
function isoInZone(tz: string, ms: number): string {
  const p = zoneParts(tz, ms);
  return `${p.year}-${pad(p.month)}-${pad(p.day)}T${pad(p.hour)}:${pad(p.minute)}:${pad(p.second)}${offsetLabel(tz, ms)}`;
}

/** Friendly localized rendering of an instant in a zone (with the zone name). */
function humanInZone(tz: string, ms: number): string {
  return new Intl.DateTimeFormat(undefined, {
    timeZone: tz,
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
    timeZoneName: "short",
  }).format(new Date(ms));
}

/** Same, but appends the numeric offset (skipped for UTC, where it's noise). */
const humanWithOffset = (tz: string, ms: number): string =>
  tz === "UTC" ? humanInZone(tz, ms) : `${humanInZone(tz, ms)} (UTC${offsetLabel(tz, ms)})`;

/**
 * Resolve a wall time (as if in `tz`) to a UTC instant. Two passes so a value
 * near a DST transition converges on the correct offset.
 */
function wallToEpoch(y: number, mo: number, d: number, h: number, mi: number, s: number, tz: string): number {
  const naive = Date.UTC(y, mo - 1, d, h, mi, s);
  const guess = naive - offsetMsAt(tz, naive);
  return naive - offsetMsAt(tz, guess);
}

/** Human "in 3 days" / "2 hours ago", via Intl with a graceful fallback. */
function relative(targetMs: number, nowMs: number): string {
  const diffMs = targetMs - nowMs;
  try {
    const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });
    const s = Math.round(diffMs / 1000);
    const steps: [Intl.RelativeTimeFormatUnit, number][] = [
      ["year", 31536000],
      ["month", 2592000],
      ["week", 604800],
      ["day", 86400],
      ["hour", 3600],
      ["minute", 60],
    ];
    for (const [unit, secs] of steps) {
      if (Math.abs(s) >= secs) return rtf.format(Math.round(s / secs), unit);
    }
    return rtf.format(s, "second");
  } catch {
    const s = Math.round(Math.abs(diffMs) / 1000);
    return diffMs >= 0 ? `in ${s}s` : `${s}s ago`;
  }
}

// ── Timezone list ──────────────────────────────────────────────────────────
const FALLBACK_ZONES = [
  "UTC",
  "America/New_York",
  "America/Chicago",
  "America/Denver",
  "America/Los_Angeles",
  "America/Sao_Paulo",
  "Europe/London",
  "Europe/Paris",
  "Europe/Berlin",
  "Europe/Madrid",
  "Europe/Moscow",
  "Africa/Cairo",
  "Africa/Johannesburg",
  "Asia/Dubai",
  "Asia/Kolkata",
  "Asia/Bangkok",
  "Asia/Shanghai",
  "Asia/Singapore",
  "Asia/Tokyo",
  "Asia/Seoul",
  "Australia/Sydney",
  "Pacific/Auckland",
  "Pacific/Honolulu",
];

const zoneCity = (tz: string) => tz.split("/").pop()!.replace(/_/g, " ");

// ── Epoch → date parsing ───────────────────────────────────────────────────
type AState =
  | { state: "empty" }
  | { state: "error"; message: string }
  | { state: "ok"; ms: number; resolved: Unit };

function computeEpoch(raw: string, unit: UnitOpt): AState {
  const cleaned = raw.replace(/[_,\s]/g, "");
  if (!cleaned) return { state: "empty" };
  if (!/^[+-]?(\d+\.?\d*|\.\d+)$/.test(cleaned)) return { state: "error", message: "Enter a number — a Unix epoch." };
  const n = Number(cleaned);
  if (!Number.isFinite(n)) return { state: "error", message: "That isn't a finite number." };
  const resolved = unit === "auto" ? detectUnit(n) : unit;
  const ms = toMs(n, resolved);
  if (!Number.isFinite(ms) || Math.abs(ms) > MAX_MS)
    return { state: "error", message: "Out of range — a Unix date spans ±271,821 years from 1970." };
  return { state: "ok", ms, resolved };
}

// ── Date → epoch parsing ───────────────────────────────────────────────────
type BState =
  | { state: "incomplete" }
  | { state: "error"; message: string }
  | { state: "ok"; ms: number };

function computeDate(date: string, time: string, tz: string): BState {
  if (!date || !time) return { state: "incomplete" };
  const dm = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  const tm = /^(\d{2}):(\d{2})(?::(\d{2}))?$/.exec(time);
  if (!dm || !tm) return { state: "error", message: "Pick a valid date and time." };
  const y = +dm[1];
  const mo = +dm[2];
  const d = +dm[3];
  const h = +tm[1];
  const mi = +tm[2];
  const s = tm[3] ? +tm[3] : 0;
  if (mo < 1 || mo > 12 || d < 1 || d > new Date(Date.UTC(y, mo, 0)).getUTCDate() || h > 23 || mi > 59 || s > 59)
    return { state: "error", message: "Date or time is out of range." };
  try {
    const ms = wallToEpoch(y, mo, d, h, mi, s, tz);
    if (!Number.isFinite(ms) || Math.abs(ms) > MAX_MS)
      return { state: "error", message: "That date is outside the representable range." };
    return { state: "ok", ms };
  } catch {
    return { state: "error", message: "Couldn't convert that date." };
  }
}

// ── Small building blocks ──────────────────────────────────────────────────
function Labeled({ label, icon: Icon, children }: { label: string; icon?: typeof Globe; children: ReactNode }) {
  return (
    <div className="space-y-1.5">
      <span className="text-fg flex items-center gap-1.5 text-[13px] font-medium">
        {Icon && <Icon className="text-fg-subtle size-3.5" />}
        {label}
      </span>
      {children}
    </div>
  );
}

function ZoneSelect({
  value,
  onChange,
  zones,
  local,
}: {
  value: string;
  onChange: (v: string) => void;
  zones: string[];
  local: string;
}) {
  const pinned = Array.from(new Set([local, "UTC"]));
  const rest = zones.filter((z) => !pinned.includes(z));
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {pinned.map((z) => (
          <SelectItem key={z} value={z}>
            {z === local ? `${zoneCity(z)} — local` : z}
          </SelectItem>
        ))}
        <div className="border-edge my-1 border-t" />
        <p className="text-fg-subtle px-2.5 py-1 text-[10.5px] font-semibold tracking-wide uppercase">All time zones</p>
        {rest.map((z) => (
          <SelectItem key={z} value={z}>
            {z.replace(/_/g, " ")}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

const EMPTY_BOX =
  "text-fg-subtle mt-3 flex items-center justify-center rounded-lg border border-dashed border-edge px-4 py-8 text-center text-[13px]";

// ── Component ──────────────────────────────────────────────────────────────
export default function TimestampTool() {
  const toast = useToast();

  // Local zone + the full IANA list (guarded — the API is absent on old engines,
  // and typed via a cast so it compiles regardless of the TS lib target).
  const { zones, localZone } = useMemo(() => {
    let localZone = "UTC";
    try {
      localZone = Intl.DateTimeFormat().resolvedOptions().timeZone || "UTC";
    } catch {
      /* keep UTC */
    }
    const supported = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf;
    let list: string[] = [];
    if (typeof supported === "function") {
      try {
        list = supported("timeZone");
      } catch {
        /* fall through */
      }
    }
    if (!list.length) list = FALLBACK_ZONES;
    const set = new Set(list);
    set.add("UTC");
    set.add(localZone);
    return { zones: Array.from(set).sort((a, b) => a.localeCompare(b)), localZone };
  }, []);

  // Live clock — drives the header and every relative label.
  const [nowMs, setNowMs] = useState(() => Date.now());
  useEffect(() => {
    const id = setInterval(() => setNowMs(Date.now()), 500);
    return () => clearInterval(id);
  }, []);

  // Section A — epoch → date.
  const [rawEpoch, setRawEpoch] = useState(() => String(Math.floor(Date.now() / 1000)));
  const [unit, setUnit] = useState<UnitOpt>("auto");
  const [tzA, setTzA] = useState(localZone);

  // Section B — date → epoch.
  const [dateStr, setDateStr] = useState(() => {
    const d = new Date();
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  });
  const [timeStr, setTimeStr] = useState(() => {
    const d = new Date();
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  });
  const [tzB, setTzB] = useState(localZone);

  const secA = useMemo(() => computeEpoch(rawEpoch, unit), [rawEpoch, unit]);
  const secB = useMemo(() => computeDate(dateStr, timeStr, tzB), [dateStr, timeStr, tzB]);

  // Heavy Intl formatting only re-runs when the value or its zone changes,
  // never on the 500 ms clock tick.
  const viewA = useMemo(() => {
    if (secA.state !== "ok") return null;
    try {
      const { ms } = secA;
      return {
        seconds: String(Math.floor(ms / 1000)),
        millis: String(Math.round(ms)),
        local: humanWithOffset(localZone, ms),
        utc: humanInZone("UTC", ms),
        zoned: humanWithOffset(tzA, ms),
        isoUtc: new Date(ms).toISOString(),
        isoZoned: isoInZone(tzA, ms),
      };
    } catch {
      return null;
    }
  }, [secA, tzA, localZone]);

  const viewB = useMemo(() => {
    if (secB.state !== "ok") return null;
    try {
      const { ms } = secB;
      return {
        seconds: String(Math.floor(ms / 1000)),
        millis: String(Math.round(ms)),
        local: humanWithOffset(localZone, ms),
        utc: humanInZone("UTC", ms),
        zoned: humanWithOffset(tzB, ms),
        isoUtc: new Date(ms).toISOString(),
        isoZoned: isoInZone(tzB, ms),
      };
    } catch {
      return null;
    }
  }, [secB, tzB, localZone]);

  const nowSeconds = Math.floor(nowMs / 1000);

  const useNow = () => {
    setUnit("auto");
    setRawEpoch(String(Math.floor(Date.now() / 1000)));
    toast("success", "Filled the current epoch into the converter");
  };

  const nowInZoneB = () => {
    const p = zoneParts(tzB, Date.now());
    setDateStr(`${p.year}-${pad(p.month)}-${pad(p.day)}`);
    setTimeStr(`${pad(p.hour)}:${pad(p.minute)}:${pad(p.second)}`);
  };

  return (
    <div className="space-y-4">
      {/* ── Live current epoch ─────────────────────────────────────────── */}
      <ToolCard
        title={
          <span className="flex items-center gap-1.5">
            <Clock className="text-fg-subtle size-4" />
            Current Unix time
          </span>
        }
        right={
          <Badge tone="success">
            <StatusDot tone="success" pulse />
            Live
          </Badge>
        }
      >
        <div className="grid gap-3 sm:grid-cols-2">
          <div className="border-edge bg-bg-subtle rounded-lg border p-3">
            <div className="text-fg-subtle mb-1 text-[10.5px] font-semibold tracking-wide uppercase">Seconds</div>
            <div className="flex items-center gap-2">
              <span className="text-fg flex-1 font-mono text-2xl tabular-nums sm:text-3xl">{nowSeconds}</span>
              <CopyIconButton value={String(nowSeconds)} label="Copy seconds" />
            </div>
          </div>
          <div className="border-edge bg-bg-subtle rounded-lg border p-3">
            <div className="text-fg-subtle mb-1 text-[10.5px] font-semibold tracking-wide uppercase">Milliseconds</div>
            <div className="flex items-center gap-2">
              <span className="text-fg flex-1 font-mono text-2xl tabular-nums sm:text-3xl">{nowMs}</span>
              <CopyIconButton value={String(nowMs)} label="Copy milliseconds" />
            </div>
          </div>
        </div>
        <div className="mt-3 flex flex-wrap items-center gap-x-4 gap-y-1.5">
          <Button size="sm" variant="primary" onClick={useNow}>
            <ArrowDown className="size-3.5" />
            Use now
          </Button>
          <span className="text-fg-subtle text-[12.5px]">
            <span className="text-fg-muted font-medium">Local</span> {humanInZone(localZone, nowMs)}
          </span>
          <span className="text-fg-subtle text-[12.5px]">
            <span className="text-fg-muted font-medium">UTC</span> {humanInZone("UTC", nowMs)}
          </span>
        </div>
      </ToolCard>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {/* ── Section A: epoch → date ──────────────────────────────────── */}
        <ToolCard
          title={
            <span className="flex items-center gap-1.5">
              <Hourglass className="text-fg-subtle size-4" />
              Epoch → date
            </span>
          }
          right={
            secA.state === "ok" ? (
              <Badge tone="accent">{UNIT_NAME[secA.resolved]}</Badge>
            ) : (
              <Badge tone="neutral">epoch</Badge>
            )
          }
        >
          <Field
            label="Unix epoch"
            error={secA.state === "error" ? secA.message : undefined}
            hint={
              secA.state === "error"
                ? undefined
                : unit === "auto"
                  ? "Seconds, millis, micros or nanos — detected by size."
                  : `Interpreting the number as ${UNIT_NAME[unit]}.`
            }
          >
            {(p) => (
              <Input
                {...p}
                value={rawEpoch}
                onChange={(e) => setRawEpoch(e.target.value)}
                inputMode="numeric"
                spellCheck={false}
                autoComplete="off"
                placeholder="1700000000"
                className="font-mono tabular-nums"
              />
            )}
          </Field>

          {/* Unit override — Auto detects, the rest force. */}
          <div className="mt-2.5 flex flex-wrap items-center gap-2">
            <div className="border-edge bg-bg-subtle inline-flex flex-wrap gap-1 rounded-lg border p-1">
              {UNIT_CHOICES.map((u) => (
                <button
                  key={u.id}
                  type="button"
                  onClick={() => setUnit(u.id)}
                  className={cn(
                    "rounded-md px-2.5 py-1 text-[12px] font-medium transition-colors",
                    unit === u.id ? "bg-accent text-accent-fg shadow-1" : "text-fg-muted hover:text-fg",
                  )}
                >
                  {u.label}
                </button>
              ))}
            </div>
            {unit === "auto" && secA.state === "ok" && (
              <span className="text-fg-subtle text-[12px]">→ {UNIT_NAME[secA.resolved]}</span>
            )}
          </div>

          <div className="mt-3">
            <Labeled label="Show in time zone" icon={Globe}>
              <ZoneSelect value={tzA} onChange={setTzA} zones={zones} local={localZone} />
            </Labeled>
          </div>

          {secA.state === "ok" && viewA ? (
            <div className="border-edge bg-bg-subtle/40 mt-3 rounded-lg border px-3 py-1.5">
              <ResultRow label="Unix seconds" value={viewA.seconds} />
              <ResultRow label="Unix milliseconds" value={viewA.millis} />
              <div className="border-edge/70 my-1 border-t" />
              <ResultRow label="Local" value={viewA.local} mono={false} />
              <ResultRow label="UTC" value={viewA.utc} mono={false} />
              <ResultRow label={zoneCity(tzA)} value={viewA.zoned} mono={false} />
              <div className="border-edge/70 my-1 border-t" />
              <ResultRow label="ISO 8601 (UTC)" value={viewA.isoUtc} />
              <ResultRow label="ISO 8601 (zone)" value={viewA.isoZoned} />
              <ResultRow label="Relative" value={relative(secA.ms, nowMs)} mono={false} />
            </div>
          ) : (
            <div className={EMPTY_BOX}>
              {secA.state === "error" ? "Fix the epoch above to see the date." : "Enter a Unix epoch to convert it to a date."}
            </div>
          )}
        </ToolCard>

        {/* ── Section B: date → epoch ──────────────────────────────────── */}
        <ToolCard
          title={
            <span className="flex items-center gap-1.5">
              <CalendarClock className="text-fg-subtle size-4" />
              Date → epoch
            </span>
          }
          right={
            <Button size="sm" variant="ghost" onClick={nowInZoneB}>
              <Clock className="size-3.5" />
              Now
            </Button>
          }
        >
          <div className="grid gap-3 sm:grid-cols-2">
            <Field label="Date">
              {(p) => (
                <Input {...p} type="date" value={dateStr} onChange={(e) => setDateStr(e.target.value)} className="font-mono" />
              )}
            </Field>
            <Field label="Time" hint="24-hour, seconds optional">
              {(p) => (
                <Input
                  {...p}
                  type="time"
                  step={1}
                  value={timeStr}
                  onChange={(e) => setTimeStr(e.target.value)}
                  className="font-mono"
                />
              )}
            </Field>
          </div>

          <div className="mt-3">
            <Labeled label="Interpret in time zone" icon={Globe}>
              <ZoneSelect value={tzB} onChange={setTzB} zones={zones} local={localZone} />
            </Labeled>
          </div>

          {secB.state === "ok" && viewB ? (
            <div className="border-edge bg-bg-subtle/40 mt-3 rounded-lg border px-3 py-1.5">
              <ResultRow label="Unix seconds" value={viewB.seconds} />
              <ResultRow label="Unix milliseconds" value={viewB.millis} />
              <div className="border-edge/70 my-1 border-t" />
              <ResultRow label="ISO 8601 (UTC)" value={viewB.isoUtc} />
              <ResultRow label="ISO 8601 (zone)" value={viewB.isoZoned} />
              <div className="border-edge/70 my-1 border-t" />
              <ResultRow label="Local" value={viewB.local} mono={false} />
              <ResultRow label="UTC" value={viewB.utc} mono={false} />
              <ResultRow label="Relative" value={relative(secB.ms, nowMs)} mono={false} />
            </div>
          ) : (
            <div className={EMPTY_BOX}>
              {secB.state === "error" ? secB.message : "Pick a date, time and zone to get its epoch."}
            </div>
          )}
          <p className="text-fg-subtle mt-2.5 text-[11.5px]">
            The wall time above is read as {zoneCity(tzB)} local time, then resolved to an exact instant.
          </p>
        </ToolCard>
      </div>
    </div>
  );
}
