import { formatMonthDay } from "@/lib/dates";
import type { components } from "@/lib/api/schema.gen";
import { CATEGORIES, type IngredientCategory } from "@/lib/ingredient-categories";

type Schemas = components["schemas"];
export type ShoppingItem = Schemas["ShoppingItem"];
export type ShoppingList = Schemas["ShoppingList"];
export type ShoppingListSummary = Schemas["ShoppingListSummary"];

export type ItemGroup = { category: IngredientCategory; items: ShoppingItem[] };

/** The items by aisle, in the order of the category catalogue. Within an aisle what is still to buy comes first. */
export function groupByCategory(items: readonly ShoppingItem[]): ItemGroup[] {
  const byCategory = new Map<IngredientCategory, ShoppingItem[]>();
  for (const item of items) {
    const group = byCategory.get(item.category) ?? [];
    group.push(item);
    byCategory.set(item.category, group);
  }
  return CATEGORIES.flatMap((category) => {
    const group = byCategory.get(category);
    if (!group) return [];
    return [{ category, items: [...group].sort((a, b) => Number(a.checked) - Number(b.checked) || a.position - b.position) }];
  });
}

export function checkedCount(items: readonly ShoppingItem[]): { done: number; total: number } {
  return { done: items.filter((item) => item.checked).length, total: items.length };
}

export function formatItemQuantity(item: { quantity: number | null; unit: "g" | "ml" | "piece" | null }): string {
  if (item.quantity === null) return "";
  const amount = item.quantity.toLocaleString("en-US", { maximumFractionDigits: 2 });
  if (item.unit === null) return amount;
  if (item.unit === "piece") return `${amount} ${item.quantity === 1 ? "piece" : "pieces"}`;
  return `${amount} ${item.unit}`;
}

/** The plan dates a list was generated from, or null for a list built by hand. */
export function rangeLabel(list: { source_from: string | null; source_to: string | null }): string | null {
  if (!list.source_from || !list.source_to) return null;
  return `${formatMonthDay(list.source_from)} – ${formatMonthDay(list.source_to)}`;
}
