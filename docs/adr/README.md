# Architectural decisions

ADRs preserve the reasons behind technical choices. An amendment or superseding decision explains
what changed; historical wording is not a current operations guide. Use the
[architecture references](../README.md#product-and-engineering-references) for current mechanics.

| ADR | Status |
| --- | --- |
| [0001 — Daemon with PostgreSQL and Qdrant](0001-single-daemon-with-postgres-and-qdrant.md) | Accepted; mandatory role coupling replaced by 0008 |
| [0002 — Contract-first REST and advisory SSE](0002-contract-first-rest-with-advisory-sse.md) | Accepted; cross-process transport amended by 0008 |
| [0003 — Fixed ranking stages](0003-fixed-ranking-stages-with-versioned-components.md) | Accepted; Feed persistence constraint replaced by 0010 |
| [0004 — PostgreSQL-leased Jobs](0004-postgres-leased-jobs.md) | Accepted |
| [0005 — Backend content and vault boundary](0005-backend-content-and-obsidian-vault-boundary.md) | Accepted |
| [0006 — RFC 9457 problem boundary](0006-rfc9457-problem-type-boundary.md) | Accepted |
| [0007 — Derived indexes shared by model](0007-share-derived-indexes-by-embedding-model.md) | Accepted |
| [0008 — Independent roles and PostgreSQL advisories](0008-independent-runtime-roles-and-postgres-advisories.md) | Accepted; later Feed preparation adds a fifth pool |
| [0009 — Persisted Feed publication](0009-persisted-feed-publication.md) | Superseded by 0010 |
| [0010 — Shared Feed and bounded preparation](0010-shared-feed-and-bounded-candidate-preparation.md) | Accepted; published history has no display expiry |
| [0011 — Incremental model switching and recovery](0011-retain-incremental-model-progress.md) | Accepted |
