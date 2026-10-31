# Using Topic Stream

The Obsidian plugin provides Feed reading, saved Digest editions, archive Search, Reader, Tasks
and a Settings workspace for Sources, Topics, Digest Definitions, Workspace Settings and a global
Preference Profile.

In Obsidian’s plugin settings, edit **Server URL** and choose **Test connection** to check the draft
address, or **Apply** to replace open connections. Typing or testing does not apply the address.
The URL stays local to this device. Default All/Unread and Snapshot content/image preferences are
saved with the vault; each Save to vault still asks for its destination and content mode.

## Manage Sources and Topics

Open the **Settings** icon, then **Sources**. **Add source** supports RSS/Atom feed URLs, arXiv
queries, YouTube Channel IDs and GitHub Trending period/language. YouTube requires an ID rather
than a channel URL. Poll interval, enabled state and RSS/Atom private-network policy are in
**Advanced**. Private-network access starts disabled.

Creating a Source saves it, then requests its first Sync separately. If Sync fails, the Source
stays saved; **Retry sync** repeats Sync rather than creation. Each row also offers Edit,
Pause/Enable, confirmed Delete and Sync now. **Sync all** requests synchronization of enabled
Sources without claiming individual results. Rows show the last saved synchronization timestamp.
Current-view Job acknowledgements and failures are session-local; after reopening, use Sync now
or inspect Tasks rather than assuming a previous per-Source outcome.

In **Topics**, create or edit Name, Include and Sources. **Advanced** contains Exclude, Item kinds,
ordinary/strict matching and enabled state. Empty Source or Item-kind scope is unrestricted.
**Choose sources** offers search, checkboxes and **All sources**. **Add source** inside the picker
opens a separate form and returns to the intact Topic draft. The new Source belongs to the whole
Workspace; it joins an explicitly limited scope while All sources stays unrestricted. Topics also
support Pause/Enable, revision display and confirmed Delete.

Leaving a dirty editor offers **Save**, **Discard** or **Stay**. Save proceeds only after validation
and persistence succeed. Versioned resources keep drafts on a conflict and offer **Review latest**,
**Keep my draft** or **Use saved values**; keeping a draft requires another explicit Save.

## Configure Digest Definitions

Under **Digest definitions**, choose ordered Topics and per-Topic Item limits. New Definitions
start with **Manual only**. **Scheduled** reveals recurrence, time and IANA timezone. Switching to
Manual only stops future scheduled runs and keeps the calendar values for later use; it does not
disable manual Build. New calendars start at Monday 12:00 in the local timezone.

**Build** requests a saved Definition explicitly. Saving or changing delivery mode never starts a
Build. If the response is uncertain, **Retry request** checks the same stored request identity,
including after reopening. A queued Build remains inspectable through its Job; completion does not
replace the current reading screen. Deleting a Definition stops its schedule and retains saved
Digest Issues.

## Read saved Digest Issues

Open **Digests** for the newest saved Issues, optionally filtered by their saved Definition.
**Load older issues** continues the archive. **Build** offers saved Definitions, including Manual
only ones; **Manage definitions** is secondary in the archive’s **…** menu. Build status and a
link to its exact task remain local. A completed Build offers **Open built issue**; completion
does not replace the edition or Item you are reading.

Opening an Issue shows the whole saved edition with its Topic sections and ordinary excerpts.
It does not mark its Items read. Opening an Item title shows its captured content in Reader and
marks that Item read; wide leaves replace the archive with compact Issue contents. **Back to
issue** restores the edition’s position and title focus, and **Back to archive** returns to the
Issue list. A partial Issue retains its readable sections and explains the incomplete ones.

The Item menu includes **Why this item**, feedback and explicit **AI summary** where the server
allows it. Summary remains separate from the ordinary excerpt and is bound to that saved text
version. Expanding it does not open Reader, mark the Item read or rebuild the Issue. A failed
summary retries its eligible task; a later result fills that Item’s disclosure without changing
the saved edition’s facts or status.

Existing history is baselined once per device, backend and Workspace. On reopening or **Refresh**,
later unopened Issues receive **New** badges without catch-up Notices. Opening that exact Issue
clears its badge on this device. Live notifications are not connected yet; use Refresh to reconcile
an already open view.

## Inspect background Tasks

Open **Tasks** to inspect Fetch, Generation, Retrieval, Feed Preparation and Maintenance work.
Filter by status or load older tasks. Details show attempts, reason, recorded timestamps and
duration, with optional fixed-cohort progress. Equal counts do not imply activation; task status
is authoritative, and unknown totals never invent an ETA.

