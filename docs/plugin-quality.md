# Obsidian plugin quality record

Verified on 2026-10-28 for the `0.1.0` working-tree candidate. This records implementation and
local release readiness; [release workflows](releases.md) bind publication to a reviewed immutable
commit and create a draft. Local verification creates no Git tag or GitHub Release.

## Required checks

| Boundary | Command / evidence |
| --- | --- |
| OpenAPI | `pnpm contract:lint`; valid authoritative contract |
| Plugin | `pnpm --dir plugin check`; generated-type drift, typecheck, lint, browser dependency build, 166 unit tests |
| Connected UI | `pnpm --dir plugin test:storybook`; 272 browser interaction/accessibility tests |
| Build outputs | `pnpm --dir plugin build` and `build:storybook`; minified plugin and static catalog |
| Real Obsidian | `pnpm --dir plugin test:e2e:headless`; nine production-bundle product flows in an isolated vault |
| Release automation | `mise run test-ci` (21 tests), `mise run lint-ci`; preparation/version rejection and workflow syntax |
| Backend change | Full Go suite/lint and scoped CORS/config/runtime race tests passed for approved global CORS |

The native host is Obsidian 1.8.7 with the earliest compatible installer (Electron 28 / Chromium
120). `test:e2e` uses a window; `test:e2e:headless` uses Docker/Xvfb and the pinned Playwright
1.64.1 Noble image. CI installs the lockfile and runs these same gates; failure logs persist under
`.artifacts/obsidian/logs/`. The native tests use external API fixtures while retaining the real
plugin bundle, Obsidian lifecycle, Markdown renderer and Vault/FileManager behavior.

## Scenario ownership

| Product behavior | Evidence owner |
| --- | --- |
| Commands, roots, URL replacement, local preferences and reload | Native foundation flow and ViewConnection/ItemView tests |
| Source types, nested Source picker, draft/dirty exit, Prefer/Avoid and conflicts | Settings production stories and source/transition owner tests |
| All/Topic publication boundaries, Unread retention, older pages and committed projection-pending writes | Feed production stories, typed-client tests and native Feed flow |
| Search, direct Reader versus Details, missing content, image consent and no implicit Vault writes | Search/Reader stories, reader owner tests and native Markdown flow |
| Explicit-only summaries, canonical/captured versions, permissions, failure and eligible Job Retry | Shared ItemSummaries tests, connected Shell/Reader/Digest stories and native summary flow |
| Manual-only definitions, safe Build, immutable editions, readiness baseline/offline badges and one live Notice | Digest/Settings/Tasks stories, readiness/build tests and native Digest/SSE flows |
| Five indexing phases, count/terminal precedence, shared queries, model replacement/recovery and outcome Notices | Job progress, transition, Settings and Tasks owner tests/stories |
| Compact/Full, fixed metadata, image modes/budgets, mixed consent, overwrite protection and rollback | Snapshot owner suites and three native Vault flows |
| Real streaming, malformed/interrupted input, bounded retries, stale callbacks and no polling | Browser transport/parser/connection tests and native SSE flow |
| Multi-view deduplication, dropped-advisory recovery, explicit REST fallback, pop-out move and unload | Native production SSE flow; shared authoritative-resource tests |

The SSE feasibility probe additionally runs against the actual Go SSE visitor and production Echo
CORS factory when `SSE_PROBE_URL` is supplied. That combined run passed ten scenarios and measured
connection chunks in 3–7 ms and Job advisories in 266–330 ms in distinct main/pop-out realms, with
zero remaining server subscriptions. Buffered `requestUrl` exposed only completed text after
about five seconds. See [the transport decision](adr/0002-contract-first-rest-with-advisory-sse.md)
and [operator CORS configuration](operations/configuration.md#browser-access-and-sse).

Native error collection retains application exceptions/rejections. Chromium's exception-free
deferred ResizeObserver notification is recorded separately, bounded and checked for settling
after window migration. This browser layout notification is defined by the
[Resize Observer specification](https://www.w3.org/TR/resize-observer/#deliver-resize-loop-error).
The production bundle contains no ResizeObserver implementation.

## Responsive and mobile evidence

The final connected Shell was rendered at 1180, 460 and 360 CSS-pixel viewports in both themes.
Forty-six captures cover Feed/Reader, Search, archive/edition, Tasks and Settings, plus 200% text
in Settings and editions. Geometry checks found no horizontal overflow or browser errors.
English/Spanish long content, title/Topic/Item/metadata hierarchy, focus, reduced motion and
synthesized Chromium touch navigation were checked. Two regression stories lock whole-word
navigation/headings and full-width narrow Settings panels. Independent visual review scored its
one responsive correction resolved and returned `ship`; the existing matte/peach system is retained.

The mobile gate bundles the actual runtime entry with browser resolution: 337 modules, with only
the public Obsidian API external. ESLint rejects Node/Electron imports, desktop filesystem adapters
and Node globals in handwritten runtime code. Negative browser-build controls reject filesystem
and Electron imports. Fetch streaming, Markdown and Vault operations use browser/public host APIs.
Physical iOS/Android smoke is outside the accepted v0.1 gate and has not been performed.

## Release assets and limits

An isolated Linux build copied current source without existing `main.js`, installed the frozen
lockfile and rebuilt the release files. Its `main.js`, `manifest.json` and `styles.css` match the
local build byte-for-byte. The minified script is 1,063,167 bytes with no source map; metadata agrees
on `0.1.0`, minimum Obsidian `1.8.7`, and `isDesktopOnly: false`. The local initial-release policy
check requires no metadata changes. The ignored `.artifacts/plugin-release-clean/` directory
contains the three assets and a SHA-256 verification record.

Release CI checks the actual remote version baseline, exact source/tag binding and metadata,
compares the downloaded asset manifest with source, then attaches exactly the three files to a
draft. Publishing remains manual. The published-baseline API-v1 compatibility checker and Community
Plugins directory submission remain deferred; neither is presented as a delivered check.
Generated bundles, local vault/cache/report paths and release staging remain ignored. Source,
stories, design/product documentation and generated contract types are eligible for source control.
