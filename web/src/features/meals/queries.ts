import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { api, unwrap } from "@/lib/api/client";
import { ApiError } from "@/lib/api/problem";
import type { components } from "@/lib/api/schema.gen";

type Schemas = components["schemas"];
export type Meal = Schemas["Meal"];
export type MealSummary = Schemas["MealSummary"];
export type Partnership = Schemas["Partnership"];
export type CreateMealRequest = Schemas["CreateMealRequest"];
export type UpdateMealRequest = Schemas["UpdateMealRequest"];
export type MealIngredientInput = Schemas["MealIngredientInput"];

/** Per-resource keys. The list keys are prefixes of nothing else, so refreshing lists never touches an open meal. */
export const mealKeys = {
  mine: ["meals", "mine"] as const,
  partner: ["meals", "partner"] as const,
  detail: (id: string) => ["meals", "detail", id] as const,
};
export const PARTNER_KEY = ["partner"] as const;

export function useMeals(scope: "mine" | "partner") {
  return useInfiniteQuery({
    queryKey: scope === "mine" ? mealKeys.mine : mealKeys.partner,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const query = { cursor: pageParam, limit: 20 };
      return scope === "mine" ? unwrap(api.GET("/meals", { params: { query } })) : unwrap(api.GET("/partner/meals", { params: { query } }));
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** The active or pending partnership, or `null` when there is none (which is an answer, not a failure). */
export function usePartnerLink() {
  return useQuery({
    queryKey: PARTNER_KEY,
    queryFn: async (): Promise<Partnership | null> => {
      try {
        return await unwrap(api.GET("/partner"));
      } catch (error) {
        if (error instanceof ApiError && error.code === "partner_not_linked") return null;
        throw error;
      }
    },
  });
}

export function useMeal(id: string) {
  return useQuery({ queryKey: mealKeys.detail(id), queryFn: () => unwrap(api.GET("/meals/{id}", { params: { path: { id } } })) });
}

const createMeal = (body: CreateMealRequest) => unwrap(api.POST("/meals", { body }));
const copyMeal = (id: string) => unwrap(api.POST("/meals/{id}/copy", { params: { path: { id } } }));
const removeMeal = async (id: string): Promise<void> => {
  await unwrap(api.DELETE("/meals/{id}", { params: { path: { id } } }));
};
const updateMeal = (id: string, body: UpdateMealRequest) => unwrap(api.PATCH("/meals/{id}", { params: { path: { id } }, body }));
const replaceMealIngredients = (id: string, items: MealIngredientInput[]) =>
  unwrap(api.PUT("/meals/{id}/ingredients", { params: { path: { id } }, body: { items } }));

export function useCreateMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: createMeal,
    onSuccess: (meal) => {
      queryClient.setQueryData(mealKeys.detail(meal.id), meal);
      void queryClient.invalidateQueries({ queryKey: mealKeys.mine });
    },
  });
}

export function useCopyMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: copyMeal,
    onSuccess: (meal) => {
      queryClient.setQueryData(mealKeys.detail(meal.id), meal);
      void queryClient.invalidateQueries({ queryKey: mealKeys.mine });
    },
  });
}

export function useDeleteMeal() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: removeMeal,
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: mealKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: mealKeys.mine });
    },
  });
}

/** The two writes the editor's autosave makes. Each keeps the open meal's cache on the server's latest answer. */
export function useMealActions(id: string) {
  const queryClient = useQueryClient();
  return useMemo(() => {
    const done = (meal: Meal) => {
      queryClient.setQueryData(mealKeys.detail(id), meal);
      void queryClient.invalidateQueries({ queryKey: mealKeys.mine });
      return meal;
    };
    return {
      patch: async (body: UpdateMealRequest) => done(await updateMeal(id, body)),
      replace: async (items: MealIngredientInput[]) => done(await replaceMealIngredients(id, items)),
    };
  }, [queryClient, id]);
}
