# Ranking and personalization

Topics decide which Items qualify; Personal Feedback helps order and select the qualified Items.
Feedback does not generate unrelated candidates or bypass Topic boundaries. Each recommendation
includes an explanation of why it was selected.

## Topic membership

A Topic combines Source and Item-kind scope, optional natural-language Include and Exclude criteria,
and ordinary or strict matching. Without Include text, recent in-scope Items are considered.
Exclude rejects matches and never finds additional content.

Add to topic and Wrong topic override automatic membership for eligible Items. Corrections survive
ordinary Topic edits unless explicitly cleared. They are independent of Like/Dislike: a Liked Item
marked Wrong topic is rejected from that Topic; Add to topic does not override a Dislike.

Corrections also teach the automatic Topic boundary. Learning begins after five Add and five Wrong
topic examples. Recalculation happens asynchronously; a new Topic or changed model can temporarily
use initial thresholds. Ordinary mode favors broader coverage, while strict mode favors precision.

## Personal preference

Like and Dislike evidence activates independently after five current decisions of each kind.
Evidence from similar Items weakens with age. Comments are retained but deterministic scoring
ignores them; optional Digest reranking can use selected current feedback comments and the
Preference Profile's written Prefer/Avoid guidance.

A direct Dislike or strong related negative evidence suppresses future Feed and Digest selection.
A direct Like can rescue an Item from indirect negative evidence. Search keeps matches available
and uses preference only for bounded reordering. Existing Feed Publications keep their saved order
and explanations; changing feedback does not rerank history.

## Feed, Search, and Digest

- Feed uses bounded prepared Topic candidates, balances Topics and Sources, and personalizes the
  common selection once. Refresh can return short results without adding filler.
- Search supports lexical, semantic, and hybrid matching. It does not apply Feed/Digest freshness
  bonuses or suppress hidden and Disliked matches.
- Digest ranks each Topic and can optionally use an LLM to reorder its shortlist. Invalid or
  unavailable LLM output preserves deterministic ranking. Duplicate Items appear in one section.

The candidate work limit defaults to 50 and is at most 100. It is separate from the display page
size. Increasing page size does not expand the recommendation candidate pool.

For formulas, calibration, fusion, versioned components, and data flow, see the
[ranking pipeline](../../architecture/ranking-pipeline.md). See also
[Feed and feedback](feed-and-feedback.md) and [Digests](digests-and-schedules.md).
