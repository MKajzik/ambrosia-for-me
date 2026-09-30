export const EVENT_TYPES = ["item_changed", "item_deleted", "list_changed", "list_deleted"] as const;
export type ListEventType = (typeof EVENT_TYPES)[number];

/** One event of a shopping list's stream: what changed, never the new content. */
export type ListEvent = { type: ListEventType; list_id: string; item_id?: string; version?: number };

/** Reads an event's `data`. Anything that is not one of the API's events is `null`, so a malformed one can never act. */
export function parseListEvent(text: string): ListEvent | null {
  let value: unknown;
  try {
    value = JSON.parse(text);
  } catch {
    return null;
  }
  if (typeof value !== "object" || value === null) return null;
  const { type, list_id, item_id, version } = value as Record<string, unknown>;
  if (typeof type !== "string" || !(EVENT_TYPES as readonly string[]).includes(type) || typeof list_id !== "string") return null;
  return { type: type as ListEventType, list_id, ...(typeof item_id === "string" ? { item_id } : {}), ...(typeof version === "number" ? { version } : {}) };
}
