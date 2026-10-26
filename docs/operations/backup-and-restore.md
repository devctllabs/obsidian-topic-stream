# Backup and restore

PostgreSQL is the authoritative full-system backup. Qdrant contains rebuildable derived vectors and is intentionally excluded. The PostgreSQL dump includes the shared Feed head, lifetime publication facts, original order/time, and bounded preparation state.

## Using the full Compose stack

Stop the server to quiesce writes, then dump PostgreSQL without installing PostgreSQL tools on the host:

```bash
docker compose stop server
docker compose exec -T postgres sh -c 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom' > topic-stream.dump
docker compose up -d
```

Keep the private `.env` and the application release version alongside your backup, stored securely.
The dump contains content and preferences; `.env` contains installation configuration and may contain secrets.

Restore into an unused Compose project so the existing database remains available. Stop the original
server to release port 7331. The new project's PostgreSQL database starts empty:

```bash
docker compose stop server
docker compose -p topic-stream-restored up -d postgres qdrant
docker compose -p topic-stream-restored exec -T postgres sh -c 'until pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"; do sleep 1; done'
docker compose -p topic-stream-restored exec -T postgres sh -c 'pg_restore --exit-on-error -U "$POSTGRES_USER" -d "$POSTGRES_DB"' < topic-stream.dump
docker compose -p topic-stream-restored up -d --wait
docker compose -p topic-stream-restored exec server topic-stream reindex
```

Use the original Workspace ID and the same reachable embedding provider in `.env`. The new `init`
applies pending migrations. Keep its worker running and wait for the reindex Job to succeed before
treating retrieval as recovered. Keep using `-p topic-stream-restored` for this recovered installation.
If switching back, stop the recovered server before starting the original one.

## Using a host process or external PostgreSQL

### Back up PostgreSQL

Quiesce writes by stopping the Topic Stream daemon, then create a custom-format dump with a `pg_dump` version compatible with the server:

```bash
pg_dump --format=custom --file=topic-stream.dump "$TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN"
```

Keep the dump and the Topic Stream release version together. Protect the dump as private data: it contains extracted content, preferences, feedback, and any non-secret endpoint configuration stored in Workspace Settings.

### Restore PostgreSQL

Restore into a new empty database rather than overwriting the running database. Keep the daemon stopped while restoring.

```bash
createdb topic_stream_restored
export TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN="postgres://${TOPIC_STREAM_POSTGRES_USER}:${TOPIC_STREAM_POSTGRES_PASSWORD}@127.0.0.1:5432/topic_stream_restored?sslmode=disable"
pg_restore --exit-on-error --dbname="$TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN" topic-stream.dump
```

Use an application version compatible with the backup's migration history. The current
pre-release eight-area baseline supports clean installation only: it cannot upgrade a backup from
the earlier unpublished migration set. Restore such a backup with its matching application
revision; do not force migration metadata to make it appear current. See
[migration compatibility](database.md).

Point `TOPIC_STREAM_DB_PRIMARY_POSTGRES_DSN` at the restored database and apply pending migrations
from a compatible binary:

```bash
topic-stream db status
topic-stream db up
```

Start HTTP and workers against the restored PostgreSQL database. A fresh Qdrant instance is the
appropriate target after rolling PostgreSQL back: existing points can have versions newer than the
restored canonical state. Missing or recreated collections are detected by incarnation and request
automatic recovery of the selected model.

Use an explicit complete rebuild when restoring an older Qdrant snapshot with the same incarnation,
or when repairing missing/corrupt points without collection replacement:

```bash
topic-stream serve
# In another terminal with the same environment:
topic-stream reindex
```

`reindex` targets the current Serving Model and requires the Desired Model to match it; allow an
outstanding model selection to finish first. It enqueues an idempotent durable Job. The running daemon rebuilds the model-derived Workspace copy from canonical Items and publication facts, then atomically activates the target after pending projections complete. Use the Task Center API or plugin to wait for the returned Job to reach `succeeded` before treating semantic search and recommendations as fully restored.

If the Job becomes `dead`, retain PostgreSQL, correct the embedding or Qdrant dependency, and retry it through the Task Center. Do not edit retrieval generation rows or job payloads manually.

## Verify

Verify all of the following after restore:

- `GET /api/v1/system/status` reports PostgreSQL ready and Qdrant ready;
- the automatic recovery or explicit reindex Job succeeded and retrieval status has a ready Serving model;
- Feed and Search return expected Items;
- Sources and Digest schedules have the intended enabled state;
- a fresh PostgreSQL backup can be created from the restored database.

Saved Feed reads remain PostgreSQL-backed during Qdrant recovery. Rebuilding reproduces publication
markers and preserves the shared Feed order and lifetime uniqueness; it does not republish the
archive. A portable `topic-stream/v2` export intentionally excludes this history and is not a substitute
for full PostgreSQL backup. An isolated integration test exercises actual `pg_dump`/`pg_restore` of
the complete application schema and verifies immutable ordinals, timestamps, Batch IDs, and duplicate
rejection after reopening the restored store.
