"use client";

import Link from "next/link";
import { useState } from "react";
import { ErrorState } from "@/components/error-state";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { servingsLabel } from "@/features/meals/meal-list";
import { useMeals, type MealSummary } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { useLoadAllPages } from "@/lib/use-load-all-pages";

type Props = { open: boolean; onOpenChange: (open: boolean) => void; title: string; onPick: (meal: MealSummary) => void };

/** Choose one of my meals for a slot. The list is filtered in the browser over all of my meals, which load a page at a time. */
export function MealPicker({ open, onOpenChange, title, onPick }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Choose one of your meals.</DialogDescription>
        </DialogHeader>
        <PickerBody
          onPick={(meal) => {
            onPick(meal);
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts with an empty filter every time. */
function PickerBody({ onPick }: { onPick: (meal: MealSummary) => void }) {
  const query = useMeals("mine");
  const [filter, setFilter] = useState("");
  useLoadAllPages(query);

  if (query.isPending) {
    return (
      <div role="status" aria-label="Loading meals" className="grid gap-2">
        <Skeleton className="h-10 rounded-lg" />
        <Skeleton className="h-10 rounded-lg" />
      </div>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />;

  const meals = query.data.pages.flatMap((page) => page.items);
  if (meals.length === 0 && !query.hasNextPage) {
    return (
      <p className="text-muted-foreground text-sm">
        You have no meals yet.{" "}
        <Link href="/meals/new" className="text-primary font-medium underline-offset-4 hover:underline">
          Create a meal
        </Link>
      </p>
    );
  }

  const needle = filter.trim().toLowerCase();
  const shown = meals.filter((meal) => meal.name.toLowerCase().includes(needle));

  return (
    <div className="grid gap-3">
      <div className="grid gap-1.5">
        <Label htmlFor="meal-picker-filter">Search your meals</Label>
        <Input id="meal-picker-filter" autoComplete="off" className="h-11 text-base md:text-sm" value={filter} onChange={(event) => setFilter(event.target.value)} />
      </div>
      {shown.length === 0 ? (
        <p className="text-muted-foreground text-sm">No meal matches “{filter.trim()}”.</p>
      ) : (
        <ul className="grid max-h-80 gap-1 overflow-y-auto">
          {shown.map((meal) => (
            <li key={meal.id}>
              <button
                type="button"
                onClick={() => onPick(meal)}
                className="hover:bg-muted focus-visible:ring-ring/50 flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm outline-none focus-visible:ring-3"
              >
                <span className="truncate font-medium">{meal.name}</span>
                <span className="text-muted-foreground shrink-0">{servingsLabel(meal.servings)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {query.hasNextPage && !query.isFetchNextPageError ? (
        <p role="status" className="text-muted-foreground text-xs">
          Loading more meals…
        </p>
      ) : null}
      {query.isFetchNextPageError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
    </div>
  );
}
