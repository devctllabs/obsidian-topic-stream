# Store one Feed history and prepare bounded Topic candidates asynchronously

Status: Accepted
Date: 2026-10-09
Amended: 2026-10-21 for [PDR-0009](../product/decisions/0009-browse-feed-history-without-display-expiry.md).
Supersedes: [ADR-0009](0009-persisted-feed-publication.md)

PostgreSQL will own one Workspace Feed head, lifetime-unique Item publications, and replaceable
bounded candidate lists per Topic. Durable Jobs prepare Topic retrieval and membership facts;
Refresh uses ready candidates and one common personal pass before a short publication transaction.
Qdrant projects publication state for exclusion before retrieval limits, while PostgreSQL remains
the authority for deduplication. This implements
[PDR-0008](../product/decisions/0008-shared-feed-with-prepared-topic-candidates.md) without
independent Topic histories, unbounded seen-ID request filters, or background publication.

## Consequences

- Canonical Items and Source Entries retain their existing ownership. Candidates and publications
  reference Items rather than duplicate content. One publication saves time, ordinal, original
  matched Topics, admission score, explanation, and component/model versions.
- Candidate preparation retains dense/BM25 retrieval, Include/Exclude gates, bounded manual Add,
  and local RRF. All selection balances the ready Topic lists, deduplicates before its limit,
  combines local ranks cheaply, and personalizes the common pool once. Search and Digest retain
  their pipelines and do not inherit publication exclusion.
- The publication transaction locks the Workspace head, rechecks current eligibility and
  uniqueness, allocates immutable order, removes published IDs from every Topic list, and records
  pending projection/refill work. Preparation commits use the same head coordination and a Job
  lease/input-version fence, preventing delayed workers from restoring published candidates.
- A monotonic Item projection version protects Qdrant updates from older deliveries. All writes
  include publication state; unchanged text/model does not require another embedding. Model
  transitions and rebuilds reproduce the marker from PostgreSQL. Projection lag is tolerated by
  indexed PostgreSQL checks against only proposed candidate IDs.
- `GET /api/v1/feed` reads saved order without Qdrant. `POST /api/v1/feed/refresh` returns
  `200 FeedPage` without waiting for preparation. Tokens bind Workspace, view filter, common head,
  and ordinal; later publications cannot enter an existing traversal.
- Saved membership is overlaid with current explicit Topic corrections at read time. Manual Add
  changes view membership without changing publication identity, score, ordinal, time.
  Hide applies to All and every Topic view; reads filter before the page limit. Publications have
  no display TTL; only candidate admission is age-limited.
- Preparation is process-global bounded work with at most one active preparation Job per
  Workspace. Durable dirty state, limited chunks, and reconciliation recover missed enqueue,
  changes during execution, lease loss, and restarts without using successful work as a retry.

This continues to supersede only the Feed persistence constraint of
[ADR-0003](0003-fixed-ranking-stages-with-versioned-components.md). Generic ranking history and
indirect-rejection caching remain excluded. Current implementation details are documented in
[the ranking pipeline](../architecture/ranking-pipeline.md),
[item projection](../architecture/item-projection.md), and
[Jobs and runtime roles](../architecture/jobs-and-runtime-roles.md).

Implementation note (2026-10-21): The shared Feed backend/API is delivered. This decision and its
historical alternatives remain unchanged.
