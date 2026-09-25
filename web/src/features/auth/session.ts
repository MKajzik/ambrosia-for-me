import type { components } from "@/lib/api/schema.gen";
import { readProblem } from "@/lib/api/problem";

export type User = components["schemas"]["User"];
export type LoginValues = components["schemas"]["LoginRequest"];
export type RegisterValues = components["schemas"]["RegisterRequest"];

async function post(path: string, body?: unknown): Promise<Response> {
  return fetch(path, {
    method: "POST",
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}

/** Signs in through the BFF, which turns the tokens into httpOnly cookies. Throws an ApiError on failure. */
export async function login(values: LoginValues): Promise<User> {
  const res = await post("/api/auth/login", values);
  if (!res.ok) throw await readProblem(res);
  return ((await res.json()) as { user: User }).user;
}

/** Creates an account and signs in. Throws an ApiError on failure. */
export async function register(values: RegisterValues): Promise<User> {
  const res = await post("/api/auth/register", values);
  if (!res.ok) throw await readProblem(res);
  return ((await res.json()) as { user: User }).user;
}

export async function logout(): Promise<void> {
  await post("/api/auth/logout");
}
