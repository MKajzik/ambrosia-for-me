"use client";

import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel, type IngredientCategory } from "@/lib/ingredient-categories";
import { parseDecimal } from "@/lib/parse-number";
import type { ShoppingItem } from "./items";
import { conflictingItem, useDeleteItem, useEditItem } from "./queries";

type Props = { item: ShoppingItem | null; listId: string; onClose: () => void };

/**
 * Edit, or remove, one item. Mount it with a `key` per item so it starts fresh. An edit is saved against the version the form
 * was opened on, not the latest one: if someone changed the item meanwhile the API refuses it with their version, and the form
 * restarts on that version with a note. A live update in the background never wipes what is being typed, and never slips past
 * the version check.
 */
export function EditItemDialog({ item, listId, onClose }: Props) {
  const [opened] = useState(item);
  const [conflict, setConflict] = useState<{ current: ShoppingItem; round: number } | null>(null);
  const base = conflict?.current ?? opened;
  return (
    <Dialog open={item !== null} onOpenChange={(open) => !open && onClose()}>
      {item && base ? (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Edit item</DialogTitle>
            <DialogDescription>Changes are saved against the version you opened, so they never silently overwrite someone else&apos;s.</DialogDescription>
          </DialogHeader>
          {conflict ? (
            <p role="alert" className="bg-destructive/10 text-destructive rounded-lg px-3 py-2 text-sm">
              {problemMessage(new ApiError({ status: 409, code: "version_conflict" }))}
            </p>
          ) : null}
          <ItemForm
            key={conflict?.round ?? 0}
            base={base}
            listId={listId}
            onDone={onClose}
            onConflict={(current) => setConflict((previous) => ({ current, round: (previous?.round ?? 0) + 1 }))}
          />
        </DialogContent>
      ) : null}
    </Dialog>
  );
}

type Errors = { name?: string; quantity?: string };

function ItemForm({ base, listId, onDone, onConflict }: { base: ShoppingItem; listId: string; onDone: () => void; onConflict: (current: ShoppingItem) => void }) {
  const edit = useEditItem(listId);
  const remove = useDeleteItem(listId);
  const [errors, setErrors] = useState<Errors>({});

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const name = String(data.get("edit-name") ?? "").trim();
        const parsed = parseDecimal(String(data.get("edit-quantity") ?? ""));
        const quantity = parsed.ok ? parsed.value : undefined;
        const unitText = String(data.get("edit-unit") ?? "");
        const unit = unitText === "g" || unitText === "ml" || unitText === "piece" ? unitText : null;
        const category = String(data.get("edit-category") ?? "") as IngredientCategory;

        const next: Errors = {};
        if (!name) next.name = "Give the item a name.";
        else if (name.length > 200) next.name = "Use at most 200 characters.";
        if (quantity === undefined) next.quantity = "Enter a number, for example 2 or 1.5.";
        else if (quantity !== null && (quantity <= 0 || quantity > 100000)) next.quantity = "The quantity must be more than 0 and at most 100000.";
        setErrors(next);
        if (next.name || next.quantity || quantity === undefined) return;

        if (name === base.name && quantity === base.quantity && unit === base.unit && category === base.category) return onDone();
        edit.mutate(
          { item: base, changes: { name, quantity, unit, category } },
          {
            onSuccess: onDone,
            onError: (error) => {
              const current = conflictingItem(error);
              return current ? onConflict(current) : toast.error(problemMessage(error));
            },
          },
        );
      }}
    >
      <Field name="edit-name" label="Name" autoComplete="off" defaultValue={base.name} error={errors.name} />
      <div className="grid grid-cols-2 gap-3">
        <Field name="edit-quantity" label="Quantity" inputMode="decimal" defaultValue={base.quantity === null ? "" : String(base.quantity)} error={errors.quantity} />
        <div className="grid gap-1.5">
          <Label htmlFor="edit-unit">Unit</Label>
          <NativeSelect id="edit-unit" name="edit-unit" defaultValue={base.unit ?? ""}>
            <option value="">None</option>
            <option value="g">g</option>
            <option value="ml">ml</option>
            <option value="piece">piece</option>
          </NativeSelect>
        </div>
      </div>
      <div className="grid gap-1.5">
        <Label htmlFor="edit-category">Category</Label>
        <NativeSelect id="edit-category" name="edit-category" defaultValue={base.category}>
          {CATEGORIES.map((category) => (
            <option key={category} value={category}>
              {categoryLabel(category)}
            </option>
          ))}
        </NativeSelect>
      </div>
      <div className="flex flex-wrap justify-between gap-2">
        <Button type="submit" disabled={edit.isPending}>
          {edit.isPending ? "Saving…" : "Save"}
        </Button>
        <Button type="button" variant="destructive" disabled={remove.isPending} onClick={() => remove.mutate(base.id, { onSuccess: onDone, onError: (error) => toast.error(problemMessage(error)) })}>
          Remove item
        </Button>
      </div>
    </form>
  );
}
