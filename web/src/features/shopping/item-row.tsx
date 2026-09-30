"use client";

import { cn } from "cn";
import { Pencil } from "lucide-react";
import { Button } from "@/components/ui/button";
import { formatItemQuantity, type ShoppingItem } from "./items";
import { isOptimistic } from "./list-cache";

type Props = { item: ShoppingItem; checkedByName: string | null; onToggle: (item: ShoppingItem) => void; onEdit: (item: ShoppingItem) => void };

/** One item: a large tick target whose label carries the name, who ticked it and the quantity, and a way to edit. */
export function ItemRow({ item, checkedByName, onToggle, onEdit }: Props) {
  // The server does not know this item's id yet, so neither a tick nor an edit could reach it.
  const unsaved = isOptimistic(item);
  const quantity = formatItemQuantity(item);
  return (
    <li className="flex items-center gap-2 px-3 py-2">
      <label className="flex min-h-10 min-w-0 flex-1 items-center gap-3">
        <input type="checkbox" className="accent-primary size-5 shrink-0" checked={item.checked} disabled={unsaved} onChange={() => onToggle(item)} />
        <span className="min-w-0 flex-1">
          <span className={cn("block truncate", item.checked && "text-muted-foreground line-through")}>{item.name}</span>
          {checkedByName ? <span className="text-muted-foreground block text-xs">Checked by {checkedByName}</span> : null}
        </span>
        {quantity ? <span className="text-muted-foreground shrink-0 text-sm tabular-nums">{quantity}</span> : null}
      </label>
      {unsaved ? null : (
        <Button type="button" variant="ghost" size="icon-sm" aria-label={`Edit ${item.name}`} onClick={() => onEdit(item)}>
          <Pencil aria-hidden />
        </Button>
      )}
    </li>
  );
}
