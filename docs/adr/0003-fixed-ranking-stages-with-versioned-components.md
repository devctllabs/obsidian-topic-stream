# Use fixed ranking stages with versioned replaceable components

Ranking follows fixed retrieval, Topic membership, fusion, personalization, recency, optional rerank, and diversification stages. Personal Feedback scores only the candidate pool accepted by Topic membership; it is deliberately not a separate candidate generator, trading some discovery recall for clear Topic boundaries and bounded work. Each algorithm and parameter set is versioned, but v0.1 does not implement a generic DAG or plugin framework; this preserves experimentability without building a workflow engine. Digest LLM output is fused with deterministic rank and cannot replace the candidate set; invalid or unavailable output falls back locally. Ranked candidates return compact explain signals, and the Topic result returns the active component versions. v0.1 does not persist ephemeral Feed, Search, or Digest ranking executions; an immutable product artifact such as a Digest Issue stores only the selected display data it owns.

[ADR-0010](0010-shared-feed-and-bounded-candidate-preparation.md), replacing
[ADR-0009](0009-persisted-feed-publication.md), supersedes the Feed persistence constraint for the
accepted shared Feed change. Feed Publications become owned product records containing selected
Items and their ordering/explanation facts; generic ranking-execution history is still excluded.
The fixed pipeline and the Search/Digest boundaries above continue to apply. The shared Feed
backend/API is implemented; current mechanics are described in the
[ranking pipeline](../architecture/ranking-pipeline.md).
