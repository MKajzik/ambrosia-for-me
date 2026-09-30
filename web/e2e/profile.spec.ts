import { expect, test } from "@playwright/test";
import { PASSWORD, api, newAccount, register } from "./support";
import { localToday } from "./today";

type Created = { id: string };

test("daily targets set in Profile show up as rings on Today, and clearing one says there is no target", async ({ page }) => {
  await register(page, newAccount());
  const today = await localToday(page);
  const oats = await api<Created>(page, "POST", "/ingredients", { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } });
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1 });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  await api(page, "PUT", `/plan/${today}/breakfast`, { meal_id: porridge.id, portion: 1 });

  await page.goto("/today");
  await expect(page.getByRole("region", { name: "Today's totals" }).getByText("No target set").first()).toBeVisible();

  await page.goto("/profile");
  await page.getByLabel("Calories (kcal)").fill("2000");
  await page.getByRole("button", { name: "Save targets" }).click();
  await expect(page.getByText("Targets saved.")).toBeVisible();

  await page.goto("/today");
  await expect(page.getByRole("region", { name: "Today's totals" }).getByText("19% of 2,000 kcal")).toBeVisible();

  await page.goto("/profile");
  await page.getByLabel("Calories (kcal)").fill("");
  await page.getByRole("button", { name: "Save targets" }).click();
  await expect(page.getByText("Targets saved.")).toBeVisible();
  await page.goto("/today");
  await expect(page.getByRole("group", { name: "Calories" }).getByText("No target set")).toBeVisible();
});

test("link a partner with an invite code through Profile, then unlink", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());

    await page.goto("/profile");
    await page.getByRole("button", { name: "Create invite code" }).click();
    const shown = page.getByText(/^[A-Z0-9]{4}-[A-Z0-9]{4}$/);
    await expect(shown).toBeVisible();
    const code = (await shown.textContent()) ?? "";
    await expect(page.getByText(/Waiting for your partner/)).toBeVisible();

    await partnerPage.goto("/profile");
    await partnerPage.getByLabel("Invite code").fill("not-a-code");
    await partnerPage.getByRole("button", { name: "Link accounts" }).click();
    await expect(partnerPage.getByText("That invite code isn't valid. It may have expired or already been used.")).toBeVisible();
    await partnerPage.getByLabel("Invite code").fill(code);
    await partnerPage.getByRole("button", { name: "Link accounts" }).click();
    await expect(partnerPage.getByText(/Linked with .* since /)).toBeVisible();

    await page.reload();
    await expect(page.getByText(/Linked with .* since /)).toBeVisible();
    await page.getByRole("button", { name: "Unlink" }).click();
    const dialog = page.getByRole("dialog", { name: /Unlink from/ });
    await expect(dialog.getByText(/need a new invite to link again/)).toBeVisible();
    await dialog.getByRole("button", { name: "Unlink" }).click();
    await expect(page.getByRole("button", { name: "Create invite code" })).toBeVisible();

    await partnerPage.reload();
    await expect(partnerPage.getByRole("button", { name: "Create invite code" })).toBeVisible();
  } finally {
    await partnerContext.close();
  }
});

test("editing a custom ingredient keeps the nutrients the form does not show, and one a meal uses cannot be deleted", async ({ page }) => {
  await register(page, newAccount());
  const granola = await api<Created>(page, "POST", "/ingredients", {
    name: "Granola",
    category: "grains_bread",
    grams_per_piece: 40,
    nutrients: { calories: 450, protein: 10, carbohydrates: 60, fat: 15, fibre: 7, iron: 3.5 },
  });
  const meal = await api<Created>(page, "POST", "/meals", { name: "Breakfast bowl", servings: 1 });
  await api(page, "PUT", `/meals/${meal.id}/ingredients`, { items: [{ ingredient_id: granola.id, quantity: 100, unit: "g" }] });

  await page.goto("/profile/ingredients");
  await page.getByLabel("Only my custom ingredients").check();
  await expect(page.getByText("Granola")).toBeVisible();
  await page.getByRole("button", { name: "Edit Granola" }).click();
  const edit = page.getByRole("dialog", { name: "Edit custom ingredient" });
  await edit.getByLabel(/Calories per 100 g/).fill("470");
  await edit.getByRole("button", { name: "Save ingredient" }).click();
  await expect(edit).toBeHidden();

  // The fibre and iron the form has no field for must still be there.
  const found = await api<{ items: { name: string; nutrients: Record<string, number | null> }[] }>(page, "GET", "/ingredients?q=Granola");
  const saved = found.items.find((i) => i.name === "Granola");
  expect(saved?.nutrients).toMatchObject({ calories: 470, protein: 10, fibre: 7, iron: 3.5 });

  // A meal still uses it, so it cannot be deleted.
  await page.getByRole("button", { name: "Delete Granola" }).click();
  const remove = page.getByRole("dialog", { name: "Delete this ingredient?" });
  await remove.getByRole("button", { name: "Delete ingredient" }).click();
  await expect(remove.getByText("A meal still uses this ingredient, so it can't be deleted.")).toBeVisible();
  await remove.getByRole("button", { name: "Keep it" }).click();

  // One nothing uses can be made and deleted from the page.
  await page.getByRole("button", { name: "New custom ingredient" }).first().click();
  const create = page.getByRole("dialog", { name: "New custom ingredient" });
  await create.getByLabel("Ingredient name").fill("Kelp");
  await create.getByRole("button", { name: "Create ingredient" }).click();
  await expect(page.getByText("Kelp")).toBeVisible();
  await page.getByRole("button", { name: "Delete Kelp" }).click();
  await page.getByRole("dialog", { name: "Delete this ingredient?" }).getByRole("button", { name: "Delete ingredient" }).click();
  await expect(page.getByText("Kelp")).toHaveCount(0);
});

test("deleting the account needs the email typed, ends the session, and the account can no longer sign in", async ({ page }) => {
  const account = newAccount();
  await register(page, account);
  await page.goto("/profile");
  await page.getByRole("button", { name: "Delete account" }).click();
  const dialog = page.getByRole("dialog", { name: "Delete your account?" });
  const confirm = dialog.getByRole("button", { name: "Delete my account" });
  await expect(confirm).toBeDisabled();
  await dialog.getByLabel(/to confirm/).fill(account.email);
  await expect(confirm).toBeEnabled();
  await confirm.click();

  await expect(page).toHaveURL(/\/login$/);
  expect(await page.evaluate(() => document.cookie)).toBe("");
  await page.goto("/today");
  await expect(page).toHaveURL(/\/login\?next=%2Ftoday$/);

  await page.getByLabel("Email").fill(account.email);
  await page.getByLabel("Password").fill(PASSWORD);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "don't match" })).toHaveText("That email and password don't match.");
});
