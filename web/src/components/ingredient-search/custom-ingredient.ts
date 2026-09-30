import { CATEGORIES, type IngredientCategory } from "@/lib/ingredient-categories";
import { parseDecimal } from "@/lib/parse-number";
import type { CreateIngredientRequest } from "./use-ingredient-search";

/** Form field names. Prefixed so they never collide with another form's ids on the same page. */
export const CI = {
  name: "ci-name",
  category: "ci-category",
  calories: "ci-calories",
  protein: "ci-protein",
  carbohydrates: "ci-carbohydrates",
  fat: "ci-fat",
  gramsPerPiece: "ci-grams-per-piece",
  density: "ci-density",
} as const;

export type CustomIngredientErrors = Partial<Record<keyof typeof CI, string>>;

const NUMBER_HELP = "Enter a number, for example 12.5.";
const NUTRIENT_FIELDS = ["calories", "protein", "carbohydrates", "fat"] as const;
const NUTRIENT_MAX = { calories: 1000, protein: 100, carbohydrates: 100, fat: 100 } as const;

function positive(text: string, max: number, what: string): { value?: number; error?: string } {
  const parsed = parseDecimal(text);
  if (!parsed.ok) return { error: NUMBER_HELP };
  if (parsed.value === null) return {};
  if (parsed.value <= 0 || parsed.value > max) return { error: `${what} must be more than 0 and at most ${max}.` };
  return { value: parsed.value };
}

/** Reads the custom-ingredient form. Only the nutrients actually entered are sent: an omitted one stays unknown, not zero. */
export function parseCustomIngredient(data: FormData): { value: CreateIngredientRequest } | { errors: CustomIngredientErrors } {
  const text = (field: string) => String(data.get(field) ?? "");
  const errors: CustomIngredientErrors = {};

  const name = text(CI.name).trim();
  if (!name) errors.name = "Give the ingredient a name.";
  else if (name.length > 200) errors.name = "Use at most 200 characters.";

  const rawCategory = text(CI.category);
  const category: IngredientCategory = (CATEGORIES as string[]).includes(rawCategory) ? (rawCategory as IngredientCategory) : "other";

  const nutrients: NonNullable<CreateIngredientRequest["nutrients"]> = {};
  for (const key of NUTRIENT_FIELDS) {
    const parsed = parseDecimal(text(CI[key]));
    if (!parsed.ok) errors[key] = NUMBER_HELP;
    else if (parsed.value !== null && parsed.value > NUTRIENT_MAX[key]) errors[key] = `That is more than ${NUTRIENT_MAX[key]} per 100 g.`;
    else if (parsed.value !== null) nutrients[key] = parsed.value;
  }

  const piece = positive(text(CI.gramsPerPiece), 10000, "Weight per piece");
  if (piece.error) errors.gramsPerPiece = piece.error;
  const density = positive(text(CI.density), 3, "Density");
  if (density.error) errors.density = density.error;

  if (Object.keys(errors).length > 0) return { errors };

  const value: CreateIngredientRequest = { name, category };
  if (piece.value !== undefined) value.grams_per_piece = piece.value;
  if (density.value !== undefined) value.density_g_per_ml = density.value;
  if (Object.keys(nutrients).length > 0) value.nutrients = nutrients;
  return { value };
}
