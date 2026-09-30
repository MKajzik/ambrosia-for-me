import { parseCustomIngredient, type CustomIngredientErrors } from "@/components/ingredient-search/custom-ingredient";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { NUTRIENTS, type NutrientKey } from "@/lib/nutrition";
import type { UpdateIngredientRequest } from "./queries";

const SHOWN = ["calories", "protein", "carbohydrates", "fat"] as const;

/**
 * The update for an edited custom ingredient. `nutrients` replaces the whole set on the API, so all 18 keys are sent: the 14 this
 * form has no field for are copied from `existing` so editing the name never erases the vitamins; the four it shows come from the
 * form (a blank one is `null`). A blank weight per piece or density is `null`, which clears it.
 */
export function ingredientUpdate(data: FormData, existing: Ingredient): { value: UpdateIngredientRequest } | { errors: CustomIngredientErrors } {
  const parsed = parseCustomIngredient(data);
  if ("errors" in parsed) return parsed;
  const form = parsed.value;

  const nutrients = Object.fromEntries(NUTRIENTS.map(({ key }) => [key, existing.nutrients[key]])) as Record<NutrientKey, number | null>;
  for (const key of SHOWN) nutrients[key] = form.nutrients?.[key] ?? null;

  return {
    value: {
      name: form.name,
      category: form.category,
      grams_per_piece: form.grams_per_piece ?? null,
      density_g_per_ml: form.density_g_per_ml ?? null,
      nutrients,
    },
  };
}
