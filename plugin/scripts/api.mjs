import { readFile, mkdir, writeFile } from "node:fs/promises";
import openapiTS, { astToString } from "openapi-typescript";

const input = new URL("../../api/openapi/openapi.yaml", import.meta.url);
const output = new URL("../src/api/generated/openapi.ts", import.meta.url);
const source = "/** Generated from api/openapi/openapi.yaml. Do not edit. */\n" +
  astToString(await openapiTS(input, { silent: true }));

if (process.argv.includes("--check")) {
  const current = await readFile(output, "utf8").catch(() => "");
  if (current !== source) {
    process.stderr.write("Generated API types differ. Run pnpm --dir plugin api:generate.\n");
    process.exitCode = 1;
  }
} else {
  await mkdir(new URL("../src/api/generated/", import.meta.url), { recursive: true });
  await writeFile(output, source);
}
