import { parseDecimal } from "@/lib/parse-number";

/** Form field names, prefixed so they never collide with another form's ids on the page. */
export const TARGET_FIELDS = { calories: "target-kcal", protein: "target-protein", carbs: "target-carbs", fat: "target-fat" } as const;
type Key = keyof typeof TARGET_FIELDS;

export type TargetsBody = { target_kcal: number | null; target_protein_g: number | null; target_carbs_g: number | null; target_fat_g: number | null };
export type TargetsErrors = Partial<Record<Key, string>>;

// The API's limits (`UpdateProfileRequest`): calories above 0; macros from 0. A blank field clears the target.
const RULES: Record<Key, { max: number; positive: boolean; over: string; example: string }> = {
  calories: { max: 20000, positive: true, over: "Calories must be more than 0 and at most 20000.", example: "2000" },
  protein: { max: 2000, positive: false, over: "Protein must be at most 2000 g.", example: "70" },
  carbs: { max: 5000, positive: false, over: "Carbohydrates must be at most 5000 g.", example: "250" },
  fat: { max: 2000, positive: false, over: "Fat must be at most 2000 g.", example: "70" },
};

export function parseTargets(data: FormData): { value: TargetsBody } | { errors: TargetsErrors } {
  const errors: TargetsErrors = {};
  const numbers: Record<Key, number | null> = { calories: null, protein: null, carbs: null, fat: null };
  for (const key of Object.keys(TARGET_FIELDS) as Key[]) {
    const rule = RULES[key];
    const parsed = parseDecimal(String(data.get(TARGET_FIELDS[key]) ?? ""));
    if (!parsed.ok) errors[key] = `Enter a number, for example ${rule.example}.`;
    else if (parsed.value !== null && ((rule.positive && parsed.value <= 0) || parsed.value > rule.max)) errors[key] = rule.over;
    else numbers[key] = parsed.value;
  }
  if (Object.keys(errors).length > 0) return { errors };
  return { value: { target_kcal: numbers.calories, target_protein_g: numbers.protein, target_carbs_g: numbers.carbs, target_fat_g: numbers.fat } };
}
