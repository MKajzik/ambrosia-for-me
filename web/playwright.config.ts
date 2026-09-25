import { defineConfig, devices } from "@playwright/test";

// The tests drive the whole Compose stack (web, API, Postgres); see `make e2e-web`.
export default defineConfig({
  testDir: "./e2e",
  // One worker: all users reach the API from the web container's single address.
  workers: 1,
  fullyParallel: false,
  retries: 0,
  reporter: process.env.CI ? [["github"], ["html", { open: "never" }]] : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
  },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
