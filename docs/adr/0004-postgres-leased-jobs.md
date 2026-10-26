# Use a PostgreSQL leased job queue

Background work uses an application-owned PostgreSQL table with transactional enqueue, idempotency keys, `SKIP LOCKED`, leases, and heartbeat recovery. A separate broker or job framework was rejected because the single-node self-hosted product already requires PostgreSQL and needs inspectable Task Center state.

