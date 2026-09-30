// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { fakeApi, json, noContent, problem } from "@/test/fake-api";
import { makeTemplate, makeTemplateSummary } from "@/test/plan-fixtures";
import { clientWrapper, testQueryClient } from "@/test/render";
import { templateKeys, useCopyTemplate, useCreateTemplate, useDeleteTemplate, useTemplateActions, useTemplates } from "./template-queries";

describe("useTemplates", () => {
  it("walks the pages of my templates with the API's cursor", async () => {
    const fake = fakeApi({
      "GET /diet-templates": (req) =>
        req.search.get("cursor") === "c1"
          ? json({ items: [makeTemplateSummary({ id: "t2", name: "Cut" })], next_cursor: null })
          : json({ items: [makeTemplateSummary()], next_cursor: "c1" }),
    });
    const { result } = renderHook(() => useTemplates("mine"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(result.current.hasNextPage).toBe(true);

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.data?.pages.flatMap((p) => p.items).map((t) => t.name)).toEqual(["Base week", "Cut"]));
    expect(fake.calls[1]?.search.get("cursor")).toBe("c1");
  });

  it("reads the partner's shared templates from the partner route", async () => {
    const fake = fakeApi({ "GET /partner/diet-templates": () => json({ items: [makeTemplateSummary({ name: "Bulk" })], next_cursor: null }) });
    const { result } = renderHook(() => useTemplates("partner"), { wrapper: clientWrapper() });
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(1));
    expect(fake.calls.map((c) => c.path)).toEqual(["/partner/diet-templates"]);
  });
});

describe("template mutations", () => {
  it("create seeds the detail cache and marks the list stale", async () => {
    const created = makeTemplate({ id: "new1", name: "Cut", day_count: 3 });
    const fake = fakeApi({ "POST /diet-templates": () => json(created, 201) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useCreateTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync({ name: "Cut", day_count: 3 });
    });
    expect(fake.callsTo("POST", "/diet-templates")[0]?.body).toEqual({ name: "Cut", day_count: 3 });
    expect(queryClient.getQueryData(templateKeys.detail("new1"))).toEqual(created);
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("copy returns my own copy and seeds its detail cache", async () => {
    const copy = makeTemplate({ id: "copy1", name: "Bulk" });
    const fake = fakeApi({ "POST /diet-templates/:id/copy": () => json(copy, 201) });
    const queryClient = testQueryClient();
    const { result } = renderHook(() => useCopyTemplate(), { wrapper: clientWrapper(queryClient) });

    let created: unknown;
    await act(async () => {
      created = await result.current.mutateAsync("partner-template");
    });
    expect(created).toEqual(copy);
    expect(fake.calls[0]?.path).toBe("/diet-templates/partner-template/copy");
    expect(queryClient.getQueryData(templateKeys.detail("copy1"))).toEqual(copy);
  });

  it("delete drops the detail cache and marks the list stale", async () => {
    fakeApi({ "DELETE /diet-templates/:id": () => noContent() });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.detail("t1"), makeTemplate());
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useDeleteTemplate(), { wrapper: clientWrapper(queryClient) });

    await act(async () => {
      await result.current.mutateAsync("t1");
    });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toBeUndefined();
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("surfaces a 404 as an ApiError", async () => {
    fakeApi({ "POST /diet-templates/:id/copy": () => problem(404, "not_found") });
    const { result } = renderHook(() => useCopyTemplate(), { wrapper: clientWrapper() });
    let caught: unknown;
    await act(async () => {
      caught = await result.current.mutateAsync("gone").catch((e: unknown) => e);
    });
    expect(caught).toBeInstanceOf(ApiError);
    expect(caught).toMatchObject({ status: 404, code: "not_found" });
  });
});

describe("useTemplateActions", () => {
  it("patches the fields and replaces the slot list, keeping the detail cache on the latest answer", async () => {
    const renamed = makeTemplate({ name: "Cut" });
    const withSlot = makeTemplate({ name: "Cut", slots: [{ id: "s1", day_index: 0, slot: "breakfast", meal_id: "m1", meal_name: "Oat bowl", portion: 1 }] });
    const fake = fakeApi({ "PATCH /diet-templates/:id": () => json(renamed), "PUT /diet-templates/:id/slots": () => json(withSlot) });
    const queryClient = testQueryClient();
    queryClient.setQueryData(templateKeys.mine, { pages: [], pageParams: [] });
    const { result } = renderHook(() => useTemplateActions("t1"), { wrapper: clientWrapper(queryClient) });

    expect(await result.current.patch({ name: "Cut" })).toEqual(renamed);
    expect(fake.callsTo("PATCH", "/diet-templates/t1")[0]?.body).toEqual({ name: "Cut" });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toEqual(renamed);

    const items = [{ day_index: 0, slot: "breakfast" as const, meal_id: "m1", portion: 1 }];
    expect(await result.current.replaceSlots(items)).toEqual(withSlot);
    expect(fake.callsTo("PUT", "/diet-templates/t1/slots")[0]?.body).toEqual({ items });
    expect(queryClient.getQueryData(templateKeys.detail("t1"))).toEqual(withSlot);
    expect(queryClient.getQueryState(templateKeys.mine)?.isInvalidated).toBe(true);
  });

  it("keeps the same actions object while the id is unchanged", () => {
    const wrapper = clientWrapper();
    const { result, rerender } = renderHook(({ id }) => useTemplateActions(id), { wrapper, initialProps: { id: "t1" } });
    const before = result.current;
    rerender({ id: "t1" });
    expect(result.current).toBe(before);
  });
});
