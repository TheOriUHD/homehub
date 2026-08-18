import { useMemo, useState, type ReactNode } from "react";
import { Calculator, Clock, Film, Hash, Info, Minus, Plus } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Timecode calculator — SMPTE HH:MM:SS:FF ⇆ frame counts at eight frame rates,
 * with correct 29.97 / 59.94 drop-frame handling. All maths is exact integer
 * arithmetic, no library.
 *
 * Timecode counts frames at an INTEGER timebase (the "nominal" rate: 24 for
 * 23.976, 30 for 29.97, 60 for 59.94). Drop-frame keeps that 30/60-per-second
 * label clock in step with the fractional real rate by renumbering — skipping
 * the first two (four at 59.94) frame LABELS at the top of every minute except
 * every tenth. No frames are lost; the forward/​reverse formulas here add those
 * skipped labels back so both directions round-trip. Verified by exhaustively
 * round-tripping every frame across two hours at both drop rates.
 */

// ── Frame-rate table ────────────────────────────────────────────────────────
type FpsId = "23.976" | "24" | "25" | "29.97" | "30" | "50" | "59.94" | "60";

interface FpsSpec {
  id: FpsId;
  label: string;
  /** Real frames per wall-clock second — drives duration. */
  actual: number;
  /** Integer timecode base — the count the FF field runs to. */
  nominal: number;
  /** Drop-frame is defined only for the 29.97 / 59.94 families. */
  dropCapable: boolean;
}

const FPS: Record<FpsId, FpsSpec> = {
  "23.976": { id: "23.976", label: "23.976", actual: 24000 / 1001, nominal: 24, dropCapable: false },
  "24": { id: "24", label: "24", actual: 24, nominal: 24, dropCapable: false },
  "25": { id: "25", label: "25", actual: 25, nominal: 25, dropCapable: false },
  "29.97": { id: "29.97", label: "29.97", actual: 30000 / 1001, nominal: 30, dropCapable: true },
  "30": { id: "30", label: "30", actual: 30, nominal: 30, dropCapable: false },
  "50": { id: "50", label: "50", actual: 50, nominal: 50, dropCapable: false },
  "59.94": { id: "59.94", label: "59.94", actual: 60000 / 1001, nominal: 60, dropCapable: true },
  "60": { id: "60", label: "60", actual: 60, nominal: 60, dropCapable: false },
};

const FPS_ORDER: FpsId[] = ["23.976", "24", "25", "29.97", "30", "50", "59.94", "60"];

/** Frames dropped per dropping-minute: 2 at 29.97, 4 at 59.94. */
const dropPerMinute = (nominal: number) => (nominal / 30) * 2;

// ── Core maths ──────────────────────────────────────────────────────────────
interface TC {
  hh: number;
  mm: number;
  ss: number;
  ff: number;
}

/** Timecode → non-negative frame index. `tc` must be non-negative. */
function tcToFrames(tc: TC, spec: FpsSpec, drop: boolean): number {
  const { nominal } = spec;
  const naive = nominal * (3600 * tc.hh + 60 * tc.mm + tc.ss) + tc.ff;
  if (!drop) return naive;
  const dc = dropPerMinute(nominal);
  const totalMinutes = 60 * tc.hh + tc.mm;
  return naive - dc * (totalMinutes - Math.floor(totalMinutes / 10));
}

/** Frame index → timecode. `n` must be non-negative (sign is the caller's job). */
function framesToTc(n: number, spec: FpsSpec, drop: boolean): TC {
  const { nominal } = spec;
  let label: number;
  if (!drop) {
    label = n;
  } else {
    const dc = dropPerMinute(nominal);
    const per10Min = nominal * 600 - 9 * dc; // real frames in a 10-minute block
    const perMin = nominal * 60 - dc; // real frames in a dropping minute
    const blocks = Math.floor(n / per10Min);
    const within = n - blocks * per10Min;
    // The first minute of each block is exempt (no drops); later minutes each
    // skip `dc` labels. Add every skipped label back to recover the label index.
    const dropped = within < nominal * 60 ? 0 : dc * (Math.floor((within - nominal * 60) / perMin) + 1);
    label = n + blocks * 9 * dc + dropped;
  }
  const ff = label % nominal;
  const totalSec = Math.floor(label / nominal);
  return { hh: Math.floor(totalSec / 3600), mm: Math.floor(totalSec / 60) % 60, ss: totalSec % 60, ff };
}

