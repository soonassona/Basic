import { defineConfig, devices } from "@playwright/test";

export default defineConfig({
  testDir: "./tests/e2e",
  fullyParallel: false,
  retries: process.env.CI ? 2 : 0,
  workers: 1,
  reporter: process.env.CI ? "github" : "list",
  use: {
    baseURL: process.env.E2E_BASE_URL ?? "http://localhost:3000",
    trace: "retain-on-failure",
  },
  webServer: process.env.E2E_NO_SERVER
    ? undefined
    : {
        // Node 25 exposes an incomplete localStorage object in server
        // runtimes; disable experimental web storage so Next dev overlay
        // does not crash before Playwright can boot the app.
        command: "node --no-experimental-webstorage ./node_modules/next/dist/bin/next dev --port 3000",
        url: "http://localhost:3000",
        reuseExistingServer: !process.env.CI,
        timeout: 120 * 1000,
      },
  projects: [{ name: "chromium", use: { ...devices["Desktop Chrome"] } }],
});
