import { expect, test } from "@playwright/test";
import { api, newAccount, register } from "./support";

type Created = { id: string };

// Per 100 g. Only four nutrients are given, so every micronutrient is unknown, which the UI must show as a dash.
const OATS = { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } };

test("build a meal from the ingredient search and read its nutrition per serving", async ({ page }) => {
  await register(page, newAccount());
  await api(page, "POST", "/ingredients", OATS);

  await page.goto("/meals");
  await expect(page.getByText("No meals yet")).toBeVisible();
  await expect(page.getByRole("tab")).toHaveCount(0);
  await page.getByRole("link", { name: "Create your first meal" }).click();

  await page.getByLabel("Name").fill("Oat bowl");
  await page.getByLabel("Servings").fill("2");
  await page.getByRole("button", { name: "Create meal" }).click();
  await expect(page.getByRole("heading", { name: "Edit meal" })).toBeVisible();

  await page.getByRole("combobox", { name: "Add an ingredient" }).fill("Rolled");
  await page.getByRole("option", { name: /Rolled oats/ }).click();
  await page.getByLabel("Quantity of Rolled oats").fill("80");

  // 80 g of oats is 304 kcal, 10.4 g protein, 48 g carbs and 5.6 g fat; per serving with two servings, half of that.
  const nutrition = page.getByRole("region", { name: "Nutrition" });
  await expect(nutrition.getByText("152 kcal")).toBeVisible();
  await expect(nutrition.getByText("5.2 g")).toBeVisible();
  await expect(nutrition.getByText("24 g")).toBeVisible();
  await expect(nutrition.getByText("2.8 g")).toBeVisible();
  await expect(page.getByText("All changes saved")).toBeVisible();

  await nutrition.getByRole("button", { name: "All nutrients" }).click();
  await expect(page.getByRole("group", { name: "Vitamins" }).getByText("—").first()).toBeVisible();
  await expect(nutrition.getByText(/some ingredients lack data/i)).toBeVisible();

  // What the server holds is what the page shows.
  await page.reload();
  await expect(page.getByLabel("Quantity of Rolled oats")).toHaveValue("80");
  await expect(page.getByRole("region", { name: "Nutrition" }).getByText("152 kcal")).toBeVisible();

  await page.getByLabel("Servings").fill("4");
  await expect(page.getByRole("region", { name: "Nutrition" }).getByText("76 kcal")).toBeVisible();

  await page.goto("/meals");
  await page.getByRole("link", { name: /Oat bowl/ }).click();
  await page.getByRole("button", { name: "Delete meal" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete meal" }).click();
  await expect(page).toHaveURL(/\/meals$/);
  await expect(page.getByText("No meals yet")).toBeVisible();
});

test("a partner's shared meal can be read and copied but not edited, and disappears when they unlink", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });

    const oats = await api<Created>(page, "POST", "/ingredients", OATS);
    const shared = await api<Created>(page, "POST", "/meals", { name: "Shared porridge", servings: 2, shared_with_partner: true });
    await api(page, "PUT", `/meals/${shared.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
    await api(page, "POST", "/meals", { name: "Private stew", servings: 1 });

    await partnerPage.goto("/meals");
    await partnerPage.getByRole("tab", { name: "Partner's" }).click();
    await expect(partnerPage.getByRole("link", { name: /Shared porridge/ })).toBeVisible();
    await expect(partnerPage.getByRole("link", { name: /Private stew/ })).toHaveCount(0);

    await partnerPage.getByRole("link", { name: /Shared porridge/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Shared porridge" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveCount(0);
    await expect(partnerPage.getByRole("region", { name: "Nutrition" }).getByText("190 kcal")).toBeVisible();
    const sharedUrl = partnerPage.url();

    await partnerPage.getByRole("button", { name: /Copy to my library/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Edit meal" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveValue("Shared porridge");
    await expect(partnerPage.getByLabel("Quantity of Rolled oats")).toHaveValue("100");
    await expect(partnerPage.getByLabel("Share with my partner")).not.toBeChecked();

    // The owner unlinks: the shared meal is no longer theirs to see, but the copy stays.
    await api(page, "DELETE", "/partner");
    await partnerPage.goto(sharedUrl);
    await expect(partnerPage.getByText("That isn't available.")).toBeVisible();
    await partnerPage.goto("/meals");
    await expect(partnerPage.getByRole("link", { name: /Shared porridge/ })).toBeVisible();
    await expect(partnerPage.getByRole("tab")).toHaveCount(0);
  } finally {
    await partnerContext.close();
  }
});
