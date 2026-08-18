import { useMemo, useState, type CSSProperties } from "react";
import { Blend, Check, Contrast, Hash, Palette, Pipette, Shuffle, Sliders, X } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Input } from "@/ui/input";
import { CopyIconButton, ResultRow, ToolCard } from "../kit";

/**
 * Color converter — a native picker plus fully two-way HEX / RGBA / HSLA text
 * fields. Everything is pure browser math (no libraries): all conversions are
 * hand-written and reversible, editing any field updates the picker and every
 * other field live, and invalid / partial input is simply ignored rather than
 * throwing.
 *
 * The single source of truth is `rgba` (0–255 channels + 0–1 alpha). A parallel
 * `hsl` is kept in step so the H/S/L inputs stay stable across achromatic
 * colors — when a color goes gray or hits pure black/white the previous hue and
 * saturation are preserved instead of collapsing to 0, which is the usual
 * annoyance with naive rgb↔hsl round-tripping.
 */

// ---------------------------------------------------------------------------
// Types + math
// ---------------------------------------------------------------------------

type RGBA = { r: number; g: number; b: number; a: number };
type HSL = { h: number; s: number; l: number };

const clamp = (n: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, n));

/** One byte as two lowercase hex digits (rounded + clamped to 0–255). */
const hex2 = (n: number) => clamp(Math.round(n), 0, 255).toString(16).padStart(2, "0");

const toHex = ({ r, g, b }: RGBA) => `#${hex2(r)}${hex2(g)}${hex2(b)}`;
const toHex8 = ({ r, g, b, a }: RGBA) => `#${hex2(r)}${hex2(g)}${hex2(b)}${hex2(a * 255)}`;

/** Trim alpha to at most 3 decimals with no trailing zeros: 1, 0.5, 0.333. */
const fmtA = (a: number) => String(Number(clamp(a, 0, 1).toFixed(3)));

const cssRgba = ({ r, g, b, a }: RGBA) =>
  `rgba(${Math.round(r)}, ${Math.round(g)}, ${Math.round(b)}, ${fmtA(a)})`;

