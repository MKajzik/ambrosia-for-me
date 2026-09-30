"use client";

import { ErrorState } from "@/components/error-state";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { BackLink } from "./back-link";
import { MealEditor } from "./meal-editor";
import { MealView } from "./meal-view";
import { useMeal } from "./queries";

/** Loads one meal: my own opens in the editor, the partner's in a read-only view. */
export function MealDetail({ id }: { id: string }) {
  const query = useMeal(id);

  // A background refetch that fails must not replace an editor that already has the meal on screen.
  if (query.data === undefined) {
    return (
      <>
        <BackLink />
        {query.isPending ? (
          <div role="status" className="grid gap-3" aria-label="Loading meal">
            <Skeleton className="h-9 w-48" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        ) : (
          <ErrorState message={problemMessage(query.error)} onRetry={query.error instanceof ApiError && query.error.status === 404 ? undefined : () => void query.refetch()} />
        )}
      </>
    );
  }

  const meal = query.data;
  return (
    <>
      <BackLink />
      <PageHeader title={meal.is_owner ? "Edit meal" : meal.name} />
      {meal.is_owner ? <MealEditor key={meal.id} meal={meal} /> : <MealView meal={meal} />}
    </>
  );
}
