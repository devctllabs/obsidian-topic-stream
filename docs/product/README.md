# Product documentation

These references describe Topic Stream behavior. Start with the
[user guides](../README.md) for installation and everyday tasks. Capability documents state current
scenarios, rules, and constraints without duplicating internal implementation. Accepted product
decision records preserve the reasons behind behavior that would otherwise be surprising.

Proposals, open questions, implementation specifications, and tickets belong in the local issue tracker until
their behavior is implemented. Architectural mechanics and trade-offs belong in `docs/architecture/`
and `docs/adr/`.

## Capabilities

- [Sources and ingestion](capabilities/sources-and-ingestion.md)
- [Topics](capabilities/topics.md)
- [Ranking and personalization](capabilities/ranking.md)
- [Feed and feedback](capabilities/feed-and-feedback.md)
- [Search, reader content, and advisory events](capabilities/search-reader-and-events.md)
- [Item AI summaries](capabilities/item-ai-summaries.md)
- [Digests and schedules](capabilities/digests-and-schedules.md)
- [Durable Jobs](capabilities/durable-jobs.md)
- [Workspace Settings](capabilities/workspace-settings.md)
- [Portability and benchmarking](capabilities/portability-and-benchmarking.md)
- [Runtime status](capabilities/runtime-status.md)
- [Obsidian experience](capabilities/obsidian-feed.md)

## Current product decisions

PDR means Product Decision Record. Records preserve rationale; capability pages describe behavior.

- [Keep current feedback separate from reading and visibility state](decisions/0002-current-feedback-with-independent-state.md)
- [Publish immutable Topic-sectioned Digest Issues](decisions/0003-immutable-topic-sectioned-digests.md)
- [Save explicit independent Vault Snapshots](decisions/0004-explicit-vault-snapshots.md)
- [Keep mobile feature parity with a device-local connection](decisions/0005-mobile-parity-with-device-local-connection.md)
- [Keep AI secrets outside the product API](decisions/0006-keep-ai-secrets-out-of-product-api.md)
- [Share Feed history and prepare Topic candidates in the background](decisions/0008-shared-feed-with-prepared-topic-candidates.md)
  (display-expiry rule replaced by PDR-0009).
- [Keep published Feed history available through pagination](decisions/0009-browse-feed-history-without-display-expiry.md)
- [Request Item AI summaries explicitly and preserve saved annotations](decisions/0010-explicit-item-ai-summaries.md)

## Superseded product decisions

- [PDR-0001 — Keep Feed stable until explicit Refresh](decisions/0001-stable-feed-until-refresh.md)
  → PDR-0007 → PDR-0008.
- [PDR-0007 — Preserve Feed order with one-time publication](decisions/0007-stable-feed-publication.md)
  → PDR-0008; display expiry later replaced by PDR-0009.
