// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeDay, makeEntry, makePlan } from "@/test/plan-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import type { PlanRange } from "./plan-cache";
import { planKeys, useApplyTemplate, useClearPlanSlot, usePlan, useSetPlanEntry } from "./queries";

const D = "2026-09-28";
const porridge = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m-old", meal_name: "Porridge" })])]);
const mealNames = (range: PlanRange | undefined) => range?.days[0]?.entries.map((e) => e.meal_name);

describe("usePlan", () => {
  it("reads one range of the plan", async () => {
    const fake = fakeApi({ "GET /plan": () => json(porridge) });
    const { result } = renderHook(() => usePlan(D, D), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.data)).toEqual(["Porridge"]));
    expect(fake.calls[0]?.search.get("from")).toBe(D);
    expect(fake.calls[0]?.search.get("to")).toBe(D);
  });
});

describe("useSetPlanEntry", () => {
  it("shows the swap at once, then settles on what the server says", async () => {
    let server = porridge;
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(server),
      "PUT /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(json(makeEntry({ meal_id: "m-new", meal_name: "Pasta", portion: 2 })));
        }),
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "m-new", mealName: "Pasta", portion: 2 }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Pasta"]));
    expect(result.current.set.isPending).toBe(true);
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)[0]?.body).toEqual({ meal_id: "m-new", portion: 2 });

    server = makePlan(D, D, [makeDay(D, [makeEntry({ meal_id: "m-new", meal_name: "Pasta", portion: 2 })])]);
    await act(async () => release());
    await waitFor(() => expect(result.current.set.isSuccess).toBe(true));
    await waitFor(() => expect(fake.callsTo("GET", "/plan")).toHaveLength(2));
    expect(mealNames(result.current.plan.data)).toEqual(["Pasta"]);
  });

  it("puts the plan back and reports why when the server refuses the swap", async () => {
    const onFailure = vi.fn();
    fakeApi({ "GET /plan": () => json(porridge), "PUT /plan/:date/:slot": () => problem(400, "invalid_meal") });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry(onFailure) }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "gone", mealName: "Ghost", portion: 1 }));
    await waitFor(() => expect(result.current.set.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    expect(onFailure).toHaveBeenCalledTimes(1);
    expect(onFailure.mock.calls[0]?.[0]).toBeInstanceOf(ApiError);
    expect(onFailure.mock.calls[0]?.[0]).toMatchObject({ code: "invalid_meal" });
  });

  it("rolls back when the network fails too", async () => {
    let fail = false;
    fakeApi({
      "GET /plan": () => json(porridge),
      "PUT /plan/:date/:slot": () => {
        fail = true;
        throw new TypeError("offline");
      },
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), set: useSetPlanEntry() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    act(() => result.current.set.mutate({ date: D, slot: "breakfast", mealId: "m2", mealName: "Pasta", portion: 1 }));
    await waitFor(() => expect(fail).toBe(true));
    await waitFor(() => expect(result.current.set.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
  });

  it("edits every cached range that holds the date, and refreshes all of them", async () => {
    const week = makePlan(D, "2026-10-04", [makeDay(D, [makeEntry({ meal_name: "Porridge" })])]);
    const fake = fakeApi({ "GET /plan": () => json(porridge), "PUT /plan/:date/:slot": () => json(makeEntry({ meal_name: "Pasta" })) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(planKeys.range(D, D), porridge);
    queryClient.setQueryData(planKeys.range(D, "2026-10-04"), week);
    const { result } = renderHook(() => useSetPlanEntry(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ date: D, slot: "breakfast", mealId: "m2", mealName: "Pasta", portion: 1 });
    });
    expect(queryClient.getQueryState(planKeys.range(D, D))?.isInvalidated).toBe(true);
    expect(queryClient.getQueryState(planKeys.range(D, "2026-10-04"))?.isInvalidated).toBe(true);
    expect(fake.callsTo("PUT", `/plan/${D}/breakfast`)).toHaveLength(1);
  });
});

describe("useClearPlanSlot", () => {
  it("removes the entry at once and asks the API to delete the slot", async () => {
    let release!: () => void;
    const fake = fakeApi({
      "GET /plan": () => json(porridge),
      "DELETE /plan/:date/:slot": () =>
        new Promise<Response>((resolve) => {
          release = () => resolve(noContent());
        }),
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));

    act(() => result.current.clear.mutate({ date: D, slot: "breakfast" }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual([]));
    expect(fake.callsTo("DELETE", `/plan/${D}/breakfast`)).toHaveLength(1);
    await act(async () => release());
    await waitFor(() => expect(result.current.clear.isSuccess).toBe(true));
  });

  it("clears every snack of the day, so the cache must too", async () => {
    const snacks = makePlan(D, D, [makeDay(D, [makeEntry({ id: "s1", slot: "snack", meal_name: "Apple" }), makeEntry({ id: "s2", slot: "snack", meal_name: "Nuts" })])]);
    let server = snacks;
    fakeApi({
      "GET /plan": () => json(server),
      "DELETE /plan/:date/:slot": () => {
        server = makePlan(D, D, [makeDay(D, [])]);
        return noContent();
      },
    });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Apple", "Nuts"]));
    act(() => result.current.clear.mutate({ date: D, slot: "snack" }));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual([]));
  });

  it("brings the entry back if the delete fails", async () => {
    fakeApi({ "GET /plan": () => json(porridge), "DELETE /plan/:date/:slot": () => problem(500, "internal_error") });
    const { result } = renderHook(() => ({ plan: usePlan(D, D), clear: useClearPlanSlot() }), { wrapper: clientWrapper() });
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
    act(() => result.current.clear.mutate({ date: D, slot: "breakfast" }));
    await waitFor(() => expect(result.current.clear.isError).toBe(true));
    await waitFor(() => expect(mealNames(result.current.plan.data)).toEqual(["Porridge"]));
  });
});

describe("useApplyTemplate", () => {
  it("sends the start date and the overwrite choice, then refreshes the plan", async () => {
    const fake = fakeApi({ "POST /diet-templates/:id/apply": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(planKeys.range(D, D), porridge);
    const { result } = renderHook(() => useApplyTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ templateId: "t1", startDate: D, overwrite: true });
    });
    expect(fake.callsTo("POST", "/diet-templates/t1/apply")[0]?.body).toEqual({ start_date: D, overwrite: true });
    expect(queryClient.getQueryState(planKeys.range(D, D))?.isInvalidated).toBe(true);
  });

  it("surfaces plan_conflict as an ApiError the dialog can act on", async () => {
    fakeApi({ "POST /diet-templates/:id/apply": () => problem(409, "plan_conflict") });
    const { result } = renderHook(() => useApplyTemplate(), { wrapper: clientWrapper() });
    let caught: unknown;
    await act(async () => {
      caught = await result.current.mutateAsync({ templateId: "t1", startDate: D, overwrite: false }).catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 409, code: "plan_conflict" });
  });
});
