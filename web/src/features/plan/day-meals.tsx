"use client";

import { toast } from "sonner";
import { problemMessage } from "@/lib/api/problem";
import { SLOTS, type PlanEntry } from "./plan-cache";
import { useClearPlanSlot, useSetPlanEntry } from "./queries";
import { SlotRow } from "./slot-row";

/** The four slots of one date, wired to the optimistic plan writes. A refused change is rolled back and explained in a toast. */
export function DayMeals({ date, entries }: { date: string; entries: PlanEntry[] }) {
  const fail = (error: Error) => toast.error(problemMessage(error));
  const set = useSetPlanEntry(fail);
  const clear = useClearPlanSlot(fail);

  return (
    <div className="grid gap-2">
      {SLOTS.map((slot) => (
        <SlotRow
          key={slot}
          slot={slot}
          entries={entries.filter((entry) => entry.slot === slot)}
          onPick={(meal, portion) => set.mutate({ date, slot, mealId: meal.id, mealName: meal.name, portion })}
          onClear={() => clear.mutate({ date, slot })}
        />
      ))}
    </div>
  );
}
