import { useEffect, useMemo, useState } from "react";
import { Clock, Dices, KeyRound, TriangleAlert, WholeWord } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { Switch } from "@/ui/switch";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Password & passphrase generator — everything is produced locally with
 * `crypto.getRandomValues` (available on insecure LAN origins, unlike
 * `crypto.randomUUID`). Every random choice is drawn with rejection sampling
 * so there is no modulo bias: an index in [0, n) is uniform.
 *
 * Two modes. Password: length 8–64, per-class toggles, optional "avoid
 * ambiguous", and at least one character from every selected class is
 * guaranteed (then the whole thing is shuffled so the guarantees aren't
 * clustered up front). Passphrase: 3–8 words from the embedded list below,
 * a chosen separator, optional capitalisation and a trailing 2-digit number.
 *
 * Entropy is estimated from the *parameters*, not the sampled string, and a
 * strength band + rough offline crack-time are derived from it.
 */

// ---- Character classes ------------------------------------------------------
const LOWER = "abcdefghijklmnopqrstuvwxyz";
const UPPER = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
const DIGITS = "0123456789";
const SYMBOLS = "!@#$%^&*()-_=+[]{};:,.<>?/";
// Glyphs that are easy to confuse in many fonts (uppercase i / lowercase L /
// one, and uppercase O / zero / lowercase o). Removed when "avoid ambiguous".
const AMBIGUOUS = new Set([..."Il1O0o"]);

// ---- Embedded wordlist (150 short, common, family-friendly words) -----------
const WORDS = [
  "apple", "amber", "angel", "ankle", "apron", "arena", "armor", "arrow", "asset", "atlas",
  "audio", "award", "badge", "baker", "banjo", "basil", "beach", "beard", "begin", "berry",
  "birch", "blade", "blaze", "blend", "block", "bloom", "board", "bonus", "boost", "brave",
  "bread", "brick", "brief", "bring", "brook", "brush", "buddy", "bunny", "cabin", "cable",
  "candy", "canoe", "cargo", "carol", "cedar", "chess", "chief", "chill", "cider", "cigar",
  "civic", "claim", "clean", "clear", "cliff", "cloud", "clove", "coach", "coast", "cobra",
  "cocoa", "comet", "coral", "couch", "cover", "craft", "crane", "crate", "cream", "creek",
  "crisp", "crown", "curve", "daisy", "dance", "delta", "depot", "diner", "ditch", "dodge",
  "donor", "dough", "drama", "dream", "dress", "drift", "drill", "drink", "eagle", "early",
  "earth", "ebony", "elbow", "elder", "elect", "elite", "ember", "entry", "equal", "event",
  "extra", "fable", "fancy", "feast", "fence", "ferry", "fever", "fiber", "field", "fifty",
  "final", "finch", "flame", "flash", "fleet", "flint", "flock", "flora", "flour", "focus",
  "forge", "found", "frame", "fresh", "frost", "fruit", "fudge", "funny", "gamma", "gecko",
  "ghost", "giant", "glass", "gleam", "globe", "glory", "glove", "grace", "grain", "grand",
  "grape", "grass", "green", "grove", "guard", "guide", "happy", "heart", "honey", "house",
] as const;

// ---- Unbiased randomness ----------------------------------------------------

/** Uniform integer in [0, n) via rejection sampling on a 32-bit word — no modulo bias. */
function randInt(n: number): number {
  if (n <= 1) return 0;
  const limit = Math.floor(0x1_0000_0000 / n) * n; // largest multiple of n ≤ 2^32
  const buf = new Uint32Array(1);
  let x: number;
  do {
    crypto.getRandomValues(buf);
    x = buf[0];
  } while (x >= limit);
  return x % n;
}

/** A single uniformly-random character from a string. */
const pick = (s: string) => s.charAt(randInt(s.length));

/** In-place Fisher–Yates shuffle using the unbiased sampler. */
function shuffle<T>(arr: T[]): T[] {
  for (let i = arr.length - 1; i > 0; i--) {
    const j = randInt(i + 1);
    [arr[i], arr[j]] = [arr[j], arr[i]];
  }
  return arr;
}

// ---- Generators -------------------------------------------------------------

type PwOptions = {
  length: number;
  lower: boolean;
  upper: boolean;
  digits: boolean;
  symbols: boolean;
  avoidAmbiguous: boolean;
};

const applyAmbiguous = (s: string, avoid: boolean) =>
  avoid ? [...s].filter((c) => !AMBIGUOUS.has(c)).join("") : s;

