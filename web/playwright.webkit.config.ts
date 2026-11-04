import { defineConfig } from "@playwright/test";
import config from "./playwright.config";

export default defineConfig({
  ...config,
  projects: [
    {
      name: "webkit",
      testMatch: /type-(board|cards)\.spec\.ts/,
      use: { browserName: "webkit", launchOptions: { args: [] } },
    },
  ],
});
