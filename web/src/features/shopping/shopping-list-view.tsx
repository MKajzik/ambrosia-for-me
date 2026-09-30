"use client";

import { ArrowLeft, Settings } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/use-me";
import { usePartnerLink } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { categoryLabel } from "@/lib/ingredient-categories";
import { EditItemDialog } from "./edit-item-dialog";
import { checkedCount, groupByCategory, rangeLabel } from "./items";
import { ItemRow } from "./item-row";
import { ListSettingsDialog } from "./list-settings-dialog";
import { QuickAdd } from "./quick-add";
import { useAddItem, useCheckItem, useShoppingList } from "./queries";
import { useListEvents } from "./use-list-events";

function BackToShopping() {
  return (
    <Link href="/shopping" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
      <ArrowLeft aria-hidden className="size-4" />
      Shopping
    </Link>
  );
}

/** One list: items by aisle, tick-off, quick-add and editing, kept live while it is open. */
export function ShoppingListView({ id }: { id: string }) {
  const query = useShoppingList(id);
  const me = useMe();
  const partner = usePartnerLink();
  const [deleted, setDeleted] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  useListEvents(id, () => setDeleted(true));

  const fail = (error: Error) => toast.error(problemMessage(error));
  const userId = me.data?.id ?? null;
  const check = useCheckItem(id, userId, fail);
  const add = useAddItem(id, fail);

  // A 404 means access is gone, even while items are still cached (a background refetch reads the same answer): never show them.
  if (deleted || (query.error instanceof ApiError && query.error.status === 404)) {
    return (
      <>
        <BackToShopping />
        <ErrorState message={deleted ? "This list was deleted." : "That list isn't available. It may have been deleted, or it is no longer shared with you."} />
      </>
    );
  }
  if (query.data === undefined) {
    return (
      <>
        <BackToShopping />
        {query.isPending ? (
          <div role="status" aria-label="Loading list" className="grid gap-3">
            <Skeleton className="h-9 w-48" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        ) : (
          <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />
        )}
      </>
    );
  }

  const list = query.data;
  const partnerName = partner.data?.status === "active" ? partner.data.display_name : null;
  const { done, total } = checkedCount(list.items);
  const range = rangeLabel(list);
  const editing = list.items.find((item) => item.id === editingId) ?? null;

  return (
    <>
      <BackToShopping />
      <PageHeader title={list.name} description={range ? `From your plan · ${range}` : undefined} />
      <div className="grid gap-6">
        <div className="flex flex-wrap items-center gap-3">
          {list.is_owner && list.shared_with_partner ? <Badge variant="secondary">Shared with {partnerName ?? "your partner"}</Badge> : null}
          {!list.is_owner ? <Badge variant="secondary">{partnerName ? `${partnerName}’s list` : "Your partner’s list"}</Badge> : null}
          <p className="text-muted-foreground text-sm">
            {done} of {total} checked
          </p>
          {list.is_owner ? (
            <Button type="button" variant="outline" size="sm" className="ml-auto" onClick={() => setSettingsOpen(true)}>
              <Settings aria-hidden />
              List settings
            </Button>
          ) : null}
        </div>

        <QuickAdd
          onAddText={(name) => add.mutate({ name })}
          onAddIngredient={(ingredient) => add.mutate({ name: ingredient.name, ingredientId: ingredient.id, category: ingredient.category })}
        />

        {total === 0 ? (
          <p className="text-muted-foreground text-sm">No items yet. Add one above.</p>
        ) : (
          <div className="grid gap-4">
            {groupByCategory(list.items).map((group) => (
              <section key={group.category} aria-label={categoryLabel(group.category)} className="bg-card overflow-hidden rounded-xl border">
                <h2 className="bg-muted/40 px-3 py-2 text-xs font-medium tracking-wide uppercase">{categoryLabel(group.category)}</h2>
                <ul className="divide-y">
                  {group.items.map((item) => (
                    <ItemRow
                      key={item.id}
                      item={item}
                      checkedByName={item.checked && item.checked_by !== null && userId !== null && item.checked_by !== userId ? (partnerName ?? "your partner") : null}
                      onToggle={(target) => check.mutate({ itemId: target.id, checked: !target.checked })}
                      onEdit={(target) => setEditingId(target.id)}
                    />
                  ))}
                </ul>
              </section>
            ))}
          </div>
        )}
      </div>

      <EditItemDialog key={editing?.id ?? "none"} item={editing} listId={id} onClose={() => setEditingId(null)} />
      {list.is_owner ? <ListSettingsDialog list={list} open={settingsOpen} onOpenChange={setSettingsOpen} /> : null}
    </>
  );
}
