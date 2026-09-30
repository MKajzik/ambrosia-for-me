# Web Plan and Today Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship the Plan and Today areas of the web app: Today with macro rings against the user's targets and one-tap meal swaps and portion changes, the Plan week calendar with day and week totals and "apply a template", and the diet template library and editor with the partner's shared templates (read-only, copy).

**Architecture:** Pages stay thin server components rendering client components on TanStack Query, through the typed `api` client and the BFF proxy. `GET /plan` is the one read for both Today (one day) and Plan (one week); it already carries each day's computed nutrition and the caller's targets, so nothing is computed in the browser except sums of already-computed days. Swaps, portion changes and slot clears are optimistic on the entries only (totals wait for the server) and roll back on error. Template editing reuses the autosave pattern of the meal editor (`useAutosave`, moved to `src/lib`), with a pure draft model per resource. All dates are local calendar dates as `YYYY-MM-DD`, never derived from UTC.

**Tech Stack:** Next.js 16 App Router, React 19, TypeScript strict (`noUncheckedIndexedAccess`), Tailwind 4, shadcn/ui, TanStack Query 5, `openapi-fetch`, Vitest 5 + Testing Library (jsdom), Playwright.

**Spec:** `docs/superpowers/specs/2026-09-25-web-app-design.md` (§2, §5, §6, §7, §10 plan 3); parent spec `docs/superpowers/specs/2026-09-21-meal-planner-design.md` §3.4 and §5.1. Backend contract: `openapi.yaml` (`/plan`, `/plan/{date}/{slot}`, `/diet-templates`, `/diet-templates/{id}`, `/diet-templates/{id}/slots`, `/diet-templates/{id}/apply`, `/diet-templates/{id}/copy`, `/partner/diet-templates`, `/me`). Builds on the merged Meals plan (`docs/superpowers/plans/2026-09-30-web-meals.md`).

**Branch:** work on `feat/web-plan-today`, created from `master` in a worktree (superpowers:using-git-worktrees). `openapi.yaml` and `backend/` do not change.

## Global Constraints

- "The browser only calls its own origin." Every call goes through `api` + `unwrap` from `@/lib/api/client`. (Spec §2)
- "Server-side data fetching: None in v1. `(app)` pages are client components on TanStack Query." Page files stay thin server components that render one client component. (Spec §2)
- "Optimistic updates only where the parent spec asks for them: shopping check-off and quick-add, and plan swaps and portion changes. Each snapshots, applies, rolls back on error and refetches." (Spec §5)
- "Query keys are per resource. Each mutation invalidates the keys it touches. Cursor lists use `useInfiniteQuery`." (Spec §5)
- "Field validation errors show inline; everything else is a toast. Both are driven by the problem `code`. Loading is a skeleton; empty states carry a call to action." (Spec §5)
- "`is_owner: false` renders read-only with a "Copy to my library" action." A shared template is copied "to change it, or to apply it": the API answers `404` for a write or an apply on the partner's template. (Spec §5, `openapi.yaml`)
- "Applying a template copies its slots into plan entries starting at the chosen date. After that, entries are independent: swapping Tuesday's meal changes one row and never alters the template. Applying over dates that already have entries returns a conflict unless the client passes `overwrite: true`." (Parent spec §3.4)
- `diet_templates.day_count` (1 to 31) cannot change after creation; a snack slot may repeat within a day, the other three may not. (Parent spec §3.4)
- "Each macro keeps one consistent colour across the product. Motion carries meaning only (rings animate on change ...); reduced-motion is respected." (Parent spec §5.2; the reduced-motion rule is already global in `globals.css`.)
- "Keyboard navigation, labelled controls and WCAG AA contrast." (Spec §5)
- "E2E creates custom ingredients through the API, so CI needs no `FDC_API_KEY`. Each test registers its own user." E2E runs with one worker. (Spec §7)
- Never hand-edit `web/src/lib/api/schema.gen.ts`. Colours, radius and macro colours live in `src/app/globals.css` only. shadcn components come from `npx shadcn@4.21.0 add <component>`.
- Component tests start with `// @vitest-environment jsdom`. Every task ends with `make lint-web` and `make test-web` clean (run from the repo root; ESLint 9 with the React 19 hooks rules: no ref writes during render, no synchronous `setState` in an effect body; `tsc` has `noUncheckedIndexedAccess`, so write `list[0]?.x` in tests and never spread `list[0]`).
- Reuse, do not rebuild: `NutritionPanel`, `NativeSelect`, `ErrorState`, `Field`, `PageHeader`, `BackLink`, `CopyMealButton` patterns, `useMeals`, `usePartnerLink`, `servingsLabel`, `parseDecimal`, `useAutosave` and the test helpers in `src/test/` all exist from the Meals plan.

## Review Focus

The spec is silent on these inputs; each line names the behaviour a person would expect and the task whose tests pin it.

1. **Which day is "today"**: the local calendar day at any hour (23:30 local must not become tomorrow's date, as `toISOString()` would make it in a positive UTC offset, or yesterday's in a negative one), across a daylight-saving change, and after midnight passes while the page stays open. (Task 1 dates, Task 5 `useToday` and Today)
2. **Applying a template over days that already have meals**: the API answers `409 plan_conflict`; the dialog asks before replacing, "Keep my plan" leaves the plan untouched with no request sent, and "Replace them" retries once with `overwrite: true`. (Task 6)
3. **A swap the server refuses** (the meal was deleted meanwhile, `invalid_meal`; or the network drops): the optimistic change is rolled back, the person is told why, and no phantom entry stays on screen. (Task 2 mutations, Task 4 slot row)
4. **Snacks are not editable one by one**: the API addresses a snack only by date and slot (`PUT` always adds, `DELETE` removes every snack that day), so the UI offers "Add snack" and a confirmed "Clear snacks", never a per-snack swap or portion control that would silently do something else. (Task 2 cache functions, Task 4 slot row)
5. **Targets that are missing, zero or exceeded, and totals that are unknown**: a `null` or non-positive target shows "No target set" (never `NaN`, `Infinity` or an empty ring pretending to be 0%); a day over target fills the ring and says so; a `null` nutrient stays "—", never 0; a week total is unknown if any day's is. (Task 1 `targetProgress` and `sumNutrition`, Task 4 rings, Task 6)

Also pinned: an empty picker (no meals yet) points to creating one (Task 4); the picker stops asking for pages after a failed page (Task 4); a partner's template offers "Copy to my library" and no editor or apply (Task 7); a template whose slots are invalid (duplicate breakfast, non-positive portion) is flagged inline and never sent (Task 8).

## File Structure

```
web/src/
  lib/
    dates.ts (+test)                        local calendar dates, weeks starting Monday   Task 1
    nutrition/{sum,progress}.ts (+test)     sumNutrition, scaleNutrition, zeroNutrition, targetProgress   Task 1
    api/problem.ts (modify) + problem-plan-codes.test.ts                                   Task 1
    use-load-all-pages.ts                   keep fetching pages until the list is complete  Task 4
    use-today.ts (+test)                    today's date, refreshed across midnight         Task 5
    use-autosave.ts (+test)                 moved from features/meals                       Task 8
  test/plan-fixtures.ts                     makeEntry, makeDay, makePlan, makeTemplate...   Task 2
  components/macro-rings.tsx (+test)                                                        Task 4
  features/plan/
    plan-cache.ts (+test)                   pure optimistic edits of a cached plan          Task 2
    queries.ts (+test)                      usePlan, useSetPlanEntry, useClearPlanSlot, useApplyTemplate   Task 2
    template-queries.ts (+test)             templates lists, detail, create, copy, delete, actions        Task 3
    meal-picker.tsx, slot-row.tsx, day-meals.tsx (+tests)                                   Task 4
    today-view.tsx (+test)                                                                  Task 5
    plan-view.tsx, apply-template-dialog.tsx (+tests)                                       Task 6
    copy-template-button.tsx, template-list.tsx, templates-page.tsx, new-template-form.tsx,
      template-view.tsx, template-detail.tsx (+tests)                                       Task 7
    template-draft.ts (+test), template-editor.tsx (+test)                                  Task 8
  app/(app)/today/page.tsx, plan/page.tsx (replace); plan/templates/{page.tsx,new/page.tsx,[id]/page.tsx}   Tasks 5-8
web/e2e/plan.spec.ts                                                                        Task 9
web/CLAUDE.md, docs/superpowers/specs/2026-09-25-web-app-design.md (modify)                 Task 10
```

---

### Task 1: Dates, sums, target progress and problem codes

**Files:**
- Create: `web/src/lib/dates.ts`, `web/src/lib/nutrition/sum.ts`, `web/src/lib/nutrition/progress.ts`
- Modify: `web/src/lib/nutrition/index.ts`, `web/src/lib/api/problem.ts`
- Test: `web/src/lib/dates.test.ts`, `web/src/lib/nutrition/sum-progress.test.ts`, `web/src/lib/api/problem-plan-codes.test.ts`

**Interfaces:**
- Consumes: `NUTRIENTS`, `NutrientAmounts`, `NutrientKey` from `@/lib/nutrition`; `ApiError`, `problemMessage` from `@/lib/api/problem`.
- Produces (`@/lib/dates`; a date is a `string` shaped `YYYY-MM-DD`, always the local calendar day):
  - `toIsoDate(date: Date): string`, `parseIsoDate(iso: string): Date` (local midnight; throws on anything that is not a real calendar date), `isIsoDate(text: string): boolean`, `today(now?: Date): string`
  - `addDays(iso: string, days: number): string`, `startOfWeek(iso: string): string` (the Monday on or before), `weekDates(startIso: string): string[]` (seven dates)
  - `formatWeekday(iso)` ("Wed"), `formatMonthDay(iso)` ("Sep 30"), `formatLongDate(iso)` ("Wednesday, September 30"), `formatWeekRange(startIso)` ("Sep 28 – Oct 4")
