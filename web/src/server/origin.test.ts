import { describe, expect, it } from "vitest";
import { originGuard } from "./origin";

function req(method: string, headers: Record<string, string>) {
  return new Request("http://app.test/api/meals", { method, headers });
}

describe("originGuard", () => {
  it("lets safe methods through without an Origin", () => {
    expect(originGuard(req("GET", { host: "app.test" }))).toBeNull();
    expect(originGuard(req("HEAD", { host: "app.test" }))).toBeNull();
  });

  it("accepts a same-origin JSON write", () => {
    expect(originGuard(req("POST", { host: "app.test", origin: "http://app.test", "content-type": "application/json" }))).toBeNull();
    expect(originGuard(req("POST", { host: "app.test", origin: "http://app.test", "content-type": "application/json; charset=utf-8" }))).toBeNull();
  });

  it("accepts a same-origin write with no body and so no content type", () => {
    expect(originGuard(req("DELETE", { host: "app.test", origin: "http://app.test" }))).toBeNull();
  });

  it("refuses a write with a foreign Origin", async () => {
    const res = originGuard(req("POST", { host: "app.test", origin: "http://evil.test", "content-type": "application/json" }));
    expect(res?.status).toBe(403);
    expect(await res?.json()).toMatchObject({ code: "csrf_rejected", status: 403 });
  });

  it("refuses a write with no Origin, or one that is not a URL", () => {
    expect(originGuard(req("POST", { host: "app.test", "content-type": "application/json" }))?.status).toBe(403);
    expect(originGuard(req("POST", { host: "app.test", origin: "null", "content-type": "application/json" }))?.status).toBe(403);
  });

  it("refuses a same-origin write whose content type is not JSON", () => {
    expect(originGuard(req("POST", { host: "app.test", origin: "http://app.test", "content-type": "text/plain" }))?.status).toBe(403);
    expect(originGuard(req("PUT", { host: "app.test", origin: "http://app.test", "content-type": "application/x-www-form-urlencoded" }))?.status).toBe(403);
  });

  it("matches on host, so a sibling port is foreign", () => {
    expect(originGuard(req("POST", { host: "app.test:3000", origin: "http://app.test:4000", "content-type": "application/json" }))?.status).toBe(403);
  });
});
