import { build } from "esbuild";
import { readFile } from "node:fs/promises";

const manifest = JSON.parse(await readFile(new URL("../manifest.json", import.meta.url), "utf8"));
if (manifest.isDesktopOnly !== false) throw new Error("The supported mobile boundary requires isDesktopOnly: false.");

// A browser dependency graph rejects Node/Electron imports, including transitive dependencies.
// Only Obsidian's public host API is external; no desktop module can hide behind externals.
const result = await build({
  entryPoints: ["src/main.ts"], bundle: true, write: false, metafile: true,
  platform: "browser", format: "cjs", target: "es2022", minify: true,
  external: ["obsidian"], logLevel: "error"
});
for (const output of Object.values(result.metafile.outputs)) {
  for (const dependency of output.imports) {
    if (dependency.external && dependency.path !== "obsidian") throw new Error(`Unsupported mobile dependency: ${dependency.path}`);
  }
}
console.log(`Mobile dependency boundary passed: ${Object.keys(result.metafile.inputs).length} modules; only public Obsidian API external.`);
