import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api/client";
import type { components } from "@/lib/api/schema.gen";
import type { IngredientCategory } from "@/lib/ingredient-categories";

export type Ingredient = components["schemas"]["Ingredient"];
export type CreateIngredientRequest = components["schemas"]["CreateIngredientRequest"];

export const ingredientKeys = {
  all: ["ingredients"] as const,
  search: (q: string, category: IngredientCategory | "") => ["ingredients", { q, category }] as const,
};

/**
 * Empty text browses alphabetically; text searches by name. The API allows 100 characters of `q`.
 * `enabled` lets a closed popup skip the request, so a page with a search box does not fetch the catalogue on load.
 */
export function useIngredientSearch(text: string, category: IngredientCategory | "", enabled = true) {
  const q = text.trim().slice(0, 100);
  return useQuery({
    queryKey: ingredientKeys.search(q, category),
    queryFn: () => unwrap(api.GET("/ingredients", { params: { query: { q: q || undefined, category: category || undefined, limit: 20 } } })),
    enabled,
    staleTime: 60_000,
    placeholderData: keepPreviousData,
  });
}

export function useCreateIngredient() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: CreateIngredientRequest) => unwrap(api.POST("/ingredients", { body })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ingredientKeys.all }),
  });
}
