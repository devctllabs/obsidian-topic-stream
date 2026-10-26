# Using Topic Stream

The current Obsidian plugin provides a Feed view, Topic creation, Sync all, original links and
Like/Dislike feedback. Its Source form uses an outdated request shape, so create Sources through
the API. This guide describes the available workflow.
The backend's other capabilities are accessible through its API.

## Choose Sources and Topics

Create an RSS/Atom Source using the [API example](getting-started.md#get-your-first-feed), then
add a Topic in the plugin with a name, optional Include and
Exclude descriptions, Source scope, Item kinds and ordinary or strict matching mode. An empty
scope considers all Sources or kinds. The plugin does not provide the full resource-management
interface.

Choose **Sync all now** to enqueue a synchronization request for enabled Sources. The view shows
the returned Job ID; fetching, indexing and Topic preparation happen in the background. Individual
Source synchronization and Job inspection are available through the API.

## Read saved Feed history

Follow the [first Feed steps](getting-started.md#get-your-first-feed) to publish recommendations
through `POST /api/v1/feed/refresh`. Publication adds eligible Items above saved history and
preserves the order of older cards. Preparation may still be running when a request returns.

The plugin's **Refresh** button rereads that saved history through GET. It cannot publish a new
Batch. The view loads older pages as you scroll and currently has no Topic Feed selector.
Opening an Item title follows its original URL; it does not open an in-plugin Reader or record
Reading State through the backend.

## Give feedback

Use **Useful** or **Not for me** to save Like or Dislike feedback. An optional comment accompanies
that decision. Feedback is your current choice; another choice replaces it. A Dislike affects
future recommendations and does not hide an already-published card.

Personalization begins using Like and Dislike evidence independently after five current decisions
of each kind. Deterministic scoring ignores comments. Topic corrections, Reading State and
Hide/Unhide are separate backend capabilities whose controls are not yet available in the plugin.

## Use the backend API

The plugin currently has no Search, Reader, Digest, Task Center, AI-summary or Vault Snapshot
controls. Use these references for the implemented server behavior and its HTTP contract:

- [Search and content delivery](../product/capabilities/search-reader-and-events.md).
- [Explicit asynchronous Item AI summaries](../product/capabilities/item-ai-summaries.md).
- [Digest Definitions, schedules and immutable Issue history](../product/capabilities/digests-and-schedules.md).
- [Jobs, cancellation, Retry and indexing progress](../product/capabilities/durable-jobs.md).
- [Workspace Settings and model selection](../product/capabilities/workspace-settings.md).
- [OpenAPI paths, request schemas and responses](../../api/openapi/openapi.yaml).

The backend supplies content and validated image downloads for clients; the current plugin does
not create Markdown files in the vault. The accepted
[Vault Snapshot boundary](../product/decisions/0004-explicit-vault-snapshots.md) describes that
explicit client-owned operation.

For connection or recommendation problems, see [troubleshooting](../operations/troubleshooting.md).
