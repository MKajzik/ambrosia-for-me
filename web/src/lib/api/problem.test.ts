import { describe, expect, it } from "vitest";
import { ApiError, problemMessage, readProblem } from "./problem";

function problemRes(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(typeof body === "string" ? body : JSON.stringify(body), { status, headers });
}

describe("readProblem", () => {
  it("reads the stable code and title", async () => {
    const err = await readProblem(problemRes(401, { type: "about:blank", title: "Unauthorized", status: 401, code: "invalid_credentials" }));
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ status: 401, code: "invalid_credentials", message: "Unauthorized" });
  });

  it("maps per-field errors to messages, keeping the first for a field", async () => {
    const err = await readProblem(
      problemRes(400, {
        code: "validation_failed",
        errors: [
          { field: "email", code: "invalid_format" },
          { field: "password", code: "too_short" },
          { field: "password", code: "required" },
        ],
      }),
    );
    expect(err.fieldErrors).toEqual({ email: "Enter a valid email address.", password: "Use at least 10 characters." });
  });

  it("reads Retry-After", async () => {
    expect((await readProblem(problemRes(429, { code: "rate_limited" }, { "Retry-After": "12" }))).retryAfter).toBe(12);
    expect((await readProblem(problemRes(429, { code: "rate_limited" }))).retryAfter).toBeNull();
  });

  it("copes with a body that is not JSON", async () => {
    const err = await readProblem(problemRes(502, "<html>Bad gateway</html>"));
    expect(err).toMatchObject({ status: 502, code: "http_502", fieldErrors: {} });
  });
});

describe("problemMessage", () => {
  it("speaks plainly for the codes the auth screens meet", () => {
    const e = (code: string, retryAfter: number | null = null) => new ApiError({ status: 400, code, retryAfter });
    expect(problemMessage(e("invalid_credentials"))).toBe("That email and password don't match.");
    expect(problemMessage(e("email_taken"))).toBe("An account with that email already exists.");
    expect(problemMessage(e("rate_limited", 30))).toBe("Too many attempts. Try again in 30 seconds.");
    expect(problemMessage(e("rate_limited"))).toBe("Too many attempts. Try again in a minute.");
  });

  it("never shows a raw code or an unexpected error's text", () => {
    expect(problemMessage(new ApiError({ status: 500, code: "internal_error", title: "boom" }))).toBe("Something went wrong. Try again.");
    expect(problemMessage(new Error("secret internals"))).toBe("Something went wrong. Try again.");
  });
});
