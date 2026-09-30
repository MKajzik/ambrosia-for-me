"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CopyMealButton } from "./copy-meal-button";
import { PARTNER_KEY, useMeals, type MealSummary } from "./queries";

type Scope = "mine" | "partner";

export function servingsLabel(servings: number): string {
  return `${servings} ${servings === 1 ? "serving" : "servings"}`;
}

export function MealList({ scope }: { scope: Scope }) {
  const query = useMeals(scope);
  const queryClient = useQueryClient();
  const unlinked = query.error instanceof ApiError && query.error.code === "partner_not_linked";

  // The partner unlinked while their tab was open: refresh the link so the tab goes away.
  useEffect(() => {
    if (unlinked) void queryClient.invalidateQueries({ queryKey: PARTNER_KEY });
  }, [unlinked, queryClient]);

  if (query.isPending) {
    return (
      <ul aria-label="Loading meals" className="grid gap-3">
        {[0, 1, 2].map((n) => (
          <li key={n}>
            <Skeleton className="h-[4.5rem] rounded-xl" />
          </li>
        ))}
      </ul>
    );
  }
  if (query.data === undefined) {
    return <ErrorState message={problemMessage(query.error)} onRetry={unlinked ? undefined : () => void query.refetch()} />;
  }

  const meals = query.data.pages.flatMap((page) => page.items);
  if (meals.length === 0) return <EmptyState scope={scope} />;

  return (
    <div className="grid gap-3">
      <ul className="grid gap-3">
        {meals.map((meal) => (
          <MealRow key={meal.id} meal={meal} scope={scope} />
        ))}
      </ul>
      {query.isError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
      {query.hasNextPage ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}

function MealRow({ meal, scope }: { meal: MealSummary; scope: Scope }) {
  return (
    <li className="bg-card flex items-center gap-3 rounded-xl border p-4">
      <Link href={`/meals/${meal.id}`} className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-md outline-none focus-visible:ring-3">
        <span className="block truncate font-medium">{meal.name}</span>
        <span className="text-muted-foreground block truncate text-sm">
          {servingsLabel(meal.servings)}
          {meal.notes ? ` · ${meal.notes}` : ""}
        </span>
      </Link>
      {scope === "mine" && meal.shared_with_partner ? <Badge variant="secondary">Shared</Badge> : null}
      {scope === "partner" ? <CopyMealButton mealId={meal.id} mealName={meal.name} /> : null}
    </li>
  );
}

function EmptyState({ scope }: { scope: Scope }) {
  return (
    <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
      <p className="font-medium">{scope === "mine" ? "No meals yet" : "Nothing shared yet"}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {scope === "mine"
          ? "Build a meal from ingredients to see its calories and nutrients."
          : "Meals your partner shares with you show up here, and you can copy them into your library."}
      </p>
      {scope === "mine" ? (
        <Button asChild>
          <Link href="/meals/new">Create your first meal</Link>
        </Button>
      ) : null}
    </div>
  );
}
