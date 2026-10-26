# Preserve Feed order across Refresh with one-time publication

Status: Superseded by [PDR-0008](0008-shared-feed-with-prepared-topic-candidates.md)
Date: 2026-10-03
Supersedes: [PDR-0001](0001-stable-feed-until-refresh.md)

Each Feed will preserve its published order across Refresh, devices, and backend restarts by
prepending newly ranked Feed Batches rather than recalculating the old list. An Item may be published
once per Feed and remains visible for 30 days from that Feed Publication, while older content stays
accessible through Search. This trades continuous optimization of the entire list for predictable
reading and keeps Feed focused on current reading rather than an unlimited browsing archive.

## Considered Options

- Recalculate the whole Feed on Refresh: keeps all recommendations current, but moves or removes
  familiar cards and loses the user's place in the sequence.
- Use chronology alone: gives a stable order, but gives up ranking within new arrivals.
- Keep every published card visible indefinitely: duplicates the archive role already served by
  Search and makes Feed history grow without a useful reading boundary.

## Consequences

- All Topics and each Topic have independent Feed Publications; publishing in one does not consume
  an Item for another.
- An Item not previously published in a Feed may enter a later Batch after recommendations change,
  even without a new Source arrival. Admission still uses the recent 30-day content window.
- Automatic feedback, Topic, calibration, and model changes affect future Batches. Explicit Hide
  and Wrong topic actions may hide existing cards without changing their saved positions.
- The first visit to each Feed in an open Topic Stream view automatically refreshes it; returning to
  that Feed reuses its loaded state. Further publication requires the Refresh button.
- A compact publication record must outlive the visible card to prevent repeated publication.
  The 30-day window limits Feed visibility, not canonical Item retention or that record's lifetime.

This record preserves the superseded design. See
[PDR-0008](0008-shared-feed-with-prepared-topic-candidates.md) and
[PDR-0009](0009-browse-feed-history-without-display-expiry.md) for the accepted shared history
and unlimited published-history behavior.
