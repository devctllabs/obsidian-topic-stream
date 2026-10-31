# Obsidian experience

The plugin opens one reading view through the ribbon or **Open feed** command. Its responsive shell
has Feed, Digests and Search tabs plus Settings and Tasks controls. Feed publishes ready
recommendations and reads saved history. Search browses the archive and opens the same Reader;
Digests opens saved editions, and Tasks inspects background work. Wide leaves show list and detail together.
Narrow leaves restore list scroll and focus on Back. Reading section, selection and positions stay
local to the backend connection.

Native plugin settings contain device-local Server URL, a draft connection test and explicit Apply,
plus vault-scoped default All/Unread and Snapshot preferences. Typing or testing does not replace
open clients. Applying the URL clears the previous connection’s caches and suppresses late results.
Unresolved Build identities and model outcome watches are scoped to endpoint and public Workspace
identity and remain device-local.

Settings manages all supported Source adapters, full Topic scope/criteria, ordered Digest
Definitions and recurrence, non-secret Workspace Settings, global Prefer/Avoid and backend status.
Source creation orchestrates a separate initial Sync without a preflight probe; failed Sync retains
the Source and offers a Sync-only retry. Per-Source Job associations last only for the current view
session. Dirty editors offer Save/Discard/Stay, and versioned resources preserve drafts through
explicit conflict review. Deletion confirms the resource and retains saved Digest Issues.

New Digest Definitions default to Manual only, retaining a valid calendar and timezone while
scheduling is off. Manual Build remains available. Uncertain acknowledgements reuse the persisted
request identity rather than creating another deliberate Build. Saving a Definition never builds.

Workspace Settings distinguishes Desired and Serving Embedding Models, confirms replacement of
active preparation, and displays authoritative phase/count without inferring completion from a full
bar. Public current-operation references govern Cancel/Retry and recovery. Device-local watches
deduplicate user-initiated terminal Notices; explicit Refresh is available before advisory event
transport is connected. AI generation is opt-in and separate from retrieval. Saving settings never
generates text. Prefer/Avoid guides optional Digest reranking without adding another Feed/Search
ranking promise. Credentials remain operator-owned.

The searchable Topic switcher offers **All Topics** and enabled Topics. Each selection attempts
publication once on its first visit in an opened view. **Refresh** deliberately checks for ready
recommendations again. Failed or incomplete attempts never retry publication automatically;
saved content remains usable. Returning to a visited selection restores its loaded pages,
selection and scroll. Older pages keep the original snapshot and publication order. A successful
Refresh with new publications replaces that traversal and moves to its new top; membership-only
changes preserve the reading position.

The vault-scoped **All / Unread** filter applies to loaded pages. A displayed Item becoming read
stays dimmed in the current Unread traversal until a successful Refresh reapplies the filter.
Older history stays accessible when the loaded pages contain no unread Items. Opening a title
opens the reading pane and marks the Item read; **Open original** and explicit **Mark as read**
also update Reading State. **Why this item** shows the full explanation and metadata without
marking it read. Reader displays the saved canonical Markdown inside the view without creating a
vault file. Code blocks remain plain source text and do not invoke language-specific Obsidian
handlers. Empty/unavailable content retains the title, metadata, excerpt and navigation; an
explicit content check or rendering retry provides recovery where applicable.

The Item's **…** menu offers **Useful** and **Not for me**. Choosing one saves the rating immediately
and opens a small blurred panel for an optional feedback note. Only **Send note** (the paper-plane
button) sends the text; closing the panel keeps the rating and preserves an unsent draft for that
opened Item. **Add note** / **Edit note** in the menu reopens it. Sending failures keep the draft and
the previously saved rating. The row and reading pane share the current rating and unsent draft.
**Not for me** retains the Item and offers a transient **Wrong topic** action in the note panel.

**Add to Topic**, **Wrong topic** and **Use automatic matching** correct Topic membership at the
original publication position. **Hide from all Feeds** changes global visibility. These actions
do not publish or rerank Items. A saved feedback/correction write awaiting retrieval projection
stays saved and reports that pending state; it is not automatically submitted again.

