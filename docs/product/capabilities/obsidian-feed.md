# Obsidian experience

The current plugin opens one Feed view through its ribbon action or **Open feed** command.
Its Server URL setting connects to the backend. It provides Topic creation with scope and
Include/Exclude criteria, and **Sync all now** with the returned Job ID. The Source form sends
the old request shape and cannot create Sources against the current contract; use the
[Source creation API example](../../guides/getting-started.md#get-your-first-feed).

The view reads saved Feed history and loads older pages as the user scrolls. **Refresh** rereads
the latest saved history through GET; the current plugin does not invoke the backend's POST
Refresh publication operation or publish on first visit. Publish through the API using the
[first Feed instructions](../../guides/getting-started.md#get-your-first-feed).

Cards show metadata, an excerpt and recommendation explanation. Item titles open the original URL.
**Useful** and **Not for me** save Like/Dislike feedback with an optional comment.

Topic Feed switching, Topic corrections, Reading State and visibility controls, Search, Reader,
Digests, Task Center, Workspace Settings, Item AI summaries and Vault Snapshot writes are not yet
available in the plugin. The current implementation does not establish the complete responsive
desktop/mobile experience or SSE reconciliation.

See [Using Topic Stream](../../guides/using-topic-stream.md) for available controls. The implemented
server behavior is described in [Feed and feedback](feed-and-feedback.md) and the other
[backend capabilities](../README.md#capabilities).
