import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const result = spawnSync("pnpm", ["exec", "playwright", "install", "chromium"], {
  stdio: "inherit",
  env: { ...process.env, PLAYWRIGHT_BROWSERS_PATH: fileURLToPath(new URL("../../.artifacts/playwright", import.meta.url)) }
});
if (result.error) process.stderr.write(`${result.error.message}\n`);
process.exit(result.status ?? 1);
