// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeItem, makeList, makeListSummary } from "@/test/shopping-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import type { ShoppingList } from "./items";
import { shoppingKeys, useAddItem, useCheckItem, useCreateShoppingList, useDeleteItem, useDeleteShoppingList, useEditItem, useGenerateShoppingList, useShoppingList, useShoppingLists, useUpdateShoppingList } from "./queries";

const L = "l1";
const base = () => makeList({ items: [makeItem({ id: "i1", name: "Milk", version: 1 }), makeItem({ id: "i2", name: "Eggs", position: 1 })] });
const state = (l: ShoppingList | undefined) => l?.items.map((i) => `${i.name}:${i.checked ? "x" : "-"}`);

describe("useShoppingLists", () => {
  it("walks the pages of my lists with the API's cursor, and reads the partner's from the partner route", async () => {
    const fake = fakeApi({
      "GET /shopping-lists": (req) =>
        req.search.get("cursor") === "c1"
          ? json({ items: [makeListSummary({ id: "l2", name: "Party" })], next_cursor: null })
          : json({ items: [makeListSummary()], next_cursor: "c1" }),
      "GET /partner/shopping-lists": () => json({ items: [makeListSummary({ name: "Shared" })], next_cursor: null }),
    });
    const mine = renderHook(() => useShoppingLists("mine"), { wrapper: clientWrapper() });
    await waitFor(() => expect(mine.result.current.hasNextPage).toBe(true));
    await act(async () => {
      await mine.result.current.fetchNextPage();
    });
    await waitFor(() => expect(mine.result.current.data?.pages.flatMap((p) => p.items).map((l) => l.name)).toEqual(["Weekly shop", "Party"]));
    expect(fake.callsTo("GET", "/shopping-lists")[1]?.search.get("cursor")).toBe("c1");

    const partner = renderHook(() => useShoppingLists("partner"), { wrapper: clientWrapper() });
    await waitFor(() => expect(partner.result.current.data?.pages).toHaveLength(1));
    expect(fake.callsTo("GET", "/partner/shopping-lists")).toHaveLength(1);
  });
});

