import type { Page } from "@playwright/test";

/** The browser's own local date as `YYYY-MM-DD`, the way the app derives "today". */
export async function localToday(page: Page): Promise<string> {
  return page.evaluate(() => {
    const d = new Date();
    return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, "0")}-${String(d.getDate()).padStart(2, "0")}`;
  });
}
