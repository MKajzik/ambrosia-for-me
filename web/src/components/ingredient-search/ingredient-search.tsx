"use client";

import { Plus } from "lucide-react";
import { useId, useState } from "react";
import { NativeSelect } from "@/components/native-select";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { problemMessage } from "@/lib/api/problem";
import { CATEGORIES, categoryLabel, type IngredientCategory } from "@/lib/ingredient-categories";
import { useDebouncedValue } from "@/lib/use-debounced-value";
import { CustomIngredientDialog } from "./custom-ingredient-dialog";
import { useIngredientSearch, type Ingredient } from "./use-ingredient-search";

type Props = { onSelect: (ingredient: Ingredient) => void; label?: string };

/** Type-ahead over the ingredient catalogue with a category filter and a "create a custom ingredient" fallback. */
export function IngredientSearch({ onSelect, label = "Add an ingredient" }: Props) {
  const baseId = useId();
  const inputId = `${baseId}-input`;
  const listId = `${baseId}-list`;
  const [text, setText] = useState("");
  const [category, setCategory] = useState<IngredientCategory | "">("");
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const [creating, setCreating] = useState(false);

  const search = useIngredientSearch(useDebouncedValue(text, 250), category, open);
  const items = search.data?.items ?? [];
  const activeIndex = Math.min(active, Math.max(items.length - 1, 0));

  function pick(ingredient: Ingredient) {
    onSelect(ingredient);
    setText("");
    setActive(0);
    setOpen(false);
  }

  function onKeyDown(event: React.KeyboardEvent<HTMLInputElement>) {
    if (event.key === "ArrowDown" || event.key === "ArrowUp") {
      event.preventDefault();
      setOpen(true);
      const step = event.key === "ArrowDown" ? 1 : -1;
      setActive((index) => (items.length === 0 ? 0 : (Math.min(index, items.length - 1) + step + items.length) % items.length));
    } else if (event.key === "Enter" && open && items[activeIndex]) {
      event.preventDefault();
      pick(items[activeIndex]);
    } else if (event.key === "Escape") {
      setOpen(false);
    }
  }

  return (
    <div className="grid gap-1.5">
      <Label htmlFor={inputId}>{label}</Label>
      <div className="flex gap-2">
        <div className="relative flex-1">
          <Input
            id={inputId}
            role="combobox"
            aria-expanded={open}
            aria-controls={listId}
            aria-autocomplete="list"
            aria-activedescendant={open && items.length > 0 ? `${baseId}-opt-${activeIndex}` : undefined}
            autoComplete="off"
            maxLength={100}
            placeholder="Search ingredients"
            className="h-11 text-base md:text-sm"
            value={text}
            onChange={(event) => {
              setText(event.target.value);
              setActive(0);
              setOpen(true);
            }}
            onFocus={() => setOpen(true)}
            onBlur={() => setOpen(false)}
            onClick={() => setOpen(true)}
            onKeyDown={onKeyDown}
          />
          {open ? (
            // Keep focus in the input while the pointer is on the popup, so a click selects instead of blurring first.
            <div className="bg-popover text-popover-foreground absolute inset-x-0 top-full z-20 mt-1 rounded-lg border shadow-md" onMouseDown={(event) => event.preventDefault()}>
              <ul id={listId} role="listbox" aria-label="Ingredients" className="max-h-64 overflow-y-auto p-1">
                {items.map((ingredient, index) => (
                  <li
                    key={ingredient.id}
                    id={`${baseId}-opt-${index}`}
                    role="option"
                    aria-selected={index === activeIndex}
                    onClick={() => pick(ingredient)}
                    onMouseMove={() => setActive(index)}
                    className="aria-selected:bg-accent aria-selected:text-accent-foreground flex cursor-pointer items-center gap-2 rounded-md px-3 py-2 text-sm"
                  >
                    <span className="flex-1 truncate">{ingredient.name}</span>
                    <span className="text-muted-foreground text-xs">{categoryLabel(ingredient.category)}</span>
                    {ingredient.is_custom ? <Badge variant="secondary">Custom</Badge> : null}
                  </li>
                ))}
              </ul>
              {search.isPending ? (
                <p role="status" className="text-muted-foreground px-3 py-2 text-sm">
                  Searching…
                </p>
              ) : null}
              {search.isError ? (
                <p role="status" className="text-destructive px-3 py-2 text-sm">
                  {problemMessage(search.error)}
                </p>
              ) : null}
              {search.isSuccess && items.length === 0 ? (
                <p role="status" className="text-muted-foreground px-3 py-2 text-sm">
                  No ingredient matches.
                </p>
              ) : null}
              <div className="border-t p-1">
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="w-full justify-start"
                  onClick={() => {
                    setOpen(false);
                    setCreating(true);
                  }}
                >
                  <Plus aria-hidden />
                  Create a custom ingredient{text.trim() ? ` “${text.trim()}”` : ""}
                </Button>
              </div>
            </div>
          ) : null}
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
      <CustomIngredientDialog open={creating} onOpenChange={setCreating} initialName={text.trim()} onCreated={pick} />
    </div>
  );
}
