"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel } from "@/lib/ingredient-categories";
import { CI, parseCustomIngredient, type CustomIngredientErrors } from "./custom-ingredient";
import { useCreateIngredient, type Ingredient } from "./use-ingredient-search";

type Props = { open: boolean; onOpenChange: (open: boolean) => void; initialName: string; onCreated: (ingredient: Ingredient) => void };

export function CustomIngredientDialog({ open, onOpenChange, initialName, onCreated }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New custom ingredient</DialogTitle>
          <DialogDescription>Amounts are per 100 g. Leave a nutrient blank if you do not know it; it will show as unknown, not zero.</DialogDescription>
        </DialogHeader>
        <CustomIngredientForm initialName={initialName} onCreated={onCreated} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh every time. */
function CustomIngredientForm({ initialName, onCreated, onDone }: { initialName: string; onCreated: (ingredient: Ingredient) => void; onDone: () => void }) {
  const create = useCreateIngredient();
  const [errors, setErrors] = useState<CustomIngredientErrors>({});

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const parsed = parseCustomIngredient(new FormData(event.currentTarget));
        if ("errors" in parsed) {
          setErrors(parsed.errors);
          return;
        }
        setErrors({});
        create.mutate(parsed.value, {
          onSuccess: (ingredient) => {
            onCreated(ingredient);
            onDone();
          },
          onError: (error) => toast.error(problemMessage(error)),
        });
      }}
    >
      <Field name={CI.name} label="Ingredient name" defaultValue={initialName} maxLength={200} error={errors.name} autoComplete="off" />
      <div className="grid gap-1.5">
        <Label htmlFor={CI.category}>Category</Label>
        <NativeSelect id={CI.category} name={CI.category} defaultValue="other">
          {CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {categoryLabel(category)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.calories} label="Calories per 100 g (kcal)" inputMode="decimal" error={errors.calories} />
        <Field name={CI.protein} label="Protein per 100 g (g)" inputMode="decimal" error={errors.protein} />
        <Field name={CI.carbohydrates} label="Carbohydrates per 100 g (g)" inputMode="decimal" error={errors.carbohydrates} />
        <Field name={CI.fat} label="Fat per 100 g (g)" inputMode="decimal" error={errors.fat} />
      </div>
      <div className="grid grid-cols-2 gap-3">
        <Field name={CI.gramsPerPiece} label="Weight of one piece (g)" inputMode="decimal" hint="Lets you use pieces." error={errors.gramsPerPiece} />
        <Field name={CI.density} label="Density (g per ml)" inputMode="decimal" hint="Lets you use millilitres." error={errors.density} />
      </div>
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create ingredient"}
      </Button>
    </form>
  );
}
