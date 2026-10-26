# Digests and schedules

A Digest Definition stores one IANA timezone, one structured recurrence, and an ordered list of Topic relationships. Each relationship has its own `itemLimit` from 1 through 15. Definitions are versioned to prevent concurrent edits from silently overwriting each other. Retained immutable issues survive definition deletion. These capabilities are available through the backend API; the current Obsidian plugin has no Digest controls.

Recurrence supports daily, a weekly subset of ISO weekdays, and monthly day-of-month, nth-weekday, or last-day rules. The scheduler chooses the first valid instant when a local wall-clock time does not exist and runs only the first occurrence when a wall-clock time repeats. It enqueues one catch-up generation when several intervals were missed, advances directly to the next occurrence after the current time, and does not move the calendar for a manual Build.

Generation is a durable `generate-digest` Job. Initial generation considers the preceding 30 days; later generation starts at the last successful issue and also excludes every Item already consumed by that Definition, including Items consumed by manual builds. Each Topic is ranked independently with its configured limit. A duplicate is assigned to the section where it has the better within-Topic rank, and another candidate backfills the losing section when available. Sections remain short rather than admitting irrelevant Items.

Manual Build requires a UUID `Idempotency-Key`. Each Workspace/Definition request identity is
permanently bound to its original Job, including after completion, restart, or Definition deletion.
Different identities share any active generation Job for that Definition; a fresh identity after
terminal completion can start new work. Admission and the binding commit together. Disabling
automatic scheduling (`enabled=false`) still allows manual Build and leaves `nextRunAt` null.
Each actual attempt captures the current Definition and Workspace Settings at its start; repeated
clicks preserve the admitted Job's arguments and do not change an attempt already executing.

A due occurrence shares an active manual, scheduled, or Retry Job and advances the calendar in the
same transaction. It does not queue a follow-up issue. A stale scheduler observation admits no work;
coalescing preserves the initiating Job's `scheduledFor`. Explicit Retry of a dead/cancelled Build
permanently binds that source Job to one successor, possibly an already active Job. Repeating Retry
returns that successor even after it completes; retrying a failed successor creates the next link.
Deleting a Definition disables its schedule and atomically cancels queued/retry-waiting Builds or
requests cancellation of running Builds. Saved issues and existing request/successor replays remain
available; new Builds and new Retry successors are rejected after deletion.

An issue stores immutable canonical Item snapshots, ordinary excerpts, explanation signals, pipeline versions, Topic names and revisions, schedule time, and per-Topic failures. A partial result follows the [Generation Queue retry policy](durable-jobs.md). Only the final failed attempt may publish a degraded issue, and only when at least one Topic branch succeeded; a total failure leaves the Job dead and creates no issue.

[Item AI summaries](item-ai-summaries.md) are separate optional annotations. Build does not request
or wait for them. A later explicit result may fill an absent annotation for the saved Item version;
it never replaces ready text or changes immutable Issue facts or status.

`GET /api/v1/digest-issues` returns pages of history summaries rather than full Item snapshots.
The default `limit` is 20 and the maximum is 100; an opaque cursor continues the same filtered
traversal, optionally scoped by `digestId`. `GET /api/v1/digest-issues/{issueId}` returns the full
saved Issue with its Item snapshots and separate AI-summary states.

Issue detail and history summaries expose `generationJobId`. One generation Job can commit one
immutable Issue. A new publication checks the actual running Job, lease owner, attempt, lease expiry,
cancellation and Definition lifecycle under Definition-before-Job locks. The Issue, consumed Item set
and success watermark commit together. Ranking and content assembly happen before these locks.
Recovery first finds an already committed result by Job, even after Definition deletion, and completes
without reranking, rewriting the result or announcing another Issue. A dead/cancelled Job with a
saved result returns `409 Job not retryable`; a new Build identity is needed for another Issue.

After an issue commits, the no-replay event service emits `digestReady` with that issue ID. REST remains authoritative for issue contents and badge reconstruction.

The shared Topic stages and the Digest-only LLM, MMR, exploration, and section-assembly stages are
described in [Ranking and filtering pipeline](../../architecture/ranking-pipeline.md).
