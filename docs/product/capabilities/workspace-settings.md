# Workspace Settings and Embedding Models

Workspace Settings control non-secret behavior: Embedding Model selection, generation, summary
language and limits, editable task instructions, content retention, and ranking/retrieval controls.
Worker capacity belongs to deployment configuration. Credentials never belong in Workspace Settings.
These settings are available through the backend API; the current plugin only exposes its Server
URL connection setting, without the Workspace Settings interface.

Settings are versioned. Concurrent edits return a conflict instead of silently overwriting each
other. Changes become available to running replicas;
each operation uses one consistent settings snapshot.

The candidate work limit defaults to 50 and ranges from 1 to 100. It bounds prepared Topic lists
and the unique Feed Refresh selection independently of the default 25-Item display page.

## Embedding Models

An installation manages named Embedding Models with an endpoint, provider model, vector dimensions,
enabled state, and initialization status. Initialization probes a real embedding and prepares the
derived index. A model can remain registered after initialization fails, so its reason can be
inspected and initialization retried.

Models created through the admin API start disabled. Only ready, enabled models are selectable. Existing Desired and Serving
models remain visible even if disabled. The endpoint and display name may change; provider model,
weights, preprocessing semantics and dimensions identify an immutable model. Moving an endpoint
must preserve those semantics; changed weights require a new model ID even at the same URL. Models cannot be deleted in v0.1.

The installation creates an initial model from operator bootstrap configuration when absent.
Later environment changes preserve the saved registry. Initialization failure leaves database-backed
settings and administration available. See [server configuration](../../operations/configuration.md#embedding-model-lifecycle)
for bootstrap values, connectivity, and provider requirements.

## Desired and Serving models

Selecting a model changes the Desired model. The previous Serving model continues answering
retrieval until the new model is ready. Another selection supersedes older preparation immediately;
the latest selection wins. Unrelated settings remain editable, with the existing version checks.

Each Workspace retains progress for three models, including the Serving model and two most recently
selected alternatives, without an expiry time. Returning to a retained model processes only its
accumulated changes; unchanged Items do not undergo another complete initialization. Inactive
models record their latest changes without requesting embeddings. Returning after eviction requires
complete initialization again. Shared model indexes are preserved when Workspace progress is evicted.

If no model is serving, retrieval is unavailable while settings, status, model administration, and
Jobs remain usable. Cancelling a transition preserves the previous Serving model. Saved Feed
history remains readable during derived-index recovery. A missing or replaced model index is
recovered automatically when selected or observed by retrieval. Search reports temporary
unavailability until recovery is complete; the Job API exposes recovery work. An older snapshot
of the same index instance requires an explicit rebuild.

Retrieval status retains the latest operation for the current model selection, including its Job
and terminal outcome after cancellation, failure, cleanup or a server restart. A selection still
waiting for a Job is shown separately. Returning A → B → A creates a new selection identity;
an older A operation does not become current again merely because its model matches.

The server indicates whether the current failed or cancelled operation permits Retry. Retry
creates a new Job for the remaining retained work and preserves the earlier Job's history.
Superseded operations and an earlier Job already replaced by Retry cannot be retried; a stale
request returns a conflict. Selecting the same Settings value again does not perform recovery.

## Generation and preferences

Generation is disabled by default. Enabling it requires an HTTP(S) endpoint and non-empty summary
and rerank model names. Its failure can degrade runtime status without blocking database-backed work.

[Item AI summaries](item-ai-summaries.md) require an explicit Item request after opt-in. Summary
language, limits and task instructions control those attempts; enabling generation alone does not
start summaries from reading, Feed preparation or Digest Build.

Editable task instructions cannot replace system-owned safety boundaries, response validation,
timeouts, or retry policy. Preference Profile is a separate versioned resource.

For HTTP schemas and concurrency headers, see the [OpenAPI contract](../../../api/openapi/openapi.yaml).
For cross-process settings propagation and index delivery, see
[runtime roles](../../architecture/jobs-and-runtime-roles.md) and
[Item projection](../../architecture/item-projection.md).
