# Keep published Feed history available through pagination

Status: Accepted
Date: 2026-10-18
Supersedes: The display-expiry rule of [PDR-0008](0008-shared-feed-with-prepared-topic-candidates.md).

Published Feed cards remain available indefinitely, with new Batches prepended and older cards
retrieved through cursor pagination. The first page defaults to 25 Items; reading deeper does not
rerank or republish history. The 30-day content-age window applies only to initial admission,
keeping new recommendations recent without making familiar cards disappear during later reading.

## Consequences

- There is no publication display TTL. Hide and explicit Topic corrections still affect visibility
  without changing publication time/order; Add and Unhide can restore old cards at their positions.
- Lifetime publication deduplication and canonical retention remain unchanged. Search also finds
  Items that were never published; Feed history does not replace the full searchable archive.
- Candidate age eligibility and cleanup remain bounded. Background preparation does not accumulate
  candidates or publish cards automatically.

Implementation note (2026-10-21): The backend/API is delivered. Current behavior is described in
[Feed and feedback](../capabilities/feed-and-feedback.md); technical mechanics are in the
[ranking pipeline](../../architecture/ranking-pipeline.md). The accepted decision and its
historical alternatives remain unchanged.