/** The character classes currently enabled, each already ambiguity-filtered. */
function activeClasses(o: PwOptions): string[] {
  const out: string[] = [];
  if (o.lower) out.push(applyAmbiguous(LOWER, o.avoidAmbiguous));
  if (o.upper) out.push(applyAmbiguous(UPPER, o.avoidAmbiguous));
  if (o.digits) out.push(applyAmbiguous(DIGITS, o.avoidAmbiguous));
  if (o.symbols) out.push(applyAmbiguous(SYMBOLS, o.avoidAmbiguous));
  return out.filter((c) => c.length > 0);
}

function generatePassword(o: PwOptions): string {
  const classes = activeClasses(o);
  if (classes.length === 0) return "";
  const pool = classes.join("");
  const chars: string[] = [];
  // Guarantee one from every selected class (length ≥ 8 ≥ class count, so all fit).
  for (const c of classes) if (chars.length < o.length) chars.push(pick(c));
  while (chars.length < o.length) chars.push(pick(pool));
  return shuffle(chars).join("");
}

type PhOptions = {
  words: number;
  separator: string;
  capitalize: boolean;
  trailingNumber: boolean;
};

const capitalize = (w: string) => w.charAt(0).toUpperCase() + w.slice(1);

function generatePassphrase(o: PhOptions): string {
  const parts: string[] = [];
  for (let i = 0; i < o.words; i++) {
    const w = WORDS[randInt(WORDS.length)];
    parts.push(o.capitalize ? capitalize(w) : w);
  }
  if (o.trailingNumber) parts.push(String(randInt(100)).padStart(2, "0"));
  return parts.join(o.separator);
}

// ---- Entropy, strength & crack-time -----------------------------------------

const NUMBER_ENTROPY = Math.log2(100); // a 2-digit 00–99 tail

type Band = { label: string; tone: "danger" | "warning" | "success" | "accent"; bar: string };

function band(bits: number): Band {
  if (bits < 40) return { label: "Weak", tone: "danger", bar: "bg-red-500" };
  if (bits < 60) return { label: "Fair", tone: "warning", bar: "bg-amber-500" };
  if (bits < 100) return { label: "Strong", tone: "success", bar: "bg-emerald-500" };
  return { label: "Excellent", tone: "accent", bar: "bg-emerald-400" };
}

/** Rough time for a strong offline attacker (10^10 guesses/sec) to find it, on
 *  average (half the keyspace). Returned as a friendly phrase. */
function crackTime(bits: number): string {
  const seconds = Math.pow(2, Math.max(0, bits - 1)) / 1e10;
  if (seconds < 1) return "less than a second";
  const YEAR = 31_557_600;
  if (seconds < 60) return plural(seconds, "second");
  if (seconds < 3600) return plural(seconds / 60, "minute");
  if (seconds < 86_400) return plural(seconds / 3600, "hour");
  if (seconds < YEAR) return plural(seconds / 86_400, "day");
  const years = seconds / YEAR;
  if (years < 1000) return plural(years, "year");
  if (years >= 1e21) return "longer than the universe has existed";
  const scales: [number, string][] = [
    [1e18, "quintillion"],
    [1e15, "quadrillion"],
    [1e12, "trillion"],
    [1e9, "billion"],
    [1e6, "million"],
    [1e3, "thousand"],
  ];
  for (const [v, name] of scales) {
    if (years >= v) {
      const q = years / v;
      const num = q < 10 ? Number(q.toFixed(1)) : Math.round(q);
      return `${num.toLocaleString()} ${name} years`;
    }
  }
  return plural(years, "year");
}

function plural(n: number, unit: string): string {
  const r = Math.round(n);
  return `${r.toLocaleString()} ${unit}${r === 1 ? "" : "s"}`;
}

// ---- Presentation helpers ---------------------------------------------------

/** Colour a character by class so the make-up of the secret is legible. */
function charTone(ch: string): string {
  if (ch === " ") return "";
  if (/[0-9]/.test(ch)) return "text-accent";
  if (/[a-z]/i.test(ch)) return "text-fg";
  return "text-amber-500";
}

const SEPARATORS: { id: string; char: string; label: string }[] = [
  { id: "dash", char: "-", label: "Hyphen  ( - )" },
  { id: "dot", char: ".", label: "Period  ( . )" },
  { id: "space", char: " ", label: "Space  ( ␣ )" },
  { id: "underscore", char: "_", label: "Underscore  ( _ )" },
];

type Mode = "password" | "passphrase";

