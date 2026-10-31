import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { mkdirSync } from "node:fs";

const source = fileURLToPath(new URL("../../", import.meta.url));
const logs = fileURLToPath(new URL("../../.artifacts/obsidian/logs/", import.meta.url));
mkdirSync(logs, { recursive: true });
const script = `
set -euo pipefail
tar -C /source --exclude=.git --exclude=node_modules --exclude=.artifacts \\
  --exclude=.scratch --exclude=.pnpm-store --exclude=storybook-static \\
  --exclude=plugin/main.js --exclude=plugin/main.js.map -cf - . | tar -C /work -xf -
corepack enable
pnpm install --frozen-lockfile
xvfb-run -a pnpm --dir plugin test:e2e
`;
const args = [
  "run", "--rm", "--init", "--ipc=host", "-e", "CI=true",
  "-v", `${source}:/source:ro`,
  "-v", "topic-stream-e2e-node-modules:/work/node_modules",
  "-v", "topic-stream-e2e-obsidian-cache:/work/.artifacts/obsidian/cache",
  "-v", `${logs}:/work/.artifacts/obsidian/logs`,
  "-w", "/work"
];
for (const name of ["OBSIDIAN_TEST_VERSIONS", "OBSIDIAN_APP_VERSION", "OBSIDIAN_INSTALLER_VERSION", "SSE_PROBE_URL"]) {
  if (process.env[name] !== undefined) args.push("-e", name);
}
args.push("mcr.microsoft.com/playwright:v1.64.1-noble", "bash", "-lc", script);
const result = spawnSync("docker", args, { stdio: "inherit" });
if (result.error) process.stderr.write(`Could not start Docker: ${result.error.message}\n`);
process.exit(result.status ?? 1);
