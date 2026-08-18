import { useEffect, useMemo, useState, type ReactNode } from "react";
import cronstrue from "cronstrue";
import { CronExpressionParser } from "cron-parser";
import { AlertCircle, CheckCircle2, Clock, Info, Wand2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { Badge } from "@/ui/badge";
import { Button } from "@/ui/button";
import { Field } from "@/ui/field";
import { Input } from "@/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/ui/select";
import { CopyIconButton, ToolCard } from "../kit";

/**
 * Cron expression builder — a raw 5-field expression with a live plain-English
 * summary (cronstrue) and the next five local run times (cron-parser v5), plus
 * a preset builder that composes common schedules. Invalid input never throws:
 * both libraries are wrapped and surface a friendly inline error instead.
 */

const FIELD_LABELS = ["Minute", "Hour", "Day (month)", "Month", "Day (week)"] as const;
const HOURS = Array.from({ length: 24 }, (_, i) => i);
const MINUTES = Array.from({ length: 60 }, (_, i) => i);
const DOMS = Array.from({ length: 31 }, (_, i) => i + 1);
const WEEKDAYS = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

const QUICK: { label: string; value: string }[] = [
  { label: "Every minute", value: "* * * * *" },
  { label: "Every 5 minutes", value: "*/5 * * * *" },
  { label: "Every 15 minutes", value: "*/15 * * * *" },
  { label: "Every 30 minutes", value: "*/30 * * * *" },
  { label: "Every hour", value: "0 * * * *" },
];

const pad = (n: number | string) => String(n).padStart(2, "0");

type Parsed =
  | { state: "empty" }
  | { state: "error"; message: string }
  | { state: "ok"; human: string | null; runs: Date[] };

function parse(expr: string): Parsed {
  const value = expr.trim();
  if (!value) return { state: "empty" };

  // Human summary is best-effort — a valid schedule can still fail to render.
  let human: string | null = null;
  try {
    human = cronstrue.toString(value, { use24HourTimeFormat: true, throwExceptionOnParseError: true });
  } catch {
    human = null;
  }

  // cron-parser is authoritative for validity: if it can't schedule it, it's invalid.
  try {
    const it = CronExpressionParser.parse(value);
    const runs: Date[] = [];
    for (let i = 0; i < 5; i++) runs.push(it.next().toDate());
    return { state: "ok", human, runs };
  } catch (e) {
    const raw = e instanceof Error ? e.message : String(e);
    return { state: "error", message: raw.replace(/^Error:\s*/i, "") || "Invalid cron expression." };
  }
}

function relative(target: Date, now: number): string {
  let s = Math.round((target.getTime() - now) / 1000);
  if (s < 0) s = 0;
  if (s < 60) return `in ${s}s`;
  const m = Math.round(s / 60);
  if (m < 60) return `in ${m} min`;
  const h = Math.floor(m / 60);
  const mr = m % 60;
  if (h < 24) return mr ? `in ${h} hr ${mr} min` : `in ${h} hr`;
  const d = Math.floor(h / 24);
  const hr = h % 24;
  return hr ? `in ${d}d ${hr}h` : `in ${d}d`;
}

const absLabel = (d: Date) =>
  d.toLocaleString(undefined, {
    weekday: "short",
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });

function PickerSelect({
  label,
  value,
  onChange,
  width = "w-20",
  children,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  width?: string;
  children: ReactNode;
}) {
  return (
    <label className="block">
      <span className="text-fg-subtle mb-1 block text-[10.5px] font-semibold tracking-wide uppercase">{label}</span>
      <div className={width}>
        <Select value={value} onValueChange={onChange}>
          <SelectTrigger className="h-8">
            <SelectValue />
          </SelectTrigger>
          <SelectContent>{children}</SelectContent>
        </Select>
      </div>
    </label>
  );
}

export default function CronTool() {
  const [expr, setExpr] = useState("*/5 * * * *");
  const [hour, setHour] = useState("9");
  const [minute, setMinute] = useState("0");
  const [dow, setDow] = useState("1");
  const [dom, setDom] = useState("1");
  const [now, setNow] = useState(() => Date.now());

  // Advance "now" every 30s so the schedule re-seeds and the relative "in …" labels stay honest.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30_000);
    return () => clearInterval(id);
  }, []);

  const result = useMemo(() => parse(expr), [expr, now]);
  const parts = result.state === "ok" ? expr.trim().split(/\s+/) : [];

  const time = `${pad(hour)}:${pad(minute)}`;
  const dailyExpr = `${minute} ${hour} * * *`;
  const weeklyExpr = `${minute} ${hour} * * ${dow}`;
  const monthlyExpr = `${minute} ${hour} ${dom} * *`;

  const validity =
    result.state === "ok"
      ? { tone: "success" as const, label: "Valid", icon: CheckCircle2 }
      : result.state === "error"
        ? { tone: "danger" as const, label: "Invalid", icon: AlertCircle }
        : { tone: "neutral" as const, label: "Empty", icon: Info };

  return (
    <div className="space-y-4">
      {/* ── Expression ─────────────────────────────────────────────── */}
      <ToolCard
        title="Expression"
        right={
          <div className="flex items-center gap-1.5">
            <Badge tone={validity.tone}>
              <validity.icon className="size-3" />
              {validity.label}
            </Badge>
            <CopyIconButton value={expr} label="Copy expression" />
          </div>
        }
      >
        <Field
          label="Cron expression"
          hint={result.state === "error" ? undefined : "Five fields: minute · hour · day-of-month · month · day-of-week"}
          error={result.state === "error" ? result.message : undefined}
        >
          {(p) => (
            <Input
              {...p}
              value={expr}
              onChange={(e) => setExpr(e.target.value)}
              spellCheck={false}
              autoComplete="off"
              autoCapitalize="off"
              placeholder="*/5 * * * *"
              className="font-mono tracking-wide"
            />
          )}
        </Field>

        {/* Field breakdown — decompose a well-formed expression token by token. */}
        {parts.length === 5 && (
          <div className="mt-3 grid grid-cols-5 gap-2">
            {parts.map((tok, i) => (
              <div key={i} className="border-edge bg-bg-subtle rounded-lg border px-2 py-1.5 text-center">
                <div className="text-fg-subtle truncate text-[10px] font-medium tracking-wide uppercase">
                  {FIELD_LABELS[i]}
                </div>
                <div className={cn("mt-0.5 truncate font-mono text-[13px]", tok === "*" ? "text-fg-subtle" : "text-accent font-semibold")}>
                  {tok}
                </div>
              </div>
            ))}
          </div>
        )}

        {/* Plain-English summary. */}
        <div
          className={cn(
            "mt-3 flex items-start gap-2 rounded-lg px-3 py-2.5 text-[13px]",
            result.state === "ok" && "bg-accent-soft text-accent-soft-fg",
            result.state === "error" && "bg-danger-soft text-danger-soft-fg",
            result.state === "empty" && "bg-bg-subtle text-fg-subtle",
          )}
        >
          {result.state === "ok" ? (
            <>
              <CheckCircle2 className="mt-px size-4 shrink-0" />
              <span className="font-medium">{result.human ?? "Valid schedule (no plain-English summary available)."}</span>
            </>
          ) : result.state === "error" ? (
            <>
              <AlertCircle className="mt-px size-4 shrink-0" />
              <span>That isn't a valid cron expression — check the highlighted field above.</span>
            </>
          ) : (
            <>
              <Info className="mt-px size-4 shrink-0" />
              <span>Type an expression or build one below to preview its schedule.</span>
            </>
          )}
        </div>
      </ToolCard>

      <div className="grid items-start gap-4 lg:grid-cols-2">
        {/* ── Presets / builder ────────────────────────────────────── */}
        <ToolCard
          title={
            <span className="flex items-center gap-1.5">
              <Wand2 className="text-fg-subtle size-4" />
              Presets
            </span>
          }
        >
          <p className="text-fg-subtle mb-2 text-[11px] font-semibold tracking-wide uppercase">Every…</p>
          <div className="flex flex-wrap gap-2">
            {QUICK.map((q) => (
              <Button
                key={q.value}
                size="sm"
                variant={expr.trim() === q.value ? "primary" : "secondary"}
                onClick={() => setExpr(q.value)}
              >
                {q.label}
              </Button>
            ))}
          </div>

          <div className="border-edge my-4 border-t" />

          <p className="text-fg-subtle mb-2 text-[11px] font-semibold tracking-wide uppercase">At a specific time</p>
          <div className="mb-3 flex items-end gap-2">
            <PickerSelect label="Hour" value={hour} onChange={setHour}>
              {HOURS.map((h) => (
                <SelectItem key={h} value={String(h)}>
                  {pad(h)}
                </SelectItem>
              ))}
            </PickerSelect>
            <span className="text-fg-subtle pb-1.5 text-sm">:</span>
            <PickerSelect label="Minute" value={minute} onChange={setMinute}>
              {MINUTES.map((m) => (
                <SelectItem key={m} value={String(m)}>
                  {pad(m)}
                </SelectItem>
              ))}
            </PickerSelect>
          </div>

          <div className="space-y-2">
            {/* Daily */}
            <div className="border-edge flex flex-wrap items-center gap-2 rounded-lg border p-2.5">
              <Badge tone="accent">Daily</Badge>
              <span className="text-fg-muted text-[13px]">every day at {time}</span>
              <span className="flex-1" />
              <code className="text-fg-subtle font-mono text-[12px]">{dailyExpr}</code>
              <Button size="sm" variant="secondary" onClick={() => setExpr(dailyExpr)}>
                Use
              </Button>
            </div>

            {/* Weekly */}
            <div className="border-edge flex flex-wrap items-center gap-2 rounded-lg border p-2.5">
              <Badge tone="accent">Weekly</Badge>
              <span className="text-fg-muted text-[13px]">on</span>
              <div className="w-32">
                <Select value={dow} onValueChange={setDow}>
                  <SelectTrigger className="h-8">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {WEEKDAYS.map((d, i) => (
                      <SelectItem key={i} value={String(i)}>
                        {d}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <span className="text-fg-muted text-[13px]">at {time}</span>
              <span className="flex-1" />
              <code className="text-fg-subtle font-mono text-[12px]">{weeklyExpr}</code>
              <Button size="sm" variant="secondary" onClick={() => setExpr(weeklyExpr)}>
                Use
              </Button>
            </div>

            {/* Monthly */}
            <div className="border-edge flex flex-wrap items-center gap-2 rounded-lg border p-2.5">
              <Badge tone="accent">Monthly</Badge>
              <span className="text-fg-muted text-[13px]">on day</span>
              <div className="w-16">
                <Select value={dom} onValueChange={setDom}>
                  <SelectTrigger className="h-8">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DOMS.map((d) => (
                      <SelectItem key={d} value={String(d)}>
                        {d}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <span className="text-fg-muted text-[13px]">at {time}</span>
              <span className="flex-1" />
              <code className="text-fg-subtle font-mono text-[12px]">{monthlyExpr}</code>
              <Button size="sm" variant="secondary" onClick={() => setExpr(monthlyExpr)}>
                Use
              </Button>
            </div>
          </div>
        </ToolCard>

        {/* ── Next runs ────────────────────────────────────────────── */}
        <ToolCard
          title={
            <span className="flex items-center gap-1.5">
              <Clock className="text-fg-subtle size-4" />
              Next runs
            </span>
          }
        >
          {result.state === "ok" ? (
            <ol className="space-y-1.5">
              {result.runs.map((d, i) => {
                const label = absLabel(d);
                return (
                  <li key={i} className="border-edge bg-bg-subtle/60 flex items-center gap-2.5 rounded-lg border px-2.5 py-2">
                    <span className="bg-surface text-fg-subtle border-edge flex size-5 shrink-0 items-center justify-center rounded-full border text-[11px] font-semibold">
                      {i + 1}
                    </span>
                    <span className="text-fg min-w-0 flex-1 truncate font-mono text-[13px]">{label}</span>
                    <span className="text-fg-subtle hidden shrink-0 text-[12px] sm:inline">{relative(d, now)}</span>
                    <CopyIconButton value={label} label="Copy run time" />
                  </li>
                );
              })}
            </ol>
          ) : (
            <div className="text-fg-subtle flex h-full min-h-32 items-center justify-center rounded-lg border border-dashed border-edge px-4 py-8 text-center text-[13px]">
              {result.state === "error"
                ? "Fix the expression to see upcoming runs."
                : "Upcoming run times will appear here."}
            </div>
          )}
          <p className="text-fg-subtle mt-2.5 text-[11.5px]">Shown in your local time zone.</p>
        </ToolCard>
      </div>

      {/* ── Field reference ──────────────────────────────────────────── */}
      <ToolCard
        title={
          <span className="flex items-center gap-1.5">
            <Info className="text-fg-subtle size-4" />
            Field reference
          </span>
        }
      >
        <div className="overflow-x-auto">
          <table className="w-full min-w-[26rem] text-left text-[12.5px]">
            <thead>
              <tr className="text-fg-subtle border-edge border-b">
                <th className="py-1.5 pr-4 font-medium">Position</th>
                <th className="py-1.5 pr-4 font-medium">Field</th>
                <th className="py-1.5 font-medium">Allowed values</th>
              </tr>
            </thead>
            <tbody className="text-fg divide-edge divide-y">
              {[
                ["1", "Minute", "0–59"],
                ["2", "Hour", "0–23"],
                ["3", "Day of month", "1–31"],
                ["4", "Month", "1–12 or JAN–DEC"],
                ["5", "Day of week", "0–6 or SUN–SAT (0 = Sunday)"],
              ].map(([pos, field, vals]) => (
                <tr key={pos}>
                  <td className="text-fg-subtle py-1.5 pr-4 font-mono">{pos}</td>
                  <td className="py-1.5 pr-4">{field}</td>
                  <td className="py-1.5 font-mono">{vals}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        <div className="text-fg-subtle mt-3 flex flex-wrap gap-x-4 gap-y-1 text-[12px]">
          <span>
            <code className="text-fg font-mono">*</code> any value
          </span>
          <span>
            <code className="text-fg font-mono">,</code> list (1,3,5)
          </span>
          <span>
            <code className="text-fg font-mono">-</code> range (1-5)
          </span>
          <span>
            <code className="text-fg font-mono">/</code> step (*/15)
          </span>
        </div>
      </ToolCard>
    </div>
  );
}