- Produces (`@/lib/nutrition`):
  - `zeroNutrition(): NutrientAmounts` (all 18 keys 0)
  - `sumNutrition(list: readonly NutrientAmounts[]): NutrientAmounts` (a key is `null` if any item's is; an empty list is zeros)
  - `scaleNutrition(n: NutrientAmounts, factor: number): NutrientAmounts` (`null` stays `null`)
  - `type TargetProgress = { fraction: number; percent: number; over: boolean }`, `targetProgress(value: number | null, target: number | null): TargetProgress | null` (`fraction` is `0..1`, `percent` is the rounded real ratio, `null` unless both are numbers and the target is above 0)
- `problemMessage` gains sentences for `plan_conflict`, `plan_range_too_long`, `plan_range_invalid`, `day_index_out_of_range`, `duplicate_slot`, `invalid_meal`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/lib/dates.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { addDays, formatLongDate, formatMonthDay, formatWeekRange, formatWeekday, isIsoDate, parseIsoDate, startOfWeek, toIsoDate, today, weekDates } from "./dates";

describe("toIsoDate and today", () => {
  it("names the local calendar day at any hour, never the UTC one", () => {
    expect(toIsoDate(new Date(2026, 9, 5, 0, 0, 0))).toBe("2026-10-05");
    expect(toIsoDate(new Date(2026, 9, 5, 12, 0, 0))).toBe("2026-10-05");
    expect(toIsoDate(new Date(2026, 9, 5, 23, 59, 59))).toBe("2026-10-05");
    expect(today(new Date(2026, 0, 1, 23, 30))).toBe("2026-01-01");
  });

  it("pads month and day", () => {
    expect(toIsoDate(new Date(2026, 2, 3))).toBe("2026-03-03");
  });
});

describe("parseIsoDate and isIsoDate", () => {
  it("round-trips a real date as local midnight", () => {
    const date = parseIsoDate("2026-09-30");
    expect([date.getFullYear(), date.getMonth(), date.getDate(), date.getHours()]).toEqual([2026, 8, 30, 0]);
    expect(toIsoDate(date)).toBe("2026-09-30");
  });

  it.each(["2026-02-30", "2026-13-01", "2026-00-10", "2026-9-30", "30-09-2026", "tomorrow", "", "2026-09-30T10:00:00Z", "0099-01-01"])("refuses %j", (text) => {
    expect(isIsoDate(text)).toBe(false);
    expect(() => parseIsoDate(text)).toThrow();
  });

  it("knows leap days", () => {
    expect(isIsoDate("2028-02-29")).toBe(true);
    expect(isIsoDate("2026-02-29")).toBe(false);
  });
});

describe("addDays", () => {
  it("crosses month, year and leap-day boundaries", () => {
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2028-02-28", 1)).toBe("2028-02-29");
    expect(addDays("2028-03-01", -1)).toBe("2028-02-29");
    expect(addDays("2026-09-30", 0)).toBe("2026-09-30");
  });

  it.each(["2026-03-01", "2026-03-25", "2026-10-20", "2026-11-01"])("gives sixty distinct, consecutive days from %s, across any daylight-saving change", (start) => {
    let previous = start;
    const seen = new Set([start]);
    for (let i = 1; i <= 60; i++) {
      const next = addDays(start, i);
      expect(next).toBe(addDays(previous, 1));
      expect(parseIsoDate(next).getTime()).toBeGreaterThan(parseIsoDate(previous).getTime());
      seen.add(next);
      previous = next;
    }
    expect(seen.size).toBe(61);
  });
});

describe("weeks start on Monday", () => {
  it("finds the Monday on or before a date", () => {
    expect(startOfWeek("2026-10-05")).toBe("2026-10-05");
    expect(startOfWeek("2026-10-04")).toBe("2026-09-28");
    expect(startOfWeek("2026-09-30")).toBe("2026-09-28");
    expect(startOfWeek("2026-01-01")).toBe("2025-12-29");
  });

  it("lists seven consecutive dates from the start", () => {
    expect(weekDates("2026-09-28")).toEqual(["2026-09-28", "2026-09-29", "2026-09-30", "2026-10-01", "2026-10-02", "2026-10-03", "2026-10-04"]);
    for (const start of ["2026-03-23", "2026-10-26"]) {
      const week = weekDates(start);
      expect(week).toHaveLength(7);
      expect(parseIsoDate(week[0] ?? "").getDay()).toBe(1);
      expect(parseIsoDate(week[6] ?? "").getDay()).toBe(0);
    }
  });
});

describe("formatting", () => {
  it("writes dates the way a person would read them", () => {
    expect(formatWeekday("2026-09-30")).toBe("Wed");
    expect(formatMonthDay("2026-09-30")).toBe("Sep 30");
    expect(formatLongDate("2026-09-30")).toBe("Wednesday, September 30");
    expect(formatWeekRange("2026-09-28")).toBe("Sep 28 – Oct 4");
  });
});
```

**Create `web/src/lib/nutrition/sum-progress.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { scaleNutrition, sumNutrition, targetProgress, zeroNutrition } from ".";

describe("zeroNutrition and sumNutrition", () => {
  it("is all zeros, and the sum of nothing is all zeros", () => {
    expect(zeroNutrition()).toEqual(nutrients());
    expect(sumNutrition([])).toEqual(nutrients());
  });

  it("adds each nutrient across the list", () => {
    const total = sumNutrition([nutrients({ calories: 300, protein: 10 }), nutrients({ calories: 450.5, iron: 2 }), nutrients({ calories: 0 })]);
    expect(total.calories).toBeCloseTo(750.5, 10);
    expect(total.protein).toBe(10);
    expect(total.iron).toBe(2);
    expect(total.sodium).toBe(0);
  });

  it("is unknown for a nutrient that any item does not know, and known for the rest", () => {
    const total = sumNutrition([nutrients({ calories: 300, iron: 2 }), nutrients({ calories: 100, iron: null })]);
    expect(total.iron).toBeNull();
    expect(total.calories).toBe(400);
  });

  it("stays unknown once unknown, whatever comes after", () => {
    const total = sumNutrition([unknownNutrients(), nutrients({ calories: 100 })]);
    expect(total.calories).toBeNull();
  });
});

describe("scaleNutrition", () => {
  it("multiplies known amounts and leaves unknown ones unknown", () => {
    const scaled = scaleNutrition(nutrients({ calories: 700, protein: null }), 1 / 7);
    expect(scaled.calories).toBeCloseTo(100, 10);
    expect(scaled.protein).toBeNull();
  });
});

describe("targetProgress", () => {
  it("is the share of the target reached", () => {
    expect(targetProgress(1200, 2000)).toEqual({ fraction: 0.6, percent: 60, over: false });
    expect(targetProgress(0, 2000)).toEqual({ fraction: 0, percent: 0, over: false });
    expect(targetProgress(2000, 2000)).toEqual({ fraction: 1, percent: 100, over: false });
  });

  it("fills the ring but keeps the real percent when the target is exceeded", () => {
    expect(targetProgress(2500, 2000)).toEqual({ fraction: 1, percent: 125, over: true });
  });

  it.each([
    [1200, null],
    [1200, 0],
    [1200, -5],
    [null, 2000],
    [null, null],
    [Number.NaN, 2000],
    [1200, Number.NaN],
  ])("has no progress for value %s and target %s (never NaN or Infinity)", (value, target) => {
    expect(targetProgress(value, target)).toBeNull();
  });
});
```

**Create `web/src/lib/api/problem-plan-codes.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { ApiError, problemMessage } from "./problem";

const err = (code: string, status = 409) => new ApiError({ status, code });

describe("problemMessage for the plan and template screens", () => {
  it.each([
    ["plan_conflict", /already have meals/],
    ["plan_range_too_long", /date range/],
    ["plan_range_invalid", /date range/],
    ["day_index_out_of_range", /days/],
    ["duplicate_slot", /already has/],
    ["invalid_meal", /isn't available/],
  ])("explains %s in words", (code, pattern) => {
    expect(problemMessage(err(code))).toMatch(pattern);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/dates.test.ts src/lib/nutrition/sum-progress.test.ts src/lib/api/problem-plan-codes.test.ts`
Expected: FAIL: `Failed to resolve import "./dates"`, missing `sumNutrition`/`targetProgress` exports, and the six problem codes answering the generic line.

- [ ] **Step 3: Write the implementation**

**Create `web/src/lib/dates.ts`**

```ts
/**
 * Calendar dates as the API takes them: "YYYY-MM-DD", the person's own local day.
 * Never derive one with `toISOString()`: that is the UTC day, which is wrong for part of every day in most time zones.
 */
const pad = (n: number) => String(n).padStart(2, "0");

export function toIsoDate(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** A real calendar date at local midnight. Throws for anything else ("2026-02-30", "tomorrow", a timestamp). */
export function parseIsoDate(iso: string): Date {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!match) throw new Error(`not a calendar date: ${iso}`);
  const date = new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
  if (toIsoDate(date) !== iso) throw new Error(`not a calendar date: ${iso}`);
  return date;
}

export function isIsoDate(text: string): boolean {
  try {
    parseIsoDate(text);
    return true;
  } catch {
    return false;
  }
}

export function today(now: Date = new Date()): string {
  return toIsoDate(now);
}

/** Moves by whole calendar days, so a daylight-saving change never skips or repeats a date. */
export function addDays(iso: string, days: number): string {
  const date = parseIsoDate(iso);
  date.setDate(date.getDate() + days);
  return toIsoDate(date);
}

/** The Monday on or before `iso`. */
export function startOfWeek(iso: string): string {
  const back = (parseIsoDate(iso).getDay() + 6) % 7;
  return addDays(iso, -back);
}

export function weekDates(startIso: string): string[] {
  return Array.from({ length: 7 }, (_, i) => addDays(startIso, i));
}

const WEEKDAY = new Intl.DateTimeFormat("en-US", { weekday: "short" });
const MONTH_DAY = new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" });
const LONG = new Intl.DateTimeFormat("en-US", { weekday: "long", month: "long", day: "numeric" });

export const formatWeekday = (iso: string) => WEEKDAY.format(parseIsoDate(iso));
export const formatMonthDay = (iso: string) => MONTH_DAY.format(parseIsoDate(iso));
export const formatLongDate = (iso: string) => LONG.format(parseIsoDate(iso));
export const formatWeekRange = (startIso: string) => `${formatMonthDay(startIso)} – ${formatMonthDay(addDays(startIso, 6))}`;
```

**Create `web/src/lib/nutrition/sum.ts`**

```ts
import { NUTRIENTS, type NutrientAmounts, type NutrientKey } from "./catalog";

export function zeroNutrition(): NutrientAmounts {
  return Object.fromEntries(NUTRIENTS.map((n) => [n.key, 0])) as NutrientAmounts;
}

/** Adds nutrient by nutrient. A nutrient is unknown (`null`) in the sum if it is unknown in any item: unknown is never treated as zero. */
export function sumNutrition(list: readonly NutrientAmounts[]): NutrientAmounts {
  const total = zeroNutrition() as Record<NutrientKey, number | null>;
  for (const item of list) {
    for (const { key } of NUTRIENTS) {
      const current = total[key];
      const value = item[key];
      total[key] = current === null || value === null ? null : current + value;
    }
  }
  return total as NutrientAmounts;
}

export function scaleNutrition(nutrition: NutrientAmounts, factor: number): NutrientAmounts {
  const scaled = { ...nutrition } as Record<NutrientKey, number | null>;
  for (const { key } of NUTRIENTS) {
    const value = nutrition[key];
    scaled[key] = value === null ? null : value * factor;
  }
  return scaled as NutrientAmounts;
}
```

**Create `web/src/lib/nutrition/progress.ts`**

```ts
export type TargetProgress = {
  /** How much of the ring to fill, 0 to 1 (a value over target fills it). */
  fraction: number;
  /** The real ratio as a rounded percent, so 125 means 25% over. */
  percent: number;
  over: boolean;
};

/** How far `value` is towards `target`, or null when either is missing or the target is not a positive number. */
export function targetProgress(value: number | null, target: number | null): TargetProgress | null {
  if (value === null || target === null || Number.isNaN(value) || Number.isNaN(target) || !(target > 0) || value < 0) return null;
  const ratio = value / target;
  return { fraction: Math.min(ratio, 1), percent: Math.round(ratio * 100), over: ratio > 1 };
}
```

**Edit `web/src/lib/nutrition/index.ts`**: append two lines so the file reads:

```ts
export * from "./catalog";
export * from "./daily-values";
export * from "./format";
export * from "./progress";
export * from "./sum";
```

**Edit `web/src/lib/api/problem.ts`**: replace

```ts
    case "invalid_ingredient":
      return "One of the ingredients can't be used in a meal.";
    default:
```

with

```ts
    case "invalid_ingredient":
      return "One of the ingredients can't be used in a meal.";
    case "plan_conflict":
      return "Some of those days already have meals.";
    case "plan_range_too_long":
    case "plan_range_invalid":
      return "That isn't a valid date range. Pick a shorter range that ends on or after it starts.";
    case "day_index_out_of_range":
      return "One of the meals is on a day outside the template's days.";
    case "duplicate_slot":
      return "That day already has a meal for that slot.";
    case "invalid_meal":
      return "That meal isn't available. It may have been deleted.";
    default:
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/dates.test.ts src/lib/nutrition src/lib/api`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/lib
git commit -m "feat(web): add local calendar dates, nutrition sums, target progress and plan problem messages"
```

---

### Task 2: The plan data layer and its optimistic edits

**Files:**
- Create: `web/src/test/plan-fixtures.ts`, `web/src/features/plan/plan-cache.ts`, `web/src/features/plan/queries.ts`
- Test: `web/src/features/plan/plan-cache.test.ts`, `web/src/features/plan/queries.test.tsx`

**Interfaces:**
- Consumes: `api`, `unwrap`; test helpers from the Meals plan (`fakeApi`, `json`, `problem`, `noContent`, `clientWrapper`, `testQueryClient`, `nutrients`).
- Produces (`plan-cache.ts`):
  - `type Slot`, `PlanEntry`, `DailyTotal`, `PlanRange`, `Targets` (`components["schemas"][...]`)
  - `SLOTS: readonly Slot[]` (`breakfast`, `lunch`, `dinner`, `snack`), `SLOT_LABELS: Record<Slot, string>` (`Breakfast`, `Lunch`, `Dinner`, `Snacks`)
  - `type SlotChange = { date: string; slot: Slot; mealId: string; mealName: string; portion: number }`
  - `withEntry(range: PlanRange, change: SlotChange): PlanRange`: for breakfast, lunch and dinner it replaces the day's entry for that slot (keeping its id) or adds one; for `snack` it always appends. Totals are untouched. Never mutates its input.
  - `withoutSlot(range: PlanRange, date: string, slot: Slot): PlanRange`: removes every entry of that slot on that date.
- Produces (`queries.ts`):
  - `planKeys = { all: ["plan"], range(from, to): ["plan", from, to] }`
  - `usePlan(from: string, to: string)`: `useQuery` over `GET /plan`, data is `PlanRange`
  - `useSetPlanEntry(onFailure?: (error: Error) => void)`: mutation `(change: SlotChange) => PlanEntry`, optimistic
  - `useClearPlanSlot(onFailure?)`: mutation `({ date, slot }) => void`, optimistic
  - `useApplyTemplate()`: mutation `({ templateId, startDate, overwrite }) => void`, invalidates `planKeys.all`
  - Optimistic mutations cancel in-flight plan queries, apply the edit to every cached plan range, roll every range back on error (then call `onFailure`), and invalidate `planKeys.all` when settled.
- Produces (`plan-fixtures.ts`, test-only): `makeEntry(over?)`, `makeDay(date, entries?, nutrition?)`, `makePlan(from, to, days?, targets?)`, `makeTemplateSlot(over?)`, `makeTemplate(over?)`, `makeTemplateSummary(over?)`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/test/plan-fixtures.ts`** (test helper, no test of its own)

```ts
import type { components } from "@/lib/api/schema.gen";
import { nutrients } from "./fixtures";

type S = components["schemas"];
const STAMP = "2026-01-01T00:00:00Z";

export function makeEntry(over: Partial<S["PlanEntry"]> = {}): S["PlanEntry"] {
  return {
    id: "e1",
    date: "2026-09-28",
    slot: "breakfast",
    meal_id: "m1",
    meal_name: "Oat bowl",
    portion: 1,
    from_template_id: null,
    created_at: STAMP,
    updated_at: STAMP,
    ...over,
  };
}

export function makeDay(date: string, entries: S["PlanEntry"][] = [], nutrition: S["NutrientAmounts"] = nutrients()): S["DailyTotal"] {
  return { date, entries, nutrition_per_day: nutrition };
}

export const TARGETS: S["Targets"] = { target_kcal: 2000, target_protein_g: 120, target_carbs_g: 250, target_fat_g: 70 };

export function makePlan(from: string, to: string, days: S["DailyTotal"][] = [], targets: S["Targets"] = TARGETS): S["PlanRange"] {
  return { from, to, days, targets };
}

export function makeTemplateSlot(over: Partial<S["TemplateSlot"]> = {}): S["TemplateSlot"] {
  return { id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1, ...over };
}

export function makeTemplate(over: Partial<S["DietTemplate"]> = {}): S["DietTemplate"] {
  return { id: "t1", name: "Base week", day_count: 7, shared_with_partner: false, is_owner: true, slots: [], created_at: STAMP, updated_at: STAMP, ...over };
}

export function makeTemplateSummary(over: Partial<S["DietTemplateSummary"]> = {}): S["DietTemplateSummary"] {
  return { id: "t1", name: "Base week", day_count: 7, shared_with_partner: false, created_at: STAMP, updated_at: STAMP, ...over };
}
```

**Create `web/src/features/plan/plan-cache.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { SLOTS, SLOT_LABELS, withEntry, withoutSlot, type SlotChange } from "./plan-cache";

const D = "2026-09-28";
const change = (over: Partial<SlotChange> = {}): SlotChange => ({ date: D, slot: "breakfast", mealId: "m-new", mealName: "Pasta", portion: 1.5, ...over });
const names = (range: ReturnType<typeof makePlan>, date = D) => range.days.find((d) => d.date === date)?.entries.map((e) => `${e.slot}:${e.meal_name}:${e.portion}`);

describe("slots", () => {
  it("lists the four slots in the order of a day, with their labels", () => {
    expect(SLOTS).toEqual(["breakfast", "lunch", "dinner", "snack"]);
    expect(SLOT_LABELS).toEqual({ breakfast: "Breakfast", lunch: "Lunch", dinner: "Dinner", snack: "Snacks" });
  });
});

describe("withEntry", () => {
  const plan = makePlan(D, "2026-09-29", [
    makeDay(D, [makeEntry({ id: "e-b", slot: "breakfast", meal_name: "Porridge" }), makeEntry({ id: "e-s1", slot: "snack", meal_name: "Apple" })]),
    makeDay("2026-09-29", [makeEntry({ id: "e-x", date: "2026-09-29", meal_name: "Toast" })]),
  ]);

  it("replaces the meal and portion of a breakfast, lunch or dinner, keeping the entry's id", () => {
    const next = withEntry(plan, change());
    expect(names(next)).toEqual(["breakfast:Pasta:1.5", "snack:Apple:1"]);
    expect(next.days[0]?.entries[0]?.id).toBe("e-b");
    expect(next.days[0]?.entries[0]?.from_template_id).toBeNull();
  });

  it("adds an entry to an empty slot", () => {
    expect(names(withEntry(plan, change({ slot: "dinner", mealName: "Soup", portion: 1 })))).toEqual(["breakfast:Porridge:1", "snack:Apple:1", "dinner:Soup:1"]);
  });

  it("always adds a snack, never replacing one, because the API can only add snacks by slot", () => {
    const next = withEntry(plan, change({ slot: "snack", mealName: "Nuts", portion: 1 }));
    expect(names(next)).toEqual(["breakfast:Porridge:1", "snack:Apple:1", "snack:Nuts:1"]);
    expect(new Set(next.days[0]?.entries.map((e) => e.id)).size).toBe(3);
  });

  it("leaves other days, the totals and the targets alone, and never mutates its input", () => {
    const before = JSON.stringify(plan);
    const next = withEntry(plan, change());
    expect(JSON.stringify(plan)).toBe(before);
    expect(next.days[1]).toBe(plan.days[1]);
    expect(next.days[0]?.nutrition_per_day).toBe(plan.days[0]?.nutrition_per_day);
    expect(next.targets).toBe(plan.targets);
  });

  it("does nothing for a date outside the cached range", () => {
    expect(withEntry(plan, change({ date: "2027-01-01" })).days).toEqual(plan.days);
  });
});

describe("withoutSlot", () => {
  const plan = makePlan(D, D, [
    makeDay(D, [
      makeEntry({ id: "e-b", slot: "breakfast" }),
      makeEntry({ id: "e-s1", slot: "snack", meal_name: "Apple" }),
      makeEntry({ id: "e-s2", slot: "snack", meal_name: "Nuts" }),
    ]),
  ]);

  it("removes the one entry of a single-meal slot", () => {
    expect(names(withoutSlot(plan, D, "breakfast"))).toEqual(["snack:Apple:1", "snack:Nuts:1"]);
  });

  it("removes every snack of that day, as the API does", () => {
    expect(names(withoutSlot(plan, D, "snack"))).toEqual(["breakfast:Oat bowl:1"]);
  });

  it("never mutates its input", () => {
    const before = JSON.stringify(plan);
    withoutSlot(plan, D, "snack");
    expect(JSON.stringify(plan)).toBe(before);
  });
});
```

**Create `web/src/features/plan/queries.test.tsx`**

```tsx
// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import type { PlanRange } from "./plan-cache";
import { planKeys, useApplyTemplate, useClearPlanSlot, usePlan, useSetPlanEntry } from "./queries";

const D = "2026-09-28";
const porridge = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m-old", meal_name: "Porridge" })])]);
const mealNames = (range: PlanRange | undefined) => range?.days[0]?.entries.map((e) => e.meal_name);

describe("usePlan", () => {
  it("reads one range of the plan", async () => {
    const fake = fakeApi({ "GET /plan": () => json(porridge) });
    const { result } = renderHook(() => usePlan(D, D), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.data)).toEqual(["Porridge"]));
    expect(fake.calls[0]?.search.get("from")).toBe(D);
    expect(fake.calls[0]?.search.get("to")).toBe(D);
  });
});

describe("useSetPlanEntry", () => {
  it("shows the swap at once, then settles on what the server says", async () => {
    let server = porridge;
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "PUT /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeEntry({ meal_id: "m-new", meal_name: "Pasta", portion: 2 })));
        }),
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "m-new", mealName: "Pasta", portion: 2 }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Pasta"]));
    expect(result.current.set.isPending).toBe(true);
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m-new", portion: 2 });

    server = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m-new", meal_name: "Pasta", portion: 2 })])]);
    await act(async () => release());
    await waitFor(() => expect(result.current.set.isSuccess).toBe(true));
    await waitFor(() => expect(fake.callsTo("GET", "/plan")).toHaveLength(2));
    expect(mealNames(result.current.plan.data)).toEqual(["Pasta"]);
  });

  it("puts the plan back and reports why when the server refuses the swap", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /plan": () => json(porridge), "PUT /plan/:date/:slot": () => problem(400, "invalid_meal") });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry(onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "gone", mealName: "Ghost", portion: 1 }));
    await waitFor(() => expect(result.current.set.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    expect(onFailure).toHaveBeenCalledTimes(1);
    expect(onFailure.mock.calls[0]?.[0]).toBeInstanceOf(ApiError);
    expect(onFailure.mock.calls[0]?.[0]).toMatchObject({ code: "invalid_meal" });
  });

  it("rolls back when the network fails too", async () => {
    let fail = false;
    fakeApi({
      "GET /plan": () => json(porridge),
      "PUT /plan/:date/:slot": () => {
        fail = true;
        throw new TypeError("offline");
      },
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "m2", mealName: "Pasta", portion: 1 }));
    await waitFor(() => expect(fail).toBe(true));
    await waitFor(() => expect(result.current.set.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
  });

  it("edits every cached range that holds the date, and refreshes all of them", async () => {
    const week = makePlan(D, "2026-10-04", [makeDay(D, [makeEntry({ meal_name: "Porridge" })])]);
    const fake = fakeApi({ "GET /plan": () => json(porridge), "PUT /plan/:date/:slot": () => json(makeEntry({ meal_name: "Pasta" })) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(planKeys.range(D, D), porridge);
    queryClient.setQueryData(planKeys.range(D, "2026-10-04"), week);
    const { result } = renderHook(() => useSetPlanEntry(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ date: D, slot: "breakfast", mealId: "m2", mealName: "Pasta", portion: 1 });
    });
    expect(queryClient.getQueryState(planKeys.range(D, D))?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(planKeys.range(D, "2026-10-04"))?.isInvalidated).toBe(true);
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)).toHaveLength(1);
  });
});

describe("useClearPlanSlot", () => {
  it("removes the entry at once and asks the API to delete the slot", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(porridge),
      "DELETE /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(noContent());
        }),
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.clear.mutate({ date: D, slot: "breakfast" }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual([]));
    expect(fake.callsTo("DELETE", `/plan/${D}/breakfast`)).toHaveLength(1);
    await act(async () => release());
    await waitFor(() => expect(result.current.clear.isSuccess).toBe(true));
  });

  it("clears every snack of the day, so the cache must too", async () => {
    const snacks = makePlan(D, D, [makeDay(D, [makeEntry({ id: "s1", slot: "snack", meal_name: "Apple" }), makeEntry({ id: "s2", slot: "snack", meal_name: "Nuts" })])]);
    let server = snacks;
    fakeApi({
      "GET /plan": () => json(server),
      "DELETE /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [])]);
        return noContent();
      },
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Apple", "Nuts"]));
    act(() => result.current.clear.mutate({ date: D, slot: "snack" }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual([]));
  });

  it("brings the entry back if the delete fails", async () => {
    fakeApi({ "GET /plan": () => json(porridge), "DELETE /plan/:date/:slot": () => problem(500, "internal_error") });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    act(() => result.current.clear.mutate({ date: D, slot: "breakfast" }));
    await waitFor(() => expect(result.current.clear.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
  });
});

describe("useApplyTemplate", () => {
  it("sends the start date and the overwrite choice, then refreshes the plan", async () => {
    const fake = fakeApi({ "POST /diet-templates/:id/apply": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(planKeys.range(D, D), porridge);
    const { result } = renderHook(() => useApplyTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ templateId: "t1", startDate: D, overwrite: true });
    });
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")[0]?.body).toEqual({ start_date: D, overwrite: true });
    expect(queryClient.getQueryState(planKeys.range(D, D))?.isInvalidated).toBe(true);
  });

  it("surfaces plan_conflict as an ApiError the dialog can act on", async () => {
    fakeApi({ "POST /diet-templates/:id/apply": () => problem(409, "plan_conflict") });
    const { result } = renderHook(() => useApplyTemplate(), { wrapper: clientWrapper() });
    let caught: unknown;
    await act(async () => {
      caught = await result.current.mutateAsync({ templateId: "t1", startDate: D, overwrite: false }).catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 409, code: "plan_conflict" });
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/plan`
Expected: FAIL: `Failed to resolve import "./plan-cache"` and `"./queries"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/plan/plan-cache.ts`**

```ts
import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type Slot = Schemas["Slot"];
export type PlanEntry = Schemas["PlanEntry"];
export type DailyTotal = Schemas["DailyTotal"];
export type PlanRange = Schemas["PlanRange"];
export type Targets = Schemas["Targets"];

/** The four slots of a day, in the order a day reads. */
export const SLOTS: readonly Slot[] = ["breakfast", "lunch", "dinner", "snack"];
export const SLOT_LABELS: Record<Slot, string> = { breakfast: "Breakfast", lunch: "Lunch", dinner: "Dinner", snack: "Snacks" };

export type SlotChange = { date: string; slot: Slot; mealId: string; mealName: string; portion: number };

/**
 * The plan as it will look once the server accepts `change`. Breakfast, lunch and dinner hold one meal each, so they are
 * replaced; a snack is always added, because the API can only add snacks by slot. Totals are left as they were: only the
 * server can compute them, and the mutation refetches them when it settles.
 */
export function withEntry(range: PlanRange, change: SlotChange): PlanRange {
  return {
    ...range,
    days: range.days.map((day) => {
      if (day.date !== change.date) return day;
      const existing = change.slot === "snack" ? undefined : day.entries.find((entry) => entry.slot === change.slot);
      const now = new Date().toISOString();
      const entry: PlanEntry = {
        id: existing?.id ?? `optimistic:${change.date}:${change.slot}:${day.entries.length}`,
        date: change.date,
        slot: change.slot,
        meal_id: change.mealId,
        meal_name: change.mealName,
        portion: change.portion,
        from_template_id: null,
        created_at: existing?.created_at ?? now,
        updated_at: now,
      };
      return { ...day, entries: existing ? day.entries.map((e) => (e === existing ? entry : e)) : [...day.entries, entry] };
    }),
  };
}

/** Removes every entry of `slot` on `date`: the one meal, or all the snacks, exactly as `DELETE /plan/{date}/{slot}` does. */
export function withoutSlot(range: PlanRange, date: string, slot: Slot): PlanRange {
  return { ...range, days: range.days.map((day) => (day.date === date ? { ...day, entries: day.entries.filter((entry) => entry.slot !== slot) } : day)) };
}
```

**Create `web/src/features/plan/queries.ts`**

```ts
import { useMutation, useQuery, useQueryClient, type QueryKey } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api/client";
import { withEntry, withoutSlot, type PlanRange, type Slot, type SlotChange } from "./plan-cache";

export const planKeys = {
  all: ["plan"] as const,
  range: (from: string, to: string) => ["plan", from, to] as const,
};

/** The plan for `from` to `to` (inclusive): one entry list and one computed nutrition total per date, plus the caller's targets. */
export function usePlan(from: string, to: string) {
  return useQuery({ queryKey: planKeys.range(from, to), queryFn: () => unwrap(api.GET("/plan", { params: { query: { from, to } } })) });
}

type Snapshot = [QueryKey, PlanRange | undefined][];

/**
 * A plan write that shows its result at once: cancel in-flight plan reads, apply `apply` to every cached range, roll every
 * range back if the server refuses (then tell `onFailure`), and refetch when it settles either way.
 */
function useOptimisticPlan<V, R>(mutationFn: (vars: V) => Promise<R>, apply: (range: PlanRange, vars: V) => PlanRange, onFailure?: (error: Error) => void) {
  const queryClient = useQueryClient();
  return useMutation<R, Error, V, { snapshot: Snapshot }>({
    mutationFn,
    onMutate: async (vars) => {
      await queryClient.cancelQueries({ queryKey: planKeys.all });
      const snapshot = queryClient.getQueriesData<PlanRange>({ queryKey: planKeys.all });
      queryClient.setQueriesData<PlanRange>({ queryKey: planKeys.all }, (range) => (range ? apply(range, vars) : range));
      return { snapshot };
    },
    onError: (error, _vars, context) => {
      for (const [key, data] of context?.snapshot ?? []) queryClient.setQueryData(key, data);
      onFailure?.(error);
    },
    onSettled: () => queryClient.invalidateQueries({ queryKey: planKeys.all }),
  });
}

/** Sets or swaps the meal of a slot (or adds a snack). Changing a portion is the same call with the same meal. */
export function useSetPlanEntry(onFailure?: (error: Error) => void) {
  return useOptimisticPlan(
    (change: SlotChange) =>
      unwrap(api.PUT("/plan/{date}/{slot}", { params: { path: { date: change.date, slot: change.slot } }, body: { meal_id: change.mealId, portion: change.portion } })),
    withEntry,
    onFailure,
  );
}

/** Removes the slot's meal, or every snack of the day. */
export function useClearPlanSlot(onFailure?: (error: Error) => void) {
  return useOptimisticPlan(
    async (target: { date: string; slot: Slot }) => {
      await unwrap(api.DELETE("/plan/{date}/{slot}", { params: { path: { date: target.date, slot: target.slot } } }));
    },
    (range, target) => withoutSlot(range, target.date, target.slot),
    onFailure,
  );
}

/** Copies a template's slots into the plan from `startDate`. `overwrite` replaces breakfast, lunch and dinner already there. */
export function useApplyTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (vars: { templateId: string; startDate: string; overwrite: boolean }) => {
      await unwrap(api.POST("/diet-templates/{id}/apply", { params: { path: { id: vars.templateId } }, body: { start_date: vars.startDate, overwrite: vars.overwrite } }));
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: planKeys.all }),
  });
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/plan`
Expected: PASS. If the "rolls back when the network fails too" test hangs on `isError`, note that `fakeApi`'s handler throws inside the stub, which the client surfaces as a rejected fetch: the mutation still errors, and TanStack's default `retry: 0` for mutations means no delay.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/test/plan-fixtures.ts web/src/features/plan
git commit -m "feat(web): add the plan queries with optimistic swaps, portion changes and slot clears"
```

### Task 3: The diet template data layer

**Files:**
- Create: `web/src/features/plan/template-queries.ts`
- Test: `web/src/features/plan/template-queries.test.tsx`

**Interfaces:**
- Consumes: `api`, `unwrap`; `ApiError`; `usePartnerLink` stays in `@/features/meals/queries` (not re-exported here); test helpers and `plan-fixtures` from Task 2.
- Produces (`@/features/plan/template-queries`):
  - Types `DietTemplate`, `DietTemplateSummary`, `TemplateSlot`, `TemplateSlotInput`, `CreateDietTemplateRequest`, `UpdateDietTemplateRequest` (`components["schemas"][...]`)
  - `templateKeys = { mine: ["templates","mine"], partner: ["templates","partner"], detail(id): ["templates","detail",id] }`
  - `useTemplates(scope: "mine" | "partner")`: `useInfiniteQuery` over `GET /diet-templates` or `GET /partner/diet-templates`, pages are `DietTemplateList`
  - `useTemplate(id)`: `useQuery` over `GET /diet-templates/{id}` keyed `templateKeys.detail(id)`
  - `useCreateTemplate()`: mutation `(body: CreateDietTemplateRequest) => DietTemplate`; `useCopyTemplate()`: `(id: string) => DietTemplate`; both seed the detail cache and mark `templateKeys.mine` stale
  - `useDeleteTemplate()`: `(id: string) => void`; drops the detail cache, marks `templateKeys.mine` stale
  - `useTemplateActions(id)`: `{ patch(body: UpdateDietTemplateRequest): Promise<DietTemplate>; replaceSlots(items: TemplateSlotInput[]): Promise<DietTemplate> }` (stable per `id`); each writes the returned template into `templateKeys.detail(id)` and marks `templateKeys.mine` stale

- [ ] **Step 1: Write the failing test**

**Create `web/src/features/plan/template-queries.test.tsx`**

```tsx
// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeTemplate, makeTemplateSummary } from "@/test/plan-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import { templateKeys, useCopyTemplate, useCreateTemplate, useDeleteTemplate, useTemplateActions, useTemplates } from "./template-queries";

describe("useTemplates", () => {
  it("walks the pages of my templates with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /diet-templates": (req) =>
        req.search.get("cursor") === "c1"
          ? json({ items: [makeTemplateSummary({ id: "t2", name: "Cut" })], next_cursor: null })
          : json({ items: [makeTemplateSummary()], next_cursor: "c1" }),
    });
    const { result } = renderHook(() => useTemplates("mine"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(result.current.hasNextPage).toBe(true);

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.data?.pages.flatMap((p) => p.items).map((t) => t.name)).toEqual(["Base week", "Cut"]));
    expect(fake.calls[1]?.search.get("cursor")).toBe("c1");
  });

  it("reads the partner's shared templates from the partner route", async () => {
    const fake = fakeApi({ "GET /partner/diet-templates": () => json({ items: [makeTemplateSummary({ name: "Bulk" })], next_cursor: null }) });
    const { result } = renderHook(() => useTemplates("partner"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(fake.calls.map((c) => c.path)).toEqual(["/partner/diet-templates"]);
  });
});

describe("template mutations", () => {
  it("create seeds the detail cache and marks the list stale", async () => {
    const created = makeTemplate({ id: "new1", name: "Cut", day_count: 3 });
    const fake = fakeApi({ "POST /diet-templates": () => json(created, 201) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useCreateTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ name: "Cut", day_count: 3 });
    });
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Cut", day_count: 3 });
    expect(queryClient.getQueryData(templateKeys.detail("new1"))).toEqual(created);
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("copy returns my own copy and seeds its detail cache", async () => {
    const copy = makeTemplate({ id: "copy1", name: "Bulk" });
    const fake = fakeApi({ "POST /diet-templates/:id/copy": () => json(copy, 201) });
    const queryClient = testQueryClient();
    const { result } = renderHook(() => useCopyTemplate(), { wrapper: clientWrapper(queryClient) });

    let created: unknown;
    await act(async () => {
      created = await result.current.mutateAsync("partner-template");
    });
    expect(created).toEqual(copy);
    expect(fake.calls[0]?.path).toBe("/diet-templates/partner-template/copy");
    expect(queryClient.getQueryData(templateKeys.detail("copy1"))).toEqual(copy);
  });

  it("delete drops the detail cache and marks the list stale", async () => {
    fakeApi({ "DELETE /diet-templates/:id": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.detail("t1"), makeTemplate());
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useDeleteTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("t1");
    });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toBeUndefined();
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("surfaces a 404 as an ApiError", async () => {
    fakeApi({ "POST /diet-templates/:id/copy": () => problem(404, "not_found") });
    const { result } = renderHook(() => useCopyTemplate(), { wrapper: clientWrapper() });
    let caught: unknown;
    await act(async () => {
      caught = await result.current.mutateAsync("gone").catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 404, code: "not_found" });
  });
});

describe("useTemplateActions", () => {
  it("patches the fields and replaces the slot list, keeping the detail cache on the latest answer", async () => {
    const renamed = makeTemplate({ name: "Cut" });
    const withSlot = makeTemplate({ name: "Cut", slots: [{ id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1 }] });
    const fake = fakeApi({ "PATCH /diet-templates/:id": () => json(renamed), "PUT /diet-templates/:id/slots": () => json(withSlot) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useTemplateActions("t1"), { wrapper: clientWrapper(queryClient) });

    expect(await result.current.patch({ name: "Cut" })).toEqual(renamed);
    expect(fake.callsTo("PATCH", "/diet-templates/t1")[0]?.body).toEqual({ name: "Cut" });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toEqual(renamed);

    const items = [{ day_index: 0, slot: "breakfast" as const, meal_id: "m1", portion: 1 }];
    expect(await result.current.replaceSlots(items)).toEqual(withSlot);
    expect(fake.callsTo("PUT", "/diet-templates/t1/slots")[0]?.body).toEqual({ items });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toEqual(withSlot);
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("keeps the same actions object while the id is unchanged", () => {
    const wrapper = clientWrapper();
    const { result, rerender } = renderHook(({ id }) => useTemplateActions(id), { wrapper, initialProps: { id: "t1" } });
    const before = result.current;
    rerender({ id: "t1" });
    expect(result.current).toBe(before);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/features/plan/template-queries.test.tsx`
Expected: FAIL: `Failed to resolve import "./template-queries"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/plan/template-queries.ts`**

```ts
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { api, unwrap } from "@/lib/api/client";
import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type DietTemplate = Schemas["DietTemplate"];
export type DietTemplateSummary = Schemas["DietTemplateSummary"];
export type TemplateSlot = Schemas["TemplateSlot"];
export type TemplateSlotInput = Schemas["TemplateSlotInput"];
export type CreateDietTemplateRequest = Schemas["CreateDietTemplateRequest"];
export type UpdateDietTemplateRequest = Schemas["UpdateDietTemplateRequest"];

/** Per-resource keys. The list keys prefix nothing else, so refreshing lists never touches an open template. */
export const templateKeys = {
  mine: ["templates", "mine"] as const,
  partner: ["templates", "partner"] as const,
  detail: (id: string) => ["templates", "detail", id] as const,
};

export function useTemplates(scope: "mine" | "partner") {
  return useInfiniteQuery({
    queryKey: scope === "mine" ? templateKeys.mine : templateKeys.partner,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const query = { cursor: pageParam, limit: 20 };
      return scope === "mine" ? unwrap(api.GET("/diet-templates", { params: { query } })) : unwrap(api.GET("/partner/diet-templates", { params: { query } }));
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useTemplate(id: string) {
  return useQuery({ queryKey: templateKeys.detail(id), queryFn: () => unwrap(api.GET("/diet-templates/{id}", { params: { path: { id } } })) });
}

const createTemplate = (body: CreateDietTemplateRequest) => unwrap(api.POST("/diet-templates", { body }));
const copyTemplate = (id: string) => unwrap(api.POST("/diet-templates/{id}/copy", { params: { path: { id } } }));
const removeTemplate = async (id: string): Promise<void> => {
  await unwrap(api.DELETE("/diet-templates/{id}", { params: { path: { id } } }));
};
const updateTemplate = (id: string, body: UpdateDietTemplateRequest) => unwrap(api.PATCH("/diet-templates/{id}", { params: { path: { id } }, body }));
const replaceTemplateSlots = (id: string, items: TemplateSlotInput[]) =>
  unwrap(api.PUT("/diet-templates/{id}/slots", { params: { path: { id } }, body: { items } }));

export function useCreateTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createTemplate,
    onSuccess: (template) => {
      queryClient.setQueryData(templateKeys.detail(template.id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

export function useCopyTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: copyTemplate,
    onSuccess: (template) => {
      queryClient.setQueryData(templateKeys.detail(template.id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

export function useDeleteTemplate() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeTemplate,
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: templateKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
    },
  });
}

/** The two writes the template editor's autosave makes. Each keeps the open template's cache on the server's latest answer. */
export function useTemplateActions(id: string) {
  const queryClient = useQueryClient();
  return useMemo(() => {
    const done = (template: DietTemplate) => {
      queryClient.setQueryData(templateKeys.detail(id), template);
      void queryClient.invalidateQueries({ queryKey: templateKeys.mine });
      return template;
    };
    return {
      patch: async (body: UpdateDietTemplateRequest) => done(await updateTemplate(id, body)),
      replaceSlots: async (items: TemplateSlotInput[]) => done(await replaceTemplateSlots(id, items)),
    };
  }, [queryClient, id]);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/features/plan/template-queries.test.tsx`
Expected: PASS.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/features/plan/template-queries.ts web/src/features/plan/template-queries.test.tsx
git commit -m "feat(web): add the diet template queries and mutations with per-resource cache keys"
```

---

### Task 4: Macro rings, the meal picker and the slot rows

**Files:**
- Create: `web/src/lib/use-load-all-pages.ts`, `web/src/components/macro-rings.tsx`, `web/src/features/plan/meal-picker.tsx`, `web/src/features/plan/slot-row.tsx`, `web/src/features/plan/day-meals.tsx`
- Test: `web/src/components/macro-rings.test.tsx`, `web/src/features/plan/meal-picker.test.tsx`, `web/src/features/plan/slot-row.test.tsx`, `web/src/features/plan/day-meals.test.tsx`

**Interfaces:**
- Consumes: `targetProgress`, `formatAmount`, `nutrientInfo`, `MACRO_KEYS`, `NO_DATA`, `NutrientAmounts` (`@/lib/nutrition`); `useMeals`, `MealSummary` (`@/features/meals/queries`); `servingsLabel` (`@/features/meals/meal-list`); `SLOTS`, `SLOT_LABELS`, `PlanEntry`, `Slot`, `Targets` (`plan-cache`); `useSetPlanEntry`, `useClearPlanSlot`, `usePlan` (`queries`); `ErrorState`, `Dialog*`, `Button`, `Skeleton`, `Input`, `Label`, `problemMessage`.
- Produces:
  - `useLoadAllPages(query: { hasNextPage: boolean; isFetchingNextPage: boolean; isFetchNextPageError: boolean; fetchNextPage: () => unknown }): void`: keeps asking for the next page until there is none; stops after a failed page.
  - `MacroRings({ nutrition: NutrientAmounts; targets: Targets; stale?: boolean; label?: string })`: a `section` (default label "Daily totals") with four rings, each a `role="group"` named "Calories", "Protein", "Carbohydrates", "Fat" holding an SVG ring whose progress circle carries `data-fraction` (`0.000` to `1.000`, three decimals), the amount (`formatAmount`, "—" if unknown), and a caption: `"{percent}% of {target}"` when there is progress, `"Target {target}"` when the target is set but the amount unknown, `"No target set"` when the target is `null` or not above 0. Over target adds the text "Over target". Each macro keeps its colour token (`stroke-macro-*`). `stale` sets `aria-busy` and dims it.
  - `MealPicker({ open, onOpenChange, title, onPick(meal: MealSummary) })`: a dialog listing all of the caller's meals (pages load automatically), a "Search your meals" filter over the loaded names, buttons per meal; picking calls `onPick(meal)` then `onOpenChange(false)`; empty state links to `/meals/new`; a failed page shows an error with "Try again" and stops paging.
  - `SlotRow({ slot; entries: PlanEntry[]; onPick(meal: { id: string; name: string }, portion: number); onClear() })`: breakfast, lunch, dinner show either "Add meal" (aria-label "Add meal for {slot}") or the meal (link to `/meals/{id}`), a portion stepper in 0.5 steps (aria-labels "Decrease portion of {meal}" / "Increase portion of {meal}"; decrease is disabled at 0.5), "Swap" (aria-label "Swap {slot} meal", opens the picker, keeps the portion) and "Remove" (aria-label "Remove {meal} from {slot}"). Snacks show their entries read-only, "Add snack", and (when there are any) "Clear snacks" behind a confirmation dialog ("Clear all snacks?", "Keep them" / "Clear snacks").
  - `DayMeals({ date: string; entries: PlanEntry[] })`: four `SlotRow`s wired to `useSetPlanEntry` and `useClearPlanSlot`; failures toast `problemMessage`.
  - `PORTION_STEP = 0.5`, `MAX_PORTION = 100` from `slot-row.tsx`.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/components/macro-rings.test.tsx`**

```tsx
// @vitest-environment jsdom
import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { TARGETS } from "@/test/plan-fixtures";
import { MacroRings } from "./macro-rings";

const ring = (name: string) => screen.getByRole("group", { name });
const fraction = (name: string) => ring(name).querySelector("circle[data-fraction]")?.getAttribute("data-fraction");

describe("MacroRings", () => {
  it("fills each ring by its share of the target and says the numbers in words", () => {
    render(<MacroRings nutrition={nutrients({ calories: 1200, protein: 90, carbohydrates: 150, fat: 40 })} targets={TARGETS} />);
    expect(within(ring("Calories")).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("60% of 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.600");
    expect(within(ring("Protein")).getByText("75% of 120 g")).toBeInTheDocument();
    expect(fraction("Protein")).toBe("0.750");
    expect(within(ring("Carbohydrates")).getByText("60% of 250 g")).toBeInTheDocument();
    expect(within(ring("Fat")).getByText("57% of 70 g")).toBeInTheDocument();
  });

  it("fills the ring and says so when the day is over target, keeping the real percent", () => {
    render(<MacroRings nutrition={nutrients({ calories: 2500 })} targets={TARGETS} />);
    expect(fraction("Calories")).toBe("1.000");
    expect(within(ring("Calories")).getByText("125% of 2,000 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("Over target")).toBeInTheDocument();
    expect(within(ring("Protein")).queryByText("Over target")).not.toBeInTheDocument();
  });

  it.each([
    ["missing", null],
    ["zero", 0],
    ["negative", -100],
  ] as const)("shows a %s target as no target, with the amount and an empty ring, never NaN or Infinity", (_name, target) => {
    render(<MacroRings nutrition={nutrients({ calories: 1200 })} targets={{ ...TARGETS, target_kcal: target }} />);
    expect(within(ring("Calories")).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("No target set")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
    expect(document.body.textContent).not.toMatch(/NaN|Infinity/);
  });

  it("shows an unknown amount as a dash with the target, and an empty ring, never as zero", () => {
    render(<MacroRings nutrition={unknownNutrients()} targets={TARGETS} />);
    const kcal = within(ring("Calories"));
    expect(kcal.getByText("—")).toBeInTheDocument();
    expect(kcal.getByText("Target 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
    expect(kcal.queryByText(/^0 /)).not.toBeInTheDocument();
  });

  it("shows a genuine zero as zero with an empty ring", () => {
    render(<MacroRings nutrition={nutrients()} targets={TARGETS} />);
    expect(within(ring("Calories")).getByText("0 kcal")).toBeInTheDocument();
    expect(within(ring("Calories")).getByText("0% of 2,000 kcal")).toBeInTheDocument();
    expect(fraction("Calories")).toBe("0.000");
  });

  it("is labelled, and marks itself busy while its numbers are out of date", () => {
    render(<MacroRings nutrition={nutrients()} targets={TARGETS} stale label="Today's totals" />);
    expect(screen.getByRole("region", { name: "Today's totals" })).toHaveAttribute("aria-busy", "true");
  });
});
```

**Create `web/src/features/plan/meal-picker.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { renderWithClient } from "@/test/render";
import { MealPicker } from "./meal-picker";

const twoPages = () =>
  fakeApi({
    "GET /meals": (req) =>
      req.search.get("cursor") === "c1"
        ? json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null })
        : json({ items: [makeSummary()], next_cursor: "c1" }),
  });

function open(onPick = vi.fn(), onOpenChange = vi.fn()) {
  renderWithClient(<MealPicker open onOpenChange={onOpenChange} title="Choose breakfast" onPick={onPick} />);
  return { onPick, onOpenChange };
}

describe("MealPicker", () => {
  it("lists all of my meals, loading every page by itself", async () => {
    const fake = twoPages();
    open();
    const dialog = await screen.findByRole("dialog", { name: "Choose breakfast" });
    expect(await within(dialog).findByRole("button", { name: /Oat bowl/ })).toBeInTheDocument();
    expect(await within(dialog).findByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/meals")[1]?.search.get("cursor")).toBe("c1");
  });

  it("narrows the loaded meals as you type", async () => {
    twoPages();
    open();
    await screen.findByRole("button", { name: /Pasta bowl/ });
    await userEvent.type(screen.getByLabelText("Search your meals"), "pasta");
    expect(screen.queryByRole("button", { name: /Oat bowl/ })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();

    await userEvent.clear(screen.getByLabelText("Search your meals"));
    await userEvent.type(screen.getByLabelText("Search your meals"), "zzz");
    expect(await screen.findByText("No meal matches “zzz”.")).toBeInTheDocument();
  });

  it("hands over the chosen meal and closes", async () => {
    twoPages();
    const { onPick, onOpenChange } = open();
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }));
    expect(onOpenChange).toHaveBeenCalledWith(false);
  });

  it("points to making a meal when there are none", async () => {
    fakeApi({ "GET /meals": () => json({ items: [], next_cursor: null }) });
    open();
    expect(await screen.findByText("You have no meals yet.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create a meal" })).toHaveAttribute("href", "/meals/new");
  });

  it("stops asking for pages after one fails, keeps what it has, and offers a retry", async () => {
    let failing = true;
    const fake = fakeApi({
      "GET /meals": (req) => (req.search.get("cursor") === "c1" ? (failing ? problem(500, "internal_error") : json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null })) : json({ items: [makeSummary()], next_cursor: "c1" })),
    });
    open();
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Oat bowl/ })).toBeInTheDocument();
    const asked = fake.callsTo("GET", "/meals").length;
    await new Promise((resolve) => setTimeout(resolve, 150));
    expect(fake.callsTo("GET", "/meals")).toHaveLength(asked);

    failing = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("button", { name: /Pasta bowl/ })).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByText("Something went wrong. Try again.")).not.toBeInTheDocument());
  });
});
```

**Create `web/src/features/plan/slot-row.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { makeEntry } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { SlotRow } from "./slot-row";

const meals = () =>
  fakeApi({ "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" }), makeSummary()], next_cursor: null }) });
const porridge = (portion = 2) => makeEntry({ slot: "breakfast", meal_id: "m1", meal_name: "Porridge", portion });

function show(slot: Parameters<typeof SlotRow>[0]["slot"], entries: ReturnType<typeof makeEntry>[]) {
  const onPick = vi.fn();
  const onClear = vi.fn();
  renderWithClient(<SlotRow slot={slot} entries={entries} onPick={onPick} onClear={onClear} />);
  return { onPick, onClear };
}

describe("SlotRow for breakfast, lunch and dinner", () => {
  it("offers to add a meal to an empty slot and hands over the pick with a portion of 1", async () => {
    meals();
    const { onPick } = show("breakfast", []);
    await userEvent.click(screen.getByRole("button", { name: "Add meal for breakfast" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }), 1);
  });

  it("shows the meal as a link with its portion", () => {
    show("breakfast", [porridge(1.5)]);
    expect(screen.getByRole("link", { name: "Porridge" })).toHaveAttribute("href", "/meals/m1");
    expect(screen.getByText("1.5 servings")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Add meal/ })).not.toBeInTheDocument();
  });

  it("changes the portion in half steps by asking for the same meal with the new portion", async () => {
    const { onPick } = show("breakfast", [porridge(2)]);
    await userEvent.click(screen.getByRole("button", { name: "Increase portion of Porridge" }));
    expect(onPick).toHaveBeenLastCalledWith({ id: "m1", name: "Porridge" }, 2.5);
    await userEvent.click(screen.getByRole("button", { name: "Decrease portion of Porridge" }));
    expect(onPick).toHaveBeenLastCalledWith({ id: "m1", name: "Porridge" }, 1.5);
  });

  it("cannot step the portion down to nothing, or past the API's limit", () => {
    show("breakfast", [porridge(0.5)]);
    expect(screen.getByRole("button", { name: "Decrease portion of Porridge" })).toBeDisabled();
  });

  it("cannot step the portion above 100", () => {
    show("lunch", [makeEntry({ slot: "lunch", meal_name: "Stew", portion: 100 })]);
    expect(screen.getByRole("button", { name: "Increase portion of Stew" })).toBeDisabled();
  });

  it("swaps the meal and keeps the current portion", async () => {
    meals();
    const { onPick } = show("breakfast", [porridge(2)]);
    await userEvent.click(screen.getByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2", name: "Pasta bowl" }), 2);
  });

  it("removes the meal", async () => {
    const { onClear } = show("breakfast", [porridge()]);
    await userEvent.click(screen.getByRole("button", { name: "Remove Porridge from breakfast" }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });
});

describe("SlotRow for snacks", () => {
  const snacks = [makeEntry({ id: "s1", slot: "snack", meal_id: "m3", meal_name: "Apple", portion: 1 }), makeEntry({ id: "s2", slot: "snack", meal_id: "m4", meal_name: "Nuts", portion: 2 })];

  it("lists the snacks read-only, with no swap or portion controls, because the API cannot address one snack", () => {
    show("snack", snacks);
    expect(screen.getByRole("link", { name: "Apple" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Nuts" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /portion/i })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /swap/i })).not.toBeInTheDocument();
  });

  it("adds a snack with a portion of 1", async () => {
    meals();
    const { onPick } = show("snack", snacks);
    await userEvent.click(screen.getByRole("button", { name: "Add snack" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));
    expect(onPick).toHaveBeenCalledWith(expect.objectContaining({ id: "m2" }), 1);
  });

  it("clears all snacks only after a confirmation, and not when the person keeps them", async () => {
    const { onClear } = show("snack", snacks);
    await userEvent.click(screen.getByRole("button", { name: "Clear snacks" }));
    let dialog = await screen.findByRole("dialog", { name: "Clear all snacks?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep them" }));
    expect(onClear).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole("button", { name: "Clear snacks" }));
    dialog = await screen.findByRole("dialog", { name: "Clear all snacks?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Clear snacks" }));
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("offers no clear when there are no snacks", () => {
    show("snack", []);
    expect(screen.queryByRole("button", { name: "Clear snacks" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add snack" })).toBeInTheDocument();
  });
});
```

**Create `web/src/features/plan/day-meals.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeSummary } from "@/test/fixtures";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { DayMeals } from "./day-meals";
import { usePlan } from "./queries";

const toastError = vi.fn();
vi.mock("sonner", () => ({ toast: { error: (message: string) => toastError(message), success: vi.fn() } }));
afterEach(() => toastError.mockReset());

const D = "2026-09-28";

function Harness() {
  const plan = usePlan(D, D);
  const day = plan.data?.days[0];
  return day ? <DayMeals date={D} entries={day.entries} /> : null;
}

const porridgeDay = () => makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m1", meal_name: "Porridge", portion: 1 })])]);
const catalogue = { "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null }) };

describe("DayMeals", () => {
  it("shows all four slots of the day", async () => {
    fakeApi({ "GET /plan": () => json(porridgeDay()) });
    renderWithClient(<Harness />);
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
    for (const name of ["Add meal for lunch", "Add meal for dinner", "Add snack"]) expect(screen.getByRole("button", { name })).toBeInTheDocument();
  });

  it("swaps a meal: the new name shows at once and the API gets the date, slot, meal and portion", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(porridgeDay()),
      ...catalogue,
      "PUT /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeEntry({ meal_id: "m2", meal_name: "Pasta bowl" })));
        }),
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    expect(await screen.findByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Porridge" })).not.toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m2", portion: 1 });
    release();
  });

  it("changes a portion with one tap", async () => {
    let server = porridgeDay();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "PUT /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m1", meal_name: "Porridge", portion: 1.5 })])]);
        return json(makeEntry({ portion: 1.5 }));
      },
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Increase portion of Porridge" }));
    expect(await screen.findByText("1.5 servings")).toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m1", portion: 1.5 });
  });

  it("puts the old meal back and toasts the reason when the server refuses the swap", async () => {
    fakeApi({ "GET /plan": () => json(porridgeDay()), ...catalogue, "PUT /plan/:date/:slot": () => problem(400, "invalid_meal") });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    await waitFor(() => expect(toastError).toHaveBeenCalledWith("That meal isn't available. It may have been deleted."));
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
    expect(screen.queryByRole("link", { name: "Pasta bowl" })).not.toBeInTheDocument();
  });

  it("removes a meal", async () => {
    let server = porridgeDay();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "DELETE /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [])]);
        return noContent();
      },
    });
    renderWithClient(<Harness />);
    await userEvent.click(await screen.findByRole("button", { name: "Remove Porridge from breakfast" }));
    await waitFor(() => expect(screen.queryByRole("link", { name: "Porridge" })).not.toBeInTheDocument());
    expect(fake.callsTo("DELETE", `/plan/${D}/breakfast`)).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/components/macro-rings.test.tsx src/features/plan/meal-picker.test.tsx src/features/plan/slot-row.test.tsx src/features/plan/day-meals.test.tsx`
Expected: FAIL: `Failed to resolve import "./macro-rings"`, `"./meal-picker"`, `"./slot-row"`, `"./day-meals"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/lib/use-load-all-pages.ts`**

```ts
import { useEffect } from "react";

type Pageable = { hasNextPage: boolean; isFetchingNextPage: boolean; isFetchNextPageError: boolean; fetchNextPage: () => unknown };

/** Keeps asking for the next page until there is none. It stops once a page has failed, so a broken server is not hammered. */
export function useLoadAllPages({ hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage }: Pageable): void {
  useEffect(() => {
    if (hasNextPage && !isFetchingNextPage && !isFetchNextPageError) void fetchNextPage();
  }, [hasNextPage, isFetchingNextPage, isFetchNextPageError, fetchNextPage]);
}
```

**Create `web/src/components/macro-rings.tsx`**

```tsx
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
```

**Create `web/src/features/plan/meal-picker.tsx`**

```tsx
"use client";

import Link from "next/link";
import { useState } from "react";
import { ErrorState } from "@/components/error-state";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { servingsLabel } from "@/features/meals/meal-list";
import { useMeals, type MealSummary } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { useLoadAllPages } from "@/lib/use-load-all-pages";

type Props = { open: boolean; onOpenChange: (open: boolean) => void; title: string; onPick: (meal: MealSummary) => void };

/** Choose one of my meals for a slot. The list is filtered in the browser over all of my meals, which load a page at a time. */
export function MealPicker({ open, onOpenChange, title, onPick }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>Choose one of your meals.</DialogDescription>
        </DialogHeader>
        <PickerBody
          onPick={(meal) => {
            onPick(meal);
            onOpenChange(false);
          }}
        />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts with an empty filter every time. */
function PickerBody({ onPick }: { onPick: (meal: MealSummary) => void }) {
  const query = useMeals("mine");
  const [filter, setFilter] = useState("");
  useLoadAllPages(query);

  if (query.isPending) {
    return (
      <div role="status" aria-label="Loading meals" className="grid gap-2">
        <Skeleton className="h-10 rounded-lg" />
        <Skeleton className="h-10 rounded-lg" />
      </div>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={() => void query.refetch()} />;

  const meals = query.data.pages.flatMap((page) => page.items);
  if (meals.length === 0 && !query.hasNextPage) {
    return (
      <p className="text-muted-foreground text-sm">
        You have no meals yet.{" "}
        <Link href="/meals/new" className="text-primary font-medium underline-offset-4 hover:underline">
          Create a meal
        </Link>
      </p>
    );
  }

  const needle = filter.trim().toLowerCase();
  const shown = meals.filter((meal) => meal.name.toLowerCase().includes(needle));

  return (
    <div className="grid gap-3">
      <div className="grid gap-1.5">
        <Label htmlFor="meal-picker-filter">Search your meals</Label>
        <Input id="meal-picker-filter" autoComplete="off" className="h-11 text-base md:text-sm" value={filter} onChange={(event) => setFilter(event.target.value)} />
      </div>
      {shown.length === 0 ? (
        <p className="text-muted-foreground text-sm">No meal matches “{filter.trim()}”.</p>
      ) : (
        <ul className="grid max-h-80 gap-1 overflow-y-auto">
          {shown.map((meal) => (
            <li key={meal.id}>
              <button
                type="button"
                onClick={() => onPick(meal)}
                className="hover:bg-muted focus-visible:ring-ring/50 flex w-full items-center justify-between gap-3 rounded-lg px-3 py-2 text-left text-sm outline-none focus-visible:ring-3"
              >
                <span className="truncate font-medium">{meal.name}</span>
                <span className="text-muted-foreground shrink-0">{servingsLabel(meal.servings)}</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {query.hasNextPage && !query.isFetchNextPageError ? (
        <p role="status" className="text-muted-foreground text-xs">
          Loading more meals…
        </p>
      ) : null}
      {query.isFetchNextPageError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
    </div>
  );
}
```

**Create `web/src/features/plan/slot-row.tsx`**

```tsx
"use client";

import { Minus, Plus, X } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { servingsLabel } from "@/features/meals/meal-list";
import { MealPicker } from "./meal-picker";
import { SLOT_LABELS, type PlanEntry, type Slot } from "./plan-cache";

/** The API takes a portion above 0 and up to 100; the stepper moves in halves. */
export const PORTION_STEP = 0.5;
export const MAX_PORTION = 100;

type Choice = { id: string; name: string };
type Props = { slot: Slot; entries: PlanEntry[]; onPick: (meal: Choice, portion: number) => void; onClear: () => void };

const stepped = (portion: number, by: number) => Math.round((portion + by) * 100) / 100;

/**
 * One slot of one day. Breakfast, lunch and dinner hold a single meal that can be swapped, resized or removed. Snacks can only
 * be added or cleared as a group: the API addresses a snack by date and slot alone, so it cannot edit one of several.
 */
export function SlotRow({ slot, entries, onPick, onClear }: Props) {
  const label = SLOT_LABELS[slot];
  const lower = label.toLowerCase();
  // The portion a meal chosen in the open picker will get; null while the picker is closed.
  const [pickerPortion, setPickerPortion] = useState<number | null>(null);
  const [confirmingClear, setConfirmingClear] = useState(false);
  const single = slot === "snack" ? undefined : entries[0];

  return (
    <div className="bg-card grid gap-2 rounded-xl border p-3 sm:grid-cols-[6rem_1fr] sm:items-center">
      <h3 className="text-sm font-medium">{label}</h3>
      <div className="grid gap-2">
        {slot === "snack" ? (
          <>
            {entries.length > 0 ? (
              <ul className="grid gap-1">
                {entries.map((entry) => (
                  <li key={entry.id} className="flex items-center gap-2 text-sm">
                    <Link href={`/meals/${entry.meal_id}`} className="min-w-0 flex-1 truncate font-medium hover:underline">
                      {entry.meal_name}
                    </Link>
                    <span className="text-muted-foreground tabular-nums">{servingsLabel(entry.portion)}</span>
                  </li>
                ))}
              </ul>
            ) : null}
            <div className="flex flex-wrap gap-2">
              <Button type="button" variant="outline" size="sm" onClick={() => setPickerPortion(1)}>
                <Plus aria-hidden />
                Add snack
              </Button>
              {entries.length > 0 ? (
                <Button type="button" variant="ghost" size="sm" onClick={() => setConfirmingClear(true)}>
                  Clear snacks
                </Button>
              ) : null}
            </div>
          </>
        ) : single ? (
          <div className="flex flex-wrap items-center gap-2">
            <Link href={`/meals/${single.meal_id}`} className="min-w-0 flex-1 truncate font-medium hover:underline">
              {single.meal_name}
            </Link>
            <div className="flex items-center gap-1">
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={`Decrease portion of ${single.meal_name}`}
                disabled={single.portion <= PORTION_STEP}
                onClick={() => onPick({ id: single.meal_id, name: single.meal_name }, stepped(single.portion, -PORTION_STEP))}
              >
                <Minus aria-hidden />
              </Button>
              <span className="min-w-20 text-center text-sm tabular-nums">{servingsLabel(single.portion)}</span>
              <Button
                type="button"
                variant="outline"
                size="icon-sm"
                aria-label={`Increase portion of ${single.meal_name}`}
                disabled={single.portion + PORTION_STEP > MAX_PORTION}
                onClick={() => onPick({ id: single.meal_id, name: single.meal_name }, stepped(single.portion, PORTION_STEP))}
              >
                <Plus aria-hidden />
              </Button>
            </div>
            <Button type="button" variant="ghost" size="sm" aria-label={`Swap ${lower} meal`} onClick={() => setPickerPortion(single.portion)}>
              Swap
            </Button>
            <Button type="button" variant="ghost" size="icon-sm" aria-label={`Remove ${single.meal_name} from ${lower}`} onClick={onClear}>
              <X aria-hidden />
            </Button>
          </div>
        ) : (
          <div>
            <Button type="button" variant="outline" size="sm" aria-label={`Add meal for ${lower}`} onClick={() => setPickerPortion(1)}>
              <Plus aria-hidden />
              Add meal
            </Button>
          </div>
        )}
      </div>

      <MealPicker
        open={pickerPortion !== null}
        onOpenChange={(open) => {
          if (!open) setPickerPortion(null);
        }}
        title={slot === "snack" ? "Add a snack" : `Choose ${lower}`}
        onPick={(meal) => onPick(meal, pickerPortion ?? 1)}
      />

      <Dialog open={confirmingClear} onOpenChange={setConfirmingClear}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Clear all snacks?</DialogTitle>
            <DialogDescription>This removes every snack planned for this day.</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingClear(false)}>
              Keep them
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={() => {
                setConfirmingClear(false);
                onClear();
              }}
            >
              Clear snacks
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
```

**Create `web/src/features/plan/day-meals.tsx`**

```tsx
"use client";

import { toast } from "sonner";
import { problemMessage } from "@/lib/api/problem";
import { SLOTS, type PlanEntry } from "./plan-cache";
import { useClearPlanSlot, useSetPlanEntry } from "./queries";
import { SlotRow } from "./slot-row";

/** The four slots of one date, wired to the optimistic plan writes. A refused change is rolled back and explained in a toast. */
export function DayMeals({ date, entries }: { date: string; entries: PlanEntry[] }) {
  const fail = (error: Error) => toast.error(problemMessage(error));
  const set = useSetPlanEntry(fail);
  const clear = useClearPlanSlot(fail);

  return (
    <div className="grid gap-2">
      {SLOTS.map((slot) => (
        <SlotRow
          key={slot}
          slot={slot}
          entries={entries.filter((entry) => entry.slot === slot)}
          onPick={(meal, portion) => set.mutate({ date, slot, mealId: meal.id, mealName: meal.name, portion })}
          onClear={() => clear.mutate({ date, slot })}
        />
      ))}
    </div>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/components/macro-rings.test.tsx src/features/plan`
Expected: PASS. If a `getByRole("button", { name: /Pasta bowl/ })` finds two buttons, the picker for another slot is open: each `SlotRow` mounts its own closed `MealPicker`, and `PickerBody` mounts only while open, so only one list can exist at a time.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/lib/use-load-all-pages.ts web/src/components/macro-rings.tsx web/src/components/macro-rings.test.tsx web/src/features/plan
git commit -m "feat(web): add macro rings, the meal picker and the slot rows with one-tap swaps"
```

---

### Task 5: Today

**Files:**
- Create: `web/src/lib/use-today.ts`, `web/src/features/plan/today-view.tsx`
- Modify: `web/src/app/(app)/today/page.tsx`
- Test: `web/src/lib/use-today.test.ts`, `web/src/features/plan/today-view.test.tsx`

**Interfaces:**
- Consumes: `today`, `formatLongDate` (`@/lib/dates`); `usePlan`; `MacroRings`; `DayMeals`; `zeroNutrition`; `ErrorState`; `Skeleton`; `Button`.
- Produces:
  - `useToday(pollMs = 60_000): string`: the local calendar date, re-read every `pollMs` so a page left open past midnight moves to the new day.
  - `TodayView()`: the long date, `MacroRings` of today's `nutrition_per_day` against the plan's `targets` (dimmed while refetching), an empty-day card that links to `/plan`, and `DayMeals` for today. Skeleton while loading; an `ErrorState` with retry on failure.

- [ ] **Step 1: Write the failing tests**

**Create `web/src/lib/use-today.test.ts`**

```ts
// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useToday } from "./use-today";

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

describe("useToday", () => {
  it("is the local calendar day even late in the evening, when the UTC day may already be another", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 23, 30));
    const { result } = renderHook(() => useToday());
    expect(result.current).toBe("2026-09-30");
  });

  it("moves to the next day once midnight passes while the page stays open", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 23, 59, 30));
    const { result } = renderHook(() => useToday());
    expect(result.current).toBe("2026-09-30");
    act(() => void vi.advanceTimersByTime(60_000));
    expect(result.current).toBe("2026-10-01");
  });

  it("keeps the same value while the day has not changed", () => {
    vi.setSystemTime(new Date(2026, 8, 30, 10, 0));
    const { result } = renderHook(() => useToday());
    act(() => void vi.advanceTimersByTime(5 * 60_000));
    expect(result.current).toBe("2026-09-30");
  });
});
```

**Create `web/src/features/plan/today-view.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeSummary, nutrients, unknownNutrients } from "@/test/fixtures";
import { TARGETS, makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TodayView } from "./today-view";

