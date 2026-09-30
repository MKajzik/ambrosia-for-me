import { describe, expect, it } from "vitest";
import { CI, parseCustomIngredient } from "./custom-ingredient";

function form(values: Partial<Record<keyof typeof CI, string>>) {
  const data = new FormData();
  const defaults = { name: "Kale", category: "produce" };
  for (const [field, value] of Object.entries({ ...defaults, ...values })) data.set(CI[field as keyof typeof CI], value ?? "");
  return data;
}

describe("parseCustomIngredient", () => {
  it("sends only what was filled in, and no nutrients object when none were", () => {
    expect(parseCustomIngredient(form({}))).toEqual({ value: { name: "Kale", category: "produce" } });
    expect(parseCustomIngredient(form({ calories: "49", protein: "4,3" }))).toEqual({
      value: { name: "Kale", category: "produce", nutrients: { calories: 49, protein: 4.3 } },
    });
  });

  it("keeps a genuine zero nutrient but drops a blank one", () => {
    expect(parseCustomIngredient(form({ calories: "0", fat: "" }))).toEqual({
      value: { name: "Kale", category: "produce", nutrients: { calories: 0 } },
    });
  });

  it("carries the weight per piece and the density when given", () => {
    expect(parseCustomIngredient(form({ gramsPerPiece: "120", density: "0,95" }))).toEqual({
      value: { name: "Kale", category: "produce", grams_per_piece: 120, density_g_per_ml: 0.95 },
    });
  });

  it("flags each bad field and sends nothing", () => {
    const result = parseCustomIngredient(form({ name: "  ", calories: "abc", protein: "101", gramsPerPiece: "0", density: "4" }));
    expect(result).toEqual({
      errors: {
        name: "Give the ingredient a name.",
        calories: "Enter a number, for example 12.5.",
        protein: "That is more than 100 per 100 g.",
        gramsPerPiece: "Weight per piece must be more than 0 and at most 10000.",
        density: "Density must be more than 0 and at most 3.",
      },
    });
  });

  it("rejects a name over 200 characters", () => {
    expect(parseCustomIngredient(form({ name: "x".repeat(201) }))).toEqual({ errors: { name: "Use at most 200 characters." } });
  });
});
