import { useMemo, useState, type ReactNode } from "react";
import { AlertTriangle, ArrowRight, Film, Gauge, HardDrive, Info, Music, Timer } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Bitrate calculator for video exports (DaVinci Resolve & friends). Two linked
 * modes around one identity — bits = bytes × 8, bitrate = bits ÷ seconds:
 *   • Size → Bitrate: a target file size + duration give the total bitrate,
 *     minus an optional audio track to leave the video bitrate to dial in.
 *   • Bitrate → Size: a video bitrate + duration (+ audio) give the file size.
 * Duration parses H:MM:SS / MM:SS / raw seconds; sizes switch between decimal
 * (MB/GB) and binary (MiB/GiB); bitrates are always SI. Everything is live and
 * every input is validated inline — the component never throws.
 */

const KBPS = 1_000;
const MBPS = 1_000_000;

type Mode = "size2rate" | "rate2size";
type UnitSystem = "decimal" | "binary";
type Magnitude = "M" | "G";
type RateUnit = "Mbps" | "kbps";

const BYTES: Record<UnitSystem, Record<Magnitude, number>> = {
  decimal: { M: 1_000_000, G: 1_000_000_000 },
  binary: { M: 1_048_576, G: 1_073_741_824 },
};
const UNIT_LABEL: Record<UnitSystem, Record<Magnitude, string>> = {
  decimal: { M: "MB", G: "GB" },
  binary: { M: "MiB", G: "GiB" },
};

const AUDIO_PRESETS = [128, 192, 256, 320, 448, 1536];

// ---- parsing --------------------------------------------------------------

const NUM = /^\d*\.?\d+$/;

/** H:MM:SS · MM:SS · SS(.fff) or a plain number of seconds. Lenient: a value
 *  over 59 in a field just carries (colon-joined "1:90" → 150 s). null = junk. */
function parseDuration(raw: string): number | null {
  const s = raw.trim();
  if (!s) return null;
  if (s.includes(":")) {
    const parts = s.split(":");
    if (parts.length > 3) return null;
    let total = 0;
    for (const part of parts) {
      const p = part.trim();
      if (!NUM.test(p)) return null;
      total = total * 60 + Number(p);
    }
    return total;
  }
  return NUM.test(s) ? Number(s) : null;
}

function parseNum(raw: string): number | null {
  const s = raw.trim();
  if (!s || !NUM.test(s)) return null;
  const n = Number(s);
  return Number.isFinite(n) ? n : null;
}

// ---- formatting -----------------------------------------------------------

/** Grouped, human display (thousands separators), capped decimals. */
function fmt(n: number, dp: number): string {
  if (!Number.isFinite(n)) return "—";
  return n.toLocaleString(undefined, { maximumFractionDigits: dp });
}

/** Plain, paste-ready number: no separators, trailing zeros trimmed. */
function plain(n: number, dp: number): string {
  if (!Number.isFinite(n)) return "";
  return String(Math.round(n * 10 ** dp) / 10 ** dp);
}

/** Seconds → H:MM:SS or M:SS, keeping any fractional seconds. */
function fmtClock(sec: number): string {
  let s = Math.round(Math.max(0, sec) * 1000) / 1000;
  const h = Math.floor(s / 3600);
  s -= h * 3600;
  const m = Math.floor(s / 60);
  s -= m * 60;
  const sr = Math.round(s * 1000) / 1000;
  const sStr = (sr < 10 ? "0" : "") + plain(sr, 3);
  return h > 0 ? `${h}:${String(m).padStart(2, "0")}:${sStr}` : `${m}:${sStr}`;
}

function rate(bps: number) {
  return { bps, kbps: bps / KBPS, mbps: bps / MBPS };
}

function sizeBreak(bytes: number) {
  return { bytes, MB: bytes / 1e6, GB: bytes / 1e9, MiB: bytes / 1_048_576, GiB: bytes / 1_073_741_824 };
}

// ---- presentational pieces ------------------------------------------------

