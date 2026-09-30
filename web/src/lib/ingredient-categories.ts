import type { components } from "@/lib/api/schema.gen";

export type IngredientCategory = components["schemas"]["IngredientCategory"];

/** Every category, in the order the API lists it, with the words a person would use. */
export const CATEGORY_LABELS: Record<IngredientCategory, string> = {
  produce: "Produce",
  dairy_eggs: "Dairy and eggs",
  meat_seafood: "Meat and seafood",
  grains_bread: "Grains and bread",
  legumes_nuts_seeds: "Legumes, nuts and seeds",
  condiments_oils: "Condiments and oils",
  spices_herbs: "Spices and herbs",
  beverages: "Beverages",
  sweets_snacks: "Sweets and snacks",
  other: "Other",
};

export const CATEGORIES = Object.keys(CATEGORY_LABELS) as IngredientCategory[];

export function categoryLabel(category: IngredientCategory): string {
  return CATEGORY_LABELS[category];
}
