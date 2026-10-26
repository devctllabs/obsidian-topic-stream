# Durable Jobs

Synchronization, generation, model transitions, indexing, and Feed preparation run as durable Jobs.
The Job API lists work for the current Workspace and exposes status, attempts, timestamps, duration,
cancellation state, and safe diagnostic reasons. Internal arguments are not exposed. The current
Obsidian plugin does not yet provide Task Center controls.

Jobs move through `queued`, `running`, `retry_wait`, `succeeded`, `dead`, or `cancelled`. Temporary
failures retry within a bounded policy; permanent failures stop. Fetch, retrieval, and Feed preparation
allow five attempts; generation and maintenance allow three. Repeated requests deduplicate active
work, while a later deliberate retry can create a new Job after the earlier one is terminal.

`reindex-workspace` Jobs may expose `progress` in list and detail responses: `phase`,
`processedItems`, nullable `totalItems`, and `updatedAt`. The total is unknown during initial
`preparing`. Once captured, it describes a fixed initial set of required Items; later arrivals do
not enlarge it. A retained model captures its remaining work, rather than the whole archive.
Accepted current deliveries count an Item once, and automatic retries of the same Job retain its
counters. A new Job captures its own remaining set.

Phases are `preparing`, `indexing`, `catching_up`, `activating`, and `completed`.
`processedItems == totalItems` finishes the initial set only; newer changes and activation may
still be pending. `Job.status` determines success, failure or cancellation.

Digest Build has persistent Retry identity: the source `generate-digest` Job is bound to one
successor. Repeating Retry returns that same successor, including after completion or restart.
Manual Build, scheduled occurrences, and explicit Retry coalesce into one active generation Job
per Definition. Other Job kinds retain their ordinary replacement policy.

Queued and retry-waiting Jobs cancel immediately. Cancelling running work records a request and
waits for cooperative execution to stop. A committed cancellation takes precedence over a concurrent
successful result. Unsupported work versions terminate with an inspectable reason.

A Digest Issue already committed before cancellation or acknowledgement loss remains saved.
Recovered execution completes from that result without another ranking or Issue notification.
Explicit Retry of a dead/cancelled generation Job with a saved Issue returns `409`; it does not
create a successor. This result guard supplements the persistent Digest Retry identity above.

Clients reconcile authoritative Job state through REST on opening, reconnecting, a relevant
notification, or explicit refresh. Advisory SSE `jobChanged` carries a Job ID and has no replay
guarantee. Durable progress notifications are limited to once per second per execution when work
advances; phase and Job status changes can notify immediately. This is not a periodic heartbeat,
and a missed notification can leave a loaded view stale until the next refetch.

A cancelled Embedding Model transition preserves the previous Serving model while leaving the
Desired selection unchanged. Retrying resumes retained target progress safely. A new model
selection supersedes older preparation. Five minutes without preparation progress ends an attempt
and follows the bounded retry policy; healthy initial indexing has no fixed total duration limit.
A cancelled or exhausted request does not automatically restart itself.

Verified model-transition Jobs also expose `modelTransition`: target model, selection identity,
Job, outcome, whether this is the current operation, and whether Retry is allowed. Retrieval
status retains the latest operation for the current selection after terminal cleanup and restart;
its active `transitionJobId` can already be null. A pending selection without an admitted Job has
status `waiting` and a null Job ID. Older Jobs without verified linkage omit this metadata;
clients must not infer their model from the current selection or private arguments.

Retry eligibility is rechecked when admitting the replacement. Retry of a superseded operation
or a Job already replaced by Retry returns `409`, including a later return to the same model.
Concurrent requests admit one replacement. These model-operation checks do not change other Job
kinds' Retry policies or add the Digest-specific persistent successor identity.

Background Topic preparation never publishes Feed cards. It prepares bounded lists for a later
Refresh; restarts and missed notifications do not discard the durable preparation request.

Queue ownership, concurrency, leases, shutdown, and reconciliation are documented in
[Jobs and runtime roles](../../architecture/jobs-and-runtime-roles.md).
