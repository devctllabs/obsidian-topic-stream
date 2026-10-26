# Item AI summaries

The backend exposes explicit asynchronous Item summaries. Canonical requests use
`POST /api/v1/items/{itemId}/ai-summary`; a saved version uses
`POST /api/v1/digest-issues/{issueId}/items/{itemId}/ai-summary`. Both return an accepted Job.
`GET /api/v1/items/{itemId}/ai-summary` is read-only and supports an exact `sourceVersion` for
Snapshot capture. Digest detail includes separate `aiSummary` states for its saved Items.
The Obsidian controls and Vault writes are delivered by the UI backlog.

Only persisted `article_body` and `repository_readme` origins with actual text qualify. Ingestion
records positive provenance; migration leaves older content `unknown`. A request neither fetches
external URLs nor accepts replacement text. README input excludes the repository description.
Input identity hashes the actual title, text and origin and is scoped to Workspace and Item.

The `summaries` service owns generation meaning, settings capture, hierarchy and reuse policy.
The PostgreSQL capability owns admission, durable Retry bindings and fenced publication.
The outbound generation adapter owns the OpenAI-compatible protocol, immutable system/data envelope
and strict output schema. HTTP and lazy Workspace worker graphs register the same Summary owner.

Admission locks one input version, aliases active work and reuses a matching ready result. It does
not freeze settings in Job arguments. A failed request remains failed until explicit Job Retry;
each source Retry permanently resolves to one successor. Each actual attempt captures current
Summary settings once and checks committed Job recovery and exact semantic cache before model work.
The fingerprint includes endpoint, Summary model, prompts, language, limits and algorithm/schema
versions. Embedding Model, feedback and unrelated settings revisions do not invalidate it.

Short input uses one final call. Longer Markdown is split at paragraph/heading boundaries, produces
bounded chunk facts and reduces only while accumulated facts exceed the input budget. Limits count
Unicode characters, not provider tokens. Every reduction must shrink; the ceiling is 128 calls and
eight reduction levels. An attempt has a three-minute deadline and each call at most one minute.
Output is validated independently against `maxSummaryCharacters` (200–4,000; default 800).
The default input budget is 48,000 characters. No model is downloaded automatically.

Model calls run outside database transactions. Publication locks the input then the actual Job,
checks Workspace, Item, source version, kind, queue, owner, attempt, database-clock lease and
cancellation, and commits one immutable result with its pointers. Recovery after a lost Job
acknowledgement returns the committed annotation without another provider call. Ready results stay
readable after generation is disabled; new requests and Retry still require permission.

Digest publication records eligible Item-version markers separately from the immutable snapshot.
Unrequested markers retain metadata with a null body; explicit admission captures model input.
Shared input locks serialize marker creation and Summary publication. Digest transactions sort
Item identities before locking shared inputs. Ready annotations are attached immediately when
available; later publication fills only absent annotations of the same saved version. An already
ready Digest annotation never changes, even when canonical text or Summary settings change.
Issue facts, ordinary excerpts, status, consumption and watermark are independent of annotations.
Summary completion emits ordinary Job state notifications and no `digestReady` event.

Digest detail hydrates all annotation bindings with at most three queries rather than one query
per Item. Reading, Feed preparation, Refresh, Digest Build and reconnect do not admit Summary work.
Snapshot callers read an exact version without generating, retrying or waiting for completion.

Deterministic HTTP/provider and PostgreSQL tests are separate from the explicitly enabled live
provider test. [Backend check instructions](../development.md#backend-checks) cover ordinary and
integration suites; [product rules](../product/capabilities/item-ai-summaries.md) define the
request, reuse and saved-version behavior those tests protect.
