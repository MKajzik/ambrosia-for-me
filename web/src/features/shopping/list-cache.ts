import type { IngredientCategory } from "@/lib/ingredient-categories";
import type { ShoppingItem, ShoppingList } from "./items";

export const OPTIMISTIC_PREFIX = "optimistic:";

/** An item the server has not answered for yet: it can be seen, but not checked or edited, because the server does not know its id. */
export const isOptimistic = (item: ShoppingItem): boolean => item.id.startsWith(OPTIMISTIC_PREFIX);

const byPosition = (a: ShoppingItem, b: ShoppingItem) => a.position - b.position;

/** Checks or unchecks an item. Only the server bumps `version`, so it is left alone. */
export function withChecked(list: ShoppingList, itemId: string, checked: boolean, by: string | null): ShoppingList {
  return { ...list, items: list.items.map((item) => (item.id === itemId ? { ...item, checked, checked_by: checked ? by : null } : item)) };
}

export function withAddedItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  return { ...list, items: [...list.items, item] };
}

export function withoutItem(list: ShoppingList, itemId: string): ShoppingList {
  return list.items.some((item) => item.id === itemId) ? { ...list, items: list.items.filter((item) => item.id !== itemId) } : list;
}

/** Takes in what the server says about an item, unless the cache already holds a newer version of it. */
export function withItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  const existing = list.items.find((i) => i.id === item.id);
  if (!existing) return { ...list, items: [...list.items, item].sort(byPosition) };
  if (existing.version > item.version) return list;
  return { ...list, items: list.items.map((i) => (i.id === item.id ? item : i)) };
}

/** Puts back exactly `item`, whatever the versions, for a rollback. */
export function restoreItem(list: ShoppingList, item: ShoppingItem): ShoppingList {
  return list.items.some((i) => i.id === item.id)
    ? { ...list, items: list.items.map((i) => (i.id === item.id ? item : i)) }
    : { ...list, items: [...list.items, item].sort(byPosition) };
}

export function optimisticItem(listId: string, input: { name: string; ingredientId?: string; category?: IngredientCategory }, position: number, id: string): ShoppingItem {
  const now = new Date().toISOString();
  return {
    id,
    list_id: listId,
    ingredient_id: input.ingredientId ?? null,
    name: input.name,
    quantity: null,
    unit: null,
    category: input.category ?? "other",
    checked: false,
    checked_by: null,
    position,
    version: 1,
    origin: "manual",
    created_at: now,
    updated_at: now,
  };
}

/** An event at or below the version already held describes something the cache has already seen (often this person's own change). */
export function isStaleEvent(list: ShoppingList | undefined, itemId: string, version: number): boolean {
  const held = list?.items.find((item) => item.id === itemId);
  return held !== undefined && held.version >= version;
}
