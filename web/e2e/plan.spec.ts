import { expect, test, type Page } from "@playwright/test";
import { api, newAccount, register } from "./support";

type Created = { id: string };

// Per 100 g. Two meals of one ingredient give round calorie numbers: 100 g is 380 kcal, 250 g is 950 kcal.
const OATS = { name: "Rolled oats", category: "grains_bread", nutrients: { calories: 380, protein: 13, carbohydrates: 60, fat: 7 } };

/** The browser's own local date, the way the app derives "today". */
async function localToday(page: Page): Promise<string> {
  return page.evaluate(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  });
}

async function seedMeals(page: Page, shared = false) {
  const oats = await api<Created>(page, "POST", "/ingredients", OATS);
  const porridge = await api<Created>(page, "POST", "/meals", { name: "Porridge", servings: 1, shared_with_partner: shared });
  await api(page, "PUT", `/meals/${porridge.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 100, unit: "g" }] });
  const feast = await api<Created>(page, "POST", "/meals", { name: "Feast", servings: 1, shared_with_partner: shared });
  await api(page, "PUT", `/meals/${feast.id}/ingredients`, { items: [{ ingredient_id: oats.id, quantity: 250, unit: "g" }] });
  return { porridge, feast };
}

test("build a template, apply it, then swap and resize a meal on Today against the targets", async ({ page }) => {
  await register(page, newAccount());
  await seedMeals(page);
  await api(page, "PATCH", "/me", { target_kcal: 2000, target_protein_g: 100, target_carbs_g: 250, target_fat_g: 70 });
  const today = await localToday(page);

  // A one-day template with porridge for breakfast.
  await page.goto("/plan/templates/new");
  await page.getByLabel("Name").fill("Base day");
  await page.getByLabel("Number of days").fill("1");
  await page.getByRole("button", { name: "Create template" }).click();
  await expect(page.getByRole("heading", { name: "Edit template" })).toBeVisible();

  const saved = page.waitForResponse((r) => r.url().includes("/slots") && r.request().method() === "PUT" && r.ok());
  await page.getByRole("button", { name: "Add breakfast to day 1" }).click();
  await page.getByRole("dialog").getByRole("button", { name: /Porridge/ }).click();
  await saved;
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();

  // Apply it from today. Applying again asks before replacing.
  await page.goto("/plan");
  await page.getByRole("button", { name: "Apply template" }).click();
  let dialog = page.getByRole("dialog", { name: "Apply a diet template" });
  await expect(dialog.getByRole("option", { name: "Base day (1 day)" })).toBeAttached();
  await dialog.getByLabel("Start date").fill(today);
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();
  await expect(page.getByRole("region", { name: "Week totals" }).getByText("380 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Apply template" }).click();
  dialog = page.getByRole("dialog", { name: "Apply a diet template" });
  await dialog.getByLabel("Start date").fill(today);
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await expect(dialog.getByText(/Some of those days already have meals\. Replace them\?/)).toBeVisible();
  await dialog.getByRole("button", { name: "Keep my plan" }).click();
  await expect(dialog.getByText(/Replace them\?/)).toBeHidden();
  await dialog.getByRole("button", { name: "Apply template" }).click();
  await dialog.getByRole("button", { name: "Replace them" }).click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("link", { name: "Porridge" })).toBeVisible();

  // Today: the rings against the targets, then a swap and a portion change.
  await page.goto("/today");
  const rings = page.getByRole("region", { name: "Today's totals" });
  await expect(rings.getByText("380 kcal")).toBeVisible();
  await expect(rings.getByText("19% of 2,000 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Swap breakfast meal" }).click();
  await page.getByRole("dialog").getByRole("button", { name: /Feast/ }).click();
  await expect(page.getByRole("link", { name: "Feast" })).toBeVisible();
  await expect(rings.getByText("950 kcal")).toBeVisible();

  await page.getByRole("button", { name: "Increase portion of Feast" }).click();
  await expect(page.getByText("1.5 servings")).toBeVisible();
  await expect(rings.getByText("1,425 kcal")).toBeVisible();

  await page.reload();
  await expect(page.getByRole("link", { name: "Feast" })).toBeVisible();
  await expect(page.getByText("1.5 servings")).toBeVisible();
});

test("a partner's template can be read and copied but not applied or edited, and the copy can be applied", async ({ page, browser, baseURL }) => {
  await register(page, newAccount());
  const partnerContext = await browser.newContext({ baseURL });
  const partnerPage = await partnerContext.newPage();
  try {
    await register(partnerPage, newAccount());
    const invite = await api<{ code: string }>(page, "POST", "/partner/invite");
    await api(partnerPage, "POST", "/partner/accept", { code: invite.code });

    const { porridge } = await seedMeals(page);
    const template = await api<Created>(page, "POST", "/diet-templates", { name: "Shared week", day_count: 2, shared_with_partner: true });
    await api(page, "PUT", `/diet-templates/${template.id}/slots`, {
      items: [
        { day_index: 0, slot: "breakfast", meal_id: porridge.id, portion: 1 },
        { day_index: 1, slot: "dinner", meal_id: porridge.id, portion: 2 },
      ],
    });

    // Before copying, the partner has no template of their own to apply.
    await partnerPage.goto("/plan");
    await partnerPage.getByRole("button", { name: "Apply template" }).click();
    await expect(partnerPage.getByText("You have no diet templates yet.")).toBeVisible();
    await partnerPage.keyboard.press("Escape");

    await partnerPage.goto("/plan/templates");
    await partnerPage.getByRole("tab", { name: "Partner's" }).click();
    await partnerPage.getByRole("link", { name: /Shared week/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Shared week" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveCount(0);
    await expect(partnerPage.getByRole("region", { name: "Day 2" }).getByText("Porridge")).toBeVisible();
    await expect(partnerPage.getByRole("button", { name: /apply/i })).toHaveCount(0);

    await partnerPage.getByRole("button", { name: /Copy to my library/ }).click();
    await expect(partnerPage.getByRole("heading", { name: "Edit template" })).toBeVisible();
    await expect(partnerPage.getByLabel("Name", { exact: true })).toHaveValue("Shared week");
    await expect(partnerPage.getByText("2 days")).toBeVisible();
    await expect(partnerPage.getByRole("region", { name: "Day 1" }).getByRole("link", { name: "Porridge" })).toBeVisible();

    // The copy is theirs: it can be applied to their own plan.
    const today = await localToday(partnerPage);
    await partnerPage.goto("/plan");
    await partnerPage.getByRole("button", { name: "Apply template" }).click();
    const dialog = partnerPage.getByRole("dialog", { name: "Apply a diet template" });
    await expect(dialog.getByRole("option", { name: "Shared week (2 days)" })).toBeAttached();
    await dialog.getByLabel("Start date").fill(today);
    await dialog.getByRole("button", { name: "Apply template" }).click();
    await expect(dialog).toBeHidden();
    await expect(partnerPage.getByRole("link", { name: "Porridge" }).first()).toBeVisible();
  } finally {
    await partnerContext.close();
  }
});
