import { useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { CalendarDays, ChevronLeft, ChevronRight, Plus, Settings2 } from "lucide-react";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/button";
import { Card } from "@/ui/card";
import { PageLoader } from "@/ui/misc";
import { EventEditor } from "./event-editor";
import { ManageCalendarsDialog } from "./manage";
import {
  addDays,
  DAY_MS,
  instanceDaySpan,
  monthGrid,
  sameDay,
  startOfDay,
  useCalendarInstances,
  useCalendars,
  weekDays,
  type CalendarInstance,
} from "./shared";

/**
 * The calendar app: month and week views over every calendar (local,
 * CalDAV, feeds, trash schedules), full-width, with an Apple-grade editor.
 */
export function CalendarPage() {
  const { t, i18n } = useTranslation();
  const [view, setView] = useState<"month" | "week">("month");
  const [anchor, setAnchor] = useState(() => startOfDay(new Date()));
  const [editing, setEditing] = useState<CalendarInstance | null>(null);
  const [creating, setCreating] = useState<{ start: Date; allDay?: boolean } | null>(null);
  const [managing, setManaging] = useState(false);
  const [hiddenCals, setHiddenCals] = useState<Set<string>>(new Set());

  const calendars = useCalendars();

  // Query window covers the whole visible grid.
  const [from, to] = useMemo(() => {
    if (view === "month") {
      const grid = monthGrid(anchor.getFullYear(), anchor.getMonth());
      return [grid[0], addDays(grid[41], 1)];
    }
    const days = weekDays(anchor);
    return [days[0], addDays(days[6], 1)];
  }, [view, anchor]);

  const instances = useCalendarInstances(from, to);

  const visible = useMemo(
    () => (instances.data ?? []).filter((i) => !hiddenCals.has(i.calendarId)),
    [instances.data, hiddenCals],
  );

  const calColor = useMemo(() => {
    const m = new Map<string, string>();
    for (const c of calendars.data ?? []) m.set(c.id, c.color);
    return m;
  }, [calendars.data]);

  const step = (dir: number) => {
    if (view === "month") setAnchor(new Date(anchor.getFullYear(), anchor.getMonth() + dir, 1));
    else setAnchor(addDays(anchor, dir * 7));
  };

  const monthTitle = anchor.toLocaleDateString(i18n.language, { month: "long", year: "numeric" });

  return (
    <div className="flex h-[calc(100dvh-6.5rem)] min-h-0 flex-col space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="bg-accent-soft text-accent-soft-fg flex size-9 items-center justify-center rounded-xl">
          <CalendarDays className="size-4.5" />
        </span>
        {/* Navigation before the title: the title's width changes with the
            month name, and anything after it would shift under a finger
            that is clicking through the year. */}
        <div className="flex items-center gap-0.5">
          <Button size="sm" variant="ghost" onClick={() => step(-1)} aria-label={t("calendar.prev")}>
            <ChevronLeft className="size-4" />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setAnchor(startOfDay(new Date()))}>
            {t("calendar.today")}
          </Button>
          <Button size="sm" variant="ghost" onClick={() => step(1)} aria-label={t("calendar.next")}>
            <ChevronRight className="size-4" />
          </Button>
        </div>
        <h1 className="text-fg ml-1 text-lg font-semibold tracking-tight capitalize">{monthTitle}</h1>

        <span className="flex-1" />

        {/* calendar visibility chips */}
        <div className="flex flex-wrap items-center gap-1">
          {(calendars.data ?? []).map((c) => {
            const off = hiddenCals.has(c.id);
            return (
              <button
                key={c.id}
                type="button"
                onClick={() =>
                  setHiddenCals((s) => {
                    const next = new Set(s);
                    if (next.has(c.id)) next.delete(c.id);
                    else next.add(c.id);
                    return next;
                  })
                }
                className={cn(
                  "flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-[11.5px] transition-opacity",
                  "border-edge text-fg-muted hover:text-fg",
                  off && "opacity-40",
                )}
              >
                <span className="size-2 rounded-full" style={{ background: c.color }} />
                {c.name}
              </button>
            );
          })}
        </div>

        <div className="border-edge flex overflow-hidden rounded-lg border">
          {(["month", "week"] as const).map((v) => (
            <button
              key={v}
              type="button"
              onClick={() => setView(v)}
              className={cn(
                "px-2.5 py-1 text-[12.5px] transition-colors",
                view === v
                  ? "bg-accent-soft text-accent-soft-fg font-medium"
                  : "text-fg-muted hover:text-fg hover:bg-bg-subtle",
              )}
            >
              {t(`calendar.view.${v}`)}
            </button>
          ))}
        </div>
        <Button size="sm" variant="ghost" onClick={() => setManaging(true)} title={t("calendar.manage.title")}>
          <Settings2 className="size-4" />
        </Button>
        <Button size="sm" variant="primary" onClick={() => setCreating({ start: nextHour() })}>
          <Plus className="size-3.5" />
          {t("calendar.newEvent")}
        </Button>
      </div>

      {calendars.isLoading || instances.isLoading ? (
        <PageLoader className="py-20" />
      ) : view === "month" ? (
        <MonthView
          anchor={anchor}
          instances={visible}
          calColor={calColor}
          onPickDay={(d) => setCreating({ start: at(d, 9), allDay: false })}
          onPickEvent={setEditing}
        />
      ) : (
        <WeekView
          anchor={anchor}
          instances={visible}
          calColor={calColor}
          onPickSlot={(d, hour) => setCreating({ start: at(d, hour) })}
          onPickEvent={setEditing}
        />
      )}

      {(creating || editing) && (
        <EventEditor
          calendars={calendars.data ?? []}
          instance={editing}
          defaults={creating ?? undefined}
          onClose={() => {
            setCreating(null);
            setEditing(null);
          }}
        />
      )}
      {managing && (
        <ManageCalendarsDialog calendars={calendars.data ?? []} onClose={() => setManaging(false)} />
      )}
    </div>
  );
}

