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