describe("list mutations", () => {
  it("create and generate seed the detail cache and mark my lists stale", async () => {
    const created = makeList({ id: "new1", name: "Party" });
    const generated = makeList({ id: "gen1", name: "Shopping", source_from: "2026-09-28", source_to: "2026-10-04" });
    const fake = fakeApi({ "POST /shopping-lists": () => json(created, 201), "POST /shopping-lists/generate": () => json(generated, 201) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(shoppingKeys.mine, { pages: [], pageParams: [] });
    const create = renderHook(() => useCreateShoppingList(), { wrapper: clientWrapper(queryClient) });
    const generate = renderHook(() => useGenerateShoppingList(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await create.result.current.mutateAsync({ name: "Party" });
      await generate.result.current.mutateAsync({ from: "2026-09-28", to: "2026-10-04" });
    });
    expect(fake.callsTo("POST", "/shopping-lists")[0]?.body).toEqual({ name: "Party" });
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04" });
    expect(queryClient.getQueryData(shoppingKeys.detail("new1"))).toEqual(created);
    expect(queryClient.getQueryData(shoppingKeys.detail("gen1"))).toEqual(generated);
    expect(queryClient.getQueryState(shoppingKeys.mine)?.isInvalidated).toBe(true);
  });

  it("generate sends the name and the list to regenerate only when given", async () => {
    const fake = fakeApi({ "POST /shopping-lists/generate": () => json(makeList(), 200) });
    const { result } = renderHook(() => useGenerateShoppingList(), { wrapper: clientWrapper() });
    await act(async () => {
      await result.current.mutateAsync({ from: "2026-09-28", to: "2026-10-04", name: "Week", listId: "l9" });
    });
    expect(fake.callsTo("POST", "/shopping-lists/generate")[0]?.body).toEqual({ from: "2026-09-28", to: "2026-10-04", name: "Week", list_id: "l9" });
  });

  it("update stores the answer and marks both list keys stale; delete drops the detail", async () => {
    const renamed = makeList({ name: "Renamed" });
    fakeApi({ "PATCH /shopping-lists/:id": () => json(renamed), "DELETE /shopping-lists/:id": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(shoppingKeys.mine, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.partner, { pages: [], pageParams: [] });
    queryClient.setQueryData(shoppingKeys.detail(L), base());
    const update = renderHook(() => useUpdateShoppingList(L), { wrapper: clientWrapper(queryClient) });
    const remove = renderHook(() => useDeleteShoppingList(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await update.result.current.mutateAsync({ name: "Renamed" });
    });
    expect(queryClient.getQueryData(shoppingKeys.detail(L))).toEqual(renamed);
    expect(queryClient.getQueryState(shoppingKeys.partner)?.isInvalidated).toBe(true);

    await act(async () => {
      await remove.result.current.mutateAsync(L);
    });
    expect(queryClient.getQueryData(shoppingKeys.detail(L))).toBeUndefined();
  });
});

describe("useCheckItem", () => {
  it("checks at once, then keeps the server's item with its new version", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "i1", name: "Milk", checked: true, checked_by: "u1", version: 2 })));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));

    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:x", "Eggs:-"]));
    expect(result.current.check.isPending).toBe(true);
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ checked: true });

    await act(async () => release());
    await waitFor(() => expect(result.current.check.isSuccess).toBe(true));
    expect(result.current.list.data?.items[0]).toMatchObject({ checked: true, checked_by: "u1", version: 2 });
    expect(fake.callsTo("GET", "/shopping-lists/l1")).toHaveLength(1);
  });

  it("unchecks the same way, sending checked: false and no version", async () => {
    const checked = makeList({ items: [makeItem({ id: "i1", checked: true, checked_by: "u1", version: 4 })] });
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(checked), "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", version: 5 })) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data?.items[0]?.checked).toBe(true));
    act(() => result.current.check.mutate({ itemId: "i1", checked: false }));
    await waitFor(() => expect(result.current.list.data?.items[0]).toMatchObject({ checked: false, checked_by: null }));
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ checked: false });
  });

  it("leaves the last tap in charge when an earlier answer arrives late", async () => {
    const releases: Array<(checked: boolean) => void> = [];
    fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () =>
        new Promise<Response>((resolve) => {
          releases.push((checked) => resolve(json(makeItem({ id: "i1", name: "Milk", checked, checked_by: checked ? "u1" : null, version: releases.length + 1 }))));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1") }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));

    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:x", "Eggs:-"]));
    act(() => result.current.check.mutate({ itemId: "i1", checked: false }));
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));
    await waitFor(() => expect(releases).toHaveLength(2));

    await act(async () => releases[0]?.(true));
    expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]);
    await act(async () => releases[1]?.(false));
    await waitFor(() => expect(result.current.check.isSuccess).toBe(true));
    expect(result.current.list.data?.items[0]).toMatchObject({ checked: false, checked_by: null });
  });

  it("puts the item back and reports the failure when the server refuses", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(500, "internal_error") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), check: useCheckItem(L, "u1", onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]));
    act(() => result.current.check.mutate({ itemId: "i1", checked: true }));
    await waitFor(() => expect(result.current.check.isError).toBe(true));
    expect(state(result.current.list.data)).toEqual(["Milk:-", "Eggs:-"]);
    expect(onFailure).toHaveBeenCalledTimes(1);
  });
});

