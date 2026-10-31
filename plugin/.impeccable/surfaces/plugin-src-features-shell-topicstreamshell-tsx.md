---
version: 1
slug: "plugin-src-features-shell-topicstreamshell-tsx"
primary_target: "plugin/src/features/shell/TopicStreamShell.tsx"
related_targets: ["plugin/src/features/settings/SettingsWorkspace.tsx","plugin/src/features/feed/FeedView.tsx","plugin/src/features/digests/DigestsWorkspace.tsx","plugin/src/features/digests/DigestEdition.tsx","plugin/src/features/digests/DigestReader.tsx","plugin/src/features/digests/ArchiveBuilds.tsx","plugin/src/features/tasks/TasksWorkspace.tsx","plugin/src/features/tasks/TaskActions.tsx","plugin/src/features/tasks/TaskModelContext.tsx","plugin/src/shared/jobs/JobProgress.tsx","plugin/src/shared/jobs/job-actions.ts","plugin/src/shared/jobs/resources.ts","plugin/styles.css"]
---

# Topic Stream application surface

Mode: Operate. Applies to the complete plugin Shell, reading lists/details, all Settings resources,
forms, menus, dialogs and states. Production components also own Storybook. Keep host light/dark,
leaf responsiveness, English UI, English/Spanish fixtures and all delivered workflows.

## Direction contract

THESIS: A composed, tactile reading workspace. One clear action per view; secondary operations
belong in real anchored menus. The user rejected the former collection of outlined buttons.

OWN-WORLD: User-pinned Dribbble reference, downloaded and inspected at
`.artifacts/storybook-review/design-reference.png` in the repository root. Original source:
https://cdn.dribbble.com/userupload/12942608/file/original-4c287785698d505006c3f207a6b77987.png?resize=2048x1536&vertical=center
Black canvas, charcoal rounded work zones, lighter matte fields, bright neutral text and user-approved soft peach #F2B38A
selection. A lighter peach supports hover. A restrained warm peach selection wash and
translucent blurred editor/menu panels are the signature, with accessible contrast.
Light mode translates the same hierarchy into pale grounds and white work zones.

STORY: Browse a calm list, focus one Item, then configure the Workspace without losing context.
All actions remain available, with labels and state rather than decorative control clutter.

FIRST VIEWPORT: Compact branded navigation above separated rounded list and reading work areas.
Primary navigation wraps complete tab controls at constrained widths so labels stay whole. Settings
uses quiet section navigation, one resource heading and one primary Add/Save action; its heading and
resource controls wrap as groups, and the narrow resource panel stretches to the available width.
Rows show identity and concise metadata, with a single ellipsis opening a floating action menu.
Choosing Useful / Not for me saves the rating immediately and opens a compact, nonmodal blurred
note panel at that Item's action trigger. Notes are optional and send only with the paper-plane
Send note button. Closing does not undo the rating; unsent drafts survive reopening and errors.
Add note / Edit note stays in the Item menu, with no permanent inline Add a note control.

DELIVERED FEED SLICE: All Topics and each selected Topic have independent first-visit and manual
refresh traversals; the searchable Topic picker is anchored to its trigger. The persisted All/Unread
filter retains read rows during the current traversal. A title opens the direct reading pane and marks
an unread Item read; Why this item opens explicit recommendation details without marking it read.
One overflow owns Useful/Not for me, Add/Edit note, Why this item, Open original, read/unread state,
Hide from all Feeds, and Add/Wrong/automatic Topic corrections. Not for me can reveal a transient
Wrong topic action in the optional-note panel. Canonical committed 503 responses preserve saved intent
and show retrieval-update-pending status; they do not roll the user's action back.

DELIVERED SEARCH: Search is a quiet text field with an explicit Search action and a Filters action.
Filters opens an anchored nonmodal glass surface with Topic, Source, UTC publication-date bounds,
explicit Apply and clear/close cancellation, local option-load recovery, 20 results per request, and
a 100-result session cap. Search rows remain text-led, expose Hidden state, and reuse the same quiet
overflow actions and selected reading composition as Feed.

DELIVERED READER: The connected shell owns the canonical Reader, with the Item title primary, source
headings subordinate, host-native reading prose near a 70ch measure, and keyboard-focusable source
code/tables with local horizontal scroll. Recognized fenced code is rendered as plain code without
invoking host code-block processors. Images are matte labelled placeholders with no thumbnail fetch;
first activation independently consents per Item and loads all known images through a bounded
four-download queue. Partial failures expose local Retry image actions, and object URLs are released
with the view. Item action notices stay near the Reader title and remain Item-scoped.

