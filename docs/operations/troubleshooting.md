# Troubleshooting

## The stack does not start

```bash
docker compose ps -a
docker compose logs --tail=100 init server postgres qdrant
```

- A missing `.env` or Workspace ID means setup is incomplete. Copy the template and set a stable UUIDv4.
- `init` exiting with code 0 is normal; it waits for dependencies and applies migrations.
- A Qdrant readiness timeout leaves `init` failed and prevents server startup. Fix Qdrant, then run `mise run up` again.
- A failed migration needs inspection with [database commands](database.md). Do not delete volumes to repair it.
- Port 7331 may already belong to a host development process or another stack.

## Obsidian cannot connect

Check the same URL outside Obsidian:

```bash
curl --fail http://127.0.0.1:7331/api/v1/system/status
```

Verify Server URL, that the plugin is enabled, and that the server container is running.
On another device, `127.0.0.1` refers to that device, not your server. Follow the
[connection guidance](configuration.md#connecting-from-another-device).

## The Feed is empty

Check that a Source is enabled and has recent content, then request Sync. Allow fetching, indexing,
and Topic preparation to complete before Refresh. Preparation coalesces changes for up to one minute.
A Refresh uses ready candidates and does not wait for a cold Topic.

The current plugin's **Refresh** button only rereads saved history. Use the
[Feed publication request](../guides/getting-started.md#get-your-first-feed) to admit new
recommendations, then reread the view. A first visit in the current plugin does not publish.

Check the synchronization Job's reason if it fails. Relax overly narrow Source/kind scope or
Include/Exclude criteria. Feed admission considers the preceding 30 days; Search can find older
content. A short or empty result is valid when nothing new qualifies.

Check `GET /api/v1/retrieval-status`: a null `servingModelId` means model initialization or transition
has not finished. System status alone does not check embedding readiness.

## Embedding model initialization failed

1. Confirm the provider is running, its model is loaded, and its vector size matches the configured dimensions.
2. Use a container-reachable endpoint. `localhost` inside the backend container points to that container.
3. Inspect the registry and retry the failed model after fixing its dependency:

   ```bash
   curl --fail http://127.0.0.1:7331/api/v1/admin/embedding-models
   curl --fail -X POST http://127.0.0.1:7331/api/v1/admin/embedding-models/MODEL_ID/initialize
   ```

Replace `MODEL_ID` with the returned ID. A retry can return a model whose status is still `failed`;
inspect its initialization status and safe failure reason. Restarting the server also retries an
unready bootstrap model. Changing bootstrap env values does not edit a model already in PostgreSQL;
use the admin API to update its endpoint or create a replacement for another model/dimension pair.

If initialization or collection preparation reports an incompatible ACORN version, check Qdrant:
the configured ACORN policy requires Qdrant 1.x version 1.16 or newer. Upgrade the server or disable
ACORN with `TOPIC_STREAM_QDRANT_ACORN_ENABLED=false` and restore query defaults with
`TOPIC_STREAM_QDRANT_HNSW_EF=0`, then restart the affected roles and retry the failed operation.
Keep shared policy settings consistent across roles. See the
[search policy and rollback instructions](configuration.md#qdrant-dense-search-policy) before
changing a stored collection threshold; setting its environment value to zero does not reset it.

For indexing failures caused by provider context/input limits, check the model's allocated window
and the rejected input. The embedding batch byte budget is not a token limit: an Item larger than
that grouping budget is sent alone without truncation. Increasing the batch budget does not
increase the provider's context. See [provider context configuration](configuration.md#ollama-embedding-context).
After fixing the provider configuration, retry the failed Job. Changes to embedding weights or
preprocessing require a new model identity rather than silently changing an existing model.

## A Job failed or is still running

Inspect `GET /api/v1/jobs` or `GET /api/v1/jobs/{jobId}` for status and a safe reason. The current
plugin has no Task Center. Automatic retries are bounded; a terminal `dead` Job
requires fixing the cause before manual retry. Cancellation of running work is cooperative.
Database-backed Job state is authoritative after reconnecting.

For `reindex-workspace`, `totalItems` can be null during preparation. Equal processed/total counters
finish the initial cohort; catch-up or activation may still be running. Check `Job.status` for
the final outcome. Advisory `jobChanged` events prompt a REST refetch and are not a heartbeat;
see [Job progress](../product/capabilities/durable-jobs.md).

For model-transition recovery, read `GET /api/v1/retrieval-status` and inspect `latestTransition`.
`waiting` with a null `jobId` means no Job has been admitted yet. A terminal operation remains
available there even when the active `transitionJobId` has been cleared. Inspect its Job's safe
reason, fix the cause, and, when `canRetry` is true, call `POST /api/v1/jobs/{jobId}/retry` using
`latestTransition.jobId`. The response identifies a new Job for remaining work. Refetch status after
`409`: the selection may have changed or another Retry may already have replaced that Job.
Patching the same model selection does not restart cancelled work. Historical Jobs with no
`modelTransition` metadata have no verified public model association.

## Qdrant was lost or restored empty

Preserve PostgreSQL and keep the configured embedding provider and workers running. Missing or
recreated collections request automatic recovery when selected or observed by retrieval. Check
`GET /api/v1/retrieval-status` and the Job API; Search returns `503 retrieval_unavailable` while
recovery is incomplete, and saved Feed remains readable.

If an older snapshot preserved the same collection incarnation, request an explicit rebuild after
Desired and Serving match:

```bash
docker compose exec server topic-stream reindex
```

Wait for the automatic recovery or explicit rebuild Job to succeed. Retry cancelled/dead work
explicitly after fixing its cause. Follow
[backup and restore](backup-and-restore.md) for full recovery and verification.
