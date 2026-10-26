# Feed and feedback

Each Workspace has one saved Feed history. All Topics shows that history; a Topic shows its matching
subset with the same publication order. An Item can be published once in its Workspace, even when
several Topics or Sources find it. Refresh adds a new Batch above older cards and preserves their
order, score explanation, and original publication time across devices and backend restarts.

Opening a Feed reads saved Publications. The first page defaults to 25 Items. Its opaque snapshot
and cursor keep older-page traversal below the same shared head while other clients publish newer
Items. A request without a snapshot reads the latest head. Published history has no display expiry:
Load more can reach arbitrarily old cards.

Refresh can target All Topics or one Topic. It publishes eligible new Items and returns the latest
saved page. The page size controls display, independently of the
candidate work budget. Empty admission still returns history published through another selection.
Admission uses content publication time, falling back to discovery, within the last 30 days. Future
dates wait until eligible; this admission window does not remove older Publications.

Enabled Topics prepare bounded candidate lists in the background. All Refresh balances the ready
lists and counts duplicate Item IDs once before the common limit. Topic Refresh selects its own
list while retaining already-known matches from all compatible ready Topics. Personal preference,
freshness, and soft Source balancing apply once to the common pool. Short results stay short.
With enabled but unready or empty Topics, Refresh returns saved history without filler or waiting.
With zero enabled Topics, it uses recent unpublished Items in chronological order. Background
preparation never publishes cards on its own.

Saved matching Topics define historical membership. Creating or editing a Topic does not
reclassify old Publications automatically. Current `Add to topic` reveals an already-published Item
in that Topic at its original position, including old cards. `Wrong topic` removes only that Topic
match. When every original match is Wrong and no manual Add remains, All hides the card too. Add to
another Topic restores it; clearing a correction falls back to saved membership. Chronological
Publications remain in All when Topics are later created. Corrections do not republish or invent a
new admission explanation.

Hide masks a card in all Feed views and preserves access through Search. Unhide restores the same
publication position and time. Hide is independent of Topic membership and Personal Feedback.
A Dislike suppresses future admission and does not remove an already-published card.

Like or Dislike is current state. Changing sentiment preserves `createdAt`, updates `updatedAt`,
and replaces the optional comment. An identical write changes neither feedback time nor projection
revision. Evidence activates independently after five current decisions of each polarity. Comments
are retained and ignored by deterministic scoring. Feedback and Topic corrections are saved before their search representation updates. If delivery
is delayed, the saved choice remains effective and retrying it does not make the feedback newer. Reading state and
visibility remain independent.

Background notifications do not insert a card into an already-loaded Feed snapshot. Refresh
explicitly to see the latest shared history.

See [ranking and filtering](../../architecture/ranking-pipeline.md),
[shared Feed decision](../decisions/0008-shared-feed-with-prepared-topic-candidates.md), and
[unlimited published history](../decisions/0009-browse-feed-history-without-display-expiry.md).
