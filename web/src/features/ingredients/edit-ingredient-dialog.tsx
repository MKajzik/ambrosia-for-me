"use client";

import { useState } from "react";
import { toast } from "sonner";
import { CI, type CustomIngredientErrors } from "@/components/ingredient-search/custom-ingredient";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel } from "@/lib/ingredient-categories";
import { ingredientUpdate } from "./edit-ingredient";
import { useUpdateIngredient } from "./queries";

const text = (value: number | null) => (value === null ? "" : String(value));

/** Edit one of my custom ingredients. Mount it with a `key` per ingredient so it starts fresh. */
export function EditIngredientDialog({ ingredient, onClose }: { ingredient: Ingredient | null; onClose: () => void }) {
  return (
    <Dialog open={ingredient !== null} onOpenChange={(open) => !open && onClose()}>
      {ingredient ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit custom ingredient</DialogTitle>
            <DialogDescription>Amounts are per 100 g. Nutrients not shown here are kept as they are.</DialogDescription>
          </DialogHeader>
          <EditForm ingredient={ingredient} onDone={onClose} />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

function EditForm({ ingredient, onDone }: { ingredient: Ingredient; onDone: () => void }) {
  const update = useUpdateIngredient();
  const [errors, setErrors] = useState<CustomIngredientErrors>({});
  const [failure, setFailure] = useState<string | null>(null);

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const result = ingredientUpdate(new FormData(event.currentTarget), ingredient);
        if ("errors" in result) return setErrors(result.errors);
        setErrors({});
        setFailure(null);
        update.mutate(
          { id: ingredient.id, body: result.value },
          {
            onSuccess: () => {
              toast.success("Ingredient saved.");
              onDone();
            },
            onError: (error) => (error instanceof ApiError && error.code === "unit_not_convertible" ? setFailure(problemMessage(error)) : toast.error(problemMessage(error))),
          },
        );
      }}
    >
      <Field name={CI.name} label="Ingredient name" defaultValue={ingredient.name} maxLength={200} error={errors.name} autoComplete="off" />
      <div className="grid gap-1.5">
        <Label htmlFor={CI.category}>Category</Label>
        <NativeSelect id={CI.category} name={CI.category} defaultValue={ingredient.category}>
          {CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {categoryLabel(category)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.calories} label="Calories per 100 g (kcal)" inputMode="decimal" defaultValue={text(ingredient.nutrients.calories)} error={errors.calories} />
        <Field name={CI.protein} label="Protein per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.protein)} error={errors.protein} />
        <Field name={CI.carbohydrates} label="Carbohydrates per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.carbohydrates)} error={errors.carbohydrates} />
        <Field name={CI.fat} label="Fat per 100 g (g)" inputMode="decimal" defaultValue={text(ingredient.nutrients.fat)} error={errors.fat} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.gramsPerPiece} label="Weight of one piece (g)" inputMode="decimal" defaultValue={text(ingredient.grams_per_piece)} hint="Lets you use pieces." error={errors.gramsPerPiece} />
        <Field name={CI.density} label="Density (g per ml)" inputMode="decimal" defaultValue={text(ingredient.density_g_per_ml)} hint="Lets you use millilitres." error={errors.density} />
      </div>
      {failure ? (
        <p role="alert" className="text-destructive text-sm">
          {failure}
        </p>
      ) : null}
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={update.isPending}>
        {update.isPending ? "Saving…" : "Save ingredient"}
      </Button>
    </form>
  );
}
