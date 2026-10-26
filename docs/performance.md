# Local performance and provider checks

Use Python 3.11 or newer, Go, and Docker. Run these commands from the repository root.
The maintained runner creates disposable services; it never reuses the test database or Qdrant
URLs in your environment. Performance and real-provider tests are explicit local checks and are
excluded from ordinary integration selection.

```bash
python3 tools/performance.py --help
python3 tools/performance.py qdrant-search --dry-run
python3 tools/performance.py qdrant-search --smoke
python3 tools/performance.py sql-feed-plan
```

`--dry-run` validates and prints the selected work without creating services or files. The Qdrant
smoke uses 1024 artificial vectors. Live smokes use a local protocol stub and three Items; they
verify wiring and reporting without calling a model:

```bash
python3 tools/performance.py live-index --smoke
python3 tools/performance.py live-lifecycle --smoke
python3 tools/performance.py live-summary --smoke
```

## Profiles and selection

| Profile | Maintained check |
| --- | --- |
| `sql-http` | Full SQL fixture, Feed/Jobs HTTP reads, startup backlog and display-write costs |
| `sql-capture` | Full-fixture change capture, fresh/repeated keys, idle and mixed HTTP load |
| `sql-ack` | Full-fixture projection delivery acknowledgement |
| `sql-model-switch` | Retained-model switching and eviction |
| `sql-model-delta` | Retained-model change coalescing and incremental capture |
| `sql-repair` | Bounded projection repair and progress |
| `sql-candidates` | Candidate preparation and bounded access paths |
| `sql-digests` | Digest history access bounds |
| `sql-feed-adversarial` | Clustered Feed exclusions under load |
| `sql-feed-plan` | Correction work after Workspace Topic growth; EXPLAIN, 2000 Items |
| `qdrant-search` | Artificial-vector search latency and exact-reference recall |
| `qdrant-ensure` | Existing collection Ensure cost; no large corpus |
| `qdrant-checks` | Vector generation, recall math, identity/filter and optimizer readiness guards |
| `math-checks` | Report percentile math; no Docker |
| `live-index` | Initial indexing of 500 Items through an explicit embedding provider |
| `live-lifecycle` | Bounded indexing, model return, delta and collection recovery |
| `live-summary` | English/Russian single-call and hierarchical Summary generation |
| `live-observer-checks` | Batch-input versus request-count accounting using a local stub |

SQL and synthetic checks select `integration,performance`; provider checks select
`integration,live`. Each invocation runs one named test or a small helper group with uncached
results and `-p=1`. Missing required live inputs fail explicitly. Selecting `integration` alone
does not invoke a model, even when live environment variables are present.

Large SQL fixtures include one million Items and related rows and can consume several GiB of
disk. Use `sql-feed-plan` for a bounded plan smoke. The runner does not make these large SQL
fixtures smaller when testing its own lifecycle.

## Resource ownership

PostgreSQL 17 Alpine receives 2 CPU and 4 GiB, with 1 GiB shared memory,
`shared_buffers=1GB`, `max_wal_size=2GB`, `checkpoint_timeout=30min` and preloaded
`pg_stat_statements` tracking all statements. Qdrant 1.20 receives 2 CPU and 6 GiB. Profiles
create only the services their package harness needs; SQL checks in the dependency-wiring
package also require a Qdrant health companion. The Go process and external model server run
on the host and are outside these limits. Record their actual configuration when comparing runs.

Containers expose ephemeral ports on `127.0.0.1`. A unique run label identifies ownership.
Cleanup checks that label and removes only owned containers and their anonymous volumes,
including after a failed setup or test. Ownership mismatches and cleanup failures fail the run.
Synthetic restart and live recovery guards also verify the owned Qdrant resource limits and
ports before acting. Existing application containers, collections and database schemas are not
runner inputs. Interrupt with Ctrl-C to let its cleanup finish.

## Qdrant search

```bash
python3 tools/performance.py qdrant-search
python3 tools/performance.py qdrant-search --distribution balanced
python3 tools/performance.py qdrant-search --points 1000000
```

The default is 100,000 deterministic 1024-dimensional artificial document vectors, with one
Workspace owning 80% of them. One million points requires an explicit flag. Online indexing
uses batches of 256, strong ordering with `wait=true`, two target segments, a 1024 KiB indexing
threshold, and no quantization. Search exercises automatic planning with full-scan threshold
100,000 KiB, `ef=256`, and ACORN enabled at a 40% selectivity threshold. It observes optimizer
readiness, restarts the owned server, and measures two rounds at concurrency one and four.

The report compares filtered search against exact nearest neighbors for separate calibration
and held-out query sets. It records result identity, filter validity, incomplete responses,
p50/p95/p99 and recall. The accepted mean recall floor is 95%; 99% is the preferred target.
These finite artificial-vector results measure index/search behavior. They do not establish
semantic relevance or an HTTP API SLO. A 1024-point smoke verifies the harness and cannot
validate 100k/1M approximate-search quality.

## Real provider prerequisites

Load an embedding model before running a 500-Item check. Supply the embedding server root;
the test appends `/v1/embeddings`. Vector dimensions must match the installed model:

```bash
python3 tools/performance.py live-index \
  --endpoint http://127.0.0.1:11434 --model qwen3-embedding --dimensions 1024
python3 tools/performance.py live-lifecycle \
  --endpoint http://127.0.0.1:11434 --model qwen3-embedding --dimensions 1024
```

The live runner uses batches of eight and one concurrent request. A model's token context window
is independent of the client's batch-input byte limit. Configure the provider context explicitly;
see [Ollama embedding context](operations/configuration.md#ollama-embedding-context). The model
name above is an example; use the exact installed provider model. This profile never expands to
one million real embedding requests.

Summary accepts an OpenAI-compatible base URL and a generation model with structured output:

```bash
python3 tools/performance.py live-summary \
  --endpoint http://127.0.0.1:11434/v1 --model installed-generation-model
```

Endpoints must omit credentials, query strings and fragments. This local harness targets
providers accessible without an API key. An unrelated live environment variable cannot change a
synthetic profile into a provider run.

## Reports and comparisons

Default output is `.artifacts/performance/<run-id>/<profile>/`; override it with `--output`.
`run.json` records the validated plan, outcome, Go command elapsed time and cleanup failures.
`go-test.jsonl` captures test output and package timings. Capacity profiles additionally write
`report.json` and any scenario-specific reports; plan-only/helper checks keep their evidence in
the Go log. Output files are private to the current user. Keep real-provider logs private too.

New percentile reports use nearest-rank quantiles: sort the samples and select
`ceil(percentile * count) - 1`. Small samples do not establish tail latency. Historical reports
retain their original quantile convention and are not recalculated by this runner. Compare only
equivalent data, source versions, service versions/resources, cache state and load. Go command
elapsed time excludes Docker preparation and cleanup; nested test durations and concurrently
executed package durations are not additive wall time.
