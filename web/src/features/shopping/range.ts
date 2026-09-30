import { addDays, isIsoDate } from "@/lib/dates";

/** The API builds a list from at most 92 days of the plan, inclusive of both ends. */
export const MAX_RANGE_DAYS = 92;

export function validateRange(from: string, to: string): { ok: true } | { ok: false; error: string } {
  if (!isIsoDate(from) || !isIsoDate(to)) return { ok: false, error: "Choose a start and end date." };
  // ISO dates sort as text, so a plain comparison is a date comparison.
  if (to < from) return { ok: false, error: "The end date must be on or after the start date." };
  if (to > addDays(from, MAX_RANGE_DAYS - 1)) return { ok: false, error: `Pick at most ${MAX_RANGE_DAYS} days.` };
  return { ok: true };
}