DELIVERED AI SUMMARY: The compact labelled neutral AI summary action sits near the Reader title and
uses Item overflow in list contexts when supported. Its accessible labelled disclosure is local
to context and exact source version; pending appears before prose, ready text remains readable after
opt-out/collapse, generation is always explicit, failed work offers Job Retry, and status recovery is
read-only. Summary arrival preserves source, focus, and position; source-version changes collapse the
previous disclosure. Shared exact-version state is provided by ItemSummaries.

DELIVERED DIGESTS AND TASKS: The connected Digest archive uses text-led Issue rows with definition
filtering, Refresh, a real Build menu, partial-Issue metadata, local loading/empty/recovery states,
and device-scoped peach New badges. Device-scoped readiness establishes a baseline from the first
successful unfiltered archive head once per device/backend/Workspace; subsequent mount/manual REST
reads reconstruct unopened post-baseline Issues without emitting catch-up Notices. Live host
Notice/event consumers are deduplicated, can add a new Issue after that baseline, and are tested at
the event port. UI 11 now delivers those advisories through a view-owned browser Fetch/ReadableStream
SSE connection with bounded reconnect, endpoint replacement, pop-out realm migration and teardown.
A saved Issue opens as an
Edition with title/date lead, section index and captured summaries; selecting an Item swaps the archive
for compact Issue contents and the captured Reader, with Back restoring selection and focus. Build
results expose one Open built issue action and local retry/uncertain/unavailable recovery.

Task Center is the shared Job history with a status filter, 30-row pages, queue/status metadata and
detail facts. Shared Job progress shows phase and initial-cohort counts, uses peach normal fills and
muted failed/cancelled history fills, and keeps status authoritative when a count reaches N/N. WebKit
and Gecko pseudo-elements are separate. Active Jobs expose Cancel with a native confirmation dialog.
Ordinary dead/cancelled Jobs expose Retry; model-preparation Jobs additionally require a verified
current, eligible model target. Digest Builds, Settings and Task Center
observe the same Job queries and event port, including model-transition context and saved-result
recovery.

DELIVERED SNAPSHOTS: Item menus in Feed/Search/Reader, captured Digest Item menus, and the whole
Edition `Issue actions` menu open one native `showModal()` Save to vault form. The form asks for
normalized folder and filename, remembers Compact/Full and Full-only Links/Remote/Local image choices
per vault, and creates an independent Markdown note without changing backend state. An individual
Item without article body/README explains why Full is unavailable, hides image controls, disables Save,
and offers explicit Use Compact. A mixed Full Digest reports its missing count, requires an inline
checkbox consent, and labels each omitted body Excerpt only; cancel leaves the form open and keeps the
choice available. Compact and Full include only ready AI annotations matching the captured Item input
version; Save never generates, waits for, retries, or mirrors later Summary work. Contextual
validation, overwrite/write errors, and pending progress remain in the native form. Full Local writes
bounded relative attachments after strict SVG sanitation, retains links on failures or budget limits,
and the serialized host Vault/FileManager boundary rolls back only attachments created by the attempt.
Item/Issue overflow triggers restore focus on dismissal or completion. Release evidence records 166
unit tests, 272 Storybook browser tests, the mobile browser dependency-boundary check, nine native
production flows plus the separately verified SSE probe, and 46 dark/light desktop/narrow/mobile/
200%-text captures of the final loaded Shell screens with zero page errors or horizontal overflow.
Separate prior feature matrices and interaction tests cover loading, empty, local failures, badges,
menus, partial Issues, cancellation, and failed/completed Job history; those states are not counted
among the 46 release captures. The one 460px Settings panel fix is resolved by the fresh ship review;
physical mobile-device parity remains unclaimed.

SCROLL TREATMENT: The Feed header remains opaque at rest and reserves `32px` below its controls. Only
the scrolled list header uses the existing glass token with `blur(12px)`, with an opaque fallback;
anchored menus, Topic and Search filters, note panels, and editor panes retain their existing
`blur(28px)` rule.

FORM: User-pinned visual reference, code-led implementation; no concept roll or generated comp.
Direction authority is the user's supplied reference and explicit peach selection. No random
seed was used or claimed; do not invent one retrospectively or reopen the user's chosen direction.
Standard Obsidian controls, host typography and keyboard/focus behavior carry the reference's
palette, density and corner language. Menus anchor without moving or clipping list rows.

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

LIVE EVENTS: UI 11 supplies actual browser Fetch/ReadableStream SSE with bounded reconnect,
authoritative REST reconciliation, endpoint/view lifecycle isolation, pop-out realm migration and
cross-view Notice deduplication. Unavailable streams keep reading and explicit Refresh usable without
automatic Feed publication, Summary generation or global offline chrome. Responsive frames and desktop
host tests do not claim physical mobile-device parity. Hidden capability fixtures and technical/
captured-reader probes are contracts or harness-only evidence, not extra product UI delivery.