// 23:30 local: the UTC date is already tomorrow's in a positive offset. The page must ask for the local day.
const DAY = "2026-09-30";
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 23, 30) }));
afterEach(() => vi.useRealTimers());

const planFor = (entries = [makeEntry({ date: DAY, meal_name: "Porridge", meal_id: "m1", portion: 1 })], nutrition = nutrients({ calories: 1200, protein: 90, carbohydrates: 150, fat: 40 })) =>
  makePlan(DAY, DAY, [makeDay(DAY, entries, nutrition)], TARGETS);

describe("TodayView", () => {
  it("asks for today's local date and shows it", async () => {
    const fake = fakeApi({ "GET /plan": () => json(planFor()) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText("Wednesday, September 30")).toBeInTheDocument();
    expect(fake.calls[0]?.search.get("from")).toBe(DAY);
    expect(fake.calls[0]?.search.get("to")).toBe(DAY);
  });

  it("shows the day's rings against the targets and the day's meals", async () => {
    fakeApi({ "GET /plan": () => json(planFor()) });
    renderWithClient(<TodayView />);
    const rings = await screen.findByRole("region", { name: "Today's totals" });
    expect(within(rings).getByText("1,200 kcal")).toBeInTheDocument();
    expect(within(rings).getByText("60% of 2,000 kcal")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Porridge" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Add meal for lunch" })).toBeInTheDocument();
  });

  it("swaps breakfast in one tap: the new meal shows at once and the day's totals are refetched", async () => {
    let server = planFor();
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "GET /meals": () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" })], next_cursor: null }),
      "PUT /plan/:date/:slot": () => {
        server = planFor([makeEntry({ date: DAY, meal_id: "m2", meal_name: "Pasta bowl" })], nutrients({ calories: 1500 }));
        return json(makeEntry({ date: DAY, meal_id: "m2", meal_name: "Pasta bowl" }));
      },
    });
    renderWithClient(<TodayView />);
    await userEvent.click(await screen.findByRole("button", { name: "Swap breakfast meal" }));
    await userEvent.click(await screen.findByRole("button", { name: /Pasta bowl/ }));

    expect(await screen.findByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
    expect(fake.callsTo("PUT", `/plan/${DAY}/breakfast`)[0]?.body).toEqual({ meal_id: "m2", portion: 1 });
    expect(await screen.findByText("1,500 kcal")).toBeInTheDocument();
  });

  it("invites you to plan an empty day", async () => {
    fakeApi({ "GET /plan": () => json(planFor([], nutrients())) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText(/Nothing planned for today yet/)).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Apply a diet template" })).toHaveAttribute("href", "/plan");
    expect(screen.getByRole("button", { name: "Add meal for breakfast" })).toBeInTheDocument();
  });

  it("shows a nutrient that some ingredient lacks as a dash, not zero", async () => {
    fakeApi({ "GET /plan": () => json(planFor([makeEntry({ date: DAY })], unknownNutrients({ calories: 800 }))) });
    renderWithClient(<TodayView />);
    const rings = await screen.findByRole("region", { name: "Today's totals" });
    expect(within(within(rings).getByRole("group", { name: "Protein" })).getByText("—")).toBeInTheDocument();
  });

  it("offers a retry when the plan cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /plan": () => (fail ? problem(500, "internal_error") : json(planFor())) });
    renderWithClient(<TodayView />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("link", { name: "Porridge" })).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/lib/use-today.test.ts src/features/plan/today-view.test.tsx`
Expected: FAIL: `Failed to resolve import "./use-today"` and `"./today-view"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/lib/use-today.ts`**

```ts
import { useEffect, useState } from "react";
import { today } from "@/lib/dates";

