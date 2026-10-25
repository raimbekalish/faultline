import { defineConfig } from "@playwright/test";
export default defineConfig({
  testDir: "./e2e",
  workers: 1,
  use: {
    baseURL: process.env.FAULTLINE_TEST_URL || "http://127.0.0.1:8080",
    channel: "chrome",
    screenshot: "only-on-failure",
  },
  outputDir:
    process.env.FAULTLINE_BROWSER_OUTPUT || "../artifacts/browser-results",
  reporter: [["list"]],
});
