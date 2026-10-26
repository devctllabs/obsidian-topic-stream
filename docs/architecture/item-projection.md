# Canonical Item projection delivery

PostgreSQL owns each Item's current projection revision and monotonic version. Every content,
provenance, feedback, correction, visibility, or Feed publication change advances that version and
coalesces the latest pending revision for every retained model in the same transaction, including
bulk publication/correction changes and Source-deletion provenance changes. Delivery selects only
Serving and the current non-cancelled transition. Projection snapshots read
content, current state/corrections, version, and the lifetime Feed publication fact consistently.

Bulk capture keeps one Feed-head/retrieval-state transaction. It assigns one revision per changed
Item and uses bounded 1,000-Item MERGE statements for current state and every retained destination.
Pending rows reference their scoped projection state; that state references the same Workspace's
canonical Item. This enforces ownership and cascades while avoiding repeated canonical-content
tuple locks when another model's pending row is created. Tables reserve 10% page space for updates.

Bulk Feed-display reconciliation uses 2,000-Item batches and an unnamed, typed statement so the
planner sees the actual Item array. Effective Topic membership is computed once, and unchanged display candidates are
filtered before upsert. Single-Item calls retain their ordinary execution mode. These changes keep
publication facts, correction semantics, generation/revision checks and the transaction boundary.

Qdrant stores the full canonical payload, including `projectionVersion`, `projectionRevision`,
`published`, `hidden`, `contentAt` (publication date with discovery fallback), and a text fingerprint.
The collection identifies the Embedding Model. An unchanged fingerprint permits conditional payload
replacement while preserving dense and BM25 vectors. Missing points or changed text use full
embedding/upsert. Full payload replacement clears removed feedback, corrections, and optional dates.

Both full and payload-only writes reject a point with a newer projection version. Full conditional
upsert may create a missing point; the same condition protects concurrent first writes once either
point exists. Payload replacement additionally requires the expected Item ID and text fingerprint.
Strong write ordering and a consistent read of Workspace, version, revision, fingerprint, and marker
fields precede acknowledgement. A conditional no-op with a newer or missing point is not acknowledged
as successful delivery. PostgreSQL completion matches the exact pending UUID revision, retained model generation,
collection binding and current execution lease. A collection identity check after writing prevents
an old-instance result from acknowledging replacement recovery.

Rebuilds write the existing 100-Item page sequentially, retaining per-Item cancellation and
no-progress deadlines, then acknowledge its successful model deliveries in one bounded transaction.
Partial model failures preserve healthy writes for acknowledgement. A repository batch accepts up
to 1,000 unique Item/model keys within one Workspace and returns one acceptance flag per input.
It checks the current canonical revision, pending revision, retained model generation, collection
binding and selected destination; Job-backed delivery also checks Job ID, lease owner, attempt,
expiry and cancellation. Reusing an owner in a later attempt never authorizes an earlier token.
SQL failure rolls back the whole batch; rejected entries remain pending. Direct delivery keeps
synchronous completion through the same single/batch fence. Successful external writes count as
useful rebuild progress, and cutover waits for the bounded page's acknowledgement to commit.

Serving completion atomically removes that pending revision and requests Topic preparation.
Batch completion coalesces one preparation request for all accepted Serving entries under the
same shared Feed head lock. Model activation serializes with the Workspace publication head and
retrieval state, and checks pending
transition projections after acquiring those locks. A publication racing cutover either leaves the
target pending or loses admission under its old Serving Model. Rebuild reads canonical snapshots,
including permanent publication facts.

Feed candidate filters require a versioned point and exclude published, Hidden, and direct Dislike
before dense, sparse, recent, and manual-Add limits. Feed date filters use `contentAt`. Personal
neighbour queries deliberately use their own Workspace/feedback scope; published feedback remains
evidence. Search, Digest, and calibration do not inherit the Feed candidate filter.

The clean-install schema tracks projection state from canonical writes. Existing canonical content
at initial Workspace setup requests asynchronous initialization in batches, without a startup-wide
pending insertion. Pending state survives interrupted delivery and is repaired by indexing/reindex
Jobs. Legacy unversioned points remain unavailable to Feed candidate queries until repaired.

`workspace_model_progress` retains seed cursor/completion for three models independently of Jobs.
Repeated inactive edits replace one pending revision per Item/model. Model-keyed access uses
`(workspace_id, embedding_model_id, item_id)`; global repair chooses eligible destinations before
inspecting bounded keys and excluding existing Jobs. Inactive million-key backlogs are not scanned
for ordinary repair. Discarded retention cleanup deletes at most 1000 pending keys per transaction.

Qdrant metadata and PostgreSQL share a collection incarnation UUID. Changed observations are reread
under a model-scoped advisory lock; normal matching snapshots require no coordination write.
Replacement invalidates old acknowledgements immediately and resets affected Workspace progress
under the canonical/retrieval locks. Selected-model recovery seeds full state in batches of 1000;
inactive models wait for selection. Shared collections are never deleted on Workspace eviction.
Read operations check the captured incarnation before and after querying, and discard changed-instance
results. See [ADR-0011](../adr/0011-retain-incremental-model-progress.md).

Conditional updates require the Qdrant API behavior originally tested against `qdrant/qdrant:v1.19`
and exercised by current integration tests against `qdrant/qdrant:v1.20`, with Go client `v1.18.2`:
absent-point upserts insert regardless of their filter; existing points apply
it atomically. Payload overwrite accepts a filter selector and preserves vectors. See the official
[conditional update and update-mode documentation](https://qdrant.tech/documentation/manage-data/points/#conditional-updates).

Verification uses isolated PostgreSQL schemas and Qdrant collections. It covers reversed deliveries,
concurrent first writes, optional payload clearing, legacy repair, pre-limit exclusion, published
personal evidence, publication lag, Source rediscovery, rebuild, and publication/cutover races.


Feed display reconciliation aggregates current corrections once per bounded Item batch before
combining them with publication membership. The Topic lookup is outside the per-Item subquery:
underestimated Workspace Topic counts must not multiply correction point lookups by every Topic.
Current visibility, Add/Wrong overlays, deterministic membership ordering and immutable publication
facts retain their existing shared-head transaction and rollback guarantees.
