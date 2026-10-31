import { defineConfig } from "eslint/config";
import { builtinModules } from "node:module";
import obsidianmd from "eslint-plugin-obsidianmd";
import reactHooks from "eslint-plugin-react-hooks";
import sonarjs from "eslint-plugin-sonarjs";
import storybook from "eslint-plugin-storybook";

export default defineConfig([
  { ignores: ["src/api/generated/**", "main.js", "storybook-static/**"] },
  ...obsidianmd.configs.recommended,
  ...storybook.configs["flat/recommended"],
  {
    files: ["src/**/*.{ts,tsx}"],
    languageOptions: { parserOptions: { projectService: true, tsconfigRootDir: import.meta.dirname } },
    plugins: { "react-hooks": reactHooks },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "obsidianmd/ui/sentence-case": ["error", { brands: ["Topic Stream"], acronyms: ["URL", "RSS", "API", "AI", "HTTP", "HTTPS", "README", "IANA", "LLM"] }],
      complexity: ["error", 10],
      "max-depth": ["error", 4],
      "max-params": ["error", 4],
      "max-statements": ["error", 60]
    }
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    ignores: ["src/**/*.test.{ts,tsx}", "src/**/*.stories.{ts,tsx}", "src/test/**"],
    plugins: { sonarjs },
    rules: {
      "max-lines-per-function": ["error", { max: 100, skipBlankLines: true, skipComments: true }],
      "sonarjs/cognitive-complexity": ["error", 15],
      "no-restricted-imports": ["error", {
        paths: [...new Set([...builtinModules, "electron"])].map((name) => ({ name, message: "Use mobile-safe browser or public Obsidian services." })).concat([
          { name: "obsidian", importNames: ["FileSystemAdapter"], message: "Use Vault/FileManager through the mobile-safe host adapter." }
        ]),
        patterns: ["node:*"]
      }],
      "no-restricted-globals": ["error", "process", "Buffer", "require", "__dirname", "__filename"]
    }
  },
  {
    files: ["src/features/reader/ReaderBody.tsx", "src/test/storybook/markdown.ts"],
    // These DOM boundaries also execute in ordinary browsers without Obsidian's DOM extensions.
    rules: { "obsidianmd/prefer-create-el": "off" }
  },
  {
    files: ["src/shared/advisory-connection.ts"],
    // This host-independent state machine owns and cancels every retry on close or realm migration.
    // Its network retries do not depend on a DOM window and also run in Node-based owner tests.
    rules: { "obsidianmd/prefer-window-timers": "off" }
  },
  {
    files: ["src/main.ts"],
    // Preserve the existing public command ID rather than breaking saved hotkeys.
    rules: { "obsidianmd/commands/no-plugin-id-in-command-id": "off" }
  },
  {
    files: ["src/settings.ts"],
    // Declarative settings need Obsidian 1.13; this plugin supports 1.8.7.
    rules: { "obsidianmd/settings-tab/prefer-setting-definitions": "off" }
  }
]);
