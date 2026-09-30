"use client";

import { cn } from "cn";
import type { components } from "@/lib/api/schema.gen";
import { MACRO_KEYS, NO_DATA, formatAmount, nutrientInfo, targetProgress, type NutrientAmounts } from "@/lib/nutrition";

type Targets = components["schemas"]["Targets"];
type Macro = (typeof MACRO_KEYS)[number];

const TARGET_OF: Record<Macro, keyof Targets> = {
  calories: "target_kcal",
  protein: "target_protein_g",
  carbohydrates: "target_carbs_g",
  fat: "target_fat_g",
};
const RING_COLOUR: Record<Macro, string> = {
  calories: "stroke-macro-kcal",
  protein: "stroke-macro-protein",
  carbohydrates: "stroke-macro-carbs",
  fat: "stroke-macro-fat",
};

const SIZE = 96;
const STROKE = 9;
const RADIUS = (SIZE - STROKE) / 2;
const CIRCUMFERENCE = 2 * Math.PI * RADIUS;

/** Calories, protein, carbs and fat as rings against the caller's daily targets. Each macro keeps its own colour. */
export function MacroRings({ nutrition, targets, stale = false, label = "Daily totals" }: { nutrition: NutrientAmounts; targets: Targets; stale?: boolean; label?: string }) {
  return (
    <section aria-label={label} aria-busy={stale} className={cn("bg-card grid grid-cols-2 gap-4 rounded-xl border p-4 transition-opacity sm:grid-cols-4", stale && "opacity-70")}>
      {MACRO_KEYS.map((macro) => (
        <Ring key={macro} macro={macro} value={nutrition[macro]} target={targets[TARGET_OF[macro]]} />
      ))}
    </section>
  );
}

function Ring({ macro, value, target }: { macro: Macro; value: number | null; target: number | null }) {
  const info = nutrientInfo(macro);
  const progress = targetProgress(value, target);
  const fraction = progress?.fraction ?? 0;
  const hasTarget = target !== null && target > 0;

  return (
    <div role="group" aria-label={info.label} className="flex flex-col items-center gap-1 text-center">
      <div className="relative size-24">
        <svg viewBox={`0 0 ${SIZE} ${SIZE}`} className="size-full -rotate-90" aria-hidden>
          <circle cx={SIZE / 2} cy={SIZE / 2} r={RADIUS} fill="none" strokeWidth={STROKE} className="stroke-muted" />
          <circle
            cx={SIZE / 2}
            cy={SIZE / 2}
            r={RADIUS}
            fill="none"
            strokeWidth={STROKE}
            strokeLinecap="round"
            strokeDasharray={CIRCUMFERENCE}
            strokeDashoffset={CIRCUMFERENCE * (1 - fraction)}
            data-fraction={fraction.toFixed(3)}
            className={cn(RING_COLOUR[macro], "transition-[stroke-dashoffset] duration-500")}
          />
        </svg>
        <span aria-hidden className="absolute inset-0 grid place-items-center text-sm font-semibold tabular-nums">
          {progress ? `${progress.percent}%` : ""}
        </span>
      </div>
      <p className="text-muted-foreground text-xs font-medium">{info.label}</p>
      <p className="text-base font-semibold tabular-nums">{value === null ? NO_DATA : formatAmount(value, info.unit)}</p>
      <p className="text-muted-foreground text-xs">{progress ? `${progress.percent}% of ${formatAmount(target, info.unit)}` : hasTarget ? `Target ${formatAmount(target, info.unit)}` : "No target set"}</p>
      {progress?.over ? <p className="text-destructive text-xs font-medium">Over target</p> : null}
    </div>
  );
}
