import { describe, expect, it } from "vitest";
import { ApiError } from "@/lib/api/problem";
import { shouldRetry } from "./providers";

describe("shouldRetry", () => {
  it("never retries an answer the caller has to fix", () => {
    expect(shouldRetry(0, new ApiError({ status: 404, code: "not_found" }))).toBe(false);
    expect(shouldRetry(0, new ApiError({ status: 429, code: "rate_limited" }))).toBe(false);
  });

  it("retries server and network failures twice", () => {
    expect(shouldRetry(0, new ApiError({ status: 502, code: "upstream_unavailable" }))).toBe(true);
    expect(shouldRetry(1, new TypeError("fetch failed"))).toBe(true);
    expect(shouldRetry(2, new TypeError("fetch failed"))).toBe(false);
  });
});