**Refresh tasks** and **Refresh selected task** read current status without polling. A failed
read preserves the last loaded details. Cancel requires confirmation and remains a request
until the server records a terminal outcome. Retry opens the server’s actual successor task,
which may already be complete. Historical model preparation offers Retry only with a verified
current public target. A Build Retry conflict checks for the Issue already saved by that task
and offers **Open saved issue** instead of submitting another Build.

## Configure the Workspace

**Workspace** separates Embedding Models from AI generation. Select an available, initialized
Embedding Model, then save explicitly. Desired and Serving remain distinct during preparation;
replacing active preparation requires confirmation. The previous Serving model stays available
until activation. Without a Serving model, saved Feed history, Settings and Jobs remain usable.

Progress uses the public Job phase and fixed initial Item count. A full bar does not mean
activation; unknown totals have no invented percentage or ETA. Refresh reads authoritative status.
Cancel preserves Desired and any previous Serving model; Retry is offered only for the verified
current operation. Current user-initiated outcomes are remembered locally and produce at most one
Notice after final failure or confirmed activation. Live Job advisories refresh authoritative
status while the view is open; progress itself never declares activation or failure.

AI generation is opt-in. Configure the operator-provided endpoint and model names, summary language
and limits; credentials remain server-managed. Saving never generates or regenerates text.
Instructions, retention and retrieval tuning are in Advanced. **AI summary** in Reader or an Item's
menu requests an annotation explicitly; opening an Item never starts generation.

**Preferences** edits Workspace-wide Prefer/Avoid guidance for optional Digest AI reranking.
Topic Include/Exclude and Feed feedback are separate; this profile does not introduce another
Feed or Search ranking effect. **Status** shows backend/component availability independently of
optional AI configuration.

## Read the Feed

Choose **All Topics** or search for an enabled Topic in the compact switcher beside Feed.
Each selection checks for ready recommendations once on its first visit in an opened view.
**Refresh** checks again deliberately. Preparation may still be running when it returns; wait
and choose Refresh again when needed. A failed attempt keeps saved content and never retries
publication automatically. Closing and reopening the view starts a new first-visit lifecycle.

Older pages load as you scroll within the saved traversal. Switching Topics restores each
selection’s loaded pages, selected Item and scroll position. Refresh with new publications
returns to the new top; membership-only changes keep the reading position. Failed Refresh reports
beside the button and preserves the loaded traversal. If an older page fails, **Try again**
continues the same history without replacing it.

**All / Unread** is saved with the vault. It filters loaded pages without requesting a new Feed.
An Item marked read stays dimmed in the current Unread traversal until successful Refresh.
**Load older items** remains available when the loaded pages contain no unread Items but older
history exists. **No unread items** means the loaded traversal is exhausted.

Open an Item title to show its reading pane and mark it read. Wide leaves retain the list;
narrow leaves use **Back to list**, preserving position and focus. **Open original** in **…**
opens the source and marks it read. **Why this item** opens full explanation, score and metadata
without changing Reading State. **Mark as read / Mark as unread** is explicit in the same menu.
Reader displays canonical Markdown inside the view without creating a vault file. Full-content
failure keeps the title, excerpt, metadata and navigation available. **Check for full content**
rereads an empty Item; rendering failures offer **Try again**. Abstracts and descriptions are
labeled honestly rather than presented as a full extracted article.

Images are withheld until you activate a placeholder. The first activation loads all known images
for that Item. Another Item requires its own consent; returning to an Item reuses loaded images
within the opened view. Failed images offer their own **Retry image** without interrupting reading.

## Search and summarize

Open **Search** to browse saved Items across all time, including Hidden Items. Submit a query for
hybrid search, or submit an empty query to browse the archive. **Filters** opens an anchored panel
for Topic, Source and UTC publication dates. Apply explicitly; closing the panel leaves the current
session unchanged. Search loads 20 results at a time, up to 100 per session. Refine the query or
filters to start another session. A temporary retrieval outage offers recovery and links to
Settings/Tasks while saved content remains usable.

Title and **Why this item** actions follow the same reading rules as Feed. Feedback, corrections,
Hide and Reading State remain shared; a Hidden Item stays discoverable in Search. Back restores
the result position and focus.

Choose **AI summary** near the Reader title, or in an Item's **…** menu. Generation must be enabled
and the server must have supported article/README text. Unsupported input does not offer a new
generation action. An existing ready annotation remains readable after generation is disabled.
The pending block opens immediately without replacing the source. Duplicate clicks reuse work;
**Check summary status** reads its current state. **Retry AI summary** retries an eligible failed
summary Job. Status-read errors have a separate read-only recovery control. Until live advisories
are connected, use the explicit status check for completion.

