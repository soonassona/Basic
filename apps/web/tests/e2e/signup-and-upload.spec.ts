// Phase 1 exit criterion: signup → org provisioning → upload → image lands
// in the library. The test stands up the full stack (web + api + minio +
// postgres) via docker compose; CI calls it after `docker compose up -d`.
//
// Use a unique email per run so the test is idempotent against shared dev
// databases.

import { test, expect } from "@playwright/test";
import { randomBytes } from "node:crypto";

test("user signs up and uploads an image", async ({ page }) => {
  const suffix = randomBytes(4).toString("hex");
  const email = `e2e-${suffix}@example.com`;
  const password = "Phase1-IsGreen!"; // 16 chars, mixed case, digit

  await page.goto("/register");
  await page.getByLabel(/display name/i).fill(`E2E ${suffix}`);
  await page.getByLabel(/email/i).fill(email);
  await page.getByLabel(/password/i).fill(password);
  await page.getByRole("button", { name: /create account/i }).click();

  await expect(page).toHaveURL(/\/dashboard$/);
  await expect(page.getByRole("heading", { name: /dashboard/i })).toBeVisible();

  await page.goto("/images");
  // 1×1 PNG generated inline so we don't need a binary fixture in repo.
  const png = Buffer.from(
    "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR4nGNgYAAAAAMAAWgmWQ0AAAAASUVORK5CYII=",
    "base64",
  );

  const fileChooserPromise = page.waitForEvent("filechooser");
  await page.getByRole("button", { name: /upload/i }).click();
  const chooser = await fileChooserPromise;
  await chooser.setFiles({ name: "phase1.png", mimeType: "image/png", buffer: png });

  await expect(page.getByTestId("image-card").first()).toBeVisible({ timeout: 30_000 });
});
