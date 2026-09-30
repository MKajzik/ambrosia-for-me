import { describe, expect, it } from "vitest";
import { ApiError, problemMessage, toApiError } from "./problem";

const err = (code: string, status = 409) => new ApiError({ status, code });

describe("problemMessage for shopping and the partner link", () => {
  it.each([
    ["version_conflict", /Someone else changed this item/],
    ["version_required", /Reload/],
    ["invite_invalid", /isn't valid/],
    ["partner_already_linked", /already linked/],
  ])("explains %s in words", (code, pattern) => {
    expect(problemMessage(err(code))).toMatch(pattern);
  });
});

describe("ApiError.body", () => {
  it("keeps the whole problem document, including members the UI does not know, such as a conflict's current item", () => {
    const current = { id: "i1", version: 4, name: "Bananas" };
    const error = toApiError(409, { type: "urn:x", title: "Conflict", status: 409, code: "version_conflict", current }, new Headers());
    expect(error.code).toBe("version_conflict");
    expect(error.body.current).toEqual(current);
  });

  it("is empty when the answer was not a problem", () => {
    expect(toApiError(502, "<html>bad gateway</html>", new Headers()).body).toEqual({});
    expect(toApiError(500, null, new Headers()).body).toEqual({});
  });
});