/** Today's local calendar date, re-read every `pollMs`, so a page left open past midnight moves on to the new day. */
export function useToday(pollMs = 60_000): string {
  const [date, setDate] = useState(() => today());
  useEffect(() => {
    const timer = setInterval(() => setDate(today()), pollMs);
    return () => clearInterval(timer);
  }, [pollMs]);
  return date;
}
```

**Create `web/src/features/plan/today-view.tsx`**

```tsx
"use client";

import Link from "next/link";
import { ErrorState } from "@/components/error-state";
import { MacroRings } from "@/components/macro-rings";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { formatLongDate } from "@/lib/dates";
import { zeroNutrition } from "@/lib/nutrition";
import { useToday } from "@/lib/use-today";
import { DayMeals } from "./day-meals";
import { usePlan } from "./queries";

/** Today's rings against the targets, and today's meals with one-tap swaps and portion changes. */
export function TodayView() {
  const date = useToday();
  const plan = usePlan(date, date);

  if (plan.isPending) {
    return (
      <div role="status" aria-label="Loading today" className="grid gap-4">
        <Skeleton className="h-52 rounded-xl" />
        <Skeleton className="h-40 rounded-xl" />
      </div>
    );
  }
  if (plan.data === undefined) return <ErrorState message={problemMessage(plan.error)} onRetry={() => void plan.refetch()} />;

  const day = plan.data.days.find((d) => d.date === date);
  const entries = day?.entries ?? [];

  return (
    <div className="grid gap-6">
      <p className="text-muted-foreground -mt-4 text-sm">{formatLongDate(date)}</p>
      <MacroRings nutrition={day?.nutrition_per_day ?? zeroNutrition()} targets={plan.data.targets} label="Today's totals" stale={plan.isFetching} />
      {entries.length === 0 ? (
        <div className="bg-card flex flex-col items-start gap-3 rounded-xl border p-4">
          <p className="text-sm">Nothing planned for today yet. Add a meal below, or apply a diet template to a whole week.</p>
          <Button asChild variant="outline" size="sm">
            <Link href="/plan">Apply a diet template</Link>
          </Button>
        </div>
      ) : null}
      <section aria-labelledby="today-meals">
        <h2 id="today-meals" className="sr-only">
          Meals
        </h2>
        <DayMeals date={date} entries={entries} />
      </section>
    </div>
  );
}
```

**Replace the contents of `web/src/app/(app)/today/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { TodayView } from "@/features/plan/today-view";

export const metadata: Metadata = { title: "Today" };