const pad2 = (n: number) => String(Math.abs(n)).padStart(2, "0");

/** Render a timecode with the right separator (`;` before FF for drop-frame). */
function fmtTc(tc: TC, drop: boolean, neg = false): string {
  return `${neg ? "-" : ""}${pad2(tc.hh)}:${pad2(tc.mm)}:${pad2(tc.ss)}${drop ? ";" : ":"}${pad2(tc.ff)}`;
}

interface ParseResult {
  ok: boolean;
  tc?: TC;
  neg?: boolean;
  /** Set when a drop-frame entry landed on a skipped label and was nudged up. */
  warn?: string;
  error?: string;
}

/**
 * Parse flexible timecode text. Accepts `:`, `;`, `.` or `,` separators and a
 * leading `-`. Shorter entries right-align to the frames field (film/NLE
 * convention), so "59:29" means 00:00:59:29. Ranges are validated and
 * impossible drop-frame labels are corrected to the next existing frame.
 */
function parseTc(raw: string, spec: FpsSpec, drop: boolean): ParseResult {
  let body = raw.trim();
  if (!body) return { ok: false, error: "Enter a timecode." };
  let neg = false;
  if (body.startsWith("-")) {
    neg = true;
    body = body.slice(1).trim();
  }
  const parts = body.split(/[:;.,\s]+/).filter(Boolean);
  if (parts.length === 0) return { ok: false, error: "Enter a timecode like HH:MM:SS:FF." };
  if (parts.length > 4) return { ok: false, error: "Too many fields — use HH:MM:SS:FF." };
  if (parts.some((p) => !/^\d+$/.test(p))) return { ok: false, error: "Fields must be whole numbers." };

  const nums = parts.map((p) => parseInt(p, 10));
  while (nums.length < 4) nums.unshift(0); // right-align to frames
  const [hh, mm, ss, ff] = nums;

  if (ff >= spec.nominal)
    return { ok: false, error: `Frames must be 0–${spec.nominal - 1} at ${spec.label} fps.` };
  if (ss > 59) return { ok: false, error: "Seconds must be 0–59." };
  if (mm > 59) return { ok: false, error: "Minutes must be 0–59." };
  if (!Number.isSafeInteger(spec.nominal * 3600 * hh)) return { ok: false, error: "That timecode is too large." };

  let tc: TC = { hh, mm, ss, ff };
  let warn: string | undefined;
  if (drop) {
    const dc = dropPerMinute(spec.nominal);
    if (ss === 0 && mm % 10 !== 0 && ff < dc) {
      // These labels are skipped in drop-frame — snap to the first real frame.
      tc = { hh, mm, ss, ff: dc };
      warn = `Frames 00–${pad2(dc - 1)} are skipped at the top of most minutes in drop-frame — nudged to ${fmtTc(tc, true)}.`;
    }
  }
  return { ok: true, tc, neg, warn };
}

/** Real-time duration as H:MM:SS.mmm, sign-aware and rounding-safe. */
function fmtDuration(seconds: number): string {
  const neg = seconds < 0;
  let ms = Math.round(Math.abs(seconds) * 1000);
  const h = Math.floor(ms / 3_600_000);
  ms -= h * 3_600_000;
  const m = Math.floor(ms / 60_000);
  ms -= m * 60_000;
  const s = Math.floor(ms / 1000);
  ms -= s * 1000;
  return `${neg ? "-" : ""}${pad2(h)}:${pad2(m)}:${pad2(s)}.${String(ms).padStart(3, "0")}`;
}

const fmtSeconds = (s: number) => `${s.toFixed(3)} s`;

// ── Small presentational helpers ────────────────────────────────────────────
function Headline({ label, value, tone = "fg" }: { label: string; value: string; tone?: "fg" | "accent" }) {
  return (
    <div className="border-edge bg-bg-subtle rounded-lg border p-3">
      <div className="text-fg-subtle mb-1 text-[10.5px] font-semibold tracking-wide uppercase">{label}</div>
      <div className="flex items-center gap-2">
        <span
          className={cn(
            "min-w-0 flex-1 truncate font-mono text-2xl tabular-nums sm:text-[26px]",
            tone === "accent" ? "text-accent" : "text-fg",
          )}
        >
          {value}
        </span>
        <CopyIconButton value={value} label={`Copy ${label.toLowerCase()}`} />
      </div>
    </div>
  );
}

