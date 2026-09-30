import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/lib/api/client";
import { ApiError } from "@/lib/api/problem";
import type { components } from "@/lib/api/schema.gen";
import type { IngredientCategory } from "@/lib/ingredient-categories";
import type { ShoppingItem, ShoppingList } from "./items";
import { OPTIMISTIC_PREFIX, optimisticItem, restoreItem, withAddedItem, withChecked, withItem, withoutItem } from "./list-cache";

/** Per-resource keys. The list keys prefix nothing else, so refreshing lists never touches an open list. */
export const shoppingKeys = {
  mine: ["shopping", "mine"] as const,
  partner: ["shopping", "partner"] as const,
  detail: (id: string) => ["shopping", "detail", id] as const,
};

export function useShoppingLists(scope: "mine" | "partner") {
  return useInfiniteQuery({
    queryKey: scope === "mine" ? shoppingKeys.mine : shoppingKeys.partner,
    initialPageParam: undefined as string | undefined,
    queryFn: ({ pageParam }) => {
      const query = { cursor: pageParam, limit: 20 };
      return scope === "mine" ? unwrap(api.GET("/shopping-lists", { params: { query } })) : unwrap(api.GET("/partner/shopping-lists", { params: { query } }));
    },
    getNextPageParam: (last) => last.next_cursor ?? undefined,
  });
}

export function useShoppingList(id: string) {
  return useQuery({ queryKey: shoppingKeys.detail(id), queryFn: () => unwrap(api.GET("/shopping-lists/{id}", { params: { path: { id } } })) });
}

export function useCreateShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name: string; shared_with_partner?: boolean }) => unwrap(api.POST("/shopping-lists", { body })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(list.id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

/** Builds a list from the plan for `from` to `to` (at most 92 days), or regenerates `listId` from that range. */
export function useGenerateShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (vars: { from: string; to: string; name?: string; listId?: string }) =>
      unwrap(api.POST("/shopping-lists/generate", { body: { from: vars.from, to: vars.to, ...(vars.name ? { name: vars.name } : {}), ...(vars.listId ? { list_id: vars.listId } : {}) } })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(list.id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

export function useUpdateShoppingList(id: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (body: { name?: string; shared_with_partner?: boolean }) => unwrap(api.PATCH("/shopping-lists/{id}", { params: { path: { id } }, body })),
    onSuccess: (list) => {
      queryClient.setQueryData(shoppingKeys.detail(id), list);
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.partner });
    },
  });
}

export function useDeleteShoppingList() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string): Promise<void> => {
      await unwrap(api.DELETE("/shopping-lists/{id}", { params: { path: { id } } }));
    },
    onSuccess: (_data, id) => {
      queryClient.removeQueries({ queryKey: shoppingKeys.detail(id) });
      void queryClient.invalidateQueries({ queryKey: shoppingKeys.mine });
    },
  });
}

const patchItem = (listId: string, itemId: string, body: components["schemas"]["UpdateShoppingItemRequest"]) =>
  unwrap(api.PATCH("/shopping-lists/{id}/items/{item_id}", { params: { path: { id: listId, item_id: itemId } }, body }));

/** Check-off: applied at once, last write wins on the server (no version), put back if the server refuses. */
export function useCheckItem(listId: string, userId: string | null, onFailure?: (error: Error) => void) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  const mutationKey = ["shopping", "check", listId] as const;
  // With another tap still in flight, this answer is about a state the screen has already moved past: leave the screen to the last tap.
  const otherTapInFlight = () => queryClient.isMutating({ mutationKey }) > 1;
  return useMutation<ShoppingItem, Error, { itemId: string; checked: boolean }, { previous: ShoppingItem | undefined }>({
    mutationKey,
    mutationFn: ({ itemId, checked }) => patchItem(listId, itemId, { checked }),
    onMutate: async ({ itemId, checked }) => {
      await queryClient.cancelQueries({ queryKey: key });
      const list = queryClient.getQueryData<ShoppingList>(key);
      if (list) queryClient.setQueryData(key, withChecked(list, itemId, checked, userId));
      return { previous: list?.items.find((item) => item.id === itemId) };
    },
    onError: (error, _vars, context) => {
      const previous = context?.previous;
      if (previous && !otherTapInFlight()) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? restoreItem(list, previous) : list));
      onFailure?.(error);
    },
    onSuccess: (item) => {
      if (!otherTapInFlight()) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, item) : list));
    },
  });
}

let temporaryIds = 0;

/** Quick-add: the item shows at once under a temporary id, then is swapped for the server's. */
export function useAddItem(listId: string, onFailure?: (error: Error) => void) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  return useMutation<ShoppingItem, Error, { name: string; ingredientId?: string; category?: IngredientCategory }, { temporaryId: string }>({
    mutationFn: ({ name, ingredientId }) =>
      unwrap(api.POST("/shopping-lists/{id}/items", { params: { path: { id: listId } }, body: ingredientId ? { name, ingredient_id: ingredientId } : { name } })),
    onMutate: async (input) => {
      await queryClient.cancelQueries({ queryKey: key });
      temporaryIds += 1;
      const temporaryId = `${OPTIMISTIC_PREFIX}${temporaryIds}`;
      const list = queryClient.getQueryData<ShoppingList>(key);
      if (list) {
        const position = list.items.reduce((highest, item) => Math.max(highest, item.position), -1) + 1;
        queryClient.setQueryData(key, withAddedItem(list, optimisticItem(listId, input, position, temporaryId)));
      }
      return { temporaryId };
    },
    onError: (error, _vars, context) => {
      if (context) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withoutItem(list, context.temporaryId) : list));
      onFailure?.(error);
    },
    onSuccess: (item, _vars, context) =>
      queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(context ? withoutItem(list, context.temporaryId) : list, item) : list)),
  });
}

const isItem = (value: unknown): value is ShoppingItem =>
  typeof value === "object" && value !== null && typeof (value as ShoppingItem).id === "string" && typeof (value as ShoppingItem).version === "number";

/** The item a stale edit was refused for, from the `version_conflict` problem's `current` member; null for any other failure. */
export function conflictingItem(error: Error): ShoppingItem | null {
  return error instanceof ApiError && error.code === "version_conflict" && isItem(error.body.current) ? error.body.current : null;
}

type ItemChanges = { name?: string; quantity?: number | null; unit?: "g" | "ml" | "piece" | null; category?: IngredientCategory };

/**
 * Edits name, quantity, unit or category. The API needs the item's version and refuses a stale one with the current item,
 * which is stored here so the form can show the other person's version before the error reaches the caller.
 */
export function useEditItem(listId: string) {
  const queryClient = useQueryClient();
  const key = shoppingKeys.detail(listId);
  return useMutation<ShoppingItem, Error, { item: ShoppingItem; changes: ItemChanges }>({
    mutationFn: ({ item, changes }) => patchItem(listId, item.id, { version: item.version, ...changes }),
    onSuccess: (item) => queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, item) : list)),
    onError: (error) => {
      const current = conflictingItem(error);
      if (current) queryClient.setQueryData<ShoppingList>(key, (list) => (list ? withItem(list, current) : list));
    },
  });
}

export function useDeleteItem(listId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (itemId: string): Promise<void> => {
      await unwrap(api.DELETE("/shopping-lists/{id}/items/{item_id}", { params: { path: { id: listId, item_id: itemId } } }));
    },
    onSuccess: (_data, itemId) => queryClient.setQueryData<ShoppingList>(shoppingKeys.detail(listId), (list) => (list ? withoutItem(list, itemId) : list)),
  });
}
