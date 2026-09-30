import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type Slot = Schemas["Slot"];
export type PlanEntry = Schemas["PlanEntry"];
export type DailyTotal = Schemas["DailyTotal"];
export type PlanRange = Schemas["PlanRange"];
export type Targets = Schemas["Targets"];

/** The four slots of a day, in the order a day reads. */
export const SLOTS: readonly Slot[] = ["breakfast", "lunch", "dinner", "snack"];
export const SLOT_LABELS: Record<Slot, string> = { breakfast: "Breakfast", lunch: "Lunch", dinner: "Dinner", snack: "Snacks" };

export type SlotChange = { date: string; slot: Slot; mealId: string; mealName: string; portion: number };

/**
 * The plan as it will look once the server accepts `change`. Breakfast, lunch and dinner hold one meal each, so they are
 * replaced; a snack is always added, because the API can only add snacks by slot. Totals are left as they were: only the
 * server can compute them, and the mutation refetches them when it settles.
 */
export function withEntry(range: PlanRange, change: SlotChange): PlanRange {
  return {
    ...range,
    days: range.days.map((day) => {
      if (day.date !== change.date) return day;
      const existing = change.slot === "snack" ? undefined : day.entries.find((entry) => entry.slot === change.slot);
      const now = new Date().toISOString();
      const entry: PlanEntry = {
        id: existing?.id ?? `optimistic:${change.date}:${change.slot}:${day.entries.length}`,
        date: change.date,
        slot: change.slot,
        meal_id: change.mealId,
        meal_name: change.mealName,
        portion: change.portion,
        from_template_id: null,
        created_at: existing?.created_at ?? now,
        updated_at: now,
      };
      return { ...day, entries: existing ? day.entries.map((e) => (e === existing ? entry : e)) : [...day.entries, entry] };
    }),
  };
}

/** Removes every entry of `slot` on `date`: the one meal, or all the snacks, exactly as `DELETE /plan/{date}/{slot}` does. */
export function withoutSlot(range: PlanRange, date: string, slot: Slot): PlanRange {
  return { ...range, days: range.days.map((day) => (day.date === date ? { ...day, entries: day.entries.filter((entry) => entry.slot !== slot) } : day)) };
}
