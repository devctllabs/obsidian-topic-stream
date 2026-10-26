---
name: Topic Stream — Signal Desk
description: A calm, precise reading workspace with deliberate visual hierarchy and cool, restrained accents.
---

<!-- SEED: established with the user before implementation; re-run $impeccable document once there's code to capture the actual tokens and components. -->

# Design System: Topic Stream

## Overview

**Creative North Star: "Signal Desk"**

Signal Desk is a light-feeling, technically precise reading workspace. Readwise Reader is a
reference for tonal restraint and reading focus, not a screen to copy. Beauty comes from clear
typographic roles, controlled density, generous reading space, and a few unmistakable accents.
The interface must feel composed rather than uniformly muted: the eye always has a point of entry.

This is the accepted replacement direction, not a description of the shipped plugin. It supersedes
the incumbent warm-paper/cobalt appearance. These invariants guide implementation; exact color,
spacing, radius, and component tokens will be extracted from the reviewed implementation rather
than fabricated here. This seed records the accepted visual direction; the
[current Obsidian experience](docs/product/capabilities/obsidian-feed.md) describes the available
plugin behavior.

**Key Characteristics:**

- Cool graphite and blue-gray neutrals, with a cool off-white light counterpart.
- One restrained periwinkle-blue accent with a clear interactive or selection purpose.
- Strong primary, secondary, and supporting type roles; quiet does not mean flat.
- Matte, mostly flat surfaces; separation through tone, spacing, and subtle seams.
- Obsidian-native typography, focus, theme integration, and desktop/mobile usability.

## Colors

### Primary

Use a cool periwinkle-blue for active navigation, selected controls, the main action, and link
hover/focus. Selection can combine a restrained tonal wash with a thin accent marker. Do not make
every content title accent-colored at rest: repeated color would erase the intended hierarchy.
Focus must remain visible independently of hover, and selection must not rely on color alone.

### Neutral

Dark mode uses graphite and blue-gray surfaces, off-white primary text, and softer supporting text.
Light mode uses cool off-white surfaces, graphite primary text, and readable muted text. Follow
Obsidian's active light/dark mode; neither mode is a secondary or reduced-quality variant.

Exact values remain to be resolved during implementation against host CSS variables and contrast
checks. The plugin-owned root contains all visual overrides; other Obsidian surfaces remain intact.
Error and attention states retain semantic host colors and a textual explanation.

**The Purposeful Accent Rule.** An accent identifies a current selection, an action, or a focus
state; it is not distributed evenly as decoration. Yellow, ochre, amber, and gold are excluded from
the brand accent palette. This does not prohibit host-native semantic warning treatment.

## Typography

Use Obsidian's interface font stack for neutral, readable UI typography, with no external font
downloads. Reader content respects the host's reading typography and scaling. Keep interface copy
in sentence case; avoid decorative uppercase and excessively tracked labels.

The following are target sizes at the normal host scale, not extracted CSS tokens. Implement them
with scalable units and preserve host font preferences and zoom:

- **Primary title:** roughly 30–32 px, semibold. One dominant title in the active content pane.
- **Content title:** roughly 18–20 px, medium or semibold, visibly subordinate to the primary title.
- **Body / summary:** roughly 16–18 px, regular, with comfortable line spacing and softer foreground.
- **Section label / metadata:** roughly 12–14 px, readable but quiet; a Topic label is not another headline.
- **Navigation list title:** roughly 15–16 px, medium; selection carries emphasis without display-sized type.

**The One Focal Title Rule.** In a Digest Issue, the edition title leads; the Digest Definition name
and date are supporting metadata, Topics are section markers, and Item titles form the second
level. In Reader, the opened Item title becomes the focal title and adjacent contents recede.
Preserve the backend's actual titles; hierarchy does not require a new editorial headline or API field.

Optional AI text uses a quiet, explicit `AI summary` label and the normal body role, never a
second focal headline. Pending and error labels stay small, readable and contextual; no sparkle
branding, promotional badge or fabricated progress is needed to identify generated text.

## Layout

Use compact application chrome around the working content. Primary navigation stays easy to scan,
while management controls do not compete with reading. The first viewport must show useful content,
not oversized branding, statistics, or a decorative dashboard header.