export default function Page() {
  return (
    <>
      <PageHeader title="Today" />
      <TodayView />
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/lib/use-today.test.ts src/features/plan/today-view.test.tsx`
Expected: PASS. If `today-view.test.tsx` times out in `findBy...`, the fake `Date` may be stalling Testing Library's polling: the `toFake: ["Date"]` option leaves the timers real, so it should not; if it does, fake the date with `vi.setSystemTime` inside each test instead of `beforeEach`.

- [ ] **Step 5: Lint, check the app shell test, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/lib/use-today.ts web/src/lib/use-today.test.ts web/src/features/plan/today-view.tsx web/src/features/plan/today-view.test.tsx "web/src/app/(app)/today/page.tsx"
git commit -m "feat(web): add Today with macro rings against targets and one-tap swaps"
```

### Task 6: The Plan week and applying a template

**Files:**
- Create: `web/src/features/plan/apply-template-dialog.tsx`, `web/src/features/plan/plan-view.tsx`
- Modify: `web/src/app/(app)/plan/page.tsx`
- Test: `web/src/features/plan/plan-view.test.tsx`

**Interfaces:**
- Consumes: `usePlan`, `useApplyTemplate` (`queries`); `useTemplates` (`template-queries`); `DayMeals`; `useLoadAllPages`; `useToday`; `addDays`, `startOfWeek`, `today`, `isIsoDate`, `formatLongDate`, `formatWeekRange` (`@/lib/dates`); `sumNutrition`, `scaleNutrition`, `targetProgress`, `formatAmount`, `NutrientAmounts` (`@/lib/nutrition`); `Field`, `NativeSelect`, `Badge`, `Button`, `Dialog*`, `ErrorState`, `Skeleton`, `ApiError`, `problemMessage`.
- Produces:
  - `ApplyTemplateDialog({ open, onOpenChange, defaultStart: string; onApplied(startDate: string): void })`: "Apply a diet template" with a template select (my templates only, all pages loaded, first one preselected), a "Start date" date input, and "Apply template". A `409 plan_conflict` shows an alert "Some of those days already have meals. Replace them?" with "Replace them" (retries once with `overwrite: true`) and "Keep my plan" (dismisses, sends nothing). With no templates it says "You have no diet templates yet." and links to `/plan/templates/new`. Success toasts "Template applied.", calls `onApplied(startDate)` and closes.
  - `PlanView()`: the week (Monday to Sunday) containing today, with "Previous week", "Next week" and "This week" (disabled while on the current week), a week range label, an "Apply template" button that opens the dialog, and a "Diet templates" link to `/plan/templates`; a "Week totals" section (week total and daily average over seven days, unknown if any day is unknown, with the average's share of the kcal target); one section per day headed by its long date (with a "Today" badge), that day's kcal against the target and its macros, and its `DayMeals`. After a template is applied the view moves to the week of the start date.

- [ ] **Step 1: Write the failing test**

**Create `web/src/features/plan/plan-view.test.tsx`**

```tsx
// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { addDays, weekDates } from "@/lib/dates";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { nutrients, unknownNutrients } from "@/test/fixtures";
import { makeDay, makeEntry, makePlan, makeTemplateSummary } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { PlanView } from "./plan-view";

// Wednesday 30 September 2026: the week is Monday 28 September to Sunday 4 October.
beforeEach(() => vi.useFakeTimers({ toFake: ["Date"], now: new Date(2026, 8, 30, 10, 0) }));
afterEach(() => vi.useRealTimers());

/** A week from `from`; `kcal` maps a date to that day's calories (a day with calories gets one meal). */
function week(from: string, kcal: Record<string, number | null> = {}) {
  return makePlan(
    from,
    addDays(from, 6),
    weekDates(from).map((date) => {
      const value = kcal[date];
      const entries = value === undefined ? [] : [makeEntry({ date, meal_name: `Meal ${date}` })];
      const nutrition = value === null ? unknownNutrients() : nutrients({ calories: value ?? 0, protein: (value ?? 0) / 10 });
      return makeDay(date, entries, nutrition);
    }),
  );
}

const planRoute = (kcal: Record<string, number | null> = {}) => ({ "GET /plan": (req: { search: URLSearchParams }) => json(week(req.search.get("from") ?? "2026-09-28", kcal)) });
const templates = { "GET /diet-templates": () => json({ items: [makeTemplateSummary({ id: "t1", name: "Base week", day_count: 7 })], next_cursor: null }) };

describe("PlanView", () => {
  it("shows the Monday-to-Sunday week that holds today, with today marked", async () => {
    const fake = fakeApi(planRoute());
    renderWithClient(<PlanView />);
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
    expect(fake.calls[0]?.search.get("from")).toBe("2026-09-28");
    expect(fake.calls[0]?.search.get("to")).toBe("2026-10-04");
    const wednesday = screen.getByRole("region", { name: "Wednesday, September 30" });
    expect(within(wednesday).getByText("Today")).toBeInTheDocument();
    expect(screen.getAllByText("Today")).toHaveLength(1);
    expect(screen.getByRole("region", { name: "Monday, September 28" })).toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Sunday, October 4" })).toBeInTheDocument();
  });

  it("shows each day's calories against the target, and the week's total and daily average", async () => {
    fakeApi(planRoute({ "2026-09-28": 600, "2026-09-30": 400 }));
    renderWithClient(<PlanView />);
    const monday = await screen.findByRole("region", { name: "Monday, September 28" });
    expect(within(monday).getByText(/600 kcal of 2,000 kcal/)).toBeInTheDocument();

    const totals = screen.getByRole("region", { name: "Week totals" });
    expect(within(totals).getByText("1,000 kcal")).toBeInTheDocument();
    expect(within(totals).getByText("143 kcal")).toBeInTheDocument();
    expect(within(totals).getByText("7% of your 2,000 kcal target")).toBeInTheDocument();
  });

  it("does not turn an unknown day into zero: the week total is unknown too", async () => {
    fakeApi(planRoute({ "2026-09-28": 600, "2026-09-29": null }));
    renderWithClient(<PlanView />);
    const tuesday = await screen.findByRole("region", { name: "Tuesday, September 29" });
    expect(within(tuesday).getByText(/^—/)).toBeInTheDocument();
    const totals = screen.getByRole("region", { name: "Week totals" });
    expect(within(totals).getAllByText("—").length).toBeGreaterThanOrEqual(2);
    expect(within(totals).queryByText("600 kcal")).not.toBeInTheDocument();
  });

  it("moves between weeks and back to this one", async () => {
    const fake = fakeApi(planRoute());
    renderWithClient(<PlanView />);
    await screen.findByText("Sep 28 – Oct 4");
    expect(screen.getByRole("button", { name: "This week" })).toBeDisabled();

    await userEvent.click(screen.getByRole("button", { name: "Next week" }));
    expect(await screen.findByText("Oct 5 – Oct 11")).toBeInTheDocument();
    expect(fake.calls.at(-1)?.search.get("from")).toBe("2026-10-05");
    expect(screen.getByRole("button", { name: "This week" })).toBeEnabled();

    await userEvent.click(screen.getByRole("button", { name: "Previous week" }));
    await userEvent.click(screen.getByRole("button", { name: "Previous week" }));
    expect(await screen.findByText("Sep 21 – Sep 27")).toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: "This week" }));
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
  });

  it("links to the diet templates", async () => {
    fakeApi(planRoute());
    renderWithClient(<PlanView />);
    expect(await screen.findByRole("link", { name: "Diet templates" })).toHaveAttribute("href", "/plan/templates");
  });

  it("offers a retry when the week cannot be loaded", async () => {
    let fail = true;
    fakeApi({ "GET /plan": () => (fail ? problem(500, "internal_error") : json(week("2026-09-28"))) });
    renderWithClient(<PlanView />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Sep 28 – Oct 4")).toBeInTheDocument();
  });
});

describe("applying a template", () => {
  async function openDialog() {
    await screen.findByText("Sep 28 – Oct 4");
    await userEvent.click(screen.getByRole("button", { name: "Apply template" }));
    return screen.findByRole("dialog", { name: "Apply a diet template" });
  }

  it("applies my template from the chosen date, then shows the week that date is in", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => noContent() });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    expect(await within(dialog).findByRole("option", { name: "Base week (7 days)" })).toBeInTheDocument();
    expect(within(dialog).getByLabelText("Start date")).toHaveValue("2026-09-28");

    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "2026-10-05" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));

    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(1));
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")[0]?.body).toEqual({ start_date: "2026-10-05", overwrite: false });
    expect(await screen.findByText("Oct 5 – Oct 11")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("asks before replacing, and leaves the plan alone when the person keeps it", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => problem(409, "plan_conflict") });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));

    expect(await within(dialog).findByText(/Some of those days already have meals\. Replace them\?/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Keep my plan" }));
    expect(within(dialog).queryByText(/Replace them\?/)).not.toBeInTheDocument();
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(1);
    expect(screen.getByRole("dialog", { name: "Apply a diet template" })).toBeInTheDocument();
  });

  it("retries once with overwrite when the person says replace", async () => {
    let attempts = 0;
    const fake = fakeApi({
      ...planRoute(),
      ...templates,
      "POST /diet-templates/:id/apply": () => (++attempts === 1 ? problem(409, "plan_conflict") : noContent()),
    });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    await userEvent.click(await within(dialog).findByRole("button", { name: "Replace them" }));

    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(2));
    expect(fake.callsTo("POST", "/diet-templates/t1/apply").map((c) => c.body)).toEqual([
      { start_date: "2026-09-28", overwrite: false },
      { start_date: "2026-09-28", overwrite: true },
    ]);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
  });

  it("refuses a start date that is not a date, and sends nothing", async () => {
    const fake = fakeApi({ ...planRoute(), ...templates });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    fireEvent.change(within(dialog).getByLabelText("Start date"), { target: { value: "" } });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    expect(await within(dialog).findByText("Pick a start date.")).toBeInTheDocument();
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")).toHaveLength(0);
  });

  it("says so, and points to making one, when there are no templates", async () => {
    fakeApi({ ...planRoute(), "GET /diet-templates": () => json({ items: [], next_cursor: null }) });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    expect(await within(dialog).findByText("You have no diet templates yet.")).toBeInTheDocument();
    expect(within(dialog).getByRole("link", { name: "Create a template" })).toHaveAttribute("href", "/plan/templates/new");
    expect(within(dialog).queryByRole("button", { name: "Apply template" })).not.toBeInTheDocument();
  });

  it("explains any other failure in a toast-worthy message and keeps the dialog open", async () => {
    fakeApi({ ...planRoute(), ...templates, "POST /diet-templates/:id/apply": () => problem(404, "not_found") });
    renderWithClient(<PlanView />);
    const dialog = await openDialog();
    await within(dialog).findByRole("option", { name: "Base week (7 days)" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Apply template" }));
    await waitFor(() => expect(within(dialog).getByRole("button", { name: "Apply template" })).toBeEnabled());
    expect(screen.getByRole("dialog", { name: "Apply a diet template" })).toBeInTheDocument();
    expect(within(dialog).queryByText(/Replace them\?/)).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && npx vitest run src/features/plan/plan-view.test.tsx`
Expected: FAIL: `Failed to resolve import "./plan-view"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/plan/apply-template-dialog.tsx`**

```tsx
"use client";

import Link from "next/link";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { NativeSelect } from "@/components/native-select";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { isIsoDate } from "@/lib/dates";
import { useLoadAllPages } from "@/lib/use-load-all-pages";
import { useApplyTemplate } from "./queries";
import { useTemplates } from "./template-queries";

type Props = { open: boolean; onOpenChange: (open: boolean) => void; defaultStart: string; onApplied: (startDate: string) => void };

export function ApplyTemplateDialog({ open, onOpenChange, defaultStart, onApplied }: Props) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Apply a diet template</DialogTitle>
          <DialogDescription>Copies the template into your plan. After that, changing a day never changes the template.</DialogDescription>
        </DialogHeader>
        <ApplyForm defaultStart={defaultStart} onApplied={onApplied} onDone={() => onOpenChange(false)} />
      </DialogContent>
    </Dialog>
  );
}

/** Mounted only while the dialog is open, so it starts fresh every time. */
function ApplyForm({ defaultStart, onApplied, onDone }: { defaultStart: string; onApplied: (startDate: string) => void; onDone: () => void }) {
  const templates = useTemplates("mine");
  useLoadAllPages(templates);
  const apply = useApplyTemplate();
  const [templateId, setTemplateId] = useState("");
  const [start, setStart] = useState(defaultStart);
  const [problem, setProblem] = useState<string | null>(null);
  const [conflict, setConflict] = useState(false);

  if (templates.isPending) return <Skeleton role="status" aria-label="Loading templates" className="h-24 rounded-lg" />;
  const items = templates.data?.pages.flatMap((page) => page.items) ?? [];
  if (items.length === 0) {
    return (
      <p className="text-muted-foreground text-sm">
        You have no diet templates yet.{" "}
        <Link href="/plan/templates/new" className="text-primary font-medium underline-offset-4 hover:underline">
          Create a template
        </Link>
      </p>
    );
  }
  const chosen = templateId || items[0]?.id || "";

  function submit(overwrite: boolean) {
    if (!isIsoDate(start)) {
      setProblem("Pick a start date.");
      return;
    }
    setProblem(null);
    apply.mutate(
      { templateId: chosen, startDate: start, overwrite },
      {
        onSuccess: () => {
          toast.success("Template applied.");
          onApplied(start);
          onDone();
        },
        onError: (error) => {
          if (error instanceof ApiError && error.code === "plan_conflict") setConflict(true);
          else toast.error(problemMessage(error));
        },
      },
    );
  }

  return (
    <form
      noValidate
      className="grid gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        setConflict(false);
        submit(false);
      }}
    >
      <div className="grid gap-1.5">
        <Label htmlFor="apply-template">Template</Label>
        <NativeSelect
          id="apply-template"
          value={chosen}
          onChange={(event) => {
            setTemplateId(event.target.value);
            setConflict(false);
          }}
        >
          {items.map((template) => (
            <option key={template.id} value={template.id}>
              {template.name} ({template.day_count} {template.day_count === 1 ? "day" : "days"})
            </option>
          ))}
        </NativeSelect>
      </div>
      <Field
        name="apply-start"
        label="Start date"
        type="date"
        value={start}
        onChange={(event) => {
          setStart(event.target.value);
          setConflict(false);
        }}
        hint="Day 1 of the template lands on this date."
        error={problem ?? undefined}
      />
      {conflict ? (
        <div role="alert" className="bg-destructive/10 grid gap-2 rounded-lg p-3 text-sm">
          <p className="text-destructive">Some of those days already have meals. Replace them?</p>
          <p className="text-muted-foreground text-xs">Replacing swaps breakfast, lunch and dinner. Snacks are added to what is already there.</p>
          <div className="flex gap-2">
            <Button type="button" variant="destructive" size="sm" disabled={apply.isPending} onClick={() => submit(true)}>
              Replace them
            </Button>
            <Button type="button" variant="outline" size="sm" onClick={() => setConflict(false)}>
              Keep my plan
            </Button>
          </div>
        </div>
      ) : null}
      <p className="text-muted-foreground text-xs">To use your partner&apos;s template, copy it to your library first (Diet templates).</p>
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={apply.isPending || conflict}>
        {apply.isPending ? "Applying…" : "Apply template"}
      </Button>
    </form>
  );
}
```

**Create `web/src/features/plan/plan-view.tsx`**

```tsx
"use client";

import { ChevronLeft, ChevronRight } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { problemMessage } from "@/lib/api/problem";
import { addDays, formatLongDate, formatWeekRange, startOfWeek, today } from "@/lib/dates";
import { formatAmount, scaleNutrition, sumNutrition, targetProgress, type NutrientAmounts } from "@/lib/nutrition";
import { useToday } from "@/lib/use-today";
import { ApplyTemplateDialog } from "./apply-template-dialog";
import { DayMeals } from "./day-meals";
import type { DailyTotal, Targets } from "./plan-cache";
import { usePlan } from "./queries";

const macroLine = (n: NutrientAmounts) => `Protein ${formatAmount(n.protein, "g")} · Carbs ${formatAmount(n.carbohydrates, "g")} · Fat ${formatAmount(n.fat, "g")}`;

/** One week, Monday to Sunday: totals, and each day's meals with one-tap swaps. */
export function PlanView() {
  const todayDate = useToday();
  const [weekStart, setWeekStart] = useState(() => startOfWeek(today()));
  const [applying, setApplying] = useState(false);
  const plan = usePlan(weekStart, addDays(weekStart, 6));
  const onCurrentWeek = weekStart === startOfWeek(todayDate);

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-2">
        <Button type="button" variant="outline" size="icon" aria-label="Previous week" onClick={() => setWeekStart(addDays(weekStart, -7))}>
          <ChevronLeft aria-hidden />
        </Button>
        <p aria-live="polite" className="min-w-36 text-center font-medium">
          {formatWeekRange(weekStart)}
        </p>
        <Button type="button" variant="outline" size="icon" aria-label="Next week" onClick={() => setWeekStart(addDays(weekStart, 7))}>
          <ChevronRight aria-hidden />
        </Button>
        <Button type="button" variant="ghost" disabled={onCurrentWeek} onClick={() => setWeekStart(startOfWeek(todayDate))}>
          This week
        </Button>
        <div className="flex flex-1 flex-wrap justify-end gap-2">
          <Button asChild variant="outline">
            <Link href="/plan/templates">Diet templates</Link>
          </Button>
          <Button type="button" onClick={() => setApplying(true)}>
            Apply template
          </Button>
        </div>
      </div>

      {plan.isPending ? (
        <div role="status" aria-label="Loading the week" className="grid gap-4">
          <Skeleton className="h-24 rounded-xl" />
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : plan.data === undefined ? (
        <ErrorState message={problemMessage(plan.error)} onRetry={() => void plan.refetch()} />
      ) : (
        <>
          <WeekTotals days={plan.data.days} targets={plan.data.targets} />
          <div className="grid gap-4 lg:grid-cols-2">
            {plan.data.days.map((day) => (
              <DayCard key={day.date} day={day} targets={plan.data.targets} isToday={day.date === todayDate} />
            ))}
          </div>
        </>
      )}

      <ApplyTemplateDialog open={applying} onOpenChange={setApplying} defaultStart={weekStart} onApplied={(start) => setWeekStart(startOfWeek(start))} />
    </div>
  );
}

function WeekTotals({ days, targets }: { days: DailyTotal[]; targets: Targets }) {
  const total = sumNutrition(days.map((day) => day.nutrition_per_day));
  const average = scaleNutrition(total, 1 / 7);
  const progress = targetProgress(average.calories, targets.target_kcal);
  return (
    <section aria-label="Week totals" className="bg-card grid gap-4 rounded-xl border p-4 sm:grid-cols-2">
      <div>
        <h2 className="text-sm font-medium">Week total</h2>
        <p className="text-lg font-semibold tabular-nums">{formatAmount(total.calories, "kcal")}</p>
        <p className="text-muted-foreground text-xs">{macroLine(total)}</p>
      </div>
      <div>
        <h2 className="text-sm font-medium">Daily average</h2>
        <p className="text-lg font-semibold tabular-nums">{formatAmount(average.calories, "kcal")}</p>
        <p className="text-muted-foreground text-xs">{macroLine(average)}</p>
        {progress ? <p className="text-muted-foreground text-xs">{`${progress.percent}% of your ${formatAmount(targets.target_kcal, "kcal")} target`}</p> : null}
      </div>
    </section>
  );
}

function DayCard({ day, targets, isToday }: { day: DailyTotal; targets: Targets; isToday: boolean }) {
  const kcal = day.nutrition_per_day.calories;
  const target = targets.target_kcal !== null && targets.target_kcal > 0 ? ` of ${formatAmount(targets.target_kcal, "kcal")}` : "";
  const heading = formatLongDate(day.date);
  return (
    <section aria-label={heading} className="bg-muted/30 grid content-start gap-3 rounded-xl border p-3">
      <header className="flex items-baseline justify-between gap-2">
        <h2 className="font-semibold">{heading}</h2>
        {isToday ? <Badge>Today</Badge> : null}
      </header>
      <p className="text-muted-foreground text-sm tabular-nums">
        {formatAmount(kcal, "kcal")}
        {target} · {macroLine(day.nutrition_per_day)}
      </p>
      <DayMeals date={day.date} entries={day.entries} />
    </section>
  );
}
```

**Replace the contents of `web/src/app/(app)/plan/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { PlanView } from "@/features/plan/plan-view";

export const metadata: Metadata = { title: "Plan" };

export default function Page() {
  return (
    <>
      <PageHeader title="Plan" />
      <PlanView />
    </>
  );
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && npx vitest run src/features/plan/plan-view.test.tsx`
Expected: PASS. If "does not turn an unknown day into zero" finds no `/^—/` text in Tuesday, the day line starts with the kcal amount: `formatAmount(null, "kcal")` is "—", so the paragraph reads "— of 2,000 kcal · Protein …" and `getByText(/^—/)` matches that paragraph.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/plan/apply-template-dialog.tsx web/src/features/plan/plan-view.tsx web/src/features/plan/plan-view.test.tsx "web/src/app/(app)/plan/page.tsx"
git commit -m "feat(web): add the Plan week with totals and applying a template with a replace confirmation"
```

---

### Task 7: The diet template library and read-only view

**Files:**
- Create: `web/src/features/plan/copy-template-button.tsx`, `web/src/features/plan/template-list.tsx`, `web/src/features/plan/templates-page.tsx`, `web/src/features/plan/new-template-form.tsx`, `web/src/features/plan/template-view.tsx`, `web/src/app/(app)/plan/templates/page.tsx`, `web/src/app/(app)/plan/templates/new/page.tsx`
- Test: `web/src/features/plan/templates-page.test.tsx`, `web/src/features/plan/new-template-form.test.tsx`, `web/src/features/plan/template-view.test.tsx`

**Interfaces:**
- Consumes: `useTemplates`, `useCopyTemplate`, `useCreateTemplate`, `DietTemplate`, `DietTemplateSummary` (`template-queries`); `usePartnerLink`, `PARTNER_KEY` (`@/features/meals/queries`); `SLOTS`, `SLOT_LABELS` (`plan-cache`); `servingsLabel`; `parseDecimal`; `BackLink`-style link; `Tabs*`, `Badge`, `Skeleton`, `ErrorState`, `Field`, `Button`, `PageHeader`.
- Produces:
  - `daysLabel(n: number): string` ("1 day", "7 days") from `template-list.tsx`
  - `CopyTemplateButton({ templateId: string; templateName: string; variant?: "outline" | "default" })`: "Copy to my library" (template name as hidden text); on success toasts and `router.push("/plan/templates/{copy id}")`.
  - `TemplateList({ scope: "mine" | "partner" })`: skeleton, error (with "Try again" unless the partner is unlinked, which marks `PARTNER_KEY` stale), empty state ("No diet templates yet" with "Create your first template" for mine; text only for the partner's), rows linking to `/plan/templates/{id}` with `daysLabel`, a "Shared" badge on mine, `CopyTemplateButton` on the partner's, and "Load more".
  - `TemplatesPage()`: "New template" link to `/plan/templates/new`, a back link to `/plan`, and, with an active partner, "Mine" / "Partner's" tabs (no tabs otherwise).
  - `NewTemplateForm()`: Name and "Number of days" (default 7, a whole number 1 to 31); creates the template and `router.replace("/plan/templates/{id}")`.
  - `TemplateView({ template: DietTemplate })`: a partner's template read-only: the note "Shared by your partner. Copy it to your library to change or apply it.", `CopyTemplateButton` (default variant), and each day's slots (`Day 1` ... with slot label, meal name and portion).

- [ ] **Step 1: Write the failing tests**

**Create `web/src/features/plan/templates-page.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { PARTNER_KEY } from "@/features/meals/queries";
import { fakeApi, json, problem } from "@/test/fake-api";
import { partnership } from "@/test/fixtures";
import { makeTemplate, makeTemplateSummary } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplatesPage } from "./templates-page";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

const page = (items: ReturnType<typeof makeTemplateSummary>[], next_cursor: string | null = null) => json({ items, next_cursor });

async function partnerSettled(queryClient: ReturnType<typeof renderWithClient>["queryClient"]) {
  await waitFor(() => expect(queryClient.getQueryState(PARTNER_KEY)?.status).toBe("success"));
}

describe("TemplatesPage", () => {
  it("lists my templates with their length, a way to make one, and no tabs without a partner", async () => {
    fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /diet-templates": () => page([makeTemplateSummary({ shared_with_partner: true }), makeTemplateSummary({ id: "t2", name: "Cut", day_count: 1 })]),
    });
    const { queryClient } = renderWithClient(<TemplatesPage />);

    expect(await screen.findByRole("link", { name: /Base week/ })).toHaveAttribute("href", "/plan/templates/t1");
    expect(screen.getByRole("link", { name: /Base week/ })).toHaveTextContent("7 days");
    expect(screen.getByRole("link", { name: /Cut/ })).toHaveTextContent("1 day");
    expect(screen.getByText("Shared")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /New template/ })).toHaveAttribute("href", "/plan/templates/new");
    expect(screen.getByRole("link", { name: /Plan/ })).toHaveAttribute("href", "/plan");
    await partnerSettled(queryClient);
    expect(screen.queryByRole("tab")).not.toBeInTheDocument();
  });

  it("asks for the next page with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /partner": () => problem(404, "partner_not_linked"),
      "GET /diet-templates": (req) => (req.search.get("cursor") === "c1" ? page([makeTemplateSummary({ id: "t2", name: "Cut" })]) : page([makeTemplateSummary()], "c1")),
    });
    renderWithClient(<TemplatesPage />);
    await userEvent.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByRole("link", { name: /Cut/ })).toBeInTheDocument();
    expect(fake.callsTo("GET", "/diet-templates")[1]?.search.get("cursor")).toBe("c1");
  });

  it("invites you to make your first template when there are none", async () => {
    fakeApi({ "GET /partner": () => problem(404, "partner_not_linked"), "GET /diet-templates": () => page([]) });
    renderWithClient(<TemplatesPage />);
    expect(await screen.findByText("No diet templates yet")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "Create your first template" })).toHaveAttribute("href", "/plan/templates/new");
  });

  it("shows the partner's shared templates under their own tab, and copies one into my library", async () => {
    const copy = makeTemplate({ id: "copy1", name: "Bulk" });
    const fake = fakeApi({
      "GET /partner": () => json(partnership()),
      "GET /diet-templates": () => page([makeTemplateSummary()]),
      "GET /partner/diet-templates": () => page([makeTemplateSummary({ id: "p1", name: "Bulk" })]),
      "POST /diet-templates/:id/copy": () => json(copy, 201),
    });
    renderWithClient(<TemplatesPage />);

    await userEvent.click(await screen.findByRole("tab", { name: "Partner's" }));
    expect(await screen.findByRole("link", { name: /Bulk/ })).toHaveAttribute("href", "/plan/templates/p1");
    expect(screen.queryByRole("link", { name: /Base week/ })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });

  it("drops the partner tab, and shows my templates again, when the partner unlinks while it is open", async () => {
    let linked = true;
    fakeApi({
      "GET /partner": () => (linked ? json(partnership()) : problem(404, "partner_not_linked")),
      "GET /diet-templates": () => page([makeTemplateSummary()]),
      "GET /partner/diet-templates": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<TemplatesPage />);
    const tab = await screen.findByRole("tab", { name: "Partner's" });

    linked = false;
    await userEvent.click(tab);
    await waitFor(() => expect(screen.queryByRole("tab")).not.toBeInTheDocument());
    expect(await screen.findByRole("link", { name: /Base week/ })).toBeInTheDocument();
  });
});
```

**Create `web/src/features/plan/new-template-form.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeTemplate } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { NewTemplateForm } from "./new-template-form";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

