import path from "node:path";
import { defineConfig } from "vitest/config";
import { playwright } from "@vitest/browser-playwright";
import { storybookTest } from "@storybook/addon-vitest/vitest-plugin";

process.env.PLAYWRIGHT_BROWSERS_PATH ??= path.resolve("../.artifacts/playwright");

export default defineConfig({
  test: {
    projects: [
      { test: { name: "unit", include: ["src/**/*.test.{ts,tsx}"], environment: "node" } },
      {
        plugins: [storybookTest({ configDir: path.resolve(".storybook") })],
        test: {
          name: "storybook",
          browser: { enabled: true, headless: true, provider: playwright(), instances: [{ browser: "chromium" }] }
        }
      }
    ]
  }
});
