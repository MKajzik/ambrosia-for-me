"use client";

import { Trash2, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { IngredientSearch } from "@/components/ingredient-search/ingredient-search";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { NativeSelect } from "@/components/native-select";
import { NutritionPanel } from "@/components/nutrition-panel";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import { problemMessage } from "@/lib/api/problem";
import { categoryLabel } from "@/lib/ingredient-categories";
import { UNITS, diffDraft, draftFromMeal, hasChanges, newRow, savedFromMeal, validateDraft, type Draft, type DraftRow, type ValidDraft } from "./draft";
import { useDeleteMeal, useMealActions, usePartnerLink, type Meal } from "./queries";
import { useAutosave } from "@/lib/use-autosave";

/**
 * The owner's editor. The draft is copied from `meal` once; after that only its nutrition follows the `meal` prop, which the
 * page keeps on the server's latest answer (each save writes it), so a background refetch never overwrites what is being typed.
 */
export function MealEditor({ meal, autosaveDelayMs }: { meal: Meal; autosaveDelayMs?: number }) {
  const router = useRouter();
  const partner = usePartnerLink();
  const actions = useMealActions(meal.id);
  const deleteMeal = useDeleteMeal();

  const [draft, setDraft] = useState<Draft>(() => draftFromMeal(meal));
  const [saved, setSaved] = useState<ValidDraft>(() => savedFromMeal(meal));
  const savedRef = useRef(saved);
  const commit = useCallback((next: ValidDraft) => {
    savedRef.current = next;
    setSaved(next);
  }, []);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const validation = useMemo(() => validateDraft(draft), [draft]);
  const value = validation.ok ? validation.value : null;
  const dirty = value !== null && hasChanges(saved, value);
  const errors = validation.ok ? null : validation.errors;

  // One save writes what changed: the fields first, then the ingredient list. Each step is committed on its own, so a
  // failure in the second leaves the first counted as saved and only the rest is retried.
  const save = useCallback(
    async (next: ValidDraft) => {
      const { patch, items } = diffDraft(savedRef.current, next);
      if (patch) {
        await actions.patch(patch);
        commit({ ...savedRef.current, name: next.name, notes: next.notes, servings: next.servings, shared: next.shared });
      }
      if (items) {
        await actions.replace(items);
        commit({ ...savedRef.current, items: next.items });
      }
    },
    [actions, commit],
  );
  const autosave = useAutosave({ value, dirty, save, delayMs: autosaveDelayMs });

  const unsaved = dirty || autosave.saving;
  useEffect(() => {
    if (!unsaved) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [unsaved]);

  const setField = <K extends "name" | "notes" | "servings" | "shared">(key: K, next: Draft[K]) => setDraft((d) => ({ ...d, [key]: next }));
  const setRow = (key: string, change: Partial<Pick<DraftRow, "quantity" | "unit">>) =>
    setDraft((d) => ({ ...d, rows: d.rows.map((row) => (row.key === key ? { ...row, ...change } : row)) }));
  const removeRow = (key: string) => setDraft((d) => ({ ...d, rows: d.rows.filter((row) => row.key !== key) }));
  const addIngredient = (ingredient: Ingredient) => setDraft((d) => ({ ...d, rows: [...d.rows, newRow(ingredient)] }));

  const canShare = partner.data?.status === "active" || draft.shared;
  const status = autosave.saving ? "Saving…" : errors ? "Fix the highlighted fields to save." : dirty ? "Unsaved changes" : "All changes saved";

  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-3">
        <p role="status" className="text-muted-foreground text-sm">
          {status}
        </p>
        {autosave.error !== null && dirty ? (
          // Only while there is still something unsaved: putting the draft back to what the server holds clears the failure.
          <div role="alert" className="bg-destructive/10 text-destructive flex flex-wrap items-center gap-3 rounded-lg px-3 py-2 text-sm">
            <span>{problemMessage(autosave.error)}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => void autosave.retry()}>
              Try again
            </Button>
          </div>
        ) : null}
      </div>

      <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem] lg:items-start">
        <div className="grid gap-6">
          <section aria-label="Meal details" className="grid gap-4">
            <Field name="meal-name" label="Name" autoComplete="off" value={draft.name} onChange={(e) => setField("name", e.target.value)} error={errors?.name} />
            <Field
              name="meal-servings"
              label="Servings"
              inputMode="decimal"
              value={draft.servings}
              onChange={(e) => setField("servings", e.target.value)}
              hint="How many portions this makes. Nutrition is shown per serving."
              error={errors?.servings}
            />
            <div className="grid gap-1.5">
              <Label htmlFor="meal-notes">Notes</Label>
              <Textarea id="meal-notes" rows={3} value={draft.notes} onChange={(e) => setField("notes", e.target.value)} aria-invalid={errors?.notes ? true : undefined} aria-describedby={errors?.notes ? "meal-notes-error" : undefined} />
              {errors?.notes ? (
                <p id="meal-notes-error" className="text-destructive text-sm">
                  {errors.notes}
                </p>
              ) : null}
            </div>
            {canShare ? (
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="accent-primary size-4" checked={draft.shared} onChange={(e) => setField("shared", e.target.checked)} />
                Share with my partner
              </label>
            ) : null}
          </section>

          <section aria-labelledby="meal-ingredients" className="grid gap-4">
            <h2 id="meal-ingredients" className="text-lg font-semibold">
              Ingredients
            </h2>
            <IngredientSearch onSelect={addIngredient} />
            {errors?.ingredients ? <p className="text-destructive text-sm">{errors.ingredients}</p> : null}
            {draft.rows.length === 0 ? (
              <p className="text-muted-foreground text-sm">No ingredients yet. Search above to add one.</p>
            ) : (
              <ul className="grid gap-2">
                {draft.rows.map((row) => {
                  const rowError = errors?.rows[row.key];
                  return (
                    <li key={row.key} className="bg-card grid gap-2 rounded-xl border p-3">
                      <div className="flex items-center gap-2">
                        <div className="min-w-0 flex-1">
                          <p className="truncate font-medium">{row.name}</p>
                          <p className="text-muted-foreground text-xs">{categoryLabel(row.category)}</p>
                        </div>
                        <Input
                          aria-label={`Quantity of ${row.name}`}
                          aria-invalid={rowError ? true : undefined}
                          aria-describedby={rowError ? `${row.key}-error` : undefined}
                          inputMode="decimal"
                          className="h-11 w-24 text-base md:text-sm"
                          value={row.quantity}
                          onChange={(e) => setRow(row.key, { quantity: e.target.value })}
                        />
                        <NativeSelect aria-label={`Unit for ${row.name}`} value={row.unit} onChange={(e) => setRow(row.key, { unit: e.target.value as DraftRow["unit"] })}>
                          {UNITS.map((unit) => (
                            <option key={unit} value={unit}>
                              {unit}
                            </option>
                          ))}
                        </NativeSelect>
                        <Button type="button" variant="ghost" size="icon" aria-label={`Remove ${row.name}`} onClick={() => removeRow(row.key)}>
                          <X aria-hidden />
                        </Button>
                      </div>
                      {rowError ? (
                        <p id={`${row.key}-error`} className="text-destructive text-sm">
                          {rowError}
                        </p>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            )}
          </section>
        </div>

        <aside className="grid gap-4 lg:sticky lg:top-6">
          <NutritionPanel nutrition={meal.nutrition_per_serving} stale={unsaved} />
          <Button
            type="button"
            variant="destructive"
            onClick={() => {
              deleteMeal.reset();
              setConfirmingDelete(true);
            }}
          >
            <Trash2 aria-hidden />
            Delete meal
          </Button>
        </aside>
      </div>

      <Dialog open={confirmingDelete} onOpenChange={setConfirmingDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this meal?</DialogTitle>
            <DialogDescription>“{meal.name}” will be removed from your library. This can&apos;t be undone.</DialogDescription>
          </DialogHeader>
          {deleteMeal.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(deleteMeal.error)}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingDelete(false)}>
              Keep it
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteMeal.isPending}
              onClick={() =>
                deleteMeal.mutate(meal.id, {
                  onSuccess: () => {
                    toast.success("Meal deleted.");
                    router.replace("/meals");
                  },
                })
              }
            >
              {deleteMeal.isPending ? "Deleting…" : "Delete meal"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
