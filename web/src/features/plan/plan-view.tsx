"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { addDays, formatLongDate, formatWeekRange, startOfWeek, today } from "@/lib/dates";
import { formatAmount, scaleNutrition, sumNutrition, targetProgress, type NutrientAmounts } from "@/lib/nutrition";
import { useToday } from "@/lib/use-today";
import { ApplyTemplateDialog } from "./apply-template-dialog";
import { DayMeals } from "./day-meals";
import type { DailyTotal, Targets } from "./plan-cache";
import { usePlan } from "./queries";

const macroLine = (n: NutrientAmounts) => `Protein ${formatAmount(n.protein, "g")} · Carbs ${formatAmount(n.carbohydrates, "g")} · Fat ${formatAmount(n.fat, "g")}`;

/** One week, Monday to Sunday: totals, and each day's meals with one-tap swaps. */
export function PlanView() {
  const todayDate = useToday();
  const [weekStart, setWeekStart] = useState(() => startOfWeek(today()));
  const [applying, setApplying] = useState(false);
  const plan = usePlan(weekStart, addDays(weekStart, 6));
  const onCurrentWeek = weekStart === startOfWeek(todayDate);

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="icon" aria-label="Previous week" onClick={() => setWeekStart(addDays(weekStart, -7))}>
          <ChevronLeft aria-hidden />
        </Button>
        <p aria-live="polite" className="min-w-36 text-center font-medium">
          {formatWeekRange(weekStart)}
        </p>
        <Button type="button" variant="outline" size="icon" aria-label="Next week" onClick={() => setWeekStart(addDays(weekStart, 7))}>
          <ChevronRight aria-hidden />
        </Button>
        <Button type="button" variant="ghost" disabled={onCurrentWeek} onClick={() => setWeekStart(startOfWeek(todayDate))}>
          This week
        </Button>
        <div className="flex flex-1 flex-wrap justify-end gap-2">
          <Button asChild variant="outline">
            <Link href="/plan/templates">Diet templates</Link>
          </Button>
          <Button type="button" onClick={() => setApplying(true)}>
            Apply template
          </Button>
        </div>
      </div>

      {plan.isPending ? (
        <div role="status" aria-label="Loading the week" className="grid gap-4">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : plan.data === undefined ? (
        <ErrorState message={problemMessage(plan.error)} onRetry={() => void plan.refetch()} />
      ) : (
        <>
          <WeekTotals days={plan.data.days} targets={plan.data.targets} />
          <div className="grid gap-4 lg:grid-cols-2">
            {plan.data.days.map((day) => (
              <DayCard key={day.date} day={day} targets={plan.data.targets} isToday={day.date === todayDate} />
            ))}
          </div>
        </>
      )}

      <ApplyTemplateDialog open={applying} onOpenChange={setApplying} defaultStart={weekStart} onApplied={(start) => setWeekStart(startOfWeek(start))} />
    </div>
  );
}

function WeekTotals({ days, targets }: { days: DailyTotal[]; targets: Targets }) {
  const total = sumNutrition(days.map((day) => day.nutrition_per_day));
  const average = scaleNutrition(total, 1 / 7);
  const progress = targetProgress(average.calories, targets.target_kcal);
  return (
    <section aria-label="Week totals" className="bg-card grid gap-4 rounded-xl border p-4 sm:grid-cols-2">
      <div>
        <h2 className="text-sm font-medium">Week total</h2>
        <p className="text-lg font-semibold tabular-nums">{formatAmount(total.calories, "kcal")}</p>
        <p className="text-muted-foreground text-xs">{macroLine(total)}</p>
      </div>
      <div>
        <h2 className="text-sm font-medium">Daily average</h2>
        <p className="text-lg font-semibold tabular-nums">{formatAmount(average.calories, "kcal")}</p>
        <p className="text-muted-foreground text-xs">{macroLine(average)}</p>
        {progress ? <p className="text-muted-foreground text-xs">{`${progress.percent}% of your ${formatAmount(targets.target_kcal, "kcal")} target`}</p> : null}
      </div>
    </section>
  );
}

function DayCard({ day, targets, isToday }: { day: DailyTotal; targets: Targets; isToday: boolean }) {
  const kcal = day.nutrition_per_day.calories;
  const target = targets.target_kcal !== null && targets.target_kcal > 0 ? ` of ${formatAmount(targets.target_kcal, "kcal")}` : "";
  const heading = formatLongDate(day.date);
  return (
    <section aria-label={heading} className="bg-muted/30 grid content-start gap-3 rounded-xl border p-3">
      <header className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">{heading}</h2>
        {isToday ? <Badge>Today</Badge> : null}
      </header>
      <p className="text-muted-foreground text-sm tabular-nums">
        {formatAmount(kcal, "kcal")}
        {target} · {macroLine(day.nutrition_per_day)}
      </p>
      <DayMeals date={day.date} entries={day.entries} />
    </section>
  );
}