function ErrorNote({ children }: { children: ReactNode }) {
  return (
    <div className="text-fg-subtle border-edge mt-3 flex items-center justify-center rounded-lg border border-dashed px-4 py-6 text-center text-[13px]">
      {children}
    </div>
  );
}

function TitleWith({ icon: Icon, children }: { icon: typeof Clock; children: ReactNode }) {
  return (
    <span className="flex items-center gap-1.5">
      <Icon className="text-fg-subtle size-4" />
      {children}
    </span>
  );
}

// ── Component ────────────────────────────────────────────────────────────────
export default function TimecodeTool() {
  const [fpsId, setFpsId] = useState<FpsId>("29.97");
  const [dropWanted, setDropWanted] = useState(true);

  const [tcA, setTcA] = useState("01:00:00;00");
  const [framesB, setFramesB] = useState("1800");
  const [tcC1, setTcC1] = useState("00:10:00;00");
  const [tcC2, setTcC2] = useState("00:02:30;00");
  const [op, setOp] = useState<"+" | "-">("+");

  const spec = FPS[fpsId];
  const drop = spec.dropCapable && dropWanted;
  const sep = drop ? ";" : ":";
  const tcPlaceholder = `01:00:00${sep}00`;

  // How far the label clock drifts from real time when unaided (fractional,
  // non-drop rates only — integer rates and drop-frame stay in sync).
  const driftPerHour = useMemo(() => {
    if (drop || spec.actual === spec.nominal) return 0;
    return 3600 * (spec.nominal / spec.actual - 1); // seconds real time gains per TC hour
  }, [drop, spec]);

  // (a) timecode → frames
  const a = useMemo(() => {
    const r = parseTc(tcA, spec, drop);
    if (!r.ok || !r.tc) return { ok: false as const, error: r.error ?? "Invalid timecode." };
    const n = (r.neg ? -1 : 1) * tcToFrames(r.tc, spec, drop);
    return {
      ok: true as const,
      frames: n,
      canon: fmtTc(r.tc, drop, r.neg),
      secs: n / spec.actual,
      warn: r.warn,
    };
  }, [tcA, spec, drop]);

  // (b) frames → timecode
  const b = useMemo(() => {
    const s = framesB.trim();
    if (!s) return { ok: false as const, error: "Enter a frame count." };
    if (!/^[+-]?\d+$/.test(s)) return { ok: false as const, error: "Frames must be a whole number." };
    const n = parseInt(s, 10);
    if (!Number.isSafeInteger(n)) return { ok: false as const, error: "That number is too large." };
    return { ok: true as const, tc: fmtTc(framesToTc(Math.abs(n), spec, drop), drop, n < 0), secs: n / spec.actual, frames: n };
  }, [framesB, spec, drop]);

  // (c) timecode ± timecode
  const c = useMemo(() => {
    const r1 = parseTc(tcC1, spec, drop);
    const r2 = parseTc(tcC2, spec, drop);
    if (!r1.ok || !r1.tc) return { ok: false as const, error: `First timecode: ${r1.error}` };
    if (!r2.ok || !r2.tc) return { ok: false as const, error: `Second timecode: ${r2.error}` };
    const n1 = (r1.neg ? -1 : 1) * tcToFrames(r1.tc, spec, drop);
    const n2 = (r2.neg ? -1 : 1) * tcToFrames(r2.tc, spec, drop);
    const n = op === "+" ? n1 + n2 : n1 - n2;
    return {
      ok: true as const,
      frames: n,
      tc: fmtTc(framesToTc(Math.abs(n), spec, drop), drop, n < 0),
      secs: n / spec.actual,
      a: fmtTc(r1.tc, drop, r1.neg),
      b: fmtTc(r2.tc, drop, r2.neg),
      n1,
      n2,
      warn: r1.warn ?? r2.warn,
    };
  }, [tcC1, tcC2, op, spec, drop]);

  return (
    <div className="space-y-4">
      {/* ── Frame rate & format ─────────────────────────────────────────── */}
      <ToolCard
        title={<TitleWith icon={Film}>Frame rate &amp; format</TitleWith>}
        right={
          <Badge tone={drop ? "warning" : "accent"}>
            {spec.label} fps · {drop ? "drop-frame" : "non-drop"}
          </Badge>
        }
      >
        <div className="grid items-start gap-4 sm:grid-cols-2">
          <Field label="Frames per second" hint={`Timecode base ${spec.nominal} · frames 00–${pad2(spec.nominal - 1)}`}>
            {(p) => (
              <Select value={fpsId} onValueChange={(v) => setFpsId(v as FpsId)}>
                <SelectTrigger id={p.id} aria-describedby={p["aria-describedby"]}>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {FPS_ORDER.map((id) => (
                    <SelectItem key={id} value={id}>
                      {FPS[id].label} fps{FPS[id].dropCapable ? "  ·  drop-capable" : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
          </Field>

          <div className="space-y-1.5">
            <span className="text-fg text-[13px] font-medium">Drop-frame</span>
            <div
              className={cn(
                "border-edge bg-surface flex h-8.5 items-center gap-3 rounded-md border px-3",
                !spec.dropCapable && "opacity-55",
              )}
            >
              <Switch
                checked={drop}
                disabled={!spec.dropCapable}
                onCheckedChange={setDropWanted}
                aria-label="Drop-frame timecode"
              />
              <span className="text-fg-muted min-w-0 flex-1 truncate text-[12.5px]">
                {spec.dropCapable
                  ? drop
                    ? `Separator “;” · skips ${dropPerMinute(spec.nominal)}/min`
                    : "Off — labels run “:” continuously"
                  : `Only for 29.97 & 59.94 fps`}
              </span>
            </div>
          </div>
        </div>

        <div className="border-edge bg-bg-subtle/50 text-fg-muted mt-3 flex gap-2.5 rounded-lg border p-3 text-[12.5px] leading-relaxed">
          <Info className="text-fg-subtle mt-px size-4 shrink-0" />
          <p>
            <span className="text-fg font-medium">Drop-frame</span> keeps the {spec.dropCapable ? spec.nominal : 30}-per-second
            label clock in step with the fractional real rate. It skips two frame <em>numbers</em> (four at 59.94) — e.g.{" "}
            <span className="font-mono">;00</span> and <span className="font-mono">;01</span> — at the start of every minute{" "}
            <em>except</em> every tenth. No frames are lost, only labels renumbered, written with a semicolon:{" "}
            <span className="text-fg font-mono">00:01:00;02</span>.
            {driftPerHour > 0 && (
              <>
                {" "}
                At {spec.label} fps non-drop the clock runs about{" "}
                <span className="text-fg font-medium">{driftPerHour.toFixed(1)} s/hour</span> behind real time.
              </>
            )}
          </p>
        </div>
      </ToolCard>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {/* ── (a) timecode → frames ─────────────────────────────────────── */}
        <ToolCard title={<TitleWith icon={Clock}>Timecode → frames</TitleWith>}>
          <Field
            label="Timecode"
            error={a.ok ? undefined : a.error}
            hint={a.ok ? "HH:MM:SS:FF — shorter entries right-align to frames" : undefined}
          >
            {(p) => (
              <Input
                {...p}
                value={tcA}
                onChange={(e) => setTcA(e.target.value)}
                placeholder={tcPlaceholder}
                spellCheck={false}
                autoComplete="off"
                className="font-mono tabular-nums"
              />
            )}
          </Field>

          {a.ok ? (
            <div className="mt-3 space-y-3">
              <Headline label="Total frames" value={String(a.frames)} tone="accent" />
              {a.warn && <p className="text-[12px] leading-snug text-amber-500">{a.warn}</p>}
              <div className="border-edge bg-bg-subtle/40 rounded-lg border px-3 py-1.5">
                <ResultRow label="Normalized" value={a.canon} />
                <ResultRow label="Duration (real)" value={fmtDuration(a.secs)} />
                <ResultRow label="Seconds (real)" value={fmtSeconds(a.secs)} />
              </div>
            </div>
          ) : (
            <ErrorNote>Enter a timecode to count its frames.</ErrorNote>
          )}
        </ToolCard>

        {/* ── (b) frames → timecode ─────────────────────────────────────── */}
        <ToolCard title={<TitleWith icon={Hash}>Frames → timecode</TitleWith>}>
          <Field
            label="Frame count"
            error={b.ok ? undefined : b.error}
            hint={b.ok ? "Whole number of frames — negatives allowed" : undefined}
          >
            {(p) => (
              <Input
                {...p}
                value={framesB}
                onChange={(e) => setFramesB(e.target.value)}
                inputMode="numeric"
                placeholder="1800"
                spellCheck={false}
                autoComplete="off"
                className="font-mono tabular-nums"
              />
            )}
          </Field>

          {b.ok ? (
            <div className="mt-3 space-y-3">
              <Headline label="Timecode" value={b.tc} tone="accent" />
              <div className="border-edge bg-bg-subtle/40 rounded-lg border px-3 py-1.5">
                <ResultRow label="Duration (real)" value={fmtDuration(b.secs)} />
                <ResultRow label="Seconds (real)" value={fmtSeconds(b.secs)} />
                <ResultRow label="Frames" value={String(b.frames)} />
              </div>
            </div>
          ) : (
            <ErrorNote>Enter a frame count to build its timecode.</ErrorNote>
          )}
        </ToolCard>
      </div>

      {/* ── (c) add / subtract ──────────────────────────────────────────── */}
      <ToolCard title={<TitleWith icon={Calculator}>Add / subtract timecodes</TitleWith>}>
        <div className="grid items-end gap-3 sm:grid-cols-[1fr_auto_1fr]">
          <Field label="Timecode A">
            {(p) => (
              <Input
                {...p}
                value={tcC1}
                onChange={(e) => setTcC1(e.target.value)}
                placeholder={tcPlaceholder}
                spellCheck={false}
                autoComplete="off"
                className="font-mono tabular-nums"
              />
            )}
          </Field>

          <div className="border-edge bg-bg-subtle inline-flex gap-1 self-end rounded-lg border p-1">
            {(["+", "-"] as const).map((o) => (
              <button
                key={o}
                type="button"
                onClick={() => setOp(o)}
                aria-pressed={op === o}
                aria-label={o === "+" ? "Add" : "Subtract"}
                className={cn(
                  "flex size-8 items-center justify-center rounded-md transition-colors",
                  op === o ? "bg-accent text-accent-fg shadow-1" : "text-fg-muted hover:text-fg",
                )}
              >
                {o === "+" ? <Plus className="size-4" /> : <Minus className="size-4" />}
              </button>
            ))}
          </div>

          <Field label="Timecode B">
            {(p) => (
              <Input
                {...p}
                value={tcC2}
                onChange={(e) => setTcC2(e.target.value)}
                placeholder={tcPlaceholder}
                spellCheck={false}
                autoComplete="off"
                className="font-mono tabular-nums"
              />
            )}
          </Field>
        </div>

        {c.ok ? (
          <div className="mt-3 space-y-3">
            {c.warn && <p className="text-[12px] leading-snug text-amber-500">{c.warn}</p>}
            <div className="grid gap-3 sm:grid-cols-2">
              <Headline label={`Result (${c.a} ${op} ${c.b})`} value={c.tc} tone="accent" />
              <Headline label="Result frames" value={String(c.frames)} />
            </div>
            <div className="border-edge bg-bg-subtle/40 rounded-lg border px-3 py-1.5">
              <ResultRow label="Duration (real)" value={fmtDuration(c.secs)} />
              <ResultRow label="Seconds (real)" value={fmtSeconds(c.secs)} />
              <ResultRow label={`A · ${c.a}`} value={`${c.n1} frames`} />
              <ResultRow label={`B · ${c.b}`} value={`${c.n2} frames`} />
            </div>
            {c.frames < 0 && (
              <p className="text-fg-subtle text-[12px]">
                A is earlier than B, so the difference is negative (shown with a leading minus).
              </p>
            )}
          </div>
        ) : (
          <ErrorNote>{c.error}</ErrorNote>
        )}
      </ToolCard>
    </div>
  );
}
