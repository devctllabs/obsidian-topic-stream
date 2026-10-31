---
name: Topic Stream
description: A composed, tactile reading workspace with matte charcoal work areas, host-native type, and a soft peach action language.
colors:
  paper-light: "#edeeed"
  paper-dark: "#090909"
  panel-light: "#ffffff"
  panel-dark: "#191919"
  field-light: "#f0efed"
  field-dark: "#2b2a29"
  hover-light: "#e9e5e0"
  hover-dark: "#34312f"
  ink-light: "#24211f"
  ink-dark: "#f3f0ed"
  muted-light: "#68615c"
  muted-dark: "#b5aea8"
  line-light: "#dedbd7"
  line-dark: "#36322f"
  error-light: "#b34040"
  error-dark: "#f28b82"
  accent-light: "#99562e"
  accent-dark: "#f2b38a"
  accent-fill: "#f2b38a"
  accent-hover: "#f7c5a6"
  accent-soft-light: "#f8e8dc"
  accent-soft-dark: "#3b2b23"
  accent-ink: "#241a13"
  selected-excerpt-ink: "#493124"
  glass-light: "rgb(255 255 255 / 86%)"
  glass-dark: "rgb(35 33 31 / 82%)"
  selection: "#f2b38a"
typography:
  body:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "var(--font-text-size, 16px)"
    fontWeight: 400
    lineHeight: 1.5
  display:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "2em"
    fontWeight: 600
    lineHeight: 1.18
    letterSpacing: "-0.03em"
  title:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "1.375em"
    fontWeight: 550
    lineHeight: 1.3
    letterSpacing: "-0.02em"
  subsection:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "1em"
    fontWeight: 550
    lineHeight: 1.4
  prose:
    fontFamily: "var(--font-text, var(--font-interface))"
    fontSize: "var(--font-text-size, 16px)"
    fontWeight: 400
    lineHeight: 1.75
  metadata:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "0.8125em"
    fontWeight: 400
    lineHeight: 1.5
  settings-label:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "0.8125em"
    fontWeight: 400
    lineHeight: 1.5
  note-label:
    fontFamily: "var(--font-interface, system-ui, sans-serif)"
    fontSize: "1em"
    fontWeight: 400
    lineHeight: 1.5
rounded:
  checkbox: "5px"
  chip: "8px"
  menu-item: "10px"
  control: "12px"
  icon: "13px"
  textarea: "14px"
  menu: "15px"
  reading-row: "17px"
  note: "18px"
  mobile-shell-header: "16px"
  shell-header: "20px"
  settings-nav: "18px"
  work-area: "22px"
spacing:
  hairline-gap: "4px"
  compact-gap: "6px"
  tight: "8px"
  label-gap: "9px"
  control-inset: "12px 14px"
  group: "16px"
  row-inset: "18px 16px"
  panel: "20px"
  section: "24px"
  page: "28px 32px 40px"
  reading-pane: "20px 32px 44px"
  wide: "36px"
components:
  primary-action:
    backgroundColor: "{colors.accent-fill}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.control}"
    padding: "10px 18px"
    height: "42px"
  secondary-action:
    backgroundColor: "{colors.field-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.control}"
    padding: "10px 16px"
    height: "40px"
  icon-button:
    backgroundColor: "{colors.field-light}"
    textColor: "{colors.muted-light}"
    rounded: "{rounded.icon}"
    padding: "10px"
    height: "40px"
    width: "40px"
  primary-navigation-selected:
    backgroundColor: "{colors.ink-light}"
    textColor: "{colors.panel-light}"
    rounded: "{rounded.icon}"
    padding: "9px 16px"
    height: "40px"
  selected-reading-row:
    backgroundColor: "{colors.selection}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.reading-row}"
    padding: "{spacing.row-inset}"
  text-field:
    backgroundColor: "{colors.field-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.icon}"
    padding: "{spacing.control-inset}"
    height: "44px"
  action-menu:
    backgroundColor: "{colors.panel-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.menu}"
    padding: "6px"
    width: "208px"
  feedback-note:
    backgroundColor: "{colors.panel-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.note}"
    padding: "{spacing.panel}"
    width: "360px"
  archive-row:
    backgroundColor: "{colors.field-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.reading-row}"
    padding: "{spacing.row-inset}"
  issue-contents-item-selected:
    backgroundColor: "{colors.accent-fill}"
    textColor: "{colors.accent-ink}"
    rounded: "{rounded.control}"
    padding: "12px"
    height: "44px"
  new-issue-badge:
    textColor: "{colors.accent-light}"
  job-progress-fill:
    backgroundColor: "{colors.accent-fill}"
    height: "3px"
  job-progress-history-fill:
    backgroundColor: "{colors.muted-light}"
    height: "3px"
  snapshot-dialog:
    backgroundColor: "{colors.panel-light}"
    textColor: "{colors.ink-light}"
    rounded: "{rounded.work-area}"
    padding: "28px"
    width: "460px"
  snapshot-choice:
    backgroundColor: "{colors.field-light}"
    textColor: "{colors.muted-light}"
    rounded: "{rounded.control}"
    padding: "10px 14px"
    height: "44px"
