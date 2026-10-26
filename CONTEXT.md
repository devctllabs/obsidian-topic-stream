# Topic Stream

Topic Stream turns configured information sources into a personal, topic-organized stream and immutable digest issues.

## Language

**Workspace**:
The single user-owned scope containing every source, topic, item, preference, and digest.

**Workspace Settings**:
The non-secret, user-configurable behavior of one Workspace, including retrieval, generation, and retention. Worker capacity is operator configuration; credentials and other secrets are not Workspace Settings.

**Source**:
A configured upstream stream that can be polled for entries.
_Avoid_: Feed, subscription

**Source Entry**:
An upstream occurrence discovered in a Source and preserved as provenance for a canonical Item.

**Item**:
A canonical piece of content that may have been observed through one or more Source Entries. Its kind is Article, Paper, Video, or Repository.
_Avoid_: News, post, document

**Item AI Summary**:
An optional generated annotation for one exact supported Item content version, independent of its ordinary excerpt and immutable Digest Issue facts.

**Summary Source Version**:
The identity of the exact article body or repository README summarized for an Item, including a version saved with a Digest Issue.

**Topic**:
A saved thematic subscription with source and Item-kind scope plus optional natural-language Include and Exclude criteria.
_Avoid_: Rubric, category

**Topic Revision**:
An immutable semantic version of a Topic used to explain historical Feed Publications and Digest Issues.

**Feed**:
A Workspace-owned ordered stream of published Item recommendations. All Topics shows the stream,
and a Topic shows its matching subset with the same publication time and relative order.

**Feed Candidate**:
An unpublished Item prepared for consideration by a Topic. An Item may be a candidate for several
Topics without becoming several publications.

**Feed Publication**:
The one-time inclusion of an Item in its Workspace's Feed, shared by All Topics and matching Topics.

**Feed Batch**:
An ordered group of Feed Publications created together by one Refresh.

**Digest Definition**:
A reusable ordered composition of Topics, per-Topic item limits, and one recurrence rule.

**Digest Issue**:
An immutable result produced from one Digest Definition at a particular time.
_Avoid_: Digest run

**Personal Feedback**:
The current Like or Dislike decision for an Item, optionally accompanied by a comment.
_Avoid_: Feedback event

**Topic Membership Correction**:
An explicit Add to topic or Wrong topic judgment that overrides automatic semantic membership.

**Topic Correction Epoch**:
The current generation of a Topic's Membership Corrections. It advances only when the user explicitly clears the complete correction set.

**Topic Gate Calibration**:
The effective Include and Exclude similarity boundaries for one Topic and Embedding Model, learned from current Topic Membership Corrections or initialized from bootstrap values.

**Reading State**:
The current read or unread status of an Item. Read means the user deliberately opened Reader, opened the original, or marked it read.

**Item Visibility**:
The current visible or hidden status of an Item. Hiding does not express personal preference.
_Avoid_: Dismissal

**Preference Profile**:
User-written global Prefer and Avoid guidance plus representative current feedback used by optional personalized reranking.

**Embedding Model**:
A named, server-managed identity for immutable embedding semantics, including weights, preprocessing, provider model, and vector dimensions. Its endpoint can move while those semantics remain the same.

**Desired Embedding Model**:
The Embedding Model selected in Workspace Settings. It may differ temporarily from the model still serving retrieval while Topic Stream rebuilds the derived index.

**Serving Embedding Model**:
The ready Embedding Model whose derived index currently answers retrieval for a Workspace.

**Model Transition**:
The initialization or incremental synchronization and cutover that moves one Workspace to its latest Desired Embedding Model without making a partial index visible.

**Retained Embedding Model**:
An Embedding Model whose Workspace synchronization progress remains available for a later return, including changes accumulated while it is inactive.

**Collection Incarnation**:
One instance of an Embedding Model's derived collection. Replacing that instance invalidates synchronization progress bound to it.

**Job Queue**:
A closed execution class persisted with every Job that selects its worker capacity and retry policy.
The classes are Fetch, Generation, Retrieval, Feed Preparation, and Maintenance.

**Runtime Role**:
An independently enabled responsibility of `topic-stream serve`: HTTP, Schedulers, or Workers. HTTP and
Schedulers operate one selected Workspace; Workers claim Jobs for every Workspace.

**Reader**:
The optional full-content view rendered from canonical cleaned Markdown inside the Topic Stream Obsidian view.

**Vault Snapshot**:
An independent Markdown file created by an explicit Save to vault action. It is not synchronized back to the backend.
