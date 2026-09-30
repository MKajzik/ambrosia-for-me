import { expect, test } from "@playwright/test";
import { api, newAccount, register } from "./support";
import { localToday } from "./today";

type Created = { id: string };

const OATS = { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } };

test("generate a list from the plan, see it by aisle, check an item off, quick-add another, and find it all again after a reload", async ({ page }) => {
  await register(page, newAccount());
  const today = await localToday(page);
  const oats = await api<Created>(page, "POST", "/ingredients", OATS);
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1 });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  await api(page, "PUT", `/plan/${today}/breakfast`, { meal_id: porridge.id, portion: 2 });

  await page.goto("/shopping");
  await expect(page.getByText("No shopping lists yet")).toBeVisible();
  await page.getByRole("button", { name: "Generate from plan" }).first().click();
  await page.getByRole("dialog", { name: "Generate from your plan" }).getByRole("button", { name: "Generate list" }).click();

  // 100 g of oats per serving, planned at a portion of 2: 200 g, under its aisle.
  await expect(page).toHaveURL(/\/shopping\/[0-9a-f-]{36}$/);
  await expect(page.getByText(/From your plan ·/)).toBeVisible();
  const grains = page.getByRole("region", { name: "Grains and bread" });
  await expect(grains.getByRole("checkbox", { name: /Rolled oats/ })).toBeVisible();
  await expect(grains.getByText("200 g")).toBeVisible();
  await expect(page.getByText("0 of 1 checked")).toBeVisible();

  await grains.getByRole("checkbox", { name: /Rolled oats/ }).click();
  await expect(grains.getByRole("checkbox", { name: /Rolled oats/ })).toBeChecked();
  await expect(page.getByText("1 of 1 checked")).toBeVisible();

  await page.getByLabel("Add an item").fill("Milk");
  await page.keyboard.press("Enter");
  const other = page.getByRole("region", { name: "Other" });
  await expect(other.getByRole("checkbox", { name: /Milk/ })).toBeEnabled();
  await expect(page.getByText("1 of 2 checked")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("region", { name: "Grains and bread" }).getByRole("checkbox", { name: /Rolled oats/ })).toBeChecked();
  await expect(page.getByRole("region", { name: "Other" }).getByRole("checkbox", { name: /Milk/ })).not.toBeChecked();

  // The list shows up on the lists page too, with the range it was built from.
  await page.goto("/shopping");
  await expect(page.getByRole("link", { name: /From your plan/ })).toBeVisible();
});

test("a shared list updates live in the partner's browser, and is gone for them once the link ends", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });

    const list = await api<Created>(page, "POST", "/shopping-lists", { name: "Barbecue", shared_with_partner: true });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Sausages" });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Buns" });

    await page.goto(`/shopping/${list.id}`);
    await partnerPage.goto(`/shopping/${list.id}`);
    await expect(page.getByText(/^Shared with /)).toBeVisible();
    await expect(partnerPage.getByText(/’s list$/)).toBeVisible();
    await expect(partnerPage.getByRole("button", { name: "List settings" })).toHaveCount(0);

    // One person ticks, the other sees it without reloading: the event stream reaches the browser unbuffered.
    await page.getByRole("checkbox", { name: /Sausages/ }).click();
    await expect(page.getByRole("checkbox", { name: /Sausages/ })).toBeChecked();
    await expect(partnerPage.getByRole("checkbox", { name: /Sausages/ })).toBeChecked({ timeout: 10_000 });
    await expect(partnerPage.getByText(/^Checked by /)).toBeVisible();

    // And the other way round: the partner may tick too.
    await partnerPage.getByRole("checkbox", { name: /Buns/ }).click();
    await expect(partnerPage.getByRole("checkbox", { name: /Buns/ })).toBeChecked();
    await expect(page.getByRole("checkbox", { name: /Buns/ })).toBeChecked({ timeout: 10_000 });

    // The owner ends the link while the partner still has the list open: the stream closes and the page says so.
    await api(page, "DELETE", "/partner");
    await expect(partnerPage.getByText("That list isn't available. It may have been deleted, or it is no longer shared with you.")).toBeVisible({ timeout: 20_000 });
    await expect(partnerPage.getByRole("checkbox", { name: /Sausages/ })).toHaveCount(0);
  } finally {
    await partnerContext.close();
  }
});

test("the owner deleting a shared list tells the partner who has it open", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });
    const list = await api<Created>(page, "POST", "/shopping-lists", { name: "Party", shared_with_partner: true });
    await api(page, "POST", `/shopping-lists/${list.id}/items`, { name: "Crisps" });

    await partnerPage.goto(`/shopping/${list.id}`);
    await expect(partnerPage.getByRole("checkbox", { name: /Crisps/ })).toBeVisible();

    await page.goto(`/shopping/${list.id}`);
    await page.getByRole("button", { name: "List settings" }).click();
    const dialog = page.getByRole("dialog", { name: "List settings" });
    await dialog.getByRole("button", { name: "Delete list" }).click();
    await dialog.getByRole("button", { name: "Delete list" }).click();
    await expect(page).toHaveURL(/\/shopping$/);

    await expect(partnerPage.getByText("This list was deleted.")).toBeVisible({ timeout: 10_000 });
  } finally {
    await partnerContext.close();
  }
});
