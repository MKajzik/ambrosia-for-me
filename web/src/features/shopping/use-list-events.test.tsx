// @vitest-environment jsdom
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { FakeEventSource } from "@/test/fake-event-source";
import { fakeApi, json, problem } from "@/test/fake-api";
import { makeItem, makeList } from "@/test/shopping-fixtures";
import { clientWrapper } from "@/test/render";
import { useShoppingList } from "./queries";
import { useListEvents } from "./use-list-events";

const L = "l1";
const held = () => makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 3 }), makeItem({ id: "i2", name: "Eggs", version: 1, position: 1 })] });
const settle = () => new Promise((resolve) => setTimeout(resolve, 60));

async function setup(onDeleted = vi.fn()) {
  FakeEventSource.install();
  let server = held();
  const fake = fakeApi({ "GET /shopping-lists/:id": () => json(server) });
  const view = renderHook(
    () => {
      useListEvents(L, onDeleted);
      return useShoppingList(L);
    },
    { wrapper: clientWrapper() },
  );
  await waitFor(() => expect(view.result.current.data).toBeDefined());
  const source = FakeEventSource.last!;
  const fetches = () => fake.callsTo("GET", "/shopping-lists/l1").length;
  return { ...view, source, fetches, serve: (next: ReturnType<typeof held>) => void (server = next), onDeleted };
}

describe("useListEvents", () => {
  it("opens the list's stream on the same origin and closes it when the screen goes away", async () => {
    const { source, unmount } = await setup();
    expect(FakeEventSource.instances).toHaveLength(1);
    expect(source.url).toBe("/api/shopping-lists/l1/events");
    expect(source.readyState).not.toBe(2);
    unmount();
    expect(source.readyState).toBe(2);
  });

  it("refetches whenever the stream opens, first time or after a reconnect", async () => {
    const { source, fetches } = await setup();
    expect(fetches()).toBe(1);
    source.open();
    await waitFor(() => expect(fetches()).toBe(2));
    source.open();
    await waitFor(() => expect(fetches()).toBe(3));
  });

  it("ignores an event about a change the cache already holds, so a person's own check-off does not refetch or flicker", async () => {
    const { source, fetches } = await setup();
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 3 });
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 2 });
    await settle();
    expect(fetches()).toBe(1);
  });

  it("refetches for a newer version of an item, and shows what the partner did", async () => {
    const { source, fetches, serve, result } = await setup();
    serve(makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 4, checked: true, checked_by: "u2" }), makeItem({ id: "i2", name: "Eggs", version: 1, position: 1 })] }));
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i1", version: 4 });
    await waitFor(() => expect(result.current.data?.items[0]).toMatchObject({ checked: true, checked_by: "u2", version: 4 }));
    expect(fetches()).toBe(2);
  });

  it("refetches for an item it has never seen, which is how the partner's additions arrive", async () => {
    const { source, fetches, serve, result } = await setup();
    serve(makeList({ items: [...held().items, makeItem({ id: "i3", name: "Bread", position: 2 })] }));
    source.message("item_changed", { type: "item_changed", list_id: L, item_id: "i3", version: 1 });
    await waitFor(() => expect(result.current.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs", "Bread"]));
    expect(fetches()).toBe(2);
  });

  it("removes a deleted item without asking the server, unless the cache holds a newer version than the event", async () => {
    const { source, fetches, result } = await setup();
    source.message("item_deleted", { type: "item_deleted", list_id: L, item_id: "i1", version: 2 });
    await settle();
    expect(result.current.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs"]);

    source.message("item_deleted", { type: "item_deleted", list_id: L, item_id: "i1", version: 3 });
    await waitFor(() => expect(result.current.data?.items.map((i) => i.name)).toEqual(["Eggs"]));
    expect(fetches()).toBe(1);
  });

  it("refetches when the list is renamed or regenerated", async () => {
    const { source, fetches } = await setup();
    source.message("list_changed", { type: "list_changed", list_id: L });
    await waitFor(() => expect(fetches()).toBe(2));
  });

  it("closes the stream and tells the screen when the list is deleted", async () => {
    const onDeleted = vi.fn();
    const { source } = await setup(onDeleted);
    source.message("list_deleted", { type: "list_deleted", list_id: L });
    expect(onDeleted).toHaveBeenCalledTimes(1);
    expect(source.readyState).toBe(2);
  });

  it("refetches when the connection is lost for good (access gone), but not while it is only reconnecting", async () => {
    const { source, fetches } = await setup();
    source.fail(false);
    await settle();
    expect(fetches()).toBe(1);

    source.fail(true);
    await waitFor(() => expect(fetches()).toBe(2));
  });

  it("ignores another list's events and anything malformed", async () => {
    const { source, fetches, onDeleted } = await setup();
    source.message("list_deleted", { type: "list_deleted", list_id: "other" });
    source.message("item_changed", { type: "item_changed", list_id: "other", item_id: "i1", version: 9 });
    source.raw("item_changed", "not json");
    source.raw("list_deleted", "{}");
    await settle();
    expect(fetches()).toBe(1);
    expect(onDeleted).not.toHaveBeenCalled();
  });

  it("calls the latest onDeleted, not the one from when the stream was opened", async () => {
    const first = vi.fn();
    const second = vi.fn();
    FakeEventSource.install();
    fakeApi({ "GET /shopping-lists/:id": () => json(held()) });
    const { rerender } = renderHook(({ cb }) => useListEvents(L, cb), { wrapper: clientWrapper(), initialProps: { cb: first } });
    rerender({ cb: second });
    FakeEventSource.last!.message("list_deleted", { type: "list_deleted", list_id: L });
    expect(first).not.toHaveBeenCalled();
    expect(second).toHaveBeenCalledTimes(1);
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("does not open a second stream when the screen re-renders", async () => {
    const { rerender } = await setup();
    rerender();
    rerender();
    expect(FakeEventSource.instances).toHaveLength(1);
  });

  it("still works when a refetch after the stream opens fails", async () => {
    FakeEventSource.install();
    let fail = false;
    const fake = fakeApi({ "GET /shopping-lists/:id": () => (fail ? problem(500, "internal_error") : json(held())) });
    const { result } = renderHook(
      () => {
        useListEvents(L, vi.fn());
        return useShoppingList(L);
      },
      { wrapper: clientWrapper() },
    );
    await waitFor(() => expect(result.current.data).toBeDefined());
    fail = true;
    FakeEventSource.last!.open();
    await waitFor(() => expect(fake.callsTo("GET", "/shopping-lists/l1")).toHaveLength(2));
    expect(result.current.data?.items).toHaveLength(2);
  });
});
