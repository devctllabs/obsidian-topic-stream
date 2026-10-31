# Search, Reader, and notifications

These are backend/API capabilities. The Obsidian plugin provides archive Search and Reader,
including saved-edition content and view-owned advisory SSE. See the
[current Obsidian experience](obsidian-feed.md) for client controls and refresh boundaries.

Search supports lexical, semantic, and hybrid matching with optional Topic, Source, Item-kind, and
publication-date filters. An empty query browses the newest archive content. Non-empty queries use
bounded personal reordering and diversity selection without Feed/Digest freshness bonuses.

Published, old, hidden, and Disliked Items remain searchable. Search returns 20 Items by default and
up to 100 in one query/filter session. Its result traversal is independent of a Feed snapshot.
Topic corrections made from Search can change a published Item's Topic visibility at its original
position without republishing it or recomputing its recommendation explanation.

The content API provides canonical cleaned Markdown for Reader clients when available. Merely
retrieving an Item does not fetch remote images; image requests are validated and bounded.
Explicit Vault Snapshot writes are client-owned and available through Save to vault; Reader stays
read-only. The
backend does not synchronize files in the vault; see the
[vault boundary decision](../decisions/0004-explicit-vault-snapshots.md).

[Item AI summaries](item-ai-summaries.md) are separate annotations requested explicitly for
supported content. Reading content or an ordinary excerpt never starts generation.

Notifications signal Feed invalidation, a new Digest Issue, or a Job status change. They are advisory:
reconnecting clients refetch authoritative state rather than replaying missed events. A notification
does not insert cards into a loaded Feed. Slow clients cannot block background work.

The plugin shares one Fetch/ReadableStream connection per open view, cancels it on close,
endpoint replacement and unload, and reopens it in the owning window after pop-out migration.
Retries back off to 30 seconds; failed streaming retains explicit REST recovery and loaded content.
Desktop main/pop-out delivery is verified; the browser-only adapter has no Node/Electron path,
but physical mobile streaming is not part of the v0.1 evidence. Exact-origin
[CORS configuration](../../operations/configuration.md#browser-access-and-sse) is required.

See the [ranking pipeline](../../architecture/ranking-pipeline.md) for Search scoring differences,
[content ingestion and Reader delivery](../../architecture/content-ingestion-and-reader.md) for content/image
mechanics, and [runtime roles](../../architecture/jobs-and-runtime-roles.md#live-settings-and-advisories)
for cross-process notification delivery. The [OpenAPI contract](../../../api/openapi/openapi.yaml)
defines HTTP paths, pagination fields, and event payloads.
