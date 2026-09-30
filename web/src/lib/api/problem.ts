import type { components } from "./schema.gen";

export type Problem = components["schemas"]["Problem"];

/** An error answer from the API (or from our own proxy, which speaks the same problem+json). */
export class ApiError extends Error {
  readonly status: number;
  /** The API's stable machine-readable code, e.g. `invalid_credentials`. */
  readonly code: string;
  /** Per-field problems of a `validation_failed` answer, keyed by JSON field name. */
  readonly fieldErrors: Record<string, string>;
  /** Seconds to wait before retrying, from `Retry-After`, when the server said so. */
  readonly retryAfter: number | null;

  constructor(init: { status: number; code: string; title?: string; fieldErrors?: Record<string, string>; retryAfter?: number | null }) {
    super(init.title ?? init.code);
    this.name = "ApiError";
    this.status = init.status;
    this.code = init.code;
    this.fieldErrors = init.fieldErrors ?? {};
    this.retryAfter = init.retryAfter ?? null;
  }
}

/** What a field problem code means to a person. Unknown codes get a generic line. */
export function fieldMessage(field: string, code: string): string {
  switch (code) {
    case "required":
      return "This field is required.";
    case "too_short":
      return field === "password" ? "Use at least 10 characters." : "This is too short.";
    case "too_long":
      return "This is too long.";
    case "invalid_format":
      return field === "email" ? "Enter a valid email address." : "This is not in the expected format.";
    default:
      return "This value is not valid.";
  }
}

/** Builds an ApiError from a status, an already-parsed problem body (or anything else) and the response headers. */
export function toApiError(status: number, body: unknown, headers: Headers): ApiError {
  const problem = typeof body === "object" && body !== null ? (body as Partial<Problem>) : null;
  const fieldErrors: Record<string, string> = {};
  for (const e of problem?.errors ?? []) fieldErrors[e.field] ??= fieldMessage(e.field, e.code);
  const retry = Number(headers.get("retry-after"));
  return new ApiError({
    status,
    code: typeof problem?.code === "string" ? problem.code : `http_${status}`,
    title: typeof problem?.title === "string" ? problem.title : undefined,
    fieldErrors,
    retryAfter: Number.isFinite(retry) && retry > 0 ? retry : null,
  });
}

/**
 * Builds an ApiError from a failed response whose body is still unread.
 * Tolerates bodies that are not problem+json (a gateway's HTML error page, an empty body).
 */
export async function readProblem(res: Response): Promise<ApiError> {
  let body: unknown = null;
  try {
    body = await res.json();
  } catch {
    body = null;
  }
  return toApiError(res.status, body, res.headers);
}

/** One sentence for a person, chosen by the error's code. */
export function problemMessage(err: unknown): string {
  if (!(err instanceof ApiError)) return "Something went wrong. Try again.";
  switch (err.code) {
    case "invalid_credentials":
      return "That email and password don't match.";
    case "email_taken":
      return "An account with that email already exists.";
    case "validation_failed":
      return "Check the highlighted fields.";
    case "rate_limited":
      return err.retryAfter ? `Too many attempts. Try again in ${err.retryAfter} seconds.` : "Too many attempts. Try again in a minute.";
    case "unauthorized":
      return "Your session has ended. Sign in again.";
    case "upstream_unavailable":
      return "Can't reach the server. Try again in a moment.";
    case "csrf_rejected":
      return "That request was blocked. Reload the page and try again.";
    default:
      return "Something went wrong. Try again.";
  }
}
