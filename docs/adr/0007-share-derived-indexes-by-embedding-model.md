# Share derived indexes by Embedding Model

Topic Stream stores one Qdrant collection for each immutable Embedding Model schema and shares that collection across Workspaces. Every point carries a Workspace identifier and every read or write includes that Workspace filter; PostgreSQL remains authoritative for canonical content, feedback, corrections, revisions, and transition state. Separate collections per Workspace were rejected because they multiply initialization and maintenance work, while mixing incompatible vector dimensions in one collection is not supported.

A Workspace names a Desired Embedding Model separately from the Serving Embedding Model. Topic Stream publishes a Model Transition before copying current PostgreSQL state, writes later mutations to both destinations, repairs only failed deliveries, and changes the serving pointer only after the target is complete. Collection schema mismatches fail initialization instead of deleting or silently recreating data.

Amended 2026-10-23 by [ADR-0011](0011-retain-incremental-model-progress.md): retain model-owned
progress and coalesced changes for three models, deliver only active destinations, and fence
latest-selection and collection-incarnation recovery across independent runtime roles.
