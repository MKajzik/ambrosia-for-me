"use client";

import { Plus } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { CustomIngredientDialog } from "@/components/ingredient-search/custom-ingredient-dialog";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { ErrorState } from "@/components/error-state";
import { NativeSelect } from "@/components/native-select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel, type IngredientCategory } from "@/lib/ingredient-categories";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { useLoadAllPages } from "@/lib/use-load-all-pages";
import { EditIngredientDialog } from "./edit-ingredient-dialog";
import { useDeleteIngredient, useIngredientBrowse } from "./queries";

/** Find, create, edit and delete my custom ingredients. The catalogue's own ingredients are listed but cannot be changed. */
export function CustomIngredientsPage() {
  const [text, setText] = useState("");
  const [category, setCategory] = useState<IngredientCategory | "">("");
  const [onlyMine, setOnlyMine] = useState(false);
  const [creating, setCreating] = useState(false);
  const [editing, setEditing] = useState<Ingredient | null>(null);
  const [deleting, setDeleting] = useState<Ingredient | null>(null);

  const query = useIngredientBrowse(useDebouncedValue(text, 250), category);
  // The API cannot filter to my own, so this loads every page and filters here. It is opt-in for that reason.
  useLoadAllPages({ ...query, hasNextPage: onlyMine && query.hasNextPage });

  const all = query.data?.pages.flatMap((page) => page.items) ?? [];
  const shown = onlyMine ? all.filter((ingredient) => ingredient.is_custom) : all;
  const newButton = (
    <Button type="button" onClick={() => setCreating(true)}>
      <Plus aria-hidden />
      New custom ingredient
    </Button>
  );

  return (
    <div className="grid gap-4">
      <div className="flex justify-end">{newButton}</div>
      <div className="flex flex-wrap items-end gap-3">
        <div className="grid min-w-48 flex-1 gap-1.5">
          <Label htmlFor="ingredient-search">Search ingredients</Label>
          <Input id="ingredient-search" autoComplete="off" maxLength={100} className="h-11 text-base md:text-sm" value={text} onChange={(event) => setText(event.target.value)} />
        </div>
        <NativeSelect aria-label="Filter by category" value={category} onChange={(event) => setCategory(event.target.value as IngredientCategory | "")}>
          <option value="">All categories</option>
          {CATEGORIES.map((c) => (
            <option key={c} value={c}>
              {categoryLabel(c)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <label className="flex items-center gap-2 text-sm">
        <input type="checkbox" className="accent-primary size-4" checked={onlyMine} onChange={(event) => setOnlyMine(event.target.checked)} />
        Only my custom ingredients
      </label>

      {query.isPending ? (
        <ul aria-label="Loading ingredients" className="grid gap-2">
          {[0, 1, 2].map((n) => (
            <li key={n}>
              <Skeleton className="h-14 rounded-xl" />
            </li>
          ))}
        </ul>
      ) : query.data === undefined ? (
        <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />
      ) : shown.length === 0 && !(onlyMine && query.hasNextPage) ? (
        <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
          <p className="font-medium">{onlyMine ? "You haven't created any custom ingredients yet." : "No ingredient matches."}</p>
          {onlyMine ? newButton : null}
        </div>
      ) : (
        <ul className="grid gap-2">
          {shown.map((ingredient) => (
            <li key={ingredient.id} className="bg-card flex items-center gap-3 rounded-xl border px-4 py-3">
              <div className="min-w-0 flex-1">
                <p className="truncate font-medium">{ingredient.name}</p>
                <p className="text-muted-foreground text-xs">{categoryLabel(ingredient.category)}</p>
              </div>
              {ingredient.is_custom ? (
                <>
                  <Badge variant="secondary">Custom</Badge>
                  <Button type="button" variant="ghost" size="sm" aria-label={`Edit ${ingredient.name}`} onClick={() => setEditing(ingredient)}>
                    Edit
                  </Button>
                  <Button type="button" variant="ghost" size="sm" aria-label={`Delete ${ingredient.name}`} onClick={() => setDeleting(ingredient)}>
                    Delete
                  </Button>
                </>
              ) : null}
            </li>
          ))}
        </ul>
      )}
      {query.data !== undefined && query.hasNextPage && !onlyMine ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}

      <CustomIngredientDialog open={creating} onOpenChange={setCreating} initialName={text.trim()} onCreated={() => toast.success("Ingredient created.")} />
      <EditIngredientDialog key={editing?.id ?? "none"} ingredient={editing} onClose={() => setEditing(null)} />
      <DeleteIngredientDialog key={deleting?.id ?? "none"} ingredient={deleting} onClose={() => setDeleting(null)} />
    </div>
  );
}

function DeleteIngredientDialog({ ingredient, onClose }: { ingredient: Ingredient | null; onClose: () => void }) {
  const remove = useDeleteIngredient();
  return (
    <Dialog open={ingredient !== null} onOpenChange={(open) => !open && onClose()}>
      {ingredient ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this ingredient?</DialogTitle>
            <DialogDescription>
              Delete “{ingredient.name}”? An ingredient a meal still uses cannot be deleted; remove it from those meals first.
            </DialogDescription>
          </DialogHeader>
          {remove.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(remove.error)}
            </p>
          ) : null}
          <div className="flex flex-wrap gap-2">
            <Button
              type="button"
              variant="destructive"
              disabled={remove.isPending}
              onClick={() =>
                remove.mutate(ingredient.id, {
                  onSuccess: () => {
                    toast.success("Ingredient deleted.");
                    onClose();
                  },
                })
              }
            >
              {remove.isPending ? "Deleting…" : "Delete ingredient"}
            </Button>
            <Button type="button" variant="outline" onClick={onClose}>
              Keep it
            </Button>
          </div>
        </DialogContent>
      ) : null}
    </Dialog>
  );
}
