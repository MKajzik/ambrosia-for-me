"use client";

import { Minus, Plus, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { servingsLabel } from "@/features/meals/meal-list";
import { MealPicker } from "./meal-picker";
import { SLOT_LABELS, type PlanEntry, type Slot } from "./plan-cache";

/** The API takes a portion above 0 and up to 100; the stepper moves in halves. */
export const PORTION_STEP = 0.5;
export const MAX_PORTION = 100;

type Choice = { id: string; name: string };
type Props = { slot: Slot; entries: PlanEntry[]; onPick: (meal: Choice, portion: number) => void; onClear: () => void };

const stepped = (portion: number, by: number) => Math.round((portion + by) * 100) / 100;

/**
 * One slot of one day. Breakfast, lunch and dinner hold a single meal that can be swapped, resized or removed. Snacks can only
 * be added or cleared as a group: the API addresses a snack by date and slot alone, so it cannot edit one of several.
 */
export function SlotRow({ slot, entries, onPick, onClear }: Props) {
  const label = SLOT_LABELS[slot];
  const lower = label.toLowerCase();
  // The portion a meal chosen in the open picker will get; null while the picker is closed.
  const [pickerPortion, setPickerPortion] = useState<number | null>(null);
  const [confirmingClear, setConfirmingClear] = useState(false);
  const single = slot === "snack" ? undefined : entries[0];

  return (
    <div className="bg-card grid gap-2 rounded-xl border p-3 sm:grid-cols-[6rem_1fr] sm:items-center">
      <h3 className="text-sm font-medium">{label}</h3>
      <div className="grid gap-2">
        {slot === "snack" ? (
          <>
            {entries.length > 0 ? (
              <ul className="grid gap-1">
                {entries.map((entry) => (
                  <li key={entry.id} className="flex items-center gap-2 text-sm">
                    <Link href={`/meals/${entry.meal_id}`} className="min-w-0 flex-1 truncate font-medium hover:underline">
                      {entry.meal_name}
                    </Link>
                    <span className="text-muted-foreground tabular-nums">{servingsLabel(entry.portion)}</span>
                  </li>
                ))}
              </ul>
            ) : null}
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setPickerPortion(1)}>
                <Plus aria-hidden />
                Add snack
              </Button>
              {entries.length > 0 ? (
                <Button type="button" variant="ghost" size="sm" onClick={() => setConfirmingClear(true)}>
                  Clear snacks
                </Button>
              ) : null}
            </div>
          </>
        ) : single ? (
          <div className="flex flex-wrap items-center gap-2">
            <Link href={`/meals/${single.meal_id}`} className="min-w-0 flex-1 truncate font-medium hover:underline">
              {single.meal_name}
            </Link>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={`Decrease portion of ${single.meal_name}`}
                disabled={single.portion <= PORTION_STEP}
                onClick={() => onPick({ id: single.meal_id, name: single.meal_name }, stepped(single.portion, -PORTION_STEP))}
              >
                <Minus aria-hidden />
              </Button>
              <span className="min-w-20 text-center text-sm tabular-nums">{servingsLabel(single.portion)}</span>
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={`Increase portion of ${single.meal_name}`}
                disabled={single.portion + PORTION_STEP > MAX_PORTION}
                onClick={() => onPick({ id: single.meal_id, name: single.meal_name }, stepped(single.portion, PORTION_STEP))}
              >
                <Plus aria-hidden />
              </Button>
            </div>
            <Button type="button" variant="ghost" size="sm" aria-label={`Swap ${lower} meal`} onClick={() => setPickerPortion(single.portion)}>
              Swap
            </Button>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${single.meal_name} from ${lower}`} onClick={onClear}>
              <X aria-hidden />
            </Button>
          </div>
        ) : (
          <div>
            <Button type="button" variant="outline" size="sm" aria-label={`Add meal for ${lower}`} onClick={() => setPickerPortion(1)}>
              <Plus aria-hidden />
              Add meal
            </Button>
          </div>
        )}
      </div>

      <MealPicker
        open={pickerPortion !== null}
        onOpenChange={(open) => {
          if (!open) setPickerPortion(null);
        }}
        title={slot === "snack" ? "Add a snack" : `Choose ${lower}`}
        onPick={(meal) => onPick(meal, pickerPortion ?? 1)}
      />

      <Dialog open={confirmingClear} onOpenChange={setConfirmingClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Clear all snacks?</DialogTitle>
            <DialogDescription>This removes every snack planned for this day.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingClear(false)}>
              Keep them
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => {
                setConfirmingClear(false);
                onClear();
              }}
            >
              Clear snacks
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
