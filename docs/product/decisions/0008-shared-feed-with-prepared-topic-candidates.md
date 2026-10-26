# Share Feed history and prepare Topic candidates in the background

Status: Accepted
Date: 2026-10-09
Supersedes: [PDR-0007](0007-stable-feed-publication.md)

The display-expiry rule below was superseded on 2026-10-18 by
[PDR-0009](0009-browse-feed-history-without-display-expiry.md). Other shared-history decisions remain accepted.

Each Workspace has one stable Feed publication history; All Topics and Topic views share its
publication times and order. Background work prepares bounded Topic candidate lists, while Refresh
selects a bounded common pool, applies personalization once, and prepends new publications. This
keeps reading predictable and removes per-Topic retrieval and personalization from All Refresh,
accepting temporary candidate lag and short results in exchange for bounded interactive work.

## Considered Options

- Independent All/Topic histories: permit independent discovery and timing, but require distinct
  publication exclusions and candidate preparation for each history.
- Limit All to selected Topics: reduces work by changing coverage; All instead retains every
  enabled Topic.
- Publish All in background Jobs: makes the Feed advance without Refresh; background preparation
  instead leaves publication under the established first-visit/manual Refresh lifecycle.
- Wait for every Topic to become fresh: improves completeness but makes Refresh depend on the
  slowest preparation. Refresh instead uses compatible ready lists, even when some are absent.

## Consequences

- An Item publishes once per Workspace. Refreshing one Topic adds its new Items to All and any
  other Topics already known to match; Refresh All combines candidates from all enabled Topics.
  Duplicates occupy one candidate slot and receive one personal assessment.
- Each Topic's candidate list is replaced, not appended indefinitely. `candidateLimit` defaults
  to 50 and is at most 100; it also bounds the deduplicated Refresh pool. No refill follows
  rejection or concurrent deduplication. Topic balance offers coverage without guaranteeing quotas.
- Preparation coalesces input changes for up to one minute from the first change. A Topic's old
  list becomes unavailable after incompatible changes. Cold Refresh does not wait, and completed
  preparation never publishes later on the user's behalf.
- Saved matching Topics reflect knowledge at publication. New or edited Topics do not
  automatically attach old publications. Explicit Add/Wrong topic corrections change their
  displayed membership at the original position; Hide masks an Item across All and Topics.
- When all original matches are explicitly Wrong and no effective manual Add remains, All masks
  that publication. Adding another Topic restores the unexpired card in All and that Topic.
  Clearing a correction restores the saved membership; expiry is never renewed.
- Admission uses the preceding 30 days of content age. Display lasts 30 days from the single
  publication time. The compact publication fact remains indefinitely, including after expiry
  or repeated ingestion from another Source. Older and rejected Items remain searchable.
- Automatic recommendation changes affect future admission. Existing publications retain their
  order and original explanation facts; direct Dislike does not hide an already published card.
- Loaded All/Topic views keep separate snapshots and scroll positions. A publication elsewhere
  does not inject cards into them; explicit Refresh reads the latest shared history.

Implementation note (2026-10-21): The backend/API is delivered. Current behavior is described in
[Feed and feedback](../capabilities/feed-and-feedback.md); technical mechanics are in the
[ranking pipeline](../../architecture/ranking-pipeline.md). The accepted decision and its
historical alternatives remain unchanged.
