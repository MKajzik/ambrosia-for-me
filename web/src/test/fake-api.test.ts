// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { api } from "@/lib/api/client";
import { fakeApi, json, problem } from "./fake-api";

describe("fakeApi", () => {
  it("routes by method and path, fills :params, and records calls with their bodies", async () => {
    const fake = fakeApi({
      "GET /meals/:id": (req) => json({ id: req.params.id }),
      "POST /meals": () => json({ ok: true }, 201),
    });
    const got = await api.GET("/meals/{id}", { params: { path: { id: "abc" } } });
    expect(got.data).toEqual({ id: "abc" });
    await api.POST("/meals", { body: { name: "Soup", servings: 2 } });
    expect(fake.callsTo("POST", "/meals")[0]?.body).toEqual({ name: "Soup", servings: 2 });
  });

  it("answers problem+json the way the API does", async () => {
    fakeApi({ "GET /meals/:id": () => problem(404, "not_found") });
    const res = await api.GET("/meals/{id}", { params: { path: { id: "x" } } });
    expect(res.response.status).toBe(404);
    expect(res.error).toMatchObject({ code: "not_found" });
  });

  it("fails loudly on a request nobody expected", async () => {
    fakeApi({});
    await expect(api.GET("/meals", {})).rejects.toThrow("no route for GET /meals");
  });
});
