"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { validateDraft } from "./draft";
import { useCreateMeal } from "./queries";

/** The two things the API needs to make a meal. Everything else is edited (and autosaved) on the meal's own page. */
export function NewMealForm() {
  const router = useRouter();
  const create = useCreateMeal();
  const [errors, setErrors] = useState<{ name?: string; servings?: string }>({});

  return (
    <form
      noValidate
      className="grid max-w-md gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const result = validateDraft({ name: String(data.get("new-meal-name") ?? ""), notes: "", servings: String(data.get("new-meal-servings") ?? ""), shared: false, rows: [] });
        if (!result.ok) {
          setErrors({ name: result.errors.name, servings: result.errors.servings });
          return;
        }
        setErrors({});
        create.mutate(
          { name: result.value.name, servings: result.value.servings },
          { onSuccess: (meal) => router.replace(`/meals/${meal.id}`), onError: (error) => toast.error(problemMessage(error)) },
        );
      }}
    >
      <Field name="new-meal-name" label="Name" autoComplete="off" error={errors.name} />
      <Field name="new-meal-servings" label="Servings" inputMode="decimal" defaultValue="1" hint="How many portions this recipe makes." error={errors.servings} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create meal"}
      </Button>
    </form>
  );
}
