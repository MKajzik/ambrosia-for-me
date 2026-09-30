"use client";

import Link from "next/link";
import { ErrorState } from "@/components/error-state";
import { MacroRings } from "@/components/macro-rings";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { formatLongDate } from "@/lib/dates";
import { zeroNutrition } from "@/lib/nutrition";
import { useToday } from "@/lib/use-today";
import { DayMeals } from "./day-meals";
import { usePlan } from "./queries";

/** Today's rings against the targets, and today's meals with one-tap swaps and portion changes. */
export function TodayView() {
  const date = useToday();
  const plan = usePlan(date, date);

  if (plan.isPending) {
    return (
      <div role="status" aria-label="Loading today" className="grid gap-4">
        <Skeleton className="h-52 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (plan.data === undefined) return <ErrorState message={problemMessage(plan.error)} onRetry={() => void plan.refetch()} />;

  const day = plan.data.days.find((d) => d.date === date);
  const entries = day?.entries ?? [];

  return (
    <div className="grid gap-6">
      <p className="text-muted-foreground -mt-4 text-sm">{formatLongDate(date)}</p>
      <MacroRings nutrition={day?.nutrition_per_day ?? zeroNutrition()} targets={plan.data.targets} label="Today's totals" stale={plan.isFetching} />
      {entries.length === 0 ? (
        <div className="bg-card flex flex-col items-start gap-3 rounded-xl border p-4">
          <p className="text-sm">Nothing planned for today yet. Add a meal below, or apply a diet template to a whole week.</p>
          <Button asChild variant="outline" size="sm">
            <Link href="/plan">Apply a diet template</Link>
          </Button>
        </div>
      ) : null}
      <section aria-labelledby="today-meals">
        <h2 id="today-meals" className="sr-only">
          Meals
        </h2>
        <DayMeals date={date} entries={entries} />
      </section>
    </div>
  );
}
