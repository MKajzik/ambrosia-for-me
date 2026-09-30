import type { NutrientKey, NutrientUnit } from "./catalog";
import { dailyValuePercent } from "./daily-values";

/** What an unknown amount looks like. Never `0`: an unknown amount is not a zero amount. */
export const NO_DATA = "—";

export function formatAmount(amount: number | null, unit: NutrientUnit): string {
  if (amount === null) return NO_DATA;
  const digits = unit === "kcal" || Math.abs(amount) >= 10 ? 0 : 1;
  const text = amount.toLocaleString("en-US", { minimumFractionDigits: 0, maximumFractionDigits: digits });
  return `${text} ${unit}`;
}

export function formatDailyValue(key: NutrientKey, amount: number | null): string {
  const percent = dailyValuePercent(key, amount);
  return percent === null ? NO_DATA : `${Math.round(percent).toLocaleString("en-US")}%`;
}
