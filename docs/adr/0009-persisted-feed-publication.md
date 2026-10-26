# Persist Feed Publications and separate publication from page reads

Status: Superseded by [ADR-0010](0010-shared-feed-and-bounded-candidate-preparation.md)
Date: 2026-10-03

To implement [PDR-0007](../product/decisions/0007-stable-feed-publication.md), PostgreSQL will own
independent Workspace-scoped Feeds and atomically appended Feed Publications with immutable order
and ranking explanation facts. Page reads will use that saved order and keyset cursors bound to a
Feed head; a separate Refresh command will rank only unpublished candidates and prepend a Batch.
This replaces timestamp-only reranking and process-local snapshots with product state that survives
replica changes and restarts, without introducing a generic ranking-history or event-log system.

## Consequences

- A Feed is identified by its Workspace and All Topics or Topic scope. Publication uniqueness is
  enforced per Feed and Item, and concurrent publishers allocate order under the same locked Feed
  head. Ranking happens before the short atomic publication transaction; commit-time deduplication
  may make a Batch short without triggering another ranking pass.
- Candidate exclusion is applied before dense, BM25, recent, and manual-Add retrieval. PostgreSQL
  loads already-published IDs intersecting the current admission window rather than transferring
  the entire lifetime publication ledger to Qdrant.
- `GET /api/v1/feed` reads Publications. `POST /api/v1/feed/refresh` publishes a Batch and returns a
  `FeedPage`. The opaque snapshot fixes the Feed head, and a descending publication ordinal
  replaces ranked offset pagination. Neither page reads nor SSE reconnects publish automatically.
- Item content, feedback, Reading State, and visibility remain canonical live state. Publication
  time, order, matching Topic IDs, score, explanation, and ranking versions are saved facts.
  Explicit visibility/correction masking and expiry never recalculate the remaining order.
- Visibility expires 30 days after publication at the time of a REST read. The publication fact
  remains available to deduplication; full Item content is not copied into Publications.
- Source balancing occurs while building a Batch, not while reading pages. A per-page Source
  distribution is therefore not guaranteed across Batch boundaries or different page sizes.
- Existing ephemeral Feed order cannot be migrated. The first Refresh creates durable history;
  old page tokens are rejected and clients start a new traversal. Portable configuration archives
  keep their existing boundary; full PostgreSQL backups preserve Feed state.

This supersedes only the Feed persistence constraint in
[ADR-0003](0003-fixed-ranking-stages-with-versioned-components.md). Search and Digest ranking remain
ephemeral except for their existing owned product artifacts. This record preserves the superseded
design; [ADR-0010](0010-shared-feed-and-bounded-candidate-preparation.md) describes its replacement.
