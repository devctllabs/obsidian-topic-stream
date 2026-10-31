# Topic Stream

A personal reading stream inside Obsidian, shaped by the sources and topics you choose.

Topic Stream brings articles, papers, videos, and repositories into one place. Describe what you
want to follow, read a focused Feed, and use feedback to improve future recommendations.
Run the backend on your own machine or a reachable server, then connect the Obsidian plugin.

## What you can do

- Manage Sources, Topics, preferences, models and Digest definitions in Settings.
- Read the Feed by Topic, keep an Unread view, and publish the next batch with explicit Refresh.
- Search the archive, read cleaned Markdown, load images by consent, and request an AI summary explicitly.
- Read saved Digest editions, build Manual-only or scheduled definitions, and follow Tasks with safe Cancel/Retry.
- Save independent Compact or Full Vault Snapshots, including bounded local attachments.

Live advisories reconcile saved state without publishing Feed or generating AI text. The
[plugin guide](docs/guides/using-topic-stream.md) describes each workflow.

## Get started

You need Docker with Compose v2, mise, Obsidian, and a running local OpenAI-compatible embedding service.
Building the plugin also requires Node.js and pnpm. The server stack includes PostgreSQL and Qdrant;
AI models are managed separately.

1. Clone this repository and copy `.env.example` to `.env`.
2. Set a stable UUIDv4 Workspace ID and the embedding endpoint, model, and dimensions.
   For a provider on your Docker host, use `http://host.docker.internal:11434/v1` rather than `localhost`.
3. Start the server stack:

   ```bash
   mise run up
   ```

   The generated `.env` defaults to the public `latest` backend image. To run the current checkout
   instead, build and select the local image explicitly:

   ```bash
   mise run image-build
   TOPIC_STREAM_BACKEND_IMAGE=topic-stream:local mise run up
   ```

4. [Build and install the Obsidian plugin](docs/guides/getting-started.md#install-the-obsidian-plugin),
   connect to `http://127.0.0.1:7331`, and follow the
   [first Feed steps](docs/guides/getting-started.md#get-your-first-feed).

The [getting started guide](docs/guides/getting-started.md) walks through configuration, model setup,
and your first Feed. The default deployment exposes the API only on localhost; it has no built-in authentication.

## Install the plugin manually

Use the three files from a plugin release, or build this checkout:

```bash
pnpm install --frozen-lockfile
pnpm --dir plugin build
```

Copy `plugin/main.js`, `plugin/manifest.json` and `plugin/styles.css` to
`<vault>/.obsidian/plugins/topic-stream/`, then enable Topic Stream in Community plugins.
Obsidian 1.8.7 or newer is required. Releases use independent plugin versions and the MIT license;
Community Plugins directory submission is deferred.

For mobile, `127.0.0.1` means the phone or tablet. Set Server URL to a backend reachable from that
device through your configured LAN, VPN or protected HTTPS deployment. The default localhost-only
Docker binding must be configured for that access. Streaming also needs the backend's
[exact-origin CORS allowlist](docs/operations/configuration.md#browser-access-and-sse); the default
is `app://obsidian.md`. Browser-safe dependencies and responsive frames are checked; physical
iOS/Android smoke has not been performed.

## Development

Use a disposable development vault. Run `pnpm --dir plugin dev` and copy the three plugin files
there; reload the plugin after code changes and restart Obsidian after manifest changes.
`pnpm --dir plugin storybook` opens the connected production Shell with mocked external services
at port 6006. `pnpm --dir plugin check` covers generated types, lint, the mobile dependency graph
and unit tests. Run `pnpm --dir plugin test:storybook` for browser interactions and
`pnpm --dir plugin test:e2e:headless` for real Obsidian in Docker/Xvfb; `test:e2e` runs it with a window.
See [development](docs/development.md) and [release workflows](docs/releases.md) for details.

## Network and privacy

The plugin sends reading actions, settings and explicit requests to your configured backend via
REST and receives advisory SSE. The backend fetches configured Sources and uses the embedding/AI
providers you configure; optional AI generation is opt-in and may incur that provider's costs.
Reader images stay unloaded until you choose to load them. Save to vault writes only the requested
note and optional attachments through Obsidian; Remote-image snapshots retain network image URLs.
Opening an original link uses the system browser. There is no telemetry, plugin account or bundled
commercial service. Keep API access behind your chosen network/authentication boundary.

## Learn more

- [Use Topic Stream](docs/guides/using-topic-stream.md) — the current plugin workflow and backend API references.
- [Run and configure the server](docs/operations/configuration.md) — settings, model setup, and runtime roles.
- [Troubleshoot a problem](docs/operations/troubleshooting.md) or [back up your data](docs/operations/backup-and-restore.md).
- [Develop Topic Stream](docs/development.md) — local databases, builds, tests, and code conventions.
- [Browse the documentation](docs/README.md) — product behavior, architecture, API, and decisions.

[MIT license](LICENSE).