Wide containers support a list/detail composition, approximately one third navigation and two thirds
reading space where content permits. Narrow containers and mobile use a single active pane with a
clear Back action and restoration of selection, scroll, and focus. Choose the layout from the
Obsidian leaf's available space, not only the outer window width.

Reader prose should have a comfortable measure around 60–70 characters. Use more space between
sections than between a title and its supporting summary, with a consistent underlying spacing
rhythm. Long titles wrap naturally without forcing sideways page scrolling or shrinking all text.
Content lists remain text-led; cover thumbnails do not belong in Feed, Search, or Digest lists.

An optional summary is an inline disclosure belonging to its Item, not another floating card or
global panel. In Reader it precedes the original article; in lists/editions it stays within the
Item's reading rhythm. Distinguish it from the ordinary excerpt/body with a quiet label, whitespace
and a subtle seam. Preserve the scroll anchor/focus while pending text becomes ready. Exact
entry, generation, opt-in and saved-version behavior follow the backend's
[Item AI summary rules](docs/product/capabilities/item-ai-summaries.md). The inline controls here
describe the target UI; they are not yet available in the plugin.

Background progress stays inside the existing model/Job context rather than becoming a monitoring
dashboard. Use a compact status line with a quiet phase label and N/M count; a thin, restrained
accent bar belongs to a known initial indexing cohort only. Preparation, catch-up and activation
remain distinct waiting states. Completion is not inferred from a visually full bar, and unknown
work must not acquire an invented overall percentage or ETA. Full history belongs in Task Center;
the Settings block returns to the ordinary active-model state after activation.

Stop, Retry and Details form a small group of familiar host-native icons on that status line.
Avoid heavy button frames and permanently red destructive controls; use restrained hover/focus
feedback, visible keyboard focus, accessible names/tooltips and generous touch hit areas. Pending
is local and readable, not a decorative animation replacing the whole screen. Narrow layouts may
wrap status text while preserving clear action targets. These are target visual rules, not a claim
that progress controls or the separately planned recovery API are implemented.

## Elevation & Depth

Surfaces are matte and flat at rest. Tonal differences, subtle seams, and whitespace establish
structure; avoid nested card borders and shadows around every row. Reserve elevation for genuine
overlays, respecting Obsidian's native menus and dialogs. No decorative gradients, glowing edges,
or ornamental glass effects.

## Shapes

Use restrained, consistent corner treatment on controls and overlays. Content rows and article
sections should not become a collection of floating rounded cards. Exact radii remain unresolved
until implementation; native host controls keep their expected form and behavior.

## Do's and Don'ts

- **Do** make the primary title, content titles, Topic labels, and metadata distinguishable before reading their words.
- **Do** keep familiar toolbar actions icon-first, with accessible names, tooltips, visible focus, and usable touch targets.
- **Do** keep explicit text for unfamiliar actions and for distinctions such as Hide versus Wrong topic.
- **Do** preserve the reading position and keep asynchronous feedback near the action that caused it.
- **Do** use brief, purposeful state transitions, respect reduced motion, and keep content visible without entrance animation.
- **Do** keep summary disclosures visually subordinate to the Item title, explicitly labeled and readable in both themes without replacing source content.
- **Do** give progress and icon-controls a compact, consistent rhythm without competing with reading titles; respect reduced motion and announce meaningful phase changes without per-counter screen-reader noise.
- **Don't** solve weak hierarchy by coloring every title, adding more badges, or boxing every content group.
- **Don't** introduce permanent administrative sidebars, KPI widgets, oversized branding, or a persistent global offline banner.
- **Don't** simulate blurred image previews when no thumbnail has been fetched, or fetch article images without consent.
- **Don't** claim that excerpts are AI summaries or that planned screens are already implemented.
- **Don't** expand other reading contexts or scroll the user to an AI result when background work completes.
- **Don't** turn initial-cohort completion into an overall success indicator, add an ETA without evidence, or expose operator tuning/benchmark widgets as routine product UI.
- **Don't** treat the exploratory mockups as pixel-bound assets or a reason to generate more images; the build path remains code-led.
