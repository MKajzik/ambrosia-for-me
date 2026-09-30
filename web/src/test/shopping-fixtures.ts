import type { components } from "@/lib/api/schema.gen";

type S = components["schemas"];
const STAMP = "2026-01-01T00:00:00Z";

export function makeItem(over: Partial<S["ShoppingItem"]> = {}): S["ShoppingItem"] {
  return {
    id: "i1",
    list_id: "l1",
    ingredient_id: null,
    name: "Milk",
    quantity: null,
    unit: null,
    category: "dairy_eggs",
    checked: false,
    checked_by: null,
    position: 0,
    version: 1,
    origin: "manual",
    created_at: STAMP,
    updated_at: STAMP,
    ...over,
  };
}

export function makeList(over: Partial<S["ShoppingList"]> = {}): S["ShoppingList"] {
  return { id: "l1", name: "Weekly shop", shared_with_partner: false, is_owner: true, source_from: null, source_to: null, items: [], created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeListSummary(over: Partial<S["ShoppingListSummary"]> = {}): S["ShoppingListSummary"] {
  return { id: "l1", name: "Weekly shop", shared_with_partner: false, source_from: null, source_to: null, created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeUser(over: Partial<S["User"]> = {}): S["User"] {
  return { id: "u1", email: "ann@example.test", display_name: "Ann", target_kcal: null, target_protein_g: null, target_carbs_g: null, target_fat_g: null, created_at: STAMP, updated_at: STAMP, ...over };
}
