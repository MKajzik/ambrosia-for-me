import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { ingredientKeys, type Ingredient } from "@/components/ingredient-search/use-ingredient-search";
import { planKeys } from "@/features/plan/queries";
import { api, unwrap } from "@/lib/api/client";
import type { components } from "@/lib/api/schema.gen";
import type { IngredientCategory } from "@/lib/ingredient-categories";

export type UpdateIngredientRequest = components["schemas"]["UpdateIngredientRequest"];

/** Browse alphabetically (with a cursor), or search by name (best matches, no cursor), optionally within one category. */
export function useIngredientBrowse(text: string, category: IngredientCategory | "", limit = 50) {
  const q = text.trim().slice(0, 100);
  return useInfiniteQuery({
    queryKey: [...ingredientKeys.all, "browse", { q, category, limit }] as const,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => unwrap(api.GET("/ingredients", { params: { query: { q: q || undefined, category: category || undefined, cursor: pageParam, limit } } })),
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

/** An ingredient's nutrients feed every meal that uses it, and so the plan: refresh them all. */
export function useUpdateIngredient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body }: { id: string; body: UpdateIngredientRequest }): Promise<Ingredient> => unwrap(api.PATCH("/ingredients/{id}", { params: { path: { id } }, body })),
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ingredientKeys.all });
      void queryClient.invalidateQueries({ queryKey: ["meals"] });
      void queryClient.invalidateQueries({ queryKey: planKeys.all });
    },
  });
}

export function useDeleteIngredient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await unwrap(api.DELETE("/ingredients/{id}", { params: { path: { id } } }));
    },
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ingredientKeys.all }),
  });
}
