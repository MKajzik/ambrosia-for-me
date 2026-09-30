"use client";

import { cn } from "cn";
import { ChevronDown } from "lucide-react";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import {
  MACRO_KEYS,
  NO_DATA,
  NUTRIENTS,
  NUTRIENT_GROUPS,
  formatAmount,
  formatDailyValue,
  hasDailyValue,
  nutrientInfo,
  type NutrientAmounts,
} from "@/lib/nutrition";

const MACRO_DOT: Record<(typeof MACRO_KEYS)[number], string> = {
  calories: "bg-macro-kcal",
  protein: "bg-macro-protein",
  carbohydrates: "bg-macro-carbs",
  fat: "bg-macro-fat",
};

/**
 * The summary tier (four macros) and an expandable full tier (all 18 nutrients, micronutrients against the FDA Daily Value).
 * An unknown amount (`null`) is a dash with a hint, never a zero: a meal with no ingredients really is zero.
 */
export function NutritionPanel({ nutrition, stale = false, title = "Per serving" }: { nutrition: NutrientAmounts; stale?: boolean; title?: string }) {
  const [expanded, setExpanded] = useState(false);
  const anyUnknown = NUTRIENTS.some((n) => nutrition[n.key] === null);

  return (
    <section aria-label="Nutrition" aria-busy={stale} className={cn("bg-card rounded-xl border p-4 transition-opacity", stale && "opacity-70")}>
      <h2 className="text-sm font-medium">{title}</h2>
      <dl className="mt-3 grid grid-cols-2 gap-3">
        {MACRO_KEYS.map((key) => {
          const info = nutrientInfo(key);
          return (
            <div key={key} className="rounded-lg border p-3">
              <dt className="text-muted-foreground flex items-center gap-1.5 text-xs">
                <span aria-hidden className={cn("size-2 rounded-full", MACRO_DOT[key])} />
                {info.label}
              </dt>
              <dd className="mt-1 text-lg font-semibold tabular-nums">{formatAmount(nutrition[key], info.unit)}</dd>
            </div>
          );
        })}
      </dl>
      {anyUnknown ? (
        <p className="text-muted-foreground mt-3 text-xs">
          {NO_DATA} means some ingredients lack data for that nutrient, so the total is unknown rather than zero.
        </p>
      ) : null}

      <Button type="button" variant="ghost" size="sm" className="mt-3" aria-expanded={expanded} onClick={() => setExpanded((open) => !open)}>
        <ChevronDown aria-hidden className={cn("transition-transform", expanded && "rotate-180")} />
        All nutrients
      </Button>
      {expanded ? (
        <div className="mt-2 grid gap-4">
          {NUTRIENT_GROUPS.map((group) => (
            <div key={group} role="group" aria-label={group}>
              <h3 className="text-muted-foreground mb-1 text-xs font-medium tracking-wide uppercase">{group}</h3>
              <dl className="divide-y text-sm">
                {NUTRIENTS.filter((n) => n.group === group).map((n) => (
                  <div key={n.key} className="flex items-baseline justify-between gap-3 py-1.5">
                    <dt>{n.label}</dt>
                    <dd className="tabular-nums">
                      <span>{formatAmount(nutrition[n.key], n.unit)}</span>
                      {hasDailyValue(n.key) ? (
                        <span className="text-muted-foreground ml-2 inline-block w-12 text-right">{formatDailyValue(n.key, nutrition[n.key])}</span>
                      ) : null}
                    </dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
          <p className="text-muted-foreground text-xs">Percentages are of the FDA Daily Value for adults.</p>
        </div>
      ) : null}
    </section>
  );
}
