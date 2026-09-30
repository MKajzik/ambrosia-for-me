"use client";

import { useState } from "react";
import { Field } from "@/components/field";
import { IngredientSearch } from "@/components/ingredient-search/ingredient-search";
import type { Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { Button } from "@/components/ui/button";

/** Adding should take one line and one key press: type it and press Enter, or pick an ingredient. */
export function QuickAdd({ onAddText, onAddIngredient }: { onAddText: (name: string) => void; onAddIngredient: (ingredient: Ingredient) => void }) {
  const [text, setText] = useState("");
  const [error, setError] = useState<string | undefined>();

  return (
    <div className="grid gap-3">
      <form
        noValidate
        className="flex items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          const name = text.trim();
          if (!name) return setError("Type what to add.");
          setError(undefined);
          onAddText(name);
          setText("");
        }}
      >
        <div className="flex-1">
          <Field name="quick-add" label="Add an item" autoComplete="off" maxLength={200} value={text} onChange={(event) => setText(event.target.value)} error={error} />
        </div>
        <Button type="submit" className="h-11">
          Add
        </Button>
      </form>
      <IngredientSearch label="Or add from ingredients" onSelect={onAddIngredient} />
    </div>
  );
}
