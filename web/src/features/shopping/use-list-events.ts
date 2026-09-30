import { useQueryClient } from "@tanstack/react-query";
import { useEffect, useRef } from "react";
import type { ShoppingList } from "./items";
import { isStaleEvent, withoutItem } from "./list-cache";
import { EVENT_TYPES, parseListEvent } from "./list-events";
import { shoppingKeys } from "./queries";

const CLOSED = 2;

/**
 * Keeps an open list live. The stream says what changed, not what it changed to, so this never patches items from an event:
 * it refetches the list, except for an event about a version the cache already holds (often this person's own change) and a
 * deleted item, which simply leaves the list. The same-origin proxy attaches the bearer, and the browser reconnects by itself.
 */
export function useListEvents(listId: string, onDeleted: () => void): void {
  const queryClient = useQueryClient();
  const latestOnDeleted = useRef(onDeleted);
  useEffect(() => {
    latestOnDeleted.current = onDeleted;
  });

  useEffect(() => {
    const key = shoppingKeys.detail(listId);
    const refetch = () => void queryClient.invalidateQueries({ queryKey: key });
    const source = new EventSource(`/api/shopping-lists/${listId}/events`);

    const onMessage = (message: Event) => {
      const event = parseListEvent((message as MessageEvent<string>).data);
      if (!event || event.list_id !== listId) return;
      const list = queryClient.getQueryData<ShoppingList>(key);
      switch (event.type) {
        case "item_changed":
          if (event.item_id === undefined || event.version === undefined || !isStaleEvent(list, event.item_id, event.version)) refetch();
          break;
        case "item_deleted": {
          const held = list?.items.find((item) => item.id === event.item_id);
          if (list && held && (event.version === undefined || held.version <= event.version)) queryClient.setQueryData<ShoppingList>(key, withoutItem(list, held.id));
          break;
        }
        case "list_changed":
          refetch();
          break;
        case "list_deleted":
          source.close();
          latestOnDeleted.current();
          break;
      }
    };

    for (const type of EVENT_TYPES) source.addEventListener(type, onMessage);
    source.addEventListener("open", refetch);
    // A closed connection is a refusal (the list is gone or no longer shared): ask, and the refetch says which. A connecting one is the browser retrying.
    source.addEventListener("error", () => {
      if (source.readyState === CLOSED) refetch();
    });
    return () => source.close();
  }, [listId, queryClient]);
}
