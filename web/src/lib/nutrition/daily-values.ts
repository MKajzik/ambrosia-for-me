import type { NutrientKey } from "./catalog";

/**
 * FDA Daily Values for adults, in the catalog's own unit for each nutrient. Same for every user.
 * Only nutrients a food label gives a percentage for; macros are judged against personal targets elsewhere.
 */
export const DAILY_VALUES: Partial<Record<NutrientKey, number>> = {
  fibre: 28,
  saturated_fat: 20,
  sodium: 2300,
  potassium: 4700,
  calcium: 1300,
  iron: 18,
  magnesium: 420,
  zinc: 11,
  vitamin_a: 900,
  vitamin_c: 90,
  vitamin_d: 20,
  vitamin_b12: 2.4,
  folate: 400,
};

export function hasDailyValue(key: NutrientKey): boolean {
  return DAILY_VALUES[key] !== undefined;
}

/** The amount as a percentage of the Daily Value, or null when the amount is unknown or there is no reference. */
export function dailyValuePercent(key: NutrientKey, amount: number | null): number | null {
  const reference = DAILY_VALUES[key];
  if (reference === undefined || amount === null) return null;
  return (amount / reference) * 100;
}