function Stat({
  label,
  value,
  unit,
  sub,
  copy,
  tone = "neutral",
  icon,
}: {
  label: string;
  value: string;
  unit?: string;
  sub?: ReactNode;
  copy?: string;
  tone?: "neutral" | "accent";
  icon?: ReactNode;
}) {
  return (
    <div
      className={cn(
        "rounded-lg border p-3",
        tone === "accent" ? "border-accent/40 bg-accent-soft/40" : "border-edge bg-bg-subtle",
      )}
    >
      <div className="text-fg-subtle mb-1.5 flex items-center gap-1.5 text-[10.5px] font-semibold tracking-wide uppercase">
        {icon}
        {label}
      </div>
      <div className="flex items-baseline gap-2">
        <span className="text-fg font-mono text-[26px] leading-none font-semibold tabular-nums">{value}</span>
        {unit && <span className="text-fg-muted text-[13px] font-medium">{unit}</span>}
        <span className="flex-1" />
        {copy !== undefined && <CopyIconButton value={copy} label={`Copy ${label}`} />}
      </div>
      {sub !== undefined && <div className="text-fg-muted mt-1.5 text-[12.5px] tabular-nums">{sub}</div>}
    </div>
  );
}

function Segmented<T extends string>({
  value,
  onChange,
  options,
  size = "md",
}: {
  value: T;
  onChange: (v: T) => void;
  options: { value: T; label: ReactNode }[];
  size?: "sm" | "md";
}) {
  return (
    <div className="border-edge bg-bg-subtle inline-flex gap-1 rounded-lg border p-1">
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          onClick={() => onChange(o.value)}
          className={cn(
            "rounded-md font-medium transition-colors",
            size === "sm" ? "px-2.5 py-1 text-[12px]" : "px-3 py-1.5 text-[13px]",
            value === o.value ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
          )}
        >
          {o.label}
        </button>
      ))}
    </div>
  );
}

// ---- tool -----------------------------------------------------------------

