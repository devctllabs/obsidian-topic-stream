# Use one daemon with PostgreSQL and derived Qdrant indexes

Topic Stream uses one Go daemon for HTTP, scheduling, and worker pools. PostgreSQL is the only source of truth while Qdrant is a rebuildable retrieval index. This keeps deployment small without allowing search infrastructure to own irreplaceable product state.

The mandatory coupling of all runtime roles is superseded by ADR 0008. One all-in-one binary remains
the default deployment; PostgreSQL authority and derived Qdrant indexes remain unchanged.