Summary disclosure alone does not mark an Item read. Completion preserves focus and reading
position, and does not expand other Item contexts. A changed text version requires its own
disclosure; an old annotation is never shown for new source text.

## Give feedback

Choose **Useful** or **Not for me** from the Item's **…** menu. The rating saves immediately and a
small blurred note panel opens. A note is optional: use the paper-plane **Send note** button to send
it, or close the panel to keep only the rating. Unsent drafts survive closing/reopening the panel
for that opened Item and failed sends. Use **Add note** / **Edit note** in the same menu to return.
The row and reading pane share the rating and draft. **Not for me** also offers **Wrong topic** in
the note panel. Feedback is the current choice, so another choice replaces it. Dislike affects future recommendations and does not hide an
already-published card. Personalization begins using each sentiment independently after five
current decisions of that kind. Deterministic scoring ignores comments.

Use **Add to Topic…** to include an Item, **Wrong topic** to exclude it from the selected Topic,
or **Use automatic matching** to clear the manual correction. In All Topics, the correction menu
asks which Topic to change. Corrections keep the original publication position and do not publish
the Item again. **Hide from all Feeds** hides it globally, separately from a Topic correction.
If feedback or a correction reports **Saved. Retrieval update pending**, the write is retained;
do not submit it again just to finish retrieval. Refresh can read its current state.

## Save an independent vault snapshot

Choose **Save to vault** from an Item’s **…** menu in Feed, Search, Reader or a saved Digest.
The whole-edition **…** menu beside a Digest title saves the entire Issue. Save does not mark
Items read. Every attempt asks for **Folder** and **Filename**; leaving Folder empty uses the
vault root. Paths are normalized, `.md` is added, and an existing destination is rejected.
Choose another filename to keep both notes.

**Compact** keeps ordinary excerpts, source links, metadata and any ready AI summary bound to
that exact text version. **Full** keeps the available article body or README. An individual
abstract/description-only Item explains why Full is unavailable and offers **Use Compact**.
A mixed Full Digest requires explicit consent to keep missing bodies as **Excerpt only**;
choose Compact instead to use excerpts throughout. Digest order, captured text and matching
captured annotations are preserved even when the canonical Item has changed.

Full offers three image modes:

- **Links** creates ordinary clickable source-image links without downloading images.
- **Remote** keeps external image embeds; opening the saved note may load their original sites.
- **Local** downloads permitted Item descriptors into `<note-basename>.assets/` and uses relative
  embeds. Each Save allows 30 files, 10 MB each and 100 MB total across the entire Digest.

Unavailable, unsafe or over-budget Local images remain ordinary links, and the result reports
how many stayed links. Historical descriptors never trigger an arbitrary-URL download or
substitution with a newer canonical image. SVG is sanitized before attachment creation.
The last content and image choices are remembered with this vault.

Save includes only ready matching annotations available for that attempt, including stored text
after generation is disabled. It never generates, retries or waits for AI work. Notes remain
independent: later summaries, backend changes and plugin reloads do not update saved files.
A note-write failure removes only attachments created by that attempt; any cleanup failure names
the files that remain. Cancel or Escape before submission leaves the vault unchanged and restores
focus. While a write is in progress the form waits for its result.

## Use the backend API

An open Topic Stream view receives advisory Feed, Digest and Job events. Reconnection reads
authoritative REST state; it does not replay events, publish a new Feed or generate AI text.
An unavailable stream keeps saved content readable and explicit Refresh available, with no
polling or global offline banner. Offline Digest catch-up adds badges; live readiness produces
at most one Notice per device, backend and Workspace, including across open views.

Streaming uses browser Fetch and requires the backend's exact-origin CORS policy. The default
allows `app://obsidian.md`; see [operator configuration](../operations/configuration.md#browser-access-and-sse).
Desktop Obsidian main/pop-out streaming is verified. The adapter uses browser APIs without
Node/Electron dependencies; physical iOS/Android streaming has not been smoke-tested.

Server capabilities and contracts are documented separately:

- [Search and content delivery](../product/capabilities/search-reader-and-events.md).
- [Explicit asynchronous Item AI summaries](../product/capabilities/item-ai-summaries.md).
- [Digest schedules and immutable Issue history](../product/capabilities/digests-and-schedules.md).
- [Jobs, Retry and indexing progress](../product/capabilities/durable-jobs.md).
- [Workspace Settings and models](../product/capabilities/workspace-settings.md).
- [OpenAPI contract](../../api/openapi/openapi.yaml).

The [Vault Snapshot boundary](../product/decisions/0004-explicit-vault-snapshots.md) keeps
explicit client-owned notes separate from backend content. See [troubleshooting](../operations/troubleshooting.md) for
connection or recommendation problems.