/** Parse #rgb / #rgba / #rrggbb / #rrggbbaa (leading # optional). null if invalid. */
function parseHex(input: string): RGBA | null {
  const s = input.trim().replace(/^#/, "");
  if (!/^[0-9a-fA-F]+$/.test(s)) return null;
  const dbl = (h: string) => parseInt(h + h, 16);
  if (s.length === 3) return { r: dbl(s[0]), g: dbl(s[1]), b: dbl(s[2]), a: 1 };
  if (s.length === 4) return { r: dbl(s[0]), g: dbl(s[1]), b: dbl(s[2]), a: dbl(s[3]) / 255 };
  if (s.length === 6)
    return { r: parseInt(s.slice(0, 2), 16), g: parseInt(s.slice(2, 4), 16), b: parseInt(s.slice(4, 6), 16), a: 1 };
  if (s.length === 8)
    return {
      r: parseInt(s.slice(0, 2), 16),
      g: parseInt(s.slice(2, 4), 16),
      b: parseInt(s.slice(4, 6), 16),
      a: parseInt(s.slice(6, 8), 16) / 255,
    };
  return null;
}

/** rgb (0–255) → hsl (h 0–360, s/l 0–100). */
function rgbToHsl(r: number, g: number, b: number): HSL {
  r /= 255;
  g /= 255;
  b /= 255;
  const mx = Math.max(r, g, b);
  const mn = Math.min(r, g, b);
  const l = (mx + mn) / 2;
  const d = mx - mn;
  let h = 0;
  let s = 0;
  if (d !== 0) {
    s = l > 0.5 ? d / (2 - mx - mn) : d / (mx + mn);
    switch (mx) {
      case r:
        h = (g - b) / d + (g < b ? 6 : 0);
        break;
      case g:
        h = (b - r) / d + 2;
        break;
      default:
        h = (r - g) / d + 4;
    }
    h /= 6;
  }
  return { h: h * 360, s: s * 100, l: l * 100 };
}

/** hsl → rgb (0–255, rounded). Hue wraps, s/l clamp. */
function hslToRgb(h: number, s: number, l: number): { r: number; g: number; b: number } {
  h = (((h % 360) + 360) % 360) / 360;
  s = clamp(s, 0, 100) / 100;
  l = clamp(l, 0, 100) / 100;
  if (s === 0) {
    const v = Math.round(l * 255);
    return { r: v, g: v, b: v };
  }
  const q = l < 0.5 ? l * (1 + s) : l + s - l * s;
  const p = 2 * l - q;
  const f = (t: number) => {
    if (t < 0) t += 1;
    if (t > 1) t -= 1;
    if (t < 1 / 6) return p + (q - p) * 6 * t;
    if (t < 1 / 2) return q;
    if (t < 2 / 3) return p + (q - p) * (2 / 3 - t) * 6;
    return p;
  };
  return { r: Math.round(f(h + 1 / 3) * 255), g: Math.round(f(h) * 255), b: Math.round(f(h - 1 / 3) * 255) };
}

/** WCAG relative luminance for an sRGB triple (0–255). */
function relLuminance(r: number, g: number, b: number): number {
  const lin = (c: number) => {
    const x = c / 255;
    return x <= 0.03928 ? x / 12.92 : ((x + 0.055) / 1.055) ** 2.4;
  };
  return 0.2126 * lin(r) + 0.7152 * lin(g) + 0.0722 * lin(b);
}

const mix = (c1: { r: number; g: number; b: number }, c2: { r: number; g: number; b: number }, t: number) => ({
  r: c1.r + (c2.r - c1.r) * t,
  g: c1.g + (c2.g - c1.g) * t,
  b: c1.b + (c2.b - c1.b) * t,
});

/** Darker shades (mix→black) then the base then lighter tints (mix→white). */
function buildScale(R: number, G: number, B: number) {
  const base = { r: R, g: G, b: B };
  const step = (c: { r: number; g: number; b: number }, isBase = false) => {
    const r = Math.round(c.r);
    const g = Math.round(c.g);
    const b = Math.round(c.b);
    return { r, g, b, hex: toHex({ r, g, b, a: 1 }), base: isBase };
  };
  const out: ReturnType<typeof step>[] = [];
  for (let i = 5; i >= 1; i--) out.push(step(mix(base, { r: 0, g: 0, b: 0 }, i / 6)));
  out.push(step(base, true));
  for (let i = 1; i <= 5; i++) out.push(step(mix(base, { r: 255, g: 255, b: 255 }, i / 6)));
  return out;
}

// ---------------------------------------------------------------------------
// EyeDropper (Chromium-only, progressive enhancement)
// ---------------------------------------------------------------------------

type EyeDropperCtor = new () => { open: () => Promise<{ sRGBHex: string }> };
const getEyeDropper = (): EyeDropperCtor | undefined =>
  typeof window === "undefined" ? undefined : (window as unknown as { EyeDropper?: EyeDropperCtor }).EyeDropper;

// ---------------------------------------------------------------------------
// Presentational bits
// ---------------------------------------------------------------------------

/** A tiled checkerboard so alpha reads clearly behind any swatch. */
const CHECKER: CSSProperties = {
  backgroundColor: "#ffffff",
  backgroundImage:
    "linear-gradient(45deg, rgba(0,0,0,.16) 25%, transparent 25%)," +
    "linear-gradient(-45deg, rgba(0,0,0,.16) 25%, transparent 25%)," +
    "linear-gradient(45deg, transparent 75%, rgba(0,0,0,.16) 75%)," +
    "linear-gradient(-45deg, transparent 75%, rgba(0,0,0,.16) 75%)",
  backgroundSize: "14px 14px",
  backgroundPosition: "0 0, 0 7px, 7px -7px, -7px 0",
};

/** A single editable numeric channel: local draft while focused, live-syncs
 *  from canonical otherwise, commits only valid clamped values, arrow keys
 *  nudge (×10 with Shift). */
function Chan({
  label,
  value,
  min,
  max,
  step = 1,
  format,
  onCommit,
}: {
  label: string;
  value: number;
  min: number;
  max: number;
  step?: number;
  format?: (n: number) => string;
  onCommit: (n: number) => void;
}) {
  const fmt = format ?? ((n: number) => String(Math.round(n)));
  const [draft, setDraft] = useState<string | null>(null);
  const shown = draft ?? fmt(value);
  const num = (raw: string) => (step < 1 ? parseFloat(raw) : parseInt(raw, 10));
  const commit = (raw: string) => {
    if (raw.trim() === "") return;
    const n = num(raw);
    if (!Number.isFinite(n)) return;
    onCommit(clamp(n, min, max));
  };
  const bump = (dir: number, big: boolean) => {
    const from = Number.isFinite(num(shown)) ? num(shown) : value;
    const next = clamp(from + dir * step * (big ? 10 : 1), min, max);
    const rounded = step < 1 ? Number(next.toFixed(3)) : Math.round(next);
    setDraft(fmt(rounded));
    onCommit(rounded);
  };
  return (
    <div>
      <label className="text-fg-subtle mb-1 block text-center text-[11px] font-semibold tracking-wide uppercase">
        {label}
      </label>
      <Input
        value={shown}
        inputMode={step < 1 ? "decimal" : "numeric"}
        spellCheck={false}
        autoComplete="off"
        aria-label={label}
        className="px-1.5 text-center font-mono tabular-nums"
        onFocus={() => setDraft(fmt(value))}
        onChange={(e) => {
          setDraft(e.target.value);
          commit(e.target.value);
        }}
        onBlur={() => setDraft(null)}
        onKeyDown={(e) => {
          if (e.key === "ArrowUp") {
            e.preventDefault();
            bump(1, e.shiftKey);
          } else if (e.key === "ArrowDown") {
            e.preventDefault();
            bump(-1, e.shiftKey);
          }
        }}
      />
    </div>
  );
}

/** The one hex input — accepts every shorthand, reflects alpha as HEX8. */
function HexField({ rgba, onApply }: { rgba: RGBA; onApply: (c: RGBA) => void }) {
  const canonical = rgba.a < 1 ? toHex8(rgba) : toHex(rgba);
  const [draft, setDraft] = useState<string | null>(null);
  return (
    <Input
      value={draft ?? canonical}
      spellCheck={false}
      autoComplete="off"
      autoCapitalize="off"
      aria-label="Hex color"
      className="font-mono"
      onFocus={() => setDraft(canonical)}
      onChange={(e) => {
        setDraft(e.target.value);
        const p = parseHex(e.target.value);
        if (p) onApply(p);
      }}
      onBlur={() => setDraft(null)}
    />
  );
}

function Pass({ ok, label }: { ok: boolean; label: string }) {
  return (
    <Badge tone={ok ? "success" : "danger"} className={cn(!ok && "opacity-60")}>
      {ok ? <Check className="size-3" /> : <X className="size-3" />}
      {label}
    </Badge>
  );
}

function ContrastLine({ label, bg, color, ratio }: { label: string; bg: string; color: string; ratio: number }) {
  return (
    <div className="border-edge flex flex-wrap items-center gap-3 rounded-lg border p-3">
      <div
        className="flex size-11 shrink-0 items-center justify-center rounded-md text-base font-bold"
        style={{ background: bg, color }}
      >
        Aa
      </div>
      <div>
        <div className="text-fg-muted text-[12px]">{label}</div>
        <div className="text-fg font-mono text-[15px] font-semibold tabular-nums">
          {ratio.toFixed(2)}
          <span className="text-fg-subtle">:1</span>
        </div>
      </div>
      <span className="flex-1" />
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        <Pass ok={ratio >= 3} label="AA Large" />
        <Pass ok={ratio >= 4.5} label="AA" />
        <Pass ok={ratio >= 7} label="AAA" />
      </div>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Root
// ---------------------------------------------------------------------------

export default function Tool() {
  const [rgba, setRgba] = useState<RGBA>({ r: 59, g: 130, b: 246, a: 1 });
  const [hsl, setHsl] = useState<HSL>(() => rgbToHsl(59, 130, 246));

  // rgb is canonical; keep hsl in step but preserve hue/sat through gray & the
  // black/white extremes so the H/S sliders don't snap to 0.
  const applyRgb = (next: RGBA) => {
    setRgba(next);
    setHsl((prev) => {
      const c = rgbToHsl(next.r, next.g, next.b);
      return {
        h: c.s < 0.01 ? prev.h : c.h,
        s: c.l < 0.01 || c.l > 99.99 ? prev.s : c.s,
        l: c.l,
      };
    });
  };
  const applyHsl = (next: HSL) => {
    setHsl(next);
    setRgba((prev) => ({ ...hslToRgb(next.h, next.s, next.l), a: prev.a }));
  };
  const setAlpha = (a: number) => setRgba((prev) => ({ ...prev, a: clamp(a, 0, 1) }));

  const R = Math.round(rgba.r);
  const G = Math.round(rgba.g);
  const B = Math.round(rgba.b);
  const H = Math.round(hsl.h);
  const S = Math.round(hsl.s);
  const L = Math.round(hsl.l);

  const hex = toHex(rgba);
  const hex8 = toHex8(rgba);
  const hexLabel = rgba.a < 1 ? hex8 : hex;
  const solid = `rgb(${R}, ${G}, ${B})`;
  const rgbStr = solid;
  const rgbaStr = `rgba(${R}, ${G}, ${B}, ${fmtA(rgba.a)})`;
  const hslStr = `hsl(${H}, ${S}%, ${L}%)`;
  const hslaStr = `hsla(${H}, ${S}%, ${L}%, ${fmtA(rgba.a)})`;
  const allFormats = [hex, hex8, rgbStr, rgbaStr, hslStr, hslaStr].join("\n");

  const lum = relLuminance(R, G, B);
  const cWhite = 1.05 / (lum + 0.05);
  const cBlack = (lum + 0.05) / 0.05;
  const onColor =
    relLuminance(R * rgba.a + 255 * (1 - rgba.a), G * rgba.a + 255 * (1 - rgba.a), B * rgba.a + 255 * (1 - rgba.a)) > 0.4
      ? "#0b0b0c"
      : "#ffffff";

  const scale = useMemo(() => buildScale(R, G, B), [R, G, B]);
  const scaleCopy = scale.map((s) => s.hex).join("\n");

  const EyeDropper = getEyeDropper();
  const pickEyedropper = async () => {
    if (!EyeDropper) return;
    try {
      const res = await new EyeDropper().open();
      const p = parseHex(res.sRGBHex);
      if (p) applyRgb({ ...p, a: rgba.a });
    } catch {
      /* the user dismissed the eyedropper — nothing to do */
    }
  };
  const randomize = () => {
    const buf = new Uint8Array(3);
    crypto.getRandomValues(buf);
    applyRgb({ r: buf[0], g: buf[1], b: buf[2], a: rgba.a });
  };

  return (
    <div className="space-y-4">
      {/* Picker + swatch --------------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Palette className="text-fg-muted size-4" />
            Color
          </span>
        }
        right={
          <div className="flex items-center gap-1.5">
            {EyeDropper && (
              <Button size="sm" variant="ghost" onClick={pickEyedropper} title="Pick a color from anywhere on screen">
                <Pipette className="size-3.5" />
                Pick
              </Button>
            )}
            <Button size="sm" variant="ghost" onClick={randomize} title="Random color">
              <Shuffle className="size-3.5" />
              Random
            </Button>
          </div>
        }
      >
        <div className="grid gap-4 sm:grid-cols-[minmax(0,14rem)_1fr]">
          {/* Large swatch over a checkerboard */}
          <div className="border-edge relative h-40 overflow-hidden rounded-xl border" style={CHECKER}>
            <div className="absolute inset-0" style={{ backgroundColor: cssRgba(rgba) }} />
            <div
              className="absolute inset-x-0 top-0 flex h-14 items-center justify-center text-[26px] font-bold"
              style={{ color: onColor }}
            >
              Aa
            </div>
            <div className="absolute bottom-2 left-2 flex items-center gap-0.5 rounded-md bg-black/45 py-0.5 pr-0.5 pl-2 backdrop-blur-sm">
              <span className="font-mono text-[12.5px] font-semibold text-white">{hexLabel}</span>
              <CopyIconButton value={hexLabel} label="Copy hex" className="text-white/80 hover:bg-white/15 hover:text-white" />
            </div>
          </div>

          {/* Native picker + alpha */}
          <div className="flex flex-col justify-center gap-3">
            <div>
              <label htmlFor="hh-picker" className="text-fg-subtle mb-1.5 block text-[12px] font-medium">
                Picker
              </label>
              <input
                id="hh-picker"
                type="color"
                value={hex}
                onChange={(e) => {
                  const p = parseHex(e.target.value);
                  if (p) applyRgb({ ...p, a: rgba.a });
                }}
                className="border-edge bg-surface h-10 w-full cursor-pointer rounded-md border p-1"
                aria-label="Native color picker"
              />
            </div>
            <div>
              <div className="mb-1.5 flex items-center justify-between">
                <label htmlFor="hh-alpha" className="text-fg-subtle text-[12px] font-medium">
                  Alpha
                </label>
                <span className="text-fg-muted font-mono text-[12px] tabular-nums">{fmtA(rgba.a)}</span>
              </div>
              <div className="border-edge relative h-8 overflow-hidden rounded-md border" style={CHECKER}>
                <div
                  className="absolute inset-0"
                  style={{ background: `linear-gradient(to right, ${cssRgba({ ...rgba, a: 0 })}, ${solid})` }}
                />
                <input
                  id="hh-alpha"
                  type="range"
                  min={0}
                  max={1}
                  step={0.01}
                  value={rgba.a}
                  onChange={(e) => setAlpha(parseFloat(e.target.value))}
                  className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
                  aria-label="Alpha"
                />
                <div
                  className="pointer-events-none absolute top-1/2 size-4 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-white shadow ring-1 ring-black/40"
                  style={{ left: `calc(${rgba.a} * (100% - 1rem) + 0.5rem)`, backgroundColor: solid }}
                />
              </div>
            </div>
          </div>
        </div>
      </ToolCard>

      {/* Editable values -------------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Sliders className="text-fg-muted size-4" />
            Edit values
          </span>
        }
      >
        <div className="space-y-4">
          <div>
            <label className="text-fg-subtle mb-1.5 block text-[11px] font-semibold tracking-wide uppercase">Hex</label>
            <HexField rgba={rgba} onApply={applyRgb} />
            <p className="text-fg-subtle mt-1 text-[11.5px]">
              Accepts <span className="font-mono">#rgb</span>, <span className="font-mono">#rgba</span>,{" "}
              <span className="font-mono">#rrggbb</span> or <span className="font-mono">#rrggbbaa</span> — “#” optional.
            </p>
          </div>

          <div>
            <label className="text-fg-subtle mb-1.5 block text-[11px] font-semibold tracking-wide uppercase">RGBA</label>
            <div className="grid grid-cols-4 gap-2">
              <Chan label="R" value={rgba.r} min={0} max={255} onCommit={(n) => applyRgb({ ...rgba, r: n })} />
              <Chan label="G" value={rgba.g} min={0} max={255} onCommit={(n) => applyRgb({ ...rgba, g: n })} />
              <Chan label="B" value={rgba.b} min={0} max={255} onCommit={(n) => applyRgb({ ...rgba, b: n })} />
              <Chan label="A" value={rgba.a} min={0} max={1} step={0.01} format={fmtA} onCommit={setAlpha} />
            </div>
          </div>

          <div>
            <label className="text-fg-subtle mb-1.5 block text-[11px] font-semibold tracking-wide uppercase">HSLA</label>
            <div className="grid grid-cols-4 gap-2">
              <Chan label="H" value={hsl.h} min={0} max={360} onCommit={(n) => applyHsl({ ...hsl, h: n })} />
              <Chan label="S" value={hsl.s} min={0} max={100} onCommit={(n) => applyHsl({ ...hsl, s: n })} />
              <Chan label="L" value={hsl.l} min={0} max={100} onCommit={(n) => applyHsl({ ...hsl, l: n })} />
              <Chan label="A" value={rgba.a} min={0} max={1} step={0.01} format={fmtA} onCommit={setAlpha} />
            </div>
          </div>
        </div>
      </ToolCard>

      {/* CSS output strings ----------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Hash className="text-fg-muted size-4" />
            CSS formats
          </span>
        }
        right={<CopyIconButton value={allFormats} label="Copy all formats" />}
      >
        <div className="grid grid-cols-1 gap-x-8 sm:grid-cols-2">
          <ResultRow label="HEX" value={hex} />
          <ResultRow label="HEX8 · with alpha" value={hex8} />
          <ResultRow label="rgb()" value={rgbStr} />
          <ResultRow label="rgba()" value={rgbaStr} />
          <ResultRow label="hsl()" value={hslStr} />
          <ResultRow label="hsla()" value={hslaStr} />
        </div>
      </ToolCard>

      {/* Contrast + luminance --------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Contrast className="text-fg-muted size-4" />
            Contrast &amp; legibility
          </span>
        }
      >
        <div className="mb-3">
          <ResultRow label="Relative luminance" value={lum.toFixed(4)} />
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          <ContrastLine label="On white" bg="#ffffff" color={solid} ratio={cWhite} />
          <ContrastLine label="On black" bg="#000000" color={solid} ratio={cBlack} />
        </div>
        <p className="text-fg-subtle mt-3 text-[11.5px] leading-relaxed">
          WCAG 2.1 — normal text needs 4.5:1 (AA) or 7:1 (AAA); large text needs 3:1 (AA Large). The ratio uses the
          opaque color, so alpha is not factored in.
        </p>
      </ToolCard>

      {/* Tints & shades --------------------------------------------------- */}
      <ToolCard
        title={
          <span className="flex items-center gap-2">
            <Blend className="text-fg-muted size-4" />
            Tints &amp; shades
          </span>
        }
        right={<CopyIconButton value={scaleCopy} label="Copy all swatches" />}
      >
        <div className="flex gap-2 overflow-x-auto pb-1">
          {scale.map((s, i) => (
            <button
              key={i}
              type="button"
              onClick={() => applyRgb({ r: s.r, g: s.g, b: s.b, a: rgba.a })}
              title={`${s.hex} — click to use`}
              className="group flex shrink-0 cursor-pointer flex-col items-center gap-1"
            >
              <span
                className={cn(
                  "size-12 rounded-md border transition-transform group-hover:scale-105",
                  s.base ? "border-accent ring-accent/40 ring-2" : "border-edge/60",
                )}
                style={{ backgroundColor: s.hex }}
              />
              <span className={cn("font-mono text-[10px]", s.base ? "text-fg font-semibold" : "text-fg-subtle")}>
                {s.hex}
              </span>
            </button>
          ))}
        </div>
        <p className="text-fg-subtle mt-2 text-[11.5px]">
          Mixed toward black (shades) and white (tints). Click any swatch to make it the current color.
        </p>
      </ToolCard>
    </div>
  );
}
