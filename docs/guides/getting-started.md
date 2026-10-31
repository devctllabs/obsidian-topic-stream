# Getting started

This guide runs the server on your computer and connects the current early Obsidian plugin to it.
The plugin publishes ready recommendations on first Feed visits and explicit Refresh, then keeps
older saved history available while you read.

## Before you start

- Install Docker with Compose v2 and Obsidian 1.8 or newer.
- Install Node.js and pnpm 11.25.0 to build the plugin from this checkout.
- Run a local OpenAI-compatible embedding service and load a model. The default is
  `qwen3-embedding:0.6b` with 1024 dimensions; generation is optional and disabled initially.

For example, if you already use Ollama, load the default model with:

```bash
ollama pull qwen3-embedding:0.6b
```

The provider must accept requests from the backend container. On Docker Desktop, address a host
provider through `host.docker.internal`. On native Linux, Compose maps that hostname to the host
gateway; the provider must listen on an interface reachable from that gateway. A host service
listening only on `127.0.0.1` may need its listening address changed. Keep an unauthenticated model
endpoint accessible only to your trusted machine or network.

## Configure the server

Clone this repository, enter its directory, and create your local configuration:

```bash
cp .env.example .env
```

Set the following entries. The example Workspace ID is a valid UUIDv4; you can generate your own
with `uuidgen`. Keep the chosen value stable across restarts to keep using the same Workspace.

```dotenv
TOPIC_STREAM_WORKSPACE_ID=f433587c-d050-44bb-a0d4-e64cdb8d5849
TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT=http://host.docker.internal:11434/v1
TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL=qwen3-embedding:0.6b
TOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS=1024
```

For another provider, set its actual model name and output dimensions. The endpoint is the API
base URL, including `/v1` when required, without `/embeddings`. The provider must work without an
API key: the current adapters do not send Authorization headers.

Compose uses the `TOPIC_STREAM_COMPOSE_*` values for its internal database, Qdrant, and HTTP
addresses. Leave the host-only `TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN` empty for this workflow.
The template's PostgreSQL credentials are local development defaults; change them together with the
Compose DSN before creating a new database volume if you need different values. Bootstrap variables
apply when the installation's initial model is first created; changing `.env` later does not overwrite
its saved configuration.

## Start the stack

```bash
mise run up
docker compose ps -a
curl --fail http://127.0.0.1:7331/api/v1/system/status
curl --fail http://127.0.0.1:7331/api/v1/retrieval-status
```

Compose uses the backend image selected in `.env`, starts PostgreSQL and Qdrant, waits for them,
applies database migrations in `init`, and starts `server`. The generated template defaults to the
public `latest` image. An exited `init` with code 0 is expected. Wait for the server health check to
pass; inspect [troubleshooting](../operations/troubleshooting.md) if it does not.

To build and run the current checkout instead of the public image:

```bash
mise run image-build
TOPIC_STREAM_BACKEND_IMAGE=topic-stream:local mise run up
```

The API is at `http://127.0.0.1:7331`. Databases stay inside the Compose network. Model initialization
must also succeed before recommendations and semantic search can work; system status alone does not
prove the embedding model is ready. Check `GET /api/v1/retrieval-status` for a non-null `servingModelId`.

## Install the Obsidian plugin

Build the plugin on the host:

```bash
pnpm install --frozen-lockfile
pnpm --dir plugin build
```

Create `.obsidian/plugins/topic-stream/` inside your vault and copy these three files into it:

- `plugin/main.js`
- `plugin/manifest.json`
- `plugin/styles.css`

In Obsidian's Community plugins settings, enable **Topic Stream**. Open its settings and keep
**Server URL** at `http://127.0.0.1:7331`, or enter a different device-local address.
**Test connection** checks the draft without applying it. **Apply** replaces the address in
open views. The plugin requires Obsidian 1.8.7 or later. Open the view through the ribbon icon or the
**Topic Stream: Open feed** command.

## Get your first Feed

1. Open the plugin’s Settings → Sources and create an RSS/Atom Source with **Add source**, or use the API's typed `config`
   below. Replace the example URL with your feed:

   ```bash
   curl --fail -X POST http://127.0.0.1:7331/api/v1/sources \
     -H 'Content-Type: application/json' \
     --data '{"name":"My source","config":{"kind":"rss","url":"https://example.com/feed.xml","allowPrivateNetwork":false},"enabled":true,"pollIntervalMinutes":60}'
   ```

   Use `"kind":"atom"` in `config` for an Atom Source.
2. Create a Topic with a short Include description, such as “Practical distributed systems”.
   Leave the Source scope empty to consider all Sources; add Exclude criteria only when needed.
3. Plugin creation requests the first Sync separately after saving the Source. If you created it
   through the API, use **Sync now** or **Sync all** in Settings. Allow ingestion, indexing and Topic
   preparation to finish; a failed Sync keeps the Source and offers a deliberate retry.
4. Return to **Feed** and choose **Refresh**. The first visit to All Topics or an enabled Topic
   also checks for recommendations once. These requests use ready candidates without waiting for
   preparation. Short or empty results are valid; wait and deliberately Refresh again when needed.
   Failed requests keep saved history and do not retry publication automatically.
5. Use the Topic switcher and **All / Unread**, load older pages, and open an Item title to read its
   canonical Markdown in Reader. Images load only after you activate a placeholder. Its **…** menu
   provides feedback, the original link, Reading State, Topic corrections and global Hide. Search
   browses the all-time archive; optional **AI summary** always requires an explicit action.
6. Open **Digests** to read saved editions or **Build** from a saved Digest Definition, including
   Manual only ones. Use **Tasks** for background status and explicit Refresh, Cancel or eligible
   Retry. Opening an entire edition does not mark its Items read.

Continue with [Using Topic Stream](using-topic-stream.md) for the reading workflow.

## Stop and restart

```bash
mise run down
mise run up
```

Named volumes preserve your data. `docker compose down` also preserves them; `down -v` deletes
them. Use the [backup guide](../operations/backup-and-restore.md) before intentional data removal.

For another device, `localhost` refers to that device. Configure access through a trusted private
network using the [connection guidance](../operations/configuration.md#connecting-from-another-device).

Use **Save to vault** in an Item or whole-Digest menu to create an independent Markdown snapshot.
Choose its destination and content/image modes explicitly; existing files are never replaced.
See [Using Topic Stream](using-topic-stream.md#save-an-independent-vault-snapshot) for image limits
and mixed Full Digest consent.
