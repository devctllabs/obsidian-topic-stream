# Development

## Run the backend on the host

Install Go 1.27.3, Docker Compose v2, and mise. Configure the host process and start only its
development databases:

```bash
cp .env.example .env
mise run deps-up
```

Set these values in `.env`; use your own stable UUIDv4 for the Workspace:

```dotenv
TOPIC_STREAM_POSTGRES_DB=topic_stream
TOPIC_STREAM_POSTGRES_USER=topic_stream
TOPIC_STREAM_POSTGRES_PASSWORD=topic_stream
TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN=postgres://${TOPIC_STREAM_POSTGRES_USER}:${TOPIC_STREAM_POSTGRES_PASSWORD}@127.0.0.1:5432/${TOPIC_STREAM_POSTGRES_DB}?sslmode=disable
TOPIC_STREAM_WORKSPACE_ID=f433587c-d050-44bb-a0d4-e64cdb8d5849
TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT=http://localhost:11434/v1
```

Start the external embedding provider and load the configured model, then migrate and run:

```bash
mise run migrate-up
mise run start
```

The dev Compose project has its own volumes. It does not start the application or apply migrations.
If you also run the full stack, stop one backend before binding the other to port 7331.

To run the full stack from this checkout, use the local image explicitly:

```bash
mise run image-build
TOPIC_STREAM_BACKEND_IMAGE=topic-stream:local mise run up
```

The ordinary `mise run up` uses the backend image selected in `.env`, which defaults to the public
`latest` image.

## Build the Obsidian plugin

Use Node.js and the pnpm version declared in the root `package.json`:

```bash
pnpm install --frozen-lockfile
pnpm --dir plugin build
pnpm check
```