export default function Tool() {
  const [mode, setMode] = useState<Mode>("password");

  // Password options.
  const [length, setLength] = useState(20);
  const [lower, setLower] = useState(true);
  const [upper, setUpper] = useState(true);
  const [digits, setDigits] = useState(true);
  const [symbols, setSymbols] = useState(true);
  const [avoidAmbiguous, setAvoidAmbiguous] = useState(false);

  // Passphrase options.
  const [words, setWords] = useState(6);
  const [separatorId, setSeparatorId] = useState("dash");
  const [capitalizeWords, setCapitalizeWords] = useState(false);
  const [trailingNumber, setTrailingNumber] = useState(false);

  // A manual "roll" counter — bumping it re-runs the generator without changing
  // any option. Options changing also re-generate (via the effect deps).
  const [nonce, setNonce] = useState(0);
  const [output, setOutput] = useState("");

  const separator = SEPARATORS.find((s) => s.id === separatorId)?.char ?? "-";
  const pwOpts: PwOptions = { length, lower, upper, digits, symbols, avoidAmbiguous };
  const phOpts: PhOptions = { words, separator, capitalize: capitalizeWords, trailingNumber };

  const noClasses = mode === "password" && activeClasses(pwOpts).length === 0;

  // Regenerate whenever any option — or the manual nonce — changes.
  useEffect(() => {
    setOutput(mode === "password" ? generatePassword(pwOpts) : generatePassphrase(phOpts));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    mode, nonce,
    length, lower, upper, digits, symbols, avoidAmbiguous,
    words, separator, capitalizeWords, trailingNumber,
  ]);

  // Entropy is a property of the parameters, independent of the sampled string.
  const { bits, poolSize } = useMemo(() => {
    if (mode === "password") {
      const pool = activeClasses(pwOpts).join("");
      return { bits: pool.length ? length * Math.log2(pool.length) : 0, poolSize: pool.length };
    }
    const b = words * Math.log2(WORDS.length) + (trailingNumber ? NUMBER_ENTROPY : 0);
    return { bits: b, poolSize: WORDS.length };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [mode, length, lower, upper, digits, symbols, avoidAmbiguous, words, trailingNumber]);

  const strength = band(bits);
  const meterPct = Math.min(100, Math.round((bits / 128) * 100));
  const regenerate = () => setNonce((n) => n + 1);

  const composition =
    mode === "password"
      ? `${length} characters from a ${poolSize}-symbol alphabet`
      : `${words} words from a ${WORDS.length}-word list${trailingNumber ? " + a 2-digit number" : ""}${
          capitalizeWords ? ", capitalised" : ""
        }`;

  return (
    <div className="space-y-4">
      {/* Mode selector */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="bg-bg-subtle border-edge inline-flex items-center gap-0.5 rounded-lg border p-0.5">
          {([
            { id: "password", label: "Password", icon: KeyRound },
            { id: "passphrase", label: "Passphrase", icon: WholeWord },
          ] as const).map((m) => {
            const Icon = m.icon;
            return (
              <button
                key={m.id}
                type="button"
                aria-pressed={mode === m.id}
                onClick={() => setMode(m.id)}
                className={cn(
                  "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-3 py-1.5 text-[13px] font-medium transition-colors",
                  mode === m.id ? "bg-surface text-fg shadow-1" : "text-fg-muted hover:text-fg",
                )}
              >
                <Icon className="size-3.5" />
                {m.label}
              </button>
            );
          })}
        </div>
        <span className="text-fg-subtle text-[12.5px]">
          Generated locally with a cryptographic RNG — nothing leaves your device.
        </span>
      </div>

      {/* Result hero */}
      <ToolCard
        title={mode === "password" ? "Your password" : "Your passphrase"}
        right={!noClasses && output ? <CopyIconButton value={output} label="Copy secret" /> : undefined}
      >
        {noClasses ? (
          <div className="border-danger/40 bg-danger-soft/40 flex items-start gap-2 rounded-lg border p-3">
            <TriangleAlert className="text-danger mt-0.5 size-4 shrink-0" />
            <span className="text-danger text-[13px]">
              Select at least one character type below to generate a password.
            </span>
          </div>
        ) : (
          <div
            className={cn(
              "border-edge bg-bg-subtle/50 min-h-[64px] rounded-lg border p-4 font-mono text-xl leading-relaxed select-all sm:text-2xl",
              mode === "password" ? "break-all tracking-[0.01em]" : "break-words",
            )}
          >
            {[...output].map((ch, i) => (
              <span key={i} className={charTone(ch)}>
                {ch}
              </span>
            ))}
          </div>
        )}

        {/* Strength + regenerate */}
        <div className="mt-3.5 space-y-2.5">
          <div className="flex flex-wrap items-center gap-3">
            <Button variant="primary" size="sm" onClick={regenerate} disabled={noClasses}>
              <Dices className="size-3.5" />
              Regenerate
            </Button>
            <span className="flex-1" />
            <Badge tone={noClasses ? "neutral" : strength.tone}>{noClasses ? "—" : strength.label}</Badge>
            <span className="text-fg-muted text-[12.5px] tabular-nums">
              {noClasses ? "0" : Math.round(bits)} bits of entropy
            </span>
          </div>

          <div className="bg-bg-subtle h-2 w-full overflow-hidden rounded-full">
            <div
              className={cn("h-full rounded-full transition-[width] duration-300", noClasses ? "bg-transparent" : strength.bar)}
              style={{ width: `${noClasses ? 0 : Math.max(3, meterPct)}%` }}
            />
          </div>

          {!noClasses && (
            <div className="text-fg-subtle flex flex-wrap items-center gap-x-4 gap-y-1 text-[12px]">
              <span className="inline-flex items-center gap-1.5">
                <Clock className="size-3.5" />
                Offline crack time: <span className="text-fg-muted font-medium">{crackTime(bits)}</span>
              </span>
              <span>{composition}</span>
            </div>
          )}
        </div>
      </ToolCard>

      {/* Options */}
      <ToolCard title="Options">
        {mode === "password" ? (
          <div className="space-y-5">
            {/* Length */}
            <div>
              <div className="flex items-baseline justify-between">
                <label htmlFor="pw-length" className="text-fg text-[13px] font-medium">
                  Length
                </label>
                <span className="text-fg font-mono text-lg tabular-nums">{length}</span>
              </div>
              <input
                id="pw-length"
                type="range"
                min={8}
                max={64}
                value={length}
                onChange={(e) => setLength(Number(e.target.value))}
                className="text-accent accent-current mt-2 h-6 w-full cursor-pointer"
              />
              <div className="text-fg-subtle flex justify-between text-[11px] tabular-nums">
                <span>8</span>
                <span>64</span>
              </div>
            </div>

            {/* Character classes */}
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <ToggleRow label="Lowercase" sample="a–z" checked={lower} onCheckedChange={setLower} />
              <ToggleRow label="Uppercase" sample="A–Z" checked={upper} onCheckedChange={setUpper} />
              <ToggleRow label="Digits" sample="0–9" checked={digits} onCheckedChange={setDigits} />
              <ToggleRow label="Symbols" sample="!@#$%" checked={symbols} onCheckedChange={setSymbols} />
              <ToggleRow
                className="sm:col-span-2"
                label="Avoid ambiguous characters"
                sample="no I l 1 O 0 o"
                checked={avoidAmbiguous}
                onCheckedChange={setAvoidAmbiguous}
              />
            </div>

            <p className="text-fg-subtle text-[12px]">
              At least one character from each selected type is guaranteed, then the result is shuffled.
            </p>
          </div>
        ) : (
          <div className="space-y-5">
            {/* Word count */}
            <div>
              <div className="flex items-baseline justify-between">
                <label htmlFor="ph-words" className="text-fg text-[13px] font-medium">
                  Words
                </label>
                <span className="text-fg font-mono text-lg tabular-nums">{words}</span>
              </div>
              <input
                id="ph-words"
                type="range"
                min={3}
                max={8}
                value={words}
                onChange={(e) => setWords(Number(e.target.value))}
                className="text-accent accent-current mt-2 h-6 w-full cursor-pointer"
              />
              <div className="text-fg-subtle flex justify-between text-[11px] tabular-nums">
                <span>3</span>
                <span>8</span>
              </div>
            </div>

            {/* Separator */}
            <Field label="Separator">
              {(p) => (
                <Select value={separatorId} onValueChange={setSeparatorId}>
                  <SelectTrigger {...p}>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SEPARATORS.map((s) => (
                      <SelectItem key={s.id} value={s.id}>
                        {s.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              )}
            </Field>

            {/* Toggles */}
            <div className="grid grid-cols-1 gap-2 sm:grid-cols-2">
              <ToggleRow
                label="Capitalise each word"
                sample="Correct-Horse"
                checked={capitalizeWords}
                onCheckedChange={setCapitalizeWords}
              />
              <ToggleRow
                label="Append a number"
                sample="…-42"
                checked={trailingNumber}
                onCheckedChange={setTrailingNumber}
              />
            </div>

            <p className="text-fg-subtle text-[12px]">
              Capitalising is deterministic, so it adds no entropy — length comes from adding more words.
            </p>
          </div>
        )}
      </ToolCard>
    </div>
  );
}

/** A compact bordered switch row with a label and an example glyph set. */
function ToggleRow({
  label,
  sample,
  checked,
  onCheckedChange,
  className,
}: {
  label: string;
  sample: string;
  checked: boolean;
  onCheckedChange: (v: boolean) => void;
  className?: string;
}) {
  return (
    <label
      className={cn(
        "border-edge bg-bg-subtle/40 flex cursor-pointer items-center justify-between gap-3 rounded-lg border px-3 py-2 transition-colors",
        checked && "border-edge-strong bg-bg-subtle",
        className,
      )}
    >
      <span className="min-w-0">
        <span className="text-fg block text-[13px] font-medium">{label}</span>
        <span className="text-fg-subtle block font-mono text-[11.5px]">{sample}</span>
      </span>
      <Switch checked={checked} onCheckedChange={onCheckedChange} />
    </label>
  );
}
