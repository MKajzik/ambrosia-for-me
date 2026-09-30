import { describe, expect, it } from "vitest";
import { CI } from "@/components/ingredient-search/custom-ingredient";
import { NUTRIENTS } from "@/lib/nutrition";
import { makeIngredient, nutrients } from "@/test/fixtures";
import { ingredientUpdate } from "./edit-ingredient";

const existing = makeIngredient({
  id: "c1",
  name: "Granola",
  category: "grains_bread",
  is_custom: true,
  grams_per_piece: 40,
  density_g_per_ml: null,
  nutrients: nutrients({ calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, sodium: 120, iron: 3.5, vitamin_c: null, folate: 30 }),
});

function form(values: Partial<Record<keyof typeof CI, string>>) {
  const data = new FormData();
  const defaults = { name: "Granola", category: "grains_bread", calories: "450", protein: "10", carbohydrates: "60", fat: "15", gramsPerPiece: "40", density: "" };
  for (const [field, value] of Object.entries({ ...defaults, ...values })) data.set(CI[field as keyof typeof CI], value ?? "");
  return data;
}
function value(data: FormData) {
  const result = ingredientUpdate(data, existing);
  if ("errors" in result) throw new Error(`expected a value: ${JSON.stringify(result.errors)}`);
  return result.value;
}

describe("ingredientUpdate", () => {
  it("sends all 18 nutrients, carrying over the ones the form does not show, because the API replaces the whole set", () => {
    const body = value(form({ calories: "470" }));
    expect(Object.keys(body.nutrients ?? {}).sort()).toEqual(NUTRIENTS.map((n) => n.key).sort());
    expect(body.nutrients).toMatchObject({ calories: 470, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, sodium: 120, iron: 3.5, folate: 30 });
    expect(body.nutrients?.vitamin_c).toBeNull();
    expect(body.nutrients?.zinc).toBe(0);
  });

  it("turns a blanked macro into null, which clears it, and leaves the hidden ones alone", () => {
    const body = value(form({ protein: "", fat: "" }));
    expect(body.nutrients).toMatchObject({ protein: null, fat: null, fibre: 7, sodium: 120 });
  });

  it("clears a weight per piece or a density that was blanked, and keeps one that was not", () => {
    expect(value(form({ gramsPerPiece: "", density: "0,9" }))).toMatchObject({ grams_per_piece: null, density_g_per_ml: 0.9 });
    expect(value(form({}))).toMatchObject({ grams_per_piece: 40, density_g_per_ml: null });
  });

  it("carries the name and category", () => {
    expect(value(form({ name: "  Crunchy granola ", category: "sweets_snacks" }))).toMatchObject({ name: "Crunchy granola", category: "sweets_snacks" });
  });

  it("reports the form's own mistakes and sends nothing", () => {
    expect(ingredientUpdate(form({ name: " ", calories: "abc", gramsPerPiece: "0" }), existing)).toEqual({
      errors: { name: "Give the ingredient a name.", calories: "Enter a number, for example 12.5.", gramsPerPiece: "Weight per piece must be more than 0 and at most 10000." },
    });
  });

  it("does not change the ingredient it was given", () => {
    const before = JSON.stringify(existing);
    value(form({ calories: "1" }));
    expect(JSON.stringify(existing)).toBe(before);
  });
});
