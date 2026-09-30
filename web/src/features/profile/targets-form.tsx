"use client";

import { useState } from "react";
import { toast } from "sonner";
import { ErrorState } from "@/components/error-state";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { useMe } from "@/features/auth/use-me";
import { problemMessage } from "@/lib/api/problem";
import { useUpdateTargets } from "./queries";
import { TARGET_FIELDS, parseTargets, type TargetsErrors } from "./targets";

const text = (value: number | null | undefined) => (value === null || value === undefined ? "" : String(value));

export function TargetsForm() {
  const me = useMe();
  return (
    <Card>
      <CardHeader>
        <CardTitle>Daily targets</CardTitle>
      </CardHeader>
      <CardContent>
        {me.isPending ? (
          <Skeleton role="status" aria-label="Loading targets" className="h-40 rounded-lg" />
        ) : me.data === undefined ? (
          <ErrorState message={problemMessage(me.error)} onRetry={() => void me.refetch()} />
        ) : (
          <TargetsFields
            key={me.data.updated_at}
            initial={{ calories: text(me.data.target_kcal), protein: text(me.data.target_protein_g), carbs: text(me.data.target_carbs_g), fat: text(me.data.target_fat_g) }}
          />
        )}
      </CardContent>
    </Card>
  );
}

/** Keyed by the profile's `updated_at`, so a saved (or reloaded) profile re-fills the form. */
function TargetsFields({ initial }: { initial: Record<keyof typeof TARGET_FIELDS, string> }) {
  const update = useUpdateTargets();
  const [errors, setErrors] = useState<TargetsErrors>({});

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const parsed = parseTargets(new FormData(event.currentTarget));
        if ("errors" in parsed) return setErrors(parsed.errors);
        setErrors({});
        update.mutate(parsed.value, { onSuccess: () => toast.success("Targets saved."), onError: (error) => toast.error(problemMessage(error)) });
      }}
    >
      <p className="text-muted-foreground text-sm">Today and your plan show your progress against these. Leave a field blank for no target.</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <Field name={TARGET_FIELDS.calories} label="Calories (kcal)" inputMode="decimal" defaultValue={initial.calories} error={errors.calories} />
        <Field name={TARGET_FIELDS.protein} label="Protein (g)" inputMode="decimal" defaultValue={initial.protein} error={errors.protein} />
        <Field name={TARGET_FIELDS.carbs} label="Carbohydrates (g)" inputMode="decimal" defaultValue={initial.carbs} error={errors.carbs} />
        <Field name={TARGET_FIELDS.fat} label="Fat (g)" inputMode="decimal" defaultValue={initial.fat} error={errors.fat} />
      </div>
      <div>
        <Button type="submit" disabled={update.isPending}>
          {update.isPending ? "Saving…" : "Save targets"}
        </Button>
      </div>
    </form>
  );
}