Install the output using the [Obsidian setup instructions](guides/getting-started.md#install-the-obsidian-plugin).
For incremental builds, use `pnpm --dir plugin dev`.

The plugin generates TypeScript operation/schema types from the authoritative OpenAPI document.
After a contract edit run `pnpm --dir plugin api:generate`; `api:check` compares the generated
bytes without editing files and runs in both plugin and contract CI. Endpoint methods remain
handwritten over the buffered Obsidian `requestUrl` adapter. The generator and lint parser use
TypeScript 5.9.3's compiler API; Obsidian typings target the supported minimum host version 1.8.7.

Run individual gates with `pnpm --dir plugin typecheck`, `lint`, `test`, or `build`; `check`
combines generated drift, typecheck, lint, the browser/mobile dependency build and tests.
Production builds also typecheck and minify. Lint covers Obsidian guidelines, React Hooks, the
configured complexity ceilings and desktop imports/globals. `test:mobile` bundles the actual
production entry for browsers, allowing only the public Obsidian host API as an external.

Production-bundle lifecycle tests use a copied disposable vault:

```bash
pnpm --dir plugin test:e2e:headless
# Optional native desktop run with an application window:
pnpm --dir plugin test:e2e
```

The headless runner uses Docker/Xvfb with pinned `mcr.microsoft.com/playwright:v1.64.1-noble`,
a read-only source mount and dedicated dependency/Obsidian-cache volumes. It installs the
lockfile in Linux and builds the plugin there. Host logs persist in `.artifacts/obsidian/logs/`;
CI runs this same headless command and uploads those diagnostics on failure.
Set `OBSIDIAN_APP_VERSION` (default `1.8.7`),
`OBSIDIAN_INSTALLER_VERSION` (default `earliest`), or comma-separated `OBSIDIAN_TEST_VERSIONS`
to select hosts. Tests use local API fixtures and never install into a personal vault. The
host flows verify URL replacement, preference persistence, disable/re-enable cleanup, independent
All/Topic visits, Unread retention through reading and Refresh, and a fresh visit after view reopen.
Reader coverage uses the real host MarkdownRenderer, verifies no vault files are created, blocks
images until consent, reuses them during the view session, blocks source-triggered code processors
and checks explicit-only Summary generation. Digest/Task flows exercise saved editions and Build;
Snapshot flows verify real Vault/FileManager writes, overwrite rejection, rollback, preferences
and captured text. SSE flows cover two views, dropped/malformed advisories, REST fallback,
pop-out migration, endpoint replacement and unload. Fixtures supply external API data; the
optional production Echo probe supplies separate real backend CORS/streaming evidence.
See [the plugin quality record](plugin-quality.md) for the evidence boundary and release checks.

## Storybook

Start the component catalog with `pnpm --dir plugin storybook` and open
`http://127.0.0.1:6006/`. `pnpm --dir plugin build:storybook` produces the ignored static catalog;
`pnpm --dir plugin test:storybook` runs its play/accessibility/geometry tests in headless Chromium.
Install that browser once with `pnpm --dir plugin browser:install`; its cache stays under
`.artifacts/playwright`. CI installs the pinned browser and runs story tests and static build.

Organization follows the other Obsidian plugins: co-located `*.stories.tsx` next to components,
`Features/<Feature>/<Role>/<Component>` for product surfaces and `Shared/<Category>/<Component>`
for reusable components. PascalCase story names describe states such as Loaded, Loading,
LoadError and FeedbackPending. The default toolbar selects light/dark and leaf/narrow/mobile
frames. Shared typed fixtures, deterministic service ports, providers and host frames live in
`plugin/src/test/storybook/`; `.storybook` contains discovery, addons, preview and the curated
Obsidian CSS fixture. Test-only compositions are clearly labeled under Shared/Testing;
regression-only stories stay in browser tests while hidden from normal navigation/autodocs.
The main entry is `Features/Shell/TopicStreamShell`. It renders the production connected shell
and currently implemented screens with in-memory service implementations. It makes no live API
calls. Technical connection/error/geometry scenarios run in browser tests and stay hidden from
the ordinary catalog. Each new feature extends this same composition.

Feed owns optional consumer ports for Reader content, shared Item summary state and Vault saving.
Hidden contract scenarios exercise invocation and presentation through those ports. The plugin
and primary Shell now connect the production Reader, shared Summary and Snapshot providers;
Storybook mocks only external backend and host ports. Feed event reconciliation consumes a
separate advisory event port. Each ItemView injects a browser Fetch/ReadableStream adapter and
shares one cancellable connection among feature subscribers. Pop-out migration reopens that
stream using the new owning window without resetting navigation or cached reading state.
Retries use bounded exponential backoff (1–30 seconds with jitter), never REST polling.
Connected and typed events reconcile authoritative resources; they never publish Feed or
generate summaries. See [CORS configuration](operations/configuration.md#browser-access-and-sse)
and the [transport decision](adr/0002-contract-first-rest-with-advisory-sse.md).

## Repository map

| Location | Responsibility |
| --- | --- |
| `cmd/topic-stream/` | Backend CLI |
| `internal/` | Domain, services, adapters, and dependency wiring |
| `plugin/` | Obsidian React plugin |
| `api/openapi/openapi.yaml` | Authoritative public HTTP contract |
| `gen/` | Generated Go configuration and HTTP contract code |
| `migrations/primary/postgres/` | Embedded SQL migrations |
| `devctl.yaml` | Configuration and generation inputs |

Use [CONTEXT.md](../CONTEXT.md) for domain terminology and the
[documentation index](README.md) for product behavior, architecture, and decision records.
Local implementation specifications and tickets belong in the configured issue tracker, following the
[issue tracker conventions](agents/issue-tracker.md); public documentation must remain readable without them.

## Configuration generation

The Devctl config target owns `gen/config/config.gen.go` and `.env.example`. Edit `devctl.yaml`,
then regenerate rather than editing either output. The repository-managed `devctl` version is
installed by mise:

```bash
mise run codegen
```

Generation does not modify your private `.env`. Review new variables when updating an existing checkout.

## Backend checks


The repository pins its Go linter with mise. Ordinary tests need no Docker or external services;
lint includes the integration-tagged files:

```bash
mise run test
mise run test-race
mise run lint
```

See [continuous integration](ci.md) for changed-file routes, PR/default-branch profiles and the
required final status. `mise run lint-ci` checks workflow syntax. Install PostgreSQL 17's
`pg_dump` and `pg_restore` clients for the integration dump/restore scenario; a running server
alone does not provide those host executables.

Run the integration suite with Docker available. Testcontainers starts PostgreSQL and Qdrant
automatically; no Compose setup or manual migrations are needed:

```bash
mise run test-integration
```

Integration tests use `//go:build integration`. The task runs
`go test -count=1 -p=2 -tags=integration ./...`, including ordinary tests once and disabling test-result
caching. At most two Go packages execute concurrently; the interleavings within concurrency tests
remain enabled. Each package starts only its required services, once per package, with randomly
mapped ports. Use `mise run test-integration-race` for the same selection with the race detector.

| Command | Selection and services |
| --- | --- |
| `mise run test` | Ordinary Go tests, uncached; no services |
| `mise run test-race` | Ordinary Go tests with race detector; no services |
| `mise run test-integration` | Ordinary plus primary integration, uncached, `-p=2` |
| `mise run test-integration-race` | Same complete selection with race detector |
| `mise run lint` | Go lint including primary integration |
| `mise run lint-optional` | Go lint including performance and live source |
| `mise run test-tools` | Local runner tests, without Docker |
| `mise run test-ci` | CI/release helper tests, without GitHub access |
| `mise run test-performance qdrant-search --smoke` | One explicit optional local profile |
| `mise run test-live-index --smoke` | Bounded embedding protocol-stub check |
| `mise run test-live-lifecycle --smoke` | Bounded model lifecycle protocol-stub check |
| `mise run test-live-summary --smoke` | Summary protocol-stub check |

An integration command already includes ordinary Go tests: do not run a separate complete unit
suite again in the same CI profile. Optional profiles and provider calls remain local opt-in.

To use existing local test services, set either or both variables:

```bash
set -a
. ./.env
set +a
export TOPIC_STREAM_TEST_POSTGRES_DSN="$TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN"
export TOPIC_STREAM_TEST_QDRANT_URL="$TOPIC_STREAM_QDRANT_URL"
mise run test-integration
```

A nonempty variable selects that external service; otherwise Testcontainers starts it. The selection
is independent, so local PostgreSQL can be combined with containerized Qdrant and vice versa. Qdrant's
URL must point to its **gRPC port**, with an HTTP(S) scheme. Unreachable services or unavailable Docker
fail the run instead of skipping tests; a failing external connection never falls back to a container.

Locally started Qdrant containers use `127.0.0.1` rather than `localhost` to avoid an unnecessary
gRPC DNS TXT service-configuration lookup. Remote Docker hostnames and explicit external service
URLs are preserved. For an external service running on this machine, prefer the loopback IP too.

Use PostgreSQL 17 and Qdrant 1.20, matching Testcontainers, the local runner and Compose.
Package setup reports the actual server versions. Planner calibration used 1.19.1; its finite recall/latency measurements are not a
validation of every newer runtime version. An external test service can exercise the deployed
version through `TOPIC_STREAM_TEST_QDRANT_URL`. The PostgreSQL user needs permission to
create and drop schemas, tables, and indexes in the selected database; `CREATEDB` is unnecessary.
Each test creates a unique schema and passes its `search_path` in the DSN to every connection.
Ordinary integration fixtures apply the embedded migrations; migration tests start with empty schemas.
The clean-install schema has eight functional migration pairs. A development database from the
earlier unpublished history requires deliberate recreation; the application does not reset it.
See [migration ownership and compatibility](operations/database.md) before reusing an existing database.
Qdrant tests create unique collections and require permission to create, query, and delete them.
Cleanup removes only test-created schemas, collections, and containers, and cleanup failures fail the run.
No embedding provider or API key is required.

For a shared-service job, provision one disposable PostgreSQL and one disposable Qdrant, export
both test endpoint variables and run the complete integration command once. Each package probes
those endpoints and owns only its schemas/collections. Fixtures use UUID schema/model identities;
migration locks include the schema, model-collection locks include the model ID, and consumers
filter database-wide advisories by Workspace. The bootstrap test deliberately uses the fixed
default model ID and fails if that collection already exists, so use dedicated test services
rather than an application Qdrant. It never deletes a pre-existing collection to proceed.
The job owns service teardown; fixtures own their data cleanup, with bounded cleanup contexts
independent of assertion or startup cancellation. No blanket schema reset or collection deletion
is part of shared-service setup.

Performance tests require the additional `performance` tag; real-model indexing and Summary
checks require `live`. Both are excluded from ordinary integration, including when provider
environment variables are set. The Feed EXPLAIN/resource scenario is optional; semantic display
correctness stays in ordinary integration. Use the tracked runner and explicit resource/provider
inputs in [Local performance checks](performance.md), including bounded stub smokes. Avoid
running every optional test together: some profiles deliberately seed large SQL fixtures or
restart their owned Qdrant service.

The HTTP Feed fixture reports reconciler errors and unexpected worker termination while waiting
for readiness, and prints bounded Job diagnostics with safe reasons on failure. Its readiness
budgets remain fixed; it does not retry a failed reconciler silently. Concurrent HTTP requests
are cancelled and joined before teardown.

Integration output includes phase timings for PostgreSQL/Qdrant setup, schema creation, embedded
migrations, schema removal and container cleanup. Qdrant adapter fixtures also report collection
setup and removal timings. Use `go test -json -count=1 -tags=integration ./...`
to capture those alongside per-test and package elapsed times. Test elapsed time includes fixture
work; subtract only measured non-overlapping phases when estimating the remaining body/adapter
work, and do not treat nested test totals as independent wall times. Startup and container cleanup
belong to the package process rather than individual cases.

Keep schemas isolated for global Job claims, unscoped counts, migration/advisory locks and DDL
fault injection. A new Workspace alone does not isolate those operations. A sequential case group
may share migrated setup when each failed statement rolls back and cannot leave mutable state for
the next case, as with the two scoped projection-FK rejection paths. Verify each selection alone
as well as grouped. Functional bulk tests exercise the current 1000-Item chunk boundary and its
overflow; sustained large input measurements belong to optional performance tests.

The lint configuration enables `revive`'s `argument-limit` rule with a maximum of four parameters, `cyclop` with a maximum cyclomatic complexity of 10, and `gocognit` with a cognitive-complexity threshold of 15; cohesive request/config values and focused helper functions are preferred over suppressions. Dynamic PostgreSQL query composition uses `huandu/go-sqlbuilder` with the PostgreSQL flavor, while static SQL remains inline.

Go documentation uses concise English comments starting with the declared name. Document exported
types, functions, concrete methods, and every interface method; explain fields and private declarations
when units, zero values, invariants, side effects, resource ownership, or concurrency need clarification.
`inamedparam` requires names for every interface input, including `ctx` and single or variadic inputs.
`revive` checks documentation on exported types and functions, exported concrete methods (including
private receiver types), and public interface methods. `godoclint` requires documentation on exported
test helpers; standard test entry points are exempt. Generated files are excluded. Private and inline
interfaces, test methods with private receivers, and comment accuracy remain review responsibilities.

When consolidating adapter tests, retain each actual write path. Qdrant full projection replacement
and payload-only updates both prove optional-field removal and stale-version fencing in one
collection scenario. Ensure covers missing indexes and persisted legacy incarnation markers;
concurrent creators, pre-limit filters and cross-Workspace isolation remain separate real-server
oracles. Assembled HTTP/worker tests retain their transport and recovery guarantees even when a
focused adapter or mocked unit test covers one individual operation.

Snapshot coverage exercises fixed Item/Digest Markdown, safely quoted frontmatter, exact-version
annotations, Compact/Full consent and image modes. Local attachment budgets are shared per Save;
image downloads accept only Item descriptors, and DOMPurify uses a static SVG allowlist. The
Obsidian adapter serializes writes per Vault and uses FileManager rollback for only newly-created
attachments. Native headless tests create real isolated notes, parse host frontmatter, reject
overwrite, inject note-create failure to verify rollback, retain preferences across reload and
save a confirmed mixed historical edition. Snapshot UI and menus are part of the primary
production-composed Shell story; only Vault/preferences and backend ports are in-memory.