describe("NewTemplateForm", () => {
  it("creates a week-long template by default and opens it in the editor", async () => {
    const fake = fakeApi({ "POST /diet-templates": () => json(makeTemplate({ id: "new1", name: "Cut" }), 201) });
    renderWithClient(<NewTemplateForm />);
    expect(screen.getByLabelText("Number of days")).toHaveValue("7");
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));

    await waitFor(() => expect(replace).toHaveBeenCalledWith("/plan/templates/new1"));
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Cut", day_count: 7 });
  });

  it("takes any length from 1 to 31 days", async () => {
    const fake = fakeApi({ "POST /diet-templates": () => json(makeTemplate({ id: "n2" }), 201) });
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Two days");
    await userEvent.clear(screen.getByLabelText("Number of days"));
    await userEvent.type(screen.getByLabelText("Number of days"), "31");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    await waitFor(() => expect(fake.callsTo("POST", "/diet-templates")).toHaveLength(1));
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Two days", day_count: 31 });
  });

  it.each(["0", "32", "abc", "2.5", "", "-1"])("refuses %j days, says what is allowed, and sends nothing", async (days) => {
    const fake = fakeApi({});
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.clear(screen.getByLabelText("Number of days"));
    if (days) await userEvent.type(screen.getByLabelText("Number of days"), days);
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    expect(await screen.findByText("Days must be a whole number from 1 to 31.")).toBeInTheDocument();
    expect(fake.calls).toHaveLength(0);
  });

  it("asks for a name", async () => {
    const fake = fakeApi({});
    renderWithClient(<NewTemplateForm />);
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    expect(await screen.findByText("Give the template a name.")).toBeInTheDocument();
    expect(fake.calls).toHaveLength(0);
  });

  it("stays on the form when the API refuses", async () => {
    fakeApi({ "POST /diet-templates": () => problem(500, "internal_error") });
    renderWithClient(<NewTemplateForm />);
    await userEvent.type(screen.getByLabelText("Name"), "Cut");
    await userEvent.click(screen.getByRole("button", { name: "Create template" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Create template" })).toBeEnabled());
    expect(replace).not.toHaveBeenCalled();
  });
});
```

**Create `web/src/features/plan/template-view.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json } from "@/test/fake-api";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateView } from "./template-view";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

const shared = makeTemplate({
  id: "p1",
  name: "Bulk",
  day_count: 2,
  is_owner: false,
  slots: [
    makeTemplateSlot({ id: "a", day_index: 1, slot: "dinner", meal_name: "Stew", portion: 2 }),
    makeTemplateSlot({ id: "b", day_index: 0, slot: "lunch", meal_name: "Wrap", portion: 1 }),
    makeTemplateSlot({ id: "c", day_index: 0, slot: "breakfast", meal_name: "Oat bowl", portion: 1.5 }),
    makeTemplateSlot({ id: "d", day_index: 0, slot: "snack", meal_name: "Apple", portion: 1 }),
  ],
});

describe("TemplateView", () => {
  it("shows every day's slots in the order of a day, read-only", () => {
    renderWithClient(<TemplateView template={shared} />);
    const day1 = within(screen.getByRole("region", { name: "Day 1" }));
    const rows = day1.getAllByRole("listitem").map((li) => li.textContent);
    expect(rows).toEqual(["BreakfastOat bowl1.5 servings", "LunchWrap1 serving", "SnacksApple1 serving"]);
    const day2 = within(screen.getByRole("region", { name: "Day 2" }));
    expect(day2.getByText("Stew")).toBeInTheDocument();
    expect(day2.getByText("2 servings")).toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /apply/i })).not.toBeInTheDocument();
  });

  it("says a day with nothing planned is empty", () => {
    renderWithClient(<TemplateView template={makeTemplate({ is_owner: false, day_count: 2, slots: [makeTemplateSlot()] })} />);
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByText("Nothing planned.")).toBeInTheDocument();
  });

  it("explains that it must be copied to change or apply it, and copies it", async () => {
    const fake = fakeApi({ "POST /diet-templates/:id/copy": () => json(makeTemplate({ id: "copy1", name: "Bulk" }), 201) });
    renderWithClient(<TemplateView template={shared} />);
    expect(screen.getByText("Shared by your partner. Copy it to your library to change or apply it.")).toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });
});
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/plan/templates-page.test.tsx src/features/plan/new-template-form.test.tsx src/features/plan/template-view.test.tsx`
Expected: FAIL: `Failed to resolve import "./templates-page"`, `"./new-template-form"`, `"./template-view"`.

- [ ] **Step 3: Write the implementation**

**Create `web/src/features/plan/copy-template-button.tsx`**

```tsx
"use client";

import { Copy } from "lucide-react";
import { useRouter } from "next/navigation";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { useCopyTemplate } from "./template-queries";

/** Copies a template (usually the partner's) into my own library and opens the copy. Only a copy can be edited or applied. */
export function CopyTemplateButton({ templateId, templateName, variant = "outline" }: { templateId: string; templateName: string; variant?: "outline" | "default" }) {
  const router = useRouter();
  const copy = useCopyTemplate();
  return (
    <Button
      type="button"
      variant={variant}
      size="sm"
      disabled={copy.isPending}
      onClick={() =>
        copy.mutate(templateId, {
          onSuccess: (template) => {
            toast.success("Copied to your library.");
            router.push(`/plan/templates/${template.id}`);
          },
          onError: (error) => toast.error(problemMessage(error)),
        })
      }
    >
      <Copy aria-hidden />
      {copy.isPending ? "Copying…" : "Copy to my library"}
      <span className="sr-only"> ({templateName})</span>
    </Button>
  );
}
```

**Create `web/src/features/plan/template-list.tsx`**

```tsx
"use client";

import { useQueryClient } from "@tanstack/react-query";
import Link from "next/link";
import { useEffect } from "react";
import { ErrorState } from "@/components/error-state";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { PARTNER_KEY } from "@/features/meals/queries";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { CopyTemplateButton } from "./copy-template-button";
import { useTemplates, type DietTemplateSummary } from "./template-queries";

type Scope = "mine" | "partner";

export function daysLabel(days: number): string {
  return `${days} ${days === 1 ? "day" : "days"}`;
}

export function TemplateList({ scope }: { scope: Scope }) {
  const query = useTemplates(scope);
  const queryClient = useQueryClient();
  const unlinked = query.error instanceof ApiError && query.error.code === "partner_not_linked";

  // The partner unlinked while their tab was open: refresh the link so the tab goes away.
  useEffect(() => {
    if (unlinked) void queryClient.invalidateQueries({ queryKey: PARTNER_KEY });
  }, [unlinked, queryClient]);

  if (query.isPending) {
    return (
      <ul aria-label="Loading templates" className="grid gap-3">
        {[0, 1, 2].map((n) => (
          <li key={n}>
            <Skeleton className="h-[4.5rem] rounded-xl" />
          </li>
        ))}
      </ul>
    );
  }
  if (query.data === undefined) return <ErrorState message={problemMessage(query.error)} onRetry={unlinked ? undefined : () => void query.refetch()} />;

  const templates = query.data.pages.flatMap((page) => page.items);
  if (templates.length === 0) return <EmptyState scope={scope} />;

  return (
    <div className="grid gap-3">
      <ul className="grid gap-3">
        {templates.map((template) => (
          <TemplateRow key={template.id} template={template} scope={scope} />
        ))}
      </ul>
      {query.isError ? <ErrorState message={problemMessage(query.error)} onRetry={() => void query.fetchNextPage()} /> : null}
      {query.hasNextPage ? (
        <Button type="button" variant="outline" className="justify-self-center" disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()}>
          {query.isFetchingNextPage ? "Loading…" : "Load more"}
        </Button>
      ) : null}
    </div>
  );
}

function TemplateRow({ template, scope }: { template: DietTemplateSummary; scope: Scope }) {
  return (
    <li className="bg-card flex items-center gap-3 rounded-xl border p-4">
      <Link href={`/plan/templates/${template.id}`} className="focus-visible:ring-ring/50 min-w-0 flex-1 rounded-md outline-none focus-visible:ring-3">
        <span className="block truncate font-medium">{template.name}</span>
        <span className="text-muted-foreground block text-sm">{daysLabel(template.day_count)}</span>
      </Link>
      {scope === "mine" && template.shared_with_partner ? <Badge variant="secondary">Shared</Badge> : null}
      {scope === "partner" ? <CopyTemplateButton templateId={template.id} templateName={template.name} /> : null}
    </li>
  );
}

function EmptyState({ scope }: { scope: Scope }) {
  return (
    <div className="bg-card flex flex-col items-center gap-3 rounded-xl border px-6 py-10 text-center">
      <p className="font-medium">{scope === "mine" ? "No diet templates yet" : "Nothing shared yet"}</p>
      <p className="text-muted-foreground max-w-sm text-sm">
        {scope === "mine"
          ? "A template is a reusable schedule of meals. Build one, then apply it to any week of your plan."
          : "Templates your partner shares with you show up here, and you can copy them into your library."}
      </p>
      {scope === "mine" ? (
        <Button asChild>
          <Link href="/plan/templates/new">Create your first template</Link>
        </Button>
      ) : null}
    </div>
  );
}
```

**Create `web/src/features/plan/templates-page.tsx`**

```tsx
"use client";

import { ArrowLeft, Plus } from "lucide-react";
import Link from "next/link";
import { useState } from "react";
import { Button } from "@/components/ui/button";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { usePartnerLink } from "@/features/meals/queries";
import { TemplateList } from "./template-list";

/** My diet templates, and, while I have a partner, the ones they share. */
export function TemplatesPage() {
  const link = usePartnerLink();
  const [tab, setTab] = useState<"mine" | "partner">("mine");
  const partnerActive = link.data?.status === "active";

  return (
    <div className="grid gap-4">
      <div className="flex items-center justify-between gap-3">
        <Link href="/plan" className="text-muted-foreground hover:text-foreground inline-flex items-center gap-1 text-sm">
          <ArrowLeft aria-hidden className="size-4" />
          Plan
        </Link>
        <Button asChild>
          <Link href="/plan/templates/new">
            <Plus aria-hidden />
            New template
          </Link>
        </Button>
      </div>
      {partnerActive ? (
        <Tabs value={tab} onValueChange={(value) => setTab(value === "partner" ? "partner" : "mine")}>
          <TabsList>
            <TabsTrigger value="mine">Mine</TabsTrigger>
            <TabsTrigger value="partner">Partner&apos;s</TabsTrigger>
          </TabsList>
          <TabsContent value="mine" className="mt-4">
            <TemplateList scope="mine" />
          </TabsContent>
          <TabsContent value="partner" className="mt-4">
            <TemplateList scope="partner" />
          </TabsContent>
        </Tabs>
      ) : (
        <TemplateList scope="mine" />
      )}
    </div>
  );
}
```

**Create `web/src/features/plan/new-template-form.tsx`**

```tsx
"use client";

import { useRouter } from "next/navigation";
import { useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { problemMessage } from "@/lib/api/problem";
import { parseDecimal } from "@/lib/parse-number";
import { useCreateTemplate } from "./template-queries";

/** The two things the API needs to make a template. Its meals are edited (and autosaved) on the template's own page. */
export function NewTemplateForm() {
  const router = useRouter();
  const create = useCreateTemplate();
  const [errors, setErrors] = useState<{ name?: string; days?: string }>({});

  return (
    <form
      noValidate
      className="grid max-w-md gap-4"
      onSubmit={(event) => {
        event.preventDefault();
        const data = new FormData(event.currentTarget);
        const name = String(data.get("new-template-name") ?? "").trim();
        const parsed = parseDecimal(String(data.get("new-template-days") ?? ""));
        const days = parsed.ok ? parsed.value : null;
        const next: { name?: string; days?: string } = {};
        if (!name) next.name = "Give the template a name.";
        else if (name.length > 200) next.name = "Use at most 200 characters.";
        if (days === null || !Number.isInteger(days) || days < 1 || days > 31) next.days = "Days must be a whole number from 1 to 31.";
        setErrors(next);
        if (next.name || next.days || days === null) return;
        create.mutate({ name, day_count: days }, { onSuccess: (template) => router.replace(`/plan/templates/${template.id}`), onError: (error) => toast.error(problemMessage(error)) });
      }}
    >
      <Field name="new-template-name" label="Name" autoComplete="off" error={errors.name} />
      <Field name="new-template-days" label="Number of days" inputMode="numeric" defaultValue="7" hint="7 makes a week. It cannot be changed later." error={errors.days} />
      <Button type="submit" size="lg" className="h-11 text-base md:text-sm" disabled={create.isPending}>
        {create.isPending ? "Creating…" : "Create template"}
      </Button>
    </form>
  );
}
```

**Create `web/src/features/plan/template-view.tsx`**

```tsx
import { servingsLabel } from "@/features/meals/meal-list";
import { CopyTemplateButton } from "./copy-template-button";
import { SLOTS, SLOT_LABELS } from "./plan-cache";
import type { DietTemplate } from "./template-queries";

/** A partner's template: every slot visible, nothing editable and nothing to apply. Copy it to change it or to use it. */
export function TemplateView({ template }: { template: DietTemplate }) {
  const days = Array.from({ length: template.day_count }, (_, dayIndex) =>
    template.slots.filter((slot) => slot.day_index === dayIndex).sort((a, b) => SLOTS.indexOf(a.slot) - SLOTS.indexOf(b.slot)),
  );

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <p className="text-muted-foreground text-sm">Shared by your partner. Copy it to your library to change or apply it.</p>
        <CopyTemplateButton templateId={template.id} templateName={template.name} variant="default" />
      </div>
      <div className="grid gap-4 lg:grid-cols-2">
        {days.map((slots, dayIndex) => (
          <section key={dayIndex} aria-label={`Day ${dayIndex + 1}`} className="bg-card grid content-start gap-2 rounded-xl border p-4">
            <h2 className="font-semibold">Day {dayIndex + 1}</h2>
            {slots.length === 0 ? (
              <p className="text-muted-foreground text-sm">Nothing planned.</p>
            ) : (
              <ul className="divide-y text-sm">
                {slots.map((slot) => (
                  <li key={slot.id} className="flex items-baseline gap-3 py-1.5">
                    <span className="text-muted-foreground w-24 shrink-0">{SLOT_LABELS[slot.slot]}</span>
                    <span className="min-w-0 flex-1 truncate font-medium">{slot.meal_name}</span>
                    <span className="text-muted-foreground tabular-nums">{servingsLabel(slot.portion)}</span>
                  </li>
                ))}
              </ul>
            )}
          </section>
        ))}
      </div>
    </div>
  );
}
```

**Create `web/src/app/(app)/plan/templates/page.tsx`**

```tsx
import type { Metadata } from "next";
import { PageHeader } from "@/components/page-header";
import { TemplatesPage } from "@/features/plan/templates-page";

export const metadata: Metadata = { title: "Diet templates" };

export default function Page() {
  return (
    <>
      <PageHeader title="Diet templates" />
      <TemplatesPage />
    </>
  );
}
```

**Create `web/src/app/(app)/plan/templates/new/page.tsx`**

```tsx
import type { Metadata } from "next";
import Link from "next/link";
import { ArrowLeft } from "lucide-react";
import { PageHeader } from "@/components/page-header";
import { NewTemplateForm } from "@/features/plan/new-template-form";

export const metadata: Metadata = { title: "New diet template" };

export default function Page() {
  return (
    <>
      <Link href="/plan/templates" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
        <ArrowLeft aria-hidden className="size-4" />
        Diet templates
      </Link>
      <PageHeader title="New diet template" description="Name it and choose how many days it covers. You add meals next." />
      <NewTemplateForm />
    </>
  );
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/plan/templates-page.test.tsx src/features/plan/new-template-form.test.tsx src/features/plan/template-view.test.tsx`
Expected: PASS. The row text check in `template-view.test.tsx` reads `li.textContent` (the three cells run together without spaces), so it pins both the order (breakfast, lunch, snack) and each cell's content.

- [ ] **Step 5: Lint and commit**

```bash
make lint-web && make test-web
git add web/src/features/plan "web/src/app/(app)/plan/templates"
git commit -m "feat(web): add the diet template library, the new-template form and the partner's read-only view"
```

### Task 8: The template editor

**Files:**
- Move: `web/src/features/meals/use-autosave.ts` and `use-autosave.test.ts` to `web/src/lib/`
- Modify: `web/src/features/meals/meal-editor.tsx` (import path only)
- Create: `web/src/features/plan/template-draft.ts`, `web/src/features/plan/template-editor.tsx`, `web/src/features/plan/template-detail.tsx`, `web/src/app/(app)/plan/templates/[id]/page.tsx`
- Test: `web/src/features/plan/template-draft.test.ts`, `web/src/features/plan/template-editor.test.tsx`, `web/src/features/plan/template-detail.test.tsx`

**Interfaces:**
- Consumes: `useAutosave` (moved to `@/lib/use-autosave`, same signature: `useAutosave<T>({ value, dirty, save, delayMs? }): { saving, error, flush, retry }`); `useTemplateActions`, `useDeleteTemplate`, `useTemplate`, `DietTemplate`, `TemplateSlotInput`, `UpdateDietTemplateRequest` (`template-queries`); `usePartnerLink` (`@/features/meals/queries`); `MealPicker`; `SLOTS`, `SLOT_LABELS`, `Slot` (`plan-cache`); `daysLabel` (`template-list`); `TemplateView`; `parseDecimal`; `Field`, `Input`, `Label`, `Dialog*`, `Button`, `ErrorState`, `Skeleton`, `PageHeader`, `problemMessage`.
- Produces (`template-draft.ts`):
  - `type SlotRow = { key: string; dayIndex: number; slot: Slot; mealId: string; mealName: string; portion: string }` (portion is the text typed)
  - `type TemplateDraft = { name: string; shared: boolean; rows: SlotRow[] }`, `type ValidTemplate = { name: string; shared: boolean; items: TemplateSlotInput[] }` (items sorted by day, then slot of the day, then the order added), `type TemplateErrors = { name?: string; items?: string; rows: Record<string, string> }`, `MAX_SLOTS = 500`
  - `newSlotRow(dayIndex: number, slot: Slot, meal: { id: string; name: string }): SlotRow` (portion "1", unique key)
  - `draftFromTemplate(t: DietTemplate): TemplateDraft`, `savedFromTemplate(t: DietTemplate): ValidTemplate`
  - `validateTemplate(draft: TemplateDraft, dayCount: number): { ok: true; value: ValidTemplate } | { ok: false; errors: TemplateErrors }`
  - `diffTemplate(saved, next): { patch: UpdateDietTemplateRequest | null; items: TemplateSlotInput[] | null }`, `hasTemplateChanges(saved, next): boolean`
- Produces (components):
  - `TemplateEditor({ template: DietTemplate; autosaveDelayMs?: number })`: the owner's editor. Name, the fixed length ("7 days"), "Share with my partner" (while linked or already shared), one `region` per day ("Day 1" ...) with the four slots: an empty breakfast, lunch or dinner offers "Add {slot} to day {n}"; a filled one shows the meal (link), a portion input ("Portion of {slot} on day {n}"), "Swap {slot} on day {n}" and "Remove {slot} from day {n}"; snacks may repeat, each with its own portion and remove ("Remove {meal} from day {n} snacks") plus "Add snack to day {n}". The picker is titled "Choose {slot} for day {n}" (snacks: "Add a snack to day {n}"). It autosaves like the meal editor (fields with `PATCH`, the whole slot list with `PUT .../slots`), shows the same status line and failure alert with "Try again", and has "Delete template" behind a confirmation.
  - `TemplateDetail({ id: string })`: loads a template: mine opens in `TemplateEditor` under the heading "Edit template", the partner's in `TemplateView` under its name; a `404` says "That isn't available..." with a link back to the templates.

- [ ] **Step 1: Move `useAutosave` to `src/lib` (a refactor, tests stay green)**

```bash
git mv web/src/features/meals/use-autosave.ts web/src/lib/use-autosave.ts
git mv web/src/features/meals/use-autosave.test.ts web/src/lib/use-autosave.test.ts
sed -i 's#from "./use-autosave"#from "@/lib/use-autosave"#' web/src/features/meals/meal-editor.tsx
make lint-web && make test-web
git add -A web/src
git commit -m "refactor(web): move useAutosave to src/lib so the template editor can share it"
```

Expected: lint and the whole suite pass unchanged. The moved test imports `./use-autosave`, which still resolves because the two files moved together.

- [ ] **Step 2: Write the failing tests**

**Create `web/src/features/plan/template-draft.test.ts`**

```ts
import { describe, expect, it } from "vitest";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { MAX_SLOTS, diffTemplate, draftFromTemplate, hasTemplateChanges, newSlotRow, savedFromTemplate, validateTemplate, type SlotRow, type TemplateDraft } from "./template-draft";

const oats = { id: "m1", name: "Oat bowl" };
const draft = (rows: SlotRow[] = [], over: Partial<TemplateDraft> = {}): TemplateDraft => ({ name: "Base week", shared: false, rows, ...over });
const row = (dayIndex: number, slot: SlotRow["slot"], over: Partial<SlotRow> = {}): SlotRow => ({ ...newSlotRow(dayIndex, slot, oats), ...over });

function valid(d: TemplateDraft, dayCount = 7) {
  const result = validateTemplate(d, dayCount);
  if (!result.ok) throw new Error(`expected a valid draft: ${JSON.stringify(result.errors)}`);
  return result.value;
}
function errors(d: TemplateDraft, dayCount = 7) {
  const result = validateTemplate(d, dayCount);
  if (result.ok) throw new Error("expected errors");
  return result.errors;
}

describe("a template and its draft", () => {
  const template = makeTemplate({
    day_count: 3,
    shared_with_partner: true,
    slots: [
      makeTemplateSlot({ id: "c", day_index: 1, slot: "dinner", meal_id: "m3", meal_name: "Stew", portion: 2 }),
      makeTemplateSlot({ id: "a", day_index: 0, slot: "lunch", meal_id: "m2", meal_name: "Wrap", portion: 1 }),
      makeTemplateSlot({ id: "b", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1.5 }),
    ],
  });

  it("turns into editable text, ordered as the days read", () => {
    const d = draftFromTemplate(template);
    expect(d).toMatchObject({ name: "Base week", shared: true });
    expect(d.rows.map((r) => [r.key, r.dayIndex, r.slot, r.mealName, r.portion])).toEqual([
      ["b", 0, "breakfast", "Oat bowl", "1.5"],
      ["a", 0, "lunch", "Wrap", "1"],
      ["c", 1, "dinner", "Stew", "2"],
    ]);
  });

  it("round-trips: an untouched draft validates to exactly what the server holds, so nothing is saved", () => {
    const saved = savedFromTemplate(template);
    const next = valid(draftFromTemplate(template), 3);
    expect(next).toEqual(saved);
    expect(diffTemplate(saved, next)).toEqual({ patch: null, items: null });
    expect(hasTemplateChanges(saved, next)).toBe(false);
  });
});

describe("validateTemplate", () => {
  it("accepts a good draft and trims the name", () => {
    expect(valid(draft([], { name: "  Base week  " }))).toEqual({ name: "Base week", shared: false, items: [] });
  });

  it("sorts the slots by day, then by slot of the day, and keeps snacks in the order added", () => {
    const rows = [row(1, "dinner"), row(0, "snack", { mealId: "s1" }), row(0, "breakfast"), row(0, "snack", { mealId: "s2" })];
    expect(valid(draft(rows)).items.map((i) => [i.day_index, i.slot, i.meal_id])).toEqual([
      [0, "breakfast", "m1"],
      [0, "snack", "s1"],
      [0, "snack", "s2"],
      [1, "dinner", "m1"],
    ]);
  });

  it("is not changed by the order rows were added in", () => {
    const a = row(0, "breakfast");
    const b = row(1, "dinner");
    expect(valid(draft([a, b])).items).toEqual(valid(draft([b, a])).items);
  });

  it("reads a comma as a decimal point in a portion", () => {
    expect(valid(draft([row(0, "breakfast", { portion: "1,5" })])).items[0]?.portion).toBe(1.5);
  });

  it.each([["0"], ["-1"], ["abc"], [""], ["1e2"], ["100.5"]])("refuses a portion of %j on that row only", (portion) => {
    const bad = row(0, "breakfast", { portion });
    const fine = row(0, "lunch");
    expect(Object.keys(errors(draft([fine, bad])).rows)).toEqual([bad.key]);
  });

  it("flags a second breakfast, lunch or dinner on the same day, on the second row only, but allows many snacks", () => {
    const first = row(0, "lunch");
    const second = row(0, "lunch", { mealId: "m2" });
    expect(errors(draft([first, second])).rows).toEqual({ [second.key]: "This day already has lunch." });
    expect(validateTemplate(draft([row(0, "snack"), row(0, "snack"), row(0, "snack")]), 7).ok).toBe(true);
    expect(validateTemplate(draft([row(0, "lunch"), row(1, "lunch")]), 7).ok).toBe(true);
  });

  it("flags a slot on a day the template does not have", () => {
    const outside = row(3, "breakfast");
    expect(errors(draft([outside]), 3).rows).toEqual({ [outside.key]: "This slot is outside the template's days." });
  });

  it("asks for a name of at most 200 characters, and at most 500 slots", () => {
    expect(errors(draft([], { name: "  " })).name).toBe("Give the template a name.");
    expect(errors(draft([], { name: "x".repeat(201) })).name).toBe("Use at most 200 characters.");
    const many = Array.from({ length: MAX_SLOTS + 1 }, (_, i) => row(i % 7, "snack"));
    expect(errors(draft(many)).items).toBe("A template can have at most 500 slots.");
  });
});

describe("diffTemplate", () => {
  const saved = valid(draft([row(0, "breakfast", { key: "a" })]));

  it("sends only the fields that changed, with the API's field name for sharing", () => {
    expect(diffTemplate(saved, { ...saved, name: "Cut" })).toEqual({ patch: { name: "Cut" }, items: null });
    expect(diffTemplate(saved, { ...saved, shared: true })).toEqual({ patch: { shared_with_partner: true }, items: null });
  });

  it("sends the whole slot list when any slot changes, and never a patch for it", () => {
    const changed = { ...saved, items: [{ day_index: 0, slot: "breakfast" as const, meal_id: "m1", portion: 2 }] };
    expect(diffTemplate(saved, changed)).toEqual({ patch: null, items: changed.items });
    expect(diffTemplate(saved, { ...saved, items: [] })).toEqual({ patch: null, items: [] });
  });
});
```

**Create `web/src/features/plan/template-editor.test.tsx`**

```tsx
// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeSummary, partnership } from "@/test/fixtures";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateEditor } from "./template-editor";

const replace = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ replace: (url: string) => replace(url), push: vi.fn() }) }));
afterEach(() => replace.mockReset());