---

# Design System: Topic Stream

## Overview

**Creative North Star: "A Composed Tactile Reading Workspace"**

Topic Stream is a calm, direct control surface for a personal information-retrieval system. Its
visual authority is the user-pinned reference at
`.artifacts/storybook-review/design-reference.png`, sourced from the approved
[Dribbble image](https://cdn.dribbble.com/userupload/12942608/file/original-4c287785698d505006c3f207a6b77987.png?resize=2048x1536&vertical=center).
The implementation translates that reference into black and charcoal work areas, lighter matte
fields, clear host typography, rounded work zones, and a soft peach action language. The reference
is a direction anchor, not a pixel-bound asset or a generated concept.

The production shell is code-led and retains Obsidian's interface and reading typography. Real
ConnectedShell and production screen compositions render in Storybook; external services and data
are mocked at their ports, and English/Spanish fixtures are synthetic demonstrations. The shipped
slice includes the Feed's independent All Topics/Topic traversals, persisted All/Unread filtering,
direct-title reading and explicit Item details, immediate feedback with optional notes, visibility,
and Topic corrections, the quiet Search + Filters flow, and the canonical Reader with full-body
Markdown, consent-gated images, and an explicit shared AI Summary disclosure. Sources, Topics,
Digest definitions, Workspace, Preferences, and Status remain available as management surfaces.
The Reader preserves title-first hierarchy and Item-scoped notices; Search keeps text-led rows,
anchored filters, UTC date bounds, 20-result pages, and a 100-result session cap. UI 08 also ships
the connected Digest archive, saved Edition with compact contents and captured Reader, saved-definition
Build and results, device-scoped New Issue badges, and the Task Center's shared Job progress,
Cancel/Retry actions, and model-recovery context. Device-scoped readiness establishes a baseline from
the first successful unfiltered archive head once per device/backend/Workspace; subsequent mount or
manual REST reads reconstruct unopened post-baseline Issues without emitting catch-up Notices. UI 11
delivers live Notice/event consumers through an actual view-owned browser `fetch`/`ReadableStream`
SSE transport with bounded reconnect, cancellation, endpoint replacement, pop-out realm migration,
and cross-view deduplication. Reconnect reconciles REST resources without Feed publication, Summary
generation or global offline chrome; unavailable streams retain loaded content and explicit Refresh.
UI 09 also ships an explicit Save to vault workflow from Item and
captured Digest Item overflows plus the whole-Issue actions menu. Its native protected-focus form
normalizes folder and filename paths, remembers Compact/Full and Full-only Links/Remote/Local image
choices per vault, and keeps Save independent from backend state. Full-only image controls disappear
when an individual Item has no full text; mixed Full Digests require explicit consent and label missing
Items Excerpt only. Ready, source-version-matching AI annotations are captured when available, while
Save never generates, waits for, or mirrors later summary work. Portable writes use bounded local
attachments, safe SVG sanitation, overwrite checks, and rollback through the host Vault/FileManager
boundary; contextual validation, pending, and write errors stay in the form.

**Key Characteristics:**

- Black/charcoal work areas with pale light-mode equivalents and no decorative gradients.
- Matte fields and rounded work zones, with translucent blur reserved for anchored menus, note
  panels, editor panes, and the Feed header while its list is scrolled.
- A soft peach selection and action color (`#F2B38A`) with a lighter hover (`#F7C5A6`).
- Host-native interface and reading type, a restrained spacing rhythm, and equal light/dark care.
- One clear action per view; secondary operations stay in real anchored menus.
- Title-first Reader content with bounded prose, local scrolling for code/tables, and quiet source
  headings beneath the Item title.
- Consent-gated matte image placeholders and local recovery states; AI Summary remains explicit,
  version-bound, and readable when ready.
- View-owned browser SSE keeps live invalidations advisory: reconnect and unavailable-stream recovery
  reconcile authoritative REST state without automatic Feed publication, Summary generation, or a
  persistent offline banner.
- Primary navigation wraps complete controls, while Settings headings/resource actions wrap as groups
  and narrow resource panels stretch to the available width.
- Digest archive rows, Edition sections, compact contents, and captured Reader preserve the same
  text-led hierarchy while New badges identify unseen saved Issues.
- Task Center keeps Job status, progress, history facts, Cancel/Retry, and model recovery in the
  shared shell rather than turning background work into a dashboard.
- Snapshot saves use one native, focus-protected peach form for normalized paths, explicit content and
  image choices, mixed-result consent, and contextual recovery; Item and Issue overflow entry points
  restore focus after dismissal.

## Colors

The palette has a near-black dark mode and a pale, warm-neutral light mode. Semantic roles keep
the same hierarchy across themes: paper is the root ground, panel is the work zone, field is the
control surface, ink is primary text, muted is supporting text, line is a quiet seam, and peach is
reserved for action, selection, and focus. The light accent is the darker `#99562e` text/link color;
dark mode uses the approved peach directly for the same role.

### Primary

- **Soft Peach Selection** (`#F2B38A`): selected rows, checked/pressed controls, primary actions,
  and the persistent selection token in both themes.
- **Peach Hover** (`#F7C5A6`): hover state for an already selected row or peach action.
- **Light Theme Accent Ink** (`#99562e`): readable light-theme links, focus, and action text.
- **Dark Theme Accent** (`#F2B38A`): dark-theme links, focus, and action text.

### Neutral

- **Light Paper** (`#edeeed`) and **Dark Paper** (`#090909`): root workspaces.
- **Light Panel** (`#ffffff`) and **Dark Panel** (`#191919`): separated list, detail, settings,
  and dialog work zones.
- **Light Field** (`#f0efed`) and **Dark Field** (`#2b2a29`): matte inputs, rows, quiet buttons,
  and controls.
- **Light Hover** (`#e9e5e0`) and **Dark Hover** (`#34312f`): neutral hover feedback.
- **Light Ink** (`#24211f`) and **Dark Ink** (`#f3f0ed`): primary text and the selected primary-nav
  background/text pairing.
- **Light Muted** (`#68615c`) and **Dark Muted** (`#b5aea8`): metadata, labels, and supporting copy.
- **Light Line** (`#dedbd7`) and **Dark Line** (`#36322f`): seams, scrollbar tracks, and progress
  tracks.
- **Light Error** (`#b34040`) and **Dark Error** (`#f28b82`): semantic error text and recovery copy.
- **Selected Row Ink** (`#241a13`) and **Selected Excerpt Ink** (`#493124`): readable text on peach
  selection.
- **Light Glass** (`rgb(255 255 255 / 86%)`) and **Dark Glass** (`rgb(35 33 31 / 82%)`): translucent
  overlay fallback colors when blur is supported.

**The Purposeful Peach Rule.** Peach marks a current action, selected row, checked state, or focus;
it is not a decorative wash across every title or surface. Host-native semantic warning colors may
still appear for errors.

## Typography

**Display Font:** `var(--font-interface, system-ui, sans-serif)`

**Body Font:** `var(--font-interface, system-ui, sans-serif)` with
`var(--font-text, var(--font-interface))` for reading prose.

**Character:** Obsidian supplies the actual face and scaling. Topic Stream establishes hierarchy
through size, weight, line-height, and small negative tracking rather than a downloaded display font
or decorative lettering. Interface copy remains sentence case.

### Hierarchy

- **Display / H1** (600, `2em`, `1.18` line-height, `-0.03em`): the primary page or reading title.
- **Title / H2** (550, `1.375em`, `1.3` line-height, `-0.02em`): section and content titles.
- **Subsection / H3** (550, `1em`, `1.4` line-height): smaller resource headings.
- **Body** (400, `var(--font-text-size, 16px)`, `1.5` line-height): interface copy and ordinary
  content; reading prose uses `1.75` line-height and a `70ch` maximum.
- **Metadata** (400, `0.8125em`, `1.5` line-height): source, date, Topic, feedback, and status
  context.
- **Settings label** (400, `0.8125em`, `1.5` line-height): muted labels above settings form
  fields.
- **Note label** (400, `1em`, `1.5` line-height): the optional-note prompt inside the `14px`
  feedback panel.

**The One Focal Title Rule.** Give the current page or opened Item one clear title. Supporting
metadata and controls stay quieter; hierarchy does not require inventing an editorial headline or
an unsupported intelligence claim.

## Layout

The shell is a compact vertical workspace with `8px` outer padding, a `6px` shell gap, and a
rounded header above the active screen. Reading uses a list/detail split of `minmax(18em, 1fr)` to
`minmax(0, 2fr)` with a `6px` gap. The list and detail are independent rounded work areas, with
text-led rows and a generous detail reading pane. The widest reviewed Storybook leaf is 1100px
inside a 1180px viewport; the narrow reviewed leaves are 460px and 360px with no horizontal
overflow.

At `48em` container width, the split becomes a single active pane: the selected detail replaces the
list and a Back action restores the reading context. The shell header tightens from `20px` to `16px`
radius, the brand wraps above primary navigation, and page padding becomes `24px 16px`. Primary
navigation wraps complete tab controls while keeping each label whole. Settings changes from a desktop
two-column layout (148px navigation, `36px` gap, editor section) to a mobile section picker, then one
full-width editor surface. Settings headings and resource controls wrap as groups, and the narrow
resource panel stretches to the available width, including the reviewed `460px` leaf. The compact
picker remains an explicit native `select`, not a hidden desktop navigation clone. Search follows the
same split and responsive restoration rules.

Settings editor panes are right-anchored over their resource section at up to `560px`, with `28px`
padding and the same glass treatment as menus. On narrow leaves they become full width with
`24px 16px` padding and no edge shadow. The flow preserves the resource context while editing.

The Feed list header reserves `32px` below the controls; the All/Unread filter uses `0 10px 8px` and
remains fully unobstructed. At rest the sticky header is an opaque panel. Once the list is scrolled,
only its header uses the existing glass token with `blur(12px)`; an opaque panel remains the fallback.
Refresh failures use an opaque panel status strip with `4px` top and `8px` bottom padding while the
loaded scroll position remains intact. Feed Topic selection is a searchable anchored picker; each
All Topics or Topic scope keeps its own selection and list position. The persisted All/Unread view
retains rows that became read during the current traversal, so a read action does not unexpectedly
erase the user's context. Search uses a quiet text field plus Search and Filters actions; Filters is
an anchored, nonmodal glass surface with explicit Apply and clear/close cancellation, UTC publication
dates, 20 results per request, and a 100-result session cap.

Digest archive uses the same text-led rows and list/detail split: a definition select, Refresh, a
real Build menu, New badges, partial-issue metadata, and local empty/loading/recovery states. A
selected Issue becomes an Edition with a title/date lead, section index, topic sections, captured
summaries and overflow actions; selecting an Item replaces the list with compact Issue contents and
the captured Reader, with Back restoring the Issue and previous focus. Task Center follows the same
split at a `30`-Job page size with an explicit status filter, and keeps task facts and model context
next to shared Job progress and actions. The release layout evidence covers 46 dark/light captures
across desktop, narrow, mobile, and 200% text for the final loaded Shell screens; it reports no page
errors or overflow. Loading, empty, local failures, badges, menus, partial Issues, cancellation, and
failed/completed Job history are covered by separate prior feature matrices and interaction tests,
not counted among those 46 captures.

Snapshot saves use a native `showModal()` dialog sized to `min(460px, calc(100vw - 32px))`, with a
`28px` inset, `22px` dialog corners, panel fallback, and the existing optional `blur(28px)` glass
treatment. Folder and filename fields are normalized before a save; Compact and Full are native
`16px` radios inside `44px` label targets, and Full-only Links, Remote, and Local image radios are
hidden when an individual Item has no full text. A mixed Full Digest shows the missing-Item count,
requires an inline native `16px` checkbox consent, and labels each omitted body `Excerpt only`;
individual missing-full Items explain the limitation and offer explicit Use Compact. Save remains the
peach primary action while validation, overwrite/write errors, and pending progress remain contextual
inside the form. Item and captured-Item overflow actions plus the whole-Issue `Issue actions` menu
open this same form; dismissal restores the originating trigger's focus. Compact includes ordinary
metadata/excerpts and matching ready AI summaries; Full preserves captured text and selected image
handling. The form does not request or wait for summaries and never updates a saved note after the
source changes. Final release evidence records 166 unit tests, 272 Storybook browser tests, the
mobile browser dependency-boundary check, nine native production flows plus the separately verified
SSE probe, and zero page errors or horizontal overflow in the 46-capture layout report. The fresh
finish review is `ship`; its one `460px` Settings panel correction is resolved. The detector record
has zero primary findings and one inherited native-dialog backdrop advisory.

Spacing follows a small rhythm: `4px` and `6px` for adjacent controls, `8px` and `12px` for compact
insets, `16px` and `24px` for groups and sections, and `28px`/`32px` for page framing. Reader prose,
when present, stays at `70ch` maximum; content titles and long labels wrap naturally. Source code and
tables keep local horizontal scrolling and receive keyboard focus without introducing page overflow;
recognized fenced code is displayed as plain code and does not invoke host code-block processors.
Reader image slots remain matte, accessible placeholders until the user activates one; activation
loads all known images for that Item through a bounded four-download queue. Summary disclosure state
is local to the Item and exact source version. Closing the disclosure preserves its ready annotation
for reopening, and ready text remains readable when generation is disabled.

## Elevation & Depth

Resting surfaces are matte and mostly flat. Panels separate through tone, rounded geometry, and
whitespace. Shadows belong to genuine overlays and the editor edge only. Action menus, Topic and
Search filter surfaces, feedback notes, and supported editor panes use the panel color before
optional glass; when `backdrop-filter` is available, those anchored surfaces switch to the theme
glass token and `blur(28px)` so the list context remains visible without turning the entire workspace
into glass. The Feed list header is a separate, lighter treatment: it stays opaque at rest and adopts
the same glass token with `blur(12px)` only when the list has scrolled. Dialogs stay opaque on their
panel background; only their native `rgb(0 0 0 / 42%)` backdrop dims the surrounding workspace.

### Shadow Vocabulary

- **Light overlay** (`0 16px 48px rgb(33 24 18 / 16%)`): action menus, feedback notes, and dialogs
  in light mode.
- **Dark overlay** (`0 20px 56px rgb(0 0 0 / 38%)`): the same overlays in dark mode.
- **Editor edge** (`-16px 0 48px rgb(0 0 0 / 18%)`): desktop editor pane separation; mobile removes
  this shadow.
- **Dialog backdrop** (`rgb(0 0 0 / 42%)`): the native dialog backdrop, distinct from elevation.

**The Real Overlay Rule.** Use depth to clarify an anchored menu, note, dialog, or editor pane. Do
not add shadows around every reading row or turn a matte field into a floating card.

## Shapes

Rounded geometry defines work zones, not every line of content. The wide shell header uses `20px`
corners and its narrow variant uses `16px`. List/detail panes and settings resource panels remain
`22px` at every leaf width; the desktop Settings navigation uses `18px`. Reading rows use `17px`.
Compact controls use `12px` or `13px`, menu items use `10px`, anchored menus use `15px`, and note
panels use `18px`. Chips use `8px`, textareas use `14px`, and checkboxes use `5px`. Borders are
absent on most controls; the settings/source list uses a quiet one-pixel line where a seam is
meaningful.

## Components

### Buttons

- **Primary:** peach fill, selected-row ink, `12px` radius, `10px 18px` padding, and `42px` minimum
  height. Hover uses the lighter peach. The compact Send note action retains its paper-plane icon
  and uses `10px` padding.
- **Secondary:** matte field fill, primary ink, `12px` radius, `10px 16px` padding, and `40px`
  minimum height. Hover uses the neutral hover field; disabled actions are `45%` opacity.
- **Icon:** icon-first controls use field fill, muted ink, `13px` radius, `40px` square dimensions,
  and `10px` padding. A coarse pointer expands the square and minimum height to `44px`. Familiar
  actions have an accessible name and native tooltip.
- **Focus and motion:** every focus-visible control gets a `2px` peach outline with `3px` offset.
  Hover state transitions use `150ms ease-out` for background, color, and border. Reduced motion
  sets scrolling to automatic and disables transitions and animations.

### Cards / Containers

List/detail panes, settings resource sections, and editor panes use panel backgrounds with `22px`
work-area corners; desktop Settings navigation uses `18px`. Feed cards are text-led and do not get a
decorative card border. A row shows its title, two-line excerpt, kind, and date; a selected row uses
peach fill, selected-row ink, `17px` corners, and `18px 16px` internal padding, while read-row titles
quiet to muted text. The persisted Unread view retains a row after it becomes read until the current
traversal is refreshed. Menu checkboxes additionally render a visible check mark; reading-row
selection itself is expressed by its peach fill and readable text contrast. Direct title activation
opens the reading pane and marks an unread Item read; the explicit Why this item details action opens
recommendation score, signals, pipeline version, and Item metadata without marking it read.

### Inputs / Fields

Inputs, textareas, and selects use a borderless matte field, `13px` corners, `12px 14px` padding,
and a `44px` minimum height. Settings form labels are `0.8125em`, muted, and sit above fields with
a `9px` gap. Native Snapshot radios and checkboxes remain `16px` controls inside `44px` label
targets, with the peach accent and no custom glyph replacement. The feedback note's label is `1em`
within its `14px` note-panel context; its textarea
uses `14px` padding and the host text-size fallback (`var(--font-text-size, 16px)`) with `1.6`
line-height. Checkbox marks use a `16px` control with `5px` corners and the peach accent. Focus uses
the shared `2px`/`3px` outline; placeholders use muted text at full opacity.

### Navigation

The shell header places the Topic Stream brand, three primary reading tabs, and quiet Tasks/Settings
icon slots in one compact row. The Digests tab can carry a compact peach New-count badge; the Tasks
slot opens Task Center without changing the primary reading-tab model. Primary tabs are transparent at
rest, use `13px` corners and `40px` minimum height, and switch to the ink-filled selected treatment;
when space is constrained, complete tab controls wrap while their labels remain whole. Settings uses a `148px` desktop
vertical navigation with `12px` corners; the current section uses a restrained peach wash and peach
text. Headings and resource actions wrap as groups. Below `48em`, the desktop nav is replaced by a
labelled native section picker, and the resource/editor surface stretches to the full available width.

### Anchored Menus

The ellipsis trigger opens a fixed, portalled `208px` menu anchored to the trigger. One overflow owns
Useful and Not for me as `menuitemcheckbox` choices, Why this item, Open original, Mark as read or
Mark as unread, Add to Topic, Wrong topic, automatic Topic matching, and Hide from all Feeds. Add note
or Edit note appears only after a rating and is separated from the choices. Arrow keys, Home/End,
Escape, Tab, outside pointer, scroll, and resize preserve the menu's focus and anchor semantics.
Menu hover/focus uses the theme's peach-soft wash. This is a real anchored operation, not a
permanently visible row of outlined buttons.

### Feedback Note

Choosing Useful or Not for me commits the rating immediately and opens a nonmodal, anchored `360px`
note panel beside that Item's trigger. The panel title reflects the sentiment and the field reads
“Optional note for future recommendations.” Only the paper-plane Send note action sends text. Close,
outside click, Escape, Tab exit, scroll, and resize preserve the rating; an unsent draft remains in
that Item's session and reopens through Add note/Edit note. Choosing Not for me in a Topic shows a
transient Wrong topic action in this same panel; it applies a Topic correction without replacing the
rating. Pending and error feedback stays local to the panel, canonical committed responses preserve
the saved intent while showing retrieval-update-pending status, and a successful note send closes it.
There is no permanent inline Add a note control and the note editor is not a modal dialog.

### Search

Search is a quiet, text-led collection surface: the `Search` field and `Filters` action sit above
rows that reuse the Feed's title, excerpt, metadata, and overflow treatment. The search field submits
explicitly; the anchored Filters surface is nonmodal, glass-backed when supported, and exposes Topic,
Source, UTC publication dates, Clear filters, and Apply filters. Requests load `20` Items at a time,
with a `100`-Item session cap and a local retry state for unavailable results or filter options. The
selected result opens the same reading/detail composition; an Item action notice remains near the
Reader title and is scoped to that Item.

### Reader

The canonical Reader puts the Item title first, with source headings subordinate inside host-native
reading prose. The reading body is bounded to `70ch`; `pre` and `table` regions are keyboard-focusable
and scroll locally, while recognized fenced code stays plain and cannot launch host code-block
processors. Images begin as matte, labelled placeholders with no thumbnail fetch. The first
activation consents to all known images for that Item, downloads at most four concurrently, and keeps
partial failures recoverable through a local Retry image action; object URLs are released when the
view closes.

The compact, neutral `AI summary` button sits near the Reader title; list contexts use an overflow
entry where the Item supports it. Its accessible labelled disclosure is local to the Item and exact
source version: pending status appears before prose, ready text remains readable after opt-out,
generation is always explicit, and failed work offers Job Retry while status recovery is read-only.
Summary arrival preserves the current source, focus, and reading position; a source-version change
collapses the previous disclosure rather than showing stale text.

### Digest archive, Edition, and captured Reader

The Digest archive is a text-led collection surface with a definition select and one clear Build
action. The Build menu lists saved definitions, marks disabled definitions as “Manual only,” and
hands accepted work to the shared Job model; each definition keeps local requesting/queued/running,
uncertain, unavailable, and terminal history, with Check status when a Job ID is available. A successful Build exposes a
single Open built issue result; result lookup failures remain local and retryable. Archive rows show
Issue title, definition, generated date, partial status, and a peach New badge when the device-local
readiness model has not seen the Issue. The first successful unfiltered archive head establishes the
baseline once per device/backend/Workspace; later mount/manual REST reads reconstruct unopened
post-baseline Issues without catch-up Notices, while a live host Notice/event can add an Issue after
that baseline. Badges do not imply server-side reading state.

The Edition keeps the Issue title and generated date primary, uses quiet section-index links and
topic headings, and preserves captured summaries or captured excerpts without inventing content.
Its compact contents pane replaces the archive while an Item is open; current contents use peach
selection, and Back restores the Edition's prior selection and focus. The captured Reader reuses the
canonical title-first Item card, explicit details, feedback, and exact-version Summary behavior.

### Vault Snapshot form

The Snapshot form is a single native modal reached from Item and captured Digest Item overflow menus
or the whole Issue `Issue actions` menu. It asks for a normalized folder and filename, then uses
native Compact/Full radios; image mode controls (Links, Remote, Local) appear only for Full content.
An individual Item without article body/README cannot save Full: the form explains why, hides image
controls, disables Save, and offers `Use Compact`. A mixed Full Digest shows its missing count and
requires explicit checkbox consent before saving; missing bodies are marked `Excerpt only` in order.
Ready AI text is included only when it matches the captured Item source version. The workflow is
portable and bounded: local attachments use the per-save file/size budgets, SVG bytes are sanitized,
existing paths are rejected before writes, and newly created attachments are rolled back on note
failure. Pending and errors stay local to the form, and Save does not generate, wait for, or later
mirror AI work. Closing or completing the form returns focus to the invoking overflow trigger.

### Task Center and shared Job progress

Task Center is the shared Job history: a status filter, 30-row pages, queue/status metadata, and a
detail view with authoritative status, attempts, timestamps, duration, reason, and model-transition
context. Active Jobs expose a familiar Cancel icon with a native confirmation dialog. Ordinary
dead/cancelled Jobs expose Retry; model-preparation Jobs additionally require a verified current,
eligible model target. Generated Digest Jobs can
open their saved Issue; a committed-result conflict is treated as a saved result, not a duplicate.

Shared Job progress shows phase plus an initial-cohort count (`N / N` when known, or “total not known
yet”), never an invented percentage or ETA. Normal progress fills use the peach accent. Failed and
cancelled history use a muted fill. WebKit and Gecko use separate progress pseudo-elements, while the
status label remains authoritative: a full `N / N` bar does not imply Job success. Settings and Task
Center observe the same Job queries and host event port, with explicit Refresh recovery when an event
or REST read is unavailable.

**The Status-Authority Rule.** Treat the Job status label as the outcome; the progress bar only
describes the observed initial cohort. Never infer success from a full bar or a count alone.

## Do's and Don'ts

### Do:

- **Do** derive new surfaces from the black/charcoal work-area hierarchy and the light-mode pale
  equivalent; keep fields matte and readable.
- **Do** reserve `#F2B38A` and `#F7C5A6` for action, selection, checked/pressed, hover, and focus
  states; retain the light-theme accent ink where contrast requires it.
- **Do** preserve host typography, theme switching, keyboard focus, `44px` coarse-pointer targets,
  and the `48em` leaf breakpoint.
- **Do** keep menus, filters, and note panels anchored to their trigger, with panel fallback colors
  when blur is unavailable and `blur(28px)` only when supported.
- **Do** keep the Feed header opaque at rest and use the existing glass token with `blur(12px)` only
  while its list is scrolled; preserve the opaque fallback and the `32px` header clearance.
- **Do** preserve the Reader's title-first hierarchy, `70ch` prose measure, local code/table scroll,
  consent-gated images, and exact-version explicit summary disclosure.
- **Do** keep Search text-led with quiet Search + Filters controls, UTC date semantics, 20-result
  pages, and a 100-result session limit.
- **Do** keep Digest archive and Edition surfaces text-led, preserve compact contents and captured
  Reader return/focus, and show New Issue badges only for unseen device-scoped saved Issues.
- **Do** keep Task Center and Settings on shared Job queries; use peach normal progress, muted
  failed/cancelled history, and explicit Cancel/Retry/model recovery actions.
- **Do** keep view-owned browser Fetch/ReadableStream SSE advisory: bind reconnect and pop-out
  lifecycle to the active view, reconcile REST authoritatively, and retain loaded content plus explicit
  Refresh when streaming is unavailable.
- **Do** keep Snapshot saves explicit and portable: normalize paths, reject existing destinations,
  preserve captured content, honor image budgets and safe SVG handling, and restore focus to the
  originating Item or Issue action after the native form closes.
- **Do** keep ready AI annotations source-version-bound and readable in Compact or Full without
  generating, waiting, or mirroring later work; keep mixed Full consent and `Excerpt only` labels
  visible when they apply.
- **Do** keep one clear action per view and use explicit text for unfamiliar distinctions.
- **Do** keep Storybook evidence connected to production components, mocking only external service
  ports and using synthetic English/Spanish demonstration data.

### Don't:

- **Don't** invent a competing palette or direction, or treat the pinned reference as a source of
  random generated comps.
- **Don't** add decorative gradients, glowing edges, ornamental glass, nested card borders, or
  shadows around every row.
- **Don't** replace real anchored menus with a permanent inline Add a note control, a modal note
  editor, or a row of outlined secondary actions.
- **Don't** claim physical mobile-device parity from responsive frames, browser dependency checks, or
  desktop host tests. Browser Fetch/ReadableStream SSE and native desktop main/pop-out lifecycle are
  shipped and verified, while physical iOS/Android streaming remains untested.
- **Don't** silently save an excerpt under Full, expose Full-only image controls when an individual
  Item has no full text, overwrite an existing path, or imply that linked/failed images were stored
  locally.
- **Don't** trigger Summary generation, waiting, Retry, or late note mutation from Snapshot Save;
  capture only a ready annotation matching the saved Item input version.
- **Don't** infer Job success from `N / N`; preserve separate peach normal and muted failed/cancelled
  progress history fills with the status label as the authority.
- **Don't** fetch thumbnails before image consent, turn a ready summary into a generated headline, or
  display summary text from a different source version.
- **Don't** claim excerpts are generated summaries or add unsupported intelligence, KPI, ETA, or
  operator-tuning UI.
- **Don't** use the exploratory raster as a pixel-bound asset; keep the implementation code-led and
  preserve its provenance.