describe("useAddItem", () => {
  it("shows the new item at once and swaps it for the server's", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "POST /shopping-lists/:id/items": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeItem({ id: "real1", name: "Bread", category: "other", position: 2 }), 201));
        }),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());

    act(() => result.current.add.mutate({ name: "Bread" }));
    await waitFor(() => expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs", "Bread"]));
    expect(result.current.list.data?.items[2]?.id.startsWith("optimistic:")).toBe(true);
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Bread" });

    await act(async () => release());
    await waitFor(() => expect(result.current.add.isSuccess).toBe(true));
    expect(result.current.list.data?.items.map((i) => i.id)).toEqual(["i1", "i2", "real1"]);
  });

  it("adds an ingredient by id", async () => {
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(base()), "POST /shopping-lists/:id/items": () => json(makeItem({ id: "k1", name: "Kale" }), 201) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.add.mutateAsync({ name: "Kale", ingredientId: "ing1", category: "produce" });
    });
    expect(fake.callsTo("POST", "/shopping-lists/l1/items")[0]?.body).toEqual({ name: "Kale", ingredient_id: "ing1" });
  });

  it("removes the temporary item and reports the failure when the server refuses", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "POST /shopping-lists/:id/items": () => problem(404, "not_found") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), add: useAddItem(L, onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    act(() => result.current.add.mutate({ name: "Bread" }));
    await waitFor(() => expect(result.current.add.isError).toBe(true));
    expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Milk", "Eggs"]);
    expect(onFailure).toHaveBeenCalledTimes(1);
  });
});

describe("useEditItem", () => {
  it("sends the item's version with the changes and keeps the server's answer", async () => {
    const fake = fakeApi({
      "GET /shopping-lists/:id": () => json(base()),
      "PATCH /shopping-lists/:id/items/:item": () => json(makeItem({ id: "i1", name: "Oat milk", quantity: 2, unit: "piece", version: 2 })),
    });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    const item = result.current.list.data!.items[0]!;
    await act(async () => {
      await result.current.edit.mutateAsync({ item, changes: { name: "Oat milk", quantity: 2, unit: "piece" } });
    });
    expect(fake.callsTo("PATCH", "/shopping-lists/l1/items/i1")[0]?.body).toEqual({ version: 1, name: "Oat milk", quantity: 2, unit: "piece" });
    await waitFor(() => expect(result.current.list.data?.items[0]).toMatchObject({ name: "Oat milk", version: 2 }));
  });

  it("stores the other person's version when the edit is stale, and rejects with the conflict", async () => {
    const theirs = makeItem({ id: "i1", name: "Bananas", version: 3 });
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict", { current: theirs }) });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    const item = result.current.list.data!.items[0]!;
    let caught: unknown;
    await act(async () => {
      caught = await result.current.edit.mutateAsync({ item, changes: { name: "Oat milk" } }).catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 409, code: "version_conflict" });
    await waitFor(() => expect(result.current.list.data?.items[0]).toMatchObject({ name: "Bananas", version: 3 }));
  });

  it("does not store anything for a conflict that carries no item", async () => {
    fakeApi({ "GET /shopping-lists/:id": () => json(base()), "PATCH /shopping-lists/:id/items/:item": () => problem(409, "version_conflict") });
    const { result } = renderHook(() => ({ list: useShoppingList(L), edit: useEditItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.edit.mutateAsync({ item: result.current.list.data!.items[0]!, changes: { name: "X" } }).catch(() => undefined);
    });
    expect(result.current.list.data?.items[0]?.name).toBe("Milk");
  });
});

describe("useDeleteItem", () => {
  it("removes the item once the server has deleted it", async () => {
    const fake = fakeApi({ "GET /shopping-lists/:id": () => json(base()), "DELETE /shopping-lists/:id/items/:item": () => noContent() });
    const { result } = renderHook(() => ({ list: useShoppingList(L), remove: useDeleteItem(L) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.list.data).toBeDefined());
    await act(async () => {
      await result.current.remove.mutateAsync("i1");
    });
    expect(fake.callsTo("DELETE", "/shopping-lists/l1/items/i1")).toHaveLength(1);
    await waitFor(() => expect(result.current.list.data?.items.map((i) => i.name)).toEqual(["Eggs"]));
  });
});
