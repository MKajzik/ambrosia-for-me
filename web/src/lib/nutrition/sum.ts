import { NUTRIENTS, type NutrientAmounts, type NutrientKey } from "./catalog";

export function zeroNutrition(): NutrientAmounts {
  return Object.fromEntries(NUTRIENTS.map((n) => [n.key, 0])) as NutrientAmounts;
}

/** Adds nutrient by nutrient. A nutrient is unknown (`null`) in the sum if it is unknown in any item: unknown is never treated as zero. */
export function sumNutrition(list: readonly NutrientAmounts[]): NutrientAmounts {
  const total = zeroNutrition() as Record<NutrientKey, number | null>;
  for (const item of list) {
    for (const { key } of NUTRIENTS) {
      const current = total[key];
      const value = item[key];
      total[key] = current === null || value === null ? null : current + value;
    }
  }
  return total as NutrientAmounts;
}

export function scaleNutrition(nutrition: NutrientAmounts, factor: number): NutrientAmounts {
  const scaled = { ...nutrition } as Record<NutrientKey, number | null>;
  for (const { key } of NUTRIENTS) {
    const value = nutrition[key];
    scaled[key] = value === null ? null : value * factor;
  }
  return scaled as NutrientAmounts;
}