Empty Search browses newest-first across all time. A query uses hybrid retrieval; Topic, Source
and UTC publication-date filters apply only after explicit submission. Each session loads 20
results per page, up to 100; changing a query or filters starts a new session. Hidden Items remain
discoverable, with a Hidden label. Search does not infer Topic rejection from missing membership.
Feedback, corrections and Reading State are shared with Feed; correcting a Topic rereads Search
with its existing filters. Retrieval preparation failures stay contextual to Search and preserve
usable Reader content.

Remote images remain behind keyboard-accessible placeholders. The first activation loads all
known images for that Item, with independent consent for other Items. Consent and successful
images last for the opened view; a failed image has its own Retry and never replaces readable text.

**AI summary** is explicit: a compact Reader button and an Item menu entry in lists. Server
availability controls generation; unsupported input and generation opt-out hide new work. Ready
annotations remain readable when generation is disabled. A request immediately opens a local
pending block before the original text. Reading, Refresh, navigation and status checks never
generate summaries. Matching ready text is reused; failures retry only the eligible summary Job.
Status checks are manual until advisory transport is connected. Summary completion preserves
Reading State, list order, focus, reading position and disclosures in other contexts. An annotation
is bound to its exact source version; changing that version collapses the previous disclosure.

The Digest archive is newest-first and filters by saved Definition. Whole-edition reading keeps
its Topic structure and ordinary excerpts; it does not mark Items read. Opening an Item uses its
captured content and replaces the wide archive pane with compact contents. Back restores edition
position and title focus. A partial Issue preserves readable content and its saved failure context.
The Item overflow shares feedback, explanation and explicit exact-version AI-summary behavior.
Late annotations never replace saved edition facts or change its complete/degraded status.

Build chooses a saved Definition, including Manual only. Shared safe request identities survive
uncertain responses and reopening. Completion exposes the Issue identified by generationJobId and
never replaces the current reader. A committed-result Retry conflict reconciles the archive without
submitting a replacement Build. Definition management remains secondary to the reading archive.

Device-local readiness establishes one first-successful unfiltered baseline, covering older pages
as existing history. Later mount/reconnect/manual reads reconstruct all unopened post-baseline
badges without Notices. Opening an exact Issue records it as seen. Live event consumers claim at
most one readiness Notice across views; device, backend and verified Workspace scopes are isolated.
One view-owned browser SSE connection supplies live advisories; mounted views also retain explicit
Refresh when streaming is unavailable. Reconnect restores REST state without publication or generation.

Task Center reads five queues, optional progress, attempts, reason, timestamps and recorded duration.
No private arguments, invented ETA or polling appear. Mount, relevant advisories, reconnect and
explicit Refresh reconcile REST state. Settings and Tasks share Job queries/actions and one model
outcome boundary; an event coalesces an affected Job read across observers. Cancel confirms scope
and remains pending until authoritative completion. Retry follows the returned durable successor;
unknown/superseded model preparation has no unsafe Retry or inferred historical model target.
Loaded task details survive a failed read with a local recovery action.

**Save to vault** is available from every Item context and the whole-edition menu. A protected-focus
form asks for normalized folder/filename and Compact/Full; existing destinations are rejected.
Full is unavailable for individual description/abstract-only inputs with an explicit Compact
switch, while mixed Full Digests require consent and label each missing body **Excerpt only**.
Source metadata, ordered captured Digest text and matching ready annotations remain independent
of later canonical changes. Save never generates, retries or waits for AI work.

Full image modes are Links, Remote and Local. Local uses only captured Item descriptors, strict
static SVG sanitization, portable relative attachments and per-operation 30-file/10 MB/100 MB
budgets across a Digest. Unavailable/over-budget assets retain original links with a truthful
result. Serialized Vault/FileManager writes never overwrite and roll back only newly created
attachments if the note fails. Content/image choices persist per vault; saved notes are never
mirrored or updated by later work. Reader remains read-only.

Desktop main/pop-out SSE and teardown are verified. Physical mobile feature parity is not claimed
by browser-safe adapters, responsive previews or desktop host tests.

See [Using Topic Stream](../../guides/using-topic-stream.md) for current controls and
[backend capabilities](../README.md#capabilities) for server behavior.
