# Item AI summaries

An Item AI Summary is an optional generated annotation, separate from an ordinary excerpt.
The backend provides this capability through its API; Obsidian summary controls and Vault Snapshot
writes are not yet available in the current plugin.

## Explicit requests and supported content

Generation requires opt-in Workspace generation settings and an explicit request for a particular
Item. Supported input is a persisted Article body or Repository README with recorded provenance.
An abstract, repository description, title alone, Video or text of unknown origin does not qualify.
Requesting a summary neither fetches a remote article nor accepts replacement text.

Canonical requests use `POST /api/v1/items/{itemId}/ai-summary`; saved Digest Item requests use
`POST /api/v1/digest-issues/{issueId}/items/{itemId}/ai-summary`. Both accept asynchronous work and
return a Job reference. Availability and state distinguish unsupported input, not requested,
pending, ready and failed results. Repeated requests reuse active work or a matching ready result;
a failed request requires explicit Job Retry rather than another automatic generation attempt.

The result belongs to one Summary Source Version and the relevant Summary configuration.
Each actual attempt captures its settings once. Changing the Embedding Model or giving feedback
does not invalidate an otherwise matching summary. The configured Summary model generates text;
the Embedding Model remains responsible for retrieval vectors.

## Saved versions and reuse

Digest requests summarize the saved Item version. An existing matching annotation can accompany
publication; a later result may fill an absent annotation for that same version. Once ready, a
Digest annotation stays unchanged when canonical text or Summary settings change. It does not
alter Issue facts, complete/degraded status, selection, order, consumption or the success watermark,
and its completion does not announce another Digest Issue.

Ready results remain readable after generation is disabled; new requests and Retry require it to
be enabled. `GET /api/v1/items/{itemId}/ai-summary` is read-only and can select an exact
`sourceVersion` for a client capturing a Snapshot. Reading a version never generates, retries or
waits for a summary. Later completion never edits a previously saved Vault Snapshot.

Ordinary reading, Feed preparation, Feed Refresh, Digest Build and reconnect do not start Summary
generation. The accepted [explicit-summary decision](../decisions/0010-explicit-item-ai-summaries.md)
explains this boundary. See [Item summary architecture](../../architecture/item-summaries.md) for
generation and publication mechanics and the [OpenAPI contract](../../../api/openapi/openapi.yaml)
for exact availability/status values, request schemas and errors.
