"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { problemMessage } from "@/lib/api/problem";
import { addDays, startOfWeek, today } from "@/lib/dates";
import { useGenerateShoppingList } from "./queries";
import { validateRange } from "./range";

export function GenerateListDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Generate from your plan</DialogTitle>
          <DialogDescription>Adds up the ingredients of every meal planned in these dates and groups them by aisle.</DialogDescription>
        </DialogHeader>
        <GenerateForm onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh (this week) every time. */
function GenerateForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const generate = useGenerateShoppingList();
  const [from, setFrom] = useState(() => startOfWeek(today()));
  const [to, setTo] = useState(() => addDays(startOfWeek(today()), 6));
  const [error, setError] = useState<string | undefined>();

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const checked = validateRange(from, to);
        if (!checked.ok) return setError(checked.error);
        setError(undefined);
        const name = String(new FormData(event.currentTarget).get("generate-name") ?? "").trim();
        generate.mutate(
          { from, to, ...(name ? { name } : {}) },
          {
            onSuccess: (list) => {
              onDone();
              router.push(`/shopping/${list.id}`);
            },
            onError: (e) => toast.error(problemMessage(e)),
          },
        );
      }}
    >
      <div className="grid grid-cols-2 gap-3">
        <Field name="generate-from" label="From" type="date" value={from} onChange={(e) => setFrom(e.target.value)} />
        <Field name="generate-to" label="To" type="date" value={to} onChange={(e) => setTo(e.target.value)} />
      </div>
      {error ? (
        <p role="alert" className="text-destructive text-sm">
          {error}
        </p>
      ) : null}
      <Field name="generate-name" label="List name (optional)" autoComplete="off" hint="Leave blank to name it after the dates." />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={generate.isPending}>
        {generate.isPending ? "Generating…" : "Generate list"}
      </Button>
    </form>
  );
}
