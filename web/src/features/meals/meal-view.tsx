import { NutritionPanel } from "@/components/nutrition-panel";
import { CopyMealButton } from "./copy-meal-button";
import { servingsLabel } from "./meal-list";
import type { Meal } from "./queries";

/** A meal that belongs to the partner: everything visible, nothing editable. Copy it to change it. */
export function MealView({ meal }: { meal: Meal }) {
  return (
    <div className="grid gap-6 lg:grid-cols-[minmax(0,1fr)_22rem] lg:items-start">
      <div className="grid gap-6">
        <div className="flex flex-wrap items-center gap-3">
          <p className="text-muted-foreground text-sm">Shared by your partner. Copy it to your library to change it.</p>
          <CopyMealButton mealId={meal.id} mealName={meal.name} variant="default" />
        </div>
        <p className="text-sm">{servingsLabel(meal.servings)}</p>
        {meal.notes ? <p className="text-sm whitespace-pre-wrap">{meal.notes}</p> : null}
        <section aria-labelledby="view-ingredients" className="grid gap-2">
          <h2 id="view-ingredients" className="text-lg font-semibold">
            Ingredients
          </h2>
          {meal.ingredients.length === 0 ? (
            <p className="text-muted-foreground text-sm">This meal has no ingredients yet.</p>
          ) : (
            <ul className="bg-card divide-y rounded-xl border">
              {meal.ingredients.map((line) => (
                <li key={line.id} className="flex items-baseline justify-between gap-3 px-4 py-2 text-sm">
                  <span>{line.ingredient_name}</span>
                  <span className="text-muted-foreground tabular-nums">{`${line.quantity.toLocaleString("en-US", { maximumFractionDigits: 2 })} ${line.unit}`}</span>
                </li>
              ))}
            </ul>
          )}
        </section>
      </div>
      <aside className="lg:sticky lg:top-6">
        <NutritionPanel nutrition={meal.nutrition_per_serving} />
      </aside>
    </div>
  );
}