export default function Tool() {
  const [mode, setMode] = useState<Mode>("size2rate");
  const [system, setSystem] = useState<UnitSystem>("decimal");
  const [duration, setDuration] = useState("10:00");
  const [sizeInput, setSizeInput] = useState("1");
  const [sizeMag, setSizeMag] = useState<Magnitude>("G");
  const [rateInput, setRateInput] = useState("20");
  const [rateUnit, setRateUnit] = useState<RateUnit>("Mbps");
  const [audioOn, setAudioOn] = useState(true);
  const [audioKbps, setAudioKbps] = useState("320");

  const seconds = useMemo(() => parseDuration(duration), [duration]);
  const durationErr =
    duration.trim() === ""
      ? null
      : seconds === null
        ? "Use H:MM:SS, MM:SS, or a number of seconds."
        : seconds <= 0
          ? "Duration must be greater than zero."
          : null;

  const audioVal = parseNum(audioKbps);
  const audioErr =
    !audioOn || audioKbps.trim() === ""
      ? null
      : audioVal === null
        ? "Enter the audio bitrate in kbps."
        : audioVal < 0
          ? "Audio bitrate can't be negative."
          : null;
  const audioBps = audioOn && audioVal !== null && audioVal > 0 ? audioVal * KBPS : 0;

  const sizeVal = parseNum(sizeInput);
  const sizeErr =
    mode !== "size2rate" || sizeInput.trim() === ""
      ? null
      : sizeVal === null
        ? "Enter a target file size."
        : sizeVal <= 0
          ? "File size must be greater than zero."
          : null;

  const rateVal = parseNum(rateInput);
  const rateErr =
    mode !== "rate2size" || rateInput.trim() === ""
      ? null
      : rateVal === null
        ? "Enter a bitrate."
        : rateVal <= 0
          ? "Bitrate must be greater than zero."
          : null;

  // ── derived results (null until every required input is valid) ──────────
  let aData: { bytes: number; totalBps: number; videoBps: number; seconds: number; sizeVal: number } | null = null;
  if (mode === "size2rate" && seconds !== null && seconds > 0 && sizeVal !== null && sizeVal > 0 && !audioErr) {
    const bytes = sizeVal * BYTES[system][sizeMag];
    const totalBps = (bytes * 8) / seconds;
    aData = { bytes, totalBps, videoBps: totalBps - audioBps, seconds, sizeVal };
  }

  let bData: { videoBps: number; totalBps: number; bytes: number; seconds: number } | null = null;
  if (mode === "rate2size" && seconds !== null && seconds > 0 && rateVal !== null && rateVal > 0 && !audioErr) {
    const videoBps = rateVal * (rateUnit === "Mbps" ? MBPS : KBPS);
    const totalBps = videoBps + audioBps;
    bData = { videoBps, totalBps, bytes: (totalBps * seconds) / 8, seconds };
  }

  const activeErr = mode === "size2rate" ? sizeErr : rateErr;
  const anyError = Boolean(durationErr || audioErr || activeErr);

  return (
    <div className="space-y-4">
      {/* ── Mode + unit system ─────────────────────────────────────────── */}
      <ToolCard>
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div>
            <Segmented<Mode>
              value={mode}
              onChange={setMode}
              options={[
                {
                  value: "size2rate",
                  label: (
                    <span className="flex items-center gap-1.5">
                      <Gauge className="size-3.5" />
                      Size → Bitrate
                    </span>
                  ),
                },
                {
                  value: "rate2size",
                  label: (
                    <span className="flex items-center gap-1.5">
                      <HardDrive className="size-3.5" />
                      Bitrate → Size
                    </span>
                  ),
                },
              ]}
            />
            <p className="text-fg-subtle mt-2 text-[12.5px]">
              {mode === "size2rate"
                ? "Pick a target file size and clip length to get the bitrate to set in your encoder."
                : "Set a bitrate and clip length to see how large the export will be."}
            </p>
          </div>
          <div className="flex flex-col items-start gap-1.5 sm:items-end">
            <span className="text-fg-subtle text-[11px] font-semibold tracking-wide uppercase">Size units</span>
            <Segmented<UnitSystem>
              value={system}
              onChange={setSystem}
              size="sm"
              options={[
                { value: "decimal", label: "MB · GB" },
                { value: "binary", label: "MiB · GiB" },
              ]}
            />
          </div>
        </div>
      </ToolCard>

      {/* ── Inputs ─────────────────────────────────────────────────────── */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Timer className="text-fg-muted size-4" />
            Inputs
          </span>
        }
      >
        <div className="grid gap-4 sm:grid-cols-2">
          <Field
            label="Duration"
            error={durationErr}
            hint={
              durationErr
                ? undefined
                : seconds !== null && seconds > 0
                  ? `= ${fmtClock(seconds)} · ${plain(seconds, 3)} seconds`
                  : "H:MM:SS, MM:SS, or seconds (e.g. 600)"
            }
          >
            {(p) => (
              <Input
                {...p}
                value={duration}
                onChange={(e) => setDuration(e.target.value)}
                placeholder="10:00"
                spellCheck={false}
                autoComplete="off"
                className="font-mono tabular-nums"
              />
            )}
          </Field>

          {mode === "size2rate" ? (
            <Field
              label="Target file size"
              error={sizeErr}
              hint={sizeErr ? undefined : `${UNIT_LABEL[system][sizeMag]} — ${system} units`}
            >
              {(p) => (
                <div className="flex gap-2">
                  <Input
                    {...p}
                    value={sizeInput}
                    onChange={(e) => setSizeInput(e.target.value)}
                    inputMode="decimal"
                    placeholder="1"
                    spellCheck={false}
                    autoComplete="off"
                    className="font-mono tabular-nums"
                  />
                  <Select value={sizeMag} onValueChange={(v) => setSizeMag(v as Magnitude)}>
                    <SelectTrigger className="w-28 shrink-0">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="M">{UNIT_LABEL[system].M}</SelectItem>
                      <SelectItem value="G">{UNIT_LABEL[system].G}</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            </Field>
          ) : (
            <Field
              label="Video bitrate"
              error={rateErr}
              hint={rateErr ? undefined : "The bitrate you set in the encoder; audio is added below."}
            >
              {(p) => (
                <div className="flex gap-2">
                  <Input
                    {...p}
                    value={rateInput}
                    onChange={(e) => setRateInput(e.target.value)}
                    inputMode="decimal"
                    placeholder="20"
                    spellCheck={false}
                    autoComplete="off"
                    className="font-mono tabular-nums"
                  />
                  <Select value={rateUnit} onValueChange={(v) => setRateUnit(v as RateUnit)}>
                    <SelectTrigger className="w-28 shrink-0">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="Mbps">Mbps</SelectItem>
                      <SelectItem value="kbps">kbps</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              )}
            </Field>
          )}
        </div>

        {/* Audio track — optional, splits total vs video bitrate */}
        <div className="mt-4">
          <div className="flex items-center gap-2.5">
            <Switch id="bitrate-audio" checked={audioOn} onCheckedChange={setAudioOn} />
            <label htmlFor="bitrate-audio" className="text-fg flex cursor-pointer items-center gap-1.5 text-[13px] font-medium">
              <Music className="text-fg-muted size-3.5" />
              Include an audio track
            </label>
          </div>

          {audioOn && (
            <div className="border-edge mt-3 rounded-lg border border-dashed p-3">
              <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
                <div className="w-40">
                  <label htmlFor="bitrate-audio-kbps" className="text-fg mb-1.5 block text-[13px] font-medium">
                    Audio bitrate
                  </label>
                  <div className="relative">
                    <Input
                      id="bitrate-audio-kbps"
                      value={audioKbps}
                      onChange={(e) => setAudioKbps(e.target.value)}
                      inputMode="decimal"
                      placeholder="320"
                      spellCheck={false}
                      autoComplete="off"
                      aria-invalid={audioErr ? true : undefined}
                      className="pr-12 font-mono tabular-nums"
                    />
                    <span className="text-fg-subtle pointer-events-none absolute inset-y-0 right-3 flex items-center text-[12px]">
                      kbps
                    </span>
                  </div>
                </div>
                <div className="flex flex-wrap items-center gap-1.5 pb-1.5">
                  <span className="text-fg-subtle mr-0.5 text-[12px]">Common</span>
                  {AUDIO_PRESETS.map((k) => (
                    <Button
                      key={k}
                      size="sm"
                      variant="ghost"
                      className={cn("font-mono text-[12px]", String(k) === audioKbps.trim() && "text-accent")}
                      onClick={() => setAudioKbps(String(k))}
                    >
                      {k}
                    </Button>
                  ))}
                </div>
              </div>
              {audioErr && <p className="text-danger mt-2 text-[12.5px]">{audioErr}</p>}
              <p className="text-fg-subtle mt-2 text-[12px]">
                AAC ~128–320 · AC-3 384–640 · PCM/WAV 1536 (48 kHz, 16-bit stereo). Bitrates are SI: 1 kbps = 1,000 bit/s.
              </p>
            </div>
          )}
        </div>
      </ToolCard>

      {/* ── Results ────────────────────────────────────────────────────── */}
      {!aData && !bData ? (
        <ToolCard>
          <div className="text-fg-muted flex items-center gap-2 text-[13px]">
            {anyError ? (
              <AlertTriangle className="size-4 shrink-0 text-amber-400" />
            ) : mode === "size2rate" ? (
              <Gauge className="text-fg-subtle size-4 shrink-0" />
            ) : (
              <HardDrive className="text-fg-subtle size-4 shrink-0" />
            )}
            <span>
              {anyError
                ? "Fix the highlighted field above to see the result."
                : mode === "size2rate"
                  ? "Enter a target size and duration to get the bitrate to set."
                  : "Enter a bitrate and duration to get the resulting file size."}
            </span>
          </div>
        </ToolCard>
      ) : aData ? (
        <ResultsSizeToRate
          data={aData}
          system={system}
          sizeMag={sizeMag}
          audioBps={audioBps}
          audioOn={audioOn}
          onSendToSize={(mbps) => {
            setRateUnit("Mbps");
            setRateInput(plain(mbps, 4));
            setMode("rate2size");
          }}
        />
      ) : bData ? (
        <ResultsRateToSize
          data={bData}
          system={system}
          audioBps={audioBps}
          audioOn={audioOn}
          onSendToRate={(bytes) => {
            const mag: Magnitude = bytes >= BYTES[system].G ? "G" : "M";
            setSizeMag(mag);
            setSizeInput(plain(bytes / BYTES[system][mag], 4));
            setMode("size2rate");
          }}
        />
      ) : null}

      {/* ── How it's calculated ────────────────────────────────────────── */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Info className="text-fg-muted size-4" />
            How it&apos;s calculated
          </span>
        }
      >
        <div className="text-fg-muted space-y-1.5 text-[12.5px]">
          <p>
            <span className="text-fg font-mono">bits = bytes × 8</span> and{" "}
            <span className="text-fg font-mono">bitrate = bits ÷ seconds</span> — file size is the total (video + audio)
            bitrate carried across the whole clip. Container overhead is ignored, so real exports land a hair larger.
          </p>
          <p>
            Bitrates always use SI: <span className="text-fg">1 kbps = 1,000 bit/s</span>,{" "}
            <span className="text-fg">1 Mbps = 1,000,000 bit/s</span>. File sizes follow the toggle above — decimal (1 MB =
            1,000,000 B, how macOS and drive makers count) or binary (1 MiB = 1,048,576 B, what Windows shows under the
            &ldquo;MB&rdquo; label).
          </p>
        </div>
      </ToolCard>
    </div>
  );
}

// ---- Size → Bitrate results -----------------------------------------------

function ResultsSizeToRate({
  data,
  system,
  sizeMag,
  audioBps,
  audioOn,
  onSendToSize,
}: {
  data: { bytes: number; totalBps: number; videoBps: number; seconds: number; sizeVal: number };
  system: UnitSystem;
  sizeMag: Magnitude;
  audioBps: number;
  audioOn: boolean;
  onSendToSize: (mbps: number) => void;
}) {
  const total = rate(data.totalBps);
  const video = rate(Math.max(data.videoBps, 0));
  const audio = rate(audioBps);
  const hasAudio = audioOn && audioBps > 0;
  const videoNegative = hasAudio && data.videoBps <= 0;
  const sizeLabel = UNIT_LABEL[system][sizeMag];

  const summary = [
    ["Duration", `${fmtClock(data.seconds)} (${plain(data.seconds, 3)} s)`],
    ["Target size", `${fmt(data.sizeVal, 3)} ${sizeLabel}`],
    ["Total bitrate", `${fmt(total.mbps, 3)} Mbps (${fmt(total.kbps, 0)} kbps)`],
    ...(hasAudio
      ? [
          ["Audio bitrate", `${fmt(audio.kbps, 0)} kbps`],
          ["Video bitrate", `${fmt(video.mbps, 3)} Mbps (${fmt(video.kbps, 0)} kbps)`],
        ]
      : []),
  ]
    .map(([k, v]) => `${k.padEnd(15)}${v}`)
    .join("\n");

  return (
    <ToolCard
      title={
        <span className="flex items-center gap-2">
          <Gauge className="text-fg-muted size-4" />
          Required bitrate
        </span>
      }
      right={
        <div className="flex items-center gap-2">
          <Badge tone="accent">{fmt((hasAudio ? video : total).mbps, 2)} Mbps</Badge>
          <CopyIconButton value={summary} label="Copy summary" />
        </div>
      }
    >
      {hasAudio ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <Stat
            label="Total bitrate"
            icon={<Gauge className="size-3.5" />}
            value={fmt(total.mbps, 3)}
            unit="Mbps"
            sub={`${fmt(total.kbps, 0)} kbps · video + audio`}
            copy={plain(total.mbps, 4)}
          />
          <Stat
            tone="accent"
            label="Video bitrate"
            icon={<Film className="size-3.5" />}
            value={videoNegative ? "0" : fmt(video.mbps, 3)}
            unit="Mbps"
            sub={videoNegative ? "audio alone exceeds the budget" : `${fmt(video.kbps, 0)} kbps · set this in your encoder`}
            copy={plain(video.mbps, 4)}
          />
        </div>
      ) : (
        <Stat
          tone="accent"
          label="Required bitrate"
          icon={<Gauge className="size-3.5" />}
          value={fmt(total.mbps, 3)}
          unit="Mbps"
          sub={`${fmt(total.kbps, 0)} kbps`}
          copy={plain(total.mbps, 4)}
        />
      )}

      {videoNegative && (
        <div className="border-danger/30 bg-danger-soft/40 text-fg mt-3 flex items-start gap-2 rounded-lg border px-3 py-2 text-[12.5px]">
          <AlertTriangle className="text-danger mt-0.5 size-4 shrink-0" />
          <span>
            The audio track ({fmt(audio.kbps, 0)} kbps) meets or exceeds the whole budget — raise the target size or
            duration, or lower the audio bitrate, to leave room for video.
          </span>
        </div>
      )}

      <div className="mt-4 grid grid-cols-1 gap-x-8 lg:grid-cols-2">
        <ResultRow label="Total bitrate (Mbps)" value={plain(total.mbps, 4)} />
        <ResultRow label="Total bitrate (kbps)" value={plain(total.kbps, 2)} />
        {hasAudio && <ResultRow label="Video bitrate (Mbps)" value={plain(Math.max(video.mbps, 0), 4)} />}
        {hasAudio && <ResultRow label="Video bitrate (kbps)" value={plain(Math.max(video.kbps, 0), 2)} />}
        {hasAudio && <ResultRow label="Audio bitrate (kbps)" value={plain(audio.kbps, 2)} />}
        <ResultRow label="Total bits / second" value={plain(total.bps, 0)} />
        <ResultRow label="Target size" value={`${fmt(data.sizeVal, 4)} ${sizeLabel} · ${plain(data.bytes, 0)} bytes`} />
        <ResultRow label="Duration" value={`${fmtClock(data.seconds)} · ${plain(data.seconds, 3)} s`} />
      </div>

      <div className="border-edge mt-4 flex items-center justify-between gap-3 border-t pt-3">
        <p className="text-fg-subtle text-[12px]">
          {hasAudio
            ? "The Video figure is what to type into your encoder; the total already includes the audio track."
            : "Turn on an audio track above to split this into video + audio."}
        </p>
        <Button
          size="sm"
          variant="ghost"
          disabled={videoNegative}
          className="shrink-0"
          onClick={() => onSendToSize(hasAudio ? video.mbps : total.mbps)}
        >
          Check the file size
          <ArrowRight className="size-3.5" />
        </Button>
      </div>
    </ToolCard>
  );
}

// ---- Bitrate → Size results -----------------------------------------------

function ResultsRateToSize({
  data,
  system,
  audioBps,
  audioOn,
  onSendToRate,
}: {
  data: { videoBps: number; totalBps: number; bytes: number; seconds: number };
  system: UnitSystem;
  audioBps: number;
  audioOn: boolean;
  onSendToRate: (bytes: number) => void;
}) {
  const total = rate(data.totalBps);
  const video = rate(data.videoBps);
  const audio = rate(audioBps);
  const hasAudio = audioOn && audioBps > 0;
  const b = sizeBreak(data.bytes);

  const heroMag: Magnitude = data.bytes >= BYTES[system].G ? "G" : "M";
  const heroVal = data.bytes / BYTES[system][heroMag];
  const heroLabel = UNIT_LABEL[system][heroMag];
  const otherSys: UnitSystem = system === "decimal" ? "binary" : "decimal";
  const otherVal = data.bytes / BYTES[otherSys][heroMag];
  const otherLabel = UNIT_LABEL[otherSys][heroMag];

  const summary = [
    ["Duration", `${fmtClock(data.seconds)} (${plain(data.seconds, 3)} s)`],
    ["Video bitrate", `${fmt(video.mbps, 3)} Mbps (${fmt(video.kbps, 0)} kbps)`],
    ...(hasAudio ? [["Audio bitrate", `${fmt(audio.kbps, 0)} kbps`]] : []),
    ["Total bitrate", `${fmt(total.mbps, 3)} Mbps (${fmt(total.kbps, 0)} kbps)`],
    ["File size", `${fmt(b.GB, 3)} GB / ${fmt(b.GiB, 3)} GiB (${plain(b.bytes, 0)} bytes)`],
  ]
    .map(([k, v]) => `${k.padEnd(15)}${v}`)
    .join("\n");

  return (
    <ToolCard
      title={
        <span className="flex items-center gap-2">
          <HardDrive className="text-fg-muted size-4" />
          Resulting file size
        </span>
      }
      right={
        <div className="flex items-center gap-2">
          <Badge tone="accent">
            {fmt(heroVal, 2)} {heroLabel}
          </Badge>
          <CopyIconButton value={summary} label="Copy summary" />
        </div>
      }
    >
      <Stat
        tone="accent"
        label="File size"
        icon={<HardDrive className="size-3.5" />}
        value={fmt(heroVal, 3)}
        unit={heroLabel}
        sub={`${fmt(otherVal, 3)} ${otherLabel} · ${plain(b.bytes, 0)} bytes`}
        copy={plain(heroVal, 4)}
      />

      <div className="border-edge bg-bg-subtle/50 mt-3 flex flex-wrap items-center gap-x-4 gap-y-1 rounded-lg border px-3 py-2 text-[12.5px]">
        <span className="text-fg-muted">
          At <span className="text-fg font-mono">{fmt(total.mbps, 3)} Mbps</span> total
        </span>
        {hasAudio && (
          <span className="text-fg-subtle">
            = video <span className="font-mono">{fmt(video.mbps, 3)}</span> + audio{" "}
            <span className="font-mono">{fmt(audio.kbps, 0)} kbps</span>
          </span>
        )}
        <span className="text-fg-subtle">over {fmtClock(data.seconds)}</span>
      </div>

      <div className="mt-4 grid grid-cols-1 gap-x-8 lg:grid-cols-2">
        <ResultRow label="File size (MB, decimal)" value={plain(b.MB, 3)} />
        <ResultRow label="File size (GB, decimal)" value={plain(b.GB, 4)} />
        <ResultRow label="File size (MiB, binary)" value={plain(b.MiB, 3)} />
        <ResultRow label="File size (GiB, binary)" value={plain(b.GiB, 4)} />
        <ResultRow label="File size (bytes)" value={plain(b.bytes, 0)} />
        <ResultRow label="Total bitrate (Mbps)" value={plain(total.mbps, 4)} />
        <ResultRow label="Total bitrate (kbps)" value={plain(total.kbps, 2)} />
        {hasAudio && <ResultRow label="Video bitrate (Mbps)" value={plain(video.mbps, 4)} />}
        {hasAudio && <ResultRow label="Audio bitrate (kbps)" value={plain(audio.kbps, 2)} />}
        <ResultRow label="Duration" value={`${fmtClock(data.seconds)} · ${plain(data.seconds, 3)} s`} />
      </div>

      <div className="border-edge mt-4 flex items-center justify-between gap-3 border-t pt-3">
        <p className="text-fg-subtle text-[12px]">Sizes shown in both decimal (MB/GB) and binary (MiB/GiB).</p>
        <Button size="sm" variant="ghost" className="shrink-0" onClick={() => onSendToRate(data.bytes)}>
          Use as a target size
          <ArrowRight className="size-3.5" />
        </Button>
      </div>
    </ToolCard>
  );
}
