import type { components } from "@/lib/api/schema.gen";

export type NutrientAmounts = components["schemas"]["NutrientAmounts"];
export type NutrientKey = keyof NutrientAmounts;
export type NutrientUnit = "kcal" | "g" | "mg" | "µg";
export type NutrientGroup = "Macronutrients" | "Minerals" | "Vitamins";

export type NutrientInfo = { key: NutrientKey; label: string; unit: NutrientUnit; group: NutrientGroup };

/** The 18 nutrients the API tracks, in the order it lists them. Units are the API's (`NutrientAmounts` in openapi.yaml). */
export const NUTRIENTS: readonly NutrientInfo[] = [
  { key: "calories", label: "Calories", unit: "kcal", group: "Macronutrients" },
  { key: "protein", label: "Protein", unit: "g", group: "Macronutrients" },
  { key: "carbohydrates", label: "Carbohydrates", unit: "g", group: "Macronutrients" },
  { key: "sugar", label: "Sugar", unit: "g", group: "Macronutrients" },
  { key: "fibre", label: "Fibre", unit: "g", group: "Macronutrients" },
  { key: "fat", label: "Fat", unit: "g", group: "Macronutrients" },
  { key: "saturated_fat", label: "Saturated fat", unit: "g", group: "Macronutrients" },
  { key: "sodium", label: "Sodium", unit: "mg", group: "Minerals" },
  { key: "potassium", label: "Potassium", unit: "mg", group: "Minerals" },
  { key: "calcium", label: "Calcium", unit: "mg", group: "Minerals" },
  { key: "iron", label: "Iron", unit: "mg", group: "Minerals" },
  { key: "magnesium", label: "Magnesium", unit: "mg", group: "Minerals" },
  { key: "zinc", label: "Zinc", unit: "mg", group: "Minerals" },
  { key: "vitamin_a", label: "Vitamin A", unit: "µg", group: "Vitamins" },
  { key: "vitamin_c", label: "Vitamin C", unit: "mg", group: "Vitamins" },
  { key: "vitamin_d", label: "Vitamin D", unit: "µg", group: "Vitamins" },
  { key: "vitamin_b12", label: "Vitamin B12", unit: "µg", group: "Vitamins" },
  { key: "folate", label: "Folate", unit: "µg", group: "Vitamins" },
];

export const NUTRIENT_GROUPS: readonly NutrientGroup[] = ["Macronutrients", "Minerals", "Vitamins"];

/** The summary tier: each keeps one colour across the product. */
export const MACRO_KEYS = ["calories", "protein", "carbohydrates", "fat"] as const;

const BY_KEY = new Map(NUTRIENTS.map((n) => [n.key, n]));

export function nutrientInfo(key: NutrientKey): NutrientInfo {
  const info = BY_KEY.get(key);
  if (!info) throw new Error(`unknown nutrient ${key}`);
  return info;
}