const startTemplate = () =>
  makeTemplate({ day_count: 2, slots: [makeTemplateSlot({ id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 2 })] });
const catalogue = () => json({ items: [makeSummary({ id: "m2", name: "Pasta bowl" }), makeSummary()], next_cursor: null });

function setup(routes: Parameters<typeof fakeApi>[0] = {}, { delay = 10, template = startTemplate() } = {}) {
  const fake = fakeApi({
    "GET /partner": () => problem(404, "partner_not_linked"),
    "GET /meals": catalogue,
    "PUT /diet-templates/:id/slots": () => json(makeTemplate()),
    ...routes,
  });
  const view = renderWithClient(<TemplateEditor template={template} autosaveDelayMs={delay} />);
  return { fake, ...view };
}
const putSlots = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PUT", "/diet-templates/t1/slots");
const patchTemplate = (fake: ReturnType<typeof fakeApi>) => fake.callsTo("PATCH", "/diet-templates/t1");
const pickInDialog = async (title: string, meal: RegExp) => {
  const dialog = await screen.findByRole("dialog", { name: title });
  await userEvent.click(await within(dialog).findByRole("button", { name: meal }));
};
const nothingElseHappens = () => new Promise((resolve) => setTimeout(resolve, 120));

describe("TemplateEditor", () => {
  it("shows the template as saved: its name, its fixed length and every day's slots", () => {
    setup();
    expect(screen.getByLabelText("Name", { exact: true })).toHaveValue("Base week");
    expect(screen.getByText("2 days")).toBeInTheDocument();
    const day1 = within(screen.getByRole("region", { name: "Day 1" }));
    expect(day1.getByRole("link", { name: "Oat bowl" })).toHaveAttribute("href", "/meals/m1");
    expect(day1.getByLabelText("Portion of breakfast on day 1")).toHaveValue("2");
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByRole("button", { name: "Add breakfast to day 2" })).toBeInTheDocument();
    expect(screen.getByText("All changes saved")).toBeInTheDocument();
  });

  it("adds a meal to an empty slot and saves the whole slot list in day order", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Add lunch to day 2" }));
    await pickInDialog("Choose lunch for day 2", /Pasta bowl/);

    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({
      items: [
        { day_index: 0, slot: "breakfast", meal_id: "m1", portion: 2 },
        { day_index: 1, slot: "lunch", meal_id: "m2", portion: 1 },
      ],
    });
    expect(within(screen.getByRole("region", { name: "Day 2" })).getByRole("link", { name: "Pasta bowl" })).toBeInTheDocument();
  });

  it("swaps a meal and keeps its portion", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Swap breakfast on day 1" }));
    await pickInDialog("Choose breakfast for day 1", /Pasta bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [{ day_index: 0, slot: "breakfast", meal_id: "m2", portion: 2 }] });
  });

  it("removes a meal", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Remove breakfast from day 1" }));
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [] });
  });

  it("takes a portion typed with a comma", async () => {
    const { fake } = setup();
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "1,5" } });
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    expect(putSlots(fake)[0]?.body).toEqual({ items: [{ day_index: 0, slot: "breakfast", meal_id: "m1", portion: 1.5 }] });
  });

  it("flags a portion it cannot use, sends nothing, and keeps what was typed", async () => {
    const { fake } = setup();
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "0" } });
    expect(await screen.findByText("The portion must be more than 0 and at most 100.")).toBeInTheDocument();
    expect(screen.getByText("Fix the highlighted fields to save.")).toBeInTheDocument();
    await nothingElseHappens();
    expect(putSlots(fake)).toHaveLength(0);
    expect(screen.getByLabelText("Portion of breakfast on day 1")).toHaveValue("0");
  });

  it("lets a day hold several snacks, each with its own portion", async () => {
    const { fake } = setup();
    await userEvent.click(screen.getByRole("button", { name: "Add snack to day 1" }));
    await pickInDialog("Add a snack to day 1", /Pasta bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
    await userEvent.click(screen.getByRole("button", { name: "Add snack to day 1" }));
    await pickInDialog("Add a snack to day 1", /Oat bowl/);
    await waitFor(() => expect(putSlots(fake)).toHaveLength(2));
    expect(putSlots(fake)[1]?.body).toEqual({
      items: [
        { day_index: 0, slot: "breakfast", meal_id: "m1", portion: 2 },
        { day_index: 0, slot: "snack", meal_id: "m2", portion: 1 },
        { day_index: 0, slot: "snack", meal_id: "m1", portion: 1 },
      ],
    });
    expect(screen.getByLabelText("Portion of snack on day 1 (Pasta bowl)")).toBeInTheDocument();
    expect(screen.getByLabelText("Portion of snack on day 1 (Oat bowl)")).toBeInTheDocument();
  });

  it("saves a rename, sending only the name", async () => {
    const { fake } = setup({ "PATCH /diet-templates/:id": () => json(makeTemplate({ name: "Cut" })) });
    fireEvent.change(screen.getByLabelText("Name", { exact: true }), { target: { value: "Cut" } });
    await waitFor(() => expect(patchTemplate(fake)).toHaveLength(1));
    expect(patchTemplate(fake)[0]?.body).toEqual({ name: "Cut" });
    expect(putSlots(fake)).toHaveLength(0);
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
  });

  it("explains a refusal from the server, keeps the draft, and does not retry by itself", async () => {
    const { fake } = setup({ "PUT /diet-templates/:id/slots": () => problem(409, "duplicate_slot") });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(await screen.findByText("That day already has a meal for that slot.")).toBeInTheDocument();
    expect(screen.getByLabelText("Portion of breakfast on day 1")).toHaveValue("3");
    await nothingElseHappens();
    expect(putSlots(fake)).toHaveLength(1);
  });

  it("offers a retry after a failed save and saves the same draft again", async () => {
    let fail = true;
    const { fake } = setup({ "PUT /diet-templates/:id/slots": () => (fail ? problem(500, "internal_error") : json(makeTemplate())) });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("All changes saved")).toBeInTheDocument();
    expect(putSlots(fake)).toHaveLength(2);
  });

  it("saves a pending edit when the editor is closed before the pause is over", async () => {
    const { fake, unmount } = setup({}, { delay: 60_000 });
    fireEvent.change(screen.getByLabelText("Portion of breakfast on day 1"), { target: { value: "3" } });
    expect(putSlots(fake)).toHaveLength(0);
    unmount();
    await waitFor(() => expect(putSlots(fake)).toHaveLength(1));
  });

  it("offers sharing only while linked with a partner, and saves the choice", async () => {
    const { fake } = setup({ "GET /partner": () => json(partnership()), "PATCH /diet-templates/:id": () => json(makeTemplate({ shared_with_partner: true })) });
    await userEvent.click(await screen.findByLabelText("Share with my partner"));
    await waitFor(() => expect(patchTemplate(fake)).toHaveLength(1));
    expect(patchTemplate(fake)[0]?.body).toEqual({ shared_with_partner: true });
  });

  it("hides the sharing choice when there is no partner", async () => {
    const { queryClient } = setup();
    await waitFor(() => expect(queryClient.getQueryState(["partner"])?.status).toBe("success"));
    expect(screen.queryByLabelText("Share with my partner")).not.toBeInTheDocument();
  });

  it("deletes the template after a confirmation, saying that planned meals stay, and returns to the library", async () => {
    const { fake } = setup({ "DELETE /diet-templates/:id": () => noContent() });
    await userEvent.click(screen.getByRole("button", { name: "Delete template" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this template?" });
    expect(within(dialog).getByText(/Meals already in your plan stay where they are/)).toBeInTheDocument();
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete template" }));
    await waitFor(() => expect(replace).toHaveBeenCalledWith("/plan/templates"));
    expect(fake.callsTo("DELETE", "/diet-templates/t1")).toHaveLength(1);
  });

  it("stays put and says why when the delete fails", async () => {
    setup({ "DELETE /diet-templates/:id": () => problem(500, "internal_error") });
    await userEvent.click(screen.getByRole("button", { name: "Delete template" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this template?" });
    await userEvent.click(within(dialog).getByRole("button", { name: "Delete template" }));
    expect(await within(dialog).findByText("Something went wrong. Try again.")).toBeInTheDocument();
    expect(replace).not.toHaveBeenCalled();
  });
});
```

**Create `web/src/features/plan/template-detail.test.tsx`**

```tsx
// @vitest-environment jsdom
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeTemplate, makeTemplateSlot } from "@/test/plan-fixtures";
import { renderWithClient } from "@/test/render";
import { TemplateDetail } from "./template-detail";

const push = vi.fn();
vi.mock("next/navigation", () => ({ useRouter: () => ({ push: (url: string) => push(url), replace: vi.fn() }) }));
afterEach(() => push.mockReset());

describe("TemplateDetail", () => {
  it("opens my own template in the editor", async () => {
    fakeApi({ "GET /diet-templates/:id": () => json(makeTemplate({ slots: [makeTemplateSlot()] })), "GET /partner": () => problem(404, "partner_not_linked") });
    renderWithClient(<TemplateDetail id="t1" />);
    expect(await screen.findByLabelText("Name", { exact: true })).toHaveValue("Base week");
    expect(screen.getByRole("heading", { name: "Edit template" })).toBeInTheDocument();
  });

  it("shows the partner's template read-only, with a copy button and no editor", async () => {
    const shared = makeTemplate({ id: "p1", name: "Bulk", is_owner: false, day_count: 1, slots: [makeTemplateSlot({ meal_name: "Stew" })] });
    const fake = fakeApi({
      "GET /diet-templates/:id": () => json(shared),
      "POST /diet-templates/:id/copy": () => json(makeTemplate({ id: "copy1", name: "Bulk" }), 201),
    });
    renderWithClient(<TemplateDetail id="p1" />);
    expect(await screen.findByRole("heading", { name: "Bulk" })).toBeInTheDocument();
    expect(screen.getByText("Stew")).toBeInTheDocument();
    expect(screen.queryByLabelText("Name", { exact: true })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Delete template" })).not.toBeInTheDocument();

    await userEvent.click(screen.getByRole("button", { name: /Copy to my library/ }));
    await waitFor(() => expect(push).toHaveBeenCalledWith("/plan/templates/copy1"));
    expect(fake.callsTo("POST", "/diet-templates/p1/copy")).toHaveLength(1);
  });

  it("says a template is not available, with a way back, when it is gone or no longer shared", async () => {
    fakeApi({ "GET /diet-templates/:id": () => problem(404, "not_found") });
    renderWithClient(<TemplateDetail id="gone" />);
    expect(await screen.findByText("That isn't available. It may have been removed, or it isn't shared with you.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Diet templates/ })).toHaveAttribute("href", "/plan/templates");
    expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
  });

  it("offers a retry when the template cannot be loaded for another reason", async () => {
    let fail = true;
    fakeApi({
      "GET /diet-templates/:id": () => (fail ? problem(500, "internal_error") : json(makeTemplate())),
      "GET /partner": () => problem(404, "partner_not_linked"),
    });
    renderWithClient(<TemplateDetail id="t1" />);
    expect(await screen.findByText("Something went wrong. Try again.")).toBeInTheDocument();
    fail = false;
    await userEvent.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByLabelText("Name", { exact: true })).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `cd web && npx vitest run src/features/plan/template-draft.test.ts src/features/plan/template-editor.test.tsx src/features/plan/template-detail.test.tsx`
Expected: FAIL: `Failed to resolve import "./template-draft"`, `"./template-editor"`, `"./template-detail"`.

- [ ] **Step 4: Write the implementation**

**Create `web/src/features/plan/template-draft.ts`**

```ts
import { parseDecimal } from "@/lib/parse-number";
import { SLOTS, SLOT_LABELS, type Slot } from "./plan-cache";
import type { DietTemplate, TemplateSlotInput, UpdateDietTemplateRequest } from "./template-queries";

/** A slot of the template as it is being edited: the portion is the text typed. */
export type SlotRow = { key: string; dayIndex: number; slot: Slot; mealId: string; mealName: string; portion: string };
export type TemplateDraft = { name: string; shared: boolean; rows: SlotRow[] };
/** A draft that passed validation, in the shape the API takes. `items` are ordered by day, slot of the day, then the order added. */
export type ValidTemplate = { name: string; shared: boolean; items: TemplateSlotInput[] };
export type TemplateErrors = { name?: string; items?: string; rows: Record<string, string> };
export const MAX_SLOTS = 500;

let rowCounter = 0;

/** A fresh slot for a meal just picked: a portion of 1, so it is valid the moment it appears. */
export function newSlotRow(dayIndex: number, slot: Slot, meal: { id: string; name: string }): SlotRow {
  rowCounter += 1;
  return { key: `new-${rowCounter}`, dayIndex, slot, mealId: meal.id, mealName: meal.name, portion: "1" };
}

/** Day, then slot of the day, then the order added (`sort` is stable): the same content always gives the same list. */
function ordered<T extends { day_index: number; slot: Slot }>(items: T[]): T[] {
  return [...items].sort((a, b) => a.day_index - b.day_index || SLOTS.indexOf(a.slot) - SLOTS.indexOf(b.slot));
}

export function draftFromTemplate(template: DietTemplate): TemplateDraft {
  return {
    name: template.name,
    shared: template.shared_with_partner,
    rows: ordered(template.slots.map((s) => ({ day_index: s.day_index, slot: s.slot, row: s }))).map(({ row }) => ({
      key: row.id,
      dayIndex: row.day_index,
      slot: row.slot,
      mealId: row.meal_id,
      mealName: row.meal_name,
      portion: String(row.portion),
    })),
  };
}

/** What the server holds, in the same shape validation produces, so the two can be compared. */
export function savedFromTemplate(template: DietTemplate): ValidTemplate {
  return {
    name: template.name,
    shared: template.shared_with_partner,
    items: ordered(template.slots.map((s) => ({ day_index: s.day_index, slot: s.slot, meal_id: s.meal_id, portion: s.portion }))),
  };
}

export function validateTemplate(draft: TemplateDraft, dayCount: number): { ok: true; value: ValidTemplate } | { ok: false; errors: TemplateErrors } {
  const errors: TemplateErrors = { rows: {} };

  const name = draft.name.trim();
  if (!name) errors.name = "Give the template a name.";
  else if (name.length > 200) errors.name = "Use at most 200 characters.";

  if (draft.rows.length > MAX_SLOTS) errors.items = `A template can have at most ${MAX_SLOTS} slots.`;

  const taken = new Set<string>();
  const items: TemplateSlotInput[] = [];
  for (const row of draft.rows) {
    const portion = parseDecimal(row.portion);
    const value = portion.ok ? portion.value : null;
    if (value === null) errors.rows[row.key] = "Enter a portion.";
    else if (value <= 0 || value > 100) errors.rows[row.key] = "The portion must be more than 0 and at most 100.";
    else if (row.dayIndex < 0 || row.dayIndex >= dayCount) errors.rows[row.key] = "This slot is outside the template's days.";
    else if (row.slot !== "snack" && taken.has(`${row.dayIndex}:${row.slot}`)) errors.rows[row.key] = `This day already has ${SLOT_LABELS[row.slot].toLowerCase()}.`;
    else items.push({ day_index: row.dayIndex, slot: row.slot, meal_id: row.mealId, portion: value });
    if (row.slot !== "snack") taken.add(`${row.dayIndex}:${row.slot}`);
  }

  if (errors.name || errors.items || Object.keys(errors.rows).length > 0) return { ok: false, errors };
  return { ok: true, value: { name, shared: draft.shared, items: ordered(items) } };
}

/** The writes needed to bring the server from `saved` to `next`: a patch of changed fields, and the whole slot list if any slot changed. */
export function diffTemplate(saved: ValidTemplate, next: ValidTemplate): { patch: UpdateDietTemplateRequest | null; items: TemplateSlotInput[] | null } {
  const patch: UpdateDietTemplateRequest = {};
  if (next.name !== saved.name) patch.name = next.name;
  if (next.shared !== saved.shared) patch.shared_with_partner = next.shared;
  // Both sides build items as { day_index, slot, meal_id, portion } and order them the same way, so this comparison is stable.
  const sameItems = JSON.stringify(next.items) === JSON.stringify(saved.items);
  return { patch: Object.keys(patch).length > 0 ? patch : null, items: sameItems ? null : next.items };
}

export function hasTemplateChanges(saved: ValidTemplate, next: ValidTemplate): boolean {
  const { patch, items } = diffTemplate(saved, next);
  return patch !== null || items !== null;
}
```

**Create `web/src/features/plan/template-editor.tsx`**

```tsx
"use client";

import { Plus, Trash2, X } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";
import { Field } from "@/components/field";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { usePartnerLink, type MealSummary } from "@/features/meals/queries";
import { problemMessage } from "@/lib/api/problem";
import { useAutosave } from "@/lib/use-autosave";
import { MealPicker } from "./meal-picker";
import { SLOTS, SLOT_LABELS, type Slot } from "./plan-cache";
import { daysLabel } from "./template-list";
import { diffTemplate, draftFromTemplate, hasTemplateChanges, newSlotRow, savedFromTemplate, validateTemplate, type SlotRow, type TemplateDraft, type TemplateErrors, type ValidTemplate } from "./template-draft";
import { useDeleteTemplate, useTemplateActions, type DietTemplate } from "./template-queries";

type Picker = { dayIndex: number; slot: Slot; replaceKey: string | null };

/**
 * The owner's template editor. It autosaves like the meal editor: fields with `PATCH`, the whole slot list with `PUT`.
 * The draft is copied from `template` once, so a background refetch never overwrites what is being typed.
 */
export function TemplateEditor({ template, autosaveDelayMs }: { template: DietTemplate; autosaveDelayMs?: number }) {
  const router = useRouter();
  const partner = usePartnerLink();
  const actions = useTemplateActions(template.id);
  const deleteTemplate = useDeleteTemplate();

  const [draft, setDraft] = useState<TemplateDraft>(() => draftFromTemplate(template));
  const [saved, setSaved] = useState<ValidTemplate>(() => savedFromTemplate(template));
  const savedRef = useRef(saved);
  const commit = useCallback((next: ValidTemplate) => {
    savedRef.current = next;
    setSaved(next);
  }, []);
  const [picker, setPicker] = useState<Picker | null>(null);
  const [confirmingDelete, setConfirmingDelete] = useState(false);

  const validation = useMemo(() => validateTemplate(draft, template.day_count), [draft, template.day_count]);
  const value = validation.ok ? validation.value : null;
  const dirty = value !== null && hasTemplateChanges(saved, value);
  const errors = validation.ok ? null : validation.errors;

  // One save writes what changed: the fields first, then the slot list. Each step is committed on its own, so a failure in the
  // second leaves the first counted as saved and only the rest is retried.
  const save = useCallback(
    async (next: ValidTemplate) => {
      const { patch, items } = diffTemplate(savedRef.current, next);
      if (patch) {
        await actions.patch(patch);
        commit({ ...savedRef.current, name: next.name, shared: next.shared });
      }
      if (items) {
        await actions.replaceSlots(items);
        commit({ ...savedRef.current, items: next.items });
      }
    },
    [actions, commit],
  );
  const autosave = useAutosave({ value, dirty, save, delayMs: autosaveDelayMs });

  const unsaved = dirty || autosave.saving;
  useEffect(() => {
    if (!unsaved) return;
    const warn = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", warn);
    return () => window.removeEventListener("beforeunload", warn);
  }, [unsaved]);

  const chooseMeal = (meal: MealSummary) => {
    if (!picker) return;
    const { dayIndex, slot, replaceKey } = picker;
    setDraft((d) => ({
      ...d,
      rows: replaceKey ? d.rows.map((row) => (row.key === replaceKey ? { ...row, mealId: meal.id, mealName: meal.name } : row)) : [...d.rows, newSlotRow(dayIndex, slot, meal)],
    }));
  };
  const setPortion = (key: string, portion: string) => setDraft((d) => ({ ...d, rows: d.rows.map((row) => (row.key === key ? { ...row, portion } : row)) }));
  const removeRow = (key: string) => setDraft((d) => ({ ...d, rows: d.rows.filter((row) => row.key !== key) }));

  const canShare = partner.data?.status === "active" || draft.shared;
  const status = autosave.saving ? "Saving…" : errors ? "Fix the highlighted fields to save." : dirty ? "Unsaved changes" : "All changes saved";

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <p role="status" className="text-muted-foreground text-sm">
          {status}
        </p>
        {autosave.error !== null && dirty ? (
          // Only while there is still something unsaved: putting the draft back to what the server holds clears the failure.
          <div role="alert" className="bg-destructive/10 text-destructive flex flex-wrap items-center gap-3 rounded-lg px-3 py-2 text-sm">
            <span>{problemMessage(autosave.error)}</span>
            <Button type="button" variant="outline" size="sm" onClick={() => void autosave.retry()}>
              Try again
            </Button>
          </div>
        ) : null}
      </div>

      <section aria-label="Template details" className="grid max-w-xl gap-4">
        <Field name="template-name" label="Name" autoComplete="off" value={draft.name} onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))} error={errors?.name} />
        <p className="text-muted-foreground text-sm">
          {daysLabel(template.day_count)}
          <span className="sr-only"> (cannot be changed after creation)</span>
        </p>
        {canShare ? (
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" className="accent-primary size-4" checked={draft.shared} onChange={(e) => setDraft((d) => ({ ...d, shared: e.target.checked }))} />
            Share with my partner
          </label>
        ) : null}
        {errors?.items ? <p className="text-destructive text-sm">{errors.items}</p> : null}
      </section>

      <div className="grid gap-4 lg:grid-cols-2">
        {Array.from({ length: template.day_count }, (_, dayIndex) => (
          <DaySection
            key={dayIndex}
            dayIndex={dayIndex}
            rows={draft.rows.filter((row) => row.dayIndex === dayIndex)}
            errors={errors}
            onAdd={(slot) => setPicker({ dayIndex, slot, replaceKey: null })}
            onSwap={(row) => setPicker({ dayIndex, slot: row.slot, replaceKey: row.key })}
            onPortion={setPortion}
            onRemove={removeRow}
          />
        ))}
      </div>

      <div>
        <Button
          type="button"
          variant="destructive"
          onClick={() => {
            deleteTemplate.reset();
            setConfirmingDelete(true);
          }}
        >
          <Trash2 aria-hidden />
          Delete template
        </Button>
      </div>

      <MealPicker
        open={picker !== null}
        onOpenChange={(open) => {
          if (!open) setPicker(null);
        }}
        title={picker ? (picker.slot === "snack" ? `Add a snack to day ${picker.dayIndex + 1}` : `Choose ${SLOT_LABELS[picker.slot].toLowerCase()} for day ${picker.dayIndex + 1}`) : "Choose a meal"}
        onPick={chooseMeal}
      />

      <Dialog open={confirmingDelete} onOpenChange={setConfirmingDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Delete this template?</DialogTitle>
            <DialogDescription>“{template.name}” will be removed from your library. Meals already in your plan stay where they are.</DialogDescription>
          </DialogHeader>
          {deleteTemplate.error ? (
            <p role="alert" className="text-destructive text-sm">
              {problemMessage(deleteTemplate.error)}
            </p>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setConfirmingDelete(false)}>
              Keep it
            </Button>
            <Button
              type="button"
              variant="destructive"
              disabled={deleteTemplate.isPending}
              onClick={() =>
                deleteTemplate.mutate(template.id, {
                  onSuccess: () => {
                    toast.success("Template deleted.");
                    router.replace("/plan/templates");
                  },
                })
              }
            >
              {deleteTemplate.isPending ? "Deleting…" : "Delete template"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

type DayProps = {
  dayIndex: number;
  rows: SlotRow[];
  errors: TemplateErrors | null;
  onAdd: (slot: Slot) => void;
  onSwap: (row: SlotRow) => void;
  onPortion: (key: string, portion: string) => void;
  onRemove: (key: string) => void;
};

function DaySection({ dayIndex, rows, errors, onAdd, onSwap, onPortion, onRemove }: DayProps) {
  const n = dayIndex + 1;
  return (
    <section aria-label={`Day ${n}`} className="bg-card grid content-start gap-2 rounded-xl border p-4">
      <h2 className="font-semibold">Day {n}</h2>
      {SLOTS.map((slot) => {
        const label = SLOT_LABELS[slot];
        const lower = label.toLowerCase();
        const slotRows = rows.filter((row) => row.slot === slot);
        return (
          <div key={slot} className="grid gap-1.5 sm:grid-cols-[6rem_1fr] sm:items-start">
            <h3 className="text-sm font-medium sm:pt-2">{label}</h3>
            <div className="grid gap-2">
              {slotRows.map((row) => {
                const error = errors?.rows[row.key];
                const portionLabel = slot === "snack" ? `Portion of snack on day ${n} (${row.mealName})` : `Portion of ${lower} on day ${n}`;
                return (
                  <div key={row.key} className="grid gap-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <Link href={`/meals/${row.mealId}`} className="min-w-0 flex-1 truncate text-sm font-medium hover:underline">
                        {row.mealName}
                      </Link>
                      <Input
                        aria-label={portionLabel}
                        aria-invalid={error ? true : undefined}
                        aria-describedby={error ? `${row.key}-error` : undefined}
                        inputMode="decimal"
                        className="h-9 w-20 text-base md:text-sm"
                        value={row.portion}
                        onChange={(e) => onPortion(row.key, e.target.value)}
                      />
                      {slot === "snack" ? null : (
                        <Button type="button" variant="ghost" size="sm" aria-label={`Swap ${lower} on day ${n}`} onClick={() => onSwap(row)}>
                          Swap
                        </Button>
                      )}
                      <Button
                        type="button"
                        variant="ghost"
                        size="icon-sm"
                        aria-label={slot === "snack" ? `Remove ${row.mealName} from day ${n} snacks` : `Remove ${lower} from day ${n}`}
                        onClick={() => onRemove(row.key)}
                      >
                        <X aria-hidden />
                      </Button>
                    </div>
                    {error ? (
                      <p id={`${row.key}-error`} className="text-destructive text-sm">
                        {error}
                      </p>
                    ) : null}
                  </div>
                );
              })}
              {slot === "snack" || slotRows.length === 0 ? (
                <div>
                  <Button type="button" variant="outline" size="sm" aria-label={slot === "snack" ? `Add snack to day ${n}` : `Add ${lower} to day ${n}`} onClick={() => onAdd(slot)}>
                    <Plus aria-hidden />
                    {slot === "snack" ? "Add snack" : "Add meal"}
                  </Button>
                </div>
              ) : null}
            </div>
          </div>
        );
      })}
    </section>
  );
}
```

**Create `web/src/features/plan/template-detail.tsx`**

```tsx
"use client";

import { ArrowLeft } from "lucide-react";
import Link from "next/link";
import { ErrorState } from "@/components/error-state";
import { PageHeader } from "@/components/page-header";
import { Skeleton } from "@/components/ui/skeleton";
import { ApiError, problemMessage } from "@/lib/api/problem";
import { TemplateEditor } from "./template-editor";
import { useTemplate } from "./template-queries";
import { TemplateView } from "./template-view";

function BackToTemplates() {
  return (
    <Link href="/plan/templates" className="text-muted-foreground hover:text-foreground mb-4 inline-flex items-center gap-1 text-sm">
      <ArrowLeft aria-hidden className="size-4" />
      Diet templates
    </Link>
  );
}

/** Loads one template: mine opens in the editor, the partner's in a read-only view. */
export function TemplateDetail({ id }: { id: string }) {
  const query = useTemplate(id);

  // A background refetch that fails must not replace an editor that already has the template on screen.
  if (query.data === undefined) {
    return (
      <>
        <BackToTemplates />
        {query.isPending ? (
          <div role="status" className="grid gap-3" aria-label="Loading template">
            <Skeleton className="h-9 w-48" />
            <Skeleton className="h-40 rounded-xl" />
          </div>
        ) : (
          <ErrorState message={problemMessage(query.error)} onRetry={query.error instanceof ApiError && query.error.status === 404 ? undefined : () => void query.refetch()} />
        )}
      </>
    );
  }

  const template = query.data;
  return (
    <>
      <BackToTemplates />
      <PageHeader title={template.is_owner ? "Edit template" : template.name} />
      {template.is_owner ? <TemplateEditor key={template.id} template={template} /> : <TemplateView template={template} />}
    </>
  );
}
```

**Create `web/src/app/(app)/plan/templates/[id]/page.tsx`**

```tsx
import type { Metadata } from "next";
import { TemplateDetail } from "@/features/plan/template-detail";

export const metadata: Metadata = { title: "Diet template" };

export default async function Page({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params;
  return <TemplateDetail id={id} />;
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd web && npx vitest run src/features/plan src/lib/use-autosave.test.ts src/features/meals`
Expected: PASS. If a label lookup finds two elements, check the accessible names: `Portion of snack on day 1 (Pasta bowl)` carries the meal name so two snacks stay distinguishable, and a non-snack slot's portion label has no meal name.

- [ ] **Step 6: Lint, build and commit**

```bash
make lint-web && make test-web
cd web && npx next build && cd ..
git add web/src/features/plan "web/src/app/(app)/plan/templates"
git commit -m "feat(web): add the diet template editor with autosave, and the template detail page"
```

Expected: `next build` lists `/plan`, `/plan/templates`, `/plan/templates/new`, `/plan/templates/[id]` and `/today`.

---

### Task 9: Plan and Today in Playwright

**Files:**
- Create: `web/e2e/plan.spec.ts`

**Interfaces:**
- Consumes: `e2e/support.ts` (`api`, `newAccount`, `register`) from the Meals plan; the running Compose stack (`make e2e-web`); `PATCH /api/me` to set targets (the Profile screen for them comes with the Shopping and Profile plan).
- Produces: two flows: build a template, apply it (with the replace confirmation), then swap and resize on Today against the targets; and a partner's template read-only, copied, and applied.

- [ ] **Step 1: Write the flows**

**Create `web/e2e/plan.spec.ts`**

```ts
import { expect, test, type Page } from "@playwright/test";
import { api, newAccount, register } from "./support";

type Created = { id: string };

// Per 100 g. Two meals of one ingredient give round calorie numbers: 100 g is 380 kcal, 250 g is 950 kcal.
const OATS = { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } };

/** The browser's own local date, the way the app derives "today". */
async function localToday(page: Page): Promise<string> {
  return page.evaluate(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  });
}

async function seedMeals(page: Page, shared = false) {
  const oats = await api<Created>(page, "POST", "/ingredients", OATS);
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1, shared_with_partner: shared });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  const feast = await api<Created>(page, "POST", "/meals", { name: "Feast", servings: 1, shared_with_partner: shared });
  await api(page, "PUT", `/meals/${feast.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 250, unit: "g" }] });
  return { porridge, feast };
}

test("build a template, apply it, then swap and resize a meal on Today against the targets", async ({ page }) => {
  await register(page, newAccount());
  await seedMeals(page);
  await api(page, "PATCH", "/me", { target_kcal: 2000, target_protein_g: 100, target_carbs_g: 250, target_fat_g: 70 });
  const today = await localToday(page);

  // A one-day template with porridge for breakfast.
  await page.goto("/plan/templates/new");
  await page.getByLabel("Name").fill("Base day");
  await page.getByLabel("Number of days").fill("1");
  await page.getByRole("button", { name: "Create template" }).click();
  await expect(page.getByRole("heading", { name: "Edit template" })).toBeVisible();

  const saved = page.waitForResponse((r) => r.url().includes("/slots") && r.request().method() === "PUT" && r.ok());
  await page.getByRole("button", { name: "Add breakfast to day 1" }).click();
  await page.getByRole("dialog").getByRole("button", { name: /Porridge/ }).click();
  await saved;
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();

  // Apply it from today. Applying again asks before replacing.
  await page.goto("/plan");
  await page.getByRole("button", { name: "Apply template" }).click();
  let dialog = page.getByRole("dialog", { name: "Apply a diet template" });
  await expect(dialog.getByRole("option", { name: "Base day (1 day)" })).toBeAttached();
  await dialog.getByLabel("Start date").fill(today);
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Week totals" }).getByText("380 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Apply template" }).click();
  dialog = page.getByRole("dialog", { name: "Apply a diet template" });
  await dialog.getByLabel("Start date").fill(today);
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await expect(dialog.getByText(/Some of those days already have meals\. Replace them\?/)).toBeVisible();
  await dialog.getByRole("button", { name: "Keep my plan" }).click();
  await expect(dialog.getByText(/Replace them\?/)).toBeHidden();
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await dialog.getByRole("button", { name: "Replace them" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();

  // Today: the rings against the targets, then a swap and a portion change.
  await page.goto("/today");
  const rings = page.getByRole("region", { name: "Today's totals" });
  await expect(rings.getByText("380 kcal")).toBeVisible();
  await expect(rings.getByText("19% of 2,000 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Swap breakfast meal" }).click();
  await page.getByRole("dialog").getByRole("button", { name: /Feast/ }).click();
  await expect(page.getByRole("link", { name: "Feast" })).toBeVisible();
  await expect(rings.getByText("950 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Increase portion of Feast" }).click();
  await expect(page.getByText("1.5 servings")).toBeVisible();
  await expect(rings.getByText("1,425 kcal")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("link", { name: "Feast" })).toBeVisible();
  await expect(page.getByText("1.5 servings")).toBeVisible();
});

test("a partner's template can be read and copied but not applied or edited, and the copy can be applied", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });

    const { porridge } = await seedMeals(page);
    const template = await api<Created>(page, "POST", "/diet-templates", { name: "Shared week", day_count: 2, shared_with_partner: true });
    await api(page, "PUT", `/diet-templates/${template.id}/slots`, {
      items: [
        { day_index: 0, slot: "breakfast", meal_id: porridge.id, portion: 1 },
        { day_index: 1, slot: "dinner", meal_id: porridge.id, portion: 2 },
      ],
    });

    // Before copying, the partner has no template of their own to apply.
    await partnerPage.goto("/plan");
    await partnerPage.getByRole("button", { name: "Apply template" }).click();
    await expect(partnerPage.getByText("You have no diet templates yet.")).toBeVisible();
    await partnerPage.keyboard.press("Escape");

    await partnerPage.goto("/plan/templates");
    await partnerPage.getByRole("tab", { name: "Partner's" }).click();
    await partnerPage.getByRole("link", { name: /Shared week/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Shared week" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveCount(0);
    await expect(partnerPage.getByRole("region", { name: "Day 2" }).getByText("Porridge")).toBeVisible();
    await expect(partnerPage.getByRole("button", { name: /apply/i })).toHaveCount(0);

    await partnerPage.getByRole("button", { name: /Copy to my library/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Edit template" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveValue("Shared week");
    await expect(partnerPage.getByText("2 days")).toBeVisible();
    await expect(partnerPage.getByRole("region", { name: "Day 1" }).getByRole("link", { name: "Porridge" })).toBeVisible();

    // The copy is theirs: it can be applied to their own plan.
    const today = await localToday(partnerPage);
    await partnerPage.goto("/plan");
    await partnerPage.getByRole("button", { name: "Apply template" }).click();
    const dialog = partnerPage.getByRole("dialog", { name: "Apply a diet template" });
    await expect(dialog.getByRole("option", { name: "Shared week (2 days)" })).toBeAttached();
    await dialog.getByLabel("Start date").fill(today);
    await dialog.getByRole("button", { name: "Apply template" }).click();
    await expect(dialog).toBeHidden();
    await expect(partnerPage.getByRole("link", { name: "Porridge" }).first()).toBeVisible();
  } finally {
    await partnerContext.close();
  }
});
```

- [ ] **Step 2: Run the flows**

Run: `make e2e-web` (needs Docker; builds the stack under its own project name and ports, runs every Playwright spec, tears the stack down).
Expected: PASS for the auth, meals and plan specs. On a failure, open the trace: `cd web && npx playwright show-trace test-results/<test>/trace.zip`. If Docker is not available locally, say so in the PR: CI's `e2e` job (`web.yml`) runs these flows.

Notes for a red run: each partner flow accepts one invite (the API limits `POST /partner/accept` to 10 per hour per client address, and all E2E users share one, so a stack that is torn down after each run never hits it); the plan flow's day cards are found through their long-date headings, so it never hardcodes today's date; and the ring text `19% of 2,000 kcal` is `Math.round(380 / 2000 * 100)`.

- [ ] **Step 3: Commit**

```bash
make lint-web
git add web/e2e/plan.spec.ts
git commit -m "test(web): cover templates, applying with a replace confirmation, Today's swaps and partner templates end to end"
```

---

### Task 10: Docs

**Files:**
- Modify: `web/CLAUDE.md`, `docs/superpowers/specs/2026-09-25-web-app-design.md`

**Interfaces:**
- Consumes: the behaviour built in Tasks 1 to 9.
- Produces: `web/CLAUDE.md` describes the plan area and its gotchas; the web spec records the date, snack and optimistic-write decisions.

- [ ] **Step 1: Update `web/CLAUDE.md`**

After the bullet that begins ``- `src/features/meals/`:``, add:

```markdown
- `src/features/plan/`: `queries.ts` (`usePlan`, optimistic `useSetPlanEntry` / `useClearPlanSlot`, `useApplyTemplate`), `plan-cache.ts` (pure optimistic edits of a cached plan, `SLOTS`), `today-view`, `plan-view` (week + totals), `apply-template-dialog`, `day-meals` / `slot-row` / `meal-picker` (shared by Today and Plan), `template-queries.ts`, `template-list` / `templates-page` / `new-template-form` / `template-view` / `template-detail` / `template-editor` (autosave), `template-draft.ts` (pure validate + diff). `src/components/macro-rings.tsx`: the four rings against targets.
- `src/lib/dates.ts`: local calendar dates as `YYYY-MM-DD`, weeks start Monday. `src/lib/use-today.ts`, `src/lib/use-autosave.ts` (shared by both editors), `src/lib/use-load-all-pages.ts`.
```

After the last bullet under Gotchas, add:

```markdown
- Dates are the person's local calendar day, `YYYY-MM-DD`. Never `toISOString()` (that is the UTC day: wrong for part of every day in most zones); use `src/lib/dates.ts`. Move by days with `addDays` (`setDate`), never by adding 24 h, so a daylight-saving change cannot skip or repeat a date. Tests build dates with `new Date(y, m, d, h)` (local), and fake only `Date` (`vi.useFakeTimers({ toFake: ["Date"] })`) so Testing Library's polling keeps working.
- Plan writes are optimistic on the entries only. Day and week totals come from `GET /plan` and refetch when the write settles; the rings and totals dim (`aria-busy`) meanwhile. The API addresses a snack only by date and slot (`PUT` adds, `DELETE` clears every snack of the day): the plan offers "Add snack" and a confirmed "Clear snacks" and never edits one snack. Template slots are replaced as a whole, so a template's snacks are editable.
- A partner's template is read-only and cannot be applied (`404`): copy it first. `POST /diet-templates/{id}/apply` answers `409 plan_conflict` unless `overwrite: true`; the dialog asks first.
- Targets (`target_kcal`, ...) come back on `GET /plan`; they are set with `PATCH /me` (the Profile screen for them arrives in the Shopping and Profile plan). A `null` or non-positive target is "No target set", never a ring at 0%.
- `useMutation({ onError })` on the hook, not `mutate(vars, { onError })`: the latter only fires for the latest call, so rapid taps would lose errors.
```

- [ ] **Step 2: Record the decisions in the web spec**

In `docs/superpowers/specs/2026-09-25-web-app-design.md`, replace this line:

```markdown
**Autosave.** The meal editor has no Save button: it saves 700 ms after the last edit (fields with `PATCH`, the ingredient list with `PUT`) and shows the API's nutrition from the last save. The browser does not compute nutrition: a loaded meal carries only each ingredient's name and category, not its per-100 g data, and the API stays the one place the math lives.
```

with:

```markdown
**Autosave.** The meal editor and the template editor have no Save button: they save 700 ms after the last edit (fields with `PATCH`, the ingredient or slot list with `PUT`). The meal editor shows the API's nutrition from the last save. The browser does not compute nutrition: a loaded meal carries only each ingredient's name and category, not its per-100 g data, and the API stays the one place the math lives.

**Plan and Today.** Dates are the person's local calendar day (`YYYY-MM-DD`), never UTC, and weeks start on Monday. Swaps, portion changes and slot clears are optimistic on the entries only: totals come from `GET /plan` and refetch when the write settles. The API addresses a snack only by date and slot (`PUT` adds, `DELETE` clears every snack of the day), so the plan offers "Add snack" and a confirmed "Clear snacks" and never edits one snack; template slots are replaced as a whole, so a template's snacks are editable. A partner's template is read-only; it is copied to change it or to apply it. Applying over days that already have meals asks before replacing (`409 plan_conflict`, then `overwrite: true`).
```

and replace this line:

```markdown
Until the profile targets exist (Shopping and Profile plan), the Meals screens show the four as tiles; the rings arrive with Today.
```

with:

```markdown
The Meals screens show the four as tiles (a meal has no target); the rings are on Today, against the targets `GET /plan` returns. The targets are set with `PATCH /me`; their Profile screen arrives with the Shopping and Profile plan.
```

- [ ] **Step 3: Verify and commit**

Run: `make check`
Expected: PASS (API lint, backend vet/test/lint, generated-code drift, web typecheck, lint, tests).

```bash
git add web/CLAUDE.md docs/superpowers/specs/2026-09-25-web-app-design.md
git commit -m "docs: describe the plan area and record the date, snack and optimistic-write decisions in the web spec"
```

Finish with superpowers:finishing-a-development-branch. The PR title is "Web app Plan and Today: rings, week calendar, diet templates"; its body links this plan and notes that the `e2e` CI job runs the two new flows.

---

## After this plan

Plan 4 (Shopping and Profile) is written after this one merges, as the spec's §10 says. Two things to carry into it: the Profile screen must let the person set the four targets (`PATCH /me`), which Today and the Plan totals already read; and generating a shopping list takes a plan date range, for which `usePlan`'s ranges and `weekDates` are the natural picker. A known API gap worth an issue if it bites: per-snack edit in the plan needs an entry-addressed endpoint (spec first, then `make generate`).

