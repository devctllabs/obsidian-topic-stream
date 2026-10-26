# Retain incremental progress across Embedding Model selections

A Workspace retains synchronization progress for three Embedding Models: its protected Serving Model
and two most recently selected alternatives, without a TTL. Canonical changes coalesce into the
latest pending revision for every retained model, while only Serving and the current transition
receive embedding/index delivery. This trades bounded additional PostgreSQL state for avoiding a
million-Item rebuild on every return; continuously embedding inactive models and retaining an
unbounded revision log were rejected.

Progress belongs to the Workspace/model, independently of a Job attempt. The latest Desired Model
supersedes older work; selection and execution generations fence late seed, acknowledgement,
cleanup and cutover. Healthy preparation has no total duration limit, but five minutes without
useful progress ends an attempt under the existing three-attempt retry policy. Explicit cancellation
requires deliberate retry or another selection and does not silently restart the same request.

A durable Qdrant collection incarnation is bound to PostgreSQL progress. Replacement triggers full,
bounded selected-model recovery; eviction never deletes a collection shared by other Workspaces.
Same-incarnation snapshot rollback requires explicit rebuild. Model IDs identify immutable weights
and preprocessing semantics; endpoint relocation preserves that identity only for the same model.

Each retrieval operation captures authoritative serving/model/endpoint state from PostgreSQL and
checks collection identity around its read. Independent runtime roles remain correct without
notifications, and PostgreSQL-backed Feed history remains available during derived-index recovery.
This amends the transition lifecycle in [ADR-0007](0007-share-derived-indexes-by-embedding-model.md).
