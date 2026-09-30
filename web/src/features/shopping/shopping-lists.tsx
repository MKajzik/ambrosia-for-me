"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PARTNER_KEY } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { rangeLabel, type ShoppingListSummary } from "./items";
import { useShoppingLists } from "./queries";

type Scope = "mine" | "partner";

export function ShoppingLists({ scope, emptyAction }: { scope: Scope; emptyAction?: React.ReactNode }) {
  const query = useShoppingLists(scope);
  const queryClient = useQueryClient();
  const unlinked = query.error instanceof ApiError && query.error.code === "partner_not_linked";

  // The partner unlinked while their tab was open: refresh the link so the tab goes away.
  useEffect(() => {
    if (unlinked) void queryClient.invalidateQueries({ queryKey: PARTNER_KEY });
  }, [unlinked, queryClient]);

  if (query.isPending) {
    return (
      <ul aria-label="Loading lists" className="grid gap-3">
        {[0, 1, 2].map((n) => (
          <li key={n}>
            <Skeleton className="h-[4.5rem] rounded-xl" />
          </li>
        ))}
      </ul>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={unlinked ? undefined : () => void query.refetch()} />;

  const lists = query.data.pages.flatMap((page) => page.items);
  if (lists.length === 0) return <EmptyState scope={scope} action={emptyAction} />;

  return (
    <div className="grid gap-3">
      <ul className="grid gap-3">
        {lists.map((list) => (
          <ListRow key={list.id} list={list} scope={scope} />
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

function ListRow({ list, scope }: { list: ShoppingListSummary; scope: Scope }) {
  const range = rangeLabel(list);
  return (
    <li className="bg-card flex items-center gap-3 rounded-xl border p-4">
      <Link href={`/shopping/${list.id}`} className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-md outline-none focus-visible:ring-3">
        <span className="block truncate font-medium">{list.name}</span>
        <span className="text-muted-foreground block truncate text-sm">{range ? `From your plan · ${range}` : "Your own list"}</span>
      </Link>
      {scope === "mine" && list.shared_with_partner ? <Badge variant="secondary">Shared</Badge> : null}
      {scope === "partner" ? <Badge variant="secondary">Shared by your partner</Badge> : null}
    </li>
  );
}

function EmptyState({ scope, action }: { scope: Scope; action?: React.ReactNode }) {
  return (
    <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
      <p className="font-medium">{scope === "mine" ? "No shopping lists yet" : "Nothing shared yet"}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {scope === "mine" ? "Generate a list from the meals in your plan, or start an empty one." : "Lists your partner shares with you show up here, and you can both tick things off."}
      </p>
      {scope === "mine" ? action : null}
    </div>
  );
}
