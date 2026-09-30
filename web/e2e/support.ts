import { expect, type Page } from "@playwright/test";

export const PASSWORD = "correct-horse-battery";

export function newAccount() {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return { name: "E2E Tester", email: `e2e-${id}@example.test` };
}

export async function register(page: Page, account: { name: string; email: string }) {
  await page.goto("/register");
  await page.getByLabel("Your name").fill(account.name);
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/today$/);
}

/**
 * Calls the API through the web app's own `/api` proxy, as a signed-in page would. It runs inside the page so the browser adds
 * the `Origin` header the proxy's CSRF check needs (`page.request` does not), and it needs a page already on the app's origin.
 */
export async function api<T = unknown>(page: Page, method: string, path: string, body?: unknown): Promise<T> {
  return (await page.evaluate(
    async ({ method, path, body }) => {
      const res = await fetch(`/api${path}`, {
        method,
        headers: body === undefined ? undefined : { "content-type": "application/json" },
        body: body === undefined ? undefined : JSON.stringify(body),
      });
      if (!res.ok) throw new Error(`${method} ${path} answered ${res.status}`);
      return res.status === 204 ? null : await res.json();
    },
    { method, path, body },
  )) as T;
}
