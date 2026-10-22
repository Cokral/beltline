import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: ".",
  testMatch: "capture.spec.ts",
  timeout: 900_000,
  workers: 1,
  reporter: "line",
  use: { browserName: "chromium", headless: true, viewport: { width: 1080, height: 680 } }
});
