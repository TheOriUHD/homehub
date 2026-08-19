import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api";
import type { ApiSchemas } from "@/lib/api";

export type Calendar = ApiSchemas["Calendar"];
export type CalendarInstance = ApiSchemas["CalendarInstance"];
export type CalendarEventInput = ApiSchemas["CalendarEventInput"];
export type EditScope = "all" | "this" | "following";

/* ---- date helpers (all local-time; the API speaks UTC ISO) ---------------- */

export const DAY_MS = 86_400_000;

/** Monday-start day-of-week index (0 = Monday … 6 = Sunday). */
export const mondayIndex = (d: Date) => (d.getDay() + 6) % 7;

export const startOfDay = (d: Date) => new Date(d.getFullYear(), d.getMonth(), d.getDate());

export const addDays = (d: Date, n: number) =>
  new Date(d.getFullYear(), d.getMonth(), d.getDate() + n);

export const sameDay = (a: Date, b: Date) =>
  a.getFullYear() === b.getFullYear() && a.getMonth() === b.getMonth() && a.getDate() === b.getDate();

/** The 6-week grid that shows a month (Monday-start). */
export function monthGrid(year: number, month: number): Date[] {
  const first = new Date(year, month, 1);
  const start = addDays(first, -mondayIndex(first));
  return Array.from({ length: 42 }, (_, i) => addDays(start, i));
}

export function weekDays(anchor: Date): Date[] {
  const start = addDays(startOfDay(anchor), -mondayIndex(anchor));
  return Array.from({ length: 7 }, (_, i) => addDays(start, i));
}

/** All-day events live on UTC dates; render them on the matching LOCAL day.
 * Returns [firstDay, endDayExclusive] as local midnights for any instance. */
export function instanceDaySpan(inst: { start: string; end: string; allDay: boolean }): [Date, Date] {
  if (inst.allDay) {
    const s = new Date(inst.start);
    const e = new Date(inst.end);
    const first = new Date(s.getUTCFullYear(), s.getUTCMonth(), s.getUTCDate());
    let endEx = new Date(e.getUTCFullYear(), e.getUTCMonth(), e.getUTCDate());
    if (endEx <= first) endEx = addDays(first, 1);
    return [first, endEx];
  }
  const s = new Date(inst.start);
  return [startOfDay(s), new Date(inst.end)];
}

export const toLocalDateInput = (d: Date) =>
  `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;

export const toLocalTimeInput = (d: Date) =>
  `${String(d.getHours()).padStart(2, "0")}:${String(d.getMinutes()).padStart(2, "0")}`;

export function fromInputs(date: string, time: string): Date {
  const [y, m, dd] = date.split("-").map(Number);
  const [hh, mm] = (time || "00:00").split(":").map(Number);
  return new Date(y, (m ?? 1) - 1, dd ?? 1, hh ?? 0, mm ?? 0);
}

/* ---- queries --------------------------------------------------------------- */

export function useCalendars() {
  return useQuery({
    queryKey: ["calendars"],
    queryFn: async () => unwrap(await api.GET("/calendars")),
    staleTime: 30_000,
  });
}

export function useCalendarInstances(from: Date, to: Date) {
  return useQuery({
    queryKey: ["calendar-events", from.toISOString(), to.toISOString()],
    queryFn: async () =>
      unwrap(
        await api.GET("/calendar/events", {
          params: { query: { from: from.toISOString(), to: to.toISOString() } },
        }),
      ),
    staleTime: 15_000,
  });
}

export function useInvalidateCalendar() {
  const qc = useQueryClient();
  return () => {
    void qc.invalidateQueries({ queryKey: ["calendar-events"] });
    void qc.invalidateQueries({ queryKey: ["calendars"] });
  };
}

export function useCreateEvent() {
  const invalidate = useInvalidateCalendar();
  return useMutation({
    mutationFn: async (vars: { calendarId: string; event: CalendarEventInput }) =>
      unwrap(await api.POST("/calendar/events", { body: vars })),
    onSuccess: invalidate,
  });
}

export function useUpdateEvent() {
  const invalidate = useInvalidateCalendar();
  return useMutation({
    mutationFn: async (vars: {
      id: string;
      scope?: EditScope;
      occurrenceStart?: string;
      event: CalendarEventInput;
    }) =>
      unwrap(
        await api.PUT("/calendar/events/{id}", {
          params: { path: { id: vars.id } },
          body: { scope: vars.scope, occurrenceStart: vars.occurrenceStart, event: vars.event },
        }),
      ),
    onSuccess: invalidate,
  });
}

export function useDeleteEvent() {
  const invalidate = useInvalidateCalendar();
  return useMutation({
    mutationFn: async (vars: { id: string; scope?: EditScope; occurrenceStart?: string }) =>
      unwrap(
        await api.DELETE("/calendar/events/{id}", {
          params: {
            path: { id: vars.id },
            query: { scope: vars.scope, occurrenceStart: vars.occurrenceStart },
          },
        }),
      ),
    onSuccess: invalidate,
  });
}

/* ---- recurrence presets ---------------------------------------------------- */

const WD = ["MO", "TU", "WE", "TH", "FR", "SA", "SU"];

/** Build the preset RRULE for an anchor date; "" = no repeat. */
export function presetRRule(preset: string, anchor: Date): string {
  switch (preset) {
    case "daily":
      return "FREQ=DAILY";
    case "weekly":
      return `FREQ=WEEKLY;BYDAY=${WD[mondayIndex(anchor)]}`;
    case "biweekly":
      return `FREQ=WEEKLY;INTERVAL=2;BYDAY=${WD[mondayIndex(anchor)]}`;
    case "monthly":
      return `FREQ=MONTHLY;BYMONTHDAY=${anchor.getDate()}`;
    case "yearly":
      return "FREQ=YEARLY";
    default:
      return "";
  }
}

/** Reverse-map an RRULE to a preset key, or "custom". */
export function rruleToPreset(rule: string | undefined, anchor: Date): string {
  if (!rule) return "none";
  const presets = ["daily", "weekly", "biweekly", "monthly", "yearly"];
  for (const p of presets) {
    if (presetRRule(p, anchor) === rule) return p;
  }
  return "custom";
}
