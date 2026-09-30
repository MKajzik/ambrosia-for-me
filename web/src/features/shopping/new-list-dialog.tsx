"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { problemMessage } from "@/lib/api/problem";
import { useCreateShoppingList } from "./queries";

export function NewListDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New shopping list</DialogTitle>
          <DialogDescription>An empty list you fill in yourself.</DialogDescription>
        </DialogHeader>
        <NewListForm onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh every time. */
function NewListForm({ onDone }: { onDone: () => void }) {
  const router = useRouter();
  const create = useCreateShoppingList();
  const [error, setError] = useState<string | undefined>();

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const name = String(new FormData(event.currentTarget).get("new-list-name") ?? "").trim();
        if (!name) return setError("Give the list a name.");
        if (name.length > 200) return setError("Use at most 200 characters.");
        setError(undefined);
        create.mutate(
          { name },
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
      <Field name="new-list-name" label="Name" autoComplete="off" error={error} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create list"}
      </Button>
    </form>
  );
}
