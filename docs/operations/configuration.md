# Server configuration

## Choose a runtime

| Workflow | Command | Application runs |
| --- | --- | --- |
| Published/default stack | `mise run up` | In Docker with the image selected by `TOPIC_STREAM_BACKEND_IMAGE` |
| Local source image | `mise run image-build` then `TOPIC_STREAM_BACKEND_IMAGE=topic-stream:local mise run up` | Current checkout in Docker with PostgreSQL and Qdrant |
| Host development | `mise run deps-up`, `mise run migrate-up`, `mise run start` | PostgreSQL/Qdrant in Docker and backend on the host |

These are independent Compose projects with separate volumes. The development file publishes
PostgreSQL on 5432 and Qdrant on 6333/6334, bound to localhost. The full stack publishes only the
API on `127.0.0.1:7331` and preserves the existing `topic-stream` project's database volume names.
The generated `.env.example` selects the public `latest` image by default; replace it with a pinned
version tag or digest for reproducible operation.

## Environment files

Copy the generated `.env.example` to `.env` and edit the copy. Keep it private; it is ignored by Git
and excluded from the Docker build context. Devctl owns the template; see [development](../development.md#configuration-generation).

Compose reads `.env` for interpolation and passes it into `server` and `init` through `env_file`.
Explicit Compose `environment` entries override those container values with
`TOPIC_STREAM_COMPOSE_DB_PRIMARY_POSTGRES_DSN`, `TOPIC_STREAM_COMPOSE_QDRANT_URL`, and
`TOPIC_STREAM_COMPOSE_HTTP_ADDR`. The full stack therefore uses `postgres:5432`, Qdrant gRPC at
`http://qdrant:6334`, and an HTTP listener on `0.0.0.0:7331` without putting those values in YAML.
Run `mise run up` after environment changes to recreate affected services; `restart` alone
does not reload their environment. Avoid sharing rendered Compose output: it can contain secrets.

For a host process, configuration precedence is process environment → optional `.env` in the current
directory → generated defaults. Host processes use `127.0.0.1` for the development databases.

| Variable | Meaning / default |
| --- | --- |
| `TOPIC_STREAM_WORKSPACE_ID` | Required stable UUIDv4 for HTTP or schedulers |
| `TOPIC_STREAM_BACKEND_IMAGE` | Backend image for the full Compose stack; default `ghcr.io/ethernity/obsidian-topic-stream:latest` |
| `TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN` | Required host-process DSN; empty in the generated template |
| `TOPIC_STREAM_COMPOSE_DB_PRIMARY_POSTGRES_DSN` | Internal Compose DSN; local default uses `postgres:5432` |
| `TOPIC_STREAM_POSTGRES_DB` | PostgreSQL database name for both Compose projects; default `topic_stream` |
| `TOPIC_STREAM_POSTGRES_USER` | PostgreSQL user for both Compose projects; default `topic_stream` |
| `TOPIC_STREAM_POSTGRES_PASSWORD` | Local development password for both Compose projects; default `topic_stream` |
| `TOPIC_STREAM_HTTP_ADDR` | Host listener, default `127.0.0.1:7331` |
| `TOPIC_STREAM_QDRANT_URL` | gRPC endpoint, default `http://127.0.0.1:6334` |
| `TOPIC_STREAM_COMPOSE_HTTP_ADDR` | Container HTTP listener, default `0.0.0.0:7331` |
| `TOPIC_STREAM_COMPOSE_QDRANT_URL` | Container Qdrant gRPC endpoint, default `http://qdrant:6334` |
| `TOPIC_STREAM_BOOTSTRAP_EMBEDDING_ENDPOINT` | Initial model API base URL, default `http://localhost:11434/v1` |
| `TOPIC_STREAM_BOOTSTRAP_EMBEDDING_MODEL` | Initial provider model, default `qwen3-embedding:0.6b` |
| `TOPIC_STREAM_BOOTSTRAP_EMBEDDING_DIMENSIONS` | Initial vector size, default `1024`, allowed `1..65536` |
| `TOPIC_STREAM_LOG_LEVEL` | Default `info` |
| `TOPIC_STREAM_SHUTDOWN_TIMEOUT` | Default `10s`; allow a longer container stop grace period |

The template also includes role and worker-capacity variables. The generated `HEALTH_*` variables
currently have no runtime listener; use `/api/v1/system/status` on the main API port.

The host and Compose DSNs are intentionally separate because `127.0.0.1` means the host while
`postgres` means the Compose network. The PostgreSQL credentials are local development defaults,
not production secrets. If they are changed, update both DSNs before creating a new database volume;
the PostgreSQL image does not change credentials in an existing volume automatically.

## Embedding model lifecycle

Start the external provider and load the configured model before the first server startup.
In Compose, use `http://host.docker.internal:11434/v1` for a provider on the Docker host. Native Linux
requires the provider to listen on an interface reachable from the host gateway; Compose supplies
the hostname mapping. Providers on another machine need their actual reachable URL.

Bootstrap configuration creates the installation's **Default embedding model** only when absent.
It never overwrites the name, endpoint, provider model, dimensions, or state already stored in
PostgreSQL. All replicas of one installation should use consistent bootstrap values.

Manage an existing model through the trusted admin API. Its provider model and dimensions are
immutable: create a new model when either changes. Model ID also fixes the actual weights and
preprocessing semantics, which a dimensions probe cannot verify. Relocate an endpoint only for the
same semantics; use a new model ID for changed weights even at an unchanged URL. Enable a ready
model, then select it in Workspace Settings. The previous Serving model remains available until
synchronization completes; another selection supersedes older work.

Progress is retained for three models per Workspace without TTL. A retained return catches up only
its latest changes; an evicted model needs full initialization. Inactive models receive no background
embedding delivery. Missing/recreated collections request automatic selected-model recovery, visible
in Task Center. Cancelled/dead requests require explicit retry or another selection. Five minutes
without preparation progress retries an attempt; healthy large runs have no total duration limit.

Initialization failure leaves database-backed APIs usable. After fixing the endpoint or Qdrant,
retry initialization or restart the server. There is no periodic automatic retry of a failed initial
model. See [troubleshooting](troubleshooting.md#embedding-model-initialization-failed).

Embedding and optional generation endpoints must be local OpenAI-compatible services that accept
requests without Authorization headers. API-key forwarding is not implemented. Generation is
disabled by default; its endpoint and summary/rerank models belong in Workspace Settings.
Operator secrets never belong in product settings or portable exports.

### Background embedding batches

| Variable | Default | Allowed values |
| --- | --- | --- |
| `TOPIC_STREAM_EMBEDDING_BATCH_SIZE` | `8` | `1..128` input texts |
| `TOPIC_STREAM_EMBEDDING_BATCH_INPUT_BYTES` | `1048576` | `1024..4194304` encoded input bytes per batch |
| `TOPIC_STREAM_EMBEDDING_CONCURRENCY` | `1` | `1..8` active background requests per endpoint/process |

Rebuild groups compatible model inputs from bounded pages. The same endpoint request bound also
applies to ordinary background Item delivery across Workspaces in that process. Separate worker
processes have independent limits; this is not an installation-wide quota. Query embeddings keep
their existing single-input path. Set batch size/concurrency to `1/1` for compatibility.

The byte budget counts JSON-encoded input strings; the client separately caps encoded batch
requests at 4 MiB and responses at 16 MiB. A text larger than the grouping budget is sent alone
through the existing single-text path, without truncation. Provider input rejections can split a
batch boundedly; outage/rate-limit errors stop repeated calls for that model within the current
page. Index writes still require current revision, generation, collection and Job fences before ACK.

These settings change scheduling, not model context or embedding semantics. Ollama owns its
allocated context window; the application does not infer tokens from this byte budget. Initial
defaults are conservative and measured tuning is recorded separately; larger batches/concurrency
are not a promise of higher throughput or lower Search latency.

## Qdrant dense search policy

| Variable | Default | Meaning / allowed values |
| --- | --- | --- |
| `TOPIC_STREAM_QDRANT_FULL_SCAN_THRESHOLD_KB` | `100000` | Qdrant vector KB budget **per segment**; `0` or `1..2147483647`; `0` leaves the existing collection threshold unchanged |
| `TOPIC_STREAM_QDRANT_HNSW_EF` | `256` | Dense graph search budget, `0` or `1..65536`; `0` leaves the server query default |
| `TOPIC_STREAM_QDRANT_ACORN_ENABLED` | `true` | Allow conditional ACORN traversal for dense queries |
| `TOPIC_STREAM_QDRANT_ACORN_MAX_SELECTIVITY_PERCENT` | `40` | Matching-fraction threshold in percent, `1..100` when ACORN is enabled; `0..100` when disabled; `40` becomes request value `0.4` |

Normal dense queries omit `exact`, allowing Qdrant to choose direct scoring or HNSW per segment
using payload-index cardinality estimates. ACORN runs only where its eligibility condition is met;
enabling it does not force it for every query. There is no new per-query Count, SQL count, application
cardinality cache or Source-presence switch. Sparse/BM25 requests and exact stored-vector reads retain
their existing behavior; personal-feedback dense batch queries use the same policy.

These defaults were selected on synthetic 1024-dimensional vectors at Qdrant 2 CPU / 6 GiB. Both fresh
million-point confirmations reached at least 99.95% mean recall per tested filter/calibration/held-out
set. This is not semantic relevance, a guarantee for all future filters/rebuilds, or an API latency
SLO. A 100000 KB threshold corresponds to roughly 25000 float32 vectors of 1024 dimensions **per
segment**; other model dimensions yield different counts. It is not a whole-collection Item cutoff.
Structural HNSW values, indexing threshold, target segment count, storage, quantization, replication,
model identity and dimensions are preserved. Indexing remains the operator's online configuration;
benchmark batch256 writes were not adopted as production fenced delivery.

ACORN requires a compatible Qdrant 1.x server version >=1.16; the measured server is 1.19.1 and SDK
v1.18.2. Compatibility is checked lazily on first configured use and successful checks are cached
per index instance. Unsupported versions fail clearly; transient health failures remain retryable.
Constructing the application does not wait on optional Qdrant I/O for database-backed routes.

For new collections, initialization includes the selected HNSW threshold. For existing collections,
normal collection `Ensure` during retrieval/model initialization validates the schema and required
indexes, updates only a differing owned threshold and verifies the saved setting. An unchanged value
does not issue a configuration update. Ordinary query methods do not create or configure collections;
read-only readers such as `bench` never reconcile them. Initialize/ensure the collection through the
normal runtime before treating a read-only benchmark as a measurement of this collection policy.

For an already-ready model, the existing trusted admin operation
`POST /api/v1/admin/embedding-models/{modelId}/initialize` runs collection initialization again,
including the normal provider probe; it does not re-embed the archive when the collection and
incarnation are intact. A normal retrieval `Ensure` also reconciles the setting. Restart alone does
not eagerly update every retained collection, and read-only Feed/bench readers do not apply settings.

Changing the threshold replaced HNSW segments in measurements. The setting can be saved while the
optimizer is still rebuilding, and queries remain available during that period. Do not interpret
successful application, model readiness or a single green observation as proof of measured steady
latency/recall. Observe optimizer progress and stable completion before assessing results. No provider
re-embedding, point deletion or Collection Incarnation change is required. All roles sharing an
Embedding Model collection must use consistent operator policy; restarting only some roles with
conflicting thresholds can repeatedly change the shared setting. Environment changes require process
restart; Compose deployments require service recreation as described above.

Before rollout, record the existing collection threshold and any named-vector override through the
Qdrant collection configuration endpoint. A conflicting `dense` named-vector threshold is rejected
instead of silently ignoring the operator policy. Changing a shared global threshold in a collection
with additional dense vectors is also rejected; explicitly reconcile those custom settings rather
than allowing this application's policy to change unrelated vectors.

For rollback, restore the **recorded previous** `TOPIC_STREAM_QDRANT_FULL_SCAN_THRESHOLD_KB`, set
`TOPIC_STREAM_QDRANT_HNSW_EF=0` and `TOPIC_STREAM_QDRANT_ACORN_ENABLED=false`, then restart all roles
consistently and ensure the collection. This restores default query parameters; restoring a threshold
may cause another HNSW replacement. Setting the threshold override to `0` only stops future
reconciliation: it does **not** restore a previous threshold or disable HNSW. All-zero policy values
are also valid for programmatic legacy callers. Generated environment defaults enable the measured
policy for normal CLI deployments.

## Ollama embedding context

The model's advertised maximum window and the window allocated by Ollama are different values.
Inspect the running model through `/api/ps` after an embedding request; `/api/show` describes model
capabilities and configured parameters. The batch byte budget above is not a token-window setting.

With the OpenAI-compatible `/v1/embeddings` API, configure context on the Ollama model rather than
adding a nonstandard request field. For the installed Qwen3 embedding weights, a separate local
configuration can use this Modelfile:

```text
FROM qwen3-embedding:0.6b
PARAMETER num_ctx 32768
```

Create a local name with `ollama create topic-stream-qwen3-embedding-32k:latest -f Modelfile`, then
use that provider model name in the appropriate Embedding Model descriptor. A fixture selecting this
name does not change existing Workspace selections. Larger windows can increase allocated memory;
record the actual running window and resource usage when comparing capacity. Window selection does
not compact Markdown or change the model's vector dimensions. See [Ollama context configuration](https://github.com/ollama/ollama/blob/main/docs/api/openai-compatibility.mdx#setting-the-local-context-size).

## Runtime roles and capacity

HTTP, schedulers, and workers are enabled by default in one process. Separate replicas can select
them independently. For example, a host worker-only process uses:

```bash
export TOPIC_STREAM_HTTP_SERVER_ENABLED=false
export TOPIC_STREAM_SCHEDULERS_ENABLED=false
export TOPIC_STREAM_WORKERS_ENABLED=true
go run ./cmd/topic-stream serve
```

Workers serve all Workspaces and do not require a selected Workspace ID. Pool sizes and retry
ownership are documented in [Jobs and runtime roles](../architecture/jobs-and-runtime-roles.md).

## Connecting from another device

The default full stack publishes only to localhost. For a phone or another computer, expose the API
through a trusted private network or an externally authenticated TLS proxy and set that device's
Obsidian Server URL accordingly. A container's `0.0.0.0` listener does not itself change the host
port's localhost binding; adjust deployment networking deliberately. Topic Stream has no built-in
authentication, including on its admin API. Keep PostgreSQL and Qdrant private.
