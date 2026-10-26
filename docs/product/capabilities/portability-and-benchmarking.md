# Portability and benchmarking

## Product archive

`GET /api/v1/export` returns a versioned `topic-stream/v2` JSON archive containing non-secret Workspace Settings, the selected Embedding Model descriptor, the Preference Profile, Sources, Topics, current feedback keyed by canonical Item URL, and Digest Definitions. The descriptor includes name, endpoint, provider model, and dimensions but no credentials or Qdrant collection name. The archive deliberately excludes raw feed responses, canonical Item content, Jobs, Feed Publications, candidate caches, and Qdrant vectors.

`POST /api/v1/import` accepts that archive only for an empty target Workspace and returns a durable import Job. Import is transactional and safe to retry with the same archive identity. It reuses an identical Embedding Model registry entry or creates one, initializes its shared collection, and rebuilds the imported Workspace before making it serve retrieval. A same-name model with a different endpoint, provider model, or dimensions produces a typed conflict. A different archive is rejected once the Workspace contains imported state. Imported Sources and Digest Definitions are disabled, `lastSyncedAt` and `nextRunAt` are cleared, and the user must explicitly enable schedules after reviewing them.

Because Items are intentionally absent from the archive, feedback is staged by canonical URL. When a Source later rediscovers the corresponding Item, PostgreSQL attaches the staged current feedback atomically during Item upsert.

The product archive is for moving Topic Stream configuration and preference state. It is not a full disaster-recovery backup; the full PostgreSQL backup procedure is documented in [backup and restore](../../operations/backup-and-restore.md).

## Offline benchmark

`topic-stream bench` evaluates the current Workspace feedback with a deterministic temporal train/test split. The earliest feedback is training data and the remainder is test data. It compares BM25, dense, hybrid RRF, and hybrid plus personalization using Precision@10, Recall@50, MRR, and NDCG@10.

The default split is 80 percent. `--since` accepts an RFC 3339 lower bound and `--train-percent` accepts 1 through 99. The default output is a terminal table; `--json` emits only the versioned JSON report.

```bash
topic-stream bench
topic-stream bench --since 2026-01-01T00:00:00Z --train-percent 70
topic-stream bench --json > benchmark.json
```

The command is read-only with respect to production state. It reads the existing PostgreSQL state and active Qdrant generation without creating a Workspace, reconciling settings, enqueueing Jobs, or ensuring Qdrant collections. Embedding the benchmark query may still call the configured OpenAI-compatible embedding endpoint.

A portable import starts a new Workspace Feed history; it does not recreate published order from
another Workspace. Full PostgreSQL backup/restore preserves the shared head, permanent publication
facts, and once-per-Workspace/Item uniqueness.
