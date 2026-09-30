/**
 * Calendar dates as the API takes them: "YYYY-MM-DD", the person's own local day.
 * Never derive one with `toISOString()`: that is the UTC day, which is wrong for part of every day in most time zones.
 */
const pad = (n: number) => String(n).padStart(2, "0");

export function toIsoDate(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** A real calendar date at local midnight. Throws for anything else ("2026-02-30", "tomorrow", a timestamp). */
export function parseIsoDate(iso: string): Date {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!match) throw new Error(`not a calendar date: ${iso}`);
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  if (toIsoDate(date) !== iso) throw new Error(`not a calendar date: ${iso}`);
  return date;
}

export function isIsoDate(text: string): boolean {
  try {
    parseIsoDate(text);
    return true;
  } catch {
    return false;
  }
}

export function today(now: Date = new Date()): string {
  return toIsoDate(now);
}

/** Moves by whole calendar days, so a daylight-saving change never skips or repeats a date. */
export function addDays(iso: string, days: number): string {
  const date = parseIsoDate(iso);
  date.setDate(date.getDate() + days);
  return toIsoDate(date);
}

/** The Monday on or before `iso`. */
export function startOfWeek(iso: string): string {
  const back = (parseIsoDate(iso).getDay() + 6) % 7;
  return addDays(iso, -back);
}

export function weekDates(startIso: string): string[] {
  return Array.from({ length: 7 }, (_, i) => addDays(startIso, i));
}

const WEEKDAY = new Intl.DateTimeFormat("en-US", { weekday: "short" });
const MONTH_DAY = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const LONG = new Intl.DateTimeFormat("en-US", { weekday: "long", month: "long", day: "numeric" });

export const formatWeekday = (iso: string) => WEEKDAY.format(parseIsoDate(iso));
export const formatMonthDay = (iso: string) => MONTH_DAY.format(parseIsoDate(iso));
export const formatLongDate = (iso: string) => LONG.format(parseIsoDate(iso));
export const formatWeekRange = (startIso: string) => `${formatMonthDay(startIso)} – ${formatMonthDay(addDays(startIso, 6))}`;
