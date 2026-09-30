// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeMeal, makeSummary, partnership } from "@/test/fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import { mealKeys, useCopyMeal, useCreateMeal, useDeleteMeal, useMealActions, useMeals, usePartnerLink } from "./queries";

describe("useMeals", () => {
  it("walks the pages of the caller's own meals with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /meals": (req) =>
        req.search.get("cursor") === "c1"
          ? json({ items: [makeSummary({ id: "m2", name: "Pasta" })], next_cursor: null })
          : json({ items: [makeSummary()], next_cursor: "c1" }),
    });
    const { result } = renderHook(() => useMeals("mine"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(result.current.hasNextPage).toBe(true);

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.data?.pages.flatMap((p) => p.items).map((m) => m.name)).toEqual(["Oat bowl", "Pasta"]));
    expect(result.current.hasNextPage).toBe(false);
    expect(fake.calls[1]?.search.get("cursor")).toBe("c1");
  });

  it("reads the partner's shared meals from the partner route, never mixing in the caller's own", async () => {
    const fake = fakeApi({ "GET /partner/meals": () => json({ items: [makeSummary({ name: "Soup" })], next_cursor: null }) });
    const { result } = renderHook(() => useMeals("partner"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(fake.calls.map((c) => c.path)).toEqual(["/partner/meals"]);
  });
});

describe("usePartnerLink", () => {
  it("returns the partnership when there is one", async () => {
    fakeApi({ "GET /partner": () => json(partnership()) });
    const { result } = renderHook(() => usePartnerLink(), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data).toEqual(partnership()));
  });

  it("returns null, not an error, when there is no partner", async () => {
    fakeApi({ "GET /partner": () => problem(404, "partner_not_linked") });
    const { result } = renderHook(() => usePartnerLink(), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data).toBeNull());
    expect(result.current.isError).toBe(false);
  });

  it("does not hide a real failure", async () => {
    fakeApi({ "GET /partner": () => problem(500, "internal_error") });
    const { result } = renderHook(() => usePartnerLink(), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.isError).toBe(true));
  });
});

describe("meal mutations", () => {
  it("create seeds the detail cache and marks the list stale", async () => {
    const meal = makeMeal({ id: "new1", name: "Curry" });
    fakeApi({ "POST /meals": () => json(meal, 201) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(mealKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useCreateMeal(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ name: "Curry", servings: 2 });
    });
    expect(queryClient.getQueryData(mealKeys.detail("new1"))).toEqual(meal);
    expect(queryClient.getQueryState(mealKeys.mine)?.isInvalidated).toBe(true);
  });

  it("copy returns the caller's own copy and seeds its detail cache", async () => {
    const copy = makeMeal({ id: "copy1", name: "Soup", is_owner: true });
    const fake = fakeApi({ "POST /meals/:id/copy": () => json(copy, 201) });
    const queryClient = testQueryClient();
    const { result } = renderHook(() => useCopyMeal(), { wrapper: clientWrapper(queryClient) });

    let created: unknown;
    await act(async () => {
      created = await result.current.mutateAsync("partner-meal");
    });
    expect(created).toEqual(copy);
    expect(fake.calls[0]?.path).toBe("/meals/partner-meal/copy");
    expect(queryClient.getQueryData(mealKeys.detail("copy1"))).toEqual(copy);
  });

  it("delete drops the detail cache and marks the list stale", async () => {
    fakeApi({ "DELETE /meals/:id": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(mealKeys.detail("m1"), makeMeal());
    queryClient.setQueryData(mealKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useDeleteMeal(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("m1");
    });
    expect(queryClient.getQueryData(mealKeys.detail("m1"))).toBeUndefined();
    expect(queryClient.getQueryState(mealKeys.mine)?.isInvalidated).toBe(true);
  });

  it("delete surfaces meal_in_use as an ApiError with its code", async () => {
    fakeApi({ "DELETE /meals/:id": () => problem(409, "meal_in_use") });
    const { result } = renderHook(() => useDeleteMeal(), { wrapper: clientWrapper() });
    let caught: unknown;
    await act(async () => {
      caught = await result.current.mutateAsync("m1").catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 409, code: "meal_in_use" });
  });
});

describe("useMealActions", () => {
  it("patches and replaces ingredients, keeping the detail cache on the latest meal and the list stale", async () => {
    const first = makeMeal({ name: "Porridge" });
    const second = makeMeal({ name: "Porridge", servings: 3 });
    const fake = fakeApi({
      "PATCH /meals/:id": () => json(first),
      "PUT /meals/:id/ingredients": () => json(second),
    });
    const queryClient = testQueryClient();
    queryClient.setQueryData(mealKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useMealActions("m1"), { wrapper: clientWrapper(queryClient) });

    expect(await result.current.patch({ name: "Porridge" })).toEqual(first);
    expect(fake.callsTo("PATCH", "/meals/m1")[0]?.body).toEqual({ name: "Porridge" });
    expect(queryClient.getQueryData(mealKeys.detail("m1"))).toEqual(first);

    const items = [{ ingredient_id: "i1", quantity: 80, unit: "g" as const }];
    expect(await result.current.replace(items)).toEqual(second);
    expect(fake.callsTo("PUT", "/meals/m1/ingredients")[0]?.body).toEqual({ items });
    expect(queryClient.getQueryData(mealKeys.detail("m1"))).toEqual(second);
    expect(queryClient.getQueryState(mealKeys.mine)?.isInvalidated).toBe(true);
  });

  it("keeps the same actions object while the meal id is unchanged", () => {
    const wrapper = clientWrapper();
    const { result, rerender } = renderHook(({ id }) => useMealActions(id), { wrapper, initialProps: { id: "m1" } });
    const before = result.current;
    rerender({ id: "m1" });
    expect(result.current).toBe(before);
  });
});
