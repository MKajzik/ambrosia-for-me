import type { components } from "@/lib/api/schema.gen";
import { nutrients } from "./fixtures";

type S = components["schemas"];
const STAMP = "2026-01-01T00:00:00Z";

export function makeEntry(over: Partial<S["PlanEntry"]> = {}): S["PlanEntry"] {
  return {
    id: "e1",
    date: "2026-09-28",
    slot: "breakfast",
    meal_id: "m1",
    meal_name: "Oat bowl",
    portion: 1,
    from_template_id: null,
    created_at: STAMP,
    updated_at: STAMP,
    ...over,
  };
}

export function makeDay(date: string, entries: S["PlanEntry"][] = [], nutrition: S["NutrientAmounts"] = nutrients()): S["DailyTotal"] {
  return { date, entries, nutrition_per_day: nutrition };
}

export const TARGETS: S["Targets"] = { target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: 70 };

export function makePlan(from: string, to: string, days: S["DailyTotal"][] = [], targets: S["Targets"] = TARGETS): S["PlanRange"] {
  return { from, to, days, targets };
}

export function makeTemplateSlot(over: Partial<S["TemplateSlot"]> = {}): S["TemplateSlot"] {
  return { id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1, ...over };
}

export function makeTemplate(over: Partial<S["DietTemplate"]> = {}): S["DietTemplate"] {
  return { id: "t1", name: "Base week", day_count: 7, shared_with_partner: false, is_owner: true, slots: [], created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeTemplateSummary(over: Partial<S["DietTemplateSummary"]> = {}): S["DietTemplateSummary"] {
  return { id: "t1", name: "Base week", day_count: 7, shared_with_partner: false, created_at: STAMP, updated_at: STAMP, ...over };
}
