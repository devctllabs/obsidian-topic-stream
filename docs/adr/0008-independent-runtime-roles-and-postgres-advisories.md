# Run independent roles with Workspace-aware workers and PostgreSQL advisories

Status: Accepted

Topic Stream keeps one `serve` binary, but HTTP, schedulers, and workers are independently enabled.
All roles remain enabled by default for the smallest deployment. API and scheduler replicas select
one Workspace; a worker-only replica has no selected Workspace and claims Jobs for every Workspace.

Every Job persists a closed Queue value and its Workspace ID. Four process-global pools claim only
their Queue. A worker lazily creates and caches retrieval and handler state per Workspace, using the
Workspace ID from the claimed row rather than from JSON arguments.

Amendment (2026-10-22): Shared Feed preparation adds a fifth process-global pool. The current five
Queues and their capacities are listed in [Jobs and runtime roles](../architecture/jobs-and-runtime-roles.md).
The four-pool description above records the original role-separation decision.

PostgreSQL `LISTEN/NOTIFY` carries typed, Workspace-scoped settings invalidations and product
advisories between replicas. PostgreSQL REST state remains authoritative: notifications are not
replayed, listener recovery reloads cached settings, and connected SSE clients are forced to
reconnect and refetch after a delivery gap.

This supersedes only the mandatory role coupling in ADR 0001 and the process-local transport
assumption in ADR 0002. PostgreSQL remains authoritative, Qdrant remains derived, and SSE remains
advisory rather than a durable event log.

