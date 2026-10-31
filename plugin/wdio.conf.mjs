import path from "node:path";

const artifacts = path.resolve("../.artifacts/obsidian");
const versions = (process.env.OBSIDIAN_TEST_VERSIONS ?? process.env.OBSIDIAN_APP_VERSION ?? "1.8.7").split(",");

export const config = {
  runner: "local",
  framework: "mocha",
  specs: ["./test/e2e/**/*.e2e.mjs"],
  maxInstances: 1,
  capabilities: versions.map((appVersion) => ({
    browserName: "obsidian",
    "wdio:obsidianOptions": {
      appVersion,
      installerVersion: process.env.OBSIDIAN_INSTALLER_VERSION ?? "earliest",
      plugins: ["."],
      vault: "test/vault",
      ...(process.env.OBSIDIAN_TEST_BINARY ? { binaryPath: process.env.OBSIDIAN_TEST_BINARY } : {})
    }
  })),
  services: ["obsidian"],
  cacheDir: path.join(artifacts, "cache"),
  outputDir: path.join(artifacts, "logs"),
  logLevel: "warn",
  injectGlobals: false,
  waitforTimeout: 10_000,
  connectionRetryTimeout: 15_000,
  connectionRetryCount: 0,
  mochaOpts: { timeout: 30_000 }
};
