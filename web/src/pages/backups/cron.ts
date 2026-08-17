// Cron helpers for the backup schedule builder: preset → cron string, and a
// best-effort humanizer for the shapes the builder emits (plus a few common
// hand-written ones). Anything unrecognized falls back to the raw expression.
import type { TFunction } from "i18next";

export type CronPreset = "daily" | "weekly" | "custom";

/** Locale-aware weekday name for a cron day-of-week (0 = Sunday). */
export function weekdayName(day: number): string {
  // 2021-08-01 was a Sunday.
  return new Date(Date.UTC(2021, 7, 1 + (((day % 7) + 7) % 7))).toLocaleDateString(undefined, {
    weekday: "long",
    timeZone: "UTC",
  });
}

/** Locale-formatted "HH:MM" for cron hour/minute fields. */
function timeLabel(hour: number, minute: number): string {
  return new Date(2000, 0, 1, hour, minute).toLocaleTimeString(undefined, {
    hour: "2-digit",
    minute: "2-digit",
  });
}

/** buildCron turns the visual builder's state into a 5-field expression. */
export function buildCron(preset: Exclude<CronPreset, "custom">, time: string, weekday: number): string {
  const [h, m] = time.split(":").map((v) => Number(v));
  const hour = Number.isFinite(h) ? h : 3;
  const minute = Number.isFinite(m) ? m : 0;
  return preset === "daily" ? `${minute} ${hour} * * *` : `${minute} ${hour} * * ${weekday}`;
}

const FIELD_RE = /^(\*|\d{1,2})(-\d{1,2})?(\/\d{1,2})?(,(\*|\d{1,2})(-\d{1,2})?(\/\d{1,2})?)*$/;

/** Shallow 5-field shape check; the server is the real validator. */
export function looksLikeCron(cron: string): boolean {
  const fields = cron.trim().split(/\s+/);
  return fields.length === 5 && fields.every((f) => FIELD_RE.test(f));
}

const num = (field: string): number | null => (/^\d{1,2}$/.test(field) ? Number(field) : null);

/**
 * describeCron renders a recognized expression as human text ("every day at
 * 03:00"). Returns null for shapes it doesn't understand.
 */
export function describeCron(cron: string, t: TFunction): string | null {
  const fields = cron.trim().split(/\s+/);
  if (fields.length !== 5) return null;
  const [minF, hourF, domF, monF, dowF] = fields;
  if (monF !== "*") return null;

  const minute = num(minF);
  const hour = num(hourF);

  // */n * * * * — every n minutes.
  if (hourF === "*" && domF === "*" && dowF === "*") {
    if (minF === "*") return t("backups.cron.everyMinute");
    const step = /^\*\/(\d{1,2})$/.exec(minF);
    if (step) return t("backups.cron.everyNMinutes", { n: Number(step[1]) });
    if (minute !== null) return t("backups.cron.hourly", { m: String(minute).padStart(2, "0") });
    return null;
  }

  // m */n * * * — every n hours.
  const hourStep = /^\*\/(\d{1,2})$/.exec(hourF);
  if (hourStep && minute !== null && domF === "*" && dowF === "*") {
    return t("backups.cron.everyNHours", { n: Number(hourStep[1]) });
  }

  if (minute === null || hour === null) return null;
  const time = timeLabel(hour, minute);

  if (domF === "*" && dowF === "*") return t("backups.cron.daily", { time });

  // m h * * d — weekly (7 is an alias for Sunday).
  const dow = num(dowF);
  if (domF === "*" && dow !== null && dow <= 7) {
    return t("backups.cron.weekly", { day: weekdayName(dow % 7), time });
  }

  // m h d * * — monthly.
  const dom = num(domF);
  if (dowF === "*" && dom !== null && dom >= 1 && dom <= 31) {
    return t("backups.cron.monthly", { dom, time });
  }

  return null;
}
