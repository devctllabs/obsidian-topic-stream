# Request Item AI summaries explicitly and preserve saved annotations

Status: Accepted
Date: 2026-10-23

Item AI summaries are optional asynchronous annotations requested explicitly for supported Item
content; automatic generation on opening, Feed preparation or Digest Build was rejected to keep
reading and publication independent of model latency and cost. A result may fill a missing
annotation for a saved Digest Item version, but ready text and immutable Issue facts stay unchanged
so retries and later content/settings changes cannot rewrite an edition. This complements the
[immutable Digest decision](0003-immutable-topic-sectioned-digests.md); the
[current capability](../capabilities/item-ai-summaries.md) defines requests and reuse.
