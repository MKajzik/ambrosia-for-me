import { expect, test, type Page } from "@playwright/test";

const PASSWORD = "correct-horse-battery";

function newAccount() {
  const id = `${Date.now()}-${Math.random().toString(36).slice(2, 8)}`;
  return { name: "E2E Tester", email: `e2e-${id}@example.test` };
}

async function register(page: Page, account: { name: string; email: string }) {
  await page.goto("/register");
  await page.getByLabel("Your name").fill(account.name);
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/today$/);
}

test("register, sign out, then sign in again and land where you were headed", async ({ page }) => {
  const account = newAccount();
  await register(page, account);
  await expect(page.getByRole("heading", { name: "Today" })).toBeVisible();

  await page.getByRole("navigation", { name: "Main" }).first().getByRole("link", { name: "Profile" }).click();
  await expect(page.getByText(account.email)).toBeVisible();
  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);

  // Signed out: a protected page bounces to /login and remembers itself.
  await page.goto("/meals");
  await expect(page).toHaveURL(/\/login\?next=%2Fmeals$/);

  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/meals$/);
});

test("a wrong password says so and stays on the sign-in page", async ({ page }) => {
  await page.goto("/login");
  await page.getByLabel("Email").fill(newAccount().email);
  await page.getByLabel("Password").fill("not-the-password");
  await page.getByRole("button", { name: "Sign in" }).click();
  // Next.js keeps its own empty role="alert" route announcer on every page, so match on the text too.
  await expect(page.getByRole("alert").filter({ hasText: "don't match" })).toHaveText("That email and password don't match.");
  await expect(page).toHaveURL(/\/login$/);
});

test("the browser never holds a token it can read", async ({ page }) => {
  await register(page, newAccount());
  expect(await page.evaluate(() => document.cookie)).toBe("");
  const session = (await page.context().cookies()).filter((c) => c.name.startsWith("mp_"));
  expect(session.map((c) => c.name).sort()).toEqual(["mp_access", "mp_refresh"]);
  expect(session.every((c) => c.httpOnly && c.sameSite === "Lax")).toBe(true);
});

test("signing in never redirects to a page outside the app", async ({ page, context, baseURL }) => {
  const account = newAccount();
  await register(page, account);
  await context.clearCookies();

  await page.goto("/login?next=//evil.test/phish");
  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(`${baseURL}/today`);
});

test("a session survives an expired access token", async ({ page, context }) => {
  const account = newAccount();
  await register(page, account);
  const before = (await context.cookies()).find((c) => c.name === "mp_refresh")!.value;

  // What the browser does when the 15-minute access cookie runs out.
  await context.clearCookies({ name: "mp_access" });
  await page.goto("/profile");
  await expect(page.getByText(account.email)).toBeVisible();

  const after = await context.cookies();
  expect(after.find((c) => c.name === "mp_access")).toBeDefined();
  expect(after.find((c) => c.name === "mp_refresh")!.value).not.toBe(before);
});

test("parallel requests with an expired access token share one refresh and keep the session", async ({ page, context }) => {
  await register(page, newAccount());
  await context.clearCookies({ name: "mp_access" });

  // Five calls at once, all carrying the same (soon rotated) refresh token.
  const statuses = await page.evaluate(async () => (await Promise.all(Array.from({ length: 5 }, () => fetch("/api/me")))).map((r) => r.status));
  expect(statuses).toEqual([200, 200, 200, 200, 200]);

  // A replayed refresh token would have revoked the whole session family by now.
  expect(await page.evaluate(async () => (await fetch("/api/me")).status)).toBe(200);
  await page.reload();
  await expect(page).toHaveURL(/\/today$/);
});

test("a cross-site write to the API is refused", async ({ page }) => {
  await register(page, newAccount());
  const res = await page.request.post("/api/auth/logout", { headers: { origin: "http://evil.test" } });
  expect(res.status()).toBe(403);
  await page.goto("/today");
  await expect(page).toHaveURL(/\/today$/);
});