const nextHour = () => {
  const d = new Date();
  d.setMinutes(0, 0, 0);
  d.setHours(d.getHours() + 1);
  return d;
};

const at = (day: Date, hour: number) =>
  new Date(day.getFullYear(), day.getMonth(), day.getDate(), hour);

/* ---- month view ------------------------------------------------------------ */

function MonthView({
  anchor,
  instances,
  calColor,
  onPickDay,
  onPickEvent,
}: {
  anchor: Date;
  instances: CalendarInstance[];
  calColor: Map<string, string>;
  onPickDay: (d: Date) => void;
  onPickEvent: (i: CalendarInstance) => void;
}) {
  const { t, i18n } = useTranslation();
  const grid = monthGrid(anchor.getFullYear(), anchor.getMonth());
  const today = new Date();

  const weekdayLabels = useMemo(() => {
    const fmt = new Intl.DateTimeFormat(i18n.language, { weekday: "short" });
    return Array.from({ length: 7 }, (_, i) => fmt.format(new Date(Date.UTC(2024, 0, 8 + i))));
  }, [i18n.language]);

  // Bucket instances per day (multi-day events land in every day they span).
  const byDay = useMemo(() => {
    const m = new Map<string, CalendarInstance[]>();
    for (const inst of instances) {
      const [start, end] = instanceDaySpan(inst);
      for (let d = start; d < end; d = addDays(d, 1)) {
        const k = d.toDateString();
        const arr = m.get(k) ?? [];
        arr.push(inst);
        m.set(k, arr);
      }
    }
    return m;
  }, [instances]);

  return (
    <Card className="flex min-h-0 flex-1 flex-col overflow-hidden p-0">
      <div className="border-edge grid grid-cols-7 border-b">
        {weekdayLabels.map((l) => (
          <div key={l} className="text-fg-subtle px-2 py-1.5 text-center text-[11.5px] font-medium uppercase">
            {l}
          </div>
        ))}
      </div>
      <div className="grid min-h-0 flex-1 grid-cols-7 grid-rows-6">
        {grid.map((day, i) => {
          const inMonth = day.getMonth() === anchor.getMonth();
          const isToday = sameDay(day, today);
          const events = (byDay.get(day.toDateString()) ?? []).slice().sort(
            (a, b) => Number(b.allDay) - Number(a.allDay) || a.start.localeCompare(b.start),
          );
          const shown = events.slice(0, 4);
          return (
            <div
              key={i}
              onClick={() => onPickDay(day)}
              className={cn(
                "border-edge flex min-h-0 cursor-pointer flex-col gap-0.5 overflow-hidden border-r border-b p-1 transition-colors",
                (i + 1) % 7 === 0 && "border-r-0",
                i >= 35 && "border-b-0",
                !inMonth && "bg-bg-subtle/40",
                "hover:bg-accent-soft/10",
              )}
            >
              <span
                className={cn(
                  "mb-0.5 flex size-6 items-center justify-center self-start rounded-full text-[12px]",
                  isToday ? "bg-accent text-accent-fg font-semibold" : inMonth ? "text-fg" : "text-fg-subtle",
                )}
              >
                {day.getDate()}
              </span>
              {shown.map((ev, j) => (
                <button
                  key={ev.eventId + ev.occurrenceStart + j}
                  type="button"
                  onClick={(e) => {
                    e.stopPropagation();
                    onPickEvent(ev);
                  }}
                  className="hover:brightness-110 flex w-full items-center gap-1 truncate rounded px-1 py-0.5 text-left text-[11px] leading-tight"
                  style={{
                    background: `color-mix(in oklab, ${calColor.get(ev.calendarId) ?? "#4f46e5"} 18%, transparent)`,
                  }}
                >
                  <span
                    className="size-1.5 shrink-0 rounded-full"
                    style={{ background: calColor.get(ev.calendarId) ?? "#4f46e5" }}
                  />
                  {!ev.allDay && (
                    <span className="text-fg-subtle shrink-0 tabular-nums">
                      {new Date(ev.start).toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })}
                    </span>
                  )}
                  <span className="text-fg truncate">{ev.title || "—"}</span>
                </button>
              ))}
              {events.length > shown.length && (
                <span className="text-fg-subtle px-1 text-[10.5px]">
                  {t("calendar.more", { count: events.length - shown.length })}
                </span>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
}

/* ---- week view ------------------------------------------------------------- */

const HOUR_PX = 48;
const DAY_START = 0;

function WeekView({
  anchor,
  instances,
  calColor,
  onPickSlot,
  onPickEvent,
}: {
  anchor: Date;
  instances: CalendarInstance[];
  calColor: Map<string, string>;
  onPickSlot: (d: Date, hour: number) => void;
  onPickEvent: (i: CalendarInstance) => void;
}) {
  const { i18n } = useTranslation();
  const days = weekDays(anchor);
  const today = new Date();

  const allDayByDay = days.map((d) =>
    instances.filter((i) => {
      if (!i.allDay) return false;
      const [s, e] = instanceDaySpan(i);
      return s < addDays(d, 1) && e > d;
    }),
  );
  const timedByDay = days.map((d) =>
    instances.filter(
      (i) => !i.allDay && sameDay(startOfDay(new Date(i.start)), d),
    ),
  );

  const dayFmt = new Intl.DateTimeFormat(i18n.language, { weekday: "short", day: "numeric" });

  return (
    <Card className="flex min-h-0 flex-1 flex-col overflow-hidden p-0">
      {/* header + all-day row */}
      <div className="border-edge grid shrink-0 border-b" style={{ gridTemplateColumns: "52px repeat(7, 1fr)" }}>
        <div />
        {days.map((d, i) => (
          <div key={i} className="border-edge border-l px-2 py-1.5 text-center">
            <span
              className={cn(
                "text-[12px] font-medium",
                sameDay(d, today) ? "text-accent" : "text-fg-muted",
              )}
            >
              {dayFmt.format(d)}
            </span>
            <div className="mt-0.5 space-y-0.5">
              {allDayByDay[i].map((ev, j) => (
                <button
                  key={ev.eventId + j}
                  type="button"
                  onClick={() => onPickEvent(ev)}
                  className="block w-full truncate rounded px-1 py-0.5 text-left text-[10.5px]"
                  style={{
                    background: `color-mix(in oklab, ${calColor.get(ev.calendarId) ?? "#4f46e5"} 22%, transparent)`,
                  }}
                >
                  {ev.title}
                </button>
              ))}
            </div>
          </div>
        ))}
      </div>
      {/* time grid */}
      <div className="min-h-0 flex-1 overflow-y-auto">
        <div className="relative grid" style={{ gridTemplateColumns: "52px repeat(7, 1fr)", height: 24 * HOUR_PX }}>
          {/* hour gutter */}
          <div className="relative">
            {Array.from({ length: 24 }, (_, h) => (
              <span
                key={h}
                className="text-fg-subtle absolute right-2 -translate-y-1/2 text-[10.5px] tabular-nums"
                style={{ top: h * HOUR_PX }}
              >
                {h > 0 ? `${String(h).padStart(2, "0")}:00` : ""}
              </span>
            ))}
          </div>
          {days.map((d, di) => (
            <div key={di} className="border-edge relative border-l">
              {Array.from({ length: 24 }, (_, h) => (
                <div
                  key={h}
                  onClick={() => onPickSlot(d, h)}
                  className="border-edge/60 hover:bg-accent-soft/10 absolute right-0 left-0 cursor-pointer border-t"
                  style={{ top: h * HOUR_PX, height: HOUR_PX }}
                />
              ))}
              {timedByDay[di].map((ev, j) => {
                const start = new Date(ev.start);
                const end = new Date(ev.end);
                const top = ((start.getHours() - DAY_START) + start.getMinutes() / 60) * HOUR_PX;
                const height = Math.max(20, ((end.getTime() - start.getTime()) / DAY_MS) * 24 * HOUR_PX);
                const color = calColor.get(ev.calendarId) ?? "#4f46e5";
                return (
                  <button
                    key={ev.eventId + ev.occurrenceStart + j}
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onPickEvent(ev);
                    }}
                    className="absolute right-1 left-1 z-10 overflow-hidden rounded-md border-l-2 px-1.5 py-0.5 text-left text-[11px] leading-tight"
                    style={{
                      top,
                      height,
                      borderLeftColor: color,
                      background: `color-mix(in oklab, ${color} 20%, var(--hh-surface))`,
                    }}
                  >
                    <span className="text-fg block truncate font-medium">{ev.title}</span>
                    <span className="text-fg-subtle tabular-nums">
                      {start.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })}
                      –{end.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })}
                    </span>
                  </button>
                );
              })}
              {sameDay(d, today) && <NowLine />}
            </div>
          ))}
        </div>
      </div>
    </Card>
  );
}

function NowLine() {
  const now = new Date();
  const top = (now.getHours() + now.getMinutes() / 60) * HOUR_PX;
  return (
    <div className="bg-danger pointer-events-none absolute right-0 left-0 z-20 h-0.5" style={{ top }}>
      <span className="bg-danger absolute -top-1 -left-1 size-2 rounded-full" />
    </div>
  );
}
