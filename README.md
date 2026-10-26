# Topic Stream

A personal reading stream inside Obsidian, shaped by the sources and topics you choose.

Topic Stream brings articles, papers, videos, and repositories into one place. Describe what you
want to follow, read a focused Feed, and use feedback to improve future recommendations.
The server runs on your own machine. The current Obsidian plugin provides an early Feed view;
the backend exposes the broader reading and retrieval capabilities through its API.

## What you can do

In the current plugin you can create Topics, request Sync all, browse already-published Feed
history, open original links, and give Like/Dislike feedback with optional comments. Its Source
form still sends the old request shape; create Sources through the backend API using the guide.
Its **Refresh** button rereads saved history. To publish recommendations, use the backend
[Feed Refresh API](docs/guides/getting-started.md#get-your-first-feed).

The backend also supports arXiv, GitHub Trending and YouTube Sources, Topic corrections, archive
Search, cleaned content and image delivery, explicit Item AI summaries, and scheduled Digests.
Search, Reader, Digest, Task Center and AI-summary controls are not yet available in the plugin.
The plugin does not yet write Vault Snapshots. See the
[current Obsidian capabilities](docs/product/capabilities/obsidian-feed.md).

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
   [first Feed steps](docs/guides/getting-started.md#get-your-first-feed), including API publication.

The [getting started guide](docs/guides/getting-started.md) walks through configuration, model setup,
and your first Feed. The default deployment exposes the API only on localhost; it has no built-in authentication.

## Learn more

- [Use Topic Stream](docs/guides/using-topic-stream.md) — the current plugin workflow and backend API references.
- [Run and configure the server](docs/operations/configuration.md) — settings, model setup, and runtime roles.
- [Troubleshoot a problem](docs/operations/troubleshooting.md) or [back up your data](docs/operations/backup-and-restore.md).
- [Develop Topic Stream](docs/development.md) — local databases, builds, tests, and code conventions.
- [Browse the documentation](docs/README.md) — product behavior, architecture, API, and decisions.

[MIT license](LICENSE).
